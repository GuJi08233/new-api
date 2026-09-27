package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
