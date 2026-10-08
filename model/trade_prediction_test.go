package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func predictionTestRound(t *testing.T, windowStart int64) TradePredictionRound {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TradePredictionRound{}, &TradePredictionPosition{}))
	truncateTables(t)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&TradePredictionPosition{}).Error)
		require.NoError(t, DB.Where("1 = 1").Delete(&TradePredictionRound{}).Error)
	})
	round := TradePredictionRound{WindowStart: windowStart, EndTime: windowStart + TradePredictionWindow, Slug: "btc-updown-5m-test", Title: "BTC up/down", ConditionId: "condition", UpToken: "111", DownToken: "222"}
	require.NoError(t, SaveTradePredictionRound(round))
	return round
}

func predictionTestBuy(userId int, round TradePredictionRound) TradePredictionFill {
	return TradePredictionFill{UserId: userId, WindowStart: round.WindowStart, Side: TradePredictionUp, Qty: 20 * 100_000_000, Amount: tradeUsd(10), Fee: tradeUsd(0.35), Price: "0.5", ExpiresAtMs: time.Now().Add(3 * time.Second).UnixMilli(), MaxCost: tradeUsd(5000)}
}

func TestTradePredictionRequiresDepositAndHonorsCollateral(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300)
	seedTradeUser(t, 6201, 100)
	input := predictionTestBuy(6201, round)
	_, err := BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	account, err := GetTradeAccount(6201)
	require.NoError(t, err)
	assert.Zero(t, account.Cash)
	_, err = TransferQuotaToTrade(6201, tradeUsd(20))
	require.NoError(t, err)
	market := tradeTestMarket{tradeFuturesSymbol: "100"}
	openTradeFutures(t, 6201, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	input.Market = market
	_, err = BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradeCashInsufficient)
	assert.Equal(t, tradeUsd(20), tradeTestAccount(t, 6201).Cash)
	input.Amount, input.Fee = tradeUsd(5), 0
	position, err := BuyTradePrediction(input)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(5), position.Cost)
	assert.Equal(t, tradeUsd(15), tradeTestAccount(t, 6201).Cash)
}

func TestTradePredictionPartialSalePreservesCostsAndOwnership(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300)
	seedTradeFuturesAccount(t, 6202, 30)
	input := predictionTestBuy(6202, round)
	position, err := BuyTradePrediction(input)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(19.65), tradeTestAccount(t, 6202).Cash)
	sale := TradePredictionFill{UserId: 6202, WindowStart: round.WindowStart, Side: TradePredictionUp, Qty: 5 * 100_000_000, Amount: tradeUsd(3), Fee: tradeUsd(0.084), Price: "0.6", ExpiresAtMs: input.ExpiresAtMs}
	other := sale
	other.UserId = 6203
	_, err = SellTradePrediction(position.Id, other)
	require.Error(t, err)
	position, err = SellTradePrediction(position.Id, sale)
	require.NoError(t, err)
	assert.Equal(t, int64(15*100_000_000), position.Qty)
	assert.Equal(t, tradeUsd(7.7625), position.Cost)
	assert.Equal(t, tradeUsd(10.35), position.TotalCost)
	assert.Equal(t, tradeUsd(2.916), position.Payout)
	assert.Equal(t, tradeUsd(22.566), tradeTestAccount(t, 6202).Cash)
	sale.Qty = 16 * 100_000_000
	_, err = SellTradePrediction(position.Id, sale)
	require.ErrorIs(t, err, ErrTradePositionInsufficient)
	assert.Equal(t, tradeUsd(22.566), tradeTestAccount(t, 6202).Cash)
}

func TestTradePredictionRejectsExpiredQuoteAndClosedRound(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300)
	seedTradeFuturesAccount(t, 6204, 100)
	input := predictionTestBuy(6204, round)
	input.ExpiresAtMs = time.Now().Add(-time.Second).UnixMilli()
	_, err := BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradePredictionQuote)
	input.ExpiresAtMs = time.Now().Add(3 * time.Second).UnixMilli()
	require.NoError(t, DB.Model(&TradePredictionRound{}).Where("window_start = ?", round.WindowStart).Update("end_time", common.GetTimestamp()-1).Error)
	_, err = BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradePredictionClosed)
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, 6204).Cash)
	input.Amount = common.MaxQuota - 1
	input.Fee = 2
	_, err = BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradeAmountInvalid)
}

func TestTradePredictionOfficialSettlementIsRecoverableAndIdempotent(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300-300)
	seedTradeFuturesAccount(t, 6205, 20)
	positions := []TradePredictionPosition{
		{UserId: 6205, WindowStart: round.WindowStart, Side: TradePredictionUp, Qty: 20 * 100_000_000, InitialQty: 20 * 100_000_000, Cost: tradeUsd(10), TotalCost: tradeUsd(10), Status: TradePredictionActive},
		{UserId: 6205, WindowStart: round.WindowStart, Side: TradePredictionDown, Qty: 5 * 100_000_000, InitialQty: 5 * 100_000_000, Cost: tradeUsd(3), TotalCost: tradeUsd(3), Status: TradePredictionActive},
	}
	require.NoError(t, DB.Create(&positions).Error)
	require.ErrorIs(t, SettleTradePredictionPosition(positions[0].Id), ErrTradePredictionOutcome)
	assert.Equal(t, tradeUsd(20), tradeTestAccount(t, 6205).Cash)
	require.ErrorIs(t, ResolveTradePredictionRound(round.WindowStart, ""), ErrTradePredictionOutcome)
	require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionUp))
	// 先保存结果，模拟在发钱前重启；从持久化结果恢复，无需进程内赔率缓存。
	require.NoError(t, SettleTradePredictionPosition(positions[0].Id))
	require.NoError(t, SettleTradePredictionPosition(positions[1].Id))
	require.NoError(t, SettleTradePredictionPosition(positions[0].Id))
	require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionUp))
	require.ErrorIs(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionDown), ErrTradePredictionOutcome)
	assert.Equal(t, tradeUsd(40), tradeTestAccount(t, 6205).Cash)
	winner, err := GetTradePredictionPosition(6205, positions[0].Id)
	require.NoError(t, err)
	assert.Equal(t, TradePredictionWon, winner.Status)
	assert.Zero(t, winner.Cost)
	assert.Zero(t, winner.Qty)
	assert.Equal(t, tradeUsd(20), winner.Payout)
	loser, err := GetTradePredictionPosition(6205, positions[1].Id)
	require.NoError(t, err)
	assert.Equal(t, TradePredictionLost, loser.Status)
	assert.Zero(t, loser.Payout)
	var ledgers []TradeLedger
	require.NoError(t, DB.Where("user_id = ? AND type = ?", 6205, TradeLedgerPredictionSettle).Find(&ledgers).Error)
	require.Len(t, ledgers, 2)
	assert.Equal(t, tradeUsd(7), ledgers[0].Pnl+ledgers[1].Pnl)
}

func TestTradePredictionPositionCapAndCreditOverflowRollBack(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300)
	seedTradeFuturesAccount(t, 6206, 100)
	input := predictionTestBuy(6206, round)
	input.MaxCost = tradeUsd(11)
	position, err := BuyTradePrediction(input)
	require.NoError(t, err)
	_, err = BuyTradePrediction(input)
	require.ErrorIs(t, err, ErrTradePositionLimit)
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 6206).Update("cash", common.MaxQuota-1).Error)
	sale := TradePredictionFill{UserId: 6206, WindowStart: round.WindowStart, Side: TradePredictionUp, Qty: position.Qty, Amount: tradeUsd(12), Price: "0.6", ExpiresAtMs: time.Now().Add(3 * time.Second).UnixMilli()}
	_, err = SellTradePrediction(position.Id, sale)
	require.ErrorIs(t, err, ErrTradeAmountInvalid)
	unchanged, err := GetTradePredictionPosition(6206, position.Id)
	require.NoError(t, err)
	assert.Equal(t, position.Qty, unchanged.Qty)
	assert.Zero(t, unchanged.Payout)
}

func TestTradePredictionProfitUsesExistingWithdrawalCap(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300)
	seedTradeFuturesAccount(t, 6207, 10.35)
	position, err := BuyTradePrediction(predictionTestBuy(6207, round))
	require.NoError(t, err)
	_, _, err = TransferQuotaFromTrade(6207, tradeUsd(1), tradeTestDay, tradeUsd(2), nil)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded, "未结算预测份额不能直接转出")
	require.NoError(t, DB.Model(&TradePredictionRound{}).Where("window_start = ?", round.WindowStart).Update("end_time", common.GetTimestamp()-1).Error)
	require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionUp))
	require.NoError(t, SettleTradePredictionPosition(position.Id))
	_, _, err = TransferQuotaFromTrade(6207, tradeUsd(20), tradeTestDay, tradeUsd(2), nil)
	require.ErrorIs(t, err, ErrTradeProfitOutLimit)
	_, profit, err := TransferQuotaFromTrade(6207, tradeUsd(12.35), tradeTestDay, tradeUsd(2), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), profit)
	assert.Equal(t, tradeUsd(7.65), tradeTestAccount(t, 6207).Cash)
}

// Polymarket 判五五开时两边每份都兑付 0.5 美元，持仓不会因为没有赢家而一直挂着。
func TestTradePredictionFiftyFiftyPaysHalfToBothSides(t *testing.T) {
	round := predictionTestRound(t, common.GetTimestamp()/300*300-300)
	seedTradeFuturesAccount(t, 6206, 20)
	positions := []TradePredictionPosition{
		{UserId: 6206, WindowStart: round.WindowStart, Side: TradePredictionUp, Qty: 20 * 100_000_000, InitialQty: 20 * 100_000_000, Cost: tradeUsd(10), TotalCost: tradeUsd(10), Status: TradePredictionActive},
		{UserId: 6206, WindowStart: round.WindowStart, Side: TradePredictionDown, Qty: 6 * 100_000_000, InitialQty: 6 * 100_000_000, Cost: tradeUsd(4), TotalCost: tradeUsd(4), Status: TradePredictionActive},
	}
	require.NoError(t, DB.Create(&positions).Error)
	require.NoError(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionSplit))
	require.ErrorIs(t, ResolveTradePredictionRound(round.WindowStart, TradePredictionUp), ErrTradePredictionOutcome)
	for _, position := range positions {
		require.NoError(t, SettleTradePredictionPosition(position.Id))
	}
	assert.Equal(t, tradeUsd(33), tradeTestAccount(t, 6206).Cash)
	for i, payout := range []int{tradeUsd(10), tradeUsd(3)} {
		settled, err := GetTradePredictionPosition(6206, positions[i].Id)
		require.NoError(t, err)
		assert.Equal(t, TradePredictionHalf, settled.Status)
		assert.Equal(t, payout, settled.Payout)
		assert.Zero(t, settled.Qty)
	}
	var ledgers []TradeLedger
	require.NoError(t, DB.Where("user_id = ? AND type = ?", 6206, TradeLedgerPredictionSettle).Order("id").Find(&ledgers).Error)
	require.Len(t, ledgers, 2)
	assert.Equal(t, "0.5", ledgers[0].Price)
	assert.Equal(t, tradeUsd(-1), ledgers[0].Pnl+ledgers[1].Pnl)
}
