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

	// 3. 在下注游戏下注 4 币赢回 6 币：流水锁从 10 降到 6；扣除的 4 币先消耗不可提现余额，赢回的 6 币属于游戏产出，计入可提现余额。
	_, err = SettleGameWager(userId, "dice", GameWager{
		Bet: coin(4), Day: tradeTestDay, Ip: "1.1.1.1",
		Play: func() (GameCoins, string, error) { return coin(6), "{}", nil },
	})
	require.NoError(t, err)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(12), account.Balance)
	assert.Equal(t, coin(6), account.LockedWager)
	assert.Equal(t, coin(6), account.WithdrawableBalance, "wager payout becomes on-chain withdrawable")

	// 4. 后续继续投入 3 币并输掉：优先消耗充入的不可提现余额(12-6=6 够扣 3)，已产出的 6 币可提现余额不变。
	_, err = SettleGameWager(userId, "dice", GameWager{
		Bet: coin(3), Day: tradeTestDay, Ip: "1.1.1.1",
		Play: func() (GameCoins, string, error) { return 0, "{}", nil },
	})
	require.NoError(t, err)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(9), account.Balance)
	assert.Equal(t, coin(6), account.WithdrawableBalance, "subsequent wagers must consume non-withdrawable coins first")

	// 5. 转入 3 币到模拟盘并完成一笔 6 美元现货买入成交(100% 计入流水)：剩余 3 币流水锁清零，且转入模拟盘也优先消耗不可提现部分。
	_, err = TransferGameCoinsToTrade(userId, coin(3), tradeCashEquity(t, userId))
	require.NoError(t, err)
	tradeRoundTrip(t, userId, 3, 6)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Zero(t, account.LockedWager, "simulator spot fills reduce locked wager")
	assert.Equal(t, coin(6), account.WithdrawableBalance)

	// 6. 此时 6 币可提现余额可直接发起链上提现，且流水解锁后也可将游戏币换回平台额度。
	_, bal, err = ConvertGameCoinsToQuota(userId, coin(6))
	require.NoError(t, err)
	assert.Zero(t, bal)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Zero(t, account.WithdrawableBalance)
}

// 验证多步下注游戏、奖池与 PVP 对战产出的提现权限，以及退款/平局不洗白不可提现余额。
func TestGameCoinGameOutputsBecomeWithdrawableWhileRefundsDoNot(t *testing.T) {
	truncateTables(t)
	prevGame := operation_setting.GetGameSetting()
	setting := operation_setting.DefaultGameSetting()
	setting.Enabled = true
	setting.DepositEnabled = true
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(prevGame) })

	const userId = 6604
	const rivalId = 6605
	seedTradeUser(t, userId, 100)
	seedTradeUser(t, rivalId, 100)

	_, _, err := DepositQuotaToGameCoins(userId, coin(30))
	require.NoError(t, err)
	_, _, err = DepositQuotaToGameCoins(rivalId, coin(30))
	require.NoError(t, err)

	// 1. 多步下注游戏(如挖宝/21点)：投入 10 赢回 15 -> 15 变为可提现，余额 35 中还有 20 不可提现。
	roundSession, _, err := OpenGameRound(userId, "mines", GameRound{Bet: coin(10), Ip: "1.1.1.1", State: "{}"})
	require.NoError(t, err)
	_, err = SettleGameRound(roundSession, GameRoundSettlement{Win: coin(15), State: "{}", Detail: "{}", Day: tradeTestDay})
	require.NoError(t, err)
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(35), account.Balance)
	assert.Equal(t, coin(15), account.WithdrawableBalance)

	// 2. PVP 取消与平局退款不洗白不可提现余额，只有获胜派彩计入可提现。
	roomCancel := &GameRoom{Game: "rps", Stake: coin(5), Escrow: coin(5), Seats: 2, Code: "c1", HostId: userId, RakePercent: 5, MoveSeconds: 20}
	_, err = CreateGameRoom(roomCancel, "1.1.1.1")
	require.NoError(t, err)
	require.NoError(t, CancelGameRoom(roomCancel, GameRoomEndCancel))
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(15), account.WithdrawableBalance, "cancelling a PVP room must not convert deposited coins into withdrawable")

	roomDraw := &GameRoom{Game: "rps", Stake: coin(5), Escrow: coin(5), Seats: 2, Code: "d1", HostId: userId, RakePercent: 5, MoveSeconds: 20}
	_, err = CreateGameRoom(roomDraw, "1.1.1.1")
	require.NoError(t, err)
	_, err = JoinGameRoom(roomDraw, rivalId, "2.2.2.2", startTestGameRoom)
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(roomDraw, GameRoomResult{WinnerSeat: -1, Nets: []GameCoins{0, 0}}, GameRoomEndDraw, "{}", tradeTestDay))
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(15), account.WithdrawableBalance, "PVP draw refund must not convert deposited coins into withdrawable")

	roomWin := &GameRoom{Game: "rps", Stake: coin(10), Escrow: coin(10), Seats: 2, Code: "w1", HostId: userId, RakePercent: 5, MoveSeconds: 20}
	_, err = CreateGameRoom(roomWin, "1.1.1.1")
	require.NoError(t, err)
	_, err = JoinGameRoom(roomWin, rivalId, "2.2.2.2", startTestGameRoom)
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(roomWin, GameRoomResult{WinnerSeat: 0, Winners: []int{0}, Nets: []GameCoins{coin(10), -coin(10)}}, GameRoomEndWin, "{}", tradeTestDay))
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	// 双人 10 币场抽水 5% = 1 币，赢家拿到 19 币全部为游戏产出。
	assert.Equal(t, coin(44), account.Balance)
	assert.Equal(t, coin(34), account.WithdrawableBalance)

	// 3. 奖池：单人超时退款不增加可提现，多人开奖赢家的派彩增加可提现。
	jpRule := GameJackpotEntryRule{MaxBet: coin(100), RoundSeconds: 60, RakePercent: 5}
	_, _, err = EnterGameJackpot(userId, coin(5), jpRule, "1.1.1.1")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&GameJackpotRound{}).Where("status = ?", GameJackpotStatusOpen).Update("created_at", common.GetTimestamp()-3600).Error)
	_, err = RefundStaleGameJackpot(common.GetTimestamp() - 60)
	require.NoError(t, err)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(34), account.WithdrawableBalance, "stale jackpot refund must not convert deposited coins into withdrawable")

	round, _, err := EnterGameJackpot(userId, coin(10), jpRule, "1.1.1.1")
	require.NoError(t, err)
	_, _, err = EnterGameJackpot(rivalId, coin(10), jpRule, "2.2.2.2")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Update("deadline", common.GetTimestamp()-1).Error)
	drawn, err := DrawGameJackpot(func(int) (int, error) { return 0, nil }, tradeTestDay)
	require.NoError(t, err)
	require.NotNil(t, drawn)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	// 投入 10(优先扣了剩余 10 不可提现余额)，赢得 19 币派彩。
	assert.Equal(t, coin(53), account.Balance)
	assert.Equal(t, coin(53), account.WithdrawableBalance)

	// 4. 置换后可提现，提现后剩余逻辑正确。
	_, err = BindGameWithdrawAddress(userId, "0x1111111111111111111111111111111111111111")
	require.NoError(t, err)
	bal, err := CreateGameCoinWithdrawal(&GameCoinWithdrawal{
		UserId: userId, ToAddress: "0x1111111111111111111111111111111111111111", Amount: 50, Day: tradeTestDay,
	}, 100)
	require.NoError(t, err)
	assert.Equal(t, coin(3), bal)
	account, err = GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, coin(3), account.WithdrawableBalance)
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
