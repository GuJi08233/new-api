package model

import (
	"errors"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 联机房间。房主开房时坐 0 号座位并押下押注，其他玩家依次入座、各押同样多，坐满即开局。分出结果后按规则引擎给出的每个座位的
// 输赢结算：双人对局按赢家所得奖池抽成，多人对局按赢得的净额抽成，平局各自退回；开局前离开的退回押注，房主离开则房间关闭、全部退回。押注为 0 的
// 是友谊赛，不涉及游戏币。棋盘与轮到谁都在 State 里，由各游戏的规则引擎解释；每次改动 Version 加一，并发的两步只有一步能成功。

const (
	GameRoomStatusWaiting   = "waiting"   // 等待玩家入座
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
	GameRoomEndCancel  = "cancel"  // 房主关闭
	GameRoomEndExpired = "expired" // 等太久没坐满
)

// GameRoomMaxSeats 是一个房间最多的座位数。
const GameRoomMaxSeats = 8

var (
	ErrGameRoomNotFound = errors.New("game room not found")
	// ErrGameRoomActive 表示玩家在这个游戏里已有一个等待中或对局中的房间。
	ErrGameRoomActive = errors.New("an active room of this game exists")
	// ErrGameRoomChanged 表示房间在读出之后被另一个请求改过(例如对手已经走了一步、房间已经开局)，这次操作没有生效。
	ErrGameRoomChanged = errors.New("game room changed by another request")
	// ErrGameRoomFull 表示房间的座位已经坐满。
	ErrGameRoomFull = errors.New("game room is full")
	// ErrGameRoomSameIp 表示押注房间里已经坐着同一 IP 下的账号。
	ErrGameRoomSameIp = errors.New("a player from the same ip is already seated")
)

var gameRoomActiveStatuses = []string{GameRoomStatusWaiting, GameRoomStatusPlaying}

// GameRoom 是一个联机房间。座位 0 是房主。
type GameRoom struct {
	Id     int    `json:"id"`
	Game   string `json:"game" gorm:"type:varchar(32);index:idx_game_room_lobby,priority:1"`
	Status string `json:"status" gorm:"type:varchar(16);index:idx_game_room_lobby,priority:2;index:idx_game_room_due,priority:1"`
	// Stake 是底注：两人对弈时就是每人的押注，多人游戏按各自的规则以它为单位计输赢。0 是友谊赛。
	Stake GameCoins `json:"stake" gorm:"type:bigint;not null;default:0"`
	// Escrow 是每位玩家入座时押下的游戏币：底注乘以规则里一个人最多输的倍数，结算时没输掉的部分退回。
	Escrow GameCoins `json:"escrow" gorm:"type:bigint;not null;default:0"`
	// Seats 是开局人数，坐满即开局；Players 是已经入座的人数。
	Seats   int `json:"seats" gorm:"not null;default:2"`
	Players int `json:"players" gorm:"not null;default:0"`
	// Private 的房间不在大厅列出，只能凭邀请码加入；PasswordHash 不为空的房间在大厅里显示为加锁，凭密码加入。
	Private      bool   `json:"private"`
	Code         string `json:"-" gorm:"type:varchar(16);index"`
	PasswordHash string `json:"-" gorm:"type:varchar(128)"`
	HostId       int    `json:"host_id" gorm:"index"`
	State        string `json:"-" gorm:"type:text"`
	Version      int    `json:"version" gorm:"not null;default:0"`
	// ToMove 是轮到哪个座位走，-1 表示同时出招(例如石头剪刀布)或对局已结束。
	ToMove int `json:"to_move" gorm:"not null;default:0"`
	// Deadline 是这一步的截止时间，过了还没走就按规则处理(两人对弈判负，多人游戏由 AI 替他走并转为托管)；等待中与已结束的房间为 0。
	Deadline int64 `json:"deadline" gorm:"bigint;index:idx_game_room_due,priority:2"`
	// AutoSeats 是托管中的座位(按位记，第 n 位是座位 n)：多人游戏里超时或自己选择托管的座位，轮到时由 AI 替他走。
	AutoSeats int `json:"auto_seats" gorm:"not null;default:0"`
	// WinnerId 是赢得最多的玩家，平局与取消为 0。
	WinnerId  int    `json:"winner_id"`
	EndReason string `json:"end_reason" gorm:"type:varchar(16)"`
	// RakePercent 与 MoveSeconds 是开房时的抽成比例与每步限时：之后管理员改了配置，已经开的房间仍按开房时的规则。
	RakePercent int `json:"rake_percent" gorm:"not null;default:0"`
	MoveSeconds int `json:"move_seconds" gorm:"not null;default:0"`
	// Rake 是平台从赢家那里抽走的游戏币合计。
	Rake       GameCoins `json:"rake" gorm:"type:bigint;not null;default:0"`
	CreatedAt  int64     `json:"created_at" gorm:"bigint;index"`
	StartedAt  int64     `json:"started_at" gorm:"bigint"`
	FinishedAt int64     `json:"finished_at" gorm:"bigint"`
}

// Locked 表示房间设了密码。
func (r *GameRoom) Locked() bool {
	return r.PasswordHash != ""
}

// GameRoomSeat 是房间里的一个座位。开局前离开的玩家座位被删掉；开局后座位一直留着，结算时记下这位玩家的净输赢。
type GameRoomSeat struct {
	Id     int    `json:"-"`
	RoomId int    `json:"room_id" gorm:"uniqueIndex:idx_game_room_seat,priority:1;uniqueIndex:idx_game_room_seat_user,priority:1"`
	Seat   int    `json:"seat" gorm:"uniqueIndex:idx_game_room_seat,priority:2"`
	UserId int    `json:"user_id" gorm:"uniqueIndex:idx_game_room_seat_user,priority:2;index"`
	Ip     string `json:"-" gorm:"type:varchar(64)"`
	// Net 是结算之后这位玩家的净输赢(已扣抽成)，结算之前为 0。
	Net GameCoins `json:"net" gorm:"type:bigint;not null;default:0"`
	// Result 保留规则上的胜负，友谊赛、零收益队友以及抽成后亏损的赢家也有明确结果。
	Result    string `json:"result" gorm:"type:varchar(8)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

// GameRoomStart 是坐满开局时的开局状态。
type GameRoomStart struct {
	State    string
	ToMove   int
	Deadline int64
}

// GameRoomResult 是规则引擎给出的结算结果。Nets 按座位排列，金额为抽成前的净输赢；WinnerSeat 为获胜座位，
// 平局是 -1。Winners 记录团队获胜的全部座位，为空时沿用 WinnerSeat 单人获胜；单独保留胜者让不涉及游戏币的友谊赛也能记录胜负。
type GameRoomResult struct {
	Nets       []GameCoins
	WinnerSeat int
	Winners    []int
}

// gameRoomActiveOf 是用户坐着的房间编号的子查询。
func gameRoomActiveOf(tx *gorm.DB, userId int) *gorm.DB {
	return tx.Model(&GameRoomSeat{}).Select("room_id").Where("user_id = ?", userId)
}

// countActiveGameRooms 统计用户在这个游戏里等待中与对局中的房间，调用方要先锁住用户的账户。
func countActiveGameRooms(tx *gorm.DB, userId int, game string) (int64, error) {
	var count int64
	err := tx.Model(&GameRoom{}).
		Where("game = ? AND status IN ? AND id IN (?)", game, gameRoomActiveStatuses, gameRoomActiveOf(tx, userId)).
		Count(&count).Error
	return count, err
}

// lockGameRoomTx 锁住房间这一行并读出最新的内容。各操作都先锁房间再锁账户，与开局、结算的顺序一致，不会互相等待成环。
func lockGameRoomTx(tx *gorm.DB, id int) (*GameRoom, error) {
	var rooms []GameRoom
	if err := lockForUpdate(tx).Where("id = ?", id).Limit(1).Find(&rooms).Error; err != nil {
		return nil, err
	}
	if len(rooms) == 0 {
		return nil, ErrGameRoomNotFound
	}
	return &rooms[0], nil
}

func loadGameRoomSeatsTx(tx *gorm.DB, roomId int) ([]GameRoomSeat, error) {
	var seats []GameRoomSeat
	err := tx.Where("room_id = ?", roomId).Order("seat").Find(&seats).Error
	return seats, err
}

// CreateGameRoom 开一个等待玩家入座的房间，房主坐 0 号座位并押下押注。room 由调用方填好游戏、底注、押注、座位数、是否私密、
// 邀请码、密码与房主，状态与时间在这里生成。玩家在这个游戏里已有等待中或对局中的房间时返回 ErrGameRoomActive，余额不够时返回
// ErrGameCoinInsufficient，都什么都不会改变。
func CreateGameRoom(room *GameRoom, hostIp string) (GameCoins, error) {
	if room.Seats < 2 || room.Seats > GameRoomMaxSeats {
		return 0, errors.New("invalid game room seats")
	}
	// 奖池也必须能作为一笔合法入账，先做除法再比较，避免乘法溢出。
	if room.Stake < 0 || room.Escrow < room.Stake || room.Escrow > GameCoins((common.MaxQuota-1)/room.Seats) || (room.Stake == 0) != (room.Escrow == 0) {
		return 0, ErrGameCoinAmountInvalid
	}
	if room.RakePercent < 0 || room.RakePercent > 100 {
		return 0, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	room.Status, room.CreatedAt, room.Version, room.Players = GameRoomStatusWaiting, now, 0, 1
	var balance GameCoins
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockGameCoinAccountTx(tx, room.HostId); err != nil {
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
		if err := tx.Create(&GameRoomSeat{RoomId: room.Id, Seat: 0, UserId: room.HostId, Ip: hostIp, CreatedAt: now}).Error; err != nil {
			return err
		}
		balance, err = escrowGameRoomTx(tx, room, room.HostId)
		return err
	})
	if err != nil {
		return 0, err
	}
	return balance, nil
}

// JoinGameRoom 让用户坐进等待中的房间、押下押注；这一位坐满了房间就开局，start 按全部座位给出开局状态。房间已开局、关闭时返回
// ErrGameRoomChanged，坐满时返回 ErrGameRoomFull；押注房间里已有同一 IP 的账号时返回 ErrGameRoomSameIp；用户在这个游戏里
// 已有别的房间(或已坐在这个房间)时返回 ErrGameRoomActive；余额不够时返回 ErrGameCoinInsufficient。成功后 room 更新为最新状态。
func JoinGameRoom(room *GameRoom, userId int, ip string, start func(seats []GameRoomSeat) (GameRoomStart, error)) (GameCoins, error) {
	now := common.GetTimestamp()
	var balance GameCoins
	err := DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockGameRoomTx(tx, room.Id)
		if err != nil {
			return err
		}
		if current.Status != GameRoomStatusWaiting {
			return ErrGameRoomChanged
		}
		if current.Players >= current.Seats {
			return ErrGameRoomFull
		}
		// MySQL 默认的可重复读事务要在锁账户后才进行普通读取，避免建立旧快照而漏掉刚加入的另一间房。
		if _, err := lockGameCoinAccountTx(tx, userId); err != nil {
			return err
		}
		seats, err := loadGameRoomSeatsTx(tx, current.Id)
		if err != nil {
			return err
		}
		taken := make(map[int]bool, len(seats))
		for _, seat := range seats {
			if seat.UserId == userId {
				return ErrGameRoomActive
			}
			if current.Stake > 0 && seat.Ip == ip {
				return ErrGameRoomSameIp
			}
			taken[seat.Seat] = true
		}
		active, err := countActiveGameRooms(tx, userId, current.Game)
		if err != nil {
			return err
		}
		if active > 0 {
			return ErrGameRoomActive
		}
		free := 0
		for taken[free] {
			free++
		}
		seat := GameRoomSeat{RoomId: current.Id, Seat: free, UserId: userId, Ip: ip, CreatedAt: now}
		if err := tx.Create(&seat).Error; err != nil {
			return err
		}
		if balance, err = escrowGameRoomTx(tx, current, userId); err != nil {
			return err
		}
		updates := map[string]interface{}{"players": current.Players + 1, "version": current.Version + 1}
		if current.Players+1 == current.Seats {
			if start == nil {
				return errors.New("game room opening is required")
			}
			seats = append(seats, seat)
			sort.Slice(seats, func(i, j int) bool { return seats[i].Seat < seats[j].Seat })
			opening, err := start(seats)
			if err != nil {
				return err
			}
			updates["status"] = GameRoomStatusPlaying
			updates["state"] = opening.State
			updates["to_move"] = opening.ToMove
			updates["deadline"] = opening.Deadline
			updates["started_at"] = now
		}
		return casGameRoomTx(tx, current, GameRoomStatusWaiting, updates)
	})
	if err != nil {
		return 0, err
	}
	return balance, reloadGameRoom(room)
}

// LeaveGameRoom 让用户离开还没开局的房间、退回押注；房主离开时房间关闭，所有人的押注退回。房间已开局或关闭时返回
// ErrGameRoomChanged，用户不在房间里时返回 ErrGameRoomNotFound。成功后 room 更新为最新状态。
func LeaveGameRoom(room *GameRoom, userId int) error {
	now := common.GetTimestamp()
	err := DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockGameRoomTx(tx, room.Id)
		if err != nil {
			return err
		}
		if current.Status != GameRoomStatusWaiting {
			return ErrGameRoomChanged
		}
		if current.HostId == userId {
			return cancelGameRoomTx(tx, current, GameRoomEndCancel, now)
		}
		seats, err := loadGameRoomSeatsTx(tx, current.Id)
		if err != nil {
			return err
		}
		index := -1
		for i, seat := range seats {
			if seat.UserId == userId {
				index = i
			}
		}
		if index < 0 {
			return ErrGameRoomNotFound
		}
		if err := tx.Where("id = ?", seats[index].Id).Delete(&GameRoomSeat{}).Error; err != nil {
			return err
		}
		if err := refundGameRoomEscrowTx(tx, current, userId, now); err != nil {
			return err
		}
		return casGameRoomTx(tx, current, GameRoomStatusWaiting, map[string]interface{}{
			"players": current.Players - 1,
			"version": current.Version + 1,
		})
	})
	if err != nil {
		return err
	}
	return reloadGameRoom(room)
}

// UpdateGameRoomState 保存对局中的一步与之后托管中的座位。只有 room.Version 仍是库里的版本时才会成功，否则返回
// ErrGameRoomChanged；成功后 room 随之更新。
func UpdateGameRoomState(room *GameRoom, state string, toMove int, deadline int64, autoSeats int) error {
	result := DB.Model(&GameRoom{}).
		Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusPlaying, room.Version).
		Updates(map[string]interface{}{
			"state":      state,
			"to_move":    toMove,
			"deadline":   deadline,
			"auto_seats": autoSeats,
			"version":    room.Version + 1,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGameRoomChanged
	}
	room.State, room.ToMove, room.Deadline, room.AutoSeats, room.Version = state, toMove, deadline, autoSeats, room.Version+1
	return nil
}

// FinishGameRoom 结束对局并结算：Nets 按座位顺序给出每位玩家的输赢(游戏币，抽成之前)，合计为 0，输的不超过押注。双人
// 保持原有的奖池抽成，多人按赢家净赢抽成(均向下取整)，每人拿回押注加上净输赢；平局各自退回押注、不抽成。只有
// room.Version 仍是库里的版本时才会成功，否则返回 ErrGameRoomChanged：同一局只结算一次。
func FinishGameRoom(room *GameRoom, result GameRoomResult, reason string, state string, day string) error {
	now := common.GetTimestamp()
	rake, winnerId := GameCoins(0), 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockGameRoomTx(tx, room.Id)
		if err != nil {
			return err
		}
		if current.Status != GameRoomStatusPlaying || current.Version != room.Version {
			return ErrGameRoomChanged
		}
		if current.Seats < 2 || current.Seats > GameRoomMaxSeats || len(result.Nets) != current.Seats || result.WinnerSeat < -1 || result.WinnerSeat >= current.Seats {
			return errors.New("game room result does not match its seats")
		}
		if current.Escrow < 0 || current.Escrow >= common.MaxQuota || current.RakePercent < 0 || current.RakePercent > 100 {
			return ErrGameCoinAmountInvalid
		}
		winners := make([]bool, current.Seats)
		winnerSeats := result.Winners
		if len(winnerSeats) == 0 && result.WinnerSeat >= 0 {
			winnerSeats = []int{result.WinnerSeat}
		}
		for _, seat := range winnerSeats {
			if seat < 0 || seat >= current.Seats || winners[seat] {
				return errors.New("game room winners do not match its seats")
			}
			winners[seat] = true
		}
		if (result.WinnerSeat == -1) != (len(winnerSeats) == 0) || (result.WinnerSeat >= 0 && !winners[result.WinnerSeat]) {
			return errors.New("game room winner does not match its team")
		}
		// 每项先限界，最多 8 项、每项最多 7 * MaxQuota，累加与后面的抽成都不会溢出 int64。
		total, best := GameCoins(0), GameCoins(0)
		for i, net := range result.Nets {
			if net < -current.Escrow || net > current.Escrow*GameCoins(current.Seats-1) {
				return errors.New("game room result exceeds the escrow")
			}
			if (net > 0 && !winners[i]) || (net < 0 && winners[i]) {
				return errors.New("game room winnings do not match its winners")
			}
			total += net
			best = max(best, net)
		}
		if total != 0 {
			return errors.New("game room result does not balance")
		}
		if best > 0 && (result.WinnerSeat < 0 || result.Nets[result.WinnerSeat] != best) {
			return errors.New("game room winner does not match its result")
		}
		if current.Escrow > 0 && best == 0 && result.WinnerSeat != -1 {
			return errors.New("a tied game room cannot have a winner")
		}
		seats, err := loadGameRoomSeatsTx(tx, current.Id)
		if err != nil {
			return err
		}
		if len(seats) != current.Seats {
			return errors.New("game room seats are incomplete")
		}
		settled := make([]GameCoins, len(seats))
		for i, seat := range seats {
			if seat.Seat != i {
				return errors.New("game room seats are incomplete")
			}
			cut := GameCoins(0)
			if result.Nets[i] > 0 {
				base := result.Nets[i]
				if current.Seats == 2 {
					base += current.Escrow
				}
				cut = base * GameCoins(current.RakePercent) / 100
			}
			rake += cut
			settled[i] = result.Nets[i] - cut
			if i == result.WinnerSeat {
				winnerId = seat.UserId
			}
		}
		update := tx.Model(&GameRoom{}).
			Where("id = ? AND status = ? AND version = ?", room.Id, GameRoomStatusPlaying, room.Version).
			Updates(map[string]interface{}{
				"status":      GameRoomStatusFinished,
				"to_move":     -1,
				"deadline":    0,
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
		// 玩家按用户 ID 从小到大记账，几局同时结算时加锁顺序一致。
		order := make([]int, len(seats))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return seats[order[a]].UserId < seats[order[b]].UserId })
		for _, i := range order {
			seat := seats[i]
			outcome := "draw"
			if len(winnerSeats) > 0 {
				outcome = "loss"
				if winners[i] {
					outcome = "win"
				}
			}
			if err := tx.Model(&GameRoomSeat{}).Where("id = ?", seat.Id).Updates(map[string]interface{}{"net": settled[i], "result": outcome}).Error; err != nil {
				return err
			}
			if current.Escrow > 0 {
				event := "settle"
				switch {
				case winners[i]:
					event = "win"
				case len(winnerSeats) == 0:
					event = "draw"
				}
				if err := settleGameRoomPlayerTx(tx, current, seat.UserId, settled[i], current.Escrow+settled[i], event, now); err != nil {
					return err
				}
			}
			if _, _, err := recordGameDayResult(tx, seat.UserId, current.Game, day, settled[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	room.Status, room.EndReason, room.State, room.WinnerId, room.Rake = GameRoomStatusFinished, reason, state, winnerId, rake
	room.ToMove, room.Deadline, room.FinishedAt, room.Version = -1, 0, now, room.Version+1
	return nil
}

// CancelGameRoom 关闭还没开局的房间并退回所有人的押注。房间已开局或已关闭时返回 ErrGameRoomChanged。成功后 room 更新为
// 最新状态。
func CancelGameRoom(room *GameRoom, reason string) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		current, err := lockGameRoomTx(tx, room.Id)
		if err != nil {
			return err
		}
		if current.Status != GameRoomStatusWaiting || current.Version != room.Version {
			return ErrGameRoomChanged
		}
		return cancelGameRoomTx(tx, current, reason, common.GetTimestamp())
	})
	if err != nil {
		return err
	}
	return reloadGameRoom(room)
}

// cancelGameRoomTx 在已经锁住的等待中房间上关闭房间并退回所有入座玩家的押注。
func cancelGameRoomTx(tx *gorm.DB, room *GameRoom, reason string, now int64) error {
	if err := casGameRoomTx(tx, room, GameRoomStatusWaiting, map[string]interface{}{
		"status":      GameRoomStatusCancelled,
		"end_reason":  reason,
		"finished_at": now,
		"version":     room.Version + 1,
	}); err != nil {
		return err
	}
	seats, err := loadGameRoomSeatsTx(tx, room.Id)
	if err != nil {
		return err
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].UserId < seats[j].UserId })
	for _, seat := range seats {
		if err := refundGameRoomEscrowTx(tx, room, seat.UserId, now); err != nil {
			return err
		}
	}
	return nil
}

// casGameRoomTx 只在房间仍是 status、版本仍是 room.Version 时写入 updates，否则返回 ErrGameRoomChanged。
func casGameRoomTx(tx *gorm.DB, room *GameRoom, status string, updates map[string]interface{}) error {
	result := tx.Model(&GameRoom{}).Where("id = ? AND status = ? AND version = ?", room.Id, status, room.Version).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGameRoomChanged
	}
	return nil
}

// reloadGameRoom 把 room 换成库里最新的样子。
func reloadGameRoom(room *GameRoom) error {
	latest, err := GetGameRoom(room.Id)
	if err != nil {
		return err
	}
	*room = *latest
	return nil
}

// escrowGameRoomTx 扣下一位入座玩家的押注并记流水，返回扣完的余额；友谊赛不扣，只读出余额。
func escrowGameRoomTx(tx *gorm.DB, room *GameRoom, userId int) (GameCoins, error) {
	if room.Escrow == 0 {
		return readGameCoinBalanceTx(tx, userId)
	}
	balance, err := debitGameCoinsTx(tx, userId, room.Escrow, map[string]interface{}{
		"total_wagered": gorm.Expr("total_wagered + ?", room.Escrow),
	})
	if err != nil {
		return 0, err
	}
	return balance, tx.Create(&GameCoinLog{
		UserId:    userId,
		Type:      GameCoinLogTypeGamePvp,
		Amount:    -room.Escrow,
		Balance:   balance,
		RefId:     gameRoomRef(room.Id),
		Remark:    gameRoomRemark(room, "stake"),
		CreatedAt: common.GetTimestamp(),
	}).Error
}

// refundGameRoomEscrowTx 退回一位没开局就离开(或房间关闭)的玩家的押注并记流水，这笔押注不算下注过。
func refundGameRoomEscrowTx(tx *gorm.DB, room *GameRoom, userId int, now int64) error {
	if room.Escrow == 0 {
		return nil
	}
	balance, err := creditGameCoinsTx(tx, userId, room.Escrow, map[string]interface{}{
		"total_wagered": gorm.Expr("total_wagered - ?", room.Escrow),
	})
	if err != nil {
		return err
	}
	return tx.Create(&GameCoinLog{
		UserId:    userId,
		Type:      GameCoinLogTypeGamePvp,
		Amount:    room.Escrow,
		Balance:   balance,
		RefId:     gameRoomRef(room.Id),
		Remark:    gameRoomRemark(room, "refund"),
		CreatedAt: now,
	}).Error
}

// settleGameRoomPlayerTx 给一位玩家结算：净输赢计入累计输赢，有拿回的游戏币(押注加上净输赢)时入账并记流水，event 见
// gameRoomRemark。
func settleGameRoomPlayerTx(tx *gorm.DB, room *GameRoom, userId int, net GameCoins, credit GameCoins, event string, now int64) error {
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

// gameRoomRemark 是对战流水的备注：游戏标识与事件(stake 押注、win 获胜、draw 平局退回、settle 结算退回没输掉的押注、
// refund 房间关闭或离开退回)，由前端翻译成玩家的语言。
func gameRoomRemark(room *GameRoom, event string) string {
	return room.Game + ":" + event
}

// gameRoomRef 是对战流水的关联编号，与对局编号区分开。
func gameRoomRef(roomId int) string {
	return "room:" + strconv.Itoa(roomId)
}

// gameRoomVersionBatch 是 GetGameRoomVersions 一条查询最多带的房间数，远低于各数据库对参数个数的限制。
const gameRoomVersionBatch = 500

// GetGameRoomVersions 返回这些房间当前的版本，键是房间 ID；不存在的房间不在结果里。
func GetGameRoomVersions(ids []int) (map[int]int, error) {
	versions := make(map[int]int, len(ids))
	for start := 0; start < len(ids); start += gameRoomVersionBatch {
		var rooms []GameRoom
		batch := ids[start:min(start+gameRoomVersionBatch, len(ids))]
		if err := DB.Select("id", "version").Where("id IN ?", batch).Find(&rooms).Error; err != nil {
			return nil, err
		}
		for _, room := range rooms {
			versions[room.Id] = room.Version
		}
	}
	return versions, nil
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

// GetGameRoomSeats 按房间读出座位，键是房间 ID，每个房间的座位按座位号排列。
func GetGameRoomSeats(roomIds []int) (map[int][]GameRoomSeat, error) {
	result := make(map[int][]GameRoomSeat, len(roomIds))
	for start := 0; start < len(roomIds); start += gameRoomVersionBatch {
		var seats []GameRoomSeat
		batch := roomIds[start:min(start+gameRoomVersionBatch, len(roomIds))]
		if err := DB.Where("room_id IN ?", batch).Order("room_id, seat").Find(&seats).Error; err != nil {
			return nil, err
		}
		for _, seat := range seats {
			result[seat.RoomId] = append(result[seat.RoomId], seat)
		}
	}
	return result, nil
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

// ListWaitingPublicGameRooms 按开房先后返回未过期且未坐满的公开等待房间(含密码房)。
// 在分页前筛选有效房间，避免较早的过期房间占满整个大厅列表。
func ListWaitingPublicGameRooms(games []string, waitingAfter int64, limit int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	if len(games) == 0 {
		return rooms, nil
	}
	err := DB.Where("game IN ? AND status = ? AND private = ?", games, GameRoomStatusWaiting, false).
		Where("players < seats AND created_at >= ?", waitingAfter).
		Order("id asc").Limit(limit).Find(&rooms).Error
	return rooms, err
}

// FindGameRoomMatch 为快速匹配找一个底注与座位数相同、还有空位、不要密码的公开房间，按开房先后取最早的；没有时返回 nil。
// 用户已经坐着的房间不算；excludeIp 不为空时跳过坐着同一 IP 账号的房间(押注对局不能和自己的其他账号对战)。
func FindGameRoomMatch(game string, stake GameCoins, seats int, userId int, excludeIp string) (*GameRoom, error) {
	seated := DB.Model(&GameRoomSeat{}).Select("room_id").Where("user_id = ?", userId)
	if excludeIp != "" {
		seated = DB.Model(&GameRoomSeat{}).Select("room_id").Where("user_id = ? OR ip = ?", userId, excludeIp)
	}
	var rooms []GameRoom
	err := DB.Where("game = ? AND status = ? AND private = ? AND (password_hash = ? OR password_hash IS NULL)", game, GameRoomStatusWaiting, false, "").
		Where("stake = ? AND seats = ? AND players < seats AND id NOT IN (?)", stake, seats, seated).
		Order("id asc").Limit(1).Find(&rooms).Error
	if err != nil || len(rooms) == 0 {
		return nil, err
	}
	return &rooms[0], nil
}

// GetUserActiveGameRooms 返回用户坐着的所有等待中与对局中的房间。
func GetUserActiveGameRooms(userId int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("status IN ? AND id IN (?)", gameRoomActiveStatuses, gameRoomActiveOf(DB, userId)).
		Order("id desc").Find(&rooms).Error
	return rooms, err
}

// ListUserFinishedGameRooms 按结束先后倒序返回用户在某个游戏里最近结束的对局。
func ListUserFinishedGameRooms(userId int, game string, limit int) ([]*GameRoom, error) {
	var rooms []*GameRoom
	err := DB.Where("game = ? AND status = ? AND id IN (?)", game, GameRoomStatusFinished, gameRoomActiveOf(DB, userId)).
		Order("id desc").Limit(limit).Find(&rooms).Error
	return rooms, err
}

// CountWaitingGameRooms 按游戏统计公开且等待玩家入座的房间数。
func CountWaitingGameRooms() (map[string]int, error) {
	var rows []struct {
		Game  string
		Rooms int
	}
	err := DB.Model(&GameRoom{}).Select("game, COUNT(*) AS rooms").
		Where("status = ? AND private = ?", GameRoomStatusWaiting, false).Group("game").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Game] = row.Rooms
	}
	return counts, nil
}

// ListDueGameRooms 返回需要系统处理的房间：对局中已超过这一步截止时间的，以及在 waitingBefore 之前开、还没坐满的。
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
