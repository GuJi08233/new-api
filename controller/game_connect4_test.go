package controller

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/connect4"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connect4Setting(ai operation_setting.SkillRule) *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Connect4 = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 30, Ai: ai}
	})
}

// startConnect4Room 开一局押 stake 的房间并让对手加入，按随机决定的先后返回先手与后手。
func startConnect4Room(t *testing.T, host *gamePlayer, guest *gamePlayer, stake int) (int, map[string]any, *gamePlayer, *gamePlayer) {
	t.Helper()
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameConnect4, "stake": stake})
	roomId := jsonInt(t, created["id"])
	joined := guest.ok(http.MethodPost, gameRoomPath(roomId, "join"), nil)
	first, second := host, guest
	if jsonInt(t, joined["state"].(map[string]any)["first"]) == 1 {
		first, second = guest, host
	}
	return roomId, joined, first, second
}

// 押注对局走真实接口：先手在第 0 列竖着连成 4 子获胜，拿走奖池 30 减去 5% 抽成(向下取整为 1)。没轮到的一方、列号越界、
// 缺字段都被拒绝，棋盘按行从上到下给出。
func TestConnect4StakedRoomPaysWinnerMinusRake(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(operation_setting.SkillRule{}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	roomId, view, first, second := startConnect4Room(t, host, guest, 15)
	assert.Equal(t, 85, host.balance())
	assert.Equal(t, 85, guest.balance())

	version := view["version"]
	drop := func(col any) map[string]any {
		return map[string]any{"version": version, "move": map[string]any{"col": col}}
	}
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomNotYourTurn), second.fails(http.MethodPost, gameRoomPath(roomId, "move"), drop(3)))
	for i := 0; i < 4; i++ {
		view = first.ok(http.MethodPost, gameRoomPath(roomId, "move"), drop(0))
		version = view["version"]
		if i == 3 {
			break
		}
		if i == 0 {
			for _, bad := range []map[string]any{drop(7), drop(-1), drop("0"), {"version": version, "move": map[string]any{}}} {
				assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameMoveInvalid), second.fails(http.MethodPost, gameRoomPath(roomId, "move"), bad), "move %v", bad["move"])
			}
		}
		view = second.ok(http.MethodPost, gameRoomPath(roomId, "move"), drop(1))
		version = view["version"]
	}

	assert.Equal(t, model.GameRoomStatusFinished, view["status"])
	assert.EqualValues(t, first.user.Id, view["winner_id"])
	assert.EqualValues(t, 1, view["rake"])
	state := view["state"].(map[string]any)
	assert.Equal(t, "......."+"......."+"r......"+"ry....."+"ry....."+"ry.....", state["board"])
	assert.ElementsMatch(t, []any{
		map[string]any{"col": 0.0, "row": 2.0}, map[string]any{"col": 0.0, "row": 3.0},
		map[string]any{"col": 0.0, "row": 4.0}, map[string]any{"col": 0.0, "row": 5.0},
	}, state["line"])
	assert.Equal(t, 114, first.balance())
	assert.Equal(t, 85, second.balance())
}

// 轮到的一方过了这一步的截止时间还没走，下一次有人读房间时判他超时负，对手拿走奖池减去抽成。
func TestConnect4TimeoutLosesTheSideToMove(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(operation_setting.SkillRule{}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	roomId, view, first, second := startConnect4Room(t, host, guest, 20)
	view = first.ok(http.MethodPost, gameRoomPath(roomId, "move"), map[string]any{"version": view["version"], "move": map[string]any{"col": 3}})
	assert.EqualValues(t, view["state"].(map[string]any)["to_move"], view["to_move"])

	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", roomId).Update("deadline", 1).Error)
	polled := first.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", roomId), nil)
	assert.Equal(t, model.GameRoomStatusFinished, polled["status"])
	assert.Equal(t, model.GameRoomEndTimeout, polled["end_reason"])
	assert.EqualValues(t, first.user.Id, polled["winner_id"], "轮到后手走时超时，先手获胜")
	assert.EqualValues(t, 2, polled["rake"])
	assert.Equal(t, 118, first.balance())
	assert.Equal(t, 80, second.balance())
}

// 人机对战走真实接口：玩家每投一子，AI 在同一个请求里回一子，局面与玩家这边按双方着法重放的一致。玩家这边用困难难度的
// AI 下，赢下简单难度后按成绩 1 领到 1 个游戏币。
func TestConnect4AiGameSettles(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)

	session := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	local := connect4.New()
	if last, ok := session["state"].(map[string]any)["last"].(map[string]any); ok {
		require.NoError(t, local.Play(jsonInt(t, last["col"])))
	}
	rng := rand.New(rand.NewPCG(1, 1))
	var result map[string]any
	for result == nil {
		col, err := connect4.BestMove(local, 3, rng)
		require.NoError(t, err)
		require.NoError(t, local.Play(col))
		reply := player.ok(http.MethodPost, "/api/game/connect4/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"col": col},
		})
		session = reply["session"].(map[string]any)
		if aiMove, ok := reply["ai_move"].(map[string]any); ok {
			require.NoError(t, local.Play(jsonInt(t, aiMove["col"])))
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

// AI 搜索名额一直占满时，这一步排队到上限就回应失败，不会一直挂着；玩家这一步没有记下，名额空出来之后原样再走一次就行。
func TestConnect4AiMoveGivesUpWhenSearchesAreBusy(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	previousTimeout := pvpAiQueueTimeout
	pvpAiQueueTimeout = 20 * time.Millisecond
	t.Cleanup(func() { pvpAiQueueTimeout = previousTimeout })
	player := env.player(0, gameTestIpA)
	session := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)

	for len(pvpAiSearches) < cap(pvpAiSearches) {
		pvpAiSearches <- struct{}{}
	}
	t.Cleanup(func() {
		for len(pvpAiSearches) > 0 {
			<-pvpAiSearches
		}
	})
	move := map[string]any{"session_id": session["session_id"], "version": session["version"], "move": map[string]any{"col": 0}}
	assert.Equal(t, gameMessage(i18n.MsgGamePlayFailed), player.fails(http.MethodPost, "/api/game/connect4/ai/move", move))
	current := player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"].(map[string]any)
	assert.Equal(t, session["version"], current["version"], "这一步没有记下")
	assert.Equal(t, session["moves"], current["moves"])

	for len(pvpAiSearches) > 0 {
		<-pvpAiSearches
	}
	moved := player.ok(http.MethodPost, "/api/game/connect4/ai/move", move)["session"].(map[string]any)
	assert.EqualValues(t, jsonInt(t, session["moves"])+2, moved["moves"], "玩家与 AI 各走了一步")
}
