package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
)

// tradeDirty 标记一个交易对上一轮推送之后变了什么。
type tradeDirty uint8

const (
	tradeDirtyTicker tradeDirty = 1 << iota
	tradeDirtyKline
	tradeDirtyBook
	tradeDirtyMark
	tradeDirtyTrades
)

const (
	// tradeBroadcastPeriod 是向页面推送行情的间隔：这段时间里的变化合并成一次，只推最新的。
	tradeBroadcastPeriod = 500 * time.Millisecond
	tradeBookPushLevels  = 10
	tradeSubscriberQueue = 256
)

// TradeEvent 是推给页面的一条行情，Name 是 SSE 的事件名，Data 是 JSON。
type TradeEvent struct {
	Name string
	Data []byte
}

// TradeSubscriber 是一个页面的行情订阅：一组交易对的 24 小时行情(合约另有标记价格与资金费率)，以及其中一个交易对的
// 1 分钟 K 线、盘口与最新成交。
// 页面处理太慢、队列满了时丢掉这一轮的推送，下一轮仍推最新的。最新成交推的是增量，tapeResets 与 tapeTradeID 记着推到了哪里。
type TradeSubscriber struct {
	Events       chan TradeEvent
	symbols      map[string]bool
	klineSymbol  string
	bookSymbol   string
	tradesSymbol string
	tapeResets   uint64
	tapeTradeID  int64
}

type tradeTickerEvent struct {
	Symbol      string `json:"s"`
	Price       string `json:"c"`
	Open        string `json:"o"`
	High        string `json:"h"`
	Low         string `json:"l"`
	Volume      string `json:"v"`
	QuoteVolume string `json:"q"`
	Time        int64  `json:"t"`
}

type tradeKlineEvent struct {
	Symbol string `json:"s"`
	Open   int64  `json:"t"`
	O      string `json:"o"`
	H      string `json:"h"`
	L      string `json:"l"`
	C      string `json:"c"`
	V      string `json:"v"`
	Q      string `json:"q"`
}

type tradeBookEvent struct {
	Symbol string      `json:"s"`
	Bids   [][2]string `json:"b"`
	Asks   [][2]string `json:"a"`
}

type tradeMarkEvent struct {
	Symbol          string `json:"s"`
	Mark            string `json:"p"`
	Index           string `json:"i"`
	FundingRate     string `json:"r"`
	NextFundingTime int64  `json:"T"`
}

// Subscribe 登记一个页面的订阅，并立刻把当前的行情、K 线、盘口与最新成交放进队列，页面不必等下一次变化。tradesSymbol 不为空时
// 连上它的成交推送，它必须在 symbols 里。
func (m *TradeMarket) Subscribe(symbols []string, klineSymbol string, bookSymbol string, tradesSymbol string) *TradeSubscriber {
	sub := &TradeSubscriber{
		Events:       make(chan TradeEvent, tradeSubscriberQueue),
		symbols:      map[string]bool{},
		klineSymbol:  klineSymbol,
		bookSymbol:   bookSymbol,
		tradesSymbol: tradesSymbol,
	}
	for _, symbol := range symbols {
		sub.symbols[symbol] = true
	}
	if tradesSymbol != "" {
		m.watchTape(tradesSymbol)
	}
	m.mu.RLock()
	events := []TradeEvent{m.statusEventLocked()}
	for symbol := range sub.symbols {
		events = append(events, m.symbolEventsLocked(symbol, tradeDirtyTicker|tradeDirtyKline|tradeDirtyBook|tradeDirtyMark|tradeDirtyTrades, sub)...)
	}
	m.mu.RUnlock()
	for _, event := range events {
		sub.Events <- event
	}
	m.feedMu.Lock()
	m.subs[sub] = struct{}{}
	m.feedMu.Unlock()
	return sub
}

// Unsubscribe 取消订阅。
func (m *TradeMarket) Unsubscribe(sub *TradeSubscriber) {
	m.feedMu.Lock()
	delete(m.subs, sub)
	m.feedMu.Unlock()
	if sub.tradesSymbol != "" {
		m.unwatchTape(sub.tradesSymbol)
	}
}

func (m *TradeMarket) markDirty(symbol string, what tradeDirty) {
	m.feedMu.Lock()
	m.dirty[symbol] |= what
	m.feedMu.Unlock()
}

// markAllDirty 在连接状态变化时让所有交易对重推一次，页面据此显示断线或恢复。
func (m *TradeMarket) markAllDirty() {
	m.mu.RLock()
	symbols := m.config.Symbols
	m.mu.RUnlock()
	m.feedMu.Lock()
	m.dirty[""] = tradeDirtyTicker
	for _, symbol := range symbols {
		m.dirty[symbol] |= tradeDirtyTicker | tradeDirtyBook | tradeDirtyMark
	}
	m.feedMu.Unlock()
}

func (m *TradeMarket) broadcast() {
	ticker := time.NewTicker(tradeBroadcastPeriod)
	defer ticker.Stop()
	lastPrune := time.Now()
	for range ticker.C {
		if time.Since(lastPrune) > time.Minute {
			m.pruneKlineCache()
			lastPrune = time.Now()
		}
		m.pruneTapes()
		m.pushDirty()
	}
}

// pushDirty 把上一轮之后变了的行情推给订阅了的页面，只在 broadcast 这一个 goroutine 里调用(测试里直接调用)。
func (m *TradeMarket) pushDirty() {
	m.feedMu.Lock()
	dirty := m.dirty
	m.dirty = map[string]tradeDirty{}
	subs := make([]*TradeSubscriber, 0, len(m.subs))
	for sub := range m.subs {
		subs = append(subs, sub)
	}
	m.feedMu.Unlock()
	if len(dirty) == 0 || len(subs) == 0 {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var status []TradeEvent
	if _, ok := dirty[""]; ok {
		status = []TradeEvent{m.statusEventLocked()}
	}
	for _, sub := range subs {
		events := status
		for symbol, what := range dirty {
			if sub.symbols[symbol] {
				events = append(events, m.symbolEventsLocked(symbol, what, sub)...)
			}
		}
		for _, event := range events {
			select {
			case sub.Events <- event:
			default:
				// 最新成交推的是增量，丢了一次就要整份重推。
				if event.Name == "trades" {
					sub.tapeResets = 0
				}
			}
		}
	}
}

func (m *TradeMarket) statusEventLocked() TradeEvent {
	data, _ := common.Marshal(map[string]bool{"connected": m.connected && m.dataConnected})
	return TradeEvent{Name: "status", Data: data}
}

// symbolEventsLocked 生成一个交易对要推给 sub 的事件，调用方持有 m.mu 读锁。
func (m *TradeMarket) symbolEventsLocked(symbol string, what tradeDirty, sub *TradeSubscriber) []TradeEvent {
	var events []TradeEvent
	add := func(name string, value any) {
		data, err := common.Marshal(value)
		if err != nil {
			common.SysError(fmt.Sprintf("trade feed: failed to encode %s for %s: %v", name, symbol, err))
			return
		}
		events = append(events, TradeEvent{Name: name, Data: data})
	}
	if ticker, ok := m.tickers[symbol]; ok && what&tradeDirtyTicker != 0 {
		add("ticker", tradeTickerEvent{
			Symbol: symbol, Price: ticker.Close.String(), Open: ticker.Open.String(), High: ticker.High.String(), Low: ticker.Low.String(),
			Volume: ticker.Volume.String(), QuoteVolume: ticker.QuoteVolume.String(), Time: ticker.EventTime,
		})
	}
	if kline, ok := m.klines[symbol]; ok && what&tradeDirtyKline != 0 && symbol == sub.klineSymbol {
		add("kline", tradeKlineEvent{
			Symbol: symbol, Open: kline.OpenTime, O: kline.Open.String(), H: kline.High.String(), L: kline.Low.String(),
			C: kline.Close.String(), V: kline.Volume.String(), Q: kline.QuoteVolume.String(),
		})
	}
	if mark, ok := m.marks[symbol]; ok && what&tradeDirtyMark != 0 {
		add("mark", tradeMarkEvent{
			Symbol: symbol, Mark: mark.Mark.String(), Index: mark.Index.String(), FundingRate: mark.FundingRate.String(),
			NextFundingTime: mark.NextFundingTime,
		})
	}
	if what&tradeDirtyBook != 0 && symbol == sub.bookSymbol {
		event := tradeBookEvent{Symbol: symbol, Bids: [][2]string{}, Asks: [][2]string{}}
		if book := m.books[symbol]; book != nil && m.connected {
			event.Bids, event.Asks = tradeBookSide(book.bids), tradeBookSide(book.asks)
		}
		add("book", event)
	}
	if what&tradeDirtyTrades != 0 && symbol == sub.tradesSymbol {
		if event, ok := m.tapeEventLocked(symbol, sub); ok {
			add("trades", event)
		}
	}
	return events
}

func tradeBookSide(levels []tradesim.Level) [][2]string {
	out := make([][2]string, 0, min(len(levels), tradeBookPushLevels))
	for _, level := range levels[:min(len(levels), tradeBookPushLevels)] {
		out = append(out, [2]string{level.Price.String(), level.Qty.String()})
	}
	return out
}

// TradeKlineIntervals 是页面可以请求的 K 线周期。
var TradeKlineIntervals = []string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"}

const (
	TradeKlineMaxLimit = 1000
	// 最新一段 K 线缓存几秒，翻历史的缓存久一些：历史 K 线不会再变。
	tradeKlineLatestTTL  = 3 * time.Second
	tradeKlineHistoryTTL = 10 * time.Minute
	tradeSparklineTTL    = 5 * time.Minute
)

type tradeKlineEntry struct {
	at     time.Time
	ttl    time.Duration
	klines []binance.Kline
}

// pruneKlineCache 删掉过期的 K 线缓存。翻历史的请求各不相同，不清理的话缓存会一直长下去。
func (m *TradeMarket) pruneKlineCache() {
	m.klineCache.Range(func(key, value any) bool {
		if entry := value.(tradeKlineEntry); time.Since(entry.at) >= entry.ttl {
			m.klineCache.Delete(key)
		}
		return true
	})
}

// Klines 返回 K 线，同样的请求在缓存期内共用一份，同时到达的请求只向 Binance 发一次。endTime 为 0 表示最新一段。
func (m *TradeMarket) Klines(ctx context.Context, symbol string, interval string, limit int, endTime int64) ([]binance.Kline, error) {
	ttl := tradeKlineLatestTTL
	if endTime > 0 {
		ttl = tradeKlineHistoryTTL
	}
	return m.cachedKlines(ctx, symbol, interval, limit, endTime, ttl)
}

// Sparkline 返回交易对最近 24 小时的小时收盘价，行情列表画迷你走势用。
func (m *TradeMarket) Sparkline(ctx context.Context, symbol string) ([]string, error) {
	klines, err := m.cachedKlines(ctx, symbol, "1h", 24, 0, tradeSparklineTTL)
	if err != nil {
		return nil, err
	}
	closes := make([]string, len(klines))
	for i, kline := range klines {
		closes[i] = kline.Close.String()
	}
	return closes, nil
}

func (m *TradeMarket) cachedKlines(ctx context.Context, symbol string, interval string, limit int, endTime int64, ttl time.Duration) ([]binance.Kline, error) {
	key := fmt.Sprintf("%s|%s|%d|%d|%d", symbol, interval, limit, endTime, ttl)
	if value, ok := m.klineCache.Load(key); ok {
		if entry := value.(tradeKlineEntry); time.Since(entry.at) < ttl {
			return entry.klines, nil
		}
	}
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, ErrTradeMarketUnavailable
	}
	value, err, _ := m.klineGroup.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tradeRestTimeout)
		defer cancel()
		klines, err := client.Klines(ctx, symbol, interval, limit, endTime)
		if err != nil {
			return nil, err
		}
		m.klineCache.Store(key, tradeKlineEntry{at: time.Now(), ttl: ttl, klines: klines})
		return klines, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]binance.Kline), nil
}
