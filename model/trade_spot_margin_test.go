package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// tradeSpotTestMarket 给每个现货交易对一个买一价与卖一价；fresh 为假时所有价格都不新鲜。测试里没有全仓合约仓位，不会问到标记价格。
type tradeSpotTestMarket struct {
	bids  map[string]decimal.Decimal
	asks  map[string]decimal.Decimal
	fresh bool
}

func (m tradeSpotTestMarket) SpotPrice(symbol string) (decimal.Decimal, bool) {
	price, ok := m.bids[symbol]
	return price, ok && m.fresh
}

func (m tradeSpotTestMarket) SpotAsk(symbol string) (decimal.Decimal, bool) {
	price, ok := m.asks[symbol]
	return price, ok && m.fresh
}
func (m tradeSpotTestMarket) MarkPrice(string) (decimal.Decimal, bool) { return decimal.Zero, false }
func (m tradeSpotTestMarket) Brackets(string) tradesim.Brackets        { return nil }

func btcBid(price int64) tradeSpotTestMarket {
	return tradeSpotTestMarket{bids: map[string]decimal.Decimal{"BTCUSDT": decimal.NewFromInt(price)}, fresh: true}
}

// 1 美元 = common.QuotaPerUnit 额度单位，现货手续费 0.1%。
func tradeSpotTestPricing() tradesim.Pricing {
	return tradesim.Pricing{QuotaPerUsd: decimal.NewFromFloat(common.QuotaPerUnit), FeeBps: 10}
}

// useTradeSpotRate 换上测试用的借款日利率，测试结束时换回来。
func useTradeSpotRate(t *testing.T, rate string) {
	t.Helper()
	previous := TradeSpotDailyRate()
	SetTradeSpotDailyRate(decimal.RequireFromString(rate))
	t.Cleanup(func() { SetTradeSpotDailyRate(previous) })
}

func setupTradeSpotFinancing(t *testing.T, userId int, cash float64) {
	t.Helper()
	truncateTables(t)
	seedTradeUser(t, userId, cash)
	_, err := TransferQuotaToTrade(userId, tradeUsd(cash))
	require.NoError(t, err)
	useTradeSpotRate(t, "0")
}

// addTradeSpotTestCash 给用户的主钱包加额度再转进模拟盘。
func addTradeSpotTestCash(t *testing.T, userId int, usd float64) {
	t.Helper()
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", tradeUsd(usd))).Error)
	_, err := TransferQuotaToTrade(userId, tradeUsd(usd))
	require.NoError(t, err)
}

// tradeSpotMarginBuy 是按 100 买 10 个 BTC 的市价单(成交 1000、手续费 1)，leverage 倍杠杆，账户最高 5 倍。
func tradeSpotMarginBuy(userId int, leverage int, market TradeFuturesMarket) TradeOrderInput {
	return TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: 10 * 100_000_000,
		Leverage: leverage, SpotMaxLeverage: 5, Market: market,
		Fill: TradeFill{Qty: 10 * 100_000_000, Value: decimal.NewFromInt(1000), Amount: tradeUsd(1000), Fee: tradeUsd(1), Price: "100"}}
}

func tradeSpotTestLoan(t *testing.T, userId int) TradeSpotMargin {
	t.Helper()
	loan, err := GetTradeSpotMargin(userId)
	require.NoError(t, err)
	return loan
}

// 5 倍买入自己付五分之一和手续费，其余借入；借款不算本金。卖出到账的钱先还利息再还本金，还清后利息从头算。
func TestTradeSpotMarginBorrowsAndSalesRepayInterestFirst(t *testing.T) {
	const userId = 3701
	setupTradeSpotFinancing(t, userId, 201)
	order, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(800), order.Borrowed)
	account := tradeTestAccount(t, userId)
	assert.Zero(t, account.Cash)
	assert.Equal(t, tradeUsd(201), account.QuotaPrincipal, "借款不是转入的本金")
	assert.Equal(t, tradeUsd(201), account.TotalIn)

	addTradeSpotTestCash(t, userId, 50)
	loan, err := RepayTradeSpotMargin(userId, tradeUsd(50), btcBid(100))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(750), loan.Principal)
	require.NoError(t, DB.Model(&TradeSpotMargin{}).Where("user_id = ?", userId).Update("interest", tradeUsd(2)).Error)

	_, err = PlaceTradeOrder(TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeMarket, Qty: 10 * 100_000_000,
		Fill: TradeFill{Qty: 10 * 100_000_000, Value: decimal.NewFromInt(1100), Amount: tradeUsd(1100), Fee: tradeUsd(1.1), Price: "110"}})
	require.NoError(t, err)
	repaid := tradeSpotTestLoan(t, userId)
	assert.Zero(t, repaid.Principal)
	assert.Zero(t, repaid.Interest)
	assert.Zero(t, repaid.ChargedUntil)
	assert.Equal(t, tradeUsd(1098.9-752), tradeTestAccount(t, userId).Cash)
	entries, _, err := GetTradeLedgers(userId, TradeLedgerSpotRepay, 0, 10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, -tradeUsd(752), entries[0].Amount, "卖出还掉 2 的利息和 750 的本金")
}

// 利息按整点预收：到了已经收到的整点才收下一小时，节点停过、漏收的整点一起补，同一小时里重复调用不重复收，零头留到下次。
func TestTradeSpotInterestIsChargedHourly(t *testing.T) {
	const hour = int64(3600)
	start := int64(1791400000) / hour * hour
	rate := decimal.RequireFromString("0.0024") // 每小时 0.01%
	loan := TradeSpotMargin{Principal: tradeUsd(1000), ChargedUntil: start}
	for _, step := range []struct {
		name         string
		now          int64
		wantInterest float64
		wantUntil    int64
	}{
		{"before the whole hour", start - 1, 0, start},
		{"at the whole hour the coming hour is charged", start, 0.1, start + hour},
		{"later in the same hour", start + 1800, 0.1, start + hour},
		{"missed hours are charged together", start + 3*hour + 5, 0.4, start + 4*hour},
	} {
		_, err := chargeTradeSpotInterest(&loan, step.now, rate)
		require.NoError(t, err)
		assert.Equal(t, tradeUsd(step.wantInterest), loan.Interest, step.name)
		assert.Equal(t, step.wantUntil, loan.ChargedUntil, step.name)
	}

	small := TradeSpotMargin{Principal: 1000, ChargedUntil: start}
	for i := int64(0); i < 10; i++ {
		_, err := chargeTradeSpotInterest(&small, start+i*hour, rate)
		require.NoError(t, err)
	}
	assert.Equal(t, 1, small.Interest, "每小时 0.1 个额度单位，十个小时收满 1 个")
}

// 借款时先收到下一个整点为止的利息，记一条借款利息的账单，资金不动。
func TestTradeSpotMarginFirstInterestRunsToTheNextHour(t *testing.T) {
	const userId = 3702
	setupTradeSpotFinancing(t, userId, 201)
	useTradeSpotRate(t, "0.0024")
	before := common.GetTimestamp()
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	after := common.GetTimestamp()

	loan := tradeSpotTestLoan(t, userId)
	assert.Contains(t, []int64{tradeSpotNextHour(before), tradeSpotNextHour(after)}, loan.ChargedUntil)
	interestAt := func(now int64) int {
		return int(decimal.NewFromInt(int64(tradeUsd(800))).Mul(decimal.RequireFromString("0.0024")).
			Mul(decimal.NewFromInt(loan.ChargedUntil - now)).Div(decimal.NewFromInt(86400)).Floor().IntPart())
	}
	assert.GreaterOrEqual(t, loan.Interest, interestAt(after))
	assert.LessOrEqual(t, loan.Interest, interestAt(before))
	assert.Equal(t, "0.0024", loan.DailyRate)
	assert.Zero(t, tradeTestAccount(t, userId).Cash, "利息记进借款，不从资金里扣")
	entries, _, err := GetTradeLedgers(userId, TradeLedgerSpotInterest, 0, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, loan.Interest, entries[0].Fee)
	assert.Equal(t, "0.0024", entries[0].Price)
}

// 已经借过钱、风险率低于开仓线(5 倍账户 1.25)时不能再借；不加杠杆的买入照样可以。
func TestTradeSpotMarginBorrowNeedsTheInitialLevel(t *testing.T) {
	const userId = 3703
	setupTradeSpotFinancing(t, userId, 201)
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	addTradeSpotTestCash(t, userId, 100)

	// 买一 90：借 50 之后风险率 (150 + 900) / 850 ≈ 1.235。
	leveraged := TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: 100_000_000,
		Leverage: 2, SpotMaxLeverage: 5, Market: btcBid(90),
		Fill: TradeFill{Qty: 100_000_000, Value: decimal.NewFromInt(100), Amount: tradeUsd(100), Fee: tradeUsd(0.1), Price: "100"}}
	_, err = PlaceTradeOrder(leveraged)
	require.ErrorIs(t, err, ErrTradeSpotMarginLevel)
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, userId).Cash)
	assert.Equal(t, tradeUsd(800), tradeSpotTestLoan(t, userId).Principal)

	plain := leveraged
	plain.Leverage, plain.Qty = 1, 50_000_000
	plain.Fill = TradeFill{Qty: 50_000_000, Value: decimal.NewFromInt(50), Amount: tradeUsd(50), Fee: tradeUsd(0.05), Price: "100"}
	_, err = PlaceTradeOrder(plain)
	require.NoError(t, err, "买完风险率 (49.95 + 945) / 800 ≈ 1.24，高于强平线")
}

// 买完风险率不高于 1.1(会被立刻强平)的买入整笔回滚；持仓上限按全额成交金额算，不按保证金。
func TestTradeSpotMarginRejectsBuysThatWouldBeLiquidated(t *testing.T) {
	const userId = 3704
	setupTradeSpotFinancing(t, userId, 201)
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(87)))
	require.ErrorIs(t, err, ErrTradeSpotMarginLevel, "成交价 100、买一 87 时风险率 870 / 800 ≈ 1.09")
	assert.Equal(t, tradeUsd(201), tradeTestAccount(t, userId).Cash)
	assert.Zero(t, tradeSpotTestLoan(t, userId).Principal)
	positions, err := GetTradePositions(userId)
	require.NoError(t, err)
	assert.Empty(t, positions)

	request := tradeSpotMarginBuy(userId, 5, btcBid(100))
	request.MaxPositionCost = tradeUsd(500)
	_, err = PlaceTradeOrder(request)
	require.ErrorIs(t, err, ErrTradePositionLimit, "持仓上限按 1001 而不是保证金 201 检查")
}

// 有借款时把钱转出去(转回额度、给合约或预测用)之后风险率不能低于 2。
func TestTradeSpotMarginTransfersKeepLevelTwo(t *testing.T) {
	const userId = 3705
	setupTradeSpotFinancing(t, userId, 201)
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	addTradeSpotTestCash(t, userId, 700)

	// 风险率 (700 + 1000) / 800，要留下 2 × 800 − 1000 = 600。
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(100.01), tradeTestDay, tradeUsd(100), btcBid(100))
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(100), tradeTestDay, tradeUsd(100), btcBid(100))
	require.NoError(t, err)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		require.NoError(t, err)
		require.ErrorIs(t, checkTradeAffordTx(tx, account, tradeUsd(1), tradeUsd(1), btcBid(100)), ErrTradeCashInsufficient)
		require.NoError(t, checkTradeCashTx(tx, account, tradeUsd(600), tradeUsd(600), btcBid(100)), "买现货不是转出")
		return nil
	}))
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(1), tradeTestDay, tradeUsd(100), tradeSpotTestMarket{fresh: false})
	require.ErrorIs(t, err, ErrTradeMarkUnavailable, "价格不新鲜时不能转出")
}

// 强平先撤挂单，再按顺序卖出现货，卖够借款本息加 2% 强平费为止，剩下的现货留给用户。
func TestTradeSpotMarginLiquidationSellsJustEnough(t *testing.T) {
	const userId = 3706
	setupTradeSpotFinancing(t, userId, 301.1)
	eth := TradeOrderInput{UserId: userId, Symbol: "ETHUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: 100_000_000,
		Fill: TradeFill{Qty: 100_000_000, Value: decimal.NewFromInt(100), Amount: tradeUsd(100), Fee: tradeUsd(0.1), Price: "100"}}
	_, err := PlaceTradeOrder(eth)
	require.NoError(t, err)
	_, err = PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, tradeSpotTestMarket{
		bids: map[string]decimal.Decimal{"BTCUSDT": decimal.NewFromInt(100), "ETHUSDT": decimal.NewFromInt(100)}, fresh: true}))
	require.NoError(t, err)
	resting, err := PlaceTradeOrder(TradeOrderInput{UserId: userId, Symbol: "ETHUSDT", Side: TradeSideSell, Type: TradeOrderTypeLimit,
		Price: "200", Qty: 50_000_000, Rest: true})
	require.NoError(t, err)

	market := tradeSpotTestMarket{bids: map[string]decimal.Decimal{"BTCUSDT": decimal.NewFromInt(75), "ETHUSDT": decimal.NewFromInt(100)}, fresh: true}
	books := []TradeSpotBook{
		{Symbol: "BTCUSDT", Bids: []tradesim.Level{{Price: decimal.NewFromInt(75), Qty: decimal.NewFromInt(5)}, {Price: decimal.NewFromInt(74), Qty: decimal.NewFromInt(20)}}, Step: decimal.RequireFromString("0.001")},
		{Symbol: "ETHUSDT", Bids: []tradesim.Level{{Price: decimal.NewFromInt(100), Qty: decimal.NewFromInt(10)}}, Step: decimal.RequireFromString("0.0001")},
	}
	result, err := SettleTradeSpotMargin(userId, market, books, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.True(t, result.Liquidated)
	assert.Zero(t, result.Covered)
	require.Len(t, result.Canceled, 1)
	assert.Equal(t, resting.Id, result.Canceled[0].Id)
	assert.Equal(t, TradeCancelByLiquidation, result.Canceled[0].CancelReason)
	assert.Equal(t, "10", result.Fills["BTCUSDT"].Qty.String())
	assert.Equal(t, "0.7183", result.Fills["ETHUSDT"].Qty.String())

	// 到账 744.255 + 71.758815，还掉 800，强平费 16。
	assert.Zero(t, tradeSpotTestLoan(t, userId).Principal)
	assert.Equal(t, 6585, tradeTestAccount(t, userId).Cash)
	positions, err := GetTradePositions(userId)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "ETHUSDT", positions[0].Symbol)
	assert.Equal(t, int64(28_170_000), positions[0].Qty)
	assert.Zero(t, positions[0].FrozenQty)
	fees, _, err := GetTradeLedgers(userId, TradeLedgerSpotLiquidationFee, 0, 10)
	require.NoError(t, err)
	require.Len(t, fees, 1)
	assert.Equal(t, -tradeUsd(16), fees[0].Amount)
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Zero(t, quota)
}

// 价格跳空、现货卖光也还不够时，不够的部分连同强平费从资金里扣，资金扣成负数从站内额度补，额度可以是负的；重试不重复扣。
func TestTradeSpotMarginGapLiquidationChargesUserQuota(t *testing.T) {
	const userId = 3707
	setupTradeSpotFinancing(t, userId, 201)
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	books := []TradeSpotBook{{Symbol: "BTCUSDT", Bids: []tradesim.Level{{Price: decimal.NewFromInt(40), Qty: decimal.NewFromInt(100)}}, Step: decimal.RequireFromString("0.001")}}

	stale := btcBid(40)
	stale.fresh = false
	_, err = SettleTradeSpotMargin(userId, stale, books, tradeSpotTestPricing(), 5)
	require.ErrorIs(t, err, ErrTradeMarkUnavailable)
	assert.Equal(t, tradeUsd(800), tradeSpotTestLoan(t, userId).Principal)

	result, err := SettleTradeSpotMargin(userId, btcBid(40), books, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.True(t, result.Liquidated)
	// 卖出到账 399.6，还差 400.4，强平费 16。
	assert.Equal(t, tradeUsd(416.4), result.Covered)
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(416.4), quota)
	account := tradeTestAccount(t, userId)
	assert.Zero(t, account.Cash)
	assert.Equal(t, tradeUsd(201+416.4), account.TotalIn, "补的亏空算作转入")
	loan := tradeSpotTestLoan(t, userId)
	assert.Zero(t, loan.Principal+loan.Interest)

	result, err = SettleTradeSpotMargin(userId, btcBid(40), books, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.False(t, result.Liquidated)
	quota, err = GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(416.4), quota, "重试不能重复扣额度")
}

// 风险率跌到追加保证金线(5 倍账户 1.16)时提醒一次，回到线上之后再跌下来再提醒。
func TestTradeSpotMarginCallIsSentOncePerDrop(t *testing.T) {
	const userId = 3708
	setupTradeSpotFinancing(t, userId, 201)
	_, err := PlaceTradeOrder(tradeSpotMarginBuy(userId, 5, btcBid(100)))
	require.NoError(t, err)
	countCalls := func() int {
		notices, _, err := GetTradeNotices(userId, 0, 50)
		require.NoError(t, err)
		calls := 0
		for _, notice := range notices {
			if notice.Kind == TradeNoticeMarginCall {
				calls++
			}
		}
		return calls
	}
	for _, step := range []struct {
		name  string
		bid   int64
		calls int
	}{
		{"level 1.15 is below the call line", 92, 1},
		{"still below, no repeat", 91, 1},
		{"back to 1.25 clears the mark", 100, 1},
		{"below again calls again", 92, 2},
	} {
		result, err := SettleTradeSpotMargin(userId, btcBid(step.bid), nil, tradeSpotTestPricing(), 5)
		require.NoError(t, err, step.name)
		assert.False(t, result.Liquidated, step.name)
		assert.Equal(t, step.calls, countCalls(), step.name)
	}
}

func TestTradeSpotFinancingRejectsUnsupportedCombinations(t *testing.T) {
	for _, tc := range []struct {
		name      string
		leverage  int
		side      string
		orderType string
		budget    int
	}{
		{"above maximum", 6, TradeSideBuy, TradeOrderTypeMarket, 0},
		{"negative", -1, TradeSideBuy, TradeOrderTypeMarket, 0},
		{"leveraged limit", 2, TradeSideBuy, TradeOrderTypeLimit, 0},
		{"leveraged sell", 2, TradeSideSell, TradeOrderTypeMarket, 0},
		{"leveraged buy by amount", 2, TradeSideBuy, TradeOrderTypeMarket, tradeUsd(10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PlaceTradeOrder(TradeOrderInput{Leverage: tc.leverage, Side: tc.side, Type: tc.orderType, Budget: tc.budget})
			require.ErrorIs(t, err, ErrTradeSpotFinancingInvalid)
		})
	}
}
