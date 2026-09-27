package controller

import (
	"math/rand/v2"
	"net/http"
	"testing"

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
	assert.EqualValues(t, 1, view["rake"])
	state = view["state"].(map[string]any)
	assert.Equal(t, true, state["over"])
	assert.EqualValues(t, 13, state["black_discs"])
	assert.EqualValues(t, 0, state["white_discs"])
	assert.Empty(t, state["legal"])
	assert.Equal(t, map[string]any{"x": 5.0, "y": 3.0}, state["last"])
	assert.Equal(t, 114, black.balance())
	assert.Equal(t, 85, white.balance())
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
		// movePvpAi 在玩家走完之后只让 AI 回一步：玩家在 AI 走完之后无子可下时，这一局会停在轮到 AI 走，玩家也走不了。
		// 这里固定种子下玩家执黑、执白的两局都没有碰到这种情况。
		require.EqualValues(t, 0, session["to_move"], "the AI should keep moving while the player has to pass")
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
		require.Equal(t, local.Board(), session["state"].(map[string]any)["board"])
		result, _ = reply["result"].(map[string]any)
	}

	assert.Equal(t, true, session["over"])
	assert.EqualValues(t, 0, session["winner"])
	assert.EqualValues(t, 1, result["score"])
	assert.EqualValues(t, 1, result["reward"])
	assert.Equal(t, 1, player.balance())
}
