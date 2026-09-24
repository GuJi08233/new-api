package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每人每天从每个游戏获得的游戏币不超过该游戏的上限：同一天后面的对局只发剩下的额度，领满或管理员调低上限后
// 照常结算但不再发币；换一天、换一个游戏各自重新计算。
func TestFinishGameSessionCapsDailyRewardPerGame(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1201, 0, 0)

	cases := []struct {
		game        string
		day         string
		reward      int
		limit       int
		wantGranted int
		wantEarned  int
	}{
		{game: "tetris", day: "2026-09-24", reward: 2, limit: 3, wantGranted: 2, wantEarned: 2},
		{game: "tetris", day: "2026-09-24", reward: 5, limit: 3, wantGranted: 1, wantEarned: 3},
		{game: "tetris", day: "2026-09-24", reward: 4, limit: 3, wantGranted: 0, wantEarned: 3},
		{game: "tetris", day: "2026-09-24", reward: 4, limit: 2, wantGranted: 0, wantEarned: 3},
		{game: "tetris", day: "2026-09-25", reward: 4, limit: 3, wantGranted: 3, wantEarned: 3},
		{game: "snake", day: "2026-09-24", reward: 1, limit: 3, wantGranted: 1, wantEarned: 1},
	}
	balance := 0
	for i, c := range cases {
		session, err := CreateGameSession(1201, c.game, 7)
		require.NoError(t, err)
		granted, earned, gotBalance, err := FinishGameSession(session, 1000, "{}", c.reward, c.limit, c.day)
		require.NoError(t, err, i)
		balance += c.wantGranted
		assert.Equal(t, c.wantGranted, granted, "case %d granted", i)
		assert.Equal(t, c.wantEarned, earned, "case %d earned", i)
		assert.Equal(t, balance, gotBalance, "case %d balance", i)

		var stored GameSession
		require.NoError(t, DB.First(&stored, session.Id).Error)
		assert.Equal(t, GameSessionStatusFinished, stored.Status)
		assert.Equal(t, c.wantGranted, stored.Reward, "case %d session reward", i)
	}

	account, err := GetGameCoinAccount(1201)
	require.NoError(t, err)
	assert.Equal(t, balance, account.Balance)
	assert.Equal(t, balance, account.TotalEarned)
	// 没有发币的对局不写流水。
	assert.Equal(t, []string{GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward, GameCoinLogTypeGameReward}, gameCoinLogTypes(t, 1201))
}

// 同一局只能结算一次：重复提交既不改成绩也不再发币。最高分只看结算过的对局。
func TestFinishGameSessionSettlesOnce(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1202, 0, 0)
	const day = "2026-09-24"

	session, err := CreateGameSession(1202, "tetris", 7)
	require.NoError(t, err)
	_, err = CreateGameSession(1202, "tetris", 8) // 开了没结算的对局
	require.NoError(t, err)

	granted, _, _, err := FinishGameSession(session, 1200, "{}", 1, 3, day)
	require.NoError(t, err)
	assert.Equal(t, 1, granted)
	_, _, _, err = FinishGameSession(session, 99999, "{}", 3, 3, day)
	require.ErrorIs(t, err, ErrGameSessionFinished)

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
