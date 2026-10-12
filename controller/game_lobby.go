package controller

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// ListGameLobby 汇总各游戏的公开等待房间及当前玩家自己的未结束房间。
// 私密房间仅作为本人的恢复入口返回，游戏关闭后也保留已有对局的入口。
func ListGameLobby(c *gin.Context) {
	filter := c.Query("game")
	if filter != "" && pvpGames[filter] == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	active, err := model.GetUserActiveGameRooms(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	resumable := make([]*model.GameRoom, 0, len(active))
	for _, room := range active {
		if filter != "" && room.Game != filter {
			continue
		}
		if _, err := resolveGameRoom(room); err != nil {
			common.ApiError(c, err)
			return
		}
		if room.Status == model.GameRoomStatusWaiting || room.Status == model.GameRoomStatusPlaying {
			resumable = append(resumable, room)
		}
	}
	setting := operation_setting.GetGameSetting()
	games := make([]string, 0, len(pvpGames))
	for key := range pvpGames {
		if (filter == "" || filter == key) && setting.GameEnabled(key) {
			games = append(games, key)
		}
	}
	sort.Strings(games)
	rooms, err := model.ListWaitingPublicGameRooms(games, common.GetTimestamp()-pvpWaitingSeconds, pvpLobbyLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	all := append(append([]*model.GameRoom{}, rooms...), resumable...)
	seats, names, err := pvpRoomParticipants(all)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	listed := make([]gin.H, 0, len(rooms))
	for _, room := range rooms {
		if common.GetTimestamp()-room.CreatedAt > pvpWaitingSeconds || room.Players >= room.Seats {
			continue
		}
		listed = append(listed, pvpLobbyRoomSummary(room, userId, seats[room.Id], names))
	}
	owned := make([]gin.H, 0, len(resumable))
	for _, room := range resumable {
		owned = append(owned, pvpLobbyRoomSummary(room, userId, seats[room.Id], names))
	}
	common.ApiSuccess(c, gin.H{"rooms": listed, "active_rooms": owned})
}

func pvpLobbyRoomSummary(room *model.GameRoom, viewer int, seats []model.GameRoomSeat, names map[int]string) gin.H {
	mySeat := -1
	for _, member := range seats {
		if member.UserId == viewer {
			mySeat = member.Seat
			break
		}
	}
	return gin.H{
		"id": room.Id, "game": room.Game, "status": room.Status,
		"stake": room.Stake, "escrow": room.Escrow,
		"seats": room.Seats, "player_count": room.Players,
		"locked": room.Locked(), "private": room.Private,
		"members": pvpRoomMembers(room, seats, names), "host": names[room.HostId],
		"own": mySeat >= 0, "seat": mySeat, "created_at": room.CreatedAt,
	}
}
