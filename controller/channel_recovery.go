package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	channelRecoveryChannelTarget = -1
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

// channelRecoveryLastTest 记录每个恢复目标（渠道或密钥）上次测试的时间。进程内即可：
// 系统任务只在持有租约的主节点上执行，进程重启后至多提前一次重测。
var channelRecoveryLastTest sync.Map // "channelId:keyIndex" -> int64 unix seconds

func channelRecoveryTargetKey(channelId int, keyIndex int) string {
	return fmt.Sprintf("%d:%d", channelId, keyIndex)
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

	type recoveryJob struct {
		channel *model.Channel
		targets []int
	}
	now := common.GetTimestamp()
	jobs := make([]recoveryJob, 0)
	total := 0
	for _, channel := range channels {
		targets := selectChannelRecoveryTargets(channel, channel.GetSetting(), now, func(keyIndex int) int64 {
			if value, ok := channelRecoveryLastTest.Load(channelRecoveryTargetKey(channel.Id, keyIndex)); ok {
				return value.(int64)
			}
			return 0
		})
		if len(targets) == 0 {
			continue
		}
		jobs = append(jobs, recoveryJob{channel: channel, targets: targets})
		total += len(targets)
	}
	summary.Channels = len(jobs)

	processed := 0
	for _, job := range jobs {
		keys := job.channel.GetKeys()
		for _, target := range job.targets {
			if ctx.Err() != nil {
				return summary, nil
			}
			if report != nil {
				report(processed, total)
			}
			testTarget := job.channel
			if target != channelRecoveryChannelTarget {
				// GetNextEnabledKey 只会选出可用密钥；测试指定密钥时改用把 Key 固定为该密钥的
				// 单密钥视图，从而复用完整的测试链路（含每日计数与测试日志）。
				pinned := *job.channel
				pinned.Key = keys[target]
				pinned.Keys = nil
				pinned.ChannelInfo.IsMultiKey = false
				testTarget = &pinned
			}
			result := testChannel(ctx, testTarget, testUserID, "", "", shouldUseStreamForAutomaticChannelTest(job.channel))
			channelRecoveryLastTest.Store(channelRecoveryTargetKey(job.channel.Id, target), common.GetTimestamp())
			summary.Tested++
			processed++
			if result.localErr != nil || result.newAPIError != nil {
				summary.Failed++
				var failure error = result.newAPIError
				if result.localErr != nil {
					failure = result.localErr
				}
				common.SysLog(fmt.Sprintf("channel recovery test failed: channel_id=%d key_index=%d error=%s", job.channel.Id, target, common.LocalLogPreview(failure.Error())))
			} else if target == channelRecoveryChannelTarget {
				service.EnableChannel(job.channel.Id, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), job.channel.Name)
				summary.Recovered++
			} else if service.EnableChannelKey(job.channel.Id, target, job.channel.Name) {
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
