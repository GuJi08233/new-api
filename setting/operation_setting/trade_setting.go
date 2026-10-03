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
	// StaleMs 是盘口多久没有更新就视为过期：市价单与限价单的撮合都只用这段时间内收到的盘口。
	StaleMs int `json:"stale_ms"`
	// RestUrl 与 WsUrl 是 Binance 现货行情的 REST 与 WebSocket 地址，服务器连不上官方地址时可以换成镜像，
	// 例如 https://data-api.binance.vision 与 wss://data-stream.binance.vision。
	RestUrl string `json:"rest_url"`
	WsUrl   string `json:"ws_url"`
	// ProxyUrl 是连接 Binance 用的代理(http、https 或 socks5)，空表示直连。
	ProxyUrl string `json:"proxy_url"`
}

// 交易对的种类，决定前端放在哪个分组里。
const (
	TradeKindCrypto = "crypto" // 加密货币
	TradeKindStock  = "stock"  // 美股代币
)

// TradeSymbolInfo 是模拟盘支持的一个 Binance 现货交易对。
type TradeSymbolInfo struct {
	Symbol string `json:"symbol"`
	// Ticker 是展示用的代码：加密货币是币名，美股代币是股票代码。
	Ticker string `json:"ticker"`
	Kind   string `json:"kind"`
}

// TradeSymbols 是模拟盘支持的全部交易对，开放哪些由 TradeSetting.Symbols 决定。美股代币是 Binance 现货上
// 以 B 结尾的代币化股票，24 小时交易，盘口比主流加密货币薄得多。
var TradeSymbols = []TradeSymbolInfo{
	{Symbol: "BTCUSDT", Ticker: "BTC", Kind: TradeKindCrypto},
	{Symbol: "ETHUSDT", Ticker: "ETH", Kind: TradeKindCrypto},
	{Symbol: "SOLUSDT", Ticker: "SOL", Kind: TradeKindCrypto},
	{Symbol: "BNBUSDT", Ticker: "BNB", Kind: TradeKindCrypto},
	{Symbol: "XRPUSDT", Ticker: "XRP", Kind: TradeKindCrypto},
	{Symbol: "DOGEUSDT", Ticker: "DOGE", Kind: TradeKindCrypto},
	{Symbol: "ZECUSDT", Ticker: "ZEC", Kind: TradeKindCrypto},
	{Symbol: "NVDABUSDT", Ticker: "NVDA", Kind: TradeKindStock},
	{Symbol: "TSLABUSDT", Ticker: "TSLA", Kind: TradeKindStock},
	{Symbol: "AMDBUSDT", Ticker: "AMD", Kind: TradeKindStock},
	{Symbol: "MUBUSDT", Ticker: "MU", Kind: TradeKindStock},
	{Symbol: "SNDKBUSDT", Ticker: "SNDK", Kind: TradeKindStock},
	{Symbol: "CRCLBUSDT", Ticker: "CRCL", Kind: TradeKindStock},
	{Symbol: "MSTRBUSDT", Ticker: "MSTR", Kind: TradeKindStock},
	{Symbol: "SPCXBUSDT", Ticker: "SPCX", Kind: TradeKindStock},
	{Symbol: "QQQBUSDT", Ticker: "QQQ", Kind: TradeKindStock},
	{Symbol: "SOXLBUSDT", Ticker: "SOXL", Kind: TradeKindStock},
}

const (
	TradeSettingPrefix = "trade_setting."

	TradeMaxFeeBps      = 100 // 手续费最多 1%
	TradeMaxOrderUsd    = 1_000_000
	TradeMaxPositionUsd = 10_000_000
	TradeMaxProfitOut   = 1_000_000
	TradeMinStaleMs     = 500
	TradeMaxStaleMs     = 60_000
)

// tradeSetting 仅作为配置框架(基于反射、无同步)就地修改的暂存区，请求 goroutine 读取下方发布的只读快照。
var tradeSetting = *DefaultTradeSetting()

var tradeSettingSnapshot atomic.Pointer[TradeSetting]

// DefaultTradeSetting 返回一份默认配置：默认关闭，开放全部交易对。
func DefaultTradeSetting() *TradeSetting {
	symbols := make([]string, 0, len(TradeSymbols))
	for _, info := range TradeSymbols {
		symbols = append(symbols, info.Symbol)
	}
	return &TradeSetting{
		Enabled:           false,
		Symbols:           symbols,
		FeeBps:            10,
		MaxOrderUsd:       1000,
		MaxPositionUsd:    5000,
		DailyProfitOutUsd: 100,
		StaleMs:           3000,
		RestUrl:           "https://api.binance.com",
		WsUrl:             "wss://stream.binance.com:9443",
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
	return &clone
}

// SymbolEnabled 表示这个交易对已开放交易。
func (s *TradeSetting) SymbolEnabled(symbol string) bool {
	return slices.Contains(s.Symbols, symbol)
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
		if value != "" && !isUrlWithScheme(value, "http", "https", "socks5") {
			return fmt.Errorf("代理地址必须是 http、https 或 socks5 地址")
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
