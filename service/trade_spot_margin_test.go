package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeSpotMarginUsesFreshConfirmedBookWithoutRequiringPriceChanges(t *testing.T) {
	setupTradeMarketTest(t, nil)
	setTradeTestBook(1, tradeTestLevels("99", "20"), tradeTestLevels("100", "20"))
	tradeMarket.mu.Lock()
	previousSync := tradeMarket.bookSyncedAt
	tradeMarket.bookSyncedAt = time.Time{}
	tradeMarket.mu.Unlock()
	t.Cleanup(func() { tradeMarket.mu.Lock(); tradeMarket.bookSyncedAt = previousSync; tradeMarket.mu.Unlock() })
	marks := tradeFuturesMarks{market: futuresMarket}
	_, fresh := marks.SpotPrice("BTCUSDT")
	assert.False(t, fresh, "只有连接，没有确认当前盘口，不能释放抵押")
	require.NoError(t, tradeMarket.awaitFreshBook(context.Background(), time.Now().Add(-time.Second), 1000))
	price, fresh := marks.SpotPrice("BTCUSDT")
	assert.True(t, fresh, "报价没有变化，但pong确认后可继续估值")
	assert.Equal(t, "99", price.String())
	tradeMarket.mu.Lock()
	tradeMarket.bookSyncedAt = time.Now().Add(-time.Hour)
	tradeMarket.mu.Unlock()
	_, fresh = marks.SpotPrice("BTCUSDT")
	assert.False(t, fresh, "确认已过期不能继续用来放款")
}

// 按金额买入不能加杠杆，超过账户最高杠杆的倍数不收；按数量 5 倍买入借五分之四，利率用当前的实时利率。
func TestTradeSpotMarginServiceBorrowsAtTheCurrentRate(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 4190
	fundTradeTestUser(t, userId, "201")
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotMargin{}).Error)
	})
	previousRate := model.TradeSpotDailyRate()
	model.SetTradeSpotDailyRate(tradeTestDecimal("0.0005"))
	t.Cleanup(func() { model.SetTradeSpotDailyRate(previousRate) })
	setTradeTestBook(1, tradeTestLevels("99", "20"), tradeTestLevels("100", "20"))

	_, err := PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Amount: tradeTestDecimal("100"), Leverage: 5})
	require.ErrorIs(t, err, model.ErrTradeSpotFinancingInvalid)
	setting := operation_setting.GetTradeSetting().Clone()
	setting.SpotMaxLeverage = 3
	operation_setting.SetTradeSettingForTest(setting)
	_, err = PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("10"), Leverage: 5})
	require.ErrorIs(t, err, model.ErrTradeSpotFinancingInvalid, "账户最高 3 倍")
	setting.SpotMaxLeverage = 5
	operation_setting.SetTradeSettingForTest(setting)

	order, err := PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("10"), Leverage: 5})
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("800"), order.Borrowed)
	loan, err := model.GetTradeSpotMargin(userId)
	require.NoError(t, err)
	assert.Equal(t, "0.0005", loan.DailyRate)
	assert.Zero(t, tradeTestCash(t, userId))
}

// 风险率跌到 1.1 时定时检查按盘口逐档卖出、还清借款，卖掉的数量记进盘口，下一笔委托不能再吃。
func TestTradeSpotMarginRiskLiquidatesAgainstTheBook(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 4191
	fundTradeTestUser(t, userId, "201")
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotMargin{}).Error)
	})
	previousRate := model.TradeSpotDailyRate()
	model.SetTradeSpotDailyRate(decimal.Zero)
	t.Cleanup(func() { model.SetTradeSpotDailyRate(previousRate) })
	setTradeTestBook(1, tradeTestLevels("100", "20"), tradeTestLevels("100", "20"))
	_, err := PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideBuy, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("10"), Leverage: 5})
	require.NoError(t, err)

	// 买一跌到 85：风险率 850 / 800.5 ≤ 1.1。
	setTradeTestBook(2, tradeTestLevels("85", "4", "84", "20"), tradeTestLevels("86", "20"))
	require.NoError(t, tradeMarket.awaitFreshBook(context.Background(), time.Now().Add(-time.Second), 1000))
	require.NoError(t, settleTradeSpotMarginUser(userId, operation_setting.GetTradeSetting()))

	loan, err := model.GetTradeSpotMargin(userId)
	require.NoError(t, err)
	assert.Zero(t, loan.Principal+loan.Interest)
	positions, err := model.GetTradePositions(userId)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Less(t, positions[0].Qty, int64(10*100_000_000), "只卖够还债的数量")
	assert.Positive(t, positions[0].Qty)
	view, ok := tradeMarket.bookView("BTCUSDT")
	require.True(t, ok)
	assert.Equal(t, "84", view.Bids[0].Price.String(), "买一那档已经被强平吃完")
}

// useTradeSpotCoinRate 把借 asset 的日利率换成 rate，测试结束时换回来。
func useTradeSpotCoinRate(t *testing.T, asset string, rate string) {
	t.Helper()
	previous, ok := model.TradeSpotAssetDailyRate(asset)
	require.True(t, ok)
	model.SetTradeSpotAssetDailyRates(map[string]decimal.Decimal{asset: tradeTestDecimal(rate)})
	t.Cleanup(func() { model.SetTradeSpotAssetDailyRates(map[string]decimal.Decimal{asset: previous}) })
}

// 借币卖出是新开的仓位，模拟盘关了不能借；买回还币是减少风险，关了也能做：数量是欠的币向上取到步长，买到的先还借币。
func TestTradeSpotCoverBuysBackTheDebtEvenWhenTradingIsClosed(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 4192
	fundTradeTestUser(t, userId, "1000")
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotAssetLoan{}).Error)
	})
	useTradeSpotCoinRate(t, "BTC", "0")
	setting := operation_setting.GetTradeSetting().Clone()
	setting.SpotMaxLeverage = 5
	operation_setting.SetTradeSettingForTest(setting)
	setTradeTestBook(1, tradeTestLevels("99.9", "20"), tradeTestLevels("100", "20"))

	short := TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideSell, Type: model.TradeOrderTypeMarket, Qty: tradeTestDecimal("2.5"), Borrow: true}
	_, err := PlaceTradeOrder(context.Background(), userId, short)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("1000")+tradeTestUsd("249.75")-tradeTestUsd("0.24975"), tradeTestCash(t, userId))
	_, err = PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideSell, Type: model.TradeOrderTypeLimit,
		Qty: tradeTestDecimal("1"), Price: tradeTestDecimal("100"), Borrow: true})
	require.ErrorIs(t, err, model.ErrTradeSpotFinancingInvalid, "借币卖出只能是市价单")

	closed := setting.Clone()
	closed.Enabled = false
	operation_setting.SetTradeSettingForTest(closed)
	_, err = PlaceTradeOrder(context.Background(), userId, short)
	require.ErrorIs(t, err, ErrTradeDisabled)

	// 盘口变了：卖一 101。
	setTradeTestBook(2, tradeTestLevels("100.9", "20"), tradeTestLevels("101", "20"))
	order, err := CoverTradeSpotAsset(context.Background(), userId, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(250_000_000), order.FilledQty)
	loans, err := model.GetTradeSpotAssetLoans([]int{userId})
	require.NoError(t, err)
	assert.Empty(t, loans)
	positions, err := model.GetTradePositions(userId)
	require.NoError(t, err)
	assert.Empty(t, positions, "买到的正好还清借币")
	_, err = CoverTradeSpotAsset(context.Background(), userId, "BTCUSDT")
	require.ErrorIs(t, err, model.ErrTradePositionInsufficient, "没有借着的币")
}

// 借着的币涨到风险率不高于 1.1 时，定时检查在卖盘上把币买回来还掉，买掉的数量记进盘口。
func TestTradeSpotCoinLoanLiquidatesAgainstTheAsks(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 4193
	fundTradeTestUser(t, userId, "100")
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotAssetLoan{}).Error)
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotMargin{}).Error)
	})
	useTradeSpotCoinRate(t, "BTC", "0")
	setting := operation_setting.GetTradeSetting().Clone()
	setting.SpotMaxLeverage = 5
	operation_setting.SetTradeSettingForTest(setting)
	setTradeTestBook(1, tradeTestLevels("100", "20"), tradeTestLevels("100", "20"))
	_, err := PlaceTradeOrder(context.Background(), userId, TradeOrderRequest{Symbol: "BTCUSDT", Side: model.TradeSideSell, Type: model.TradeOrderTypeMarket,
		Qty: tradeTestDecimal("3.9"), Borrow: true})
	require.NoError(t, err)

	// 卖一涨到 115：风险率 489.61 / 448.5 ≤ 1.1。
	setTradeTestBook(2, tradeTestLevels("114", "20"), tradeTestLevels("115", "3", "116", "20"))
	require.NoError(t, tradeMarket.awaitFreshBook(context.Background(), time.Now().Add(-time.Second), 1000))
	require.NoError(t, settleTradeSpotMarginUser(userId, operation_setting.GetTradeSetting()))

	loans, err := model.GetTradeSpotAssetLoans([]int{userId})
	require.NoError(t, err)
	assert.Empty(t, loans, "借着的 3.9 个都买回来还掉了")
	view, ok := tradeMarket.bookView("BTCUSDT")
	require.True(t, ok)
	assert.Equal(t, "116", view.Asks[0].Price.String(), "卖一那档已经被强平吃完")
}
