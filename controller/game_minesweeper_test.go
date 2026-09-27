package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/minesweeper"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minesweeperSetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Minesweeper = rule })
}

// startMinesweeperSession 直接建一局用固定密钥布雷的扫雷，并把开局时间往前拨 startedAgo 秒，模拟玩家已经玩了这么久。
func startMinesweeperSession(t *testing.T, env *gameTestEnv, userId int, difficulty int, startedAgo int64) *model.GameSession {
	t.Helper()
	game, err := minesweeper.New(difficulty, [32]byte{7})
	require.NoError(t, err)
	state, err := common.Marshal(game)
	require.NoError(t, err)
	session, err := model.CreateGameSession(userId, gameMinesweeper, 0, string(state))
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", session.CreatedAt-startedAgo).Error)
	return session
}

// storedMinesweeper 读出服务端保存的雷区，测试据此知道雷在哪里。
func storedMinesweeper(t *testing.T, userId int, sessionId int) *minesweeper.Game {
	t.Helper()
	session, err := model.GetUserGameSession(userId, gameMinesweeper, sessionId)
	require.NoError(t, err)
	var game minesweeper.Game
	require.NoError(t, common.UnmarshalJsonStr(session.State, &game))
	return &game
}

// minesweeperHiddenSafeCell 返回按行优先第一个还没翻开的空格，没有时 ok 为假。
func minesweeperHiddenSafeCell(game *minesweeper.Game) ([2]int, bool) {
	taken := map[[2]int]bool{}
	for _, mine := range game.MineCells() {
		taken[mine] = true
	}
	for _, cell := range game.Revealed() {
		taken[[2]int{cell[0], cell[1]}] = true
	}
	for y := 0; y < game.Height; y++ {
		for x := 0; x < game.Width; x++ {
			if !taken[[2]int{x, y}] {
				return [2]int{x, y}, true
			}
		}
	}
	return [2]int{}, false
}

// revealMinesweeper 发一次翻开请求，要求成功，返回 data 与响应原文。
func revealMinesweeper(t *testing.T, player *gamePlayer, sessionId int, version int, cells ...[2]int) (map[string]any, string) {
	t.Helper()
	recorder := player.send(http.MethodPost, "/api/game/minesweeper/reveal", map[string]any{"session_id": sessionId, "version": version, "cells": cells})
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"], response["message"])
	return response["data"].(map[string]any), recorder.Body.String()
}

// 开局只给出雷区大小和雷数，雷等第一下翻开时才布置，避开点的格子和它周围 8 格。翻开的回应与刷新页面后的局面里只有翻开的
// 格子，雷的位置与布雷的密钥一局结束之前不会出现在任何响应里。
func TestMinesweeperHidesMinesUntilTheGameEnds(t *testing.T) {
	env := newGameTestEnv(t, minesweeperSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/minesweeper", nil)
	levels := info["levels"].([]any)
	require.Len(t, levels, 3)
	assert.Equal(t, map[string]any{"difficulty": 3.0, "width": 30.0, "height": 16.0, "mines": 99.0}, levels[2])
	player.fails(http.MethodPost, "/api/game/minesweeper/start", map[string]any{"difficulty": 4})

	started := player.ok(http.MethodPost, "/api/game/minesweeper/start", map[string]any{"difficulty": 2})
	assert.EqualValues(t, 16, started["width"])
	assert.EqualValues(t, 16, started["height"])
	assert.EqualValues(t, 40, started["mines"])
	assert.EqualValues(t, 0, started["version"])
	assert.Equal(t, minesweeper.StatusPlaying, started["status"])
	assert.Empty(t, started["revealed"])
	sessionId := int(started["session_id"].(float64))
	assert.Empty(t, storedMinesweeper(t, player.user.Id, sessionId).MineCells(), "第一下之前还没有布雷")

	data, raw := revealMinesweeper(t, player, sessionId, 0, [2]int{7, 8})
	assert.EqualValues(t, 1, data["version"])
	assert.Equal(t, minesweeper.StatusPlaying, data["status"])
	mines := storedMinesweeper(t, player.user.Id, sessionId).MineCells()
	require.Len(t, mines, 40)
	isMine := map[[2]int]bool{}
	for _, mine := range mines {
		isMine[mine] = true
		assert.False(t, mine[0] >= 6 && mine[0] <= 8 && mine[1] >= 7 && mine[1] <= 9, "雷 %v 在第一下周围", mine)
	}
	revealed := data["revealed"].([]any)
	require.NotEmpty(t, revealed)
	assert.Equal(t, []any{7.0, 8.0, 0.0}, revealed[0], "第一下周围没有雷，从它展开")
	for _, cell := range revealed {
		xy := cell.([]any)
		assert.False(t, isMine[[2]int{int(xy[0].(float64)), int(xy[1].(float64))}], "翻开的 %v 不能是雷", xy)
	}
	for _, hidden := range []string{"mine_cells", `"key"`, `"cells"`, "exploded"} {
		assert.NotContains(t, raw, hidden)
	}

	recorder := player.send(http.MethodGet, "/api/game/minesweeper/state", nil)
	var state map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &state))
	restored := state["data"].(map[string]any)["session"].(map[string]any)
	assert.EqualValues(t, sessionId, restored["session_id"])
	assert.EqualValues(t, 1, restored["version"])
	assert.EqualValues(t, 40, restored["mines"])
	assert.ElementsMatch(t, revealed, restored["revealed"], "刷新页面后恢复出翻开过的格子")
	for _, hidden := range []string{"mine_cells", `"key"`, `"cells"`} {
		assert.NotContains(t, recorder.Body.String(), hidden)
	}
}

// 翻开所有不是雷的格子就赢了：按赢下的难度领这一档的奖励，响应里公开全部雷的位置。踩雷按成绩 0 结算、什么都不发，公开全部雷
// 和踩到的那一格。已经结束的一局再发翻开请求，拿到的是第一次结算的结果；结束之后刷新页面没有进行中的一局。
func TestMinesweeperSettlesWinsAndLosses(t *testing.T) {
	env := newGameTestEnv(t, minesweeperSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}, {Score: 3, Coins: 2}}, MinSecondsPerCoin: 30,
	}))
	player := env.player(0, gameTestIpA)

	// 赢一局初级：每次翻开按行优先第一个还没翻开的空格，直到翻完。
	won := startMinesweeperSession(t, env, player.user.Id, 1, 100)
	version := 0
	var last map[string]any
	for {
		cell, ok := minesweeperHiddenSafeCell(storedMinesweeper(t, player.user.Id, won.Id))
		require.True(t, ok, "还有空格没翻开时这局不会结束")
		data, raw := revealMinesweeper(t, player, won.Id, version, cell)
		last, version = data, int(data["version"].(float64))
		if data["status"] != minesweeper.StatusPlaying {
			break
		}
		assert.NotContains(t, raw, "mine_cells", "赢之前不公开雷的位置")
	}
	assert.Equal(t, minesweeper.StatusWon, last["status"])
	assert.Len(t, last["mine_cells"], 10)
	assert.NotContains(t, last, "exploded")
	result := last["result"].(map[string]any)
	assert.EqualValues(t, 1, result["score"], "成绩是赢下的难度")
	assert.EqualValues(t, 1, result["reward"])
	assert.Equal(t, "", result["limited_by"])
	assert.EqualValues(t, 1, result["balance"])
	assert.Equal(t, map[string]any{"difficulty": 1.0, "won": true, "opened": 71.0}, result["detail"])

	again := player.ok(http.MethodPost, "/api/game/minesweeper/reveal", map[string]any{"session_id": won.Id, "version": version, "cells": [][2]int{{0, 0}}})
	assert.Equal(t, result, again, "重复提交返回第一次结算的结果")
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/minesweeper/state", nil)["session"])

	// 输一局：第一下之后直接点一颗雷。
	lost := startMinesweeperSession(t, env, player.user.Id, 1, 100)
	data, _ := revealMinesweeper(t, player, lost.Id, 0, [2]int{4, 4})
	require.Equal(t, minesweeper.StatusPlaying, data["status"])
	mine := storedMinesweeper(t, player.user.Id, lost.Id).MineCells()[0]
	data, _ = revealMinesweeper(t, player, lost.Id, 1, mine)
	assert.Equal(t, minesweeper.StatusLost, data["status"])
	assert.Empty(t, data["revealed"])
	assert.Equal(t, []any{float64(mine[0]), float64(mine[1])}, data["exploded"])
	assert.Len(t, data["mine_cells"], 10)
	result = data["result"].(map[string]any)
	assert.EqualValues(t, 0, result["score"])
	assert.EqualValues(t, 0, result["reward"])
	assert.Equal(t, false, result["detail"].(map[string]any)["won"])
	assert.EqualValues(t, 2, result["progress"].(map[string]any)["plays_today"])
	assert.EqualValues(t, 1, result["progress"].(map[string]any)["rewards_today"])
	assert.Equal(t, 1, player.balance(), "踩雷什么都不发")
}

// 翻开请求要带玩家看到的版本号：过期的版本、越界的格子、一次太多格、不是 [x, y] 的格子、别人的对局都拒绝，局面不变，
// 也不会回应任何格子。
func TestMinesweeperRejectsStaleAndInvalidReveals(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, minesweeperSetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	session := startMinesweeperSession(t, env, player.user.Id, 1, 100)
	revealMinesweeper(t, player, session.Id, 0, [2]int{4, 4})
	before := storedMinesweeper(t, player.user.Id, session.Id)
	hidden, ok := minesweeperHiddenSafeCell(before)
	require.True(t, ok)
	tooMany := make([][2]int, minesweeper.MaxRevealCells+1)
	for i := range tooMany {
		tooMany[i] = [2]int{i % 9, 8}
	}

	cases := map[string]struct {
		player *gamePlayer
		body   map[string]any
		want   string
	}{
		"a stale version":         {player: player, body: map[string]any{"session_id": session.Id, "version": 0, "cells": [][2]int{hidden}}, want: i18n.MsgGameRoundChanged},
		"a cell off the board":    {player: player, body: map[string]any{"session_id": session.Id, "version": 1, "cells": [][2]int{hidden, {9, 0}}}, want: i18n.MsgGameMoveInvalid},
		"more cells than a chord": {player: player, body: map[string]any{"session_id": session.Id, "version": 1, "cells": tooMany}, want: i18n.MsgGameMoveInvalid},
		"no cells":                {player: player, body: map[string]any{"session_id": session.Id, "version": 1, "cells": [][2]int{}}, want: i18n.MsgGameMoveInvalid},
		"a cell that is not x,y":  {player: player, body: map[string]any{"session_id": session.Id, "version": 1, "cells": [][]int{{hidden[0]}}}, want: i18n.MsgInvalidParams},
		"someone else's session":  {player: other, body: map[string]any{"session_id": session.Id, "version": 1, "cells": [][2]int{hidden}}, want: i18n.MsgGameSessionNotFound},
	}
	for name, c := range cases {
		message := c.player.fails(http.MethodPost, "/api/game/minesweeper/reveal", c.body)
		assert.Equal(t, i18n.Translate(i18n.DefaultLang, c.want), message, name)
		assert.Equal(t, before, storedMinesweeper(t, player.user.Id, session.Id), name)
	}
}
