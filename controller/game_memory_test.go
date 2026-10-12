package controller

import (
	"net/http"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/memory"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memorySetting(rule operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(setting *operation_setting.GameSetting) { setting.Memory = rule })
}

// startMemorySession 直接建一局用固定密钥洗牌的初级记忆翻牌(4×4、8 对、20 步)，并把开局时间往前拨 startedAgo 秒；
// 返回的牌面让测试知道每张牌是什么。
func startMemorySession(t *testing.T, env *gameTestEnv, userId int, startedAgo int64) (*model.GameSession, *memory.Game) {
	t.Helper()
	game, err := memory.New(1, [32]byte{3})
	require.NoError(t, err)
	state, err := common.Marshal(game)
	require.NoError(t, err)
	session, err := model.CreateGameSession(userId, gameMemory, 0, string(state))
	require.NoError(t, err)
	// 开局 startedAgo 秒前，之后一直在玩：游戏时长按操作累计，只把开局时间往前挪不算玩过。
	require.NoError(t, env.db.Model(session).Updates(map[string]any{"created_at": session.CreatedAt - startedAgo, "play_seconds": startedAgo}).Error)
	return session, game
}

func storedMemory(t *testing.T, userId int, sessionId int) *memory.Game {
	t.Helper()
	session, err := model.GetUserGameSession(userId, gameMemory, sessionId)
	require.NoError(t, err)
	var game memory.Game
	require.NoError(t, common.UnmarshalJsonStr(session.State, &game))
	return &game
}

// flipMemory 翻开一张牌，要求成功，返回 data 与响应原文。
func flipMemory(t *testing.T, player *gamePlayer, sessionId int, version int, index int) (map[string]any, string) {
	t.Helper()
	recorder := player.send(http.MethodPost, "/api/game/memory/flip", map[string]any{"session_id": sessionId, "version": version, "index": index})
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"], response["message"])
	return response["data"].(map[string]any), recorder.Body.String()
}

// memoryPartner 返回与 index 同图案的另一张牌。
func memoryPartner(layout []int, index int) int {
	for other, symbol := range layout {
		if other != index && symbol == layout[index] {
			return other
		}
	}
	return -1
}

// 开局只给出牌面大小、对数与步数上限，看不到任何一张牌。每翻一张只回应这一张的图案，一步里的第二张再告诉配没配上；
// 刷新页面后的局面里只有配上的牌和这一步翻开的第一张。整副牌在一局结束之前不会出现在任何响应里。
func TestMemoryRevealsOnlyFlippedCards(t *testing.T) {
	env := newGameTestEnv(t, memorySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/memory", nil)
	assert.EqualValues(t, memory.SymbolCount, info["symbols"])
	levels := info["levels"].([]any)
	require.Len(t, levels, 3)
	assert.Equal(t, map[string]any{"difficulty": 2.0, "rows": 4.0, "cols": 6.0, "pairs": 12.0, "move_limit": 32.0}, levels[1])
	player.fails(http.MethodPost, "/api/game/memory/start", map[string]any{"difficulty": 0})

	recorder := player.send(http.MethodPost, "/api/game/memory/start", map[string]any{"difficulty": 3})
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"], response["message"])
	started := response["data"].(map[string]any)
	assert.EqualValues(t, 6, started["rows"])
	assert.EqualValues(t, 6, started["cols"])
	assert.EqualValues(t, 18, started["pairs"])
	assert.EqualValues(t, 50, started["move_limit"])
	assert.EqualValues(t, 0, started["moves"])
	assert.EqualValues(t, 0, started["version"])
	assert.Empty(t, started["matched"])
	assert.Nil(t, started["face_up"])
	assert.NotContains(t, recorder.Body.String(), "layout")
	sessionId := int(started["session_id"].(float64))
	layout := storedMemory(t, player.user.Id, sessionId).Layout
	partner := memoryPartner(layout, 0)
	stranger := slices.IndexFunc(layout, func(symbol int) bool { return symbol != layout[0] })

	first, raw := flipMemory(t, player, sessionId, 0, 0)
	assert.Equal(t, map[string]any{
		"session_id": float64(sessionId), "version": 1.0, "index": 0.0, "symbol": float64(layout[0]), "first": true,
		"moves": 0.0, "move_limit": 50.0, "status": memory.StatusPlaying,
	}, first)
	assert.NotContains(t, raw, "layout")
	state := player.ok(http.MethodGet, "/api/game/memory/state", nil)["session"].(map[string]any)
	assert.Equal(t, map[string]any{"index": 0.0, "symbol": float64(layout[0])}, state["face_up"])
	assert.Empty(t, state["matched"])

	hit, _ := flipMemory(t, player, sessionId, 1, partner)
	assert.Equal(t, false, hit["first"])
	assert.Equal(t, true, hit["matched"])
	assert.Equal(t, map[string]any{"index": 0.0, "symbol": float64(layout[0])}, hit["previous"])
	assert.EqualValues(t, 1, hit["moves"])

	// 再翻两张不同的牌：没配上，刷新后都翻回去了。
	third := slices.IndexFunc(layout, func(symbol int) bool { return symbol != layout[0] && symbol != layout[stranger] })
	flipMemory(t, player, sessionId, 2, stranger)
	miss, raw := flipMemory(t, player, sessionId, 3, third)
	assert.Equal(t, false, miss["matched"])
	assert.Equal(t, map[string]any{"index": float64(stranger), "symbol": float64(layout[stranger])}, miss["previous"])
	assert.NotContains(t, raw, "layout")

	recorder = player.send(http.MethodGet, "/api/game/memory/state", nil)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	state = response["data"].(map[string]any)["session"].(map[string]any)
	pair := []any{[]any{0.0, float64(layout[0])}, []any{float64(partner), float64(layout[0])}}
	assert.Equal(t, pair, state["matched"], "只有配上的一对朝上，按位置排列")
	assert.Nil(t, state["face_up"])
	assert.EqualValues(t, 2, state["moves"])
	assert.EqualValues(t, 4, state["version"])
	assert.NotContains(t, recorder.Body.String(), "layout")
}

// 在步数上限之内配完所有对就赢了，按赢下的难度领这一档的奖励，响应里公开整副牌；用完 20 步还没配完就输了，按成绩 0 结算、
// 什么都不发。已经结束的一局再翻牌，拿到的是第一次结算的结果。
func TestMemorySettlesWinsAndLosses(t *testing.T) {
	env := newGameTestEnv(t, memorySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}, {Score: 3, Coins: 2}}, MinSecondsPerCoin: 30,
	}))
	player := env.player(0, gameTestIpA)

	won, game := startMemorySession(t, env, player.user.Id, 100)
	version := 0
	var last map[string]any
	for index := range game.Layout {
		partner := memoryPartner(game.Layout, index)
		if partner < index {
			continue
		}
		flipMemory(t, player, won.Id, version, index)
		last, _ = flipMemory(t, player, won.Id, version+1, partner)
		version += 2
	}
	assert.Equal(t, memory.StatusWon, last["status"])
	assert.EqualValues(t, 8, last["moves"])
	layout := make([]any, 0, len(game.Layout))
	for _, symbol := range game.Layout {
		layout = append(layout, float64(symbol))
	}
	assert.Equal(t, layout, last["layout"], "结束后公开整副牌")
	result := last["result"].(map[string]any)
	assert.EqualValues(t, 1, result["score"], "成绩是赢下的难度")
	assert.EqualValues(t, 1, result["reward"])
	assert.Equal(t, "", result["limited_by"])
	assert.Equal(t, map[string]any{"difficulty": 1.0, "won": true, "moves": 8.0}, result["detail"])
	again := player.ok(http.MethodPost, "/api/game/memory/flip", map[string]any{"session_id": won.Id, "version": version, "index": 0})
	assert.Equal(t, result, again, "重复提交返回第一次结算的结果")
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/memory/state", nil)["session"])

	lost, game := startMemorySession(t, env, player.user.Id, 100)
	stranger := slices.IndexFunc(game.Layout, func(symbol int) bool { return symbol != game.Layout[0] })
	for move := 1; move <= game.MoveLimit; move++ {
		flipMemory(t, player, lost.Id, 2*(move-1), 0)
		last, _ = flipMemory(t, player, lost.Id, 2*move-1, stranger)
		assert.EqualValues(t, move, last["moves"])
		if move < game.MoveLimit {
			assert.Equal(t, memory.StatusPlaying, last["status"], "move %d", move)
			assert.NotContains(t, last, "layout")
		}
	}
	assert.Equal(t, memory.StatusLost, last["status"])
	assert.Len(t, last["layout"], 16)
	result = last["result"].(map[string]any)
	assert.EqualValues(t, 0, result["score"])
	assert.EqualValues(t, 0, result["reward"])
	assert.Equal(t, map[string]any{"difficulty": 1.0, "won": false, "moves": 20.0}, result["detail"])
	assert.Equal(t, 1, player.balance(), "输了什么都不发")
}

// 翻牌请求要带玩家看到的版本号：过期的版本、正翻开着的牌、已经配上的牌、越界的位置、别人的对局都拒绝，局面不变，
// 也不会回应任何一张牌的图案。
func TestMemoryRejectsStaleAndInvalidFlips(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, memorySetting(operation_setting.SkillRule{
		Enabled: true, DailyRewards: 3, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}},
	}))
	player := env.player(0, gameTestIpA)
	other := env.player(0, gameTestIpB)
	session, game := startMemorySession(t, env, player.user.Id, 100)
	partner := memoryPartner(game.Layout, 0)
	matched := slices.IndexFunc(game.Layout, func(symbol int) bool { return symbol != game.Layout[0] })
	flipMemory(t, player, session.Id, 0, matched)
	flipMemory(t, player, session.Id, 1, memoryPartner(game.Layout, matched))
	flipMemory(t, player, session.Id, 2, 0)
	before := storedMemory(t, player.user.Id, session.Id)

	cases := map[string]struct {
		player *gamePlayer
		body   map[string]any
		want   string
	}{
		"a stale version":        {player: player, body: map[string]any{"session_id": session.Id, "version": 2, "index": partner}, want: i18n.MsgGameRoundChanged},
		"the card already up":    {player: player, body: map[string]any{"session_id": session.Id, "version": 3, "index": 0}, want: i18n.MsgGameMoveInvalid},
		"a matched card":         {player: player, body: map[string]any{"session_id": session.Id, "version": 3, "index": matched}, want: i18n.MsgGameMoveInvalid},
		"past the last card":     {player: player, body: map[string]any{"session_id": session.Id, "version": 3, "index": 16}, want: i18n.MsgGameMoveInvalid},
		"before the first card":  {player: player, body: map[string]any{"session_id": session.Id, "version": 3, "index": -1}, want: i18n.MsgGameMoveInvalid},
		"someone else's session": {player: other, body: map[string]any{"session_id": session.Id, "version": 3, "index": partner}, want: i18n.MsgGameSessionNotFound},
	}
	for name, c := range cases {
		message := c.player.fails(http.MethodPost, "/api/game/memory/flip", c.body)
		assert.Equal(t, i18n.Translate(i18n.DefaultLang, c.want), message, name)
		assert.Equal(t, before, storedMemory(t, player.user.Id, session.Id), name)
	}
}
