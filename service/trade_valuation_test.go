package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预测买入只是现金变为持仓，不应让总资产和排行榜凭空减少。
func TestTradeValuationIncludesPendingPredictionAssets(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 9821
	require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("90"), TotalIn: tradeTestUsd("100")}).Error)
	position := model.TradePredictionPosition{UserId: userId, WindowStart: 300, Side: model.TradePredictionUp,
		Qty: 20 * 100_000_000, InitialQty: 20 * 100_000_000, Cost: tradeTestUsd("10"), TotalCost: tradeTestUsd("10"), Status: model.TradePredictionActive}
	require.NoError(t, model.DB.Create(&position).Error)
	t.Cleanup(func() { model.DB.Where("user_id = ?", userId).Delete(&model.TradePredictionPosition{}) })
	account, valuation, err := ValueTradeUser(userId)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("10"), valuation.PredictionValue)
	assert.Equal(t, tradeTestUsd("100"), valuation.Equity)
	assert.Zero(t, valuation.Equity-account.TotalIn)
	assert.Equal(t, tradeTestUsd("90"), valuation.Cross.Withdrawable, "预测持仓不能作为现金转出")
}

// 借来的钱不是用户收益；持仓价值必须扣除借款及应计利息。
func TestTradeValuationDeductsSpotDebtAndFreezesUnpricedCollateral(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 9822
	require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("10"), TotalIn: tradeTestUsd("110")}).Error)
	require.NoError(t, model.DB.Create(&model.TradePosition{UserId: userId, Symbol: "BTCUSDT", Qty: 10 * 100_000_000, Cost: tradeTestUsd("1000")}).Error)
	require.NoError(t, model.DB.Create(&model.TradeSpotMargin{UserId: userId, Principal: tradeTestUsd("900"), Interest: tradeTestUsd("2"),
		ChargedUntil: common.GetTimestamp() + 3600}).Error)
	t.Cleanup(func() { model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotMargin{}) })
	account, valuation, err := ValueTradeUser(userId)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("902"), valuation.SpotDebt)
	assert.Equal(t, tradeTestUsd("108"), valuation.Equity)
	assert.Equal(t, -tradeTestUsd("2"), valuation.Equity-account.TotalIn)
	assert.False(t, valuation.SpotPricesFresh, "成交价可展示，但没有新鲜盘口不能释放抵押")
	assert.Zero(t, valuation.Cross.Withdrawable)
}

func TestTradeSnapshotSeparatesPredictionAndFinancingCashFlows(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 9823
	require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("50"), TotalIn: tradeTestUsd("100")}).Error)
	entries := []model.TradeLedger{
		{UserId: userId, Type: model.TradeLedgerPredictionBuy, Amount: -tradeTestUsd("20"), CreatedAt: 10},
		{UserId: userId, Type: model.TradeLedgerPredictionSettle, Amount: tradeTestUsd("25"), CreatedAt: 11},
		{UserId: userId, Type: model.TradeLedgerSpotLoan, Amount: tradeTestUsd("100"), CreatedAt: 12},
		{UserId: userId, Type: model.TradeLedgerSpotRepay, Amount: -tradeTestUsd("102"), CreatedAt: 13},
		{UserId: userId, Type: model.TradeLedgerSpotCover, Amount: tradeTestUsd("2"), CreatedAt: 14},
	}
	require.NoError(t, model.DB.Create(&entries).Error)
	snapshot, err := LiveTradeSnapshot(userId, "2026-10-06", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("5"), snapshot.PredictionFlow)
	assert.Equal(t, -tradeTestUsd("2"), snapshot.SpotFinanceFlow, "扣平台额度补入是外部入金，不是融资收益")
}
