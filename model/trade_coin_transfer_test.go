package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeCoinAndQuotaPrincipalShareOneCashPool(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6100, 10)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6100, Balance: 10}).Error)
	_, err := TransferQuotaToTrade(6100, tradeUsd(4))
	require.NoError(t, err)
	account, err := TransferGameCoinsToTrade(6100, 6)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), account.Cash)
	assert.Equal(t, tradeUsd(10), account.QuotaPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalIn)

	_, profit, err := TransferGameCoinsFromTrade(6100, 7, tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit, "both deposit currencies contribute to the same principal")
	account, profit, err = TransferQuotaFromTrade(6100, tradeUsd(3), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.QuotaPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalOut)
	quota, err := GetUserQuota(6100, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(9), quota)
	coins, err := GetGameCoinAccount(6100)
	require.NoError(t, err)
	assert.Equal(t, 11, coins.Balance)
	assert.Equal(t, tradeUsd(20), quota+tradeUsd(float64(coins.Balance)), "principal roundtrip must conserve value across both wallets")

	var logs []GameCoinLog
	require.NoError(t, DB.Where("user_id = ?", 6100).Order("id ASC").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, GameCoinLogTypeTradeOut, logs[0].Type)
	assert.Equal(t, -6, logs[0].Amount)
	assert.Equal(t, tradeUsd(6), logs[0].Quota)
	assert.Equal(t, GameCoinLogTypeTradeIn, logs[1].Type)
	assert.Equal(t, 7, logs[1].Amount)
	assert.Equal(t, tradeUsd(7), logs[1].Quota)
	for _, log := range logs {
		ledgerId, err := strconv.Atoi(log.RefId)
		require.NoError(t, err)
		var ledger TradeLedger
		require.NoError(t, DB.First(&ledger, ledgerId).Error)
		assert.Equal(t, log.UserId, ledger.UserId)
		assert.Equal(t, -tradeUsd(float64(log.Amount)), ledger.Amount)
	}
}

func TestTradeCoinAndQuotaWithdrawalsShareDailyProfitLimit(t *testing.T) {
	for _, firstWallet := range []string{"quota", "coin"} {
		t.Run(firstWallet, func(t *testing.T) {
			truncateTables(t)
			seedTradeUser(t, 6101, 10)
			_, err := TransferQuotaToTrade(6101, tradeUsd(10))
			require.NoError(t, err)
			tradeRoundTrip(t, 6101, 10, 115)
			var profit int
			if firstWallet == "quota" {
				_, profit, err = TransferQuotaFromTrade(6101, tradeUsd(110), tradeTestDay, tradeUsd(100), nil)
			} else {
				_, profit, err = TransferGameCoinsFromTrade(6101, 110, tradeTestDay, tradeUsd(100), nil)
			}
			require.NoError(t, err)
			assert.Equal(t, tradeUsd(100), profit)
			if firstWallet == "quota" {
				_, _, err = TransferGameCoinsFromTrade(6101, 1, tradeTestDay, tradeUsd(100), nil)
			} else {
				_, _, err = TransferQuotaFromTrade(6101, tradeUsd(1), tradeTestDay, tradeUsd(100), nil)
			}
			require.ErrorIs(t, err, ErrTradeProfitOutLimit, "switching wallets must not reset the profit cap")
			assert.Equal(t, tradeUsd(5), tradeTestAccount(t, 6101).Cash)
			used, err := GetTradeProfitOutUsed(6101, tradeTestDay)
			require.NoError(t, err)
			assert.Equal(t, tradeUsd(100), used)
			if firstWallet == "quota" {
				_, profit, err = TransferGameCoinsFromTrade(6101, 1, "2026-10-04", tradeUsd(100), nil)
			} else {
				_, profit, err = TransferQuotaFromTrade(6101, tradeUsd(1), "2026-10-04", tradeUsd(100), nil)
			}
			require.NoError(t, err)
			assert.Equal(t, tradeUsd(1), profit)
		})
	}
}

func TestTradeCoinWalletFailureRollsBackBothLedgersAndProfitLimit(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6102, 10)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6102, Balance: common.MaxQuota - 1}).Error)
	_, err := TransferQuotaToTrade(6102, tradeUsd(10))
	require.NoError(t, err)
	tradeRoundTrip(t, 6102, 10, 12)
	before := tradeTestAccount(t, 6102)
	_, ledgersBefore, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	_, _, err = TransferGameCoinsFromTrade(6102, 12, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	assert.Equal(t, before, tradeTestAccount(t, 6102))
	coins, err := GetGameCoinAccount(6102)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota-1, coins.Balance)
	used, err := GetTradeProfitOutUsed(6102, tradeTestDay)
	require.NoError(t, err)
	assert.Zero(t, used, "failed receiving wallet credit must release the daily profit allowance")
	_, ledgersAfter, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, ledgersBefore, ledgersAfter)
	var logCount int64
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ?", 6102).Count(&logCount).Error)
	assert.Zero(t, logCount)
	_, profit, err := TransferQuotaFromTrade(6102, tradeUsd(12), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), profit)
}

func TestTradeCoinTransferRejectsUnfundedOrInvalidDeposits(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6103, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6103, Balance: 2}).Error)
	_, err := TransferGameCoinsToTrade(6103, 3)
	require.ErrorIs(t, err, ErrGameCoinInsufficient)
	for _, amount := range []int{-1, 0, common.MaxQuota} {
		_, err = TransferGameCoinsToTrade(6103, amount)
		require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
		_, _, err = TransferGameCoinsFromTrade(6103, amount, tradeTestDay, tradeUsd(100), nil)
		require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	}
	account := tradeTestAccount(t, 6103)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.TotalIn)
	coins, err := GetGameCoinAccount(6103)
	require.NoError(t, err)
	assert.Equal(t, 2, coins.Balance)
	_, total, err := GetTradeLedgers(6103, "", 0, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	var logCount int64
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ?", 6103).Count(&logCount).Error)
	assert.Zero(t, logCount)
}

func TestTradeCoinWithdrawalPreservesCrossMarginAndExcludesFloatingProfit(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 6104, 100)
	market := tradeTestMarket{tradeFuturesSymbol: "500"}
	openTradeFutures(t, 6104, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "500", 10, 0, market)
	_, _, err := TransferGameCoinsFromTrade(6104, 51, tradeTestDay, tradeUsd(100), market)
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	account, profit, err := TransferGameCoinsFromTrade(6104, 50, tradeTestDay, tradeUsd(100), market)
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Equal(t, tradeUsd(50), account.Cash)
	_, _, err = TransferGameCoinsFromTrade(6104, 1, tradeTestDay, tradeUsd(100), tradeTestMarket{tradeFuturesSymbol: "550"})
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	_, _, err = TransferGameCoinsFromTrade(6104, 1, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeMarkUnavailable)
}

func TestTradeCoinFundingCanRepayNegativeSiteQuota(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 6105, 10)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6105, Balance: 5}).Error)
	openTradeFutures(t, 6105, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	liquidated, err := LiquidateTradeFuturesPosition(6105, tradeFuturesSymbol, TradeFuturesLong, decimal.NewFromInt(80),
		tradeTestMarket{}.Brackets(tradeFuturesSymbol), 0)
	require.NoError(t, err)
	require.True(t, liquidated)
	quota, err := GetUserQuota(6105, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(10), quota)
	_, err = TransferGameCoinsToTrade(6105, 5)
	require.NoError(t, err)
	_, profit, err := TransferQuotaFromTrade(6105, tradeUsd(5), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err, "negative site quota must not block a transfer that repays it")
	assert.Zero(t, profit)
	quota, err = GetUserQuota(6105, true)
	require.NoError(t, err)
	assert.Equal(t, -tradeUsd(5), quota)
	coins, err := GetGameCoinAccount(6105)
	require.NoError(t, err)
	assert.Zero(t, coins.Balance)
}
