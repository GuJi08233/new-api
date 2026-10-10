package model

import (
	"errors"

	"gorm.io/gorm"
)

// migrateGameRoomSeats 把旧双人房间中的成员移入座位表。新字段 players 默认为 0，回填完成后至少为 1，作为幂等标记。
// 不改旧列、不重复扣币，未结束房间继续使用原有押注和状态；已结束房间的净额优先使用旧列，否则由原结算结果还原。
func migrateGameRoomSeats(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&GameRoom{}, "guest_id") {
		return nil
	}
	hasNets := db.Migrator().HasColumn(&GameRoom{}, "host_net") && db.Migrator().HasColumn(&GameRoom{}, "guest_net")
	after := 0
	for {
		var ids []int
		if err := db.Model(&GameRoom{}).Where("id > ? AND players = ?", after, 0).Order("id").Limit(gameRoomVersionBatch).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			err := db.Transaction(func(tx *gorm.DB) error {
				var legacy struct {
					GameRoom
					GuestId  int
					HostIp   string
					GuestIp  string
					HostNet  int
					GuestNet int
				}
				if err := lockForUpdate(tx).Table("game_rooms").Where("id = ?", id).Take(&legacy).Error; err != nil {
					return err
				}
				if legacy.Players != 0 {
					return nil
				}
				if legacy.HostId <= 0 || legacy.GuestId == legacy.HostId || (legacy.Status == GameRoomStatusPlaying && legacy.GuestId <= 0) {
					return errors.New("legacy game room has invalid players")
				}
				if !hasNets && legacy.Status == GameRoomStatusFinished && legacy.WinnerId != 0 {
					legacy.HostNet, legacy.GuestNet = -legacy.Stake, -legacy.Stake
					if legacy.WinnerId == legacy.HostId {
						legacy.HostNet = legacy.Stake - legacy.Rake
					} else if legacy.WinnerId == legacy.GuestId {
						legacy.GuestNet = legacy.Stake - legacy.Rake
					} else {
						return errors.New("legacy game room has invalid winner")
					}
				}
				seats := []GameRoomSeat{{RoomId: id, Seat: 0, UserId: legacy.HostId, Ip: legacy.HostIp, Net: legacy.HostNet, CreatedAt: legacy.CreatedAt}}
				if legacy.GuestId > 0 {
					seats = append(seats, GameRoomSeat{RoomId: id, Seat: 1, UserId: legacy.GuestId, Ip: legacy.GuestIp, Net: legacy.GuestNet, CreatedAt: legacy.StartedAt})
				}
				if err := tx.Create(&seats).Error; err != nil {
					return err
				}
				return tx.Model(&GameRoom{}).Where("id = ?", id).Updates(map[string]interface{}{
					"escrow": legacy.Stake, "seats": 2, "players": len(seats),
				}).Error
			})
			if err != nil {
				return err
			}
		}
		after = ids[len(ids)-1]
	}
}
