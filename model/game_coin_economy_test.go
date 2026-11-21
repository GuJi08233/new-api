package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验证余额充游戏币、免费奖励流水锁、下注/模拟盘消减流水、归零免债与链上可提现额度隔离。
func TestGameCoinDepositWagerLockAndWithdrawableIsolation(t *testing.T) {
	truncateTables(t)
	prevGame := operation_setting.GetGameSetting()
	setting := operation_setting.DefaultGameSetting()
	setting.Enabled = true
	setting.DepositEnabled = true
	setting.ConvertEnabled = true
	setting.ConvertDailyLimit = 50
	setting.DepositWagerMultiplier = 1
	setting.FreeWagerMultiplier = 3
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(prevGame) })

	const userId = 6601
	seedTradeUser(t, userId, 20)

	// 1. 平台余额充入 10 游戏币：扣主余额、增加 1x 流水锁，但绝不增加链上可提现额度。
	bal, locked, err := DepositQuotaToGameCoins(userId, coin(10))
	require.NoError(t, err)
	assert.Equal(t, coin(10), bal)
	assert.Equal(t, coin(10), locked)
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Zero(t, account.WithdrawableBalance, "quota deposit must never widen on-chain withdrawable balance")
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(10), quota)

	// 2. 流水未解锁前不能换回平台额度，也不能链上提现。
	_, _, err = ConvertGameCoinsToQuota(userId, coin(1))
	require.ErrorIs(t, err, ErrGameCoinLockedWager)
	_, err = BindGameWithdrawAddress(userId, "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	_, err = CreateGameCoinWithdrawal(&GameCoinWithdrawal{
		UserId: userId, ToAddress: "0x1111111111111111111111111111111111111111", Amount: 1, Day: tradeTestDay,
	}, 100)
	require.ErrorIs(t, err, ErrGameCoinWithdrawableInsufficient)

	// 3. 在下注游戏下注 4 币赢回 6 币：流水锁从 10 降到 6，且赢钱增量同样不增加链上可提现额度。
	_, err = SettleGameWager(userId, "dice", GameWager{
		Bet: coin(4), Day: tradeTestDay, Ip: "1.1.1.1",
		Play: func() (GameCoins, string, error) { return coin(6), "{}", nil },
	})
	require.NoError(t, err)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(12), account.Balance)
	assert.Equal(t, coin(6), account.LockedWager)
	assert.Zero(t, account.WithdrawableBalance, "wager wins must not become on-chain withdrawable")

	// 4. 转入 6 币到模拟盘并完成一笔 6 美元现货买入成交(100% 计入流水)：剩余 6 币流水锁清零！
	_, err = TransferGameCoinsToTrade(userId, coin(6), tradeCashEquity(t, userId))
	require.NoError(t, err)
	tradeRoundTrip(t, userId, 6, 6)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Zero(t, account.LockedWager, "simulator spot fills reduce locked wager")

	// 5. 流水解锁后可将游戏币余额换回平台额度。
	convertedQuota, bal, err := ConvertGameCoinsToQuota(userId, coin(6))
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(6), convertedQuota)
	assert.Zero(t, bal)
}

func TestGameCoinLockedWagerZeroResetsOnlyWithoutOpenExposure(t *testing.T) {
	truncateTables(t)
	prevGame := operation_setting.GetGameSetting()
	setting := operation_setting.DefaultGameSetting()
	setting.Enabled = true
	setting.DepositEnabled = true
	setting.DepositWagerMultiplier = 3
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(prevGame) })

	const userId = 6602
	seedTradeUser(t, userId, 10)
	_, _, err := DepositQuotaToGameCoins(userId, coin(5))
	require.NoError(t, err)

	// 全部押入分步下注游戏(余额为 0 但还有进行中对局)，此时不能清零流水锁。
	session, bal, err := OpenGameRound(userId, "blackjack", GameRound{Bet: coin(5), Ip: "1.1.1.1", State: "{}"})
	require.NoError(t, err)
	assert.Zero(t, bal)
	require.NoError(t, resetGameCoinLockedWagerIfEmptyTx(DB, userId))
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(15), account.LockedWager, "in-flight staked round must preserve locked wager")

	// 结算输光后(余额 0 且无在途对局与模拟盘本金)，自动免债清零。
	_, err = SettleGameRound(session, GameRoundSettlement{Win: 0, State: "{}", Detail: "{}", Day: tradeTestDay})
	require.NoError(t, err)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Zero(t, account.Balance)
	assert.Zero(t, account.LockedWager, "bankrupted account clears locked wager")
}

func TestGameCoinWithdrawableMigrationBackfillsLegacyBalances(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Where(commonKeyCol+" = ?", gameCoinWithdrawableOption).Delete(&Option{}).Error)
	const userId = 6603
	seedTradeUser(t, userId, 0)
	require.NoError(t, DB.Create(&GameCoinAccount{
		UserId:              userId,
		Balance:             coin(25),
		WithdrawableBalance: 0,
	}).Error)
	require.NoError(t, migrateGameCoinWithdrawable())
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(25), account.WithdrawableBalance)

	// 二次执行幂等，不会覆盖后续新充入的不可提现余额。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", userId).Updates(map[string]any{
		"balance":              coin(40),
		"withdrawable_balance": coin(25),
	}).Error)
	require.NoError(t, migrateGameCoinWithdrawable())
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(25), account.WithdrawableBalance)
	_ = common.MaxQuota
}
