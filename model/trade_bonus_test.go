package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableTradeBonusForTest(t *testing.T, mutate func(*operation_setting.TradeSetting)) {
	t.Helper()
	prev := operation_setting.GetTradeSetting()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled = true
	setting.BonusEnabled = true
	setting.BonusPoolDailyUsd = 1000
	setting.BonusFirstAmountUsd = 100
	setting.BonusFirstProfitCapUsd = 20
	setting.BonusMatchEnabled = true
	setting.BonusMatchPeriod = operation_setting.TradeBonusPeriodWeekly
	setting.BonusMatchMaxUsd = 50
	setting.BonusTaskWagerCoins = 5
	if mutate != nil {
		mutate(setting)
	}
	operation_setting.SetTradeSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(prev) })
}

func TestTradeFirstBonusClaimProfitCapAndReclaim(t *testing.T) {
	truncateTables(t)
	enableTradeBonusForTest(t, func(s *operation_setting.TradeSetting) {
		s.BonusPoolDailyUsd = 150
	})

	const userId = 6701
	const otherSameIp = 6702
	const otherDiffIp = 6703
	seedTradeUser(t, userId, 0)
	seedTradeUser(t, otherSameIp, 0)
	seedTradeUser(t, otherDiffIp, 0)

	// 首次领取 100 USDT 体验金。
	account, granted, err := ClaimTradeFirstBonus(userId, "10.0.0.1", tradeTestDay)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(100), granted)
	assert.Equal(t, tradeUsd(100), account.Cash)
	assert.Equal(t, tradeUsd(100), account.BonusPrincipal)
	assert.Equal(t, TradeBonusModeFirst, account.BonusMode)

	// 同一账号不能重领；同 IP 其他账号不能领；全局池仅剩 50U 时第 3 人领 100U 被拒。
	_, _, err = ClaimTradeFirstBonus(userId, "10.0.0.1", tradeTestDay)
	require.ErrorIs(t, err, ErrTradeBonusAccountNotEmpty)
	_, _, err = ClaimTradeFirstBonus(otherSameIp, "10.0.0.1", tradeTestDay)
	require.ErrorIs(t, err, ErrTradeBonusIpLimit)
	_, _, err = ClaimTradeFirstBonus(otherDiffIp, "10.0.0.2", tradeTestDay)
	require.ErrorIs(t, err, ErrTradeBonusPoolExhausted)

	// 持有未结清体验金时不能混充自有资金。
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", tradeUsd(50)).Error)
	_, err = TransferQuotaToTrade(userId, tradeUsd(10), tradeCashEquity(t, userId))
	require.ErrorIs(t, err, ErrTradeBonusActiveExists)

	// 纯体验金交易赚 35U(总现金 135U)，且不会错误消减游戏币流水锁。
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: userId, Balance: coin(5), LockedWager: coin(15)}).Error)
	tradeRoundTrip(t, userId, 100, 135)
	coinAcc, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(15), coinAcc.LockedWager, "first bonus trades must not unlock game coin wager lock")

	// 超出首次盈利上限 20U 被拒绝；转出 20U 盈利时立即回收剩余 100U 体验金与超额 15U，防止二次绕过。
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(21), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.ErrorIs(t, err, ErrTradeBonusProfitCap)
	account, profit, err := TransferQuotaFromTrade(userId, tradeUsd(20), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(20), profit)
	assert.Zero(t, account.Cash)
	assert.Zero(t, account.BonusPrincipal)
	assert.Empty(t, account.BonusMode)
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(70), quota)
}

func TestTradeMatchBonusTaskLockLossOrderReclaimAndZeroDeficit(t *testing.T) {
	truncateTables(t)
	enableTradeBonusForTest(t, nil)

	const userId = 6710
	seedTradeUser(t, userId, 100)

	// 1. 未完成游戏中心 5 币有效下注任务时，1:1 加赠被拒。
	_, _, err := TransferQuotaToTradeWithOptions(userId, tradeUsd(50), tradeCashEquity(t, userId), TradeTransferInOptions{
		ClaimMatch: true, Ip: "10.0.0.9", Day: tradeTestDay,
	})
	require.ErrorIs(t, err, ErrTradeBonusTaskIncomplete)

	// 2. 在游戏中心完成 5 币有效下注，解锁 1:1 体验金加赠(封顶 50U)。
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: userId, Balance: coin(10)}).Error)
	_, err = SettleGameWager(userId, "dice", GameWager{
		Bet: coin(5), Day: tradeTestDay, Ip: "10.0.0.9",
		Play: func() (GameCoins, string, error) { return coin(5), "{}", nil },
	})
	require.NoError(t, err)

	account, bonusGranted, err := TransferQuotaToTradeWithOptions(userId, tradeUsd(60), tradeCashEquity(t, userId), TradeTransferInOptions{
		ClaimMatch: true, Ip: "10.0.0.9", Day: tradeTestDay,
	})
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(50), bonusGranted, "1:1 match capped at bonus_match_max_usd")
	assert.Equal(t, tradeUsd(110), account.Cash)
	assert.Equal(t, tradeUsd(60), account.QuotaPrincipal)
	assert.Equal(t, tradeUsd(50), account.BonusPrincipal)
	assert.Equal(t, TradeBonusModeMatch, account.BonusMode)

	// 3. 亏损 40U(现金剩 70U)：亏损先扣自有本金，可转出上限 = 70 - 50 = 20U。
	tradeRoundTrip(t, userId, 100, 60)
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(21), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.ErrorIs(t, err, ErrTradeWithdrawExceeded)
	account, profit, err := TransferQuotaFromTrade(userId, tradeUsd(20), tradeTestDay, tradeUsd(100), nil, tradeCashEquity(t, userId))
	require.NoError(t, err)
	assert.Zero(t, profit)
	assert.Zero(t, account.Cash, "withdrawing reclaims the remaining 50U bonus immediately")
	assert.Zero(t, account.BonusPrincipal)

	// 4. 体验金保护区间穿仓：直接归零结清，绝不倒扣用户主余额或游戏币。
	const userId2 = 6711
	seedTradeUser(t, userId2, 20)
	_, _, err = ClaimTradeFirstBonus(userId2, "10.0.0.11", tradeTestDay)
	require.NoError(t, err)
	openTradeFutures(t, userId2, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	// 手动消耗掉多余现金模拟满仓强平跳空穿仓。
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", userId2).Update("cash", 0).Error)
	liquidated, err := LiquidateTradeFuturesPosition(userId2, tradeFuturesSymbol, TradeFuturesLong, decimal.NewFromInt(80),
		tradeTestMarket{}.Brackets(tradeFuturesSymbol), 0)
	require.NoError(t, err)
	require.True(t, liquidated)
	after := tradeTestAccount(t, userId2)
	assert.Zero(t, after.Cash)
	assert.Zero(t, after.BonusPrincipal)
	user2Quota, err := GetUserQuota(userId2, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(20), user2Quota, "bonus liquidation deficit must never charge user's main quota")
	_ = common.MaxQuota
}
