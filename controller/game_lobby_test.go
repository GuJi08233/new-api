package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 大厅摘要是可见成员信息，不能暴露邀请码、密码、内部状态或来源 IP。
func assertGameLobbyRoomSanitized(t *testing.T, room map[string]any) {
	t.Helper()
	for _, key := range []string{"code", "password", "password_hash", "state", "ip", "host_ip", "guest_ip"} {
		assert.NotContains(t, room, key)
	}
	members, ok := room["members"].([]any)
	require.True(t, ok)
	for _, value := range members {
		member, ok := value.(map[string]any)
		require.True(t, ok)
		for _, key := range []string{"ip", "password", "password_hash", "state"} {
			assert.NotContains(t, member, key)
		}
	}
}

func TestGameLobbyListsCrossGameRoomsWithoutJoiningOrLeakingSecrets(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		rule := operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 120}
		s.Rps, s.Landlord, s.Gomoku = rule, rule, rule
	}))
	rpsHost := env.player(100, gameTestIpA)
	landlordHost := env.player(100, gameTestIpB)
	privateHost := env.player(100, "192.0.2.3")
	member := env.player(100, "192.0.2.4")
	viewer := env.player(100, "192.0.2.5")
	rps := rpsHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 17, "password": "public-secret"})
	landlord := landlordHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 11})
	member.ok(http.MethodPost, gameRoomPath(jsonInt(t, landlord["id"]), "join"), nil)
	private := privateHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 9, "private": true, "password": "private-secret"})

	before := make(map[int]model.GameRoom)
	for _, created := range []map[string]any{rps, landlord, private} {
		room, err := model.GetGameRoom(jsonInt(t, created["id"]))
		require.NoError(t, err)
		before[room.Id] = *room
	}
	var logsBefore int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Count(&logsBefore).Error)

	lobby := viewer.ok(http.MethodGet, "/api/game/lobby", nil)
	rooms := lobby["rooms"].([]any)
	require.Len(t, rooms, 2)
	assert.Empty(t, lobby["active_rooms"])
	byGame := make(map[string]map[string]any)
	for _, value := range rooms {
		room := value.(map[string]any)
		byGame[room["game"].(string)] = room
		assertGameLobbyRoomSanitized(t, room)
		assert.Equal(t, false, room["own"])
		assert.EqualValues(t, -1, room["seat"])
		assert.Equal(t, false, room["private"])
		assert.Equal(t, model.GameRoomStatusWaiting, room["status"])
	}
	require.Contains(t, byGame, gameRps)
	require.Contains(t, byGame, gameLandlord)
	listedRps, listedLandlord := byGame[gameRps], byGame[gameLandlord]
	assert.Equal(t, rps["id"], listedRps["id"])
	assert.Equal(t, 17.0, listedRps["stake"])
	assert.Equal(t, 17.0, listedRps["escrow"])
	assert.EqualValues(t, 2, listedRps["seats"])
	assert.EqualValues(t, 1, listedRps["player_count"])
	assert.Equal(t, true, listedRps["locked"])
	assert.Equal(t, []any{map[string]any{"seat": 0.0, "user_id": float64(rpsHost.user.Id), "name": rpsHost.user.Username, "net": 0.0, "result": "", "auto": false}}, listedRps["members"])
	assert.Equal(t, landlord["id"], listedLandlord["id"])
	assert.Equal(t, 11.0, listedLandlord["stake"])
	assert.Equal(t, 22.0, listedLandlord["escrow"], "landlord seats escrow twice the stake")
	assert.EqualValues(t, 3, listedLandlord["seats"])
	assert.EqualValues(t, 2, listedLandlord["player_count"])
	assert.Equal(t, false, listedLandlord["locked"])
	assert.Equal(t, []any{
		map[string]any{"seat": 0.0, "user_id": float64(landlordHost.user.Id), "name": landlordHost.user.Username, "net": 0.0, "result": "", "auto": false},
		map[string]any{"seat": 1.0, "user_id": float64(member.user.Id), "name": member.user.Username, "net": 0.0, "result": "", "auto": false},
	}, listedLandlord["members"])

	filtered := viewer.ok(http.MethodGet, "/api/game/lobby?game="+gameLandlord, nil)
	require.Len(t, filtered["rooms"], 1)
	assert.Equal(t, landlord["id"], filtered["rooms"].([]any)[0].(map[string]any)["id"])
	assert.Empty(t, filtered["active_rooms"])
	viewer.fails(http.MethodGet, "/api/game/lobby?game=unknown", nil)
	assert.Equal(t, 100.0, viewer.balance(), "浏览大厅与筛选不得扣币")
	for id, expected := range before {
		room, err := model.GetGameRoom(id)
		require.NoError(t, err)
		assert.Equal(t, expected, *room, "浏览大厅不修改他人的房间")
	}
	var logsAfter int64
	require.NoError(t, env.db.Model(&model.GameCoinLog{}).Count(&logsAfter).Error)
	assert.Equal(t, logsBefore, logsAfter, "浏览大厅不产生扣币或入座流水")
}

func TestGameLobbyKeepsPrivateAndDisabledGameRecoveryAcrossGames(t *testing.T) {
	setting := gameSetting(func(s *operation_setting.GameSetting) {
		rule := operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 120}
		s.Rps, s.Landlord, s.Gomoku = rule, rule, rule
	})
	env := newGameTestEnv(t, setting)
	owner, rival := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	publicHost, viewer := env.player(100, "192.0.2.3"), env.player(100, "192.0.2.4")
	gomoku := owner.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 5, "private": true, "password": "private-secret"})
	rival.ok(http.MethodPost, "/api/game/rooms/join", map[string]any{"code": gomoku["code"], "password": "private-secret"})
	rps := owner.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 7})
	landlord := owner.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 9, "private": true})
	publicHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameGomoku, "stake": 11})

	setting.Gomoku.Enabled = false
	other := viewer.ok(http.MethodGet, "/api/game/lobby", nil)
	require.Len(t, other["rooms"], 1, "关闭游戏的公开房间与他人的私密房间不进入大厅")
	assert.Equal(t, rps["id"], other["rooms"].([]any)[0].(map[string]any)["id"])
	assert.Empty(t, other["active_rooms"])
	lobby := owner.ok(http.MethodGet, "/api/game/lobby", nil)
	active := lobby["active_rooms"].([]any)
	require.Len(t, active, 3)
	byGame := make(map[string]map[string]any)
	for _, value := range active {
		room := value.(map[string]any)
		byGame[room["game"].(string)] = room
		assert.Equal(t, true, room["own"])
		assert.EqualValues(t, 0, room["seat"])
		assertGameLobbyRoomSanitized(t, room)
	}
	require.Contains(t, byGame, gameGomoku)
	require.Contains(t, byGame, gameRps)
	require.Contains(t, byGame, gameLandlord)
	assert.Equal(t, gomoku["id"], byGame[gameGomoku]["id"])
	assert.Equal(t, model.GameRoomStatusPlaying, byGame[gameGomoku]["status"])
	assert.Equal(t, true, byGame[gameGomoku]["private"])
	assert.Equal(t, landlord["id"], byGame[gameLandlord]["id"])
	assert.Equal(t, true, byGame[gameLandlord]["private"])
	assert.Equal(t, rps["id"], byGame[gameRps]["id"])

	setting.Enabled = false
	closed := owner.ok(http.MethodGet, "/api/game/lobby", nil)
	assert.Empty(t, closed["rooms"])
	assert.Len(t, closed["active_rooms"], 3, "总开关关闭后仍保留本人的所有游戏恢复入口")
	filtered := owner.ok(http.MethodGet, "/api/game/lobby?game="+gameGomoku, nil)
	assert.Empty(t, filtered["rooms"])
	require.Len(t, filtered["active_rooms"], 1)
	assert.Equal(t, gomoku["id"], filtered["active_rooms"].([]any)[0].(map[string]any)["id"])
	assert.Equal(t, 70.0, owner.balance(), "恢复入口不重复扣押注")
	assert.Equal(t, 95.0, rival.balance())
}

func TestGameLobbyExpiresOwnRoomAndHidesStalePublicRoom(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		rule := operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 120}
		s.Rps, s.Landlord = rule, rule
	}))
	owner := env.player(100, gameTestIpA)
	staleHost := env.player(100, gameTestIpB)
	validHost := env.player(100, "192.0.2.3")
	own := owner.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 17, "private": true})
	stale := staleHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameLandlord, "stake": 19})
	valid := validHost.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 7})
	ownId, staleId, validId := jsonInt(t, own["id"]), jsonInt(t, stale["id"]), jsonInt(t, valid["id"])
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id IN ?", []int{ownId, staleId}).
		Update("created_at", common.GetTimestamp()-pvpWaitingSeconds-1).Error)
	before, err := model.GetGameRoom(validId)
	require.NoError(t, err)
	lobby := owner.ok(http.MethodGet, "/api/game/lobby", nil)
	assert.Empty(t, lobby["active_rooms"])
	rooms := lobby["rooms"].([]any)
	require.Len(t, rooms, 1)
	assert.Equal(t, valid["id"], rooms[0].(map[string]any)["id"])
	assert.Equal(t, 100.0, owner.balance(), "过期的自有房间退回全部押注")
	expired, err := model.GetGameRoom(ownId)
	require.NoError(t, err)
	assert.Equal(t, model.GameRoomStatusCancelled, expired.Status)
	assert.Equal(t, model.GameRoomEndExpired, expired.EndReason)
	owner.ok(http.MethodGet, "/api/game/lobby", nil)
	assert.Equal(t, 100.0, owner.balance(), "再次读取不重复退押注")
	after, err := model.GetGameRoom(validId)
	require.NoError(t, err)
	assert.Equal(t, *before, *after)
	assert.Equal(t, 93.0, validHost.balance(), "浏览不修改其他有效房间的押注")
	var seatCount int64
	require.NoError(t, env.db.Model(&model.GameRoomSeat{}).Where("room_id = ?", validId).Count(&seatCount).Error)
	assert.EqualValues(t, 1, seatCount, "浏览不占用有效房间座位")
}

func TestGameLobbyExcludesExpiredRoomsBeforeApplyingLimit(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, MoveSeconds: 120}
	}))
	// 一整页较早的房间已经过期，每个房间仍有真实房主和座位；有效房间排在它们之后。
	expiredAt := common.GetTimestamp() - pvpWaitingSeconds - 60
	users := make([]model.User, pvpLobbyLimit)
	accounts := make([]model.GameCoinAccount, pvpLobbyLimit)
	expired := make([]model.GameRoom, pvpLobbyLimit)
	for i := range expired {
		id := int(gameTestUserSeq.Add(1))
		users[i] = model.User{
			Id: id, Username: fmt.Sprintf("old-lobby-%d", id), AffCode: fmt.Sprintf("old-aff-%d", id),
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		}
		accounts[i] = model.GameCoinAccount{UserId: id, CreatedAt: expiredAt, UpdatedAt: expiredAt}
		expired[i] = model.GameRoom{
			Game: gameRps, Status: model.GameRoomStatusWaiting, HostId: id,
			Seats: 2, Players: 1, MoveSeconds: 120, CreatedAt: expiredAt, Code: fmt.Sprintf("old%d", id),
		}
	}
	require.NoError(t, env.db.Create(&users).Error)
	require.NoError(t, env.db.Create(&accounts).Error)
	require.NoError(t, env.db.Create(&expired).Error)
	seats := make([]model.GameRoomSeat, len(expired))
	for i, room := range expired {
		seats[i] = model.GameRoomSeat{RoomId: room.Id, Seat: 0, UserId: room.HostId, Ip: gameTestIpA, CreatedAt: expiredAt}
	}
	require.NoError(t, env.db.Create(&seats).Error)
	host, viewer := env.player(100, gameTestIpA), env.player(100, gameTestIpB)
	valid := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 7})
	require.Greater(t, jsonInt(t, valid["id"]), expired[len(expired)-1].Id)
	lobby := viewer.ok(http.MethodGet, "/api/game/lobby", nil)
	rooms := lobby["rooms"].([]any)
	require.Len(t, rooms, 1, "过期房间必须在限制条数前过滤，不能挡住后面的有效房间")
	assert.Equal(t, valid["id"], rooms[0].(map[string]any)["id"])
	assert.Equal(t, 100.0, viewer.balance())
}
