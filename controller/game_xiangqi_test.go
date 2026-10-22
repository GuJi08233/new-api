package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/xiangqi"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xiangqiSetting 打开中国象棋：押注对局每人最多押 100 个游戏币、抽成 5%、每步限时 120 秒；人机对战赢下普通奖 1 个、
// 困难奖 2 个游戏币，每个游戏币至少要下 60 秒。
func xiangqiSetting() *operation_setting.GameSetting {
	return gameSetting(func(s *operation_setting.GameSetting) {
		s.Xiangqi = operation_setting.PvpRule{
			Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 120,
			Ai: operation_setting.SkillRule{
				Enabled: true, DailyRewards: 2, MinSecondsPerCoin: 60,
				Tiers: []operation_setting.RewardTier{{Score: 2, Coins: 1}, {Score: 3, Coins: 2}},
			},
		}
	})
}

func xiangqiMoveBody(version any, fromX, fromY, toX, toY int) map[string]any {
	return map[string]any{"version": version, "move": map[string]any{"from": []int{fromX, fromY}, "to": []int{toX, toY}}}
}

// 押注对局：先走的座位执红，双方看到同一个开局，只有轮到的一方拿到全部 44 步合法着法。不是自己走、马腿被相挡住的马、
// 走对方的子、坐标出了棋盘都被拒绝，房间版本不变。红方走了当头炮之后黑方认输，红方拿走两份押注减去 5% 抽成。
func TestXiangqiRoomStakedGameEndsInResign(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	redSeat := int(joined["state"].(map[string]any)["red_seat"].(float64))
	assert.EqualValues(t, redSeat, joined["to_move"], "执红的一方先走")
	players := [2]*gamePlayer{host, guest}
	red, black := players[redSeat], players[1-redSeat]

	redView := red.ok(http.MethodGet, roomPath, nil)
	redState := redView["state"].(map[string]any)
	board := redState["board"].([]any)
	assert.Equal(t, "RNBAKABNR", board[0])
	assert.Equal(t, "rnbakabnr", board[9])
	assert.Equal(t, "red", redState["turn"])
	assert.Nil(t, redState["last"])
	assert.Equal(t, []any{}, redState["history"])
	assert.Equal(t, map[string]any{"red": nil, "black": nil}, redState["last_moves"])
	assert.Equal(t, false, redState["check"])
	assert.Len(t, redState["legal"], 44)
	blackView := black.ok(http.MethodGet, roomPath, nil)
	assert.Equal(t, board, blackView["state"].(map[string]any)["board"])
	assert.Empty(t, blackView["state"].(map[string]any)["legal"], "没轮到的一方没有合法着法")

	version := redView["version"]
	for _, rejected := range []struct {
		player *gamePlayer
		body   map[string]any
	}{
		{player: black, body: xiangqiMoveBody(version, 1, 9, 2, 7)},
		{player: red, body: xiangqiMoveBody(version, 1, 0, 3, 1)},
		{player: red, body: xiangqiMoveBody(version, 1, 9, 2, 7)},
		{player: red, body: xiangqiMoveBody(version, 1, 2, 1, 10)},
		{player: red, body: map[string]any{"version": version, "move": map[string]any{"from": []int{1, 2}}}},
	} {
		rejected.player.fails(http.MethodPost, roomPath+"/move", rejected.body)
	}
	assert.Equal(t, version, red.ok(http.MethodGet, roomPath, nil)["version"])

	moved := red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(version, 1, 2, 4, 2))
	movedState := moved["state"].(map[string]any)
	assert.Equal(t, "....C..C.", movedState["board"].([]any)[2])
	assert.Equal(t, map[string]any{"from": []any{1.0, 2.0}, "to": []any{4.0, 2.0}}, movedState["last"])
	assert.Equal(t, "black", movedState["turn"])
	assert.EqualValues(t, 1-redSeat, moved["to_move"])
	assert.Empty(t, movedState["legal"])
	redMove := map[string]any{
		"ply": 1.0, "color": "red", "piece": "C", "from": []any{1.0, 2.0}, "to": []any{4.0, 2.0},
		"notation": "炮八平五",
	}
	assert.Equal(t, []any{redMove}, movedState["history"])
	assert.Equal(t, map[string]any{"red": redMove, "black": nil}, movedState["last_moves"])
	refreshedState := black.ok(http.MethodGet, roomPath, nil)["state"].(map[string]any)
	assert.Equal(t, movedState["history"], refreshedState["history"])
	assert.Equal(t, movedState["last_moves"], refreshedState["last_moves"])

	resigned := black.ok(http.MethodPost, roomPath+"/resign", nil)
	assert.Equal(t, model.GameRoomStatusFinished, resigned["status"])
	assert.Equal(t, model.GameRoomEndResign, resigned["end_reason"])
	assert.EqualValues(t, red.user.Id, resigned["winner_id"])
	assert.Equal(t, 59.0, red.balance(), "50 - 10 + 20 - 1")
	assert.Equal(t, 40.0, black.balance())
}

// 人机对战：玩家坐座位 0，AI 执红时开局就先替它走一步。玩家走一步，AI 在同一个请求里回一步，返回的最后一手就是 AI 的走法。
// 之后把对局换成玩家一步杀的局面：玩家将死困难难度的 AI，这局按成绩 3 结算，下够了时长，拿到最高一档 2 个游戏币。
func TestXiangqiAiGameAgainstServer(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(0, gameTestIpA)

	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 3})
	session := started["session"].(map[string]any)
	state := session["state"].(map[string]any)
	aiIsRed := state["red_seat"] == 1.0
	openingMoves := 0
	if aiIsRed {
		openingMoves = 1
	}
	assert.EqualValues(t, 0, session["to_move"])
	assert.EqualValues(t, openingMoves, session["moves"])
	assert.Equal(t, aiIsRed, state["last"] != nil, "AI 执红时已经走了第一步")
	legal := state["legal"].([]any)
	require.NotEmpty(t, legal)

	replied := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
		"session_id": session["session_id"], "version": session["version"], "move": legal[0],
	})
	after := replied["session"].(map[string]any)
	afterState := after["state"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusPlaying, after["status"])
	assert.EqualValues(t, openingMoves+2, after["moves"])
	assert.EqualValues(t, 0, after["to_move"])
	require.NotNil(t, replied["ai_move"])
	assert.Equal(t, replied["ai_move"], afterState["last"])
	assert.NotEmpty(t, afterState["legal"])
	history := afterState["history"].([]any)
	require.Len(t, history, openingMoves+2)
	playerMove := history[len(history)-2].(map[string]any)
	aiMove := history[len(history)-1].(map[string]any)
	assert.Equal(t, legal[0].(map[string]any)["from"], playerMove["from"])
	assert.Equal(t, legal[0].(map[string]any)["to"], playerMove["to"])
	assert.Equal(t, replied["ai_move"].(map[string]any)["from"], aiMove["from"])
	assert.Equal(t, replied["ai_move"].(map[string]any)["to"], aiMove["to"])
	lastMoves := afterState["last_moves"].(map[string]any)
	assert.Equal(t, playerMove, lastMoves[playerMove["color"].(string)])
	assert.Equal(t, aiMove, lastMoves[aiMove["color"].(string)])
	refreshedState := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)["state"].(map[string]any)
	assert.Equal(t, afterState["history"], refreshedState["history"])
	assert.Equal(t, afterState["last_moves"], refreshedState["last_moves"])

	// 玩家执红，车 (8,7) 走到底线就将死黑将。
	mateInOne := "...K....." + strings.Repeat(".........", 6) + "........R" + "R........" + "....k...."
	board, err := common.Marshal(xiangqiState{RedSeat: 0, Game: xiangqi.Game{Board: mateInOne, Turn: xiangqi.Red, Plies: 30, Quiet: 4, Winner: xiangqi.NoColor}})
	require.NoError(t, err)
	aiState, err := common.Marshal(pvpAiState{Level: 3, Board: board, ToMove: 0, Winner: -1, Moves: 30})
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", after["session_id"]).
		Updates(map[string]any{"state": string(aiState), "created_at": common.GetTimestamp() - 200, "play_seconds": 200}).Error)

	won := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
		"session_id": after["session_id"], "version": after["version"], "move": map[string]any{"from": []int{8, 7}, "to": []int{8, 9}},
	})
	final := won["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusFinished, final["status"])
	assert.Equal(t, true, final["over"])
	assert.EqualValues(t, 0, final["winner"])
	assert.Equal(t, "checkmate", final["state"].(map[string]any)["result"])
	assert.Nil(t, won["ai_move"], "将死之后 AI 不再走")
	result := won["result"].(map[string]any)
	assert.EqualValues(t, 3, result["score"])
	assert.Equal(t, 2.0, result["reward"])
	assert.Equal(t, "", result["limited_by"])
	assert.Equal(t, 2.0, player.balance())
}

// 人机对战悔棋：撤回玩家上一步和 AI 的应着，局面回到玩家走之前。前 3 次免费，第 4 次收 1 个游戏币，余额不够时拒绝、局面不变；
// 还没走过棋时没有可以悔的。
func TestXiangqiAiUndoTakesBackTheLastRound(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(1, gameTestIpA)

	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 1})
	session := started["session"].(map[string]any)
	undoBody := func(s map[string]any) map[string]any {
		return map[string]any{"session_id": s["session_id"], "version": s["version"]}
	}
	player.fails(http.MethodPost, "/api/game/xiangqi/ai/undo", undoBody(session))

	for round := 1; round <= 4; round++ {
		state := session["state"].(map[string]any)
		undo := state["undo"].(map[string]any)
		assert.EqualValues(t, max(3-(round-1), 0), undo["free_left"], "第 %d 次之前", round)
		replied := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": state["legal"].([]any)[0],
		})
		after := replied["session"].(map[string]any)
		undone := player.ok(http.MethodPost, "/api/game/xiangqi/ai/undo", undoBody(after))
		session = undone["session"].(map[string]any)
		back := session["state"].(map[string]any)
		assert.Equal(t, state["board"], back["board"], "第 %d 次悔棋回到走之前", round)
		assert.Equal(t, state["last"], back["last"])
		assert.Equal(t, state["history"], back["history"])
		assert.Equal(t, state["last_moves"], back["last_moves"])
		assert.Equal(t, state["plies"], back["plies"])
		assert.Equal(t, session["moves"], started["session"].(map[string]any)["moves"])
		assert.EqualValues(t, 0, session["to_move"])
	}
	assert.Equal(t, 0.0, player.balance(), "第 4 次悔棋收了 1 个游戏币")
	var fee model.GameCoinLog
	require.NoError(t, env.db.Where("user_id = ? AND remark = ?", player.user.Id, "xiangqi_ai:undo").First(&fee).Error)
	assert.Equal(t, -1.0, coinsOf(t, fee.Amount))

	final := session["state"].(map[string]any)
	cost, err := model.ParseGameCoins(fmt.Sprint(final["undo"].(map[string]any)["cost"]))
	require.NoError(t, err)
	assert.Equal(t, 1.0, coinsOf(t, cost), "免费次数用完，下一次要 1 个游戏币")
	paid := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
		"session_id": session["session_id"], "version": session["version"], "move": final["legal"].([]any)[0],
	})
	after := paid["session"].(map[string]any)
	player.fails(http.MethodPost, "/api/game/xiangqi/ai/undo", undoBody(after))
	latest := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)
	assert.Equal(t, after["version"], latest["version"], "余额不够时局面不变")
}

// 押注对局悔棋要对方同意：提出的一方不能自己同意；被拒绝后再走一步之前不能再提；同意后撤回到提出的一方走之前，又轮到他。
// 每人前 3 次免费，第 4 次在同意时从提出的一方扣 1 个游戏币，对方不付钱。
func TestXiangqiRoomUndoNeedsTheOpponent(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	redSeat := int(joined["state"].(map[string]any)["red_seat"].(float64))
	players := [2]*gamePlayer{host, guest}
	red, black := players[redSeat], players[1-redSeat]
	start := red.ok(http.MethodGet, roomPath, nil)
	startBoard := start["state"].(map[string]any)["board"]

	red.fails(http.MethodPost, roomPath+"/undo", nil)
	moved := red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(start["version"], 1, 2, 4, 2))
	asked := red.ok(http.MethodPost, roomPath+"/undo", nil)
	assert.EqualValues(t, redSeat, asked["state"].(map[string]any)["undo"].(map[string]any)["pending_by"])
	assert.EqualValues(t, 1-redSeat, asked["to_move"], "提出悔棋不改变轮到谁")
	red.fails(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": true})
	red.fails(http.MethodPost, roomPath+"/undo", nil)

	declined := black.ok(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": false})
	assert.Equal(t, moved["state"].(map[string]any)["board"], declined["state"].(map[string]any)["board"])
	assert.Equal(t, true, red.ok(http.MethodGet, roomPath, nil)["state"].(map[string]any)["undo"].(map[string]any)["declined"])
	red.fails(http.MethodPost, roomPath+"/undo", nil)

	// 黑方应了一步，红方再提：同意后撤回黑方的应着和红方的一步，回到开局、轮到红方。
	replied := black.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(declined["version"], 1, 9, 2, 7))
	_ = replied
	red.ok(http.MethodPost, roomPath+"/undo", nil)
	accepted := black.ok(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": true})
	assert.Equal(t, startBoard, accepted["state"].(map[string]any)["board"])
	assert.EqualValues(t, redSeat, accepted["to_move"])
	assert.Nil(t, accepted["state"].(map[string]any)["last"])
	assert.Equal(t, []any{}, accepted["state"].(map[string]any)["history"])
	assert.Equal(t, map[string]any{"red": nil, "black": nil}, accepted["state"].(map[string]any)["last_moves"])
	redUndo := red.ok(http.MethodGet, roomPath, nil)["state"].(map[string]any)["undo"].(map[string]any)
	assert.EqualValues(t, 1, redUndo["used"])
	assert.EqualValues(t, 2, redUndo["free_left"])

	for round := 2; round <= 4; round++ {
		current := red.ok(http.MethodGet, roomPath, nil)
		red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(current["version"], 1, 2, 4, 2))
		red.ok(http.MethodPost, roomPath+"/undo", nil)
		black.ok(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": true})
	}
	assert.Equal(t, 39.0, red.balance(), "50 - 10 押注 - 第 4 次悔棋 1")
	assert.Equal(t, 40.0, black.balance())
	var fee model.GameCoinLog
	require.NoError(t, env.db.Where("user_id = ? AND remark = ?", red.user.Id, "xiangqi:undo").First(&fee).Error)
	assert.Equal(t, -1.0, coinsOf(t, fee.Amount))
}

// 记录走法之前开始的对局(升级前的存档)没有棋步记录：可以照常走棋，但更早的棋步撤不回来，悔棋按钮不可用、请求被拒绝；
// 升级后走的棋有记录，可以悔。
func TestXiangqiUndoSkipsMovesFromBeforeHistory(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 1})
	session := started["session"].(map[string]any)

	// 换成旧存档：玩家执红，已经走了 2 步，没有 history。
	legacy, err := common.Marshal(xiangqiState{RedSeat: 0, Game: xiangqi.Game{Board: xiangqi.StartBoard, Turn: xiangqi.Red, Plies: 2, Quiet: 2, Winner: xiangqi.NoColor}})
	require.NoError(t, err)
	aiState, err := common.Marshal(pvpAiState{Level: 1, Board: legacy, ToMove: 0, Winner: -1, Moves: 2})
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", session["session_id"]).Update("state", string(aiState)).Error)

	current := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)
	assert.Equal(t, []any{}, current["state"].(map[string]any)["history"])
	assert.Equal(t, map[string]any{"red": nil, "black": nil}, current["state"].(map[string]any)["last_moves"])
	undo := current["state"].(map[string]any)["undo"].(map[string]any)
	assert.Equal(t, false, undo["available"])
	body := map[string]any{"session_id": current["session_id"], "version": current["version"]}
	player.fails(http.MethodPost, "/api/game/xiangqi/ai/undo", body)

	replied := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
		"session_id": current["session_id"], "version": current["version"], "move": current["state"].(map[string]any)["legal"].([]any)[0],
	})
	after := replied["session"].(map[string]any)
	assert.Equal(t, true, after["state"].(map[string]any)["undo"].(map[string]any)["available"])
	history := after["state"].(map[string]any)["history"].([]any)
	require.Len(t, history, 2)
	assert.Equal(t, 3.0, history[0].(map[string]any)["ply"])
	assert.Equal(t, 4.0, history[1].(map[string]any)["ply"])
	assert.NotEmpty(t, history[0].(map[string]any)["notation"])
	assert.NotEmpty(t, history[1].(map[string]any)["notation"])
	undone := player.ok(http.MethodPost, "/api/game/xiangqi/ai/undo", map[string]any{"session_id": after["session_id"], "version": after["version"]})
	back := undone["session"].(map[string]any)["state"].(map[string]any)
	assert.Equal(t, current["state"].(map[string]any)["board"], back["board"])
	assert.Equal(t, []any{}, back["history"])
	assert.Equal(t, map[string]any{"red": nil, "black": nil}, back["last_moves"])
}

// 一枚行棋的棋子后来被吃掉，仍能从悔棋记录还原它的原始种类；双方最近一手随撤回和重新落子更新。
func TestXiangqiHistoryRestoresCapturedPiecesAndUndo(t *testing.T) {
	engine := xiangqiEngine{}
	state, err := engine.New(1)
	require.NoError(t, err)
	moves := []xiangqiHistoryMove{
		{Ply: 1, Color: "red", Piece: "C", Notation: "炮八进七", Captured: "n", From: []int{1, 2}, To: []int{1, 9}},
		{Ply: 2, Color: "black", Piece: "r", Notation: "车1平2", Captured: "C", From: []int{0, 9}, To: []int{1, 9}},
		{Ply: 3, Color: "red", Piece: "P", Notation: "兵九进一", From: []int{0, 3}, To: []int{0, 4}},
		{Ply: 4, Color: "black", Piece: "p", Notation: "卒3进1", From: []int{2, 6}, To: []int{2, 5}},
	}
	for i, move := range moves {
		body, err := common.Marshal(xiangqiMove{From: move.From, To: move.To})
		require.NoError(t, err)
		state, err = engine.Play(state.Data, 1-i%2, body)
		require.NoError(t, err)
	}
	for _, seat := range []int{0, 1} {
		result, err := engine.View(state.Data, seat)
		require.NoError(t, err)
		view := result.(xiangqiView)
		assert.Equal(t, moves, view.History)
		assert.Equal(t, &moves[2], view.LastMoves.Red)
		assert.Equal(t, &moves[3], view.LastMoves.Black)
	}
	asked, _, err := engine.UndoAsk(state.Data, 1)
	require.NoError(t, err)
	undone, err := engine.UndoAnswer(asked.Data, 0, true)
	require.NoError(t, err)
	result, err := engine.View(undone.State.Data, 1)
	require.NoError(t, err)
	view := result.(xiangqiView)
	assert.Equal(t, moves[:2], view.History)
	assert.Equal(t, &moves[0], view.LastMoves.Red)
	assert.Equal(t, &moves[1], view.LastMoves.Black)

	body, err := common.Marshal(xiangqiMove{From: []int{2, 3}, To: []int{2, 4}})
	require.NoError(t, err)
	replayed, err := engine.Play(undone.State.Data, 1, body)
	require.NoError(t, err)
	result, err = engine.View(replayed.Data, 0)
	require.NoError(t, err)
	view = result.(xiangqiView)
	expected := xiangqiHistoryMove{Ply: 3, Color: "red", Piece: "P", Notation: "兵七进一", From: []int{2, 3}, To: []int{2, 4}}
	require.Len(t, view.History, 3)
	assert.Equal(t, moves[:2], view.History[:2])
	assert.Equal(t, expected, view.History[2])
	assert.Equal(t, &expected, view.LastMoves.Red)
	assert.Equal(t, &moves[1], view.LastMoves.Black)
}

// 接口从只含坐标的既有存档派生标准记谱，两个座位刷新后看到相同的六步开局。
func TestXiangqiHistoryNotationMatchesCompetitionOpening(t *testing.T) {
	engine := xiangqiEngine{}
	state, err := engine.New(0)
	require.NoError(t, err)
	expected := []xiangqiHistoryMove{
		{Ply: 1, Color: "red", Piece: "C", Notation: "炮二平五", From: []int{7, 2}, To: []int{4, 2}},
		{Ply: 2, Color: "black", Piece: "n", Notation: "马8进7", From: []int{7, 9}, To: []int{6, 7}},
		{Ply: 3, Color: "red", Piece: "N", Notation: "马二进三", From: []int{7, 0}, To: []int{6, 2}},
		{Ply: 4, Color: "black", Piece: "r", Notation: "车9平8", From: []int{8, 9}, To: []int{7, 9}},
		{Ply: 5, Color: "red", Piece: "R", Notation: "车一平二", From: []int{8, 0}, To: []int{7, 0}},
		{Ply: 6, Color: "black", Piece: "c", Notation: "炮8进4", From: []int{7, 7}, To: []int{7, 3}},
	}
	for i, move := range expected {
		body, err := common.Marshal(xiangqiMove{From: move.From, To: move.To})
		require.NoError(t, err)
		state, err = engine.Play(state.Data, i%2, body)
		require.NoError(t, err)
	}
	assert.NotContains(t, string(state.Data), "notation", "存档继续只保存原始棋步，无需迁移")
	for _, seat := range []int{0, 1} {
		result, err := engine.View(state.Data, seat)
		require.NoError(t, err)
		view := result.(xiangqiView)
		assert.Equal(t, expected, view.History)
		assert.Equal(t, &expected[4], view.LastMoves.Red)
		assert.Equal(t, &expected[5], view.LastMoves.Black)
	}
}

// 前车移出原路后当前棋盘已无双车同路，记谱仍须使用走前局面的“前车”；悔棋后改走后车同样正确。
func TestXiangqiHistoryNotationUsesBoardBeforeMoveAndUndo(t *testing.T) {
	board := []byte(strings.Repeat(".", xiangqi.Squares))
	board[xiangqi.Square(3, 0)], board[xiangqi.Square(5, 9)] = 'K', 'k'
	board[xiangqi.Square(2, 3)], board[xiangqi.Square(2, 6)] = 'R', 'R'
	data, err := common.Marshal(xiangqiState{RedSeat: 0, Game: xiangqi.Game{
		Board: string(board), Turn: xiangqi.Red, Plies: 10, Winner: xiangqi.NoColor,
	}})
	require.NoError(t, err)
	engine := xiangqiEngine{}
	body, err := common.Marshal(xiangqiMove{From: []int{2, 6}, To: []int{4, 6}})
	require.NoError(t, err)
	moved, err := engine.Play(data, 0, body)
	require.NoError(t, err)
	result, err := engine.View(moved.Data, 0)
	require.NoError(t, err)
	view := result.(xiangqiView)
	require.Len(t, view.History, 1)
	assert.Equal(t, "前车平五", view.History[0].Notation)
	assert.Equal(t, 11, view.History[0].Ply)

	asked, _, err := engine.UndoAsk(moved.Data, 0)
	require.NoError(t, err)
	undone, err := engine.UndoAnswer(asked.Data, 1, true)
	require.NoError(t, err)
	body, err = common.Marshal(xiangqiMove{From: []int{2, 3}, To: []int{4, 3}})
	require.NoError(t, err)
	replayed, err := engine.Play(undone.State.Data, 0, body)
	require.NoError(t, err)
	result, err = engine.View(replayed.Data, 1)
	require.NoError(t, err)
	view = result.(xiangqiView)
	require.Len(t, view.History, 1)
	assert.Equal(t, "后车平五", view.History[0].Notation)
	assert.Equal(t, &view.History[0], view.LastMoves.Red)
	assert.Nil(t, view.LastMoves.Black)
}

func TestXiangqiLegacyLastMoveKeepsMarkersWithoutInventingHistory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		moves    []xiangqi.Move
		expected xiangqiHistoryMove
	}{
		{
			name:     "red",
			moves:    []xiangqi.Move{{From: xiangqi.Square(1, 2), To: xiangqi.Square(1, 9)}},
			expected: xiangqiHistoryMove{Ply: 1, Color: "red", Piece: "C", From: []int{1, 2}, To: []int{1, 9}},
		},
		{
			name: "black",
			moves: []xiangqi.Move{
				{From: xiangqi.Square(1, 2), To: xiangqi.Square(1, 9)},
				{From: xiangqi.Square(0, 9), To: xiangqi.Square(1, 9)},
			},
			expected: xiangqiHistoryMove{Ply: 2, Color: "black", Piece: "r", From: []int{0, 9}, To: []int{1, 9}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := xiangqi.NewGame()
			for _, move := range tc.moves {
				require.NoError(t, game.Play(move))
			}
			game.History = nil
			data, err := common.Marshal(xiangqiState{RedSeat: 1, Game: game})
			require.NoError(t, err)
			result, err := (xiangqiEngine{}).View(data, 1)
			require.NoError(t, err)
			view := result.(xiangqiView)
			assert.Equal(t, []xiangqiHistoryMove{}, view.History)
			assert.Equal(t, &xiangqiMove{From: tc.expected.From, To: tc.expected.To}, view.Last)
			encoded, err := common.Marshal(view)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "notation", "旧存档缺少走前局面时，仅返回坐标")
			if tc.name == "red" {
				assert.Equal(t, &tc.expected, view.LastMoves.Red)
				assert.Nil(t, view.LastMoves.Black)
			} else {
				assert.Equal(t, &tc.expected, view.LastMoves.Black)
				assert.Nil(t, view.LastMoves.Red)
			}
		})
	}
}

// 人机战提示：分析当前局面并返回一条合法着法，不落子、不改动棋盘与轮次。前 3 次免费，第 4 次收 1 个游戏币，余额不够时
// 拒绝且次数不变；对局结束之后不再提示。
func TestXiangqiAiHintAnalysesWithoutMoving(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(1, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 2})
	session := started["session"].(map[string]any)
	assert.EqualValues(t, 3, session["hint"].(map[string]any)["free_left"])

	body := func(s map[string]any) map[string]any {
		return map[string]any{"session_id": s["session_id"], "version": s["version"]}
	}
	for round := 1; round <= 3; round++ {
		current := session["state"].(map[string]any)
		legalMoves := map[string]bool{}
		for _, m := range current["legal"].([]any) {
			encoded, err := common.Marshal(m)
			require.NoError(t, err)
			legalMoves[string(encoded)] = true
		}
		replied := player.ok(http.MethodPost, "/api/game/xiangqi/ai/hint", body(session))
		hint := replied["hint"].(map[string]any)
		line := hint["line"].(map[string]any)
		move, err := common.Marshal(map[string]any{"from": line["from"], "to": line["to"]})
		require.NoError(t, err)
		assert.True(t, legalMoves[string(move)], "第 %d 次的推荐着法 %s 是合法的一步", round, move)
		assert.NotEmpty(t, line["notation"])
		assert.EqualValues(t, current["plies"], hint["plies"])
		after := replied["session"].(map[string]any)
		assert.Equal(t, current, after["state"], "提示不改动棋盘与轮次")
		assert.EqualValues(t, 3-round, after["hint"].(map[string]any)["free_left"], "第 %d 次之后", round)
		assert.Equal(t, 1.0, player.balance(), "免费提示不收费")
		repeated := player.ok(http.MethodPost, "/api/game/xiangqi/ai/hint", body(session))
		assert.Equal(t, replied["hint"], repeated["hint"])
		assert.Equal(t, after["version"], repeated["session"].(map[string]any)["version"])
		// 真正走到下一盘面才消耗下一次提示。
		session = player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
			"session_id": after["session_id"], "version": after["version"],
			"move": map[string]any{"from": line["from"], "to": line["to"]},
		})["session"].(map[string]any)
		assert.Nil(t, session["hint_result"])
	}

	// 第 4 次要 1 个游戏币，玩家正好有 1 个。
	paid := player.ok(http.MethodPost, "/api/game/xiangqi/ai/hint", body(session))
	after := paid["session"].(map[string]any)
	assert.EqualValues(t, 0, after["hint"].(map[string]any)["free_left"])
	assert.Equal(t, 0.0, player.balance(), "第 4 次提示收了 1 个游戏币")
	var fee model.GameCoinLog
	require.NoError(t, env.db.Where("user_id = ? AND remark = ?", player.user.Id, "xiangqi_ai:hint").First(&fee).Error)
	assert.Equal(t, -1.0, coinsOf(t, fee.Amount))

	// 换到新的局面后余额不够，次数与局面都不变。
	nextMove := after["state"].(map[string]any)["legal"].([]any)[0]
	session = player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
		"session_id": after["session_id"], "version": after["version"], "move": nextMove,
	})["session"].(map[string]any)
	player.fails(http.MethodPost, "/api/game/xiangqi/ai/hint", body(session))
	latest := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)
	assert.Equal(t, session["version"], latest["version"])

	// 换成已经结束的一局(红方一步将死黑方)：不再提示，也不再扣次数。
	finished := xiangqi.Game{Board: matedByTwoRooksForTest(), Turn: xiangqi.Red, Plies: 40, Quiet: 4, Winner: xiangqi.NoColor}
	require.NoError(t, finished.Play(xiangqi.Move{From: xiangqi.Square(8, 7), To: xiangqi.Square(8, 9)}))
	over, err := common.Marshal(xiangqiState{RedSeat: 0, Game: finished})
	require.NoError(t, err)
	overState, err := common.Marshal(pvpAiState{Level: 2, Board: over, ToMove: 0, Winner: -1, Moves: 41})
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", session["session_id"]).Update("state", string(overState)).Error)
	current := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)
	assert.Equal(t, false, current["hint"].(map[string]any)["available"])
	player.fails(http.MethodPost, "/api/game/xiangqi/ai/hint", map[string]any{
		"session_id": current["session_id"], "version": current["version"],
	})
}

// 在本次分析完成后提交另一份相同请求的结果，确定性地覆盖“读到旧版本、竞争方先落库”的窗口。
type committingXiangqiHintEngine struct {
	xiangqiEngine
	commit func(pvpHint)
}

func (e committingXiangqiHintEngine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	answer, err := e.xiangqiEngine.HintContext(ctx, data, seat)
	if err == nil {
		e.commit(answer)
	}
	return answer, err
}

func TestXiangqiPaidHintRestoresAndChargesOnce(t *testing.T) {
	for _, competing := range []bool{false, true} {
		name := "lost_response"
		if competing {
			name = "competing_commit"
		}
		t.Run(name, func(t *testing.T) {
			env := newGameTestEnv(t, xiangqiSetting())
			player := env.player(4, gameTestIpA)
			initial, err := (xiangqiEngine{}).New(0)
			require.NoError(t, err)
			board, err := loadXiangqiState(initial.Data)
			require.NoError(t, err)
			board.Hints = gameHintFreePlays
			data, err := common.Marshal(board)
			require.NoError(t, err)
			state := pvpAiState{Level: 1, Board: data, ToMove: 0, Winner: -1}
			encoded, err := common.Marshal(state)
			require.NoError(t, err)
			session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameXiangqi), 1, string(encoded))
			require.NoError(t, err)
			request := map[string]any{"session_id": session.Id, "version": session.Version}
			path := "/api/game/xiangqi/ai/hint"
			if competing {
				game := *pvpGames[gameXiangqi]
				game.engine = committingXiangqiHintEngine{commit: func(answer pvpHint) {
					state.Board = answer.State.Data
					data, err := common.Marshal(state)
					require.NoError(t, err)
					_, err = model.ChargeGameSessionState(session, string(data), answer.Cost, "hint")
					require.NoError(t, err)
				}}
				path = "/competing-hint"
				env.router.POST(path, middleware.UserAuth(), func(c *gin.Context) { hintPvpAi(c, &game) })
			}
			paid := player.ok(http.MethodPost, path, request)
			require.NotNil(t, paid["hint"])
			assert.Equal(t, 3.0, player.balance())
			current := paid["session"].(map[string]any)
			assert.Equal(t, paid["hint"], current["hint_result"])
			for _, version := range []any{request["version"], current["version"]} {
				retry := player.ok(http.MethodPost, "/api/game/xiangqi/ai/hint", map[string]any{"session_id": session.Id, "version": version})
				assert.Equal(t, paid["hint"], retry["hint"])
				assert.Equal(t, current["version"], retry["session"].(map[string]any)["version"])
			}
			restored := player.ok(http.MethodGet, fmt.Sprintf("/api/game/xiangqi/ai?session_id=%d", session.Id), nil)
			assert.Equal(t, paid["hint"], restored["session"].(map[string]any)["hint_result"])
			assert.Equal(t, 3.0, restored["balance"], "恢复响应刷新已扣费的余额")
			page := player.ok(http.MethodGet, "/api/game/xiangqi", nil)
			assert.Equal(t, paid["hint"], page["ai"].(map[string]any)["session"].(map[string]any)["hint_result"])
			assert.Equal(t, 3.0, player.balance())
			var fees int64
			require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND remark = ?", player.user.Id, "xiangqi_ai:hint").Count(&fees).Error)
			assert.EqualValues(t, 1, fees)
			// 走棋后不再显示旧提示；悔棋回到相同步数也不能复活已经撤掉的分析。
			moved := player.ok(http.MethodPost, "/api/game/xiangqi/ai/move", map[string]any{
				"session_id": session.Id, "version": current["version"], "move": current["state"].(map[string]any)["legal"].([]any)[0],
			})["session"].(map[string]any)
			assert.Nil(t, moved["hint_result"])
			undone := player.ok(http.MethodPost, "/api/game/xiangqi/ai/undo", map[string]any{"session_id": session.Id, "version": moved["version"]})["session"].(map[string]any)
			assert.Equal(t, current["state"].(map[string]any)["board"], undone["state"].(map[string]any)["board"])
			assert.Nil(t, undone["hint_result"])
		})
	}
}

// 取消的电脑搜索和提示都不返回可落库的新状态，也不消耗提示次数。
func TestXiangqiEngineCanceledSearchKeepsState(t *testing.T) {
	engine := xiangqiEngine{}
	initial, err := engine.New(0)
	require.NoError(t, err)
	before := bytes.Clone(initial.Data)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m, err := engine.AiMoveContext(ctx, initial.Data, 0, 3)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, m)
	hint, err := engine.HintContext(ctx, initial.Data, 0)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, hint)
	assert.Equal(t, before, initial.Data)
	status, err := engine.HintStatus(initial.Data, 0)
	require.NoError(t, err)
	assert.Zero(t, status.Used)
	assert.Equal(t, gameHintFreePlays, status.FreeLeft)
}

// matedByTwoRooksForTest 是红方一步杀的局面：车 (8,7) 走到底线将军，另一只车封住 y=8，黑将无处可逃。
func matedByTwoRooksForTest() string {
	rows := []string{
		"....k....",
		"R........",
		"........R",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		"...K.....",
	}
	var board strings.Builder
	for y := 0; y < xiangqi.Height; y++ {
		board.WriteString(rows[xiangqi.Height-1-y])
	}
	return board.String()
}

// 房间对局没有提示接口：押注对局里给出引擎的判断等于让一方看别人的棋。
func TestXiangqiRoomHasNoHints(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	guest.ok(http.MethodPost, roomPath+"/join", nil)
	// 路由没注册就是 404，不会给出任何分析。
	recorder := host.send(http.MethodPost, roomPath+"/hint", nil)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

// 悔棋被拒绝之后只有提出的一方要等到下一步才能再提；拒绝的一方既不该看到"对方拒绝了悔棋"，也不该连自己的悔棋一起被锁住。
func TestXiangqiDeclinedUndoOnlyBlocksTheAsker(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	redSeat := int(joined["state"].(map[string]any)["red_seat"].(float64))
	players := [2]*gamePlayer{host, guest}
	red, black := players[redSeat], players[1-redSeat]

	start := red.ok(http.MethodGet, roomPath, nil)
	red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(start["version"], 1, 0, 2, 2))
	afterRed := black.ok(http.MethodGet, roomPath, nil)
	black.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(afterRed["version"], 1, 9, 2, 7))

	red.ok(http.MethodPost, roomPath+"/undo", nil)
	black.ok(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": false})

	asker := red.ok(http.MethodGet, roomPath, nil)["state"].(map[string]any)["undo"].(map[string]any)
	assert.Equal(t, true, asker["declined"], "提出的一方知道自己被拒绝")
	assert.Equal(t, false, asker["available"], "提出的一方要走一步之后才能再提")
	red.fails(http.MethodPost, roomPath+"/undo", nil)

	decliner := black.ok(http.MethodGet, roomPath, nil)["state"].(map[string]any)["undo"].(map[string]any)
	assert.Equal(t, false, decliner["declined"], "拒绝的一方没有被拒绝过")
	assert.Equal(t, true, decliner["available"], "拒绝的一方还可以悔自己刚走的一步")
	black.ok(http.MethodPost, roomPath+"/undo", nil)
}

// 升级前的存档把 declined 存成一个数字(被拒绝时的步数)。读旧存档不能失败，也不该把双方都锁住。
func TestXiangqiLoadsLegacyDeclinedUndo(t *testing.T) {
	legacy := `{"red_seat":0,"game":{"board":"` + xiangqi.StartBoard + `","turn":0,"plies":3,"quiet":3,"winner":-1,"history":[]},"undo":{"declined":3}}`
	state, err := loadXiangqiState([]byte(legacy))
	require.NoError(t, err)
	assert.EqualValues(t, 3, state.Undo.Declined.Plies)
	assert.False(t, state.declinedBy(0))
	assert.False(t, state.declinedBy(1))
}

// 房间对局求和：轮到的一方可以提，提出后请求在等对手回应、棋盘与轮次都不变；提出的一方不能自己同意、也不能再提一个；
// 对手拒绝后只撤回请求，对局继续；对手同意时这局按和棋结束，两份押注原样退回。
func TestXiangqiRoomDrawOfferEndsInADraw(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	redSeat := int(joined["state"].(map[string]any)["red_seat"].(float64))
	players := [2]*gamePlayer{host, guest}
	red, black := players[redSeat], players[1-redSeat]

	// 红方先走，走完之后轮到黑方：只有轮到的黑方可以提议和棋，红方这时提议会被拒绝。
	start := red.ok(http.MethodGet, roomPath, nil)
	moved := red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(start["version"], 1, 2, 4, 2))
	assert.NotEmpty(t, red.fails(http.MethodPost, roomPath+"/draw", nil), "没轮到的一方不能提议")
	offered := black.ok(http.MethodPost, roomPath+"/draw", nil)
	offerState := offered["state"].(map[string]any)
	assert.EqualValues(t, 1-redSeat, offerState["undo"].(map[string]any)["draw_by"])
	assert.Equal(t, moved["state"].(map[string]any)["board"], offerState["board"], "求和不改动棋局")
	assert.EqualValues(t, 1-redSeat, offered["to_move"], "求和之后还是轮到同一个人走")
	assert.Equal(t, false, offerState["undo"].(map[string]any)["can_offer"], "有请求在等回应时不能再提")

	// 提出的一方不能自己同意，也不能再提一个。
	black.fails(http.MethodPost, roomPath+"/draw/answer", map[string]any{"accept": true})
	black.fails(http.MethodPost, roomPath+"/draw", nil)

	declined := red.ok(http.MethodPost, roomPath+"/draw/answer", map[string]any{"accept": false})
	assert.Equal(t, moved["state"].(map[string]any)["board"], declined["state"].(map[string]any)["board"])
	assert.EqualValues(t, -1, declined["state"].(map[string]any)["undo"].(map[string]any)["draw_by"])
	assert.Equal(t, model.GameRoomStatusPlaying, declined["status"], "拒绝求和之后对局继续")

	// 还是轮到黑方，黑方再提；红方同意，这局按和棋结束，两份押注各退回。
	blackOffer := black.ok(http.MethodPost, roomPath+"/draw", nil)
	assert.EqualValues(t, 1-redSeat, blackOffer["state"].(map[string]any)["undo"].(map[string]any)["draw_by"])
	finished := red.ok(http.MethodPost, roomPath+"/draw/answer", map[string]any{"accept": true})
	assert.Equal(t, model.GameRoomStatusFinished, finished["status"])
	assert.Equal(t, model.GameRoomEndDraw, finished["end_reason"])
	assert.Equal(t, "agreement", finished["state"].(map[string]any)["result"])
	assert.Equal(t, moved["state"].(map[string]any)["board"], finished["state"].(map[string]any)["board"], "和棋的棋盘停在同意时的局面")
	assert.Equal(t, 50.0, red.balance(), "和棋退回押注")
	assert.Equal(t, 50.0, black.balance())
}

// 人机对战求和：电脑按局面分数决定。玩家把局面换成自己明显占优(电脑少一个车)，电脑拒绝，这局接着下；换成电脑占优，
// 电脑接受，这局按和棋结算，成绩 0、不发奖励。
func TestXiangqiAiDrawOfferFollowsTheEvaluation(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/xiangqi/ai/start", map[string]any{"level": 2})
	session := started["session"].(map[string]any)

	// 玩家执红，电脑执黑。黑方多一个车时电脑明显占优，拒绝求和；红方多两个车时电脑落后，愿意和棋。
	aiAhead := "....k...." + "....r...." + strings.Repeat(".........", 7) + "...K....."
	aiBehind := "....k...." + strings.Repeat(".........", 7) + "R.......R" + "...K....."
	offer := func(board string) map[string]any {
		boardState, err := common.Marshal(xiangqiState{RedSeat: 0, Game: xiangqi.Game{Board: board, Turn: xiangqi.Red, Plies: 20, Quiet: 20, Winner: xiangqi.NoColor}})
		require.NoError(t, err)
		aiState, err := common.Marshal(pvpAiState{Level: 2, Board: boardState, ToMove: 0, Winner: -1, Moves: 20})
		require.NoError(t, err)
		require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", session["session_id"]).
			Updates(map[string]any{"state": string(aiState), "created_at": common.GetTimestamp() - 200, "play_seconds": 200}).Error)
		current := player.ok(http.MethodGet, "/api/game/xiangqi/ai", nil)["session"].(map[string]any)
		return player.ok(http.MethodPost, "/api/game/xiangqi/ai/draw", map[string]any{
			"session_id": current["session_id"], "version": current["version"],
		})
	}

	refused := offer(aiAhead)
	assert.Equal(t, false, refused["accepted"], "黑方多一个车，电脑不接受和棋")
	assert.Equal(t, model.GameSessionStatusPlaying, refused["session"].(map[string]any)["status"])
	assert.EqualValues(t, -1, refused["session"].(map[string]any)["state"].(map[string]any)["undo"].(map[string]any)["draw_by"], "拒绝之后请求撤回")

	// 换成红方多两个车，电脑落后，接受求和。
	accepted := offer(aiBehind)
	assert.Equal(t, true, accepted["accepted"], "红方多两个车，电脑接受和棋")
	final := accepted["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusFinished, final["status"])
	assert.Equal(t, "draw", final["result"])
	assert.EqualValues(t, 0, accepted["result"].(map[string]any)["score"])
	assert.Equal(t, 0.0, accepted["result"].(map[string]any)["reward"])
	assert.Equal(t, 0.0, player.balance(), "和棋不发奖励")

	// 已经结束的这局求和无从谈起：读回的是结算后的这一局，不会多出一个 accepted。
	after := player.ok(http.MethodPost, "/api/game/xiangqi/ai/draw", map[string]any{"session_id": final["session_id"], "version": final["version"]})
	assert.Nil(t, after["accepted"], "结束的对局不再接受求和")
	assert.Equal(t, model.GameSessionStatusFinished, after["session"].(map[string]any)["status"])
}

// 求和与悔棋共用一个请求位：有悔棋请求在等回应时不能求和，有求和请求在等回应时也不能提悔棋。
func TestXiangqiDrawAndUndoDoNotOverlap(t *testing.T) {
	env := newGameTestEnv(t, xiangqiSetting())
	host, guest := env.player(50, gameTestIpA), env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameXiangqi, "stake": 10})
	roomPath := fmt.Sprintf("/api/game/rooms/%v", created["id"])
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	redSeat := int(joined["state"].(map[string]any)["red_seat"].(float64))
	red := [2]*gamePlayer{host, guest}[redSeat]
	black := [2]*gamePlayer{host, guest}[1-redSeat]
	playing := red.ok(http.MethodGet, roomPath, nil)
	red.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(playing["version"], 1, 2, 4, 2))
	// 黑方也应一步，这才有自己的棋可以悔，而且又轮到红方走。
	afterRed := black.ok(http.MethodGet, roomPath, nil)
	black.ok(http.MethodPost, roomPath+"/move", xiangqiMoveBody(afterRed["version"], 1, 9, 2, 7))

	// 轮到红方。红方提出悔棋之后不能再求和：两个请求位一次只放一个。
	red.ok(http.MethodPost, roomPath+"/undo", nil)
	red.fails(http.MethodPost, roomPath+"/draw", nil)
	// 黑方拒绝悔棋，红方改成求和：求和请求在等回应时，红方再提悔棋被拒绝。
	black.ok(http.MethodPost, roomPath+"/undo/answer", map[string]any{"accept": false})
	red.ok(http.MethodPost, roomPath+"/draw", nil)
	red.fails(http.MethodPost, roomPath+"/undo", nil)
}
