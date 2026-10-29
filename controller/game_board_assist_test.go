package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gomoku"
	"github.com/QuantumNous/new-api/pkg/reversi"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 悔棋撤到提出的一方最后一次落子之前：自己刚走完撤一步，对方已经应了撤两步，自己被跳过、对方连走几步时一并撤掉；
// 还没落过子的一方没有可撤的棋。
func TestBoardTakebackRewindsToTheAskersLastMove(t *testing.T) {
	cases := map[string]struct {
		movers []int
		seat   int
		steps  int
	}{
		"own move just played":     {movers: []int{0, 1, 0}, seat: 0, steps: 1},
		"opponent already replied": {movers: []int{0, 1, 0, 1}, seat: 0, steps: 2},
		"opponent moved twice":     {movers: []int{0, 1, 0, 1, 1}, seat: 0, steps: 3},
		"no own move yet":          {movers: []int{1}, seat: 0, steps: 0},
		"empty board":              {movers: []int{}, seat: 1, steps: 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.steps, boardTakeback(c.movers, c.seat))
		})
	}
}

// 人机四子棋的提示与悔棋走真实接口：提示不落子、同一局面重复请求返回同一个结果且不多扣次数，推荐的列是能投的一列；
// 悔棋撤回玩家的一步和 AI 的应着，回到玩家走之前的局面。每样前 3 次免费，第 4 次各收 1 个游戏币，余额不够时局面不变。
func TestConnect4AiHintAndUndo(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(2, gameTestIpA)
	body := func(s map[string]any) map[string]any {
		return map[string]any{"session_id": s["session_id"], "version": s["version"]}
	}

	session := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	player.fails(http.MethodPost, "/api/game/connect4/ai/undo", body(session))
	for round := 1; round <= 4; round++ {
		before := session["state"].(map[string]any)
		assert.EqualValues(t, max(3-(round-1), 0), session["hint"].(map[string]any)["free_left"], "第 %d 次提示之前", round)
		hinted := player.ok(http.MethodPost, "/api/game/connect4/ai/hint", body(session))
		hint := hinted["hint"].(map[string]any)
		col := jsonInt(t, hint["x"])
		assert.EqualValues(t, before["moves"], hint["moves"])
		assert.Equal(t, byte('.'), before["board"].(string)[col], "推荐的第 %d 列还能投", col)
		after := hinted["session"].(map[string]any)
		assert.Equal(t, before, after["state"], "提示不改动棋盘")
		repeated := player.ok(http.MethodPost, "/api/game/connect4/ai/hint", body(session))
		assert.Equal(t, hint, repeated["hint"])
		assert.Equal(t, after["version"], repeated["session"].(map[string]any)["version"], "重复请求不再记次数")

		moved := player.ok(http.MethodPost, "/api/game/connect4/ai/move", map[string]any{
			"session_id": after["session_id"], "version": after["version"], "move": map[string]any{"col": col},
		})["session"].(map[string]any)
		assert.Nil(t, moved["hint_result"], "落子之后旧提示作废")
		assert.EqualValues(t, max(3-(round-1), 0), moved["state"].(map[string]any)["undo"].(map[string]any)["free_left"])
		session = player.ok(http.MethodPost, "/api/game/connect4/ai/undo", body(moved))["session"].(map[string]any)
		assert.Equal(t, before["board"], session["state"].(map[string]any)["board"], "第 %d 次悔棋回到走之前", round)
		assert.EqualValues(t, 0, session["to_move"])
	}
	assert.Equal(t, 0.0, player.balance(), "第 4 次提示与第 4 次悔棋各收了 1 个游戏币")
	for _, remark := range []string{"connect4_ai:hint", "connect4_ai:undo"} {
		var fee model.GameCoinLog
		require.NoError(t, env.db.Where("user_id = ? AND remark = ?", player.user.Id, remark).First(&fee).Error)
		assert.Equal(t, -1.0, coinsOf(t, fee.Amount), remark)
	}

	player.fails(http.MethodPost, "/api/game/connect4/ai/hint", body(session))
	latest := player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"].(map[string]any)
	assert.Equal(t, session["version"], latest["version"], "余额不够时局面不变")
}

// 黑白棋里玩家走完一步后自己无子可下、AI 连走了两步：悔棋把这三步一起撤掉，回到玩家走之前、又轮到玩家。
func TestReversiAiUndoTakesBackMovesAfterAPass(t *testing.T) {
	env := newGameTestEnv(t, reversiSetting(boardAiRule()))
	player := env.player(0, gameTestIpA)
	moves := []int{37, 45, 44, 43, 51, 38, 42, 41, 52, 60, 58, 34, 26, 61, 33, 59, 46, 30, 53, 32, 24, 17, 8, 39, 62, 54, 25, 19,
		31, 49, 55, 18, 11, 50, 20, 63, 47, 57, 40, 48, 29, 13, 21, 4, 56, 16, 5, 12, 9, 6, 2, 22, 3, 10, 1, 23}
	board, err := common.Marshal(reversiState{Black: 0, Moves: moves})
	require.NoError(t, err)
	state, err := common.Marshal(pvpAiState{Level: 3, Board: board, ToMove: 0, Winner: -1, Moves: len(moves)})
	require.NoError(t, err)
	session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameReversi), 0, string(state))
	require.NoError(t, err)
	before, err := reversi.Replay(moves)
	require.NoError(t, err)

	moved := player.ok(http.MethodPost, "/api/game/reversi/ai/move", map[string]any{
		"session_id": session.Id, "version": session.Version, "move": reversi.Point{X: 6, Y: 1},
	})["session"].(map[string]any)
	require.EqualValues(t, len(moves)+3, moved["moves"], "AI 连走了两步")

	undone := player.ok(http.MethodPost, "/api/game/reversi/ai/undo", map[string]any{
		"session_id": moved["session_id"], "version": moved["version"],
	})["session"].(map[string]any)
	view := undone["state"].(map[string]any)
	assert.Equal(t, before.Board(), view["board"])
	assert.EqualValues(t, len(moves), view["moves"])
	assert.EqualValues(t, 0, undone["to_move"])
	assert.EqualValues(t, 1, view["undo"].(map[string]any)["used"])
}

// 押注房间里的五子棋悔棋要对方同意：提出的一方不能自己同意，被拒绝后再走一步之前不能再提；对方已经应了一步时同意，
// 两步一起撤回，又轮到提出的一方。
func TestGomokuRoomUndoNeedsTheOpponent(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60}
	}))
	host, guest := env.player(20, gameTestIpA), env.player(20, gameTestIpB)
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 10})
	roomId := jsonInt(t, created["id"])
	joined := guest.ok(http.MethodPost, gameRoomPath(roomId, "join"), nil)
	black, white := host, guest
	if jsonInt(t, joined["state"].(map[string]any)["black"]) == 1 {
		black, white = guest, host
	}
	blackSeat := jsonInt(t, joined["state"].(map[string]any)["black"])
	move := func(version any, x int, y int) map[string]any {
		return map[string]any{"version": version, "move": map[string]any{"x": x, "y": y}}
	}

	black.fails(http.MethodPost, gameRoomPath(roomId, "undo"), nil)
	moved := black.ok(http.MethodPost, gameRoomPath(roomId, "move"), move(joined["version"], 7, 7))
	asked := black.ok(http.MethodPost, gameRoomPath(roomId, "undo"), nil)
	assert.EqualValues(t, blackSeat, asked["state"].(map[string]any)["undo"].(map[string]any)["pending_by"])
	black.fails(http.MethodPost, gameRoomPath(roomId, "undo/answer"), map[string]any{"accept": true})

	declined := white.ok(http.MethodPost, gameRoomPath(roomId, "undo/answer"), map[string]any{"accept": false})
	assert.Equal(t, moved["state"].(map[string]any)["board"], declined["state"].(map[string]any)["board"])
	assert.Equal(t, true, black.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", roomId), nil)["state"].(map[string]any)["undo"].(map[string]any)["declined"])
	black.fails(http.MethodPost, gameRoomPath(roomId, "undo"), nil)

	white.ok(http.MethodPost, gameRoomPath(roomId, "move"), move(declined["version"], 8, 8))
	black.ok(http.MethodPost, gameRoomPath(roomId, "undo"), nil)
	accepted := white.ok(http.MethodPost, gameRoomPath(roomId, "undo/answer"), map[string]any{"accept": true})
	view := accepted["state"].(map[string]any)
	assert.Equal(t, strings.Repeat(".", gomoku.Squares), view["board"])
	assert.EqualValues(t, blackSeat, accepted["to_move"])
	assert.Nil(t, view["last"])
	assert.Equal(t, 10.0, black.balance(), "免费悔棋不扣游戏币")
	assert.Equal(t, 10.0, white.balance())
}
