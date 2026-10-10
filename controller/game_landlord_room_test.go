package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLandlordRoomWaitsForThreeAndKeepsHandsPrivate(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Landlord = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 45}
	}))
	players := []*gamePlayer{env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, "192.0.2.3")}
	created := players[0].ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 10, "seats": 3})
	id := jsonInt(t, created["id"])
	assert.EqualValues(t, 3, created["seats"])
	second := players[1].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	assert.Equal(t, model.GameRoomStatusWaiting, second["status"])
	assert.EqualValues(t, 2, second["player_count"])
	assert.NotContains(t, second, "state")
	third := players[2].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
	assert.Equal(t, model.GameRoomStatusPlaying, third["status"])
	assert.EqualValues(t, 3, third["player_count"])
	for seat, player := range players {
		assert.Equal(t, 90, player.balance())
		view := player.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
		assert.EqualValues(t, seat, view["seat"])
		state := view["state"].(map[string]any)
		assert.Len(t, state["hand"], 17)
		assert.Equal(t, []any{17.0, 17.0, 17.0}, state["counts"])
		assert.Empty(t, state["bottom"])
		assert.NotContains(t, state, "hands")
	}
	first := jsonInt(t, third["to_move"])
	playing := players[first].ok(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": third["version"], "move": map[string]any{"bid": 3}})
	state := playing["state"].(map[string]any)
	assert.Equal(t, "playing", state["phase"])
	assert.Len(t, state["hand"], 20)
	assert.Len(t, state["bottom"], 3)
	players[(first+1)%3].fails(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": playing["version"], "move": map[string]any{"cards": []int{0}}})
	players[first].fails(http.MethodPost, gameRoomPath(id, "move"), map[string]any{"version": third["version"], "move": map[string]any{"pass": true}})
	for _, player := range players {
		assert.Equal(t, 90, player.balance(), "叫分不得追加扣币")
	}
}

func TestLandlordRoomTeamSettlementAndForfeit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stake    int
		ending   string
		seat     int
		winners  []int
		balances []int
	}{
		{"landlord finishes", 10, "play", 0, []int{0}, []int{118, 90, 90}},
		{"farmers finish", 10, "play", 1, []int{1, 2}, []int{90, 105, 105}},
		{"small farmer pool", 1, "play", 2, []int{1, 2}, []int{99, 101, 100}},
		{"free team win", 0, "play", 1, []int{1, 2}, []int{100, 100, 100}},
		{"landlord resigns", 10, "resign", 0, []int{1, 2}, []int{90, 105, 105}},
		{"farmer resigns", 10, "resign", 2, []int{0}, []int{118, 90, 90}},
		{"landlord times out", 10, "timeout", 0, []int{1, 2}, []int{90, 105, 105}},
		{"farmer times out", 10, "timeout", 1, []int{0}, []int{118, 90, 90}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
				s.Landlord = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 10, MoveSeconds: 45}
			}))
			players := []*gamePlayer{env.player(100, gameTestIpA), env.player(100, gameTestIpB), env.player(100, "192.0.2.3")}
			created := players[0].ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": tc.stake})
			id := jsonInt(t, created["id"])
			players[1].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
			third := players[2].ok(http.MethodPost, gameRoomPath(id, "join"), nil)
			// 准备守恒的残局，测试真实落子/超时/认输接口和团队钱包结算。
			position := landlordTestPlaying(t, 0, tc.seat, [3][]int{{0}, {4}, {8}})
			deadline := common.GetTimestamp() + 45
			if tc.ending == "timeout" {
				deadline = common.GetTimestamp() - 1
			}
			require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", id).Updates(map[string]any{
				"state": string(position.Data), "to_move": tc.seat, "deadline": deadline,
			}).Error)
			var finished map[string]any
			switch tc.ending {
			case "play":
				finished = players[tc.seat].ok(http.MethodPost, gameRoomPath(id, "move"), map[string]any{
					"version": third["version"], "move": map[string]any{"cards": []int{tc.seat * 4}},
				})
			case "resign":
				finished = players[tc.seat].ok(http.MethodPost, gameRoomPath(id, "resign"), nil)
			case "timeout":
				finished = players[0].ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
			}
			assert.Equal(t, model.GameRoomStatusFinished, finished["status"])
			members := finished["members"].([]any)
			for seat, player := range players {
				won := false
				for _, winner := range tc.winners {
					won = won || winner == seat
				}
				want := "loss"
				if won {
					want = "win"
				}
				assert.Equal(t, want, members[seat].(map[string]any)["result"])
				assert.Equal(t, tc.balances[seat], player.balance())
				recent := player.ok(http.MethodGet, "/api/game/landlord", nil)["recent"].([]any)
				require.Len(t, recent, 1)
				assert.Equal(t, want, recent[0].(map[string]any)["result"])
			}
			players[0].ok(http.MethodPost, gameRoomPath(id, "resign"), nil)
			for seat, player := range players {
				assert.Equal(t, tc.balances[seat], player.balance(), "已结束的房间不得重复结算")
			}
		})
	}
}

func TestLandlordMatchFillsThreeSeatsAndRejectsWrongSize(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Landlord = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 45}
	}))
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
		assert.Equal(t, 90, player.balance())
	}
}
