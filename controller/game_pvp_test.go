package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPvpInvitePreviewShowsStakeWithoutJoining(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 20}
	}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{
		"game": gameRps, "stake": 17, "private": true, "password": "secret",
	})
	preview := guest.ok(http.MethodGet, "/api/game/rooms/invite?code="+created["code"].(string), nil)
	assert.Equal(t, map[string]any{
		"game": gameRps, "stake": 17.0, "escrow": 17.0, "seats": 2.0,
		"player_count": 1.0, "locked": true, "status": model.GameRoomStatusWaiting,
	}, preview)
	assert.Equal(t, 100, guest.balance())
	room, err := model.GetGameRoom(jsonInt(t, created["id"]))
	require.NoError(t, err)
	assert.Equal(t, 1, room.Players)
	assert.EqualValues(t, created["version"], room.Version)
	guest.fails(http.MethodGet, "/api/game/rooms/invite?code=invalid", nil)
	guest.fails(http.MethodGet, "/api/game/rooms/invite?code=xxxxxxxx", nil)
	host.ok(http.MethodPost, gameRoomPath(room.Id, "cancel"), nil)
	guest.fails(http.MethodGet, "/api/game/rooms/invite?code="+created["code"].(string), nil)
	assert.Equal(t, 100, guest.balance())
}

func TestPvpRoomPasswordAndSeatContract(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 20}
	}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	password := strings.Repeat("密", 24) // 72 字节边界允许完整保存和验证。
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{
		"game": gameRps, "stake": 10, "seats": 2, "password": password,
	})
	id := jsonInt(t, created["id"])
	assert.EqualValues(t, 2, created["seats"])
	assert.EqualValues(t, 1, created["player_count"])
	assert.EqualValues(t, 10, created["escrow"])
	assert.Equal(t, true, created["locked"])
	for _, removed := range []string{"players", "guest_id", "host_net", "guest_net"} {
		assert.NotContains(t, created, removed)
	}
	assert.Equal(t, []any{map[string]any{"seat": 0.0, "user_id": float64(host.user.Id), "name": host.user.Username, "net": 0.0, "result": "", "auto": false}}, created["members"])
	assert.NotContains(t, created, "password")
	assert.NotContains(t, created, "password_hash")
	stored, err := model.GetGameRoom(id)
	require.NoError(t, err)
	assert.NotEqual(t, password, stored.PasswordHash)
	assert.True(t, common.ValidatePasswordAndHash(password, stored.PasswordHash))

	lobby := guest.ok(http.MethodGet, "/api/game/rooms?game=rps", nil)
	rooms := lobby["rooms"].([]any)
	require.Len(t, rooms, 1)
	listed := rooms[0].(map[string]any)
	assert.Equal(t, true, listed["locked"])
	assert.Equal(t, created["members"], listed["members"])
	assert.EqualValues(t, 1, listed["player_count"])
	assert.NotContains(t, listed, "code")
	wrong := i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomPassword)
	assert.Equal(t, wrong, guest.fails(http.MethodPost, gameRoomPath(id, "join"), nil))
	assert.Equal(t, wrong, guest.fails(http.MethodPost, "/api/game/rooms/join", map[string]any{"code": created["code"], "password": "wrong"}))
	guest.fails(http.MethodPost, gameRoomPath(id, "join"), map[string]any{"password": password + "密"})
	assert.Equal(t, 100, guest.balance(), "密码失败不能扣币或占座")
	assert.Equal(t, created["version"], host.ok(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)["version"])

	joined := guest.ok(http.MethodPost, gameRoomPath(id, "join"), map[string]any{"password": password})
	assert.EqualValues(t, 2, joined["player_count"])
	assert.EqualValues(t, guest.user.Id, joined["members"].([]any)[1].(map[string]any)["user_id"])
	assert.NotContains(t, joined, "code")
	guest.fails(http.MethodPost, gameRoomPath(id, "leave"), nil)
	assert.Equal(t, 90, guest.balance(), "开局后不能离座退押注")
	finished := guest.ok(http.MethodPost, gameRoomPath(id, "resign"), nil)
	assert.EqualValues(t, host.user.Id, finished["winner_id"])
	assert.EqualValues(t, 1, finished["rake"])
	members := finished["members"].([]any)
	assert.EqualValues(t, 9, members[0].(map[string]any)["net"])
	assert.EqualValues(t, -10, members[1].(map[string]any)["net"])
	recent := host.ok(http.MethodGet, "/api/game/rps", nil)["recent"].([]any)
	require.Len(t, recent, 1)
	assert.Equal(t, "win", recent[0].(map[string]any)["result"])
	assert.EqualValues(t, 9, recent[0].(map[string]any)["net"])
	assert.Equal(t, 109, host.balance())
	assert.Equal(t, 90, guest.balance())
}

func TestPvpPrivatePasswordAndMatchIsolation(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MoveSeconds: 20}
	}))
	privateHost, lockedHost := env.player(0, gameTestIpA), env.player(0, gameTestIpB)
	guest, matcher := env.player(0, gameTestIpA), env.player(0, gameTestIpB)
	private := privateHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "private": true, "password": "private"})
	locked := lockedHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "password": "locked"})
	id := jsonInt(t, private["id"])
	guest.fails(http.MethodGet, fmt.Sprintf("/api/game/rooms/%d", id), nil)
	guest.fails(http.MethodPost, gameRoomPath(id, "join"), map[string]any{"password": "private"})
	lobby := guest.ok(http.MethodGet, "/api/game/rooms?game=rps", nil)["rooms"].([]any)
	require.Len(t, lobby, 1)
	assert.Equal(t, locked["id"], lobby[0].(map[string]any)["id"])
	matched := matcher.ok(http.MethodPost, "/api/game/rooms/match", map[string]any{"game": gameRps})
	assert.NotEqual(t, private["id"], matched["id"])
	assert.NotEqual(t, locked["id"], matched["id"])
	assert.Equal(t, model.GameRoomStatusWaiting, matched["status"])
	assert.Equal(t, false, matched["locked"])
	joined := guest.ok(http.MethodPost, "/api/game/rooms/join", map[string]any{"code": private["code"], "password": "private"})
	assert.Equal(t, private["id"], joined["id"])
	assert.NotContains(t, joined, "code")
	finished := guest.ok(http.MethodPost, gameRoomPath(id, "resign"), nil)
	assert.EqualValues(t, privateHost.user.Id, finished["winner_id"], "零押注也保留赢家")
	for _, member := range finished["members"].([]any) {
		assert.EqualValues(t, 0, member.(map[string]any)["net"])
	}
	recent := guest.ok(http.MethodGet, "/api/game/rps", nil)["recent"].([]any)
	require.Len(t, recent, 1)
	assert.Equal(t, "loss", recent[0].(map[string]any)["result"])
	matchedGuest := guest.ok(http.MethodPost, "/api/game/rooms/match", map[string]any{"game": gameRps})
	assert.Equal(t, matched["id"], matchedGuest["id"])
	assert.Equal(t, model.GameRoomStatusPlaying, matchedGuest["status"])
	assert.EqualValues(t, 2, matchedGuest["player_count"])
}

func TestPvpRoomRejectsUnsupportedSeatsAndOversizePassword(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 20}
	}))
	for _, request := range []map[string]any{
		{"game": gameRps, "stake": 10, "seats": 0},
		{"game": gameRps, "stake": 10, "seats": 3},
		{"game": gameRps, "stake": 10, "password": strings.Repeat("密", 25)},
	} {
		player := env.player(100, gameTestIpA)
		player.fails(http.MethodPost, "/api/game/rooms", request)
		player.fails(http.MethodPost, "/api/game/rooms/match", request)
		assert.Equal(t, 100, player.balance())
		rooms, err := model.GetUserActiveGameRooms(player.user.Id)
		require.NoError(t, err)
		assert.Empty(t, rooms)
	}
}

func TestPvpLeaveRefundsAndRevokesPrivateRoomPolling(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 20}
	}))
	host, guest := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	// 直接准备尚未坐满的座位房间，验证通用离座接口；游戏开房接口仍只接受两人规则。
	room := &model.GameRoom{Game: gameRps, HostId: host.user.Id, Seats: 3, Stake: 10, Escrow: 10, Private: true}
	_, err := model.CreateGameRoom(room, gameTestIpA)
	require.NoError(t, err)
	_, err = model.JoinGameRoom(room, guest.user.Id, gameTestIpB, func([]model.GameRoomSeat) (model.GameRoomStart, error) {
		return model.GameRoomStart{}, fmt.Errorf("room must remain waiting")
	})
	require.NoError(t, err)
	path := fmt.Sprintf("/api/game/rooms/%d", room.Id)
	polled := make(chan *httptest.ResponseRecorder, 1)
	go func() { polled <- guest.send(http.MethodGet, fmt.Sprintf("%s?after=%d", path, room.Version), nil) }()
	waitForRoomWatcher(t, room.Id)
	left := guest.ok(http.MethodPost, path+"/leave", nil)
	assert.EqualValues(t, -1, left["seat"])
	assert.EqualValues(t, 1, left["player_count"])
	assert.EqualValues(t, 100, left["balance"])
	assert.NotContains(t, left, "code")
	assert.Equal(t, 100, guest.balance())
	select {
	case response := <-polled:
		var body map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		assert.Equal(t, false, body["success"])
		assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoomNotPlayer), body["message"])
	case <-time.After(10 * time.Second):
		require.FailNow(t, "leaving must wake the poll and revoke access")
	}
	guest.fails(http.MethodGet, path, nil)
	guest.fails(http.MethodPost, path+"/leave", nil)
	closed := host.ok(http.MethodPost, path+"/leave", nil)
	assert.Equal(t, model.GameRoomStatusCancelled, closed["status"])
	assert.EqualValues(t, 100, closed["balance"])
	assert.Equal(t, 100, host.balance())
	assert.Equal(t, 100, guest.balance())
}

func TestPvpAiRestoreCommittedMoveAfterStaleVersion(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	request := map[string]any{"session_id": started["session_id"], "version": started["version"], "move": map[string]any{"col": 0}}
	committed := player.ok(http.MethodPost, "/api/game/connect4/ai/move", request)["session"].(map[string]any)
	assert.Equal(t, i18n.Translate(i18n.DefaultLang, i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/connect4/ai/move", request))

	path := fmt.Sprintf("/api/game/connect4/ai?session_id=%d", jsonInt(t, started["session_id"]))
	restored := player.ok(http.MethodGet, path, nil)["session"].(map[string]any)
	assert.Equal(t, committed, restored)
	assert.Greater(t, jsonInt(t, restored["version"]), jsonInt(t, started["version"]))
	request["version"] = restored["version"]
	continued := player.ok(http.MethodPost, "/api/game/connect4/ai/move", request)["session"].(map[string]any)
	assert.Equal(t, model.GameSessionStatusPlaying, continued["status"])
	assert.EqualValues(t, jsonInt(t, restored["moves"])+2, continued["moves"])
	assert.Equal(t, continued, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}

func TestPvpAiRestoreWinningResultDoesNotRewardAgain(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Gomoku = operation_setting.PvpRule{Enabled: true, Ai: boardAiRule()}
	}))
	player := env.player(0, gameTestIpA)
	// 玩家执黑已有横向四子，双方棋谱合法，下一手连五即结束，不需要 AI 搜索。
	board, err := common.Marshal(gomokuState{Black: 0, Moves: []int{0, 15, 1, 16, 2, 17, 3, 18}})
	require.NoError(t, err)
	state, err := common.Marshal(pvpAiState{Level: 1, Board: board, ToMove: 0, Winner: -1, Moves: 8})
	require.NoError(t, err)
	session, err := model.CreateGameSession(player.user.Id, pvpAiGame(gameGomoku), 1, string(state))
	require.NoError(t, err)
	finished := player.ok(http.MethodPost, "/api/game/gomoku/ai/move", map[string]any{
		"session_id": session.Id, "version": session.Version, "move": map[string]any{"x": 4, "y": 0},
	})
	result := finished["result"].(map[string]any)
	require.EqualValues(t, 1, result["reward"])
	path := fmt.Sprintf("/api/game/gomoku/ai?session_id=%d", session.Id)
	for range 2 {
		restored := player.ok(http.MethodGet, path, nil)
		view := restored["session"].(map[string]any)
		assert.Equal(t, model.GameSessionStatusFinished, view["status"])
		assert.Equal(t, true, view["over"])
		assert.EqualValues(t, 0, view["winner"])
		assert.Equal(t, finished["session"].(map[string]any)["state"], view["state"])
		assert.Equal(t, result, restored["result"])
	}
	assert.Equal(t, 1, player.balance())
	progress, err := model.GetGameRewardProgress(player.user.Id, gameTestIpA, pvpAiGame(gameGomoku), gameCenterDay())
	require.NoError(t, err)
	assert.Equal(t, 1, progress.Plays)
	assert.Equal(t, 1, progress.Rewards)
	var rewards int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Where("user_id = ? AND type = ?", player.user.Id, model.GameCoinLogTypeGameReward).Count(&rewards).Error)
	assert.EqualValues(t, 1, rewards)
}

func TestPvpAiRestoreResignedSessionWhileAnotherIsPlaying(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	finished := player.ok(http.MethodPost, "/api/game/connect4/ai/resign", map[string]any{"session_id": started["session_id"], "version": started["version"]})
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
	newer := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	path := fmt.Sprintf("/api/game/connect4/ai?session_id=%d", jsonInt(t, started["session_id"]))
	restored := player.ok(http.MethodGet, path, nil)
	view := restored["session"].(map[string]any)
	assert.Equal(t, started["session_id"], view["session_id"])
	assert.Equal(t, model.GameSessionStatusFinished, view["status"])
	assert.Equal(t, true, view["over"])
	assert.EqualValues(t, 1, view["winner"])
	assert.Equal(t, finished["session"].(map[string]any)["state"], view["state"])
	assert.Equal(t, finished["result"], restored["result"])
	assert.Equal(t, newer, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
	assert.Zero(t, player.balance())
}

func TestPvpAiRestoreRejectsUnownedAndInvalidSessions(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player, other := env.player(0, gameTestIpA), env.player(0, gameTestIpB)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	id := jsonInt(t, started["session_id"])
	missing := i18n.Translate(i18n.DefaultLang, i18n.MsgGameSessionNotFound)
	assert.Equal(t, missing, other.fails(http.MethodGet, fmt.Sprintf("/api/game/connect4/ai?session_id=%d", id), nil))
	assert.Equal(t, missing, player.fails(http.MethodGet, fmt.Sprintf("/api/game/gomoku/ai?session_id=%d", id), nil))
	for _, invalid := range []string{"", "0", "-1", "abc", "9223372036854775808", "999999999"} {
		assert.Equal(t, missing, player.fails(http.MethodGet, "/api/game/connect4/ai?session_id="+invalid, nil), "session_id=%q", invalid)
	}
	assert.Equal(t, started, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}

func TestPvpAiRestoreExpiredSessionReturnsNoSession(t *testing.T) {
	env := newGameTestEnv(t, connect4Setting(boardAiRule()))
	fixBoardAiRandom(t, 7)
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/connect4/ai/start", map[string]any{"level": 1})["session"].(map[string]any)
	id := jsonInt(t, started["session_id"])
	require.NoError(t, env.db.Model(&model.GameSession{}).Where("id = ?", id).Update("created_at", common.GetTimestamp()-skillSessionTTL-1).Error)
	assert.Nil(t, player.ok(http.MethodGet, fmt.Sprintf("/api/game/connect4/ai?session_id=%d", id), nil)["session"])
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/connect4/ai", nil)["session"])
}
