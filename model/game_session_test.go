package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gameTestIp      = "203.0.113.7"
	gameTestOtherIp = "198.51.100.9"
)

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
// 结算和保存每一步一样核对版本：同一局的另一步已经保存了新局面，依据旧局面算出的结算作废，对局照常进行；
// 拿着当前局面的结算照常成功。玩家不能同时发几步、挑赢了的那一步结算。
func TestFinishGameSessionRejectsStaleVersion(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 1211, 0)
	session, err := CreateGameSession(1211, "memory", 0, `{"step":0}`)
	require.NoError(t, err)
	stale := *session
	require.NoError(t, UpdateGameSessionState(session, `{"step":1}`, ""))

	settlement := GameSettlement{Score: 1, Reward: 1, DailyRewards: 1, Day: "2026-09-29", Ip: gameTestIp}
	_, err = FinishGameSession(&stale, settlement)
	require.ErrorIs(t, err, ErrGameSessionClosed)
	var current GameSession
	require.NoError(t, DB.First(&current, session.Id).Error)
	assert.Equal(t, GameSessionStatusPlaying, current.Status)
	assert.Equal(t, `{"step":1}`, current.State)
	assert.Zero(t, gameBalance(t, 1211))

	result, err := FinishGameSession(session, settlement)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Session.Reward)
	assert.Equal(t, 1, gameBalance(t, 1211))
}

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

// 每个游戏币要账号当天在这个游戏里累计玩够 SecondsPerCoin 秒(已经领到的也算在内)，不要求一局玩这么久：成绩先达标、时长
// 还不够时奖励先记着，之后的对局哪怕没达标，把时长补够就发；判定为脚本的对局不计时长，也不会把记着的奖励发出去；
// 当天的领奖次数用完就不再记。
func TestFinishGameSessionWaitsForDailyPlayTime(t *testing.T) {
	truncateTables(t)
	const userId, day = 1801, "2026-09-28"
	cases := []struct {
		name          string
		reward        int
		seconds       int
		cheat         bool
		wantGranted   int
		wantLimitedBy string
		wantSeconds   int
		wantPending   int
	}{
		{name: "达标但累计时长不够", reward: 1, seconds: 40, wantLimitedBy: GameRewardLimitedByTime, wantSeconds: 40, wantPending: 1},
		{name: "脚本对局不计时长", reward: 1, seconds: 500, cheat: true, wantLimitedBy: GameRewardLimitedBySpeed, wantSeconds: 40, wantPending: 1},
		{name: "没达标的一局补够时长，发记着的奖励", reward: 0, seconds: 30, wantGranted: 1, wantSeconds: 70},
		{name: "第二个游戏币要累计 120 秒", reward: 1, seconds: 30, wantLimitedBy: GameRewardLimitedByTime, wantSeconds: 100, wantPending: 1},
		{name: "补够 120 秒", reward: 0, seconds: 20, wantGranted: 1, wantSeconds: 120},
		{name: "当天的次数领完了", reward: 1, seconds: 600, wantLimitedBy: GameRewardLimitedByDaily, wantSeconds: 720},
	}
	balance := 0
	for _, c := range cases {
		session, err := CreateGameSession(userId, "tetris", 7, "")
		require.NoError(t, err)
		result, err := FinishGameSession(session, GameSettlement{
			Score: 100, Detail: "{}", Reward: c.reward, Seconds: c.seconds, Cheat: c.cheat,
			SecondsPerCoin: 60, DailyRewards: 2, Day: day, Ip: gameTestIp,
		})
		require.NoError(t, err, c.name)
		balance += c.wantGranted
		assert.Equal(t, c.wantGranted, result.Session.Reward, c.name)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, c.name)
		assert.Equal(t, c.wantSeconds, result.Seconds, c.name)
		assert.Equal(t, c.wantPending, result.PendingReward, c.name)
		assert.Equal(t, balance, result.Balance, c.name)
	}
	progress, err := GetGameRewardProgress(userId, gameTestIp, "tetris", day)
	require.NoError(t, err)
	assert.Equal(t, GameRewardProgress{Plays: 6, Rewards: 2, IpRewards: 2, Earned: 2, FreeEarned: 2, IpFreeEarned: 2, Seconds: 720}, progress)
}

// 一次奖励有几个游戏币、累计时长只够发一部分时，先发够时长的几个，剩下的记着，之后的对局把时长补够接着发，不占用新的领奖
// 次数，一个都不会少。这一局没领上奖(次数用完)也照样发之前待发的。
func TestFinishGameSessionPaysMultiCoinRewardAsPlayTimeAccrues(t *testing.T) {
	truncateTables(t)
	const userId, day = 1811, "2026-09-28"
	cases := []struct {
		name          string
		reward        int
		seconds       int
		wantGranted   int
		wantLimitedBy string
		wantPending   int
	}{
		{name: "达标领奖，时长一个都不够", reward: 5, seconds: 30, wantLimitedBy: GameRewardLimitedByTime, wantPending: 5},
		{name: "次数已用完，时长够发一个", reward: 5, seconds: 30, wantGranted: 1, wantLimitedBy: GameRewardLimitedByTime, wantPending: 4},
		{name: "补够时长，发完剩下的", reward: 0, seconds: 600, wantGranted: 4},
		{name: "再达标也不能再领", reward: 5, seconds: 600, wantLimitedBy: GameRewardLimitedByDaily},
	}
	balance := 0
	for _, c := range cases {
		session, err := CreateGameSession(userId, "tetris", 7, "")
		require.NoError(t, err)
		result, err := FinishGameSession(session, GameSettlement{
			Score: 100, Detail: "{}", Reward: c.reward, Seconds: c.seconds, SecondsPerCoin: 60, DailyRewards: 1, Day: day, Ip: gameTestIp,
		})
		require.NoError(t, err, c.name)
		balance += c.wantGranted
		assert.Equal(t, c.wantGranted, result.Session.Reward, c.name)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, c.name)
		assert.Equal(t, c.wantPending, result.PendingReward, c.name)
		assert.Equal(t, 1, result.Rewards, "%s: 达标那一局就占用了当天唯一的一次领奖", c.name)
		assert.Equal(t, balance, result.Balance, c.name)
	}
	assert.Equal(t, 5, gameBalance(t, userId))
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

// 大转盘的免费旋转：每人每天、同一 IP 合计只有 FreeLimit 次，无论中没中奖都占一次；奖品全额发放，没中奖时不写流水。
func TestSettleGameFreePlayLimitsFreePlays(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 1701, 0)
	seedGameUser(t, 1702, 0)
	const day = "2026-09-28"
	cases := []struct {
		userId    int
		win       int
		freeLimit int
		wantErr   error
		wantWin   int
	}{
		{userId: 1701, win: 0, freeLimit: 2},
		{userId: 1701, win: 3, freeLimit: 2, wantWin: 3},
		{userId: 1701, win: 3, freeLimit: 2, wantErr: ErrGameFreePlayUsed},
		// 同一 IP 的另一个账号：IP 合计的两次已经用完。
		{userId: 1702, win: 3, freeLimit: 2, wantErr: ErrGameIpFreePlayUsed},
		// 管理员加了免费次数，新的旋转仍按奖品全额发放。
		{userId: 1702, win: 50, freeLimit: 4, wantWin: 50},
		{userId: 1702, win: 1, freeLimit: 4, wantWin: 1},
		{userId: 1702, win: 1, freeLimit: 4, wantErr: ErrGameIpFreePlayUsed},
	}
	for i, c := range cases {
		result, err := SettleGameFreePlay(c.userId, "wheel", GameFreePlay{
			FreeLimit: c.freeLimit,
			Play:      func() (int, string, error) { return c.win, "{}", nil },
			Day:       day,
			Ip:        gameTestIp,
		})
		if c.wantErr != nil {
			assert.ErrorIs(t, err, c.wantErr, "case %d", i)
			continue
		}
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, c.wantWin, result.Session.Reward, "case %d win", i)
		assert.Empty(t, result.Session.LimitedBy, "case %d limited by", i)
	}
	assert.Equal(t, 3, gameBalance(t, 1701))
	assert.Equal(t, 51, gameBalance(t, 1702))
	assert.Equal(t, []string{GameCoinLogTypeGameReward}, gameCoinLogTypes(t, 1701), "没中奖的那次不写流水")
}

// 转盘与技巧游戏无论先后，都能领取各自奖励：转盘派彩不被免费奖励总上限截断，也不占用该上限。
func TestWheelFreePrizeIsIndependentOfSkillRewardLimit(t *testing.T) {
	const userId, day = 1751, "2026-09-28"
	for _, first := range []string{"wheel", "skill"} {
		t.Run(first+"_first", func(t *testing.T) {
			truncateTables(t)
			seedGameUser(t, userId, 0)
			order := []string{"wheel", "skill"}
			if first == "skill" {
				order = []string{"skill", "wheel"}
			}
			for _, game := range order {
				if game == "skill" {
					result := finishWithReward(t, userId, "tetris", gameTestIp, day, 3, 2, 3)
					assert.Equal(t, 3, result.Session.Reward)
					continue
				}
				result, err := SettleGameFreePlay(userId, "wheel", GameFreePlay{
					FreeLimit: 1,
					Play:      func() (int, string, error) { return 50, "{}", nil },
					Day:       day,
					Ip:        gameTestIp,
				})
				require.NoError(t, err)
				assert.Equal(t, 50, result.Session.Score)
				assert.Equal(t, 50, result.Session.Reward)
				assert.Empty(t, result.Session.LimitedBy)
			}
			progress, err := GetGameRewardProgress(userId, gameTestIp, "tetris", day)
			require.NoError(t, err)
			assert.Equal(t, 3, progress.FreeEarned)
			assert.Equal(t, 3, progress.IpFreeEarned)
			assert.Equal(t, 53, gameBalance(t, userId))
			limited := finishWithReward(t, userId, "snake", gameTestIp, day, 1, 2, 3)
			assert.Zero(t, limited.Session.Reward)
			assert.Equal(t, GameRewardLimitedByTotal, limited.Session.LimitedBy)
		})
	}
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

	settlement := GameRoundSettlement{Win: 16, State: `{"step":3}`, Detail: "{}", Day: "2026-09-28"}
	settled, err := SettleGameRound(round, settlement)
	require.NoError(t, err)
	assert.Equal(t, 28, settled.Balance)
	assert.Equal(t, 8, settled.Net)
	_, err = SettleGameRound(round, settlement)
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

// 让一局结束的最后一步还追加了下注(21 点加倍后直接结束)：追加与派彩在同一个事务里，余额不够追加时这一局原样不动。
func TestGameRoundSettleWithFinalRaise(t *testing.T) {
	truncateTables(t)
	const userId = 1902
	seedGameUser(t, userId, 10)
	round, balance, err := OpenGameRound(userId, "blackjack", GameRound{Bet: 6, State: "{}", Ip: gameTestIp})
	require.NoError(t, err)
	assert.Equal(t, 4, balance)

	_, err = SettleGameRound(round, GameRoundSettlement{Raise: 6, Win: 24, State: "{}", Detail: "{}", Day: "2026-09-28"})
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	open, err := GetPlayingGameSession(userId, "blackjack")
	require.NoError(t, err)
	require.NotNil(t, open, "余额不够追加时这一局还在进行")
	assert.Equal(t, 6, open.Bet)
	assert.Equal(t, round.Version, open.Version)
	assert.Equal(t, 4, gameBalance(t, userId))

	settled, err := SettleGameRound(round, GameRoundSettlement{Raise: 4, Win: 20, State: "{}", Detail: "{}", Day: "2026-09-28"})
	require.NoError(t, err)
	assert.Equal(t, 20, settled.Balance, "4 - 4 + 20")
	assert.Equal(t, 10, settled.Net)
	assert.Equal(t, 10, settled.Session.Bet)

	var amounts []int
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ? AND type = ?", userId, GameCoinLogTypeGameWager).Order("id asc").Pluck("amount", &amounts).Error)
	assert.Equal(t, []int{-6, -4, 20}, amounts)
	account, err := GetGameCoinAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, 10, account.TotalWagered)
	assert.Equal(t, 10, account.TotalWagerNet)
}
