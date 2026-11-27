package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogicPuzzleRecoveryOwnershipAndVersions(t *testing.T) {
	for _, key := range []string{"lightsout", "hanoi"} {
		t.Run(key, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Lightsout.Enabled = true; s.Hanoi.Enabled = true }))
			player := env.player(0, gameTestIpA)
			other := env.player(0, gameTestIpB)
			path := "/api/game/" + key
			started := player.ok(http.MethodPost, path+"/start", map[string]any{"difficulty": 1})
			body := map[string]any{"session_id": started["session_id"], "version": started["version"], "action": "move", "cell": 0, "from": 0, "to": 2}
			other.fails(http.MethodPost, path+"/action", body)
			moved := player.ok(http.MethodPost, path+"/action", body)
			assert.Equal(t, 1.0, moved["moves"])
			player.fails(http.MethodPost, path+"/action", body)
			restored := player.ok(http.MethodGet, path+"/state", nil)["session"].(map[string]any)
			assert.Equal(t, moved, restored)
			undone := player.ok(http.MethodPost, path+"/action", map[string]any{"session_id": moved["session_id"], "version": moved["version"], "action": "undo"})
			assert.Equal(t, 0.0, undone["moves"])
			ended := player.ok(http.MethodPost, path+"/action", map[string]any{"session_id": undone["session_id"], "version": undone["version"], "action": "finish"})
			assert.Equal(t, "finished", ended["status"])
			assert.Equal(t, 0.0, ended["result"].(map[string]any)["score"])
		})
	}
}

func TestHanoiCompletionPaysOnceAndCanBeReadAfterLostResponse(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Hanoi = operation_setting.SkillRule{Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}}}
	}))
	player := env.player(0, gameTestIpA)
	view := player.ok(http.MethodPost, "/api/game/hanoi/start", map[string]any{"difficulty": 1})
	id := int(view["session_id"].(float64))
	var request map[string]any
	for _, move := range [][2]int{{0, 2}, {0, 1}, {2, 1}, {0, 2}, {1, 0}, {1, 2}, {0, 2}} {
		request = map[string]any{"session_id": id, "version": view["version"], "action": "move", "from": move[0], "to": move[1]}
		view = player.ok(http.MethodPost, "/api/game/hanoi/action", request)
	}
	assert.Equal(t, true, view["won"])
	assert.Equal(t, 1.0, player.balance())
	repeated := player.ok(http.MethodPost, "/api/game/hanoi/action", request)
	assert.Equal(t, 1.0, repeated["reward"])
	restored := player.ok(http.MethodGet, fmt.Sprintf("/api/game/hanoi/state?session_id=%d", id), nil)["session"].(map[string]any)
	assert.Equal(t, "finished", restored["status"])
	assert.Equal(t, 1.0, restored["result"].(map[string]any)["reward"])
	assert.Equal(t, 1.0, player.balance())
	var count int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND type = ?", player.user.Id, model.GameCoinLogTypeGameReward).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestFloodMoveUndoResetAndFinish(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Flood = operation_setting.SkillRule{Enabled: true, DailyRewards: 1, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}}}
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/flood/start", map[string]any{"difficulty": 1})
	board := started["board"].([]any)
	require.Len(t, board, 64)
	curColor := int(board[0].(float64))
	nextColor := (curColor + 1) % 6

	// 重复当前颜色应被拒绝
	player.fails(http.MethodPost, "/api/game/flood/action", map[string]any{
		"session_id": started["session_id"],
		"version":    started["version"],
		"action":     "move",
		"color":      curColor,
	})

	moved := player.ok(http.MethodPost, "/api/game/flood/action", map[string]any{
		"session_id": started["session_id"],
		"version":    started["version"],
		"action":     "move",
		"color":      nextColor,
	})
	assert.Equal(t, 1.0, moved["moves"])
	assert.Equal(t, float64(nextColor), moved["board"].([]any)[0])

	undone := player.ok(http.MethodPost, "/api/game/flood/action", map[string]any{
		"session_id": moved["session_id"],
		"version":    moved["version"],
		"action":     "undo",
	})
	assert.Equal(t, 0.0, undone["moves"])
	assert.Equal(t, float64(curColor), undone["board"].([]any)[0])

	ended := player.ok(http.MethodPost, "/api/game/flood/action", map[string]any{
		"session_id": undone["session_id"],
		"version":    undone["version"],
		"action":     "finish",
	})
	assert.Equal(t, "finished", ended["status"])
}

func TestPipesRotateUndoAndRejectsBadMoves(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pipes.Enabled = true }))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/pipes/start", map[string]any{"difficulty": 1})
	require.Len(t, started["tiles"].([]any), 25)
	assert.Equal(t, 12.0, started["source"])
	assert.Equal(t, false, started["won"])
	move := func(view map[string]any, cell, dir int) map[string]any {
		return map[string]any{"session_id": view["session_id"], "version": view["version"], "action": "move", "cell": cell, "dir": dir}
	}

	player.fails(http.MethodPost, "/api/game/pipes/action", move(started, 0, 2))
	player.fails(http.MethodPost, "/api/game/pipes/action", move(started, 25, 0))

	turned := player.ok(http.MethodPost, "/api/game/pipes/action", move(started, 0, 0))
	assert.Equal(t, 1.0, turned["moves"])
	assert.Equal(t, 0.0, turned["last"])
	back := player.ok(http.MethodPost, "/api/game/pipes/action", move(turned, 0, 1))
	assert.Equal(t, 2.0, back["moves"])
	assert.Equal(t, started["tiles"], back["tiles"], "a clockwise turn followed by an anticlockwise turn restores the tile")

	undone := player.ok(http.MethodPost, "/api/game/pipes/action", map[string]any{"session_id": back["session_id"], "version": back["version"], "action": "undo", "steps": 2})
	assert.Equal(t, 0.0, undone["moves"])
	assert.Equal(t, started["tiles"], undone["tiles"])
	ended := player.ok(http.MethodPost, "/api/game/pipes/action", map[string]any{"session_id": undone["session_id"], "version": undone["version"], "action": "finish"})
	assert.Equal(t, "finished", ended["status"])
}
