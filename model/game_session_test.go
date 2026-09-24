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
