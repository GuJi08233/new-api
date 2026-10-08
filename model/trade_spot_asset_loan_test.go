package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useTradeSpotAssetRates 换上测试用的借币日利率表，测试结束时换回来。
func useTradeSpotAssetRates(t *testing.T, rates map[string]string) {
	t.Helper()
	previous := tradeSpotAssetRates.Load()
	next := make(map[string]decimal.Decimal, len(rates))
	for asset, rate := range rates {
		next[asset] = decimal.RequireFromString(rate)
	}
	tradeSpotAssetRates.Store(&next)
	t.Cleanup(func() { tradeSpotAssetRates.Store(previous) })
}

// spotBook 给每个交易对一个买一价与卖一价(同价)，都新鲜。
func spotBook(prices map[string]float64) tradeSpotTestMarket {
	market := tradeSpotTestMarket{bids: map[string]decimal.Decimal{}, asks: map[string]decimal.Decimal{}, fresh: true}
	for symbol, price := range prices {
		market.bids[symbol] = decimal.NewFromFloat(price)
		market.asks[symbol] = decimal.NewFromFloat(price)
	}
	return market
}

// tradeSpotTestTrade 是一笔按 price 成交 qty(10^-8)的 BTCUSDT 市价单，手续费 0.1%。
func tradeSpotTestTrade(userId int, side string, qty int64, price float64, market TradeFuturesMarket) TradeOrderInput {
	value := tradesim.QtyFromUnits(qty).Mul(decimal.NewFromFloat(price))
	amount := tradeUsd(value.InexactFloat64())
	return TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: side, Type: TradeOrderTypeMarket, Qty: qty, SpotMaxLeverage: 5, Market: market,
		Fill: TradeFill{Qty: qty, Value: value, Amount: amount, Fee: amount / 1000, Price: decimal.NewFromFloat(price).String()}}
}

func tradeSpotTestAssetLoan(t *testing.T, userId int, symbol string) TradeSpotAssetLoan {
	t.Helper()
	var loans []TradeSpotAssetLoan
	require.NoError(t, DB.Where("user_id = ? AND symbol = ?", userId, symbol).Find(&loans).Error)
	if len(loans) == 0 {
		return TradeSpotAssetLoan{UserId: userId, Symbol: symbol}
	}
	return loans[0]
}

// 借币卖出：没有持仓时借入要卖的全部数量卖掉，到账的钱进资金。之后买入这个币时先还借币，先还利息再还本金，开空时的到账按还掉的
// 本金比例结转；还清之后多买的才进持仓，成本按数量比例分给持仓。
func TestTradeSpotBorrowSellOpensAShortThatBuysRepay(t *testing.T) {
	const userId = 3801
	setupTradeSpotFinancing(t, userId, 1000)
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0"})
	market := spotBook(map[string]float64{"BTCUSDT": 100})

	short := tradeSpotTestTrade(userId, TradeSideSell, 10*100_000_000, 100, market)
	short.Borrow = true
	_, err := PlaceTradeOrder(short)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(1999), tradeTestAccount(t, userId).Cash, "卖出 1000 扣 1 的手续费到账")
	loan := tradeSpotTestAssetLoan(t, userId, "BTCUSDT")
	assert.Equal(t, int64(10*100_000_000), loan.Principal)
	assert.Equal(t, tradeUsd(999), loan.Proceeds)
	positions, err := GetTradePositions(userId)
	require.NoError(t, err)
	assert.Empty(t, positions, "借来的币卖掉了，没有持仓")
	risk, err := ReadTradeSpotRisk(userId, market)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(1000), risk.Risk.AssetDebt)
	assert.Equal(t, "1.999", risk.Risk.Level().String())

	require.NoError(t, DB.Model(&TradeSpotAssetLoan{}).Where("user_id = ?", userId).Update("interest", 1_000_000).Error)
	_, err = PlaceTradeOrder(tradeSpotTestTrade(userId, TradeSideBuy, 4*100_000_000, 101, market))
	require.NoError(t, err)
	loan = tradeSpotTestAssetLoan(t, userId, "BTCUSDT")
	assert.Zero(t, loan.Interest)
	assert.Equal(t, int64(601_000_000), loan.Principal, "买到的 4 个先还 0.01 的利息，再还 3.99 的本金")
	assert.Equal(t, tradeUsd(999)-199_300_500, loan.Proceeds)
	positions, err = GetTradePositions(userId)
	require.NoError(t, err)
	assert.Empty(t, positions)

	_, err = PlaceTradeOrder(tradeSpotTestTrade(userId, TradeSideBuy, 7*100_000_000, 102, market))
	require.NoError(t, err)
	loan = tradeSpotTestAssetLoan(t, userId, "BTCUSDT")
	assert.Zero(t, loan.Debt())
	assert.Zero(t, loan.Proceeds)
	assert.Zero(t, loan.ChargedUntil)
	positions, err = GetTradePositions(userId)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, int64(99_000_000), positions[0].Qty, "还清 6.01 之后多买的 0.99 进持仓")
	assert.Equal(t, 50_540_490, positions[0].Cost, "持仓成本是这笔花费 714.714 的 0.99/7")
	assert.Equal(t, tradeUsd(1999-404.404-714.714), tradeTestAccount(t, userId).Cash)

	repays, _, err := GetTradeLedgers(userId, TradeLedgerSpotAssetRepay, 0, 10)
	require.NoError(t, err)
	require.Len(t, repays, 2)
	assert.Equal(t, int64(601_000_000), repays[0].Qty)
	assert.Equal(t, int64(400_000_000), repays[1].Qty)
	loans, _, err := GetTradeLedgers(userId, TradeLedgerSpotAssetLoan, 0, 10)
	require.NoError(t, err)
	require.Len(t, loans, 1)
	assert.Equal(t, int64(10*100_000_000), loans[0].Qty)
}

// 借币卖出先卖可卖的持仓，只借不够的部分(Binance 的自动借款)：卖掉的持仓按数量比例结转成本，挂单冻结的数量留着；开空到账只记
// 借来的那部分。可卖的持仓够数时就是普通卖出，不借币。
func TestTradeSpotBorrowSellBorrowsOnlyTheShortfall(t *testing.T) {
	const userId = 3805
	setupTradeSpotFinancing(t, userId, 1000)
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0"})
	market := spotBook(map[string]float64{"BTCUSDT": 100})
	_, err := PlaceTradeOrder(tradeSpotTestTrade(userId, TradeSideBuy, 3*100_000_000, 100, market))
	require.NoError(t, err)
	resting, err := PlaceTradeOrder(TradeOrderInput{UserId: userId, Symbol: "BTCUSDT", Side: TradeSideSell, Type: TradeOrderTypeLimit,
		Price: "120", Qty: 100_000_000, Rest: true})
	require.NoError(t, err)

	short := tradeSpotTestTrade(userId, TradeSideSell, 5*100_000_000, 100, market)
	short.Borrow = true
	_, err = PlaceTradeOrder(short)
	require.NoError(t, err)
	loan := tradeSpotTestAssetLoan(t, userId, "BTCUSDT")
	assert.Equal(t, int64(3*100_000_000), loan.Principal, "可卖 2 个，借 3 个")
	assert.Equal(t, tradeUsd(299.7), loan.Proceeds, "到账 499.5 的 3/5")
	positions, err := GetTradePositions(userId)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, int64(100_000_000), positions[0].Qty)
	assert.Equal(t, int64(100_000_000), positions[0].FrozenQty)
	assert.Equal(t, tradeUsd(100.1), positions[0].Cost, "买入成本 300.3 结转 2/3")
	assert.Equal(t, tradeUsd(1000-300.3+499.5), tradeTestAccount(t, userId).Cash)

	_, err = CancelTradeOrder(userId, resting.Id, TradeCancelByUser)
	require.NoError(t, err)
	plain := tradeSpotTestTrade(userId, TradeSideSell, 100_000_000, 100, market)
	plain.Borrow = true
	_, err = PlaceTradeOrder(plain)
	require.NoError(t, err)
	assert.Equal(t, int64(3*100_000_000), tradeSpotTestAssetLoan(t, userId, "BTCUSDT").Principal, "持仓够卖就不借")
	loans, _, err := GetTradeLedgers(userId, TradeLedgerSpotAssetLoan, 0, 10)
	require.NoError(t, err)
	require.Len(t, loans, 1)
	assert.Equal(t, int64(3*100_000_000), loans[0].Qty)
	positions, err = GetTradePositions(userId)
	require.NoError(t, err)
	assert.Empty(t, positions)
}

// 借币卖出之后风险率不能低于开仓线(5 倍账户 1.25)，借着的币按卖一估值；超过单个交易对的持仓上限、没有这个币的利率、账户没开
// 杠杆或者不是按数量的市价卖出时都不能借。被拒的整笔不留痕迹。
func TestTradeSpotBorrowSellChecksTheOpeningLevel(t *testing.T) {
	const userId = 3802
	setupTradeSpotFinancing(t, userId, 100)
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0"})
	market := spotBook(map[string]float64{"BTCUSDT": 100})

	tooBig := tradeSpotTestTrade(userId, TradeSideSell, 5*100_000_000, 100, market)
	tooBig.Borrow = true
	_, err := PlaceTradeOrder(tooBig)
	require.ErrorIs(t, err, ErrTradeSpotMarginLevel, "风险率 (100 + 499.5) / 500 ≈ 1.199")
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, userId).Cash)
	assert.Zero(t, tradeSpotTestAssetLoan(t, userId, "BTCUSDT").Debt())

	limited := tradeSpotTestTrade(userId, TradeSideSell, 3*100_000_000, 100, market)
	limited.Borrow, limited.MaxPositionCost = true, tradeUsd(250)
	_, err = PlaceTradeOrder(limited)
	require.ErrorIs(t, err, ErrTradePositionLimit)

	fits := tradeSpotTestTrade(userId, TradeSideSell, 390_000_000, 100, market)
	fits.Borrow = true
	_, err = PlaceTradeOrder(fits)
	require.NoError(t, err, "风险率 (100 + 389.61) / 390 ≈ 1.255")
	assert.Equal(t, int64(390_000_000), tradeSpotTestAssetLoan(t, userId, "BTCUSDT").Principal)

	for _, tc := range []struct {
		name   string
		adjust func(*TradeOrderInput)
	}{
		{"buy", func(in *TradeOrderInput) { in.Side = TradeSideBuy }},
		{"limit", func(in *TradeOrderInput) { in.Type, in.Rest, in.Price = TradeOrderTypeLimit, true, "100" }},
		{"with leverage", func(in *TradeOrderInput) { in.Leverage = 2 }},
		{"margin disabled", func(in *TradeOrderInput) { in.SpotMaxLeverage = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tradeSpotTestTrade(userId, TradeSideSell, 10_000_000, 100, market)
			input.Borrow = true
			tc.adjust(&input)
			_, err := PlaceTradeOrder(input)
			require.ErrorIs(t, err, ErrTradeSpotFinancingInvalid)
		})
	}
	useTradeSpotAssetRates(t, map[string]string{"ETH": "0"})
	noRate := tradeSpotTestTrade(userId, TradeSideSell, 10_000_000, 100, market)
	noRate.Borrow = true
	_, err = PlaceTradeOrder(noRate)
	require.ErrorIs(t, err, ErrTradeSpotFinancingInvalid, "没有 BTC 的利率不能借")
}

// 借币的利息按币算、按整点预收：到了已经收到的整点才收下一小时，漏收的整点一起补，不足 10^-8 的零头留到下次；拿不到这个币现在的
// 利率时沿用上次的。
func TestTradeSpotCoinInterestIsChargedHourly(t *testing.T) {
	const hour = int64(3600)
	start := int64(1791400000) / hour * hour
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0.0024"}) // 每小时 0.01%
	loan := TradeSpotAssetLoan{Symbol: "BTCUSDT", Principal: 100_000_000, ChargedUntil: start}
	for _, step := range []struct {
		name         string
		now          int64
		wantInterest int64
		wantUntil    int64
	}{
		{"before the whole hour", start - 1, 0, start},
		{"at the whole hour the coming hour is charged", start, 10_000, start + hour},
		{"within the hour nothing more", start + 10, 10_000, start + hour},
		{"missed hours are made up", start + 3*hour + 5, 40_000, start + 4*hour},
	} {
		_, err := chargeTradeSpotAssetInterest(&loan, step.now)
		require.NoError(t, err, step.name)
		assert.Equal(t, step.wantInterest, loan.Interest, step.name)
		assert.Equal(t, step.wantUntil, loan.ChargedUntil, step.name)
	}

	dust := TradeSpotAssetLoan{Symbol: "BTCUSDT", Principal: 3_000, ChargedUntil: start}
	_, err := chargeTradeSpotAssetInterest(&dust, start)
	require.NoError(t, err)
	assert.Zero(t, dust.Interest)
	assert.Equal(t, "0.3", dust.InterestRemainder, "每小时 0.3 个单位，零头留着")

	useTradeSpotAssetRates(t, map[string]string{})
	_, err = chargeTradeSpotAssetInterest(&loan, start+4*hour)
	require.NoError(t, err)
	assert.Equal(t, int64(50_000), loan.Interest, "没有现在的利率时按上次的 0.0024 收")
}

// 强平要还清借着的币：现金不够买回时先按顺序卖出现货凑钱，再在卖盘上把币买回来还掉，按买回的成交额收 2% 的强平费。
func TestTradeSpotLiquidationBuysBackBorrowedCoins(t *testing.T) {
	const userId = 3803
	setupTradeSpotFinancing(t, userId, 200.1)
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0"})
	opening := spotBook(map[string]float64{"BTCUSDT": 100, "ETHUSDT": 100})
	eth := TradeOrderInput{UserId: userId, Symbol: "ETHUSDT", Side: TradeSideBuy, Type: TradeOrderTypeMarket, Qty: 100_000_000,
		Fill: TradeFill{Qty: 100_000_000, Value: decimal.NewFromInt(100), Amount: tradeUsd(100), Fee: tradeUsd(0.1), Price: "100"}}
	_, err := PlaceTradeOrder(eth)
	require.NoError(t, err)
	short := tradeSpotTestTrade(userId, TradeSideSell, 390_000_000, 100, opening)
	short.Borrow = true
	_, err = PlaceTradeOrder(short)
	require.NoError(t, err)

	// BTC 涨到 140：风险率 (489.61 + 100) / 546 ≈ 1.08。
	market := spotBook(map[string]float64{"BTCUSDT": 140, "ETHUSDT": 100})
	books := []TradeSpotBook{
		{Symbol: "BTCUSDT", Asks: []tradesim.Level{{Price: decimal.NewFromInt(140), Qty: decimal.NewFromInt(10)}}, Bids: []tradesim.Level{{Price: decimal.NewFromInt(139), Qty: decimal.NewFromInt(10)}}, Step: decimal.RequireFromString("0.001")},
		{Symbol: "ETHUSDT", Bids: []tradesim.Level{{Price: decimal.NewFromInt(100), Qty: decimal.NewFromInt(10)}}, Step: decimal.RequireFromString("0.0001")},
	}
	result, err := SettleTradeSpotMargin(userId, market, books, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.True(t, result.Liquidated)
	assert.Zero(t, result.Covered)
	assert.Equal(t, "3.9", result.BuyFills["BTCUSDT"].Qty.String())
	// 买回 546 + 0.546 手续费、强平费 10.92，资金 489.61 差 67.856：够数的 0.6793 ETH 再多卖一个步长抵消取整，0.6794 到账 67.87203。
	assert.Equal(t, "0.6794", result.Fills["ETHUSDT"].Qty.String())
	assert.Zero(t, tradeSpotTestAssetLoan(t, userId, "BTCUSDT").Debt())
	assert.Equal(t, tradeUsd(0.01606), tradeTestAccount(t, userId).Cash)
	positions, err := GetTradePositions(userId)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "ETHUSDT", positions[0].Symbol)
	assert.Equal(t, int64(32_060_000), positions[0].Qty)
	fees, _, err := GetTradeLedgers(userId, TradeLedgerSpotLiquidationFee, 0, 10)
	require.NoError(t, err)
	require.Len(t, fees, 1)
	assert.Equal(t, -tradeUsd(10.92), fees[0].Amount)
	notices, _, err := GetTradeNotices(userId, 0, 10)
	require.NoError(t, err)
	sides := map[string]bool{}
	for _, notice := range notices {
		if notice.Kind == TradeNoticeLiquidation {
			sides[notice.Symbol+" "+notice.Side] = true
		}
	}
	assert.Equal(t, map[string]bool{"BTCUSDT buy": true, "ETHUSDT sell": true}, sides)
}

// 价格跳空、现金买不回借着的币时照样买回(资金扣成负的)，连同强平费从站内额度补。
func TestTradeSpotGapLiquidationOfACoinLoanChargesUserQuota(t *testing.T) {
	const userId = 3804
	setupTradeSpotFinancing(t, userId, 100)
	useTradeSpotAssetRates(t, map[string]string{"BTC": "0"})
	short := tradeSpotTestTrade(userId, TradeSideSell, 390_000_000, 100, spotBook(map[string]float64{"BTCUSDT": 100}))
	short.Borrow = true
	_, err := PlaceTradeOrder(short)
	require.NoError(t, err)

	books := []TradeSpotBook{{Symbol: "BTCUSDT", Asks: []tradesim.Level{{Price: decimal.NewFromInt(200), Qty: decimal.NewFromInt(10)}}, Step: decimal.RequireFromString("0.001")}}
	result, err := SettleTradeSpotMargin(userId, spotBook(map[string]float64{"BTCUSDT": 200}), books, tradeSpotTestPricing(), 5)
	require.NoError(t, err)
	assert.True(t, result.Liquidated)
	// 买回 780 + 0.78，强平费 15.6，资金 489.61 差 306.77。
	assert.Equal(t, tradeUsd(306.77), result.Covered)
	assert.Zero(t, tradeTestAccount(t, userId).Cash)
	assert.Zero(t, tradeSpotTestAssetLoan(t, userId, "BTCUSDT").Debt())
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(306.77), quota)

	users, err := ListTradeSpotMarginUsers()
	require.NoError(t, err)
	assert.NotContains(t, users, userId, "还清之后不再检查")
}
