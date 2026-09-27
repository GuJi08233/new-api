package model

import (
	"errors"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 对战房间。房主开房时押下 Stake 个游戏币，对手加入时押下同样多，开局；分出胜负后赢家拿走两份押注，平台按比例抽成，
// 平局各自退回，等不到对手时房主可以取消、押注退回。押注为 0 的是友谊赛，不涉及游戏币。棋盘与轮到谁都在 State 里，
// 由各游戏的规则引擎解释；每次改动 Version 加一，并发的两步只有一步能成功。

const (
	GameRoomStatusWaiting   = "waiting"   // 等待对手加入
	GameRoomStatusPlaying   = "playing"   // 对局中
	GameRoomStatusFinished  = "finished"  // 已分出结果并结算
	GameRoomStatusCancelled = "cancelled" // 没有开局就关闭，押注已退回
)

// 对局结束或房间关闭的原因。
const (
	GameRoomEndWin     = "win"     // 按规则分出胜负
	GameRoomEndDraw    = "draw"    // 和棋
	GameRoomEndResign  = "resign"  // 一方认输
	GameRoomEndTimeout = "timeout" // 一方超时未走
	GameRoomEndCancel  = "cancel"  // 房主取消
	GameRoomEndExpired = "expired" // 等太久没人加入
)

var (
	ErrGameRoomNotFound = errors.New("game room not found")
	// ErrGameRoomActive 表示玩家在这个游戏里已有一个等待中或对局中的房间。
	ErrGameRoomActive = errors.New("an active room of this game exists")
	// ErrGameRoomChanged 表示房间在读出之后被另一个请求改过(例如对手已经走了一步、别人先加入了)，这次操作没有生效。
	ErrGameRoomChanged = errors.New("game room changed by another request")
)

var gameRoomActiveStatuses = []string{GameRoomStatusWaiting, GameRoomStatusPlaying}

// GameRoom 是一个对战房间。座位 0 是房主、1 是对手。
type GameRoom struct {
	Id     int    `json:"id"`
	Game   string `json:"game" gorm:"type:varchar(32);index:idx_game_room_lobby,priority:1"`
	Status string `json:"status" gorm:"type:varchar(16);index:idx_game_room_lobby,priority:2;index:idx_game_room_due,priority:1"`
	// Stake 是每人押注的游戏币，0 是友谊赛。
	Stake int `json:"stake" gorm:"type:bigint;not null;default:0"`
	// Private 的房间不在大厅列出，只能凭邀请码加入。
	Private bool   `json:"private"`
	Code    string `json:"-" gorm:"type:varchar(16);index"`
	HostId  int    `json:"host_id" gorm:"index"`
	GuestId int    `json:"guest_id" gorm:"index"`
	// 两位玩家的 IP(IPv6 已归并到前缀)。同一个 IP 视为同一个人，不能互相押注对战。
	HostIp  string `json:"-" gorm:"type:varchar(64)"`
	GuestIp string `json:"-" gorm:"type:varchar(64)"`
	State   string `json:"-" gorm:"type:text"`
	Version int    `json:"version" gorm:"not null;default:0"`
	// ToMove 是轮到哪个座位走，-1 表示双方同时出招(例如石头剪刀布)。
	ToMove int `json:"to_move" gorm:"not null;default:0"`
	// Deadline 是这一步的截止时间，过了还没走就判负；等待中的房间为 0。
	Deadline int64 `json:"deadline" gorm:"bigint;index:idx_game_room_due,priority:2"`
	// WinnerId 是赢家的用户 ID，平局与取消为 0。
	WinnerId  int    `json:"winner_id"`
	EndReason string `json:"end_reason" gorm:"type:varchar(16)"`
	// RakePercent 与 MoveSeconds 是开房时的抽成比例与每步限时：之后管理员改了配置，已经开的房间仍按开房时的规则。
	RakePercent int `json:"rake_percent" gorm:"not null;default:0"`
	MoveSeconds int `json:"move_seconds" gorm:"not null;default:0"`
	// Rake 是平台从奖池抽走的游戏币。
	Rake       int   `json:"rake" gorm:"type:bigint;not null;default:0"`
	CreatedAt  int64 `json:"created_at" gorm:"bigint;index"`
	StartedAt  int64 `json:"started_at" gorm:"bigint"`
	FinishedAt int64 `json:"finished_at" gorm:"bigint"`
}

// GameRoomStart 是对手加入时的开局状态。
type GameRoomStart struct {
	State    string
	ToMove   int
	Deadline int64
}

// SeatOf 返回用户在房间里的座位，不在房间里时返回 -1。
func (r *GameRoom) SeatOf(userId int) int {
	switch {
	case userId == r.HostId:
		return 0
	case r.GuestId != 0 && userId == r.GuestId:
		return 1
	}
	return -1
}

// PlayerAt 返回座位上的用户 ID。
func (r *GameRoom) PlayerAt(seat int) int {
	if seat == 0 {
		return r.HostId
	}
	return r.GuestId
}

// countActiveGameRooms 统计用户在这个游戏里等待中与对局中的房间，调用方要先锁住用户的账户。
func countActiveGameRooms(tx *gorm.DB, userId int, game string) (int64, error) {
	var count int64
	err := tx.Model(&GameRoom{}).
		Where("game = ? AND status IN ? AND (host_id = ? OR guest_id = ?)", game, gameRoomActiveStatuses, userId, userId).
		Count(&count).Error
	return count, err
}

// lockGameCoinAccountTx 锁住用户的游戏币账户，同一用户的几个请求依次进行；账户还不存在时什么都不锁，
// 这时余额为 0，后面的扣币自然会失败。
func lockGameCoinAccountTx(tx *gorm.DB, userId int) error {
	var balances []int
	return lockForUpdate(tx).Model(&GameCoinAccount{}).Where("user_id = ?", userId).Pluck("balance", &balances).Error
}

// CreateGameRoom 开一个等待对手的房间并押下房主的押注。room 由调用方填好游戏、押注、是否私密、邀请码与房主，
// 状态与时间在这里生成。玩家在这个游戏里已有等待中或对局中的房间时返回 ErrGameRoomActive，余额不够时返回
// ErrGameCoinInsufficient，都什么都不会改变。
func CreateGameRoom(room *GameRoom) (int, error) {
	if room.Stake < 0 || room.Stake >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	room.Status = GameRoomStatusWaiting
	room.CreatedAt = common.GetTimestamp()
	room.Version = 0
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockGameCoinAccountTx(tx, room.HostId); err != nil {
			return err
		}
		active, err := countActiveGameRooms(tx, room.HostId, room.Game)
		if err != nil {
			return err
		}
		if active > 0 {
			return ErrGameRoomActive
		}
		if err := tx.Create(room).Error; err != nil {
			return err
		}
		if room.Stake == 0 {
			balance, err = readGameCoinBalanceTx(tx, room.HostId)
			return err
		}
		balance, err = debitGameRoomStakeTx(tx, room, room.HostId)
		return err
	})
	if err != nil {
		return 0, err
	}
	return balance, nil
}

// JoinGameRoom 以对手身份加入等待中的房间、押下押注并开局。房间已被别人加入或取消时返回 ErrGameRoomChanged；
// 对手在这个游戏里已有别的房间时返回 ErrGameRoomActive；余额不够时返回 ErrGameCoinInsufficient。
func JoinGameRoom(room *GameRoom, guestId int, guestIp string, start GameRoomStart) (int, error) {
	now := common.GetTimestamp()
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockGameCoinAccountTx(tx, guestId); err != nil {
			return err
		}
		active, err := countActiveGameRooms(tx, guestId, room.Game)
		if err != nil {
			return err
		}
		if active > 0 {
			return ErrGameRoomActive
		}
		update := tx.Model(&GameRoom{}).
			Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusWaiting, room.Version).
			Updates(map[string]interface{}{
				"guest_id":   guestId,
				"guest_ip":   guestIp,
				"status":     GameRoomStatusPlaying,
				"state":      start.State,
				"to_move":    start.ToMove,
				"deadline":   start.Deadline,
				"started_at": now,
				"version":    room.Version + 1,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrGameRoomChanged
		}
		if room.Stake == 0 {
			balance, err = readGameCoinBalanceTx(tx, guestId)
			return err
		}
		balance, err = debitGameRoomStakeTx(tx, room, guestId)
		return err
	})
	if err != nil {
		return 0, err
	}
	room.GuestId, room.GuestIp = guestId, guestIp
	room.Status, room.State, room.ToMove, room.Deadline = GameRoomStatusPlaying, start.State, start.ToMove, start.Deadline
	room.StartedAt, room.Version = now, room.Version+1
	return balance, nil
}

// UpdateGameRoomState 保存对局中的一步。只有 room.Version 仍是库里的版本时才会成功，否则返回 ErrGameRoomChanged；
// 成功后 room 随之更新。
func UpdateGameRoomState(room *GameRoom, state string, toMove int, deadline int64) error {
	result := DB.Model(&GameRoom{}).
		Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusPlaying, room.Version).
		Updates(map[string]interface{}{
			"state":    state,
			"to_move":  toMove,
			"deadline": deadline,
			"version":  room.Version + 1,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGameRoomChanged
	}
	room.State, room.ToMove, room.Deadline, room.Version = state, toMove, deadline, room.Version+1
	return nil
}

// FinishGameRoom 结束对局并结算押注：winnerId 为 0 是平局，两人各自退回押注；否则赢家拿走两份押注减去抽成，抽成是奖池的
// room.RakePercent%(向下取整)。只有 room.Version 仍是库里的版本时才会成功，否则返回 ErrGameRoomChanged：同一局只结算一次。
func FinishGameRoom(room *GameRoom, winnerId int, reason string, state string, day string) error {
	if winnerId != 0 && room.SeatOf(winnerId) < 0 {
		return errors.New("winner is not a player of the room")
	}
	now := common.GetTimestamp()
	pot := 2 * room.Stake
	rake := 0
	if winnerId != 0 {
		rake = pot * room.RakePercent / 100
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&GameRoom{}).
			Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusPlaying, room.Version).
			Updates(map[string]interface{}{
				"status":      GameRoomStatusFinished,
				"winner_id":   winnerId,
				"end_reason":  reason,
				"state":       state,
				"rake":        rake,
				"finished_at": now,
				"version":     room.Version + 1,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrGameRoomChanged
		}
		// 两位玩家按用户 ID 从小到大记账，两局同时结算时加锁顺序一致。
		players := []int{room.HostId, room.GuestId}
		sort.Ints(players)
		for _, userId := range players {
			net, credit, event := 0, 0, "draw"
			switch {
			case winnerId == 0:
				credit = room.Stake
			case userId == winnerId:
				net, credit, event = room.Stake-rake, pot-rake, "win"
			default:
				net = -room.Stake
			}
			if room.Stake > 0 {
				if err := settleGameRoomPlayerTx(tx, room, userId, net, credit, event, now); err != nil {
					return err
				}
			}
			if _, _, err := recordGameDayResult(tx, userId, room.Game, day, net); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	room.Status, room.WinnerId, room.EndReason, room.State, room.Rake = GameRoomStatusFinished, winnerId, reason, state, rake
	room.FinishedAt, room.Version = now, room.Version+1
	return nil
}

// CancelGameRoom 关闭还没开局的房间并退回房主的押注。房间已开局或已关闭时返回 ErrGameRoomChanged。
func CancelGameRoom(room *GameRoom, reason string) error {
	now := common.GetTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&GameRoom{}).
			Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusWaiting, room.Version).
			Updates(map[string]interface{}{
				"status":      GameRoomStatusCancelled,
				"end_reason":  reason,
				"finished_at": now,
				"version":     room.Version + 1,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrGameRoomChanged
		}
		if room.Stake == 0 {
			return nil
		}
		// 没有开局，这笔押注不算下注过。
		balance, err := creditGameCoinsTx(tx, room.HostId, room.Stake, map[string]interface{}{
			"total_wagered": gorm.Expr("total_wagered - ?", room.Stake),
		})
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    room.HostId,
			Type:      GameCoinLogTypeGamePvp,
			Amount:    room.Stake,
			Balance:   balance,
			RefId:     gameRoomRef(room.Id),
			Remark:    gameRoomRemark(room, "refund"),
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return err
	}
	room.Status, room.EndReason, room.FinishedAt, room.Version = GameRoomStatusCancelled, reason, now, room.Version+1
	return nil
}

// debitGameRoomStakeTx 扣下一位玩家的押注并记流水。
func debitGameRoomStakeTx(tx *gorm.DB, room *GameRoom, userId int) (int, error) {
	balance, err := debitGameCoinsTx(tx, userId, room.Stake, map[string]interface{}{
		"total_wagered": gorm.Expr("total_wagered + ?", room.Stake),
	})
	if err != nil {
		return 0, err
	}
	return balance, tx.Create(&GameCoinLog{
		UserId:    userId,
		Type:      GameCoinLogTypeGamePvp,
		Amount:    -room.Stake,
		Balance:   balance,
		RefId:     gameRoomRef(room.Id),
		Remark:    gameRoomRemark(room, "stake"),
		CreatedAt: common.GetTimestamp(),
	}).Error
}

// settleGameRoomPlayerTx 给一位玩家结算押注：净输赢计入累计输赢，有退回或赢得的游戏币时入账并记流水，event 见 gameRoomRemark。
func settleGameRoomPlayerTx(tx *gorm.DB, room *GameRoom, userId int, net int, credit int, event string, now int64) error {
	fields := map[string]interface{}{"total_wager_net": gorm.Expr("total_wager_net + ?", net)}
	if credit == 0 {
		fields["updated_at"] = now
		return tx.Model(&GameCoinAccount{}).Where("user_id = ?", userId).Updates(fields).Error
	}
	balance, err := creditGameCoinsTx(tx, userId, credit, fields)
	if err != nil {
		return err
	}
	return tx.Create(&GameCoinLog{
		UserId:    userId,
		Type:      GameCoinLogTypeGamePvp,
		Amount:    credit,
		Balance:   balance,
		RefId:     gameRoomRef(room.Id),
		Remark:    gameRoomRemark(room, event),
		CreatedAt: now,
	}).Error
}

// gameRoomRemark 是对战流水的备注：游戏标识与事件(stake 押注、win 获胜、draw 平局退回、refund 房间关闭退回)，
// 由前端翻译成玩家的语言。
func gameRoomRemark(room *GameRoom, event string) string {
	return room.Game + ":" + event
}

// gameRoomRef 是对战流水的关联编号，与对局编号区分开。
func gameRoomRef(roomId int) string {
	return "room:" + strconv.Itoa(roomId)
}

// GetGameRoom 按 ID 读取房间，不存在时返回 ErrGameRoomNotFound。
func GetGameRoom(id int) (*GameRoom, error) {
	var rooms []GameRoom
	if err := DB.Where("id = ?", id).Limit(1).Find(&rooms).Error; err != nil {
		return nil, err
	}
	if len(rooms) == 0 {
		return nil, ErrGameRoomNotFound
	}
	return &rooms[0], nil
}

// GetWaitingGameRoomByCode 按邀请码找等待中的房间，没有时返回 ErrGameRoomNotFound。
func GetWaitingGameRoomByCode(code string) (*GameRoom, error) {
	var rooms []GameRoom
	if err := DB.Where("code = ? AND status = ?", code, GameRoomStatusWaiting).Limit(1).Find(&rooms).Error; err != nil {
		return nil, err
	}
	if len(rooms) == 0 {
		return nil, ErrGameRoomNotFound
	}
	return &rooms[0], nil
}

// ListWaitingGameRooms 按开房先后返回某个游戏在大厅里等待对手的公开房间。
func ListWaitingGameRooms(game string, limit int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("game = ? AND status = ? AND private = ?", game, GameRoomStatusWaiting, false).
		Order("id asc").Limit(limit).Find(&rooms).Error
	return rooms, err
}

// FindGameRoomMatch 为快速匹配找一个押注相同、不是自己开的公开房间，按开房先后取最早的；没有时返回 nil。
// excludeIp 不为空时跳过同一 IP 开的房间(押注对局不能和自己的其他账号对战)。
func FindGameRoomMatch(game string, stake int, userId int, excludeIp string) (*GameRoom, error) {
	query := DB.Where("game = ? AND status = ? AND private = ? AND stake = ? AND host_id <> ?", game, GameRoomStatusWaiting, false, stake, userId)
	if excludeIp != "" {
		query = query.Where("host_ip <> ?", excludeIp)
	}
	var rooms []GameRoom
	if err := query.Order("id asc").Limit(1).Find(&rooms).Error; err != nil || len(rooms) == 0 {
		return nil, err
	}
	return &rooms[0], nil
}

// GetUserActiveGameRooms 返回用户所有等待中与对局中的房间。
func GetUserActiveGameRooms(userId int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("status IN ? AND (host_id = ? OR guest_id = ?)", gameRoomActiveStatuses, userId, userId).
		Order("id desc").Find(&rooms).Error
	return rooms, err
}

// ListUserFinishedGameRooms 按结束先后倒序返回用户在某个游戏里最近结束的对局。
func ListUserFinishedGameRooms(userId int, game string, limit int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("game = ? AND status = ? AND (host_id = ? OR guest_id = ?)", game, GameRoomStatusFinished, userId, userId).
		Order("id desc").Limit(limit).Find(&rooms).Error
	return rooms, err
}

// CountWaitingGameRooms 按游戏统计大厅里等待对手的公开房间数。
func CountWaitingGameRooms() (map[string]int, error) {
	var rows []struct {
		Game  string
		Total int
	}
	err := DB.Model(&GameRoom{}).Select("game, COUNT(*) AS total").
		Where("status = ? AND private = ?", GameRoomStatusWaiting, false).Group("game").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Game] = row.Total
	}
	return counts, nil
}

// ListDueGameRooms 返回需要系统处理的房间：对局中已超过这一步截止时间的，以及在 waitingBefore 之前开、还没等到对手的。
func ListDueGameRooms(now int64, waitingBefore int64, limit int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("(status = ? AND deadline < ?) OR (status = ? AND created_at < ?)",
		GameRoomStatusPlaying, now, GameRoomStatusWaiting, waitingBefore).
		Order("id asc").Limit(limit).Find(&rooms).Error
	return rooms, err
}

// HasDueGameRooms 表示有需要系统处理的房间，见 ListDueGameRooms。
func HasDueGameRooms(now int64, waitingBefore int64) bool {
	rooms, err := ListDueGameRooms(now, waitingBefore, 1)
	return err == nil && len(rooms) > 0
}
