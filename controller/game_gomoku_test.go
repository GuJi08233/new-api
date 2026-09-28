package controller

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gomoku"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixBoardAiRandom 让棋类 AI 在这个测试里从同一串固定种子的随机数里取数：与线上一样每一步取到的数互不相关，
// 但人机对局里 AI 的每一步都确定下来。
func fixBoardAiRandom(t *testing.T, seed uint64) {
	t.Helper()
	previous := boardAiRandom
	rng := rand.New(rand.NewPCG(seed, seed))
	boardAiRandom = func() (*rand.Rand, error) { return rng, nil }
	t.Cleanup(func() { boardAiRandom = previous })
}

// boardAiRule 是测试里人机对战的领奖规则：赢下简单难度就奖励 1 个游戏币，不限游戏时长。
func boardAiRule() operation_setting.SkillRule {
	return operation_setting.SkillRule{Enabled: true, DailyRewards: 2, Tiers: []operation_setting.RewardTier{{Score: 1, Coins: 1}, {Score: 3, Coins: 2}}}
}

func gameRoomPath(id int, action string) string {
	return fmt.Sprintf("/api/game/rooms/%d/%s", id, action)
}

// jsonInt 取出响应里的整数(JSON 数字解析成 float64)。
func jsonInt(t *testing.T, value any) int {
	t.Helper()
	number, ok := value.(float64)
	require.True(t, ok, "%v is not a number", value)
	return int(number)
}

// 押注对局走真实接口：房主开房押 15，同一 IP 的账号不能加入，另一个 IP 的玩家加入后随机决定谁执黑先走。没轮到的一方、
// 落在已有棋子的格子、出界、缺字段都被拒绝；黑棋连成五子获胜，拿走奖池 30 减去 5% 抽成(1.5 向下取整为 1)，输家的押注
// 不退。
func TestGomokuStakedRoomPaysWinnerMinusRake(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60}
	}))
	host, guest, sameIp := env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, gameTestIpA)
	notYourTurn := i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomNotYourTurn)
	invalid := i18n.Translate(i18n.DefaultLang, i18n.MsgGameMoveInvalid)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 15})
	assert.EqualValues(t, 85, created["balance"])
	roomId := jsonInt(t, created["id"])
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomSameIp), sameIp.fails(http.MethodPost, gameRoomPath(roomId, "join"), nil))
	assert.Equal(t, 100, sameIp.balance())

	joined := guest.ok(http.MethodPost, gameRoomPath(roomId, "join"), nil)
	assert.EqualValues(t, 85, joined["balance"])
	assert.Equal(t, model.GameRoomStatusPlaying, joined["status"])
	state := joined["state"].(map[string]any)
	assert.Equal(t, strings.Repeat(".", gomoku.Squares), state["board"])
	blackSeat := jsonInt(t, state["black"])
	assert.Equal(t, blackSeat, jsonInt(t, joined["to_move"]), "执黑的一方先走")
	black, white := host, guest
	if blackSeat == 1 {
		black, white = guest, host
	}

	version := joined["version"]
	move := func(x int, y int) map[string]any {
		return map[string]any{"version": version, "move": map[string]any{"x": x, "y": y}}
	}
	assert.Equal(t, notYourTurn, white.fails(http.MethodPost, gameRoomPath(roomId, "move"), move(7, 7)))

	blackMoves := [][2]int{{3, 7}, {4, 7}, {5, 7}, {6, 7}, {7, 7}}
	whiteMoves := [][2]int{{3, 9}, {4, 9}, {5, 9}, {6, 9}}
	var view map[string]any
	for i, point := range blackMoves {
		view = black.ok(http.MethodPost, gameRoomPath(roomId, "move"), move(point[0], point[1]))
		version = view["version"]
		if i == len(whiteMoves) {
			break
		}
		if i == 0 {
			assert.Equal(t, notYourTurn, black.fails(http.MethodPost, gameRoomPath(roomId, "move"), move(0, 0)), "刚走完的一方不能接着走")
			for _, bad := range []any{map[string]any{"x": 3, "y": 7}, map[string]any{"x": 15, "y": 0}, map[string]any{"x": 1}, "e5"} {
				body := map[string]any{"version": version, "move": bad}
				assert.Equal(t, invalid, white.fails(http.MethodPost, gameRoomPath(roomId, "move"), body), "move %v", bad)
			}
		}
		view = white.ok(http.MethodPost, gameRoomPath(roomId, "move"), move(whiteMoves[i][0], whiteMoves[i][1]))
		version = view["version"]
	}

	assert.Equal(t, model.GameRoomStatusFinished, view["status"])
	assert.EqualValues(t, black.user.Id, view["winner_id"])
	assert.Equal(t, model.GameRoomEndWin, view["end_reason"])
	assert.EqualValues(t, 1, view["rake"])
	state = view["state"].(map[string]any)
	assert.Equal(t, true, state["over"])
	assert.EqualValues(t, blackSeat, state["winner"])
	assert.EqualValues(t, -1, state["to_move"])
	assert.Len(t, state["line"], 5)
	assert.Equal(t, map[string]any{"x": 7.0, "y": 7.0}, state["last"])
	assert.Equal(t, 114, black.balance())
	assert.Equal(t, 85, white.balance())

	polled := white.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", roomId), nil)
	assert.Equal(t, model.GameRoomStatusFinished, polled["status"])
	assert.Equal(t, state["board"], polled["state"].(map[string]any)["board"])
}

// 人机对战走真实接口：开局后(AI 执黑时它已经走了天元)玩家每走一步，AI 在同一个请求里回一步，局面与玩家这边按双方着法
// 重放的一致；落在已有棋子的格子被拒绝。玩家这边用困难难度的 AI 下，赢下简单难度后按成绩 1 领到 1 个游戏币。
// 人机对战的一局放过了有效期就走不了也认不了输：游戏页不再恢复它，玩家直接回到大厅，开新的一局时旧的一局作废。
func TestGomokuAiExpiredSessionIsNotRestored(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/gomoku/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	require.NotNil(t, player.ok(http.MethodGet, "/api/game/gomoku", nil)["ai"].(map[string]any)["session"])

	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", started["session_id"]).
		Update("created_at", common.GetTimestamp()-skillSessionTTL-1).Error)
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/gomoku", nil)["ai"].(map[string]any)["session"])
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/gomoku/ai", nil)["session"])
	restarted := player.ok(http.MethodPost, "/api/game/gomoku/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	assert.NotEqual(t, started["session_id"], restarted["session_id"])
}

func TestGomokuAiGameSettles(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 60, Ai: boardAiRule()}
	}))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)

	session := player.ok(http.MethodPost, "/api/game/gomoku/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	local := gomoku.New()
	if last, ok := session["state"].(map[string]any)["last"].(map[string]any); ok {
		assert.Equal(t, map[string]any{"x": 7.0, "y": 7.0}, last, "AI 执黑先走天元")
		require.NoError(t, local.Play(7, 7))
		assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/gomoku/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"x": 7, "y": 7},
		}))
	}

	rng := rand.New(rand.NewPCG(1, 1))
	var result map[string]any
	for result == nil {
		point, err := gomoku.BestMove(local, 3, rng)
		require.NoError(t, err)
		require.NoError(t, local.Play(point.X, point.Y))
		reply := player.ok(http.MethodPost, "/api/game/gomoku/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": point,
		})
		session = reply["session"].(map[string]any)
		if aiMove, ok := reply["ai_move"].(map[string]any); ok {
			require.NoError(t, local.Play(jsonInt(t, aiMove["x"]), jsonInt(t, aiMove["y"])))
		}
		require.Equal(t, local.Board(), session["state"].(map[string]any)["board"])
		result, _ = reply["result"].(map[string]any)
	}

	assert.Equal(t, model.GameSessionStatusFinished, session["status"])
	assert.Equal(t, true, session["over"])
	assert.EqualValues(t, 0, session["winner"])
	assert.EqualValues(t, 1, result["score"])
	assert.EqualValues(t, 1, result["reward"])
	assert.EqualValues(t, 1, result["balance"])
	assert.Equal(t, map[string]any{"level": 1.0, "winner": 0.0, "moves": float64(len(local.Moves()))}, result["detail"])
	assert.Equal(t, 1, player.balance())
}
