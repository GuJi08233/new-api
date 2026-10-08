package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/binance"

	"github.com/bytedance/gopkg/util/gopool"
)

// 最新成交(Binance 交易页上的"最新成交")：只有页面正在看的交易对才连 Binance 的归集成交推送，每个交易对一条连接；连上(含断线
// 重连)时先用 REST 补一份最近的成交，推送来的按编号合并去重，留最近 tradeTapeSize 笔。最后一个页面走了以后再连 tradeTapeLinger，
// 免得刷新页面、切换周期时反复断开重连。成交只用于展示，撮合仍按盘口。
const (
	tradeTapeSize   = 50
	tradeTapeLinger = 30 * time.Second
)

// tradeTape 是一个交易对的最新成交，旧的在前。resets 在成交列表不能只往后追加时加一(换了连接、重连补进了更早的成交)，
// 页面据此整份替换。
type tradeTape struct {
	generation uint64
	cancel     context.CancelFunc
	viewers    int
	idleSince  time.Time
	trades     []binance.AggTrade
	resets     uint64
}

type tradeTradesEvent struct {
	Symbol string           `json:"s"`
	Reset  bool             `json:"r"`
	Trades []tradeTapeTrade `json:"t"`
}

type tradeTapeTrade struct {
	ID         int64  `json:"i"`
	Price      string `json:"p"`
	Qty        string `json:"q"`
	Time       int64  `json:"T"`
	BuyerMaker bool   `json:"m"`
}

// watchTape 登记一个页面在看 symbol 的最新成交，还没连就连上。
func (m *TradeMarket) watchTape(symbol string) {
	m.mu.RLock()
	config, generation, client := m.config, m.generation, m.client
	m.mu.RUnlock()
	m.tapeMu.Lock()
	defer m.tapeMu.Unlock()
	tape := m.tapes[symbol]
	if tape == nil {
		tape = &tradeTape{}
		m.tapes[symbol] = tape
	}
	tape.viewers++
	if tape.cancel == nil || tape.generation != generation {
		m.startTapeLocked(symbol, tape, config, generation, client)
	}
}

// unwatchTape 是页面不再看 symbol 的最新成交，连接由 pruneTapes 在没人看 tradeTapeLinger 之后断开。
func (m *TradeMarket) unwatchTape(symbol string) {
	m.tapeMu.Lock()
	defer m.tapeMu.Unlock()
	if tape := m.tapes[symbol]; tape != nil && tape.viewers > 0 {
		tape.viewers--
		if tape.viewers == 0 {
			tape.idleSince = time.Now()
		}
	}
}

// pruneTapes 断开没人看了 tradeTapeLinger 的成交推送；行情中心换了配置(地址、代理、开放的交易对)时，还有人看的按新配置重连。
func (m *TradeMarket) pruneTapes() {
	m.mu.RLock()
	config, generation, client := m.config, m.generation, m.client
	m.mu.RUnlock()
	m.tapeMu.Lock()
	defer m.tapeMu.Unlock()
	for symbol, tape := range m.tapes {
		if tape.viewers == 0 && time.Since(tape.idleSince) >= tradeTapeLinger {
			if tape.cancel != nil {
				tape.cancel()
			}
			delete(m.tapes, symbol)
			continue
		}
		if tape.viewers > 0 && tape.generation != generation {
			m.startTapeLocked(symbol, tape, config, generation, client)
		}
	}
}

// startTapeLocked 按行情中心当前的配置(重新)连上 symbol 的归集成交推送，清空旧的成交。行情中心没连这个交易对(模拟盘关闭或者
// 交易对不开放)时不连。调用方持有 tapeMu。
func (m *TradeMarket) startTapeLocked(symbol string, tape *tradeTape, config tradeMarketConfig, generation uint64, client *binance.Client) {
	if tape.cancel != nil {
		tape.cancel()
	}
	tape.cancel, tape.generation, tape.trades = nil, generation, nil
	tape.resets++
	if client == nil || !slices.Contains(config.Symbols, symbol) {
		return
	}
	_, dialer, err := tradeMarketTransport(config.ProxyURL)
	if err != nil {
		return
	}
	base := config.WsURL
	if m.futures {
		base = strings.TrimRight(config.WsURL, "/") + "/market"
	}
	ctx, cancel := context.WithCancel(context.Background())
	tape.cancel = cancel
	stream := binance.NewStream(binance.StreamConfig{BaseURL: base, Symbols: []string{symbol}, AggTrades: true, Dialer: dialer},
		&tradeTapeHandler{market: m, symbol: symbol, generation: generation, client: client, ctx: ctx})
	gopool.Go(func() { stream.Run(ctx) })
}

// appendTape 把成交按编号合并进 symbol 的最新成交，留最近 tradeTapeSize 笔；连接已经换代时丢弃。
func (m *TradeMarket) appendTape(symbol string, generation uint64, trades []binance.AggTrade) {
	m.tapeMu.Lock()
	tape := m.tapes[symbol]
	if tape == nil || tape.generation != generation || len(trades) == 0 {
		m.tapeMu.Unlock()
		return
	}
	var newest int64
	if count := len(tape.trades); count > 0 {
		newest = tape.trades[count-1].ID
	}
	merged := append(slices.Clone(tape.trades), trades...)
	slices.SortFunc(merged, func(a, b binance.AggTrade) int { return cmp.Compare(a.ID, b.ID) })
	merged = slices.CompactFunc(merged, func(a, b binance.AggTrade) bool { return a.ID == b.ID })
	if len(merged) > tradeTapeSize {
		merged = merged[len(merged)-tradeTapeSize:]
	}
	for _, trade := range trades {
		if trade.ID < newest && newest > 0 {
			tape.resets++
			break
		}
	}
	tape.trades = merged
	m.tapeMu.Unlock()
	m.markDirty(symbol, tradeDirtyTrades)
}

// tapeEventLocked 是要推给 sub 的最新成交：sub 还没收到过、或者成交列表被整份换过时推整份，否则只推 sub 上次之后的新成交。
// 调用方持有 m.mu 读锁(锁的顺序是 mu 在 tapeMu 之前)。
func (m *TradeMarket) tapeEventLocked(symbol string, sub *TradeSubscriber) (tradeTradesEvent, bool) {
	m.tapeMu.Lock()
	tape := m.tapes[symbol]
	if tape == nil {
		m.tapeMu.Unlock()
		return tradeTradesEvent{}, false
	}
	trades, resets := tape.trades, tape.resets
	m.tapeMu.Unlock()
	event := tradeTradesEvent{Symbol: symbol, Reset: sub.tapeResets != resets, Trades: []tradeTapeTrade{}}
	for _, trade := range trades {
		if event.Reset || trade.ID > sub.tapeTradeID {
			event.Trades = append(event.Trades, tradeTapeTrade{ID: trade.ID, Price: trade.Price.String(), Qty: trade.Qty.String(),
				Time: trade.Time, BuyerMaker: trade.BuyerMaker})
		}
	}
	if !event.Reset && len(event.Trades) == 0 {
		return tradeTradesEvent{}, false
	}
	sub.tapeResets = resets
	if count := len(trades); count > 0 {
		sub.tapeTradeID = trades[count-1].ID
	}
	return event, true
}

// tradeTapeHandler 接收一个交易对的归集成交推送。连上时用 REST 补最近的成交：推送不先给快照，断线期间的成交也只能这样补回来。
type tradeTapeHandler struct {
	market     *TradeMarket
	symbol     string
	generation uint64
	client     *binance.Client
	ctx        context.Context
}

func (h *tradeTapeHandler) OnAggTrade(_ string, trade *binance.AggTrade, _ time.Time) {
	h.market.appendTape(h.symbol, h.generation, []binance.AggTrade{*trade})
}

func (h *tradeTapeHandler) OnConnected(connected bool) {
	if !connected {
		return
	}
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(h.ctx, tradeRestTimeout)
		defer cancel()
		trades, err := h.client.AggTrades(ctx, h.symbol, tradeTapeSize)
		if err != nil {
			if h.ctx.Err() == nil {
				common.SysError(fmt.Sprintf("trade tape: failed to load recent trades of %s: %v", h.symbol, err))
			}
			return
		}
		h.market.appendTape(h.symbol, h.generation, trades)
	})
}

func (h *tradeTapeHandler) OnDepth(string, *binance.Depth, time.Time) {}

func (h *tradeTapeHandler) OnTicker(*binance.Ticker, time.Time) {}

func (h *tradeTapeHandler) OnKline(string, *binance.Kline, bool, time.Time) {}

func (h *tradeTapeHandler) OnMarkPrice(*binance.MarkPrice, time.Time) {}
