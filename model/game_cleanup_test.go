package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 清理过了保留期的游戏记录。结束了的对局只删不影响最高成绩的：每个玩家在每个游戏里成绩最高的一局留着，同分留最近的一局，
// 清理前后游戏页的最高分不变。游戏币明细只删游戏里的奖励、下注与对战流水，兑换、提现与管理员调整的流水留着。进行中的对局与
// 房间、开放中的奖池、保留期以内的记录都不动；再清理一次什么也不删。系统任务记录只清理游戏中心自己的、已经结束的。
func TestPruneGameRecordsKeepsBestScoresAndMoneyRecords(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	old, recent := now.AddDate(0, 0, -100).Unix(), now.AddDate(0, 0, -1).Unix()
	oldDay, today := now.AddDate(0, 0, -100).Format("2006-01-02"), now.Format("2006-01-02")

	sessions := []GameSession{
		{UserId: 3001, Game: "tetris", Score: 100, CreatedAt: old},      // 0 删：有更高分
		{UserId: 3001, Game: "tetris", Score: 300, CreatedAt: old},      // 1 留：最高分
		{UserId: 3001, Game: "tetris", Score: 200, CreatedAt: old},      // 2 删
		{UserId: 3001, Game: "tetris", Score: 250, CreatedAt: recent},   // 3 留：保留期以内
		{UserId: 3001, Game: "sokoban", Score: 1, CreatedAt: old},       // 4 删：同分还有更近的一局
		{UserId: 3001, Game: "sokoban", Score: 1, CreatedAt: old},       // 5 删
		{UserId: 3001, Game: "sokoban", Score: 1, CreatedAt: recent},    // 6 留
		{UserId: 3002, Game: "slots", Score: 0, CreatedAt: old},         // 7 删
		{UserId: 3002, Game: "slots", Score: 0, CreatedAt: old},         // 8 留：同分里最近的一局
		{UserId: 3002, Game: "blackjack", Score: 0, CreatedAt: old - 1}, // 9 留：还没结束
	}
	for i := range sessions {
		sessions[i].Status, sessions[i].FinishedAt = GameSessionStatusFinished, sessions[i].CreatedAt
	}
	sessions[9].Status, sessions[9].FinishedAt = GameSessionStatusPlaying, 0
	require.NoError(t, DB.Create(&sessions).Error)
	owners := map[int][]string{3001: {"tetris", "sokoban"}, 3002: {"slots", "blackjack"}}
	bestBefore := map[string]int{}
	for userId, games := range owners {
		for _, game := range games {
			best, err := GetGameBestScore(userId, game)
			require.NoError(t, err)
			bestBefore[game] = best
		}
	}

	require.NoError(t, DB.Create(&[]GameCoinLog{
		{UserId: 3001, Type: GameCoinLogTypeGameWager, Amount: -1, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeGameReward, Amount: 1, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeGamePvp, Amount: 2, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeConvert, Amount: -3, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeWithdraw, Amount: -4, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeWithdrawRefund, Amount: 4, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeAdminAdjust, Amount: 5, CreatedAt: old},
		{UserId: 3001, Type: GameCoinLogTypeGameWager, Amount: -1, CreatedAt: recent},
	}).Error)
	require.NoError(t, DB.Create(&[]GameRewardDay{{UserId: 3001, Game: "tetris", Day: oldDay}, {UserId: 3001, Game: "tetris", Day: today}}).Error)
	require.NoError(t, DB.Create(&[]GameRewardIpDay{{Ip: gameTestIp, Game: "tetris", Day: oldDay}, {Ip: gameTestIp, Game: "tetris", Day: today}}).Error)
	require.NoError(t, DB.Create(&[]GameCoinConvertDay{{UserId: 3001, Day: oldDay, Used: 1}, {UserId: 3001, Day: today, Used: 1}}).Error)

	rooms := []GameRoom{
		{Game: "gomoku", Status: GameRoomStatusFinished, HostId: 3001, CreatedAt: old},
		{Game: "gomoku", Status: GameRoomStatusCancelled, HostId: 3001, CreatedAt: old},
		{Game: "gomoku", Status: GameRoomStatusPlaying, HostId: 3002, CreatedAt: old},
		{Game: "gomoku", Status: GameRoomStatusFinished, HostId: 3001, CreatedAt: recent},
	}
	require.NoError(t, DB.Create(&rooms).Error)
	for _, room := range rooms {
		require.NoError(t, DB.Create(&GameRoomSeat{RoomId: room.Id, UserId: room.HostId, CreatedAt: room.CreatedAt}).Error)
	}

	rounds := []GameJackpotRound{
		{Slot: "old-drawn", Status: GameJackpotStatusDrawn, CreatedAt: old},
		{Slot: "old-refunded", Status: GameJackpotStatusRefunded, CreatedAt: old},
		{Slot: "recent-drawn", Status: GameJackpotStatusDrawn, CreatedAt: recent},
		{Slot: gameJackpotOpenSlot, Status: GameJackpotStatusOpen, CreatedAt: old},
	}
	require.NoError(t, DB.Create(&rounds).Error)
	for _, round := range rounds {
		require.NoError(t, DB.Create(&GameJackpotEntry{RoundId: round.Id, UserId: 3001, Amount: 1, CreatedAt: round.CreatedAt}).Error)
	}

	require.NoError(t, DB.Create(&[]SystemTask{
		{TaskID: "sweep-old", Type: SystemTaskTypeGameCenterSweep, Status: SystemTaskStatusSucceeded, CreatedAt: old},
		{TaskID: "withdraw-old", Type: SystemTaskTypeGameCoinWithdraw, Status: SystemTaskStatusFailed, CreatedAt: old},
		{TaskID: "sweep-running", Type: SystemTaskTypeGameCenterSweep, Status: SystemTaskStatusRunning, CreatedAt: old},
		{TaskID: "channel-old", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusSucceeded, CreatedAt: old},
		{TaskID: "sweep-recent", Type: SystemTaskTypeGameCenterSweep, Status: SystemTaskStatusSucceeded, CreatedAt: recent},
	}).Error)

	cutoff := now.AddDate(0, 0, -90)
	result, err := PruneGameRecords(context.Background(), cutoff)
	require.NoError(t, err)
	assert.Equal(t, GameCleanupResult{Sessions: 5, Logs: 3, DayRows: 3, Rooms: 2, Jackpots: 2}, result)
	tasks, err := PruneGameSystemTasks(context.Background(), now.AddDate(0, 0, -7).Unix())
	require.NoError(t, err)
	assert.Equal(t, 2, tasks)

	var sessionIds []int
	require.NoError(t, DB.Model(&GameSession{}).Order("id").Pluck("id", &sessionIds).Error)
	assert.Equal(t, []int{sessions[1].Id, sessions[3].Id, sessions[6].Id, sessions[8].Id, sessions[9].Id}, sessionIds)
	for userId, games := range owners {
		for _, game := range games {
			best, err := GetGameBestScore(userId, game)
			require.NoError(t, err)
			assert.Equal(t, bestBefore[game], best, "%s 的最高成绩不变", game)
		}
	}

	var logTypes []string
	require.NoError(t, DB.Model(&GameCoinLog{}).Order("id").Pluck("type", &logTypes).Error)
	assert.Equal(t, []string{GameCoinLogTypeConvert, GameCoinLogTypeWithdraw, GameCoinLogTypeWithdrawRefund, GameCoinLogTypeAdminAdjust, GameCoinLogTypeGameWager}, logTypes)
	for _, table := range []any{&GameRewardDay{}, &GameRewardIpDay{}, &GameCoinConvertDay{}} {
		var days []string
		require.NoError(t, DB.Model(table).Pluck("day", &days).Error)
		assert.Equal(t, []string{today}, days)
	}

	var roomIds []int
	require.NoError(t, DB.Model(&GameRoom{}).Order("id").Pluck("id", &roomIds).Error)
	assert.Equal(t, []int{rooms[2].Id, rooms[3].Id}, roomIds)
	var seatRooms []int
	require.NoError(t, DB.Model(&GameRoomSeat{}).Order("room_id").Pluck("room_id", &seatRooms).Error)
	assert.Equal(t, []int{rooms[2].Id, rooms[3].Id}, seatRooms, "删掉的房间连座位一起删")
	var slots []string
	require.NoError(t, DB.Model(&GameJackpotRound{}).Order("id").Pluck("slot", &slots).Error)
	assert.Equal(t, []string{"recent-drawn", gameJackpotOpenSlot}, slots)
	var entryRounds []int
	require.NoError(t, DB.Model(&GameJackpotEntry{}).Order("id").Pluck("round_id", &entryRounds).Error)
	assert.Equal(t, []int{rounds[2].Id, rounds[3].Id}, entryRounds)
	var taskIds []string
	require.NoError(t, DB.Model(&SystemTask{}).Order("id").Pluck("task_id", &taskIds).Error)
	assert.Equal(t, []string{"sweep-running", "channel-old", "sweep-recent"}, taskIds)

	again, err := PruneGameRecords(context.Background(), cutoff)
	require.NoError(t, err)
	assert.Equal(t, GameCleanupResult{}, again)
}
