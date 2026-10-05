package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTradeSyncer struct{ err error }

func (f fakeTradeSyncer) Sync(context.Context) error { return f.err }

func tradeTestDecimal(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func tradeTestLevels(pairs ...string) []tradesim.Level {
	levels := make([]tradesim.Level, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		levels = append(levels, tradesim.Level{Price: tradeTestDecimal(pairs[i]), Qty: tradeTestDecimal(pairs[i+1])})
	}
	return levels
}

func tradeTestUsd(usd string) int {
	return int(tradeTestDecimal(usd).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
}

// setupTradeMarketTest 让行情中心像已经连上一样：BTCUSDT 的交易规则(数量步长 0.001、价格精度 0.01、最少 5 USDT)、
// 最新价 100，确认盘口新鲜的 ping 由 syncErr 决定成败。
func setupTradeMarketTest(t *testing.T, syncErr error) {
	t.Helper()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled = true
	previousSetting := operation_setting.GetTradeSetting()
	operation_setting.SetTradeSettingForTest(setting)
	// 撮合只在主节点运行，挂单只有在主节点上才进撮合索引。
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true

	m := tradeMarket
	m.mu.Lock()
	previous := struct {
		connected bool
		stream    tradeSyncer
		books     map[string]*tradeBook
		tickers   map[string]binance.Ticker
		rules     map[string]binance.SymbolInfo
	}{m.connected, m.stream, m.books, m.tickers, m.rules}
	m.connected = true
	m.stream = fakeTradeSyncer{err: syncErr}
	m.books = map[string]*tradeBook{}
	m.tickers = map[string]binance.Ticker{"BTCUSDT": {Symbol: "BTCUSDT", Close: tradeTestDecimal("100")}}
	m.rules = map[string]binance.SymbolInfo{"BTCUSDT": {
		Symbol:      "BTCUSDT",
		TickSize:    tradeTestDecimal("0.01"),
		StepSize:    tradeTestDecimal("0.001"),
		MinQty:      tradeTestDecimal("0.001"),
		MinNotional: tradeTestDecimal("5"),
	}}
	m.mu.Unlock()
	m.matchMu.Lock()
	m.resting = map[string][]tradeRestingOrder{}
	m.matchMu.Unlock()

	t.Cleanup(func() {
		common.IsMasterNode = previousMaster
		operation_setting.SetTradeSettingForTest(previousSetting)
		m.mu.Lock()
		m.connected, m.stream, m.books, m.tickers, m.rules = previous.connected, previous.stream, previous.books, previous.tickers, previous.rules
		m.mu.Unlock()
		m.matchMu.Lock()
		m.resting = map[string][]tradeRestingOrder{}
		m.matchMu.Unlock()
		for _, table := range []string{"users", "logs", "trade_accounts", "trade_positions", "trade_orders", "trade_ledgers", "trade_notices"} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}

func setTradeTestBook(version int64, bids []tradesim.Level, asks []tradesim.Level) {
	tradeMarket.mu.Lock()
	defer tradeMarket.mu.Unlock()
	tradeMarket.books["BTCUSDT"] = &tradeBook{
		bids:      bids,
		asks:      asks,
		version:   version,
		takenBids: map[string]decimal.Decimal{},
		takenAsks: map[string]decimal.Decimal{},
	}
}

func fundTradeTestUser(t *testing.T, userId int, usd string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: userId, Username: "trade-svc-" + strconv.Itoa(userId), AffCode: "trade-svc-aff-" + strconv.Itoa(userId), Quota: tradeTestUsd(usd)}).Error)
	_, err := model.TransferQuotaToTrade(userId, tradeTestUsd(usd))
	require.NoError(t, err)
}

func tradeTestCash(t *testing.T, userId int) int {
	t.Helper()
	account, err := model.GetTradeAccount(userId)
	require.NoError(t, err)
	return account.Cash
}

// 市价单按盘口逐档成交并收 0.1% 手续费；同一份盘口上已经成交掉的数量，下一笔单子不能再用，换了新盘口才重新计算。
func TestPlaceTradeOrderWalksTheBookAndConsumesDepth(t *testing.T) {
	setupTradeMarketTest(t, nil)
	fundTradeTestUser(t, 4101, "1000")
	setTradeTestBook(1, tradeTestLevels("99", "1"), tradeTestLevels("100", "0.5", "101", "1", "105", "2"))

	order, err := PlaceTradeOrder(context.Background(), 4101, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1.2")})
	require.NoError(t, err)
	assert.Equal(t, model.TradeOrderStatusFilled, order.Status)
	assert.Equal(t, "120.7", order.FilledValue, "0.5 @ 100 + 0.7 @ 101")
	assert.Equal(t, tradeTestUsd("120.7"), order.FilledAmount)
	assert.Equal(t, tradeTestUsd("0.1207"), order.Fee)

	order, err = PlaceTradeOrder(context.Background(), 4101, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")})
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("103.8"), order.FilledAmount, "0.3 @ 101 剩下的加上 0.7 @ 105")

	setTradeTestBook(2, tradeTestLevels("99", "1"), tradeTestLevels("100", "0.5", "101", "1"))
	order, err = PlaceTradeOrder(context.Background(), 4101, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("2")})
	require.NoError(t, err)
	assert.Equal(t, model.TradeOrderStatusCanceled, order.Status, "盘口只有 1.5 个，剩下的撤销")
	assert.Equal(t, model.TradeCancelByDepth, order.CancelReason)
	assert.EqualValues(t, 150_000_000, order.FilledQty)
	assert.Equal(t, tradeTestUsd("1000")-tradeTestUsd("120.7")-tradeTestUsd("0.1207")-tradeTestUsd("103.8")-tradeTestUsd("0.1038")-tradeTestUsd("151")-tradeTestUsd("0.151"), tradeTestCash(t, 4101))

	order, err = PlaceTradeOrder(context.Background(), 4101, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideSell, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")})
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("99"), order.FilledAmount, "卖单吃买一，买一只挂了 1 个")
}

// 按金额买入的市价单花的钱(含手续费)不超过给出的金额。
func TestPlaceTradeOrderByAmountStaysWithinTheAmount(t *testing.T) {
	setupTradeMarketTest(t, nil)
	fundTradeTestUser(t, 4102, "100")
	setTradeTestBook(1, tradeTestLevels("99", "1"), tradeTestLevels("100", "5"))

	order, err := PlaceTradeOrder(context.Background(), 4102, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Amount: tradeTestDecimal("50")})
	require.NoError(t, err)
	assert.Equal(t, model.TradeOrderStatusFilled, order.Status)
	assert.EqualValues(t, 49_900_000, order.Qty, "50 / (100 × 1.001) = 0.4995，按步长取到 0.499")
	assert.Equal(t, order.Qty, order.FilledQty)
	assert.LessOrEqual(t, order.FilledAmount+order.Fee, tradeTestUsd("50"))
	assert.Equal(t, tradeTestUsd("50"), order.Budget)
}

// 等不到下单之后的盘口(ping 没有回来)时拒单，账户不变。
func TestPlaceTradeOrderRejectsWhenTheBookCannotBeConfirmedFresh(t *testing.T) {
	setupTradeMarketTest(t, errors.New("ping timed out"))
	fundTradeTestUser(t, 4103, "100")
	setTradeTestBook(1, tradeTestLevels("99", "1"), tradeTestLevels("100", "5"))

	_, err := PlaceTradeOrder(context.Background(), 4103, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("0.1")})
	require.ErrorIs(t, err, ErrTradeMarketStale)
	assert.Equal(t, tradeTestUsd("100"), tradeTestCash(t, 4103))
	orders, total, err := model.GetTradeOrders(4103, false, "", 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, orders)

	// 不能立刻成交的限价单不需要最新盘口，照常挂单。
	order, err := PlaceTradeOrder(context.Background(), 4103, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("0.1"), Price: tradeTestDecimal("90")})
	require.NoError(t, err)
	assert.Equal(t, model.TradeOrderStatusOpen, order.Status)
}

// 下单前的检查：数量步长与最小数量、最小成交额、单笔上限、价格精度与价格范围。
func TestPlaceTradeOrderValidatesAgainstTheRules(t *testing.T) {
	setupTradeMarketTest(t, nil)
	fundTradeTestUser(t, 4104, "5000")
	setTradeTestBook(1, tradeTestLevels("99", "100"), tradeTestLevels("100", "100"))

	tests := []struct {
		name string
		req  TradeOrderRequest
		want error
	}{
		{name: "quantity below the step", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("0.0004")}, want: ErrTradeQtyTooSmall},
		{name: "value below the minimum", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("0.01")}, want: ErrTradeNotionalTooSmall},
		{name: "value above the per-order cap", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("11")}, want: ErrTradeOrderTooLarge},
		{name: "amount above the per-order cap", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Amount: tradeTestDecimal("1000.01")}, want: ErrTradeOrderTooLarge},
		{name: "price off the tick", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("99.005")}, want: ErrTradePriceInvalid},
		{name: "price far below the market", req: TradeOrderRequest{Side: model.TradeSideBuy, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("49.99")}, want: ErrTradePriceInvalid},
		{name: "amount on a sell", req: TradeOrderRequest{Side: model.TradeSideSell, Type: model.TradeOrderTypeMarket, Amount: tradeTestDecimal("10")}, want: ErrTradeOrderInvalid},
		{name: "unknown side", req: TradeOrderRequest{Side: "short", Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")}, want: ErrTradeOrderInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.req.Symbol = "BTCUSDT"
			_, err := PlaceTradeOrder(context.Background(), 4104, test.req)
			require.ErrorIs(t, err, test.want)
		})
	}
	_, err := PlaceTradeOrder(context.Background(), 4104, TradeOrderRequest{Symbol: "ETHUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("1")})
	require.ErrorIs(t, err, ErrTradeMarketUnavailable, "没有交易规则和盘口的交易对不能下单")
	assert.Equal(t, tradeTestUsd("5000"), tradeTestCash(t, 4104))
}

// 挂着的限价单在盘口满足限价时按限价成交，能成交多少受盘口深度限制，先挂的先成交；成交完退回多冻结的资金。
func TestRestingLimitOrdersFillAtTheLimitWhenTheBookCrosses(t *testing.T) {
	setupTradeMarketTest(t, nil)
	fundTradeTestUser(t, 4105, "1000")
	fundTradeTestUser(t, 4106, "1000")
	setTradeTestBook(1, tradeTestLevels("99", "1"), tradeTestLevels("100", "5"))

	first, err := PlaceTradeOrder(context.Background(), 4105, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("98")})
	require.NoError(t, err)
	second, err := PlaceTradeOrder(context.Background(), 4106, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeLimit, Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("98")})
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("98.098"), first.Frozen)
	assert.Equal(t, tradeTestUsd("1000")-tradeTestUsd("98.098"), tradeTestCash(t, 4105))

	setTradeTestBook(2, tradeTestLevels("97", "1"), tradeTestLevels("97.5", "1.5"))
	tradeMarket.matchSymbol("BTCUSDT")

	orders, _, err := model.GetTradeOrders(4105, false, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, first.Id, orders[0].Id)
	assert.Equal(t, model.TradeOrderStatusFilled, orders[0].Status)
	assert.Equal(t, tradeTestUsd("98"), orders[0].FilledAmount, "按限价 98 成交，不按盘口的 97.5")
	assert.Equal(t, "98", orders[0].FilledValue)
	assert.Zero(t, orders[0].Frozen)

	orders, _, err = model.GetTradeOrders(4106, true, "", 0, 10)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, second.Id, orders[0].Id)
	assert.EqualValues(t, 50_000_000, orders[0].FilledQty, "盘口只剩 0.5 个")
	assert.Equal(t, tradeTestUsd("98.098")-tradeTestUsd("49")-tradeTestUsd("0.049"), orders[0].Frozen)
}
