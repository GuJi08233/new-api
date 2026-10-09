package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// 游戏中心的数据清理。对局、对战房间、奖池、每日计数与游戏币明细里的游戏流水随玩家活跃一直增长，由系统任务按保留天数定期
// 删掉旧的。兑换、提现与管理员调整的流水，以及提现单和账户关系到钱，不清理。

// gameCleanupBatch 是清理时一条删除语句最多删的行数：每批都是一个短语句，SQLite 的写锁不会被一次大删除占住。
const gameCleanupBatch = 500

// gameCleanupLogTypes 是会被清理的游戏币流水：游戏里的奖励、下注与对战输赢。
var gameCleanupLogTypes = []string{GameCoinLogTypeGameReward, GameCoinLogTypeGameWager, GameCoinLogTypeGamePvp}

// gameSystemTaskTypes 是游戏中心自己的系统任务，运行记录由 PruneGameSystemTasks 清理。
var gameSystemTaskTypes = []string{SystemTaskTypeGameCoinWithdraw, SystemTaskTypeGameCenterSweep, SystemTaskTypeGameCenterCleanup}

// GameCleanupResult 是一次清理在各类记录里删掉的行数。
type GameCleanupResult struct {
	Sessions int `json:"sessions"`
	Logs     int `json:"logs"`
	DayRows  int `json:"day_rows"`
	Rooms    int `json:"rooms"`
	Jackpots int `json:"jackpots"`
	Tasks    int `json:"tasks"`
}

// PruneGameRecords 删掉 before 之前的游戏记录：结束了的对局、游戏币明细里的游戏流水、每日计数、结束了的对战房间，以及开完奖
// 或退回了的奖池和它们的投入。每个玩家在每个游戏里成绩最高的一局(同分留最近的一局)留着：游戏页的最高分、下注游戏的单局最高
// 派彩按全部对局算，清理之后不会变低。进行中的对局、房间和奖池不动。ctx 取消时在当前一批之后停下。
func PruneGameRecords(ctx context.Context, before time.Time) (GameCleanupResult, error) {
	var result GameCleanupResult
	var err error
	cutoff := before.Unix()
	// 比这一局成绩高、或者同分而更晚的一局还在，这一局就不是最高成绩，可以删。
	if result.Sessions, err = deleteGameRowsBefore(ctx, &GameSession{}, cutoff, "status = ? AND EXISTS ("+
		"SELECT 1 FROM game_sessions better WHERE better.user_id = game_sessions.user_id AND better.game = game_sessions.game"+
		" AND better.status = ? AND (better.score > game_sessions.score OR (better.score = game_sessions.score AND better.id > game_sessions.id)))",
		GameSessionStatusFinished, GameSessionStatusFinished); err != nil {
		return result, err
	}
	if result.Logs, err = deleteGameRowsBefore(ctx, &GameCoinLog{}, cutoff, "type IN ?", gameCleanupLogTypes); err != nil {
		return result, err
	}
	day := GameCenterDay(before)
	for _, table := range []any{&GameRewardDay{}, &GameRewardIpDay{}, &GameCoinConvertDay{}} {
		deleted, err := deleteGameDaysBefore(ctx, table, day)
		result.DayRows += deleted
		if err != nil {
			return result, err
		}
	}
	if result.Rooms, err = deleteGameRoomsBefore(ctx, cutoff); err != nil {
		return result, err
	}
	// 开放中的一轮只会是最近开的(一位玩家的一轮十分钟就退回，两位玩家的一轮几分钟就开奖)，排除它只是以防万一。
	open, err := GetOpenGameJackpotRound()
	if err != nil {
		return result, err
	}
	openId := 0
	if open != nil {
		openId = open.Id
	}
	if _, err = deleteGameRowsBefore(ctx, &GameJackpotEntry{}, cutoff, "round_id <> ?", openId); err != nil {
		return result, err
	}
	result.Jackpots, err = deleteGameRowsBefore(ctx, &GameJackpotRound{}, cutoff, "status IN ?",
		[]string{GameJackpotStatusDrawn, GameJackpotStatusRefunded})
	return result, err
}

// PruneGameSystemTasks 删掉 before 之前建的、已经结束的游戏中心系统任务记录。结算与提现任务有事可做时每 15 秒一条。
func PruneGameSystemTasks(ctx context.Context, before int64) (int, error) {
	return deleteGameRowsBefore(ctx, &SystemTask{}, before, "type IN ? AND status IN ?", gameSystemTaskTypes,
		[]SystemTaskStatus{SystemTaskStatusSucceeded, SystemTaskStatusFailed})
}

// deleteGameRowsBefore 分批删掉 model 表里 created_at 早于 before、且满足 query 的行，返回删了几行。先找出其中最大的 id，
// 之后按主键从小到大一批批往后扫：留下的行只扫一次，不会每批都从头再扫一遍。ctx 取消时在当前一批之后停下。
func deleteGameRowsBefore(ctx context.Context, model any, before int64, query string, args ...any) (int, error) {
	var last int64
	if err := DB.Model(model).Where("created_at < ?", before).Select("COALESCE(MAX(id), 0)").Scan(&last).Error; err != nil || last == 0 {
		return 0, err
	}
	deleted := 0
	after := int64(0)
	for ctx.Err() == nil {
		var ids []int64
		if err := DB.Model(model).Where("id > ? AND id <= ? AND created_at < ?", after, last, before).Where(query, args...).
			Order("id").Limit(gameCleanupBatch).Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		after = ids[len(ids)-1]
		result := DB.Where("id IN ?", ids).Delete(model)
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += int(result.RowsAffected)
	}
	return deleted, ctx.Err()
}

// deleteGameRoomsBefore 分批删掉 before 之前开的、已经结束或关闭的房间和它们的座位，返回删了几个房间。ctx 取消时在当前一批
// 之后停下。
func deleteGameRoomsBefore(ctx context.Context, before int64) (int, error) {
	deleted := 0
	after := 0
	for ctx.Err() == nil {
		var ids []int
		if err := DB.Model(&GameRoom{}).Where("id > ? AND created_at < ? AND status IN ?", after, before,
			[]string{GameRoomStatusFinished, GameRoomStatusCancelled}).Order("id").Limit(gameCleanupBatch).Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		after = ids[len(ids)-1]
		err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("room_id IN ?", ids).Delete(&GameRoomSeat{}).Error; err != nil {
				return err
			}
			result := tx.Where("id IN ?", ids).Delete(&GameRoom{})
			deleted += int(result.RowsAffected)
			return result.Error
		})
		if err != nil {
			return deleted, err
		}
	}
	return deleted, ctx.Err()
}

// deleteGameDaysBefore 删掉按天计数的 model 表里早于 day 的行，一天一批，返回删了几行。ctx 取消时在当前一天之后停下。
func deleteGameDaysBefore(ctx context.Context, model any, day string) (int, error) {
	deleted := 0
	for ctx.Err() == nil {
		var days []string
		if err := DB.Model(model).Where("day < ?", day).Order("day").Limit(1).Pluck("day", &days).Error; err != nil {
			return deleted, err
		}
		if len(days) == 0 {
			return deleted, nil
		}
		result := DB.Where("day = ?", days[0]).Delete(model)
		if result.Error != nil || result.RowsAffected == 0 {
			return deleted, result.Error
		}
		deleted += int(result.RowsAffected)
	}
	return deleted, ctx.Err()
}
