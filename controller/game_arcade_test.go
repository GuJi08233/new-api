package controller

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/breakout"
	"github.com/QuantumNous/new-api/pkg/pairlink"
	"github.com/QuantumNous/new-api/pkg/sokoban"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairlinkActionsRestoreAndSettleOnlyOnce(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
	player := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	player.fails(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 4})
	view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 2})
	id := int(view["session_id"].(float64))
	// 已经玩了两分钟：开局时间与按操作累计的游戏时长一起往前挪。
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).
		Updates(map[string]any{"created_at": common.GetTimestamp() - 121, "play_seconds": 121}).Error)
	restored := player.ok(http.MethodGet, "/api/game/pairlink/state", nil)["session"].(map[string]any)
	assert.Equal(t, view["board"], restored["board"])
	other.fails(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": 0, "action": "hint"})
	view = player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": 0, "action": "hint"})
	assert.EqualValues(t, 2, view["hints"])
	assert.Len(t, view["hint"], 2)
	player.fails(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": 0, "action": "shuffle"})
	player.fails(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": view["version"], "action": "match", "first": -1, "second": 1})
	for range 24 {
		session, err := model.GetUserGameSession(player.user.Id, gamePairlink, id)
		require.NoError(t, err)
		var game pairlink.Game
		require.NoError(t, common.UnmarshalJsonStr(session.State, &game))
		pair := game.Hint()
		require.Len(t, pair, 2)
		view = player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "version": session.Version, "action": "match", "first": pair[0], "second": pair[1]})
	}
	assert.Equal(t, "won", view["status"])
	settled := view["result"].(map[string]any)
	assert.EqualValues(t, 2, settled["score"])
	assert.Equal(t, 1.0, settled["reward"])
	again := player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": id, "action": "finish"})
	assert.Equal(t, settled, again)
	assert.Equal(t, 1.0, player.balance())
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/pairlink/state", nil)["session"])
}

func TestPairlinkTimeoutAndToolLimits(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
	player := env.player(0, gameTestIpA)
	view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 1})
	for _, action := range []string{"hint", "shuffle"} {
		for range 3 {
			view = player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": view["session_id"], "version": view["version"], "action": action})
		}
		player.fails(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": view["session_id"], "version": view["version"], "action": action})
	}
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", view["session_id"]).
		Updates(map[string]any{"created_at": common.GetTimestamp() - 181, "play_seconds": 200}).Error)
	ended := player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": view["session_id"], "version": view["version"], "action": "hint"})
	assert.Equal(t, "lost", ended["status"])
	assert.Equal(t, 0.0, ended["result"].(map[string]any)["reward"])
	assert.EqualValues(t, 180, ended["result"].(map[string]any)["progress"].(map[string]any)["seconds_today"], "游戏时长不超过这一档的限时")
}

// 开局与现在都按整秒计，到点之后的那一秒仍然算数：页面倒计时还剩时间时发出的最后一步不会被判输；再晚一秒就判输。
func TestPairlinkAllowsTheLastSecond(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Pairlink.Enabled = true }))
	player := env.player(0, gameTestIpA)
	view := player.ok(http.MethodPost, "/api/game/pairlink/start", map[string]any{"difficulty": 1})
	limit := int64(pairlink.Levels[0].Seconds)

	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", view["session_id"]).Update("created_at", common.GetTimestamp()-limit).Error)
	hinted := player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": view["session_id"], "version": view["version"], "action": "hint"})
	assert.Equal(t, "playing", hinted["status"])

	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", view["session_id"]).Update("created_at", common.GetTimestamp()-limit-1).Error)
	ended := player.ok(http.MethodPost, "/api/game/pairlink/action", map[string]any{"session_id": view["session_id"], "version": hinted["version"], "action": "hint"})
	assert.Equal(t, "lost", ended["status"])
}

func TestSokobanDailyLayoutAndReplaySettlement(t *testing.T) {
	require.NoError(t, i18n.Init())
	// 这里只看关卡与重放结算，不要求累计时长。
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Sokoban.Enabled = true
		s.Sokoban.MinSecondsPerCoin = 0
	}))
	player := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	first := player.ok(http.MethodPost, "/api/game/sokoban/start", nil)
	second := other.ok(http.MethodPost, "/api/game/sokoban/start", nil)
	assert.Equal(t, first["puzzle"], second["puzzle"], "每日关卡对所有玩家一致")
	assert.NotContains(t, first["puzzle"], "solution")
	restored := player.ok(http.MethodGet, "/api/game/sokoban/state", nil)["session"].(map[string]any)
	assert.Equal(t, first["puzzle"], restored["puzzle"])
	other.fails(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": first["session_id"], "abandon": true})

	// 一步可过关的固定局面，测试接口是否真的重放而非接受客户端成绩。
	game := &sokoban.Game{Day: gameCenterDay(), Width: 5, Height: 4, Walls: []int{0, 1, 2, 3, 4, 5, 9, 10, 14, 15, 16, 17, 18, 19}, Player: 6, Boxes: []int{7}, Goals: []int{8}}
	state, err := common.Marshal(game)
	require.NoError(t, err)
	id := first["session_id"]
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).Updates(map[string]any{"state": string(state), "created_at": common.GetTimestamp() - 181}).Error)
	for _, moves := range []string{"", "U", "RR", "X"} {
		player.fails(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": id, "moves": moves, "score": 99999})
	}
	result := player.ok(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": id, "moves": "R"})
	assert.EqualValues(t, 1, result["score"])
	assert.Equal(t, 1.0, result["reward"])
	assert.EqualValues(t, 1, result["detail"].(map[string]any)["pushes"])
	again := player.ok(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": id, "moves": "R"})
	assert.Equal(t, result, again)
	assert.Equal(t, 1.0, player.balance())
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/sokoban/state", nil)["session"])
	quit := other.ok(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": second["session_id"], "abandon": true})
	assert.EqualValues(t, 0, quit["score"])
	assert.Equal(t, 0.0, quit["reward"])
}

// 每日推箱子的奖励记在结算的这一天：前一天开局、存着前一天那张图的一局，过了零点再交解法也不算，页面也不再恢复它，
// 不然一张图的解法能领两天的奖励。
func TestSokobanRejectsYesterdaysMap(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Sokoban.Enabled = true
		s.Sokoban.MinSecondsPerCoin = 0
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/sokoban/start", nil)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	game := &sokoban.Game{Day: yesterday, Width: 5, Height: 4, Walls: []int{0, 1, 2, 3, 4, 5, 9, 10, 14, 15, 16, 17, 18, 19}, Player: 6, Boxes: []int{7}, Goals: []int{8}}
	state, err := common.Marshal(game)
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", started["session_id"]).Update("state", string(state)).Error)

	assert.Equal(t, gameMessage(i18n.MsgGameSessionExpired), player.fails(http.MethodPost, "/api/game/sokoban/finish", map[string]any{"session_id": started["session_id"], "moves": "R"}))
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/sokoban/state", nil)["session"])
	assert.Zero(t, player.balance())
}

func TestBreakoutReplayedScoreAndSharedRewardLimit(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Breakout.Enabled = true; s.FreeDailyLimit = 1 }))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/breakout/start", nil)
	session, err := model.GetUserGameSession(player.user.Id, gameBreakout, int(started["session_id"].(float64)))
	require.NoError(t, err)
	assert.EqualValues(t, session.Seed, started["seed"])
	for _, inputs := range [][]int{nil, {-1}, {breakout.Width + 1}, make([]int, breakout.MaxTicks+1), make([]int, 600)} {
		player.fails(http.MethodPost, "/api/game/breakout/finish", map[string]any{"session_id": session.Id, "inputs": inputs})
	}
	data, err := os.ReadFile("../pkg/breakout/testdata/golden.json")
	require.NoError(t, err)
	var records []struct {
		Seed   uint32
		Inputs []int
		Game   breakout.Game
	}
	require.NoError(t, common.Unmarshal(data, &records))
	require.GreaterOrEqual(t, len(records), 2)
	golden := records[1]
	require.NoError(t, env.db.Model(session).Updates(map[string]any{"seed": golden.Seed, "created_at": common.GetTimestamp() - 301}).Error)
	result := player.ok(http.MethodPost, "/api/game/breakout/finish", map[string]any{"session_id": session.Id, "inputs": golden.Inputs, "score": 99999})
	assert.EqualValues(t, golden.Game.Score, result["score"])
	assert.Equal(t, 1.0, result["reward"])
	again := player.ok(http.MethodPost, "/api/game/breakout/finish", map[string]any{"session_id": session.Id, "inputs": []int{0}})
	assert.Equal(t, result, again)
	assert.Equal(t, 1.0, player.balance())

	// 新增技巧游戏同样占用原来的免费奖励池。
	otherGame, err := model.CreateGameSession(player.user.Id, gameSokoban, 1, "")
	require.NoError(t, err)
	limited, err := model.FinishGameSession(otherGame, model.GameSettlement{Score: 1, Reward: 1, Seconds: 180, SecondsPerCoin: 180, DailyRewards: 1, FreeDailyLimit: 1, Day: gameCenterDay(), Ip: gameTestIpA})
	require.NoError(t, err)
	assert.Zero(t, limited.Session.Reward)
	assert.Equal(t, model.GameRewardLimitedByTotal, limited.Session.LimitedBy)
}

// 对局结束后追加挡板位置不能把空闲时间当成游戏时间领取奖励。
func TestBreakoutDoesNotCreditFramesAfterLoss(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Breakout.Enabled = true
		s.Breakout.Tiers = []operation_setting.RewardTier{{Score: 1, Coins: 1}}
	}))
	player := env.player(0, gameTestIpA)
	session, err := model.CreateGameSession(player.user.Id, gameBreakout, 1, "")
	require.NoError(t, err)
	require.NoError(t, env.db.Model(session).Update("created_at", common.GetTimestamp()-181).Error)
	inputs := make([]int, 180*breakout.FPS)
	for i := range inputs {
		inputs[i] = 30
	}
	result := player.ok(http.MethodPost, "/api/game/breakout/finish", map[string]any{"session_id": session.Id, "inputs": inputs})
	assert.EqualValues(t, 35, result["score"])
	assert.EqualValues(t, 10, result["progress"].(map[string]any)["seconds_today"])
	assert.Equal(t, 0.0, result["reward"])
	assert.Equal(t, model.GameRewardLimitedByTime, result["limited_by"])
}
