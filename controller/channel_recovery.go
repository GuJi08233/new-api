package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// 渠道级自动恢复：渠道在额外设置中开启 auto_recovery_enabled 后，系统任务按渠道自己的
// 间隔定时测试被自动禁用的密钥（多密钥渠道逐把密钥测试）或被自动禁用的渠道本身，
// 测试通过即恢复。与全局"定时测试"相互独立，也不受全局"成功时自动启用"开关约束。

const (
	// channelRecoveryScheduleCacheTTL 是调度信息（是否有渠道开启、最短间隔）的缓存时长：
	// 系统任务调度器每 15 秒询问一次 Enabled/Interval，不必每次都查库。
	channelRecoveryScheduleCacheTTL = 30 * time.Second
	// channelRecoveryChannelTarget 表示测试渠道本身（使用其可用密钥），而非某把具体密钥。
	channelRecoveryChannelTarget = model.ChannelRecoveryChannelTarget
)

type channelRecoveryHandler struct{}

func (channelRecoveryHandler) Type() string { return model.SystemTaskTypeChannelRecovery }

func (channelRecoveryHandler) Enabled() bool {
	enabled, _ := channelRecoverySchedule()
	return enabled
}

func (channelRecoveryHandler) Interval() time.Duration {
	_, interval := channelRecoverySchedule()
	return interval
}

func (channelRecoveryHandler) NewPayload() any { return nil }

func (channelRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelRecoveryTask(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

var channelRecoveryScheduleCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	enabled   bool
	interval  time.Duration
}

// channelRecoverySchedule 汇总所有开启了自动恢复的渠道：是否至少有一个，以及最短测试间隔。
// 系统任务以最短间隔运行，每个渠道再按自己的间隔决定目标是否到期。
func channelRecoverySchedule() (bool, time.Duration) {
	cache := &channelRecoveryScheduleCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if time.Now().Before(cache.expiresAt) {
		return cache.enabled, cache.interval
	}
	settings, err := model.GetChannelAutoRecoverySettings()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to load channel auto recovery settings: %v", err))
		return cache.enabled, cache.interval
	}
	enabled := false
	interval := dto.ChannelSettings{}.AutoRecoveryInterval()
	for _, setting := range settings {
		if !enabled || setting.Interval < interval {
			interval = setting.Interval
		}
		enabled = true
	}
	cache.enabled = enabled
	cache.interval = interval
	cache.expiresAt = time.Now().Add(channelRecoveryScheduleCacheTTL)
	return enabled, interval
}

// channelRecoverySummary 记录一轮自动恢复的结果，作为系统任务的运行结果持久化。
type channelRecoverySummary struct {
	Channels  int `json:"channels"`
	Tested    int `json:"tested"`
	Recovered int `json:"recovered"`
	Failed    int `json:"failed"`
}

// selectChannelRecoveryTargets 返回渠道本轮到期的恢复目标：多密钥渠道中被自动禁用的密钥索引，
// 以及（渠道整体处于自动禁用且仍有可用密钥时）代表渠道本身的 channelRecoveryChannelTarget。
// 到期以"被禁用时间"与"上次测试时间"中较晚者为基准，保证禁用后至少等待一个间隔再重测；
// 手动禁用的密钥/渠道与归档渠道不参与。
func selectChannelRecoveryTargets(channel *model.Channel, setting dto.ChannelSettings, now int64, lastTestedAt func(keyIndex int) int64) []int {
	if !setting.AutoRecoveryEnabled {
		return nil
	}
	if channel.Status == common.ChannelStatusManuallyDisabled || channel.Status == common.ChannelStatusArchived {
		return nil
	}
	intervalSeconds := int64(setting.AutoRecoveryInterval().Seconds())
	due := func(keyIndex int, disabledAt int64) bool {
		since := disabledAt
		if last := lastTestedAt(keyIndex); last > since {
			since = last
		}
		return now-since >= intervalSeconds
	}

	targets := make([]int, 0)
	if !channel.ChannelInfo.IsMultiKey {
		if channel.Status == common.ChannelStatusAutoDisabled && due(channelRecoveryChannelTarget, channel.GetStatusTime()) {
			targets = append(targets, channelRecoveryChannelTarget)
		}
		return targets
	}

	hasEnabledKey := false
	for index := range channel.GetKeys() {
		status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
		if !exists || status == common.ChannelStatusEnabled {
			hasEnabledKey = true
			continue
		}
		if status != common.ChannelStatusAutoDisabled {
			continue
		}
		if due(index, channel.ChannelInfo.MultiKeyDisabledTime[index]) {
			targets = append(targets, index)
		}
	}
	// 渠道整体被自动禁用但仍有可用密钥（余额检查、MJ 封禁等走的是渠道级路径）时测试渠道本身
	if channel.Status == common.ChannelStatusAutoDisabled && hasEnabledKey && due(channelRecoveryChannelTarget, channel.GetStatusTime()) {
		targets = append(targets, channelRecoveryChannelTarget)
	}
	return targets
}

// runChannelRecoveryTask 执行一轮渠道级自动恢复：逐个测试到期目标，测试通过即恢复。
func runChannelRecoveryTask(ctx context.Context, report func(processed, total int)) (channelRecoverySummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	summary := channelRecoverySummary{}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return summary, err
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return summary, err
	}
	// 记录超过最长恢复间隔后已不影响调度，定期清理已删除渠道和旧密钥的记录。
	now := common.GetTimestamp()
	retentionSeconds := int64(2 * dto.MaxAutoRecoveryIntervalMinutes * 60)
	if err := model.DeleteExpiredChannelRecoveryStates(now - retentionSeconds); err != nil {
		return summary, err
	}
	lastTests, err := model.GetChannelRecoveryLastTests()
	if err != nil {
		return summary, err
	}

	type recoveryJob struct {
		channel *model.Channel
		targets []model.ChannelRecoveryTarget
	}
	jobs := make([]recoveryJob, 0)
	total := 0
	for _, channel := range channels {
		channel.Keys = channel.GetKeys()
		indices := selectChannelRecoveryTargets(channel, channel.GetSetting(), now, func(keyIndex int) int64 {
			key := channel.Key
			if keyIndex != channelRecoveryChannelTarget {
				key = channel.Keys[keyIndex]
			}
			return lastTests[channel.Id][model.ChannelRecoveryTargetID(key, keyIndex == channelRecoveryChannelTarget)]
		})
		targets := make([]model.ChannelRecoveryTarget, 0, len(indices))
		for _, keyIndex := range indices {
			if target, ok := model.NewChannelRecoveryTarget(channel, keyIndex); ok {
				targets = append(targets, target)
			}
		}
		if len(targets) == 0 {
			continue
		}
		jobs = append(jobs, recoveryJob{channel: channel, targets: targets})
		total += len(targets)
	}
	summary.Channels = len(jobs)

	processed := 0
	for _, job := range jobs {
		for _, target := range job.targets {
			if ctx.Err() != nil {
				return summary, nil
			}
			if report != nil {
				report(processed, total)
			}
			key := target.TestedKey
			if target.KeyIndex == channelRecoveryChannelTarget {
				key = target.KeyList
			}
			targetID := model.ChannelRecoveryTargetID(key, target.KeyIndex == channelRecoveryChannelTarget)
			// 先落库再发请求，进程中断或任务换节点执行时也不能跳过已开始探测的冷却期。
			if err := model.RecordChannelRecoveryTest(target.ChannelID, targetID, common.GetTimestamp()); err != nil {
				return summary, err
			}
			// 渠道和密钥目标都固定到探测快照中的实际密钥，恢复时核对同一份身份与禁用记录。
			pinned := *job.channel
			pinned.Key = target.TestedKey
			pinned.Keys = nil
			pinned.ChannelInfo.IsMultiKey = false
			result := testChannel(ctx, &pinned, testUserID, "", "", shouldUseStreamForAutomaticChannelTest(job.channel))
			summary.Tested++
			processed++
			if err := model.RecordChannelRecoveryTest(target.ChannelID, targetID, common.GetTimestamp()); err != nil {
				return summary, err
			}
			if ctx.Err() != nil {
				return summary, nil
			}
			if result.localErr != nil || result.newAPIError != nil {
				summary.Failed++
				var failure error = result.newAPIError
				if result.localErr != nil {
					failure = result.localErr
				}
				common.SysLog(fmt.Sprintf("channel recovery test failed: channel_id=%d key_index=%d error=%s", job.channel.Id, target.KeyIndex, common.LocalLogPreview(failure.Error())))
			} else if service.RecoverChannel(target, job.channel.Name) {
				summary.Recovered++
			}
			if common.RequestInterval > 0 {
				select {
				case <-ctx.Done():
					return summary, nil
				case <-time.After(common.RequestInterval):
				}
			}
		}
	}
	if report != nil && ctx.Err() == nil {
		report(total, total)
	}
	return summary, nil
}
