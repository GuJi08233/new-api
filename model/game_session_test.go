package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gameTestIp = "203.0.113.7"

// 每人每天从每个游戏获得的游戏币不超过该游戏的上限：同一天后面的对局只发剩下的额度，领满或管理员调低上限后
// 照常结算但不再发币，对局上记下少发的原因；换一天、换一个游戏各自重新计算。
func TestFinishGameSessionCapsDailyRewardPerGame(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1201, 0, 0)

	cases := []struct {
		game          string
		day           string
		reward        int
		limit         int
		wantGranted   int
		wantEarned    int
		wantLimitedBy string
	}{
		{game: "tetris", day: "2026-09-24", reward: 2, limit: 3, wantGranted: 2, wantEarned: 2},
		{game: "tetris", day: "2026-09-24", reward: 5, limit: 3, wantGranted: 1, wantEarned: 3, wantLimitedBy: GameRewardLimitedByDaily},
		{game: "tetris", day: "2026-09-24", reward: 4, limit: 3, wantGranted: 0, wantEarned: 3, wantLimitedBy: GameRewardLimitedByDaily},
		{game: "tetris", day: "2026-09-24", reward: 4, limit: 2, wantGranted: 0, wantEarned: 3, wantLimitedBy: GameRewardLimitedByDaily},
		{game: "tetris", day: "2026-09-25", reward: 4, limit: 3, wantGranted: 3, wantEarned: 3, wantLimitedBy: GameRewardLimitedByDaily},
		{game: "snake", day: "2026-09-24", reward: 1, limit: 3, wantGranted: 1, wantEarned: 1},
	}
	balance := 0
	for i, c := range cases {
		session, err := CreateGameSession(1201, c.game, 7)
		require.NoError(t, err)
		settled, earned, gotBalance, err := FinishGameSession(session, GameSettlement{
			Score: 1000, Detail: "{}", Reward: c.reward, DailyLimit: c.limit, Day: c.day, Ip: gameTestIp,
		})
		require.NoError(t, err, i)
		balance += c.wantGranted
		assert.Equal(t, c.wantGranted, settled.Reward, "case %d granted", i)
		assert.Equal(t, c.wantLimitedBy, settled.LimitedBy, "case %d limited by", i)
		assert.Equal(t, c.wantEarned, earned, "case %d earned", i)
		assert.Equal(t, balance, gotBalance, "case %d balance", i)

		var stored GameSession
		require.NoError(t, DB.First(&stored, session.Id).Error)
		assert.Equal(t, GameSessionStatusFinished, stored.Status)
		assert.Equal(t, c.wantGranted, stored.Reward, "case %d session reward", i)
		assert.Equal(t, c.wantLimitedBy, stored.LimitedBy, "case %d session limited by", i)
		assert.Equal(t, gameTestIp, stored.Ip)
	}

	account, err := GetGameCoinAccount(1201)
	require.NoError(t, err)
	assert.Equal(t, balance, account.Balance)
	assert.Equal(t, balance, account.TotalEarned)
	// 没有发币的对局不写流水。
	assert.Equal(t, []string{GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward}, gameCoinLogTypes(t, 1201))
}

// 同一个 IP 视为同一个人：这个 IP 下所有账号当天从一个游戏获得的游戏币合计也不超过每日上限，
// 换一个账号不能多领；别的 IP 不受影响。游戏规则给出的少发原因在没有触及上限时原样保留。
func TestFinishGameSessionCapsDailyRewardPerIp(t *testing.T) {
	truncateTables(t)
	const day = "2026-09-24"
	cases := []struct {
		userId        int
		ip            string
		reward        int
		limitedBy     string
		wantGranted   int
		wantLimitedBy string
	}{
		{userId: 1301, ip: gameTestIp, reward: 2, limitedBy: GameRewardLimitedByTime, wantGranted: 2, wantLimitedBy: GameRewardLimitedByTime},
		{userId: 1302, ip: gameTestIp, reward: 3, wantGranted: 1, wantLimitedBy: GameRewardLimitedByIp},
		{userId: 1303, ip: gameTestIp, reward: 1, wantGranted: 0, wantLimitedBy: GameRewardLimitedByIp},
		{userId: 1304, ip: "198.51.100.9", reward: 3, wantGranted: 3},
	}
	for i, c := range cases {
		require.NoError(t, DB.Create(&User{Id: c.userId, Username: fmt.Sprintf("ip-user-%d", c.userId), AffCode: fmt.Sprintf("ip-aff-%d", c.userId)}).Error)
		session, err := CreateGameSession(c.userId, "tetris", 7)
		require.NoError(t, err)
		settled, earned, balance, err := FinishGameSession(session, GameSettlement{
			Score: 1000, Detail: "{}", Reward: c.reward, LimitedBy: c.limitedBy, DailyLimit: 3, Day: day, Ip: c.ip,
		})
		require.NoError(t, err, i)
		assert.Equal(t, c.wantGranted, settled.Reward, "case %d granted", i)
		assert.Equal(t, c.wantLimitedBy, settled.LimitedBy, "case %d limited by", i)
		assert.Equal(t, c.wantGranted, earned, "case %d earned", i)
		assert.Equal(t, c.wantGranted, balance, "case %d balance", i)
	}

	var ipTotal int
	require.NoError(t, DB.Model(&GameRewardIpDay{}).Where("ip = ? AND game = ? AND day = ?", gameTestIp, "tetris", day).Select("coins").Scan(&ipTotal).Error)
	assert.Equal(t, 3, ipTotal)
}

// 同一局只能结算一次：重复提交既不改成绩也不再发币。最高分只看结算过的对局。
func TestFinishGameSessionSettlesOnce(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1202, 0, 0)
	const day = "2026-09-24"

	session, err := CreateGameSession(1202, "tetris", 7)
	require.NoError(t, err)
	settled, _, _, err := FinishGameSession(session, GameSettlement{Score: 1200, Detail: "{}", Reward: 1, DailyLimit: 3, Day: day, Ip: gameTestIp})
	require.NoError(t, err)
	assert.Equal(t, 1, settled.Reward)
	_, err = CreateGameSession(1202, "tetris", 8) // 开了没结算的对局
	require.NoError(t, err)
	_, _, _, err = FinishGameSession(session, GameSettlement{Score: 99999, Detail: "{}", Reward: 3, DailyLimit: 3, Day: day, Ip: gameTestIp})
	require.ErrorIs(t, err, ErrGameSessionClosed)

	earned, err := GetGameRewardEarned(1202, "tetris", day)
	require.NoError(t, err)
	assert.Equal(t, 1, earned)
	best, err := GetGameBestScore(1202, "tetris")
	require.NoError(t, err)
	assert.Equal(t, 1200, best)
	account, err := GetGameCoinAccount(1202)
	require.NoError(t, err)
	assert.Equal(t, 1, account.Balance)
}

// 每人每个游戏同一时间只能进行一局：开新局时作废删除之前没结算的对局，已结算的和其他游戏的对局保留。
// 两次开局同时到达、都留了下来时，只有后开的那一局能结算。
func TestGameSessionsAllowOnePlayingGame(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1401, 0, 0)
	settlement := GameSettlement{Score: 100, Detail: "{}", DailyLimit: 3, Day: "2026-09-24", Ip: gameTestIp}

	finished, err := CreateGameSession(1401, "tetris", 1)
	require.NoError(t, err)
	_, _, _, err = FinishGameSession(finished, settlement)
	require.NoError(t, err)
	abandoned, err := CreateGameSession(1401, "tetris", 2)
	require.NoError(t, err)
	otherGame, err := CreateGameSession(1401, "snake", 3)
	require.NoError(t, err)
	current, err := CreateGameSession(1401, "tetris", 4)
	require.NoError(t, err)

	_, err = GetUserGameSession(1401, "tetris", abandoned.Id)
	assert.ErrorIs(t, err, ErrGameSessionNotFound)
	_, _, _, err = FinishGameSession(abandoned, settlement)
	assert.ErrorIs(t, err, ErrGameSessionClosed)
	_, err = GetUserGameSession(1401, "tetris", finished.Id)
	assert.NoError(t, err)
	_, err = GetUserGameSession(1401, "snake", otherGame.Id)
	assert.NoError(t, err)

	racing := &GameSession{UserId: 1401, Game: "tetris", Seed: 5, Status: GameSessionStatusPlaying, CreatedAt: common.GetTimestamp()}
	require.NoError(t, DB.Create(racing).Error)
	_, _, _, err = FinishGameSession(current, settlement)
	require.ErrorIs(t, err, ErrGameSessionSuperseded)
	stored, err := GetUserGameSession(1401, "tetris", current.Id)
	require.NoError(t, err)
	assert.Equal(t, GameSessionStatusPlaying, stored.Status, "被取代的对局不会被改成已结算")
	_, _, _, err = FinishGameSession(racing, settlement)
	assert.NoError(t, err)
}

// playFor 让下注游戏的这一局按给定的应得派彩结算，不看累计下注。
func playFor(win int) func(int) (int, string, error) {
	return func(int) (int, string, error) { return win, "{}", nil }
}

// 下注游戏的结果在结算事务里决定：Play 拿到的是这一局之前的累计下注，结算成功后累计下注才增加，
// 余额不够、被拒绝的下注不计入。游戏据此决定玩家处在哪个赔付阶段。
func TestSettleGameWagerPlaysWithWageredBefore(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1801, 0, 10)

	var seen []int
	play := func(wageredBefore int) (int, string, error) {
		seen = append(seen, wageredBefore)
		return 0, "{}", nil
	}
	cases := []struct {
		bet         int
		wantErr     error
		wantWagered int
	}{
		{bet: 5, wantWagered: 5},
		{bet: 3, wantWagered: 8},
		{bet: 5, wantErr: ErrGameCoinInsufficient},
		{bet: 2, wantWagered: 10},
	}
	for i, c := range cases {
		result, err := SettleGameWager(1801, "slots", GameWager{Bet: c.bet, Play: play, Day: "2026-09-24", Ip: gameTestIp})
		if c.wantErr != nil {
			require.ErrorIs(t, err, c.wantErr, "case %d", i)
			continue
		}
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, c.wantWagered, result.Wagered, "case %d wagered", i)
	}
	assert.Equal(t, []int{0, 5, 8, 8}, seen, "被拒绝的那一局也先决定了结果，但没有计入累计下注")
	account, err := GetGameCoinAccount(1801)
	require.NoError(t, err)
	assert.Equal(t, 10, account.TotalWagered)
	assert.Equal(t, 0, account.Balance)
}

// 下注游戏每局在一个事务里扣注、派彩，流水记这一局的净输赢。每日上限按净赢计算：输掉的下注会让当天还能赢的数量变多，
// 派彩会越过上限时只派到上限为止；净赢到了上限，当天就不能再下注，换一天重新计算。余额不够下注时什么都不改变。
func TestSettleGameWagerCapsDailyNetWin(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1501, 0, 20)

	cases := []struct {
		day           string
		bet           int
		win           int
		wantErr       error
		wantReward    int
		wantEarned    int
		wantBalance   int
		wantLimitedBy string
	}{
		{day: "2026-09-24", bet: 2, win: 0, wantReward: 0, wantEarned: -2, wantBalance: 18},
		{day: "2026-09-24", bet: 1, win: 1, wantReward: 1, wantEarned: -2, wantBalance: 18},
		{day: "2026-09-24", bet: 1, win: 10, wantReward: 10, wantEarned: 7, wantBalance: 27},
		{day: "2026-09-24", bet: 5, win: 50, wantReward: 8, wantEarned: 10, wantBalance: 30, wantLimitedBy: GameRewardLimitedByDaily},
		{day: "2026-09-24", bet: 1, win: 0, wantErr: ErrGameWagerDailyLimit, wantBalance: 30},
		{day: "2026-09-25", bet: 1, win: 2, wantReward: 2, wantEarned: 1, wantBalance: 31},
		{day: "2026-09-25", bet: 40, win: 0, wantErr: ErrGameCoinInsufficient, wantBalance: 31},
	}
	for i, c := range cases {
		result, err := SettleGameWager(1501, "slots", GameWager{
			Bet: c.bet, Play: playFor(c.win), DailyLimit: 10, Day: c.day, Ip: gameTestIp,
		})
		if c.wantErr != nil {
			require.ErrorIs(t, err, c.wantErr, "case %d", i)
		} else {
			require.NoError(t, err, "case %d", i)
			assert.Equal(t, c.wantReward, result.Session.Reward, "case %d reward", i)
			assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
			assert.Equal(t, c.wantEarned, result.Earned, "case %d earned", i)
			assert.Equal(t, c.wantBalance, result.Balance, "case %d balance", i)
		}
		account, err := GetGameCoinAccount(1501)
		require.NoError(t, err)
		assert.Equal(t, c.wantBalance, account.Balance, "case %d stored balance", i)
	}

	var amounts []int
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ? AND type = ?", 1501, GameCoinLogTypeGameWager).Order("id asc").Pluck("amount", &amounts).Error)
	assert.Equal(t, []int{-2, 0, 9, 3, 1}, amounts, "每局一条流水，被拒绝的下注不记")
	var sessions int64
	require.NoError(t, DB.Model(&GameSession{}).Where("user_id = ? AND game = ? AND status = ?", 1501, "slots", GameSessionStatusFinished).Count(&sessions).Error)
	assert.EqualValues(t, 5, sessions)
	earned, err := GetGameRewardEarned(1501, "slots", "2026-09-24")
	require.NoError(t, err)
	assert.Equal(t, 10, earned)
	best, err := GetGameBestScore(1501, "slots")
	require.NoError(t, err)
	assert.Equal(t, 10, best, "单次最高派彩按实际到账计算，被截断的 50 不算")
	account, err := GetGameCoinAccount(1501)
	require.NoError(t, err)
	assert.Equal(t, 11, account.TotalWagerNet, "累计净输赢等于这几局流水之和")
}

// 每日净赢上限为 0 时不限制：大奖全额派彩，赢得再多也能接着下注；今日输赢照常累计，每天重新计算。
// 下注游戏的输赢记在账户的累计净输赢里，不计入累计获得。
func TestSettleGameWagerWithoutDailyLimit(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1701, 0, 10)

	cases := []struct {
		day         string
		bet         int
		win         int
		wantEarned  int
		wantBalance int
	}{
		{day: "2026-09-24", bet: 5, win: 250, wantEarned: 245, wantBalance: 255},
		{day: "2026-09-24", bet: 5, win: 250, wantEarned: 490, wantBalance: 500},
		{day: "2026-09-24", bet: 5, win: 0, wantEarned: 485, wantBalance: 495},
		{day: "2026-09-25", bet: 3, win: 0, wantEarned: -3, wantBalance: 492},
	}
	for i, c := range cases {
		result, err := SettleGameWager(1701, "slots", GameWager{
			Bet: c.bet, Play: playFor(c.win), DailyLimit: 0, Day: c.day, Ip: gameTestIp,
		})
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, c.win, result.Session.Reward, "case %d reward", i)
		assert.Empty(t, result.Session.LimitedBy, "case %d limited by", i)
		assert.Equal(t, c.wantEarned, result.Earned, "case %d earned", i)
		assert.Equal(t, c.wantEarned, result.IpEarned, "case %d ip earned", i)
		assert.Equal(t, c.wantBalance, result.Balance, "case %d balance", i)
	}

	account, err := GetGameCoinAccount(1701)
	require.NoError(t, err)
	assert.Equal(t, 482, account.TotalWagerNet)
	assert.Equal(t, 10, account.TotalEarned, "累计获得只有初始的 10 个")
}

// 同一个 IP 视为同一个人：这个 IP 下所有账号当天的净赢合计也不超过上限，到了上限这个 IP 下的账号都不能再下注；
// 别的 IP 不受影响。结算结果带回这个 IP 今天的净赢，页面据此判断还能不能下注。
func TestSettleGameWagerSharesDailyNetWinPerIp(t *testing.T) {
	truncateTables(t)
	const day = "2026-09-24"
	for _, userId := range []int{1601, 1602, 1603, 1604} {
		require.NoError(t, DB.Create(&User{Id: userId, Username: fmt.Sprintf("wager-user-%d", userId), AffCode: fmt.Sprintf("wager-aff-%d", userId)}).Error)
		_, err := AdjustGameCoins(userId, 10, "seed")
		require.NoError(t, err)
	}

	cases := []struct {
		userId        int
		ip            string
		bet           int
		win           int
		wantErr       error
		wantReward    int
		wantIpEarned  int
		wantLimitedBy string
	}{
		{userId: 1601, ip: gameTestIp, bet: 1, win: 5, wantReward: 5, wantIpEarned: 4},
		{userId: 1602, ip: gameTestIp, bet: 1, win: 10, wantReward: 2, wantIpEarned: 5, wantLimitedBy: GameRewardLimitedByIp},
		{userId: 1603, ip: gameTestIp, bet: 1, win: 0, wantErr: ErrGameWagerIpLimit},
		{userId: 1601, ip: gameTestIp, bet: 2, win: 0, wantErr: ErrGameWagerIpLimit},
		{userId: 1604, ip: "198.51.100.9", bet: 1, win: 10, wantReward: 6, wantIpEarned: 5, wantLimitedBy: GameRewardLimitedByDaily},
	}
	for i, c := range cases {
		result, err := SettleGameWager(c.userId, "slots", GameWager{
			Bet: c.bet, Play: playFor(c.win), DailyLimit: 5, Day: day, Ip: c.ip,
		})
		if c.wantErr != nil {
			require.ErrorIs(t, err, c.wantErr, "case %d", i)
			continue
		}
		require.NoError(t, err, "case %d", i)
		assert.Equal(t, c.wantReward, result.Session.Reward, "case %d reward", i)
		assert.Equal(t, c.wantIpEarned, result.IpEarned, "case %d ip earned", i)
		assert.Equal(t, c.wantLimitedBy, result.Session.LimitedBy, "case %d limited by", i)
	}

	ipNet, err := GetGameRewardIpEarned(gameTestIp, "slots", day)
	require.NoError(t, err)
	assert.Equal(t, 5, ipNet)
	account, err := GetGameCoinAccount(1603)
	require.NoError(t, err)
	assert.Equal(t, 10, account.Balance, "被拒绝的下注不扣币")
}
