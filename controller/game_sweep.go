package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// gameCenterSweepHandler 定期处理没人访问时也要推进的游戏：结算超时的对战、关闭等太久没人加入的房间、给到点的奖池开奖、
// 结算玩家离开之后自己结束的火箭。
// 玩家在线时这些都在请求里及时处理，这里只兜底双方都离开的情况。Enabled() 折叠了"有没有要处理的"，空闲时不产生任务记录。
type gameCenterSweepHandler struct{}

// gameCenterSweepSummary 是一轮处理的结果，写进系统任务的运行记录。
type gameCenterSweepSummary struct {
	Rooms  int `json:"rooms"`
	Rounds int `json:"rounds"`
	Errors int `json:"errors"`
}

const gameCenterSweepBatch = 100

func (gameCenterSweepHandler) Type() string { return model.SystemTaskTypeGameCenterSweep }

func (gameCenterSweepHandler) Enabled() bool {
	now := common.GetTimestamp()
	if model.HasDueGameRooms(now, now-pvpWaitingSeconds) || model.HasDueGameJackpot(now, now-jackpotStaleSeconds) {
		return true
	}
	crashRounds, err := model.ListStalePlayingGameSessions(gameCrash, now-crashFlightSeconds, 1)
	return err == nil && len(crashRounds) > 0
}

func (gameCenterSweepHandler) Interval() time.Duration { return 15 * time.Second }

func (gameCenterSweepHandler) NewPayload() any { return nil }

func (gameCenterSweepHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := gameCenterSweepSummary{}
	now := common.GetTimestamp()
	rooms, err := model.ListDueGameRooms(now, now-pvpWaitingSeconds, gameCenterSweepBatch)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	for _, room := range rooms {
		if ctx.Err() != nil {
			break
		}
		changed, err := resolveGameRoom(room)
		if err != nil {
			common.SysError(fmt.Sprintf("game center sweep: failed to resolve room %d: %v", room.Id, err))
			summary.Errors++
			continue
		}
		if changed {
			summary.Rooms++
		}
	}
	if err := resolveGameJackpot(); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	settled, failed, err := sweepCrashRounds(ctx, now)
	summary.Rounds, summary.Errors = settled, summary.Errors+failed
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// gameCenterCleanupHandler 每天清理一次过了保留期的游戏记录(见 model.PruneGameRecords)，以及游戏中心自己的系统任务记录。
// 游戏中心关闭时记录不再增长，也就不再清理。
type gameCenterCleanupHandler struct{}

// gameTaskRecordDays 是游戏中心系统任务运行记录的保留天数。结算与提现任务有事可做时每 15 秒一条，只留最近一周供排查。
const gameTaskRecordDays = 7

func (gameCenterCleanupHandler) Type() string { return model.SystemTaskTypeGameCenterCleanup }

func (gameCenterCleanupHandler) Enabled() bool { return operation_setting.GetGameSetting().Enabled }

func (gameCenterCleanupHandler) Interval() time.Duration { return 24 * time.Hour }

func (gameCenterCleanupHandler) NewPayload() any { return nil }

func (gameCenterCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := time.Now()
	var result model.GameCleanupResult
	var err error
	if days := operation_setting.GetGameSetting().RecordRetentionDays; days > 0 {
		result, err = model.PruneGameRecords(ctx, now.AddDate(0, 0, -days))
	}
	if err == nil {
		result.Tasks, err = model.PruneGameSystemTasks(ctx, now.AddDate(0, 0, -gameTaskRecordDays).Unix())
	}
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}
