package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

// TradeMarket 是模拟盘的行情中心：连着 Binance 的盘口、迷你行情与 1 分钟 K 线推送，保存每个交易对最新的盘口与行情，
// 给下单和撮合提供"下单之后"的盘口，给页面推送行情，并缓存 K 线。每个节点各连一条，跟着模拟盘配置连接、换地址时重连、
// 关闭时断开；限价单的撮合只在主节点运行。
type TradeMarket struct {
	mu sync.RWMutex
	// generation 每次按新配置重连加一，旧连接的回调据此丢弃。
	generation uint64
	config     tradeMarketConfig
	stop       context.CancelFunc
	client     *binance.Client
	stream     tradeSyncer
	connected  bool
	books      map[string]*tradeBook
	tickers    map[string]binance.Ticker
	klines     map[string]binance.Kline
	rules      map[string]binance.SymbolInfo
	rulesAt    time.Time
	lastError  string
	errorAt    int64

	// symbolLocks 让同一交易对的成交依次进行：按盘口算出成交、落库、记下吃掉的数量是一个整体，否则两笔委托会在同一档上重复成交。
	symbolLocks sync.Map

	feedMu sync.Mutex
	subs   map[*TradeSubscriber]struct{}
	dirty  map[string]tradeDirty

	matchMu      sync.Mutex
	matchPending map[string]bool
	matchSignal  chan struct{}
	resting      map[string][]tradeRestingOrder

	klineGroup singleflight.Group
	klineCache sync.Map
}

// tradeSyncer 是行情连接上"发 ping 等 pong"的能力(binance.Stream.Sync)，测试里换成假的。
type tradeSyncer interface {
	Sync(ctx context.Context) error
}

type tradeMarketConfig struct {
	RestURL  string
	WsURL    string
	ProxyURL string
	Symbols  []string
}

// tradeBook 是一个交易对最新的盘口，价格最优的档在前。taken 记下本节点在这份盘口上已经成交掉的数量(按价格)，
// 下一份盘口到来时清空：同一份盘口上挂着的数量只能成交一次。
type tradeBook struct {
	bids      []tradesim.Level
	asks      []tradesim.Level
	version   int64
	received  time.Time
	takenBids map[string]decimal.Decimal
	takenAsks map[string]decimal.Decimal
}

// tradeBookView 是一份可以拿来成交的盘口，已经扣掉了本节点在这份盘口上成交掉的数量。
type tradeBookView struct {
	Version int64
	Bids    []tradesim.Level
	Asks    []tradesim.Level
}

const (
	tradeDepthLevels   = 20
	tradeKlineInterval = "1m"
	tradeSettingPoll   = 2 * time.Second
	tradeRulesTTL      = 6 * time.Hour
	tradeRestTimeout   = 15 * time.Second
	// tradeSyncDelay 是下单后要等多久才在行情连接上发 ping。盘口按 100 毫秒合并推送，过了这段时间发出的 ping，
	// 它的 pong 回来时，Binance 在下单之前产生的盘口变化都已经推到(同一条连接上先发的先到)。
	tradeSyncDelay = 100 * time.Millisecond
)

var (
	ErrTradeMarketUnavailable = errors.New("trade market data is unavailable")
	ErrTradeMarketStale       = errors.New("trade market data is stale")
)

var (
	tradeMarket = &TradeMarket{
		books:        map[string]*tradeBook{},
		tickers:      map[string]binance.Ticker{},
		klines:       map[string]binance.Kline{},
		rules:        map[string]binance.SymbolInfo{},
		subs:         map[*TradeSubscriber]struct{}{},
		dirty:        map[string]tradeDirty{},
		matchPending: map[string]bool{},
		matchSignal:  make(chan struct{}, 1),
		resting:      map[string][]tradeRestingOrder{},
	}
	tradeMarketOnce sync.Once
)

// GetTradeMarket 返回本节点的行情中心。
func GetTradeMarket() *TradeMarket {
	return tradeMarket
}

// StartTradeMarket 启动行情中心：按配置连接 Binance、定时向页面推送行情，主节点上还撮合挂着的限价单。每个节点都要启动，
// 下单用的是本节点的盘口。
func StartTradeMarket() {
	tradeMarketOnce.Do(func() {
		gopool.Go(tradeMarket.supervise)
		gopool.Go(tradeMarket.broadcast)
		if common.IsMasterNode {
			gopool.Go(tradeMarket.match)
		}
	})
}

func (m *TradeMarket) supervise() {
	ticker := time.NewTicker(tradeSettingPoll)
	defer ticker.Stop()
	for {
		m.applySetting(operation_setting.GetTradeSetting())
		<-ticker.C
	}
}

// applySetting 让连接跟上配置：关闭或没有开放的交易对时断开，地址、代理或交易对变了就重连。
func (m *TradeMarket) applySetting(setting *operation_setting.TradeSetting) {
	want := tradeMarketConfig{}
	if setting.Enabled && len(setting.Symbols) > 0 {
		want = tradeMarketConfig{RestURL: setting.RestUrl, WsURL: setting.WsUrl, ProxyURL: setting.ProxyUrl, Symbols: slices.Sorted(slices.Values(setting.Symbols))}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.RestURL == want.RestURL && m.config.WsURL == want.WsURL && m.config.ProxyURL == want.ProxyURL && slices.Equal(m.config.Symbols, want.Symbols) {
		return
	}
	if m.stop != nil {
		m.stop()
	}
	m.generation++
	m.config = want
	m.stop, m.client, m.stream, m.connected = nil, nil, nil, false
	m.books = map[string]*tradeBook{}
	if len(want.Symbols) == 0 {
		return
	}
	httpClient, dialer, err := tradeMarketTransport(want.ProxyURL)
	if err != nil {
		m.recordErrorLocked(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	m.client = binance.NewClient(want.RestURL, httpClient)
	handler := &tradeStreamHandler{market: m, generation: m.generation, ctx: ctx, client: m.client, symbols: want.Symbols}
	stream := binance.NewStream(binance.StreamConfig{
		BaseURL:       want.WsURL,
		Symbols:       want.Symbols,
		DepthLevels:   tradeDepthLevels,
		Ticker:        true,
		KlineInterval: tradeKlineInterval,
		Dialer:        dialer,
	}, handler)
	m.stream = stream
	gopool.Go(func() { stream.Run(ctx) })
}

// tradeMarketTransport 按代理配置建出 REST 客户端与 WebSocket 拨号器；没配代理时两者都按环境变量(HTTPS_PROXY 等)决定。
func tradeMarketTransport(proxyURL string) (*http.Client, *websocket.Dialer, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = tradeRestTimeout
	if proxyURL == "" {
		return &http.Client{Timeout: tradeRestTimeout}, &dialer, nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid trade proxy url: %w", err)
	}
	proxied, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil, nil, err
	}
	dialer.Proxy = http.ProxyURL(parsed)
	return &http.Client{Transport: proxied.Transport, Timeout: tradeRestTimeout}, &dialer, nil
}

func (m *TradeMarket) recordErrorLocked(err error) {
	m.lastError = err.Error()
	m.errorAt = common.GetTimestamp()
}

func (m *TradeMarket) recordError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordErrorLocked(err)
}

// tradeStreamHandler 把一条连接的推送记进行情中心。回调在读取协程里执行，只做内存更新。
type tradeStreamHandler struct {
	market     *TradeMarket
	generation uint64
	ctx        context.Context
	client     *binance.Client
	symbols    []string
}

func (h *tradeStreamHandler) OnConnected(connected bool) {
	m := h.market
	m.mu.Lock()
	if m.generation != h.generation {
		m.mu.Unlock()
		return
	}
	m.connected = connected
	// 断线期间盘口可能变了却收不到推送，旧盘口不能再用来成交；重连后重新拉一份。
	m.books = map[string]*tradeBook{}
	m.mu.Unlock()
	m.markAllDirty()
	if connected {
		gopool.Go(h.seed)
	}
}

// seed 在连上之后补齐推送不会主动给的数据：交易规则、24 小时行情，以及每个交易对的一份盘口(盘口只在变化时推送，
// 冷门交易对可能几秒都没有一份)。连接先建好再拉 REST，拉到的盘口不会比推送漏掉的更旧。
func (h *tradeStreamHandler) seed() {
	m := h.market
	ctx, cancel := context.WithTimeout(h.ctx, time.Minute)
	defer cancel()
	m.mu.RLock()
	needRules := time.Since(m.rulesAt) > tradeRulesTTL || slices.ContainsFunc(h.symbols, func(symbol string) bool {
		_, ok := m.rules[symbol]
		return !ok
	})
	m.mu.RUnlock()
	if needRules {
		rules, err := h.client.ExchangeInfo(ctx, h.symbols)
		if err != nil {
			m.recordError(fmt.Errorf("binance exchange info: %w", err))
		} else {
			m.mu.Lock()
			m.rules, m.rulesAt = rules, time.Now()
			m.mu.Unlock()
		}
	}
	if tickers, err := h.client.Tickers(ctx, h.symbols); err != nil {
		m.recordError(fmt.Errorf("binance tickers: %w", err))
	} else {
		for i := range tickers {
			m.applyTicker(h.generation, &tickers[i], false)
		}
	}
	for _, symbol := range h.symbols {
		depth, err := h.client.Depth(ctx, symbol, tradeDepthLevels)
		if err != nil {
			m.recordError(fmt.Errorf("binance depth %s: %w", symbol, err))
			continue
		}
		m.applyDepth(h.generation, symbol, depth, time.Now())
	}
}

func (h *tradeStreamHandler) OnDepth(symbol string, depth *binance.Depth, received time.Time) {
	h.market.applyDepth(h.generation, symbol, depth, received)
}

func (h *tradeStreamHandler) OnTicker(ticker *binance.Ticker, _ time.Time) {
	h.market.applyTicker(h.generation, ticker, true)
}

func (h *tradeStreamHandler) OnKline(symbol string, kline *binance.Kline, _ bool, _ time.Time) {
	m := h.market
	m.mu.Lock()
	if m.generation != h.generation {
		m.mu.Unlock()
		return
	}
	if current, ok := m.klines[symbol]; !ok || kline.OpenTime >= current.OpenTime {
		m.klines[symbol] = *kline
	}
	m.mu.Unlock()
	m.markDirty(symbol, tradeDirtyKline)
}

// OnMarkPrice 不会被调用：现货连接不订阅合约的标记价格。
func (h *tradeStreamHandler) OnMarkPrice(*binance.MarkPrice, time.Time) {}

func tradeLevels(levels []binance.Level) []tradesim.Level {
	out := make([]tradesim.Level, len(levels))
	for i, level := range levels {
		out[i] = tradesim.Level{Price: level.Price, Qty: level.Qty}
	}
	return out
}

// applyDepth 换上一份更新的盘口；比手里的旧(REST 补的那份与推送交错到达时)就丢掉。
func (m *TradeMarket) applyDepth(generation uint64, symbol string, depth *binance.Depth, received time.Time) {
	m.mu.Lock()
	if m.generation != generation || !m.connected {
		m.mu.Unlock()
		return
	}
	if current := m.books[symbol]; current != nil && depth.LastUpdateID <= current.version {
		m.mu.Unlock()
		return
	}
	m.books[symbol] = &tradeBook{
		bids:      tradeLevels(depth.Bids),
		asks:      tradeLevels(depth.Asks),
		version:   depth.LastUpdateID,
		received:  received,
		takenBids: map[string]decimal.Decimal{},
		takenAsks: map[string]decimal.Decimal{},
	}
	m.mu.Unlock()
	m.markDirty(symbol, tradeDirtyBook)
	m.signalMatch(symbol)
}

// applyTicker 记下 24 小时行情。REST 拉到的那份没有事件时间，只在还没有推送时补上。
func (m *TradeMarket) applyTicker(generation uint64, ticker *binance.Ticker, pushed bool) {
	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		return
	}
	if current, ok := m.tickers[ticker.Symbol]; ok && (!pushed || ticker.EventTime < current.EventTime) {
		m.mu.Unlock()
		return
	}
	m.tickers[ticker.Symbol] = *ticker
	m.mu.Unlock()
	m.markDirty(ticker.Symbol, tradeDirtyTicker)
}

// lockSymbol 锁住一个交易对的成交，返回解锁函数。
func (m *TradeMarket) lockSymbol(symbol string) func() {
	value, _ := m.symbolLocks.LoadOrStore(symbol, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// bookView 返回一份扣掉本节点已成交数量的盘口副本，没有盘口(未连接、刚重连还没拉到)时 ok 为假。
func (m *TradeMarket) bookView(symbol string) (tradeBookView, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	book := m.books[symbol]
	if book == nil || !m.connected {
		return tradeBookView{}, false
	}
	available := func(levels []tradesim.Level, taken map[string]decimal.Decimal) []tradesim.Level {
		out := make([]tradesim.Level, 0, len(levels))
		for _, level := range levels {
			qty := level.Qty.Sub(taken[level.Price.String()])
			if qty.IsPositive() {
				out = append(out, tradesim.Level{Price: level.Price, Qty: qty})
			}
		}
		return out
	}
	return tradeBookView{Version: book.version, Bids: available(book.bids, book.takenBids), Asks: available(book.asks, book.takenAsks)}, true
}

// recordTaken 记下一次成交在 version 这份盘口上吃掉的数量，levels 与 fill.Takes 一一对应。盘口已经换了就不用记。
func (m *TradeMarket) recordTaken(symbol string, version int64, side tradesim.Side, levels []tradesim.Level, fill tradesim.Fill) {
	m.mu.Lock()
	defer m.mu.Unlock()
	book := m.books[symbol]
	if book == nil || book.version != version {
		return
	}
	taken := book.takenBids
	if side == tradesim.Buy {
		taken = book.takenAsks
	}
	for i, qty := range fill.Takes {
		key := levels[i].Price.String()
		taken[key] = taken[key].Add(qty)
	}
}

// awaitFreshBook 等到手里的盘口不早于 since：since 之后过 tradeSyncDelay 再在行情连接上发 ping，pong 回来就说明 Binance 在
// since 之前推出的盘口变化都已经读到。最多等 staleMs 毫秒，等不到返回 ErrTradeMarketStale。
func (m *TradeMarket) awaitFreshBook(ctx context.Context, since time.Time, staleMs int) error {
	m.mu.RLock()
	stream, connected := m.stream, m.connected
	m.mu.RUnlock()
	if stream == nil || !connected {
		return ErrTradeMarketUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(staleMs)*time.Millisecond)
	defer cancel()
	if wait := time.Until(since.Add(tradeSyncDelay)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ErrTradeMarketStale
		}
	}
	if err := stream.Sync(ctx); err != nil {
		return ErrTradeMarketStale
	}
	return nil
}

// Rules 返回交易对的交易规则(价格精度、数量步长、最小成交额)。
func (m *TradeMarket) Rules(symbol string) (binance.SymbolInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rules, ok := m.rules[symbol]
	return rules, ok
}

// LastPrice 返回交易对的最新成交价，没有行情时用盘口中间价。估值用，不用于成交。
func (m *TradeMarket) LastPrice(symbol string) (decimal.Decimal, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ticker, ok := m.tickers[symbol]; ok && ticker.Close.IsPositive() {
		return ticker.Close, true
	}
	if book := m.books[symbol]; book != nil && len(book.bids) > 0 && len(book.asks) > 0 {
		return book.bids[0].Price.Add(book.asks[0].Price).Div(decimal.NewFromInt(2)), true
	}
	return decimal.Zero, false
}

// TradeMarketQuote 是一个交易对给页面看的行情。
type TradeMarketQuote struct {
	Symbol      string `json:"symbol"`
	Price       string `json:"price"`
	Open        string `json:"open"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Volume      string `json:"volume"`
	QuoteVolume string `json:"quote_volume"`
	Bid         string `json:"bid"`
	Ask         string `json:"ask"`
	Time        int64  `json:"time"`
}

// Quote 返回交易对当前的行情，还没有任何数据时 ok 为假。
func (m *TradeMarket) Quote(symbol string) (TradeMarketQuote, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.quoteLocked(symbol)
}

func (m *TradeMarket) quoteLocked(symbol string) (TradeMarketQuote, bool) {
	ticker, hasTicker := m.tickers[symbol]
	book := m.books[symbol]
	if !hasTicker && book == nil {
		return TradeMarketQuote{}, false
	}
	quote := TradeMarketQuote{Symbol: symbol}
	if hasTicker {
		quote.Price, quote.Open, quote.High, quote.Low = ticker.Close.String(), ticker.Open.String(), ticker.High.String(), ticker.Low.String()
		quote.Volume, quote.QuoteVolume, quote.Time = ticker.Volume.String(), ticker.QuoteVolume.String(), ticker.EventTime
	}
	if book != nil && len(book.bids) > 0 && len(book.asks) > 0 {
		quote.Bid, quote.Ask = book.bids[0].Price.String(), book.asks[0].Price.String()
	}
	return quote, true
}

// TradeMarketStatus 是行情连接的状态，给管理员看。
type TradeMarketStatus struct {
	Running   bool                      `json:"running"`
	Connected bool                      `json:"connected"`
	LastError string                    `json:"last_error"`
	ErrorAt   int64                     `json:"error_at"`
	Symbols   []TradeMarketSymbolStatus `json:"symbols"`
}

type TradeMarketSymbolStatus struct {
	Symbol    string `json:"symbol"`
	HasBook   bool   `json:"has_book"`
	BookAgeMs int64  `json:"book_age_ms"`
	HasRules  bool   `json:"has_rules"`
	Price     string `json:"price"`
}

// Status 返回行情连接的状态。
func (m *TradeMarket) Status() TradeMarketStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := TradeMarketStatus{Running: m.stream != nil, Connected: m.connected, LastError: m.lastError, ErrorAt: m.errorAt}
	for _, symbol := range m.config.Symbols {
		item := TradeMarketSymbolStatus{Symbol: symbol}
		if book := m.books[symbol]; book != nil {
			item.HasBook, item.BookAgeMs = true, time.Since(book.received).Milliseconds()
		}
		_, item.HasRules = m.rules[symbol]
		if ticker, ok := m.tickers[symbol]; ok {
			item.Price = ticker.Close.String()
		}
		status.Symbols = append(status.Symbols, item)
	}
	return status
}
