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
