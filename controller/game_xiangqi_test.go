package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/xiangqi"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
		{Ply: 1, Color: "red", Piece: "C", Captured: "n", From: []int{1, 2}, To: []int{1, 9}},
		{Ply: 2, Color: "black", Piece: "r", Captured: "C", From: []int{0, 9}, To: []int{1, 9}},
		{Ply: 3, Color: "red", Piece: "P", From: []int{0, 3}, To: []int{0, 4}},
		{Ply: 4, Color: "black", Piece: "p", From: []int{2, 6}, To: []int{2, 5}},
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
	expected := xiangqiHistoryMove{Ply: 3, Color: "red", Piece: "P", From: []int{2, 3}, To: []int{2, 4}}
	require.Len(t, view.History, 3)
	assert.Equal(t, moves[:2], view.History[:2])
	assert.Equal(t, expected, view.History[2])
	assert.Equal(t, &expected, view.LastMoves.Red)
	assert.Equal(t, &moves[1], view.LastMoves.Black)
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
