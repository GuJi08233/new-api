// Package binance 是 Binance 现货与 U 本位合约公开行情的最小客户端，给模拟盘按真实盘口成交用：REST 查 K 线、24 小时行情、
// 交易规则与盘口快照(合约另有标记价格与已结算的资金费率)，WebSocket 订阅局部盘口、迷你行情与 K 线(合约另有标记价格)。
// 只用不需要 API Key 的公开接口，现货的官方地址(https://api.binance.com、wss://stream.binance.com:9443)与行情镜像
// (https://data-api.binance.vision、wss://data-stream.binance.vision)用法相同；合约是 https://fapi.binance.com 与
// wss://fstream.binance.com/public、/market。地址由调用方传入。
//
// 价格与数量一律用 decimal，不经过 float64。Binance 用字符串传小数，缺字段、非法值与负数都当作错误，不会悄悄变成 0：
// 按 0 价格成交比拒绝下单危险得多。
package binance

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Level 是盘口的一档：价格与这一价位上挂着的数量。
type Level struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

// Depth 是一次盘口快照，Bids 价格从高到低、Asks 从低到高(最优价在前)。
type Depth struct {
	LastUpdateID int64
	Bids         []Level
	Asks         []Level
}

// Ticker 是 24 小时滚动窗口的迷你行情。EventTime 毫秒，来自 REST 时为 0。
type Ticker struct {
	Symbol      string
	EventTime   int64
	Close       decimal.Decimal // 最新价
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Volume      decimal.Decimal // 成交量(基础资产)
	QuoteVolume decimal.Decimal // 成交额(计价资产)
}

// Kline 是一根 K 线，时间都是毫秒。
type Kline struct {
	OpenTime    int64
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	CloseTime   int64
	QuoteVolume decimal.Decimal
	Trades      int64
}

// SymbolInfo 是交易规则，没有对应过滤器时为零值。
type SymbolInfo struct {
	Symbol       string
	BaseAsset    string
	QuoteAsset   string
	Status       string          // TRADING 表示正常交易
	TickSize     decimal.Decimal // 价格步长(PRICE_FILTER)
	StepSize     decimal.Decimal // 数量步长(LOT_SIZE，MinQty、MaxQty 同)
	MinQty       decimal.Decimal
	MaxQty       decimal.Decimal
	MarketMaxQty decimal.Decimal // 市价单最多的数量(MARKET_LOT_SIZE)
	MinNotional  decimal.Decimal // 成交额下限(现货是 NOTIONAL，没有时取旧的 MIN_NOTIONAL；合约是 MIN_NOTIONAL 的 notional)
	MaxNotional  decimal.Decimal
	// PriceUp 与 PriceDown 是合约限价相对标记价格的上下限倍数(PERCENT_PRICE)，例如 1.05 与 0.95。
	PriceUp   decimal.Decimal
	PriceDown decimal.Decimal
}

// MarkPrice 是合约的标记价格推送。标记价格用来算浮动盈亏、判断强平，资金费率是本期按目前情况估出的费率。
type MarkPrice struct {
	Symbol          string
	EventTime       int64
	Mark            decimal.Decimal
	Index           decimal.Decimal
	FundingRate     decimal.Decimal // 可以为负
	NextFundingTime int64           // 下一次结算资金费的时间，毫秒
}

// FundingRate 是一次已经结算的资金费：结算时间、费率与结算用的标记价格。
type FundingRate struct {
	Symbol      string
	FundingTime int64
	Rate        decimal.Decimal // 可以为负
	MarkPrice   decimal.Decimal
}

// decimalParser 解析 Binance 用字符串表示的价格与数量。它只记下第一个错误，一条消息里的字段可以连着解析、最后检查一次。
type decimalParser struct {
	err error
}

// parse 解析一个字段。空串(缺字段)、非法值与负数都记为错误。
func (p *decimalParser) parse(field string, value string) decimal.Decimal {
	parsed := p.parseSigned(field, value)
	if parsed.IsNegative() {
		p.err = fmt.Errorf("%s: invalid decimal %q", field, value)
		return decimal.Zero
	}
	return parsed
}

// parseSigned 解析一个可以为负的字段(资金费率)。空串与非法值记为错误。
func (p *decimalParser) parseSigned(field string, value string) decimal.Decimal {
	if p.err != nil {
		return decimal.Zero
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		p.err = fmt.Errorf("%s: invalid decimal %q", field, value)
		return decimal.Zero
	}
	return parsed
}

// levels 解析盘口的一侧，每一档是 ["价格", "数量"]，顺序保持 Binance 给的最优价在前。
func (p *decimalParser) levels(side string, rows [][2]string) []Level {
	levels := make([]Level, 0, len(rows))
	for _, row := range rows {
		levels = append(levels, Level{Price: p.parse(side, row[0]), Qty: p.parse(side, row[1])})
	}
	return levels
}

// depthPayload 是 REST 盘口快照与局部盘口推送共用的格式。合约的局部盘口推送是 depthUpdate 事件，两侧叫 b、a，
// 版本是最后一个更新 ID u。只差大小写的字段(e 与 E、u 与 U)都要声明，见 Stream.dispatch。
type depthPayload struct {
	LastUpdateID int64       `json:"lastUpdateId"`
	Bids         [][2]string `json:"bids"`
	Asks         [][2]string `json:"asks"`
	EventType    string      `json:"e"`
	EventTime    int64       `json:"E"`
	FirstID      int64       `json:"U"`
	FinalID      int64       `json:"u"`
	UpdateBids   [][2]string `json:"b"`
	UpdateAsks   [][2]string `json:"a"`
}

// depth 解析出 Depth。缺了 bids 或 asks 的不是盘口快照，当成错误，免得把缓存里的盘口换成空的；空数组是合法的。
func (d *depthPayload) depth() (*Depth, error) {
	version, bids, asks := d.LastUpdateID, d.Bids, d.Asks
	if d.EventType == "depthUpdate" {
		version, bids, asks = d.FinalID, d.UpdateBids, d.UpdateAsks
	}
	if bids == nil || asks == nil {
		return nil, errors.New("depth without bids or asks")
	}
	var p decimalParser
	depth := &Depth{LastUpdateID: version, Bids: p.levels("bids", bids), Asks: p.levels("asks", asks)}
	if p.err != nil {
		return nil, p.err
	}
	return depth, nil
}
