package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 仅包含旧表拥有的列，确保测试经过真实的新增列与成员回填过程。
type legacyGameRoomFixture struct {
	Id          int
	Game        string
	Status      string
	Stake       int `gorm:"type:bigint"`
	HostId      int
	GuestId     int
	HostIp      string
	GuestIp     string
	State       string
	Version     int
	ToMove      int
	Deadline    int64
	WinnerId    int
	Rake        int
	RakePercent int
	CreatedAt   int64
	StartedAt   int64
}

func (legacyGameRoomFixture) TableName() string { return "game_rooms" }

func TestMigrateGameRoomSeatsPreservesOldRoomsAndEscrow(t *testing.T) {
	for _, withNetColumns := range []bool{false, true} {
		name := "derive historical nets"
		if withNetColumns {
			name = "preserve historical net columns"
		}
		t.Run(name, func(t *testing.T) {
			original := DB
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			DB = db
			t.Cleanup(func() {
				DB = original
				assert.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&legacyGameRoomFixture{}, &GameCoinAccount{}, &GameCoinLog{}, &GameRewardDay{}))
			legacy := []legacyGameRoomFixture{
				{Id: 1, Game: "gomoku", Status: GameRoomStatusWaiting, Stake: 10, HostId: 2601, HostIp: gameTestIp, CreatedAt: 1},
				{Id: 2, Game: "gomoku", Status: GameRoomStatusPlaying, Stake: 10, HostId: 2602, GuestId: 2603, HostIp: gameTestIp, GuestIp: gameTestOtherIp, State: "old state", Version: 4, Deadline: 100, RakePercent: 10, CreatedAt: 2, StartedAt: 3},
				{Id: 3, Game: "gomoku", Status: GameRoomStatusFinished, Stake: 10, HostId: 2604, GuestId: 2605, WinnerId: 2605, Rake: 2, CreatedAt: 4, StartedAt: 5},
				{Id: 4, Game: "gomoku", Status: GameRoomStatusCancelled, Stake: 10, HostId: 2606, CreatedAt: 6},
			}
			require.NoError(t, db.Create(&legacy).Error)
			if withNetColumns {
				require.NoError(t, db.Exec("ALTER TABLE game_rooms ADD COLUMN host_net bigint").Error)
				require.NoError(t, db.Exec("ALTER TABLE game_rooms ADD COLUMN guest_net bigint").Error)
				require.NoError(t, db.Table("game_rooms").Where("id = ?", 3).Updates(map[string]interface{}{"host_net": -7, "guest_net": 5}).Error)
			}
			require.NoError(t, db.Create(&[]GameCoinAccount{
				{UserId: 2601, Balance: 90, TotalWagered: 10},
				{UserId: 2602, Balance: 90, TotalWagered: 10},
				{UserId: 2603, Balance: 90, TotalWagered: 10},
			}).Error)
			require.NoError(t, db.AutoMigrate(&GameRoom{}, &GameRoomSeat{}))
			require.NoError(t, migrateGameRoomSeats(db))
			require.NoError(t, migrateGameRoomSeats(db), "重启不得重复入座或扣款")
			seats, err := GetGameRoomSeats([]int{1, 2, 3, 4})
			require.NoError(t, err)
			require.Len(t, seats[1], 1)
			require.Len(t, seats[2], 2)
			require.Len(t, seats[3], 2)
			require.Len(t, seats[4], 1)
			assert.Equal(t, gameTestOtherIp, seats[2][1].Ip)
			assert.Equal(t, int64(3), seats[2][1].CreatedAt)
			if withNetColumns {
				assert.Equal(t, -7, seats[3][0].Net)
				assert.Equal(t, 5, seats[3][1].Net)
			} else {
				assert.Equal(t, -10, seats[3][0].Net)
				assert.Equal(t, 8, seats[3][1].Net)
			}
			var logCount int64
			require.NoError(t, db.Model(&GameCoinLog{}).Count(&logCount).Error)
			assert.Zero(t, logCount, "迁移本身不移动游戏币")
			active, err := GetUserActiveGameRooms(2603)
			require.NoError(t, err)
			require.Len(t, active, 1)
			assert.Equal(t, 2, active[0].Id)
			history, err := ListUserFinishedGameRooms(2605, "gomoku", 10)
			require.NoError(t, err)
			require.Len(t, history, 1)

			waiting, err := GetGameRoom(1)
			require.NoError(t, err)
			assert.Equal(t, 10, waiting.Escrow)
			assert.Equal(t, 2, waiting.Seats)
			assert.Equal(t, 1, waiting.Players)
			require.NoError(t, CancelGameRoom(waiting, GameRoomEndExpired))
			assert.Equal(t, 100, gameBalance(t, 2601), "旧房主押注可以正常退回")
			playing, err := GetGameRoom(2)
			require.NoError(t, err)
			assert.Equal(t, "old state", playing.State)
			assert.Equal(t, 4, playing.Version)
			require.NoError(t, FinishGameRoom(playing, GameRoomResult{Nets: []int{-10, 10}, WinnerSeat: 1}, GameRoomEndWin, "finished", "2026-10-10"))
			assert.Equal(t, 2, playing.Rake, "保持旧双人按奖池抽成")
			assert.Equal(t, 108, gameBalance(t, 2603))
		})
	}
}
