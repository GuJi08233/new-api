package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/blackjack"
	"github.com/QuantumNous/new-api/pkg/roulette"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGameCoinMigrationTest 清空游戏币数据与迁移标记，配置表里 1 个游戏币是 quotaPerUnit 个额度单位。
func setupGameCoinMigrationTest(t *testing.T, quotaPerUnit string) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Where(commonKeyCol+" IN ?", []string{gameCoinUnitsOption, "QuotaPerUnit"}).Delete(&Option{}).Error)
	t.Cleanup(func() {
		DB.Where(commonKeyCol+" IN ?", []string{gameCoinUnitsOption, "QuotaPerUnit"}).Delete(&Option{})
	})
	require.NoError(t, DB.Create(&Option{Key: "QuotaPerUnit", Value: quotaPerUnit}).Error)
}

// 按整币记账的旧数据换算成额度单位：每一列乘以配置表里的 QuotaPerUnit(迁移在加载配置之前运行，不能用内存里的默认值)，下注游戏
// 的成绩跟着换算，技巧游戏的成绩是分数不动；提现单补上扣下的单位，已经有的不动；还没结束的 21 点每一手的下注与轮盘注单的金额
// 一起换算。换算只做一次，再次启动不会再乘。
func TestMigrateGameCoinUnitsScalesLegacyWholeCoins(t *testing.T) {
	setupGameCoinMigrationTest(t, "1000")
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 7001, Balance: 12, TotalEarned: 30, TotalConverted: 5, TotalWithdrawn: 3,
		TotalWagerNet: -7, TotalWagered: 40}).Error)
	require.NoError(t, DB.Create(&GameCoinLog{UserId: 7001, Type: GameCoinLogTypeConvert, Amount: -5, Balance: 12, Quota: 2_500_000}).Error)
	require.NoError(t, DB.Create(&GameCoinConvertDay{UserId: 7001, Day: "2026-10-01", Used: 5}).Error)
	require.NoError(t, DB.Create(&GameRewardDay{UserId: 7001, Game: "tetris", Day: "2026-10-01", Coins: 1, Plays: 4, PendingReward: 2}).Error)
	require.NoError(t, DB.Create(&GameRewardIpDay{Ip: "203.0.113.7", Game: "tetris", Day: "2026-10-01", Coins: 1, Plays: 4}).Error)
	dice := GameSession{UserId: 7001, Game: "dice", Status: GameSessionStatusFinished, Bet: 2, Score: 4, Reward: 4}
	tetris := GameSession{UserId: 7001, Game: "tetris", Status: GameSessionStatusFinished, Score: 5000, Reward: 1}
	dealt, err := blackjack.Deal([]int{9, 5, 4, 9}, 10)
	require.NoError(t, err)
	dealtState, err := common.Marshal(dealt)
	require.NoError(t, err)
	playing := GameSession{UserId: 7001, Game: "blackjack", Status: GameSessionStatusPlaying, Bet: 10, State: string(dealtState)}
	slip, err := common.Marshal([]roulette.Bet{{Type: roulette.Red, Amount: 3}, {Type: roulette.Straight, Value: 7, Amount: 2}})
	require.NoError(t, err)
	spun := GameSession{UserId: 7001, Game: "roulette", Status: GameSessionStatusFinished, Bet: 5, Score: 0, State: string(slip)}
	for _, session := range []*GameSession{&dice, &tetris, &playing, &spun} {
		require.NoError(t, DB.Create(session).Error)
	}
	room := GameRoom{Game: "gomoku", Status: GameRoomStatusFinished, Stake: 5, Escrow: 5, Seats: 2, Rake: 1}
	require.NoError(t, DB.Create(&room).Error)
	require.NoError(t, DB.Create(&GameRoomSeat{RoomId: room.Id, Seat: 0, UserId: 7001, Net: -5}).Error)
	round := GameJackpotRound{Slot: "drawn-1", Status: GameJackpotStatusDrawn, Pot: 35, Payout: 32, Rake: 3, Ticket: 12, WinnerStake: 25}
	require.NoError(t, DB.Create(&round).Error)
	require.NoError(t, DB.Create(&GameJackpotEntry{RoundId: round.Id, UserId: 7001, Amount: 10}).Error)
	require.NoError(t, DB.Create(&TradeAccount{UserId: 7001, Cash: 6000, QuotaPrincipal: 6000, CoinPrincipal: 6}).Error)
	legacy := GameCoinWithdrawal{TradeNo: "GCW-LEGACY", UserId: 7001, Amount: 3, Status: GameCoinWithdrawalStatusConfirmed}
	recent := GameCoinWithdrawal{TradeNo: "GCW-RECENT", UserId: 7001, Amount: 2, Units: 2000, Status: GameCoinWithdrawalStatusPending}
	require.NoError(t, DB.Create(&legacy).Error)
	require.NoError(t, DB.Create(&recent).Error)

	for range 2 {
		require.NoError(t, migrateGameCoinUnits())
	}

	account, err := GetGameCoinAccount(7001)
	require.NoError(t, err)
	assert.Equal(t, []GameCoins{12_000, 30_000, 5_000, 3_000, -7_000, 40_000},
		[]GameCoins{account.Balance, account.TotalEarned, account.TotalConverted, account.TotalWithdrawn, account.TotalWagerNet, account.TotalWagered})
	var log GameCoinLog
	require.NoError(t, DB.Where("user_id = ?", 7001).First(&log).Error)
	assert.Equal(t, []GameCoins{-5_000, 12_000}, []GameCoins{log.Amount, log.Balance})
	assert.Equal(t, 2_500_000, log.Quota, "the quota side was already in quota units")
	used, err := GetGameCoinConvertDayUsed(7001, "2026-10-01")
	require.NoError(t, err)
	assert.Equal(t, GameCoins(5_000), used)
	var day GameRewardDay
	require.NoError(t, DB.Where("user_id = ?", 7001).First(&day).Error)
	assert.Equal(t, []GameCoins{1_000, 2_000}, []GameCoins{day.Coins, day.PendingReward})
	assert.Equal(t, 4, day.Plays)
	var ipDay GameRewardIpDay
	require.NoError(t, DB.Where("ip = ?", "203.0.113.7").First(&ipDay).Error)
	assert.Equal(t, GameCoins(1_000), ipDay.Coins)

	sessions := map[int]GameSession{}
	var stored []GameSession
	require.NoError(t, DB.Where("user_id = ?", 7001).Find(&stored).Error)
	for _, session := range stored {
		sessions[session.Id] = session
	}
	assert.Equal(t, []any{GameCoins(2_000), 4_000, GameCoins(4_000)}, []any{sessions[dice.Id].Bet, sessions[dice.Id].Score, sessions[dice.Id].Reward})
	assert.Equal(t, []any{5000, GameCoins(1_000)}, []any{sessions[tetris.Id].Score, sessions[tetris.Id].Reward}, "skill scores are not coins")
	var hand blackjack.Game
	require.NoError(t, common.UnmarshalJsonStr(sessions[playing.Id].State, &hand))
	assert.Equal(t, 10_000, hand.Hands[0].Bet)
	assert.Equal(t, GameCoins(10_000), sessions[playing.Id].Bet)
	var bets []roulette.Bet
	require.NoError(t, common.UnmarshalJsonStr(sessions[spun.Id].State, &bets))
	assert.Equal(t, []int{3_000, 2_000}, []int{bets[0].Amount, bets[1].Amount})

	storedRoom, err := GetGameRoom(room.Id)
	require.NoError(t, err)
	assert.Equal(t, []GameCoins{5_000, 5_000, 1_000}, []GameCoins{storedRoom.Stake, storedRoom.Escrow, storedRoom.Rake})
	seats, err := GetGameRoomSeats([]int{room.Id})
	require.NoError(t, err)
	assert.Equal(t, GameCoins(-5_000), seats[room.Id][0].Net)
	var storedRound GameJackpotRound
	require.NoError(t, DB.First(&storedRound, round.Id).Error)
	assert.Equal(t, []GameCoins{35_000, 32_000, 3_000, 12_000, 25_000},
		[]GameCoins{storedRound.Pot, storedRound.Payout, storedRound.Rake, storedRound.Ticket, storedRound.WinnerStake})
	var entry GameJackpotEntry
	require.NoError(t, DB.Where("round_id = ?", round.Id).First(&entry).Error)
	assert.Equal(t, GameCoins(10_000), entry.Amount)
	trade := tradeTestAccount(t, 7001)
	assert.Equal(t, GameCoins(6_000), trade.CoinPrincipal)
	assert.Equal(t, 6000, trade.QuotaPrincipal, "trade principal was already in quota units")

	var withdrawals []GameCoinWithdrawal
	require.NoError(t, DB.Where("user_id = ?", 7001).Order("id").Find(&withdrawals).Error)
	assert.Equal(t, []GameCoins{3_000, 2_000}, []GameCoins{withdrawals[0].Units, withdrawals[1].Units})
	assert.Equal(t, []int{3, 2}, []int{withdrawals[0].Amount, withdrawals[1].Amount}, "on-chain amounts stay whole tokens")
	var marker Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", gameCoinUnitsOption).First(&marker).Error)
	assert.Equal(t, "1", marker.Value)
}

// 有一列乘出来会越过 2^53 时整个迁移撤销：数据原样保留，也不写标记，修好数据之后下次启动再换算。QuotaPerUnit 不是正整数时同样
// 不换算。
func TestMigrateGameCoinUnitsRollsBackWhenItCannotConvert(t *testing.T) {
	setupGameCoinMigrationTest(t, "1000")
	require.NoError(t, DB.Create(&GameCoinAccount{UserId: 7002, Balance: 12}).Error)
	require.NoError(t, DB.Create(&GameSession{UserId: 7002, Game: "dice", Status: GameSessionStatusFinished, Bet: 1, Reward: common.MaxQuota/1000 + 1}).Error)
	require.Error(t, migrateGameCoinUnits())
	assert.Equal(t, GameCoins(12), gameBalance(t, 7002))
	var markers int64
	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", gameCoinUnitsOption).Count(&markers).Error)
	assert.Zero(t, markers)

	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", "QuotaPerUnit").Update("value", "0.5").Error)
	require.NoError(t, DB.Where("user_id = ?", 7002).Delete(&GameSession{}).Error)
	require.Error(t, migrateGameCoinUnits())
	assert.Equal(t, GameCoins(12), gameBalance(t, 7002))
}
