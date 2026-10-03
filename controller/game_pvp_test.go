package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPvpAiRestoreCommittedMoveAfterStaleVersion(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	request := map[string]any{"session_id": started["session_id"], "version": started["version"], "move": map[string]any{"col": 0}}
	committed := player.ok(http.MethodPost, "/api/game/connect4/ai/move", request)["session"].(map[string]any)
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/connect4/ai/move", request))

	path := fmt.Sprintf("/api/game/connect4/ai?session_id=%d", jsonInt(t, started["session_id"]))
	restored := player.ok(http.MethodGet, path, nil)["session"].(map[string]any)
	assert.Equal(t, committed, restored)
	assert.Greater(t, jsonInt(t, restored["version"]), jsonInt(t, started["version"]))
	request["version"] = restored["version"]
	continued := player.ok(http.MethodPost, "/api/game/connect4/ai/move", request)["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusPlaying, continued["status"])
	assert.EqualValues(t, jsonInt(t, restored["moves"])+2, continued["moves"])
	assert.Equal(t, continued, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}

func TestPvpAiRestoreWinningResultDoesNotRewardAgain(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	// 玩家执黑已有横向四子，双方棋谱合法，下一手连五即结束，不需要 AI 搜索。
	board, err := common.Marshal(gomokuState{Black: 0, Moves: []int{0, 15, 1, 16, 2, 17, 3, 18}})
	require.NoError(t, err)
	state, err := common.Marshal(pvpAiState{Level: 1, Board: board, ToMove: 0, Winner: -1, Moves: 8})
	require.NoError(t, err)
	session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameGomoku), 1, string(state))
	require.NoError(t, err)
	finished := player.ok(http.MethodPost, "/api/game/gomoku/ai/move", map[string]any{
		"session_id": session.Id, "version": session.Version, "move": map[string]any{"x": 4, "y": 0},
	})
	result := finished["result"].(map[string]any)
	require.EqualValues(t, 1, result["reward"])
	path := fmt.Sprintf("/api/game/gomoku/ai?session_id=%d", session.Id)
	for range 2 {
		restored := player.ok(http.MethodGet, path, nil)
		view := restored["session"].(map[string]any)
		assert.Equal(t, model.GameSessionStatusFinished, view["status"])
		assert.Equal(t, true, view["over"])
		assert.EqualValues(t, 0, view["winner"])
		assert.Equal(t, finished["session"].(map[string]any)["state"], view["state"])
		assert.Equal(t, result, restored["result"])
	}
	assert.Equal(t, 1, player.balance())
	progress, err := model.GetGameRewardProgress(player.user.Id, gameTestIpA, pvpAiGame(gameGomoku), gameCenterDay())
	require.NoError(t, err)
	assert.Equal(t, 1, progress.Plays)
	assert.Equal(t, 1, progress.Rewards)
	var rewards int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND type = ?", player.user.Id, model.GameCoinLogTypeGameReward).Count(&rewards).Error)
	assert.EqualValues(t, 1, rewards)
}

func TestPvpAiRestoreResignedSessionWhileAnotherIsPlaying(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	finished := player.ok(http.MethodPost, "/api/game/connect4/ai/resign", map[string]any{"session_id": started["session_id"], "version": started["version"]})
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
	newer := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	path := fmt.Sprintf("/api/game/connect4/ai?session_id=%d", jsonInt(t, started["session_id"]))
	restored := player.ok(http.MethodGet, path, nil)
	view := restored["session"].(map[string]any)
	assert.Equal(t, started["session_id"], view["session_id"])
	assert.Equal(t, model.GameSessionStatusFinished, view["status"])
	assert.Equal(t, true, view["over"])
	assert.EqualValues(t, 1, view["winner"])
	assert.Equal(t, finished["session"].(map[string]any)["state"], view["state"])
	assert.Equal(t, finished["result"], restored["result"])
	assert.Equal(t, newer, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
	assert.Zero(t, player.balance())
}

func TestPvpAiRestoreRejectsUnownedAndInvalidSessions(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player, other := env.player(0, gameTestIpA), env.player(0, gameTestIpB)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	id := jsonInt(t, started["session_id"])
	missing := i18n.Translate(i18n.DefaultLang, i18n.MsgGameSessionNotFound)
	assert.Equal(t, missing, other.fails(http.MethodGet, fmt.Sprintf("/api/game/connect4/ai?session_id=%d", id), nil))
	assert.Equal(t, missing, player.fails(http.MethodGet, fmt.Sprintf("/api/game/gomoku/ai?session_id=%d", id), nil))
	for _, invalid := range []string{"", "0", "-1", "abc", "9223372036854775808", "999999999"} {
		assert.Equal(t, missing, player.fails(http.MethodGet, "/api/game/connect4/ai?session_id="+invalid, nil), "session_id=%q", invalid)
	}
	assert.Equal(t, started, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}

func TestPvpAiRestoreExpiredSessionReturnsNoSession(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	id := jsonInt(t, started["session_id"])
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).Update("created_at", common.GetTimestamp()-skillSessionTTL-1).Error)
	assert.Nil(t, player.ok(http.MethodGet, fmt.Sprintf("/api/game/connect4/ai?session_id=%d", id), nil)["session"])
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}
