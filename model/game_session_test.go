package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gameTestIp = "203.0.113.7"

// seedGameUser 建一个用户，coins 大于 0 时先给他发这么多游戏币。用户名与邀请码按 ID 区分，同一个测试里可以建几个。
func seedGameUser(t *testing.T, id int, coins int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: id, Username: fmt.Sprintf("game-user-%d", id), AffCode: fmt.Sprintf("game-aff-%d", id)}).Error)
	if coins > 0 {
		_, err := AdjustGameCoins(id, coins, "seed")
		require.NoError(t, err)
	}
}

func gameBalance(t *testing.T, userId int) int {
	t.Helper()
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	return account.Balance
}

// finishWithReward 开一局并按给定的应得奖励结算，返回结算结果。
func finishWithReward(t *testing.T, userId int, game string, ip string, day string, reward int, dailyRewards int, freeLimit int) *GameSettlementResult {
	t.Helper()
	session, err := CreateGameSession(userId, game, 7, "")
	require.NoError(t, err)
	result, err := FinishGameSession(session, GameSettlement{
		Score: 100, Detail: "{}", Reward: reward, DailyRewards: dailyRewards, FreeDailyLimit: freeLimit, Day: day, Ip: ip,
	})
	require.NoError(t, err)
	return result
}

// 免费游戏每人每天只能领 DailyRewards 次奖：没达标的对局不占次数，领满之后照常结算但不再发币，对局上记下原因；
// 换一天、换一个游戏各自重新计算。只有真正发了币的对局才写流水、计入累计获得。
func TestFinishGameSessionLimitsRewardedPlaysPerDay(t *testing.T) {
	truncateTables(t)
	const userId = 1201
	cases := []struct {
		game          string
		day           string
		reward        int
		wantGranted   int
		wantRewards   int
		wantLimitedBy string
	}{
		{game: "2048", day: "2026-09-28", reward: 2, wantGranted: 2, wantRewards: 1},
		{game: "2048", day: "2026-09-28", reward: 0, wantGranted: 0, wantRewards: 1},
		{game: "2048", day: "2026-09-28", reward: 1, wantGranted: 1, wantRewards: 2},
		{game: "2048", day: "2026-09-28", reward: 2, wantGranted: 0, wantRewards: 2, wantLimitedBy: GameRewardLimitedByDaily},
		{game: "snake", day: "2026-09-28", reward: 1, wantGranted: 1, wantRewards: 1},
		{game: "2048", day: "2026-09-29", reward: 2, wantGranted: 2, wantRewards: 1},
	}
	balance := 0
	for i, c := range cases {
		result := finishWithReward(t, userId, c.game, gameTestIp, c.day, c.reward, 2, 0)
		balance += c.wantGranted
		assert.Equal(t, c.wantGranted, result.Session.Reward, "case %d granted", i)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
		assert.Equal(t, c.wantRewards, result.Rewards, "case %d rewards today", i)
		assert.Equal(t, balance, result.Balance, "case %d balance", i)

		var stored GameSession
		require.NoError(t, DB.First(&stored, result.Session.Id).Error)
		assert.Equal(t, GameSessionStatusFinished, stored.Status)
		assert.Equal(t, c.wantGranted, stored.Reward, "case %d stored reward", i)
		assert.Equal(t, c.wantLimitedBy, stored.LimitedBy, "case %d stored limited by", i)
	}
	progress, err := GetGameRewardProgress(userId, gameTestIp, "2048", "2026-09-28")
	require.NoError(t, err)
	// 合计里还有同一天贪吃蛇的那 1 个。
	assert.Equal(t, GameRewardProgress{Plays: 4, Rewards: 2, IpRewards: 2, Earned: 3, FreeEarned: 4, IpFreeEarned: 4}, progress)

	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, balance, account.Balance)
	assert.Equal(t, balance, account.TotalEarned)
	assert.Equal(t, []string{GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward}, gameCoinLogTypes(t, userId))
}

// 同一个 IP 视为同一个人：这个 IP 下所有账号当天在一个游戏里合计也只能领 DailyRewards 次，换一个账号不能多领；
// 别的 IP 不受影响。
func TestFinishGameSessionSharesRewardedPlaysPerIp(t *testing.T) {
	truncateTables(t)
	const day = "2026-09-28"
	cases := []struct {
		userId        int
		ip            string
		wantGranted   int
		wantLimitedBy string
	}{
		{userId: 1301, ip: gameTestIp, wantGranted: 1},
		{userId: 1302, ip: gameTestIp, wantGranted: 1},
		{userId: 1303, ip: gameTestIp, wantGranted: 0, wantLimitedBy: GameRewardLimitedByIp},
		{userId: 1303, ip: "198.51.100.9", wantGranted: 1},
	}
	for i, c := range cases {
		result := finishWithReward(t, c.userId, "minesweeper", c.ip, day, 1, 2, 0)
		assert.Equal(t, c.wantGranted, result.Session.Reward, "case %d granted", i)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
	}
	progress, err := GetGameRewardProgress(1303, gameTestIp, "minesweeper", day)
	require.NoError(t, err)
	assert.Equal(t, 2, progress.IpRewards)
	assert.Equal(t, 1, progress.Rewards, "账号在另一个 IP 上领的那次也算账号自己的次数")
}

// 所有免费奖励每人每天(同一 IP 合计)还有一个总上限：一局的奖励会越过它时只发到上限为止，到了上限就不再发；
// 没发币的对局不占这个游戏的领奖次数。上限为 0 表示不限制。
func TestFinishGameSessionCapsFreeRewardsInTotal(t *testing.T) {
	truncateTables(t)
	const day = "2026-09-28"
	cases := []struct {
		userId        int
		game          string
		reward        int
		wantGranted   int
		wantLimitedBy string
	}{
		{userId: 1401, game: "tetris", reward: 2, wantGranted: 2},
		{userId: 1401, game: "sudoku", reward: 2, wantGranted: 1, wantLimitedBy: GameRewardLimitedByTotal},
		{userId: 1401, game: "memory", reward: 1, wantGranted: 0, wantLimitedBy: GameRewardLimitedByTotal},
		// 同一 IP 的另一个账号合计也只有这么多。
		{userId: 1402, game: "memory", reward: 1, wantGranted: 0, wantLimitedBy: GameRewardLimitedByTotal},
	}
	for i, c := range cases {
		result := finishWithReward(t, c.userId, c.game, gameTestIp, day, c.reward, 3, 3)
		assert.Equal(t, c.wantGranted, result.Session.Reward, "case %d granted", i)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
	}
	progress, err := GetGameRewardProgress(1401, gameTestIp, "memory", day)
	require.NoError(t, err)
	assert.Equal(t, 0, progress.Rewards, "被合计上限挡住的对局不占领奖次数")
	assert.Equal(t, 3, progress.FreeEarned)

	// 不限制合计时照常发放。
	result := finishWithReward(t, 1401, "memory", gameTestIp, day, 1, 3, 0)
	assert.Equal(t, 1, result.Session.Reward)
	assert.Equal(t, 4, result.FreeEarned)
}

// 同一局只能结算一次：第二次结算返回 ErrGameSessionClosed，奖励不会重复发放。
func TestFinishGameSessionSettlesOnce(t *testing.T) {
	truncateTables(t)
	session, err := CreateGameSession(1501, "tetris", 7, "")
	require.NoError(t, err)
	settlement := GameSettlement{Score: 9000, Detail: "{}", Reward: 1, DailyRewards: 3, Day: "2026-09-28", Ip: gameTestIp}
	_, err = FinishGameSession(session, settlement)
	require.NoError(t, err)
	_, err = FinishGameSession(session, settlement)
	assert.ErrorIs(t, err, ErrGameSessionClosed)
	assert.Equal(t, 1, gameBalance(t, 1501))
}

// 每人每个游戏同一时间只有一局免费对局：开新局作废之前没结算的，被作废的那局不能再结算；两局同时留下时只有后开的能结算。
func TestGameSessionsAllowOnePlayingGame(t *testing.T) {
	truncateTables(t)
	first, err := CreateGameSession(1601, "tetris", 1, "")
	require.NoError(t, err)
	second, err := CreateGameSession(1601, "tetris", 2, "")
	require.NoError(t, err)
	settlement := GameSettlement{Score: 1, Detail: "{}", Reward: 1, DailyRewards: 3, Day: "2026-09-28", Ip: gameTestIp}
	_, err = FinishGameSession(first, settlement)
	assert.ErrorIs(t, err, ErrGameSessionClosed, "被新开的一局作废")

	// 两次开局同时到达时可能都留了下来：先开的那局不能结算。
	require.NoError(t, DB.Create(&GameSession{UserId: 1601, Game: "tetris", Status: GameSessionStatusPlaying, CreatedAt: second.CreatedAt}).Error)
	_, err = FinishGameSession(second, settlement)
	assert.ErrorIs(t, err, ErrGameSessionSuperseded)
	assert.Equal(t, 0, gameBalance(t, 1601))
}

// 大转盘的免费旋转：每人每天、同一 IP 合计只有 FreeLimit 次，无论中没中奖都占一次；奖品计入免费奖励的合计上限，
// 越过上限的部分不发，到了上限就不能再免费转。没中奖时不写流水。
func TestSettleGameFreePlayLimitsFreePlays(t *testing.T) {
	truncateTables(t)
	const day = "2026-09-28"
	cases := []struct {
		userId        int
		win           int
		freeLimit     int
		wantErr       error
		wantWin       int
		wantLimitedBy string
	}{
		{userId: 1701, win: 0, freeLimit: 2},
		{userId: 1701, win: 3, freeLimit: 2, wantWin: 3},
		{userId: 1701, win: 3, freeLimit: 2, wantErr: ErrGameFreePlayUsed},
		// 同一 IP 的另一个账号：IP 合计的两次已经用完。
		{userId: 1702, win: 3, freeLimit: 2, wantErr: ErrGameIpFreePlayUsed},
		// 管理员加了免费次数，但合计上限只剩 2 个：奖品只发 2 个。
		{userId: 1702, win: 5, freeLimit: 4, wantWin: 2, wantLimitedBy: GameRewardLimitedByTotal},
		{userId: 1702, win: 1, freeLimit: 4, wantErr: ErrGameFreeDailyLimit},
	}
	for i, c := range cases {
		result, err := SettleGameFreePlay(c.userId, "wheel", GameFreePlay{
			FreeLimit:      c.freeLimit,
			FreeDailyLimit: 5,
			Play:           func() (int, string, error) { return c.win, "{}", nil },
			Day:            day,
			Ip:             gameTestIp,
		})
		if c.wantErr != nil {
			assert.ErrorIs(t, err, c.wantErr, "case %d", i)
			continue
		}
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, c.wantWin, result.Session.Reward, "case %d win", i)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
	}
	assert.Equal(t, 3, gameBalance(t, 1701))
	assert.Equal(t, 2, gameBalance(t, 1702))
	assert.Equal(t, []string{GameCoinLogTypeGameReward}, gameCoinLogTypes(t, 1701), "没中奖的那次不写流水")
}

// 下注游戏不设每日上限：想下多少局都可以，每局扣注、派彩、写一条净输赢流水，今天的净输赢与局数照常累计；
// 余额不够时什么都不改变。
func TestSettleGameWagerHasNoDailyLimit(t *testing.T) {
	truncateTables(t)
	const userId = 1801
	seedGameUser(t, userId, 30)
	for i, win := range []int{0, 0, 20, 0, 5} {
		result, err := SettleGameWager(userId, "dice", GameWager{
			Bet:  5,
			Play: func() (int, string, error) { return win, "{}", nil },
			Day:  "2026-09-28",
			Ip:   gameTestIp,
		})
		require.NoError(t, err, "wager %d", i)
		assert.Equal(t, i+1, result.Plays)
	}
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, 30, account.Balance)
	assert.Equal(t, 25, account.TotalWagered)
	assert.Equal(t, 0, account.TotalWagerNet)
	net, plays, err := GetGameDayStats(userId, "dice", "2026-09-28")
	require.NoError(t, err)
	assert.Equal(t, 0, net)
	assert.Equal(t, 5, plays)

	_, err = SettleGameWager(userId, "dice", GameWager{Bet: 31, Play: func() (int, string, error) { return 100, "{}", nil }, Day: "2026-09-28", Ip: gameTestIp})
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	assert.Equal(t, 30, gameBalance(t, userId))
}

// 分几步完成的下注游戏：开局扣注，同一时间只能有一局；加注再扣；拿着旧版本的操作不生效；结束时派彩一次，
// 扣注与派彩的流水合计就是这一局的净输赢。
func TestGameRoundLifecycle(t *testing.T) {
	truncateTables(t)
	const userId = 1901
	seedGameUser(t, userId, 20)
	round, balance, err := OpenGameRound(userId, "blackjack", GameRound{Bet: 4, State: `{"step":0}`, Ip: gameTestIp})
	require.NoError(t, err)
	assert.Equal(t, 16, balance)
	_, _, err = OpenGameRound(userId, "blackjack", GameRound{Bet: 4, State: "{}", Ip: gameTestIp})
	assert.ErrorIs(t, err, ErrGameRoundOpen)

	stale := *round
	require.NoError(t, UpdateGameSessionState(round, `{"step":1}`, ""))
	assert.ErrorIs(t, UpdateGameSessionState(&stale, `{"step":9}`, ""), ErrGameRoundChanged)

	balance, err = RaiseGameRound(round, 4, `{"step":2}`, "")
	require.NoError(t, err)
	assert.Equal(t, 12, balance)
	assert.Equal(t, 8, round.Bet)

	settled, err := SettleGameRound(round, 16, `{"step":3}`, "{}", "2026-09-28")
	require.NoError(t, err)
	assert.Equal(t, 28, settled.Balance)
	assert.Equal(t, 8, settled.Net)
	_, err = SettleGameRound(round, 16, `{"step":3}`, "{}", "2026-09-28")
	assert.ErrorIs(t, err, ErrGameRoundChanged, "同一局只结算一次")

	var amounts []int
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ? AND type = ?", userId, GameCoinLogTypeGameWager).Order("id asc").Pluck("amount", &amounts).Error)
	assert.Equal(t, []int{-4, -4, 16}, amounts)
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, 8, account.TotalWagered)
	assert.Equal(t, 8, account.TotalWagerNet)

	// 结束之后可以开下一局。
	_, _, err = OpenGameRound(userId, "blackjack", GameRound{Bet: 2, State: "{}", Ip: gameTestIp})
	assert.NoError(t, err)
}
