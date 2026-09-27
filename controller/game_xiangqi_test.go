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

	resigned := black.ok(http.MethodPost, roomPath+"/resign", nil)
	assert.Equal(t, model.GameRoomStatusFinished, resigned["status"])
	assert.Equal(t, model.GameRoomEndResign, resigned["end_reason"])
	assert.EqualValues(t, red.user.Id, resigned["winner_id"])
	assert.Equal(t, 59, red.balance(), "50 - 10 + 20 - 1")
	assert.Equal(t, 40, black.balance())
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

	// 玩家执红，车 (8,7) 走到底线就将死黑将。
	mateInOne := "...K....." + strings.Repeat(".........", 6) + "........R" + "R........" + "....k...."
	board, err := common.Marshal(xiangqiState{RedSeat: 0, Game: xiangqi.Game{Board: mateInOne, Turn: xiangqi.Red, Plies: 30, Quiet: 4, Winner: xiangqi.NoColor}})
	require.NoError(t, err)
	aiState, err := common.Marshal(pvpAiState{Level: 3, Board: board, ToMove: 0, Winner: -1, Moves: 30})
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", after["session_id"]).
		Updates(map[string]any{"state": string(aiState), "created_at": common.GetTimestamp() - 200}).Error)

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
	assert.EqualValues(t, 2, result["reward"])
	assert.Equal(t, "", result["limited_by"])
	assert.Equal(t, 2, player.balance())
}
