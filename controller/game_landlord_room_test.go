package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/landlord"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// landlordTestTable 开一个坐满三人的斗地主房间，三个玩家各有 100 个游戏币、来自不同的 IP。
func landlordTestTable(t *testing.T, env *gameTestEnv, stake int) ([]*gamePlayer, int, map[string]any) {
	t.Helper()
	players := []*gamePlayer{env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, "192.0.2.3")}
	created := players[0].ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": stake})
	id := jsonInt(t, created["id"])
	players[1].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	started := players[2].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	require.Equal(t, model.GameRoomStatusPlaying, started["status"])
	return players, id, started
}

// landlordTestPosition 把房间换成给定的局面，轮到 toMove，这一步在 deadline 截止。
func landlordTestPosition(t *testing.T, env *gameTestEnv, id int, state pvpState, autoSeats int, deadline int64) {
	t.Helper()
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", id).Updates(map[string]any{
		"state": string(state.Data), "to_move": state.ToMove, "deadline": deadline, "auto_seats": autoSeats,
	}).Error)
}

func landlordTestRule(rake int) func(*operation_setting.GameSetting) {
	return func(s *operation_setting.GameSetting) {
		s.Landlord = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: rake, MoveSeconds: 45}
	}
}

func TestLandlordRoomWaitsForThreeAndKeepsHandsPrivate(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(landlordTestRule(0)))
	players := []*gamePlayer{env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, "192.0.2.3")}
	created := players[0].ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 10, "seats": 3})
	id := jsonInt(t, created["id"])
	assert.EqualValues(t, 3, created["seats"])
	assert.Equal(t, 20.0, created["escrow"], "anyone may become the landlord, who can lose twice the stake")
	second := players[1].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	assert.Equal(t, model.GameRoomStatusWaiting, second["status"])
	assert.EqualValues(t, 2, second["player_count"])
	assert.NotContains(t, second, "state")
	third := players[2].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	assert.Equal(t, model.GameRoomStatusPlaying, third["status"])
	assert.EqualValues(t, 3, third["player_count"])
	for seat, player := range players {
		assert.Equal(t, 80.0, player.balance())
		view := player.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
		assert.EqualValues(t, seat, view["seat"])
		state := view["state"].(map[string]any)
		assert.Len(t, state["hand"], landlord.HandSize)
		assert.Equal(t, []any{17.0, 17.0, 17.0}, state["counts"])
		assert.Empty(t, state["bottom"])
		assert.NotContains(t, state, "hands")
	}
	first := jsonInt(t, third["to_move"])
	playing := players[first].ok(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": third["version"], "move": map[string]any{"bid": 3}})
	state := playing["state"].(map[string]any)
	assert.Equal(t, landlord.PhasePlaying, state["phase"])
	assert.Len(t, state["hand"], landlord.HandSize+landlord.BottomSize)
	assert.Len(t, state["bottom"], landlord.BottomSize)
	players[(first+1)%3].fails(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": playing["version"], "move": map[string]any{"cards": []int{0}}})
	players[first].fails(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": third["version"], "move": map[string]any{"pass": true}})
	for _, player := range players {
		assert.Equal(t, 80.0, player.balance(), "bidding never takes more coins")
	}
}

// 地主输赢两倍押注、两位农民各输赢一倍；平台从赢家的净赢抽成，农民退回没输掉的那一倍押注。
func TestLandlordRoomSettlesTwoToOne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stake    int
		finisher int
		results  []string
		balances []float64
	}{
		{"landlord finishes", 10, 0, []string{"win", "loss", "loss"}, []float64{118, 90, 90}},
		{"a farmer finishes for both", 10, 1, []string{"loss", "win", "win"}, []float64{80, 109, 109}},
		{"rake takes a fraction of a coin", 1, 2, []string{"loss", "win", "win"}, []float64{98, 100.9, 100.9}},
		{"friendly game records the result", 0, 1, []string{"loss", "win", "win"}, []float64{100, 100, 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(landlordTestRule(10)))
			players, id, started := landlordTestTable(t, env, tc.stake)
			landlordTestPosition(t, env, id, landlordTestEndgame(t, 0, tc.finisher, [3][]int{{0}, {4}, {8}}), 0, common.GetTimestamp()+45)
			finished := players[tc.finisher].ok(http.MethodPost, gameRoomPath(id, "move"), map[string]any{
				"version": started["version"], "move": map[string]any{"cards": []int{tc.finisher * 4}},
			})
			assert.Equal(t, model.GameRoomStatusFinished, finished["status"])
			assert.Equal(t, model.GameRoomEndWin, finished["end_reason"])
			members := finished["members"].([]any)
			for seat, player := range players {
				assert.Equal(t, tc.results[seat], members[seat].(map[string]any)["result"])
				assert.Equal(t, tc.balances[seat], player.balance())
				recent := player.ok(http.MethodGet, "/api/game/landlord", nil)["recent"].([]any)
				require.Len(t, recent, 1)
				assert.Equal(t, tc.results[seat], recent[0].(map[string]any)["result"])
			}
			for seat, player := range players {
				player.ok(http.MethodPost, gameRoomPath(id, "resign"), nil)
				assert.Equal(t, tc.balances[seat], player.balance(), "a finished room is never settled again")
			}
		})
	}
}

// 多人对局不能认输：认输会连累队友，要离开就托管。
func TestLandlordRoomRejectsResign(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(landlordTestRule(0)))
	players, id, _ := landlordTestTable(t, env, 10)
	assert.Equal(t, gameMessage(i18n.MsgGameRoomResignTeam), players[1].fails(http.MethodPost, gameRoomPath(id, "resign"), nil))
	room := players[1].ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
	assert.Equal(t, model.GameRoomStatusPlaying, room["status"])
	for _, player := range players {
		assert.Equal(t, 80.0, player.balance())
	}
}

// 超时不判负：AI 替超时的玩家走这一步，他转为托管，下一位玩家照常有完整的限时；他自己再走一步就不再托管。
func TestLandlordRoomTimeoutHandsTheSeatToTheAi(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(landlordTestRule(0)))
	players, id, started := landlordTestTable(t, env, 10)
	first := jsonInt(t, started["to_move"])
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", id).Update("deadline", common.GetTimestamp()-1).Error)

	view := players[(first+1)%3].ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
	assert.Equal(t, model.GameRoomStatusPlaying, view["status"])
	assert.EqualValues(t, 1<<first, view["auto_seats"])
	state := view["state"].(map[string]any)
	bids := state["bids"].([]any)
	assert.NotEqualValues(t, -1, bids[first], "the ai bid for the seat that timed out")
	for seat, member := range view["members"].([]any) {
		assert.Equal(t, seat == first, member.(map[string]any)["auto"])
	}
	if next := jsonInt(t, view["to_move"]); state["phase"] == landlord.PhaseBidding && next != first {
		assert.GreaterOrEqual(t, jsonInt(t, view["deadline"]), int(common.GetTimestamp())+44, "the next human gets the full move time")
	}
	for _, player := range players {
		assert.Equal(t, 80.0, player.balance(), "a timeout costs nothing by itself")
	}

	// 轮到托管的座位时，AI 很快替他走；他自己走一步就收回托管。
	position := landlordTestEndgame(t, 1, first, [3][]int{{0, 12}, {4, 16}, {8, 20}})
	landlordTestPosition(t, env, id, position, 1<<first, common.GetTimestamp()+45)
	room, err := model.GetGameRoom(id)
	require.NoError(t, err)
	var game landlord.Game
	require.NoError(t, common.Unmarshal(position.Data, &game))
	moved := players[first].ok(http.MethodPost, gameRoomPath(id, "move"), map[string]any{
		"version": room.Version, "move": map[string]any{"cards": []int{game.Hands[first][0]}},
	})
	assert.EqualValues(t, 0, moved["auto_seats"], "playing a card yourself ends auto-play")
}

// 托管中的座位轮到时只等 pvpAutoMoveSeconds 秒；三个座位都托管时桌上没人在玩，AI 一口气下完并按规则结算。
func TestLandlordRoomEveryoneOnAutoPlaysTheGameOut(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(landlordTestRule(10)))
	players, id, started := landlordTestTable(t, env, 10)
	first := jsonInt(t, started["to_move"])
	others := (1<<3 - 1) &^ (1 << first)
	room, err := model.GetGameRoom(id)
	require.NoError(t, err)
	landlordTestPosition(t, env, id, pvpState{Data: []byte(room.State), ToMove: first}, others, common.GetTimestamp()-1)

	finished := players[first].ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
	require.Equal(t, model.GameRoomStatusFinished, finished["status"])
	assert.Equal(t, model.GameRoomEndWin, finished["end_reason"])
	state := finished["state"].(map[string]any)
	require.Equal(t, true, state["over"])
	landlordSeat := jsonInt(t, state["landlord"])
	landlordWon := jsonInt(t, state["winner"]) == landlordSeat
	total := 0.0
	for seat, player := range players {
		won := (seat == landlordSeat) == landlordWon
		want := 100 - 10
		switch {
		case seat == landlordSeat && won:
			want = 100 + 20 - 2
		case seat == landlordSeat:
			want = 100 - 20
		case won:
			want = 100 + 10 - 1
		}
		assert.Equal(t, float64(want), player.balance(), "seat %d", seat)
		total += player.balance()
	}
	assert.Equal(t, 300-finished["rake"].(float64), total, "coins are only moved between players and the rake")
}

// 托管不改变这一步从哪里开始计时：轮到自己时打开托管，AI 在这一步开始两秒后替自己走；取消托管拿回这一步剩下的时间，来回
// 切换也拖不了时间。双人对战不能托管。
func TestLandlordRoomAutoToggleKeepsTheTurnClock(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		landlordTestRule(0)(s)
		s.Gomoku = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 60}
	}))
	players, id, started := landlordTestTable(t, env, 10)
	first := jsonInt(t, started["to_move"])
	other := (first + 1) % 3
	// 这一步刚开始：截止时间是开始时间加上 45 秒的每步限时。
	turnStart := common.GetTimestamp()
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", id).Update("deadline", turnStart+45).Error)

	waiting := players[other].ok(http.MethodPost, gameRoomPath(id, "auto"), map[string]any{"auto": true})
	assert.EqualValues(t, 1<<other, waiting["auto_seats"])
	assert.EqualValues(t, turnStart+45, waiting["deadline"], "another seat going on auto leaves this turn alone")

	for range 3 {
		on := players[first].ok(http.MethodPost, gameRoomPath(id, "auto"), map[string]any{"auto": true})
		assert.EqualValues(t, turnStart+pvpAutoMoveSeconds, on["deadline"], "the ai plays two seconds after the turn started")
		assert.EqualValues(t, first, on["to_move"])
		off := players[first].ok(http.MethodPost, gameRoomPath(id, "auto"), map[string]any{"auto": false})
		assert.EqualValues(t, turnStart+45, off["deadline"], "cancelling auto-play hands back the rest of the turn, never more")
		assert.EqualValues(t, 1<<other, off["auto_seats"])
	}
	players[first].fails(http.MethodPost, gameRoomPath(id, "auto"), map[string]any{})

	// 这一步已经过了两秒才打开托管：AI 在这个请求里就替他叫分。叫 3 分时成为地主、仍由他出牌。
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", id).Update("deadline", common.GetTimestamp()+45-10).Error)
	played := players[first].ok(http.MethodPost, gameRoomPath(id, "auto"), map[string]any{"auto": true})
	state := played["state"].(map[string]any)
	bid := jsonInt(t, state["bids"].([]any)[first])
	assert.NotEqual(t, -1, bid, "the ai bid for the seat")
	if bid == landlord.MaxBid {
		assert.Equal(t, landlord.PhasePlaying, state["phase"])
		assert.EqualValues(t, first, state["landlord"])
		assert.EqualValues(t, first, played["to_move"])
	} else {
		assert.Equal(t, landlord.PhaseBidding, state["phase"])
		assert.EqualValues(t, (first+1)%landlord.Players, played["to_move"])
	}
	assert.EqualValues(t, 1<<first|1<<other, played["auto_seats"])

	gomoku := []*gamePlayer{env.player(0, gameTestIpA), env.player(0, gameTestIpB)}
	room := gomoku[0].ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 0})
	gomoku[1].ok(http.MethodPost, gameRoomPath(jsonInt(t, room["id"]), "join"), nil)
	assert.Equal(t, gameMessage(i18n.MsgGameRoomAutoUnsupported),
		gomoku[0].fails(http.MethodPost, gameRoomPath(jsonInt(t, room["id"]), "auto"), map[string]any{"auto": true}))
}

func TestLandlordMatchFillsThreeSeatsAndRejectsWrongSize(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(landlordTestRule(0)))
	players := []*gamePlayer{env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, "192.0.2.3")}
	players[0].fails(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 10, "seats": 2})
	players[0].fails(http.MethodPost, "/api/game/rooms/match", map[string]any{"game": gameLandlord, "stake": 10, "seats": 8})
	var id any
	for seat, player := range players {
		room := player.ok(http.MethodPost, "/api/game/rooms/match", map[string]any{"game": gameLandlord, "stake": 10})
		if seat == 0 {
			id = room["id"]
		}
		assert.Equal(t, id, room["id"])
		assert.EqualValues(t, seat+1, room["player_count"])
		want := model.GameRoomStatusWaiting
		if seat == 2 {
			want = model.GameRoomStatusPlaying
		}
		assert.Equal(t, want, room["status"])
		assert.Equal(t, 80.0, player.balance())
	}
}

// 人机对战：玩家坐座位 0，两个座位都是 AI。玩家当农民时，AI 队友先出完也算玩家赢，按难度领奖。
func TestLandlordAiPartnerWinningCountsForThePlayer(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Landlord = operation_setting.PvpRule{Enabled: true, MoveSeconds: 45, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	// 地主是座位 1(AI)，只剩一张 3；玩家领出 4，地主压不住，队友(座位 2)用最后一张 5 出完。
	board := landlordTestEndgame(t, 1, 0, landlordTestHands(t, []int{4, 9}, []int{3}, []int{5}))
	state, err := common.Marshal(pvpAiState{Level: 1, Board: board.Data, ToMove: 0, Winner: -1})
	require.NoError(t, err)
	session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameLandlord), 1, string(state))
	require.NoError(t, err)
	var game landlord.Game
	require.NoError(t, common.Unmarshal(board.Data, &game))
	finished := player.ok(http.MethodPost, "/api/game/landlord/ai/move", map[string]any{
		"session_id": session.Id, "version": session.Version, "move": map[string]any{"cards": game.Hands[0][:1]},
	})
	view := finished["session"].(map[string]any)
	require.Equal(t, true, view["over"])
	assert.Equal(t, "win", view["result"])
	assert.EqualValues(t, 3, view["seats"])
	assert.EqualValues(t, 2, view["state"].(map[string]any)["winner"], "the ai partner went out first")
	assert.Equal(t, 1.0, finished["result"].(map[string]any)["reward"])
	assert.Equal(t, 1.0, player.balance())
}

// 和 AI 打一整局：玩家这边也交给 AI 出牌，每一步都经过接口，局面里始终只有玩家自己的牌，结束后奖励与胜负一致。
func TestLandlordAiGamePlaysToTheEndOverHttp(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Landlord = operation_setting.PvpRule{Enabled: true, MoveSeconds: 45, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	session := player.ok(http.MethodPost, "/api/game/landlord/ai/start", map[string]any{"level": 3})["session"].(map[string]any)
	var result map[string]any
	for steps := 0; session["over"] != true; steps++ {
		require.Less(t, steps, 100)
		require.EqualValues(t, 0, session["to_move"], "the ais hand the turn back to the player")
		stored, err := model.GetUserGameSession(player.user.Id, pvpAiGame(gameLandlord), jsonInt(t, session["session_id"]))
		require.NoError(t, err)
		var saved pvpAiState
		require.NoError(t, common.UnmarshalJsonStr(stored.State, &saved))
		view := session["state"].(map[string]any)
		assert.NotContains(t, view, "hands")
		assert.Len(t, view["hand"], jsonInt(t, view["counts"].([]any)[0]))
		move, err := (landlordEngine{}).AiMove(saved.Board, 0, 2)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(move, &decoded))
		response := player.ok(http.MethodPost, "/api/game/landlord/ai/move", map[string]any{
			"session_id": session["session_id"], "version": session["version"], "move": decoded,
		})
		session = response["session"].(map[string]any)
		if response["result"] != nil {
			result = response["result"].(map[string]any)
		}
	}
	require.NotNil(t, result)
	reward := 0
	if session["result"] == "win" {
		reward = 2 // 赢下困难难度是第二档
	} else {
		assert.Equal(t, "loss", session["result"], "landlord games never end in a draw")
	}
	assert.Equal(t, float64(reward), result["reward"])
	assert.Equal(t, float64(reward), player.balance())
}

// landlordTestHands 按点数给三家发牌，同一点数依次取不同花色。
func landlordTestHands(t *testing.T, seats ...[]int) [landlord.Players][]int {
	t.Helper()
	var hands [landlord.Players][]int
	var suits [landlord.RankBigJoker + 1]int
	for seat, ranks := range seats {
		for _, rank := range ranks {
			card := rank - landlord.RankSmallJoker + landlord.SmallJoker
			if rank < landlord.RankSmallJoker {
				card = (rank-3)*4 + suits[rank]
				suits[rank]++
			}
			hands[seat] = append(hands[seat], card)
		}
	}
	return hands
}
