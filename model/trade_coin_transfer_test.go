package model

import (
	"strconv"
	"testing"
	"time"

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
	account, err := TransferGameCoinsToTrade(6100, 6, tradeTestDay)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), account.Cash)
	assert.Equal(t, tradeUsd(10), account.QuotaPrincipal)
	assert.Equal(t, 6, account.CoinPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalIn)

	_, _, err = TransferGameCoinsFromTrade(6100, 7, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeCoinPrincipal, "quota principal must not come back as game coins")
	_, profit, err := TransferGameCoinsFromTrade(6100, 6, tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit, "both deposit currencies contribute to the same principal")
	account, profit, err = TransferQuotaFromTrade(6100, tradeUsd(4), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.QuotaPrincipal)
	assert.Zero(t, account.CoinPrincipal)
	assert.Equal(t, tradeUsd(10), account.TotalOut)
	quota, err := GetUserQuota(6100, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), quota)
	coins, err := GetGameCoinAccount(6100)
	require.NoError(t, err)
	assert.Equal(t, 10, coins.Balance, "each wallet gets back exactly the principal it put in")

	var logs []GameCoinLog
	require.NoError(t, DB.Where("user_id = ?", 6100).Order("id ASC").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, GameCoinLogTypeTradeOut, logs[0].Type)
	assert.Equal(t, -6, logs[0].Amount)
	assert.Equal(t, tradeUsd(6), logs[0].Quota)
	assert.Equal(t, GameCoinLogTypeTradeIn, logs[1].Type)
	assert.Equal(t, 6, logs[1].Amount)
	assert.Equal(t, tradeUsd(6), logs[1].Quota)
	for _, log := range logs {
		ledgerId, err := strconv.Atoi(log.RefId)
		require.NoError(t, err)
		var ledger TradeLedger
		require.NoError(t, DB.First(&ledger, ledgerId).Error)
		assert.Equal(t, log.UserId, ledger.UserId)
		assert.Equal(t, -tradeUsd(float64(log.Amount)), ledger.Amount)
	}
}

// 盈利只能转出成额度：转回游戏币以转入的币数为限，从不算盈利，也不占用每日盈利转出上限。
func TestTradeProfitCanOnlyLeaveAsQuota(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6101, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6101, Balance: 10}).Error)
	_, err := TransferGameCoinsToTrade(6101, 10, tradeTestDay)
	require.NoError(t, err)
	tradeRoundTrip(t, 6101, 10, 115)
	_, _, err = TransferGameCoinsFromTrade(6101, 11, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeCoinPrincipal)
	_, profit, err := TransferGameCoinsFromTrade(6101, 10, tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit)
	used, err := GetTradeProfitOutUsed(6101, tradeTestDay)
	require.NoError(t, err)
	assert.Zero(t, used)
	_, profit, err = TransferQuotaFromTrade(6101, tradeUsd(100), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(100), profit)
	_, _, err = TransferQuotaFromTrade(6101, tradeUsd(1), tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeProfitOutLimit)
	account := tradeTestAccount(t, 6101)
	assert.Equal(t, tradeUsd(5), account.Cash)
	assert.Zero(t, account.CoinPrincipal)
	coins, err := GetGameCoinAccount(6101)
	require.NoError(t, err)
	assert.Equal(t, 10, coins.Balance)
}

// 本金转出成额度后，剩下的本金不够折合的币也不能再转回游戏币：先转出本金、再转入额度，不能把额度换成游戏币。
func TestTradeCoinPrincipalShrinksWithPrincipal(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6106, 4)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6106, Balance: 6}).Error)
	_, err := TransferGameCoinsToTrade(6106, 6, tradeTestDay)
	require.NoError(t, err)
	account, _, err := TransferQuotaFromTrade(6106, tradeUsd(4), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(2), account.QuotaPrincipal)
	assert.Equal(t, 2, account.CoinPrincipal)
	account, err = TransferQuotaToTrade(6106, tradeUsd(8))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), account.QuotaPrincipal)
	assert.Equal(t, 2, account.CoinPrincipal, "quota deposits never add coin principal")
	_, _, err = TransferGameCoinsFromTrade(6106, 3, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrTradeCoinPrincipal)
	account, _, err = TransferGameCoinsFromTrade(6106, 2, tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, account.CoinPrincipal)
	assert.Equal(t, tradeUsd(8), account.Cash)
	coins, err := GetGameCoinAccount(6106)
	require.NoError(t, err)
	assert.Equal(t, 2, coins.Balance)
}

// 转入模拟盘的币可以再转出成额度，和兑换额度共用每人每天的上限；超出时什么都不改，换一天重新计算。
func TestTradeCoinDepositSharesDailyConvertLimit(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6107, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6107, Balance: 40}).Error)
	today := GameCenterDay(time.Now())
	account, err := TransferGameCoinsToTrade(6107, 5, today)
	require.NoError(t, err)
	assert.Equal(t, 5, account.CoinPrincipal)
	used, err := GetGameCoinConvertDayUsed(6107, today)
	require.NoError(t, err)
	assert.Equal(t, 5, used, "the deposit's own ledger entry must not be counted twice")
	before := tradeTestAccount(t, 6107)
	_, err = TransferGameCoinsToTrade(6107, 11, today)
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
	coins, err := GetGameCoinAccount(6107)
	require.NoError(t, err)
	assert.Equal(t, 35, coins.Balance)
	assert.Equal(t, before, tradeTestAccount(t, 6107), "a rejected deposit must not fund the account")
	assert.Equal(t, []string{GameCoinLogTypeTradeOut}, gameCoinLogTypes(t, 6107))

	_, _, err = ConvertGameCoinsToQuota(6107, 10)
	require.NoError(t, err)
	used, err = GetGameCoinConvertDayUsed(6107, today)
	require.NoError(t, err)
	assert.Equal(t, GameCoinConvertDailyLimit, used)
	_, _, err = ConvertGameCoinsToQuota(6107, 1)
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit, "simulator deposits use up the conversion allowance too")

	_, err = TransferGameCoinsToTrade(6107, GameCoinConvertDailyLimit+1, "2026-10-04")
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
	_, err = TransferGameCoinsToTrade(6107, GameCoinConvertDailyLimit, "2026-10-04")
	require.NoError(t, err)
	coins, err = GetGameCoinAccount(6107)
	require.NoError(t, err)
	assert.Equal(t, 10, coins.Balance)
}

func TestTradeCoinWalletFailureRollsBackBothLedgers(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6102, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6102, Balance: 12}).Error)
	_, err := TransferGameCoinsToTrade(6102, 12, tradeTestDay)
	require.NoError(t, err)
	// 钱包余额已到上限，转回的币入不了账。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", 6102).Update("balance", common.MaxQuota-1).Error)
	before := tradeTestAccount(t, 6102)
	_, ledgersBefore, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	_, _, err = TransferGameCoinsFromTrade(6102, 12, tradeTestDay, tradeUsd(100), nil)
	require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	assert.Equal(t, before, tradeTestAccount(t, 6102), "cash and coin principal must roll back together")
	coins, err := GetGameCoinAccount(6102)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota-1, coins.Balance)
	_, ledgersAfter, err := GetTradeLedgers(6102, "", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, ledgersBefore, ledgersAfter)
	assert.Equal(t, []string{GameCoinLogTypeTradeOut}, gameCoinLogTypes(t, 6102))
	_, profit, err := TransferQuotaFromTrade(6102, tradeUsd(12), tradeTestDay, tradeUsd(100), nil)
	require.NoError(t, err)
	assert.Zero(t, profit)
}

func TestTradeCoinTransferRejectsUnfundedOrInvalidDeposits(t *testing.T) {
	truncateTables(t)
	seedTradeUser(t, 6103, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 6103, Balance: 2}).Error)
	_, err := TransferGameCoinsToTrade(6103, 3, tradeTestDay)
	require.ErrorIs(t, err, ErrGameCoinInsufficient)
	for _, amount := range []int{-1, 0, common.MaxQuota} {
		_, err = TransferGameCoinsToTrade(6103, amount, tradeTestDay)
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
	// 这 100 美元本金按游戏币转入记，只看全仓保证金与浮动盈利的限制。
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 6104).Update("coin_principal", 100).Error)
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
	_, err = TransferGameCoinsToTrade(6105, 5, tradeTestDay)
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
