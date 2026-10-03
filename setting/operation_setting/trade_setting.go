package operation_setting

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// TradeSetting 是模拟盘的全部可配置项。模拟盘按 Binance 现货的实时盘口成交，账户以 USDT 计价：1 USDT = 1 美元额度，
// 这个比例是产品规则，不在这里配置。账户里的钱来自用户转入的额度，又能转出成额度，所以下面的上限与手续费都是在
// 保护站点的真实额度。
type TradeSetting struct {
	// Enabled 关闭时不能下单、不能转入；已有的委托可以撤销，可用资金可以转出，持仓要等重新开放才能卖出。
	Enabled bool `json:"enabled"`
	// Symbols 是开放交易的交易对，取自 TradeSymbols。
	Symbols []string `json:"symbols"`
	// FeeBps 是成交手续费，单位万分之一，买入、卖出各收一次，按成交金额向上取整到额度单位。
	FeeBps int `json:"fee_bps"`
	// MaxOrderUsd 是单笔委托最多成交的金额(美元)。
	MaxOrderUsd int `json:"max_order_usd"`
	// MaxPositionUsd 是每个交易对的持仓成本加上未成交买单冻结的资金，最多多少美元。美股代币的盘口很薄，
	// 有人在 Binance 上挂单把价格拉偏时，这一项限制了能在偏离的价格上成交多少。
	MaxPositionUsd int `json:"max_position_usd"`
	// DailyProfitOutUsd 是每人每天最多把多少美元的盈利转出成额度；转回自己转入的额度不受限制。0 表示不限制。
	DailyProfitOutUsd int `json:"daily_profit_out_usd"`
	// StaleMs 是下单时等待最新盘口的最长时间(毫秒)。盘口只在变化时推送，冷门交易对几秒不变很正常，所以不按多久没收到推送判断
	// 过期，而是下单后在行情连接上发 ping：pong 回来说明下单前的变化都已收到。超过这个时间还没回来就拒单。
	StaleMs int `json:"stale_ms"`
	// RestUrl 与 WsUrl 是 Binance 现货行情的 REST 与 WebSocket 地址。默认用 Binance 的只读行情镜像 data-api.binance.vision 与
	// data-stream.binance.vision：数据与主站相同，受限地区(主站返回 451)也能访问；也可以换回主站 api.binance.com 与
	// stream.binance.com:9443。
	RestUrl string `json:"rest_url"`
	WsUrl   string `json:"ws_url"`
	// ProxyUrl 是连接 Binance 用的代理(http 或 socks5)，空表示按环境变量 HTTPS_PROXY 等决定。WebSocket 不支持 https 代理。
	// 现货与合约的行情连接共用它。
	ProxyUrl string `json:"proxy_url"`

	// FuturesEnabled 打开合约(U 本位永续，只有逐仓)，模拟盘本身也要打开。关闭后不能开仓；已有的仓位照样可以平仓、调整
	// 保证金和止盈止损，强平与资金费照常进行，行情连接会一直保持到所有仓位平掉。
	FuturesEnabled bool `json:"futures_enabled"`
	// FuturesSymbols 是开放开仓的永续合约，取自 TradeSymbols 的 Futures。
	FuturesSymbols []string `json:"futures_symbols"`
	// FuturesMaxLeverage 是合约最高杠杆，实际还不能超过 Binance 该合约第一档风险限额的最高杠杆。杠杆越高，价格跳空越过
	// 强平价时站点承担的亏空越多(逐仓最多亏掉保证金，超出的部分没人付)。
	FuturesMaxLeverage int `json:"futures_max_leverage"`
	// FuturesTakerFeeBps、FuturesMakerFeeBps 是合约的吃单、挂单手续费，单位万分之一。市价单和下单时就成交的部分按吃单收，
	// 挂着的限价单之后被撮合成交的部分按挂单收。
	FuturesTakerFeeBps int `json:"futures_taker_fee_bps"`
	FuturesMakerFeeBps int `json:"futures_maker_fee_bps"`
	// FuturesMaxPositionUsd 是每人每个合约多空两个仓位的开仓价值，加上挂着的开仓委托，合计最多多少美元。
	FuturesMaxPositionUsd int `json:"futures_max_position_usd"`
	// FuturesRestUrl 与 FuturesWsUrl 是 Binance U 本位合约行情的 REST 与 WebSocket 根地址。WebSocket 按 Binance 的要求
	// 分成 /public(盘口)与 /market(标记价格、行情、K 线)两条连接。Binance 没有合约的行情镜像，主站返回 451 时可以把
	// REST 换成 https://www.binance.com，或者配置代理。
	FuturesRestUrl string `json:"futures_rest_url"`
	FuturesWsUrl   string `json:"futures_ws_url"`
}

// 交易对的种类，决定前端放在哪个分组里。
const (
	TradeKindCrypto = "crypto" // 加密货币
	TradeKindStock  = "stock"  // 美股代币
)

// TradeSymbolInfo 是模拟盘支持的一个品种：Binance 现货交易对与对应的 U 本位永续合约。
type TradeSymbolInfo struct {
	Symbol string `json:"symbol"`
	// Ticker 是展示用的代码：加密货币是币名，美股代币是股票代码。
	Ticker string `json:"ticker"`
	Kind   string `json:"kind"`
	// Futures 是对应的 Binance U 本位永续合约，例如 NVDAUSDT。
	Futures string `json:"futures"`
	// FuturesMmrBps 与 FuturesMaxLeverage 是这个合约在 Binance 第一档风险限额里的维持保证金率(万分之几)与最高杠杆。
	// 模拟盘的仓位不超过 FuturesMaxPositionUsd，只用第一档。
	FuturesMmrBps      int `json:"futures_mmr_bps"`
	FuturesMaxLeverage int `json:"futures_max_leverage"`
}

// TradeSymbols 是模拟盘支持的全部品种，开放哪些由 TradeSetting.Symbols 与 FuturesSymbols 决定。美股代币是 Binance 现货上
// 以 B 结尾的代币化股票，24 小时交易，盘口比主流加密货币薄得多；对应的合约是 Binance 的 TradFi 永续合约。
var TradeSymbols = []TradeSymbolInfo{
	{Symbol: "BTCUSDT", Ticker: "BTC", Kind: TradeKindCrypto, Futures: "BTCUSDT", FuturesMmrBps: 40, FuturesMaxLeverage: 125},
	{Symbol: "ETHUSDT", Ticker: "ETH", Kind: TradeKindCrypto, Futures: "ETHUSDT", FuturesMmrBps: 40, FuturesMaxLeverage: 125},
	{Symbol: "SOLUSDT", Ticker: "SOL", Kind: TradeKindCrypto, Futures: "SOLUSDT", FuturesMmrBps: 50, FuturesMaxLeverage: 100},
	{Symbol: "BNBUSDT", Ticker: "BNB", Kind: TradeKindCrypto, Futures: "BNBUSDT", FuturesMmrBps: 50, FuturesMaxLeverage: 75},
	{Symbol: "XRPUSDT", Ticker: "XRP", Kind: TradeKindCrypto, Futures: "XRPUSDT", FuturesMmrBps: 50, FuturesMaxLeverage: 100},
	{Symbol: "DOGEUSDT", Ticker: "DOGE", Kind: TradeKindCrypto, Futures: "DOGEUSDT", FuturesMmrBps: 65, FuturesMaxLeverage: 75},
	{Symbol: "ZECUSDT", Ticker: "ZEC", Kind: TradeKindCrypto, Futures: "ZECUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 75},
	{Symbol: "NVDABUSDT", Ticker: "NVDA", Kind: TradeKindStock, Futures: "NVDAUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "TSLABUSDT", Ticker: "TSLA", Kind: TradeKindStock, Futures: "TSLAUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "AMDBUSDT", Ticker: "AMD", Kind: TradeKindStock, Futures: "AMDUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "MUBUSDT", Ticker: "MU", Kind: TradeKindStock, Futures: "MUUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "SNDKBUSDT", Ticker: "SNDK", Kind: TradeKindStock, Futures: "SNDKUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "CRCLBUSDT", Ticker: "CRCL", Kind: TradeKindStock, Futures: "CRCLUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "MSTRBUSDT", Ticker: "MSTR", Kind: TradeKindStock, Futures: "MSTRUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "SPCXBUSDT", Ticker: "SPCX", Kind: TradeKindStock, Futures: "SPCXUSDT", FuturesMmrBps: 65, FuturesMaxLeverage: 75},
	{Symbol: "QQQBUSDT", Ticker: "QQQ", Kind: TradeKindStock, Futures: "QQQUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
	{Symbol: "SOXLBUSDT", Ticker: "SOXL", Kind: TradeKindStock, Futures: "SOXLUSDT", FuturesMmrBps: 100, FuturesMaxLeverage: 50},
}

const (
	TradeSettingPrefix = "trade_setting."

	TradeMaxFeeBps      = 100 // 手续费最多 1%
	TradeMaxOrderUsd    = 1_000_000
	TradeMaxPositionUsd = 10_000_000
	TradeMaxProfitOut   = 1_000_000
	TradeMinStaleMs     = 500
	TradeMaxStaleMs     = 60_000
	TradeMaxLeverage    = 125
)

// tradeSetting 仅作为配置框架(基于反射、无同步)就地修改的暂存区，请求 goroutine 读取下方发布的只读快照。
var tradeSetting = *DefaultTradeSetting()

var tradeSettingSnapshot atomic.Pointer[TradeSetting]

// DefaultTradeSetting 返回一份默认配置：模拟盘与合约默认都关闭，开放全部交易对与合约。
func DefaultTradeSetting() *TradeSetting {
	symbols := make([]string, 0, len(TradeSymbols))
	futures := make([]string, 0, len(TradeSymbols))
	for _, info := range TradeSymbols {
		symbols = append(symbols, info.Symbol)
		futures = append(futures, info.Futures)
	}
	return &TradeSetting{
		Enabled:               false,
		Symbols:               symbols,
		FeeBps:                10,
		MaxOrderUsd:           1000,
		MaxPositionUsd:        5000,
		DailyProfitOutUsd:     100,
		StaleMs:               3000,
		RestUrl:               "https://data-api.binance.vision",
		WsUrl:                 "wss://data-stream.binance.vision",
		FuturesEnabled:        false,
		FuturesSymbols:        futures,
		FuturesMaxLeverage:    10,
		FuturesTakerFeeBps:    5,
		FuturesMakerFeeBps:    2,
		FuturesMaxPositionUsd: 5000,
		FuturesRestUrl:        "https://fapi.binance.com",
		FuturesWsUrl:          "wss://fstream.binance.com",
	}
}

func init() {
	config.GlobalConfig.Register("trade_setting", &tradeSetting)
	SyncTradeSetting()
}

// GetTradeSetting 返回只读快照，可被任意请求 goroutine 安全并发读取。
func GetTradeSetting() *TradeSetting {
	return tradeSettingSnapshot.Load()
}

// SyncTradeSetting 将暂存配置深拷贝后发布为只读快照。每次更新暂存结构后都必须调用(见 model/option.go 的 handleConfigUpdate)。
func SyncTradeSetting() {
	tradeSettingSnapshot.Store(tradeSetting.Clone())
}

// SetTradeSettingForTest 直接发布一个快照，仅供测试使用。
func SetTradeSettingForTest(setting *TradeSetting) {
	if setting == nil {
		setting = &TradeSetting{}
	}
	tradeSettingSnapshot.Store(setting)
}

// Clone 深拷贝一份配置：快照是只读的，交易对列表是切片，要改动配置必须先 Clone。
func (s *TradeSetting) Clone() *TradeSetting {
	clone := *s
	clone.Symbols = slices.Clone(s.Symbols)
	clone.FuturesSymbols = slices.Clone(s.FuturesSymbols)
	return &clone
}

// SymbolEnabled 表示这个交易对已开放交易。
func (s *TradeSetting) SymbolEnabled(symbol string) bool {
	return slices.Contains(s.Symbols, symbol)
}

// FuturesOpenEnabled 表示这个合约现在可以开仓：模拟盘与合约都打开，并且合约在开放列表里。
func (s *TradeSetting) FuturesOpenEnabled(symbol string) bool {
	return s.Enabled && s.FuturesEnabled && slices.Contains(s.FuturesSymbols, symbol)
}

// TradeSymbolOf 返回模拟盘支持的交易对的信息，不支持时 ok 为假。
func TradeSymbolOf(symbol string) (TradeSymbolInfo, bool) {
	for _, info := range TradeSymbols {
		if info.Symbol == symbol {
			return info, true
		}
	}
	return TradeSymbolInfo{}, false
}

// TradeFuturesSymbolOf 按永续合约查品种信息，不支持时 ok 为假。
func TradeFuturesSymbolOf(futures string) (TradeSymbolInfo, bool) {
	for _, info := range TradeSymbols {
		if info.Futures == futures {
			return info, true
		}
	}
	return TradeSymbolInfo{}, false
}

// FuturesLeverageLimit 是这个合约现在允许的最高杠杆：配置的上限与 Binance 第一档上限中较小的一个。
func (s *TradeSetting) FuturesLeverageLimit(info TradeSymbolInfo) int {
	return max(1, min(s.FuturesMaxLeverage, info.FuturesMaxLeverage))
}

// ValidateTradeOption 校验单个模拟盘配置项，非法值在落库前拒绝。
func ValidateTradeOption(key string, value string) error {
	field, ok := strings.CutPrefix(key, TradeSettingPrefix)
	if !ok {
		return nil
	}
	switch field {
	case "enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s 必须是 true 或 false", field)
		}
	case "symbols":
		var symbols []string
		if err := common.UnmarshalJsonStr(value, &symbols); err != nil {
			return fmt.Errorf("交易对列表格式无效")
		}
		for i, symbol := range symbols {
			if _, ok := TradeSymbolOf(symbol); !ok {
				return fmt.Errorf("不支持的交易对: %s", symbol)
			}
			if slices.Contains(symbols[:i], symbol) {
				return fmt.Errorf("交易对重复: %s", symbol)
			}
		}
	case "fee_bps":
		return validateTradeInt(value, 0, TradeMaxFeeBps, "手续费")
	case "max_order_usd":
		return validateTradeInt(value, 1, TradeMaxOrderUsd, "单笔最多成交金额")
	case "max_position_usd":
		return validateTradeInt(value, 1, TradeMaxPositionUsd, "每个交易对最多持仓金额")
	case "daily_profit_out_usd":
		return validateTradeInt(value, 0, TradeMaxProfitOut, "每天最多转出的盈利")
	case "stale_ms":
		return validateTradeInt(value, TradeMinStaleMs, TradeMaxStaleMs, "盘口过期时间")
	case "rest_url":
		if !isUrlWithScheme(value, "http", "https") {
			return fmt.Errorf("REST 地址必须是 http 或 https 地址")
		}
	case "ws_url":
		if !isUrlWithScheme(value, "ws", "wss") {
			return fmt.Errorf("WebSocket 地址必须是 ws 或 wss 地址")
		}
	case "proxy_url":
		if value != "" && !isUrlWithScheme(value, "http", "socks5") {
			return fmt.Errorf("代理地址必须是 http 或 socks5 地址")
		}
	case "futures_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s 必须是 true 或 false", field)
		}
	case "futures_symbols":
		var symbols []string
		if err := common.UnmarshalJsonStr(value, &symbols); err != nil {
			return fmt.Errorf("合约列表格式无效")
		}
		for i, symbol := range symbols {
			if _, ok := TradeFuturesSymbolOf(symbol); !ok {
				return fmt.Errorf("不支持的合约: %s", symbol)
			}
			if slices.Contains(symbols[:i], symbol) {
				return fmt.Errorf("合约重复: %s", symbol)
			}
		}
	case "futures_max_leverage":
		return validateTradeInt(value, 1, TradeMaxLeverage, "合约最高杠杆")
	case "futures_taker_fee_bps":
		return validateTradeInt(value, 0, TradeMaxFeeBps, "合约吃单手续费")
	case "futures_maker_fee_bps":
		return validateTradeInt(value, 0, TradeMaxFeeBps, "合约挂单手续费")
	case "futures_max_position_usd":
		return validateTradeInt(value, 1, TradeMaxPositionUsd, "每个合约最多持仓价值")
	case "futures_rest_url":
		if !isUrlWithScheme(value, "http", "https") {
			return fmt.Errorf("合约 REST 地址必须是 http 或 https 地址")
		}
	case "futures_ws_url":
		if !isUrlWithScheme(value, "ws", "wss") {
			return fmt.Errorf("合约 WebSocket 地址必须是 ws 或 wss 地址")
		}
	default:
		return fmt.Errorf("未知的模拟盘配置项: %s", field)
	}
	return nil
}

func validateTradeInt(value string, minValue int, maxValue int, name string) error {
	number, err := strconv.Atoi(value)
	if err != nil || number < minValue || number > maxValue {
		return fmt.Errorf("%s必须为 %d 到 %d", name, minValue, maxValue)
	}
	return nil
}

func isUrlWithScheme(value string, schemes ...string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	return slices.Contains(schemes, parsed.Scheme)
}

// ValidateTradeSetting 逐项校验一份完整配置。
func ValidateTradeSetting(setting *TradeSetting) error {
	values, err := config.ConfigToMap(setting)
	if err != nil {
		return err
	}
	for field, value := range values {
		if err := ValidateTradeOption(TradeSettingPrefix+field, value); err != nil {
			return err
		}
	}
	return nil
}
