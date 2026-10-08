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
