package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/pairlink"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// matchPairlinkTestPair 仅从公开棋盘找一对，再通过真实接口消除。
func matchPairlinkTestPair(t *testing.T, player *gamePlayer, view map[string]any) map[string]any {
	t.Helper()
	data, err := common.Marshal(view)
	require.NoError(t, err)
	var game pairlink.Game
	require.NoError(t, common.Unmarshal(data, &game))
	pair := game.Hint()
	require.Len(t, pair, 2)
	return player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{
		"session_id": view["session_id"], "version": view["version"], "action": "match", "first": pair[0], "second": pair[1],
	})
}

// preparePairlinkPendingReward 按默认规则赢下中级局，成绩达标但尚未累计够每币 120 秒。
func preparePairlinkPendingReward(t *testing.T, env *gameTestEnv, player *gamePlayer) map[string]any {
	t.Helper()
	view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 2})
	now := common.GetTimestamp()
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", view["session_id"]).
		Updates(map[string]any{"created_at": now - 5, "played_at": now - 5}).Error)
	for range 24 {
		view = matchPairlinkTestPair(t, player, view)
	}
	require.Equal(t, "won", view["status"])
	result := view["result"].(map[string]any)
	require.EqualValues(t, 0, result["reward"])
	require.EqualValues(t, 1, result["progress"].(map[string]any)["pending_reward"])
	return result
}

func TestPairlinkUnplayedFinishAndTimeoutDoNotReleasePendingReward(t *testing.T) {
	for _, test := range []struct {
		name    string
		tools   bool
		timeout bool
	}{
		{name: "idle finish"},
		{name: "idle timeout", timeout: true},
		{name: "tools without matches", tools: true},
		{name: "tools timeout without matches", tools: true, timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
			player := env.player(0, gameTestIpA)
			initial := preparePairlinkPendingReward(t, env, player)
			seconds := initial["progress"].(map[string]any)["seconds_today"]
			view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 1})
			id := view["session_id"]
			if test.tools {
				for i, action := range []string{"hint", "shuffle"} {
					now := common.GetTimestamp()
					require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
						Updates(map[string]any{"created_at": now - int64(i+1)*60, "played_at": now - 60}).Error)
					view = player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": view["version"], "action": action})
				}
			}
			now := common.GetTimestamp()
			elapsed := int64(121)
			if test.timeout {
				elapsed = int64(pairlink.Levels[0].Seconds + 1)
			}
			require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
				Updates(map[string]any{"created_at": now - elapsed, "played_at": now - 61}).Error)
			action := "finish"
			if test.timeout {
				action = "hint"
			}
			request := map[string]any{"session_id": id, "version": view["version"], "action": action}
			ended := player.ok(http.MethodPost, "/api/game/pairlink/action", request)
			require.Equal(t, "lost", ended["status"])
			result := ended["result"].(map[string]any)
			progress := result["progress"].(map[string]any)
			assert.EqualValues(t, 0, result["detail"].(map[string]any)["pairs"])
			assert.EqualValues(t, 0, result["reward"])
			assert.Equal(t, seconds, progress["seconds_today"], "结束请求和单独使用工具都不能兑换领奖时长")
			assert.EqualValues(t, 1, progress["pending_reward"])
			assert.EqualValues(t, 1, progress["rewards_today"])
			assert.Zero(t, player.balance())
			again := player.ok(http.MethodPost, "/api/game/pairlink/action", request)
			assert.Equal(t, result, again, "重交不增加局数、时长或奖励")
		})
	}
}

func TestPairlinkWinningMatchIncludesTheFinalPlayInterval(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
	player := env.player(0, gameTestIpA)
	view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 2})
	id := jsonInt(t, view["session_id"])
	now := common.GetTimestamp()
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
		Updates(map[string]any{"created_at": now - 60, "played_at": now - 60}).Error)
	for range 23 {
		view = matchPairlinkTestPair(t, player, view)
	}
	session, err := model.GetUserGameSession(player.user.Id, gamePairlink, id)
	require.NoError(t, err)
	now = common.GetTimestamp()
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
		Updates(map[string]any{"created_at": now - 120, "played_at": now - 60}).Error)
	ended := matchPairlinkTestPair(t, player, view)
	require.Equal(t, "won", ended["status"])
	result := ended["result"].(map[string]any)
	assert.EqualValues(t, session.PlaySeconds+60, result["progress"].(map[string]any)["seconds_today"])
	assert.EqualValues(t, 1, result["reward"], "正常最后一次消除依然累计这一操作间隔")
	assert.Equal(t, 1.0, player.balance())
}

func TestPairlinkPlayedFinishAndTimeoutKeepOnlySavedTime(t *testing.T) {
	for _, test := range []struct {
		name    string
		timeout bool
	}{
		{name: "finish"},
		{name: "timeout", timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
			player := env.player(0, gameTestIpA)
			initial := preparePairlinkPendingReward(t, env, player)
			before := jsonInt(t, initial["progress"].(map[string]any)["seconds_today"])
			view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 2})
			id := view["session_id"]
			// 两次成功消除各自验证 60 秒；最后一次操作后的空等不再累计。
			for i := range 2 {
				now := common.GetTimestamp()
				require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
					Updates(map[string]any{"created_at": now - int64(i+1)*60, "played_at": now - 60}).Error)
				view = matchPairlinkTestPair(t, player, view)
			}
			now := common.GetTimestamp()
			elapsed := int64(181)
			action := "finish"
			if test.timeout {
				elapsed = int64(pairlink.Levels[1].Seconds + 1)
				action = "hint"
			}
			require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
				Updates(map[string]any{"created_at": now - elapsed, "played_at": now - 61}).Error)
			request := map[string]any{"session_id": id, "version": view["version"], "action": action}
			ended := player.ok(http.MethodPost, "/api/game/pairlink/action", request)
			require.Equal(t, "lost", ended["status"])
			result := ended["result"].(map[string]any)
			progress := result["progress"].(map[string]any)
			assert.EqualValues(t, 2, result["detail"].(map[string]any)["pairs"])
			assert.EqualValues(t, 0, result["score"])
			assert.EqualValues(t, before+120, progress["seconds_today"])
			assert.EqualValues(t, 0, progress["pending_reward"])
			assert.EqualValues(t, 1, progress["rewards_today"], "仅补发旧奖励，不新增领奖次数")
			assert.EqualValues(t, 1, result["reward"])
			assert.Equal(t, 1.0, player.balance())
			again := player.ok(http.MethodPost, "/api/game/pairlink/action", request)
			assert.Equal(t, result, again)
			assert.Equal(t, 1.0, player.balance())
		})
	}
}
