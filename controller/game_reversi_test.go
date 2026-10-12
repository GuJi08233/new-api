package controller

import (
	"math/rand/v2"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/reversi"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reversiSetting(ai operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Reversi = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: ai}
	})
}

// 押注对局走真实接口，下一盘最短的对局：黑 9 手把白子全部翻掉(d3 c3 b3 d2 e1 d6 d7 e3 f4)，拿走奖池 30 减去 5% 抽成
// (向下取整为 1)。开局的视图给出黑棋能下的 4 格；夹不住对方棋子的一格、没轮到的一方都被拒绝。
func TestReversiStakedRoomPaysWinnerMinusRake(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, reversiSetting(operation_setting.SkillRule{}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameReversi, "stake": 15})
	roomId := jsonInt(t, created["id"])
	view := guest.ok(http.MethodPost, gameRoomPath(roomId, "join"), nil)
	state := view["state"].(map[string]any)
	assert.ElementsMatch(t, []any{
		map[string]any{"x": 3.0, "y": 2.0}, map[string]any{"x": 2.0, "y": 3.0}, map[string]any{"x": 5.0, "y": 4.0}, map[string]any{"x": 4.0, "y": 5.0},
	}, state["legal"])
	assert.EqualValues(t, -1, state["passed"])
	black, white := host, guest
	if jsonInt(t, state["black"]) == 1 {
		black, white = guest, host
	}

	version := view["version"]
	place := func(x int, y int) map[string]any {
		return map[string]any{"version": version, "move": map[string]any{"x": x, "y": y}}
	}
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomNotYourTurn), white.fails(http.MethodPost, gameRoomPath(roomId, "move"), place(3, 2)))
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameMoveInvalid), black.fails(http.MethodPost, gameRoomPath(roomId, "move"), place(0, 0)), "夹不住任何白子")
	for i, square := range []int{19, 18, 17, 11, 4, 43, 51, 20, 29} {
		mover := black
		if i%2 == 1 {
			mover = white
		}
		view = mover.ok(http.MethodPost, gameRoomPath(roomId, "move"), place(square%reversi.Size, square/reversi.Size))
		version = view["version"]
	}

	assert.Equal(t, model.GameRoomStatusFinished, view["status"])
	assert.EqualValues(t, black.user.Id, view["winner_id"])
	assert.Equal(t, 1.5, view["rake"])
	state = view["state"].(map[string]any)
	assert.Equal(t, true, state["over"])
	assert.EqualValues(t, 13, state["black_discs"])
	assert.EqualValues(t, 0, state["white_discs"])
	assert.Empty(t, state["legal"])
	assert.Equal(t, map[string]any{"x": 5.0, "y": 3.0}, state["last"])
	assert.Equal(t, 113.5, black.balance())
	assert.Equal(t, 85.0, white.balance())
}

// 人机对战走真实接口：玩家每走一步，AI 在同一个请求里回一步(无子可下的一方由引擎跳过)，局面与玩家这边按双方着法重放的
// 一致。玩家这边用困难难度的 AI 下，赢下简单难度后按成绩 1 领到 1 个游戏币。
func TestReversiAiGameSettles(t *testing.T) {
	env := newGameTestEnv(t, reversiSetting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)

	session := player.ok(http.MethodPost, "/api/game/reversi/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	local := reversi.New()
	if last, ok := session["state"].(map[string]any)["last"].(map[string]any); ok {
		require.NoError(t, local.Play(jsonInt(t, last["x"]), jsonInt(t, last["y"])))
	}
	rng := rand.New(rand.NewPCG(1, 1))
	var result map[string]any
	for result == nil {
		point, err := reversi.BestMove(local, 3, rng)
		require.NoError(t, err)
		require.NoError(t, local.Play(point.X, point.Y))
		reply := player.ok(http.MethodPost, "/api/game/reversi/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": point,
		})
		session = reply["session"].(map[string]any)
		if aiMove, ok := reply["ai_move"].(map[string]any); ok {
			require.NoError(t, local.Play(jsonInt(t, aiMove["x"]), jsonInt(t, aiMove["y"])))
		}
		// 这两局里 AI 每次都只回一步，ai_move 就是它走的那一步；AI 连走几步的情形见 TestReversiAiKeepsMovingWhilePlayerPasses。
		require.EqualValues(t, len(local.Moves()), session["moves"])
		require.Equal(t, local.Board(), session["state"].(map[string]any)["board"])
		result, _ = reply["result"].(map[string]any)
	}

	assert.Equal(t, true, session["over"])
	assert.EqualValues(t, 0, session["winner"])
	assert.EqualValues(t, 1, result["score"])
	assert.Equal(t, 1.0, result["reward"])
	assert.Equal(t, 1.0, player.balance())
}

// AI 走完之后玩家无子可下，AI 接着走，直到把棋交还给玩家或者终局，一个请求里完成：玩家不会停在"轮到 AI 走"的局面。
// 两个局面取自随机对局，AI 的每一步都只有一处可下，结果与难度、随机数无关；ai_move 是 AI 最后走的一步。
func TestReversiAiKeepsMovingWhilePlayerPasses(t *testing.T) {
	cases := map[string]struct {
		// black 是执黑的座位，玩家坐座位 0。
		black   int
		moves   []int
		move    reversi.Point
		aiMoves []int
		over    bool
	}{
		// 玩家执黑走 g2，白走 a1 后黑无子可下，白再走 h2，黑又有 h1 可下。
		"the player moves again": {
			black: 0,
			moves: []int{37, 45, 44, 43, 51, 38, 42, 41, 52, 60, 58, 34, 26, 61, 33, 59, 46, 30, 53, 32, 24, 17, 8, 39, 62, 54, 25, 19,
				31, 49, 55, 18, 11, 50, 20, 63, 47, 57, 40, 48, 29, 13, 21, 4, 56, 16, 5, 12, 9, 6, 2, 22, 3, 10, 1, 23},
			move:    reversi.Point{X: 6, Y: 1},
			aiMoves: []int{0, 15},
		},
		// 玩家执白走 a5，黑走 a7 后白无子可下，黑再走 a1 下满棋盘，44:20 黑胜。
		"the game ends": {
			black: 1,
			moves: []int{37, 43, 26, 38, 45, 19, 20, 53, 39, 13, 34, 25, 52, 44, 16, 46, 29, 17, 10, 51, 9, 12, 42, 31, 47, 24, 30, 41,
				61, 3, 59, 62, 1, 8, 60, 54, 21, 58, 6, 33, 55, 14, 50, 57, 4, 23, 49, 11, 15, 5, 7, 63, 2, 18, 22, 40, 56},
			move:    reversi.Point{X: 0, Y: 4},
			aiMoves: []int{48, 0},
			over:    true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newGameTestEnv(t, reversiSetting(boardAiRule()))
			player := env.player(0, gameTestIpA)
			board, err := common.Marshal(reversiState{Black: c.black, Moves: c.moves})
			require.NoError(t, err)
			state, err := common.Marshal(pvpAiState{Level: 3, Board: board, ToMove: 0, Winner: -1, Moves: len(c.moves)})
			require.NoError(t, err)
			session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameReversi), 0, string(state))
			require.NoError(t, err)

			reply := player.ok(http.MethodPost, "/api/game/reversi/ai/move", map[string]any{
				"session_id": session.Id, "version": session.Version, "move": c.move,
			})
			view := reply["session"].(map[string]any)
			local, err := reversi.Replay(append(append(c.moves, c.move.Y*reversi.Size+c.move.X), c.aiMoves...))
			require.NoError(t, err)
			assert.Equal(t, local.Board(), view["state"].(map[string]any)["board"])
			assert.EqualValues(t, len(c.moves)+1+len(c.aiMoves), view["moves"], "AI 连走了两步")
			last := c.aiMoves[len(c.aiMoves)-1]
			assert.Equal(t, map[string]any{"x": float64(last % reversi.Size), "y": float64(last / reversi.Size)}, reply["ai_move"])
			assert.Equal(t, c.over, view["over"])
			if !c.over {
				assert.EqualValues(t, 0, view["to_move"])
				assert.NotEmpty(t, view["state"].(map[string]any)["legal"])
				assert.Nil(t, reply["result"])
				return
			}
			assert.EqualValues(t, 1, view["winner"])
			result := reply["result"].(map[string]any)
			assert.EqualValues(t, 0, result["score"], "输给 AI 成绩为 0")
			assert.Equal(t, 0.0, result["reward"])
		})
	}
}
