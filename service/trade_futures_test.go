package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupFuturesMarketTest 让合约行情中心像已经连上一样：BTCUSDT 合约的交易规则(数量步长 0.001、价格精度 0.1、最少 5 USDT)、
// 标记价格 100，确认盘口新鲜的 ping 由 syncErr 决定成败。合约开放，最高 10 倍杠杆，吃单 0.05%、挂单 0.02%；风险限额按内置的
// BTCUSDT 档位(第一档维持保证金率 0.4%)。
func setupFuturesMarketTest(t *testing.T, syncErr error) {
	t.Helper()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled, setting.FuturesEnabled = true, true
	setting.FuturesMaxLeverage, setting.FuturesTakerFeeBps, setting.FuturesMakerFeeBps = 10, 5, 2
	previousSetting := operation_setting.GetTradeSetting()
	operation_setting.SetTradeSettingForTest(setting)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true

	m := futuresMarket
	m.mu.Lock()
	previous := struct {
		connected, dataConnected bool
		stream                   tradeSyncer
		books                    map[string]*tradeBook
		marks                    map[string]tradeMark
		rules                    map[string]binance.SymbolInfo
		tickers                  map[string]binance.Ticker
	}{m.connected, m.dataConnected, m.stream, m.books, m.marks, m.rules, m.tickers}
	m.connected, m.dataConnected = true, true
	m.stream = fakeTradeSyncer{err: syncErr}
	m.books = map[string]*tradeBook{}
	m.marks = map[string]tradeMark{}
	m.tickers = map[string]binance.Ticker{}
	m.rules = map[string]binance.SymbolInfo{"BTCUSDT": {
		Symbol:      "BTCUSDT",
		TickSize:    tradeTestDecimal("0.1"),
		StepSize:    tradeTestDecimal("0.001"),
		MinQty:      tradeTestDecimal("0.001"),
		MinNotional: tradeTestDecimal("5"),
	}}
	m.mu.Unlock()
	setFuturesTestMark("100", time.Now().Add(time.Hour).UnixMilli())
	resetFuturesTestIndexes()

	t.Cleanup(func() {
		common.IsMasterNode = previousMaster
		operation_setting.SetTradeSettingForTest(previousSetting)
		m.mu.Lock()
		m.connected, m.dataConnected, m.stream, m.books, m.marks, m.rules, m.tickers = previous.connected, previous.dataConnected, previous.stream, previous.books, previous.marks, previous.rules, previous.tickers
		m.client = nil
		m.mu.Unlock()
		resetFuturesTestIndexes()
		for _, table := range []string{"users", "logs", "trade_accounts", "trade_ledgers", "trade_futures_positions", "trade_futures_histories", "trade_futures_orders"} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}

func resetFuturesTestIndexes() {
	m := futuresMarket
	m.matchMu.Lock()
	m.resting = map[string][]tradeRestingOrder{}
	m.matchMu.Unlock()
	m.riskMu.Lock()
	m.positions, m.riskPending = map[string][]tradeRiskPosition{}, map[string]bool{}
	m.crossPositions, m.crossCash = map[int][]tradeRiskPosition{}, map[int]int{}
	m.riskMu.Unlock()
	m.fundingMu.Lock()
	m.fundingQuiet = map[string]time.Time{}
	m.fundingMu.Unlock()
}

func setFuturesTestBook(version int64, bids []tradesim.Level, asks []tradesim.Level) {
	futuresMarket.mu.Lock()
	defer futuresMarket.mu.Unlock()
	futuresMarket.books["BTCUSDT"] = &tradeBook{
		bids:      bids,
		asks:      asks,
		version:   version,
		takenBids: map[string]decimal.Decimal{},
		takenAsks: map[string]decimal.Decimal{},
	}
}

func setFuturesTestMark(mark string, nextFundingTime int64) {
	futuresMarket.mu.Lock()
	defer futuresMarket.mu.Unlock()
	futuresMarket.marks["BTCUSDT"] = tradeMark{
		MarkPrice: binance.MarkPrice{Symbol: "BTCUSDT", Mark: tradeTestDecimal(mark), NextFundingTime: nextFundingTime},
		received:  time.Now(),
	}
}

func setFuturesTestLast(last string) {
	futuresMarket.mu.Lock()
	defer futuresMarket.mu.Unlock()
	futuresMarket.tickers["BTCUSDT"] = binance.Ticker{Symbol: "BTCUSDT", Close: tradeTestDecimal(last)}
}

func futuresTestLevels(t *testing.T, raw string) []model.TradeFuturesLevel {
	t.Helper()
	levels, err := model.ParseTradeFuturesLevels(raw)
	require.NoError(t, err)
	return levels
}

func futuresTestPosition(t *testing.T, userId int, side string) *model.TradeFuturesPosition {
	t.Helper()
	position, err := model.GetTradeFuturesPosition(userId, "BTCUSDT", side)
	require.NoError(t, err)
	return position
}

func openFuturesTest(t *testing.T, userId int, req TradeFuturesOrderRequest) *model.TradeFuturesOrder {
	t.Helper()
	req.Symbol, req.Action = "BTCUSDT", model.TradeFuturesOpen
	order, err := PlaceTradeFuturesOrder(context.Background(), userId, req)
	require.NoError(t, err)
	return order
}

// 合约市价单与现货一样按盘口逐档成交，收吃单手续费并从可用资金拿出保证金；同一份盘口吃掉的数量后面的单子不能再用。
// 平仓吃买档，按比例释放保证金并结算盈亏。
func TestFuturesMarketOrdersOpenAndCloseAgainstTheBook(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4201, "1000")
	setFuturesTestBook(1, tradeTestLevels("99", "1", "98", "2"), tradeTestLevels("100", "1", "101", "2"))

	order := openFuturesTest(t, 4201, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1.5"), Leverage: 10})
	assert.Equal(t, model.TradeOrderStatusFilled, order.Status)
	assert.Equal(t, "150.5", order.FilledValue)
	assert.Equal(t, tradeTestUsd("0.07525"), order.Fee, "0.05% taker fee")
	order = openFuturesTest(t, 4201, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 10})
	assert.Equal(t, "101", order.FilledValue, "only 1.5 left at 101")

	position := futuresTestPosition(t, 4201, model.TradeFuturesLong)
	assert.EqualValues(t, 250_000_000, position.Qty)
	assert.Equal(t, "251.5", position.EntryValue)
	assert.Equal(t, tradeTestUsd("25.15"), position.Margin)
	assert.Equal(t, tradeTestUsd("974.72425"), tradeTestCash(t, 4201))

	order, err := PlaceTradeFuturesOrder(context.Background(), 4201, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesLong,
		Action: model.TradeFuturesClose, Type: model.TradeOrderTypeMarket})
	require.NoError(t, err, "closing without a quantity closes the whole position")
	assert.Equal(t, "246", order.FilledValue, "1 @ 99 + 1.5 @ 98")
	assert.Equal(t, -tradeTestUsd("5.5"), order.RealizedPnl)
	assert.Equal(t, tradeTestUsd("994.25125"), tradeTestCash(t, 4201), "25.15 margin - 5.5 loss - 0.123 fee comes back")
	assert.Zero(t, futuresTestPosition(t, 4201, model.TradeFuturesLong).Qty)
}

// 不能立刻成交的限价开仓挂单，冻结保证金并按吃单费率预留手续费；盘口到价时按限价成交、收挂单手续费，退回多冻结的部分。
func TestFuturesLimitOpenRestsAndFillsAsMaker(t *testing.T) {
	setupFuturesMarketTest(t, errors.New("a resting order needs no fresh book"))
	fundTradeTestUser(t, 4202, "1000")
	setFuturesTestBook(1, tradeTestLevels("99", "5"), tradeTestLevels("100", "5"))

	order := openFuturesTest(t, 4202, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeLimit, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("99.5"), Leverage: 10})
	assert.Equal(t, model.TradeOrderStatusOpen, order.Status)
	assert.Equal(t, tradeTestUsd("9.99975"), order.Frozen, "9.95 margin + 0.04975 taker fee reserve")

	setFuturesTestBook(2, tradeTestLevels("98", "5"), tradeTestLevels("99", "0.4", "99.4", "5"))
	futuresMarket.matchSymbol("BTCUSDT")
	orders, _, err := model.GetTradeFuturesOrders(4202, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, model.TradeOrderStatusFilled, orders[0].Status)
	assert.Equal(t, "99.5", orders[0].FilledValue, "filled at the limit, not at the better book prices")
	assert.Equal(t, tradeTestUsd("0.0199"), orders[0].Fee, "0.02% maker fee")
	assert.Equal(t, tradeTestUsd("990.0301"), tradeTestCash(t, 4202))
	account, err := model.GetTradeAccount(4202)
	require.NoError(t, err)
	assert.Zero(t, account.Frozen)
}

// 止损按标记价格分档触发：标记价格越过哪几档就按盘口市价平掉那几档的数量，没到的档位留着；平掉的档位从仓位上删掉。
func TestFuturesStopLossLevelsCloseByTheBook(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4203, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))
	openFuturesTest(t, 4203, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 10,
		TakeProfits: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("120"), Qty: tradeTestDecimal("1")}},
		StopLosses: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("95"), Qty: tradeTestDecimal("0.4")},
			{Price: tradeTestDecimal("94"), Qty: tradeTestDecimal("0.6")}}})
	position := futuresTestPosition(t, 4203, model.TradeFuturesLong)
	require.Len(t, futuresTestLevels(t, position.TakeProfits), 1)
	require.Len(t, futuresTestLevels(t, position.StopLosses), 2)

	setFuturesTestMark("96", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.EqualValues(t, 100_000_000, futuresTestPosition(t, 4203, model.TradeFuturesLong).Qty, "96 is above both stop losses")

	setFuturesTestBook(2, tradeTestLevels("94.6", "5"), tradeTestLevels("94.7", "5"))
	setFuturesTestMark("94.5", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	position = futuresTestPosition(t, 4203, model.TradeFuturesLong)
	assert.EqualValues(t, 60_000_000, position.Qty, "only the 95 level is hit")
	stopLosses := futuresTestLevels(t, position.StopLosses)
	require.Len(t, stopLosses, 1)
	assert.Equal(t, "94", stopLosses[0].Price)
	assert.Equal(t, tradeTestUsd("991.77108"), tradeTestCash(t, 4203), "4 margin - 2.16 loss - 0.01892 fee comes back")

	setFuturesTestBook(3, tradeTestLevels("93.8", "5"), tradeTestLevels("93.9", "5"))
	setFuturesTestMark("93.9", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.Zero(t, futuresTestPosition(t, 4203, model.TradeFuturesLong).Qty)
	assert.Equal(t, tradeTestUsd("994.02294"), tradeTestCash(t, 4203), "6 margin - 3.72 loss - 0.02814 fee comes back")
	orders, _, err := model.GetTradeFuturesOrders(4203, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, model.TradeFuturesTriggerStopLoss, orders[0].Trigger)
	assert.Equal(t, "56.28", orders[0].FilledValue)
	history, _, err := model.GetTradeFuturesHistory(4203, "BTCUSDT", 0, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, model.TradeFuturesTriggerStopLoss, history[0].CloseReason)
}

// 止盈按最新成交价触发，不看标记价格；只平到价那一档的数量。
func TestFuturesTakeProfitTriggersOnTheLastPrice(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4208, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))
	openFuturesTest(t, 4208, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 10,
		TakeProfits: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("110"), Qty: tradeTestDecimal("0.5")}}})

	setFuturesTestBook(2, tradeTestLevels("110.2", "5"), tradeTestLevels("110.3", "5"))
	setFuturesTestMark("111", time.Now().Add(time.Hour).UnixMilli())
	setFuturesTestLast("109")
	futuresMarket.checkRisk("BTCUSDT")
	assert.EqualValues(t, 100_000_000, futuresTestPosition(t, 4208, model.TradeFuturesLong).Qty, "the mark alone does not take profit")

	setFuturesTestLast("110.5")
	futuresMarket.checkRisk("BTCUSDT")
	position := futuresTestPosition(t, 4208, model.TradeFuturesLong)
	assert.EqualValues(t, 50_000_000, position.Qty)
	assert.Empty(t, position.TakeProfits)
	orders, _, err := model.GetTradeFuturesOrders(4208, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 2)
	assert.Equal(t, model.TradeFuturesTriggerTakeProfit, orders[0].Trigger)
	assert.Equal(t, "55.1", orders[0].FilledValue, "0.5 @ 110.2")
}

// 标记价格让保证金加浮动盈亏跌到维持保证金时强平，保证金全部亏掉，可用资金不变。
func TestFuturesLiquidationAtTheMark(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4204, "100")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))
	openFuturesTest(t, 4204, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 10})

	setFuturesTestMark("90.5", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.EqualValues(t, 100_000_000, futuresTestPosition(t, 4204, model.TradeFuturesLong).Qty, "0.5 left above the 0.362 maintenance margin")

	setFuturesTestMark("90.3", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.Zero(t, futuresTestPosition(t, 4204, model.TradeFuturesLong).Qty)
	assert.Equal(t, tradeTestUsd("90.20485"), tradeTestCash(t, 4204), "the 0.3 margin left minus the 0.04515 taker fee comes back")
	history, _, err := model.GetTradeFuturesHistory(4204, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, model.TradeFuturesCloseByLiquidation, history[0].CloseReason)
	assert.Equal(t, -tradeTestUsd("9.79515"), history[0].Pnl, "net PnL equals the change of cash")
}

// 全仓的保证金只算占用：权益(资金、全仓挂单冻结的钱与全仓浮动盈亏)跌到全仓维持保证金时，按标记价格平掉全部全仓仓位并撤掉全仓
// 开仓挂单，盈亏与手续费记进资金。
func TestFuturesCrossLiquidatesTheAccountAtTheMark(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4209, "100")
	setFuturesTestBook(1, tradeTestLevels("99.9", "10"), tradeTestLevels("100", "10"))
	openFuturesTest(t, 4209, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesCross,
		Qty: tradeTestDecimal("9"), Leverage: 10})
	assert.Equal(t, tradeTestUsd("99.55"), tradeTestCash(t, 4209), "a cross open only pays the fee")
	resting := openFuturesTest(t, 4209, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeLimit, MarginMode: model.TradeFuturesCross,
		Qty: tradeTestDecimal("0.1"), Price: tradeTestDecimal("95"), Leverage: 10})
	assert.Equal(t, tradeTestUsd("0.95475"), resting.Frozen)

	_, err := PlaceTradeFuturesOrder(context.Background(), 4209, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesLong,
		Action: model.TradeFuturesOpen, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesCross, Qty: tradeTestDecimal("1"), Leverage: 10})
	require.ErrorIs(t, err, model.ErrTradeCashInsufficient, "8.6 USDT of cross available cannot back 10 more")
	_, err = PlaceTradeFuturesOrder(context.Background(), 4209, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesShort,
		Action: model.TradeFuturesOpen, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated, Qty: tradeTestDecimal("0.1"), Leverage: 10})
	require.ErrorIs(t, err, model.ErrTradeFuturesModeMismatch)

	setFuturesTestMark("89.3", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.EqualValues(t, 900_000_000, futuresTestPosition(t, 4209, model.TradeFuturesLong).Qty,
		"3.25 of equity, the money frozen by the cross order included, is above the 3.2148 maintenance margin")

	setFuturesTestMark("89.2", time.Now().Add(time.Hour).UnixMilli())
	futuresMarket.checkRisk("BTCUSDT")
	assert.Zero(t, futuresTestPosition(t, 4209, model.TradeFuturesLong).Qty)
	account, err := model.GetTradeAccount(4209)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("1.9486"), account.Cash, "99.55 - 97.2 loss - 0.4014 fee, the cross order refunded")
	assert.Zero(t, account.Frozen)
	orders, _, err := model.GetTradeFuturesOrders(4209, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 3)
	assert.Equal(t, model.TradeFuturesTriggerLiquidation, orders[0].Trigger)
	assert.Equal(t, model.TradeCancelByLiquidation, orders[1].CancelReason)
}

// 反手按市价平掉仓位，再按平掉的数量、同样的保证金模式与杠杆开反方向；一键全平平掉全部仓位。
func TestFuturesReverseAndCloseAll(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4210, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "10"), tradeTestLevels("100", "10"))
	openFuturesTest(t, 4210, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("2"), Leverage: 5, StopLosses: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("90"), Qty: tradeTestDecimal("2")}}})

	closed, opened, err := ReverseTradeFutures(context.Background(), 4210, "BTCUSDT", model.TradeFuturesLong)
	require.NoError(t, err)
	assert.Equal(t, "199.8", closed.FilledValue)
	assert.Equal(t, model.TradeFuturesShort, opened.Side)
	assert.Equal(t, "199.8", opened.FilledValue)
	assert.Zero(t, futuresTestPosition(t, 4210, model.TradeFuturesLong).Qty)
	short := futuresTestPosition(t, 4210, model.TradeFuturesShort)
	assert.EqualValues(t, 200_000_000, short.Qty)
	assert.Equal(t, model.TradeFuturesIsolated, short.MarginMode)
	assert.Equal(t, 5, short.Leverage)
	assert.Empty(t, short.StopLosses, "take profits and stop losses are not carried over")

	openFuturesTest(t, 4210, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 5})
	closedAll, failures, err := CloseAllTradeFutures(context.Background(), 4210)
	require.NoError(t, err)
	assert.Len(t, closedAll, 2)
	assert.Empty(t, failures)
	positions, err := model.GetTradeFuturesPositions(4210)
	require.NoError(t, err)
	assert.Empty(t, positions)
}

// 资金费按 Binance 已经结算的期次结算：开仓之前的、还没到的不算，同一期只结算一次；结算完到下一次结算时间之前不再去查。
func TestFuturesFundingSettlesBinanceRounds(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4205, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))
	openFuturesTest(t, 4205, TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: model.TradeFuturesIsolated,
		Qty: tradeTestDecimal("1"), Leverage: 10})
	openedAt := time.Now().Add(-time.Hour).UnixMilli()
	require.NoError(t, model.DB.Model(&model.TradeFuturesPosition{}).Where("user_id = ?", 4205).Update("funding_at", openedAt).Error)

	var requests atomic.Int32
	var startTime string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		startTime = r.URL.Query().Get("startTime")
		_, _ = w.Write([]byte(`[
			{"symbol":"BTCUSDT","fundingTime":` + decimal.NewFromInt(openedAt-1000).String() + `,"fundingRate":"0.5","markPrice":"100"},
			{"symbol":"BTCUSDT","fundingTime":` + decimal.NewFromInt(openedAt+1000).String() + `,"fundingRate":"0.001","markPrice":"110"},
			{"symbol":"BTCUSDT","fundingTime":` + decimal.NewFromInt(time.Now().Add(time.Hour).UnixMilli()).String() + `,"fundingRate":"0.5","markPrice":"100"}
		]`))
	}))
	t.Cleanup(server.Close)
	client := binance.NewFuturesClient(server.URL, nil)

	futuresMarket.settleSymbolFunding(client, "BTCUSDT")
	assert.Equal(t, decimal.NewFromInt(openedAt+1).String(), startTime)
	position := futuresTestPosition(t, 4205, model.TradeFuturesLong)
	assert.Equal(t, tradeTestUsd("9.89"), position.Margin, "a long pays 0.1% of 110 USDT")
	assert.Equal(t, -tradeTestUsd("0.11"), position.Funding)
	assert.Equal(t, openedAt+1000, position.FundingAt)

	futuresMarket.settleSymbolFunding(client, "BTCUSDT")
	assert.EqualValues(t, 1, requests.Load(), "nothing to ask Binance before the next funding time")
	assert.Equal(t, tradeTestUsd("9.89"), futuresTestPosition(t, 4205, model.TradeFuturesLong).Margin)
}

// 合约下单前的检查：合约开放、杠杆上限、止盈止损方向、限价范围，以及盘口确认不了新鲜时拒单。
func TestFuturesOrderValidation(t *testing.T) {
	setupFuturesMarketTest(t, nil)
	fundTradeTestUser(t, 4206, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))

	tests := []struct {
		name string
		req  TradeFuturesOrderRequest
		want error
	}{
		{name: "leverage above the limit", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 11}, want: ErrTradeLeverageInvalid},
		{name: "no leverage", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")}, want: ErrTradeLeverageInvalid},
		{name: "take profit below a long", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5,
			TakeProfits: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("90"), Qty: tradeTestDecimal("1")}}}, want: ErrTradeTpSlInvalid},
		{name: "stop loss below a short", req: TradeFuturesOrderRequest{Side: model.TradeFuturesShort, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5,
			StopLosses: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("90"), Qty: tradeTestDecimal("1")}}}, want: ErrTradeTpSlInvalid},
		{name: "a level without a quantity", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5,
			StopLosses: []TradeFuturesLevelRequest{{Price: tradeTestDecimal("90")}}}, want: model.ErrTradeFuturesLevelsInvalid},
		{name: "unknown margin mode", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, MarginMode: "portfolio", Qty: tradeTestDecimal("1"), Leverage: 5}, want: ErrTradeOrderInvalid},
		{name: "limit beyond the price band", req: TradeFuturesOrderRequest{Side: model.TradeFuturesShort, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("150.1"), Leverage: 5}, want: ErrTradePriceInvalid},
		{name: "value below the minimum", req: TradeFuturesOrderRequest{Side: model.TradeFuturesLong, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("0.04"), Leverage: 5}, want: ErrTradeNotionalTooSmall},
		{name: "unknown side", req: TradeFuturesOrderRequest{Side: "buy", Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5}, want: ErrTradeOrderInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.req.Symbol, test.req.Action = "BTCUSDT", model.TradeFuturesOpen
			_, err := PlaceTradeFuturesOrder(context.Background(), 4206, test.req)
			require.ErrorIs(t, err, test.want)
		})
	}
	_, err := PlaceTradeFuturesOrder(context.Background(), 4206, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesShort,
		Action: model.TradeFuturesClose, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")})
	require.ErrorIs(t, err, model.ErrTradeFuturesNoPosition)
	_, err = PlaceTradeFuturesOrder(context.Background(), 4206, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesShort,
		Action: model.TradeFuturesClose, Type: model.TradeOrderTypeMarket})
	require.ErrorIs(t, err, model.ErrTradeFuturesNoPosition, "closing everything with nothing to close")

	setting := operation_setting.GetTradeSetting().Clone()
	setting.FuturesEnabled = false
	operation_setting.SetTradeSettingForTest(setting)
	_, err = PlaceTradeFuturesOrder(context.Background(), 4206, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesLong,
		Action: model.TradeFuturesOpen, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5})
	require.ErrorIs(t, err, ErrTradeFuturesDisabled)
	assert.Equal(t, tradeTestUsd("1000"), tradeTestCash(t, 4206))
}

// 等不到下单之后的盘口时拒单，账户不变。
func TestFuturesMarketOrderRejectsAStaleBook(t *testing.T) {
	setupFuturesMarketTest(t, errors.New("ping timed out"))
	fundTradeTestUser(t, 4207, "1000")
	setFuturesTestBook(1, tradeTestLevels("99.9", "5"), tradeTestLevels("100", "5"))
	_, err := PlaceTradeFuturesOrder(context.Background(), 4207, TradeFuturesOrderRequest{Symbol: "BTCUSDT", Side: model.TradeFuturesShort,
		Action: model.TradeFuturesOpen, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1"), Leverage: 5})
	require.ErrorIs(t, err, ErrTradeMarketStale)
	assert.Equal(t, tradeTestUsd("1000"), tradeTestCash(t, 4207))
}
