package controller

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// 渠道级自动恢复的目标选择契约：只挑被自动禁用的密钥/渠道，按渠道自己的间隔到期，
// 手动禁用与归档不参与，禁用后至少等待一个间隔，上次测试时间同样计入。
func TestSelectChannelRecoveryTargets(t *testing.T) {
	const now int64 = 1_800_000_000
	const hourAgo = now - 3600
	never := func(int) int64 { return 0 }
	enabled := dto.ChannelSettings{AutoRecoveryEnabled: true}
	statusInfo := func(statusTime int64) string {
		return `{"status_reason":"429","status_time":` + strconv.FormatInt(statusTime, 10) + `}`
	}
	multiKey := func(status int, keyStatus map[int]int, disabledAt map[int]int64) *model.Channel {
		return &model.Channel{
			Id:     1,
			Status: status,
			Key:    "k1\nk2\nk3",
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:           true,
				MultiKeySize:         3,
				MultiKeyStatusList:   keyStatus,
				MultiKeyDisabledTime: disabledAt,
			},
		}
	}
	allAutoDisabled := map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusAutoDisabled}
	allDisabledHourAgo := map[int]int64{0: hourAgo, 1: hourAgo, 2: hourAgo}

	tests := []struct {
		name         string
		channel      *model.Channel
		setting      dto.ChannelSettings
		lastTestedAt func(int) int64
		want         []int
	}{
		{
			name:         "recovery disabled selects nothing",
			channel:      &model.Channel{Status: common.ChannelStatusAutoDisabled, Key: "k", OtherInfo: statusInfo(hourAgo)},
			setting:      dto.ChannelSettings{},
			lastTestedAt: never,
		},
		{
			name:         "manually disabled channel is skipped",
			channel:      multiKey(common.ChannelStatusManuallyDisabled, allAutoDisabled, allDisabledHourAgo),
			setting:      enabled,
			lastTestedAt: never,
		},
		{
			name:         "archived channel is skipped",
			channel:      multiKey(common.ChannelStatusArchived, allAutoDisabled, allDisabledHourAgo),
			setting:      enabled,
			lastTestedAt: never,
		},
		{
			name:         "auto disabled single key channel is due",
			channel:      &model.Channel{Status: common.ChannelStatusAutoDisabled, Key: "k", OtherInfo: statusInfo(hourAgo)},
			setting:      enabled,
			lastTestedAt: never,
			want:         []int{channelRecoveryChannelTarget},
		},
		{
			name:         "single key channel waits one interval after being disabled",
			channel:      &model.Channel{Status: common.ChannelStatusAutoDisabled, Key: "k", OtherInfo: statusInfo(now - 60)},
			setting:      enabled,
			lastTestedAt: never,
		},
		{
			name:         "single key channel waits one interval after the last test",
			channel:      &model.Channel{Status: common.ChannelStatusAutoDisabled, Key: "k", OtherInfo: statusInfo(hourAgo)},
			setting:      enabled,
			lastTestedAt: func(int) int64 { return now - 60 },
		},
		{
			name:         "enabled single key channel has nothing to recover",
			channel:      &model.Channel{Status: common.ChannelStatusEnabled, Key: "k"},
			setting:      enabled,
			lastTestedAt: never,
		},
		{
			name:         "only auto disabled keys are selected",
			channel:      multiKey(common.ChannelStatusEnabled, map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusManuallyDisabled}, map[int]int64{0: hourAgo, 1: hourAgo}),
			setting:      enabled,
			lastTestedAt: never,
			want:         []int{0},
		},
		{
			name:         "all keys auto disabled recovers keys one by one instead of the channel",
			channel:      multiKey(common.ChannelStatusAutoDisabled, allAutoDisabled, allDisabledHourAgo),
			setting:      enabled,
			lastTestedAt: never,
			want:         []int{0, 1, 2},
		},
		{
			name: "channel level auto disable with usable keys tests the channel itself",
			channel: func() *model.Channel {
				channel := multiKey(common.ChannelStatusAutoDisabled, nil, nil)
				channel.OtherInfo = statusInfo(hourAgo)
				return channel
			}(),
			setting:      enabled,
			lastTestedAt: never,
			want:         []int{channelRecoveryChannelTarget},
		},
		{
			name:         "per key last test time is honored",
			channel:      multiKey(common.ChannelStatusEnabled, allAutoDisabled, allDisabledHourAgo),
			setting:      enabled,
			lastTestedAt: func(keyIndex int) int64 { return map[int]int64{1: now - 30}[keyIndex] },
			want:         []int{0, 2},
		},
		{
			name:         "custom interval delays the retest",
			channel:      multiKey(common.ChannelStatusEnabled, map[int]int{0: common.ChannelStatusAutoDisabled}, map[int]int64{0: now - 20*60}),
			setting:      dto.ChannelSettings{AutoRecoveryEnabled: true, AutoRecoveryIntervalMinutes: 30},
			lastTestedAt: never,
		},
		{
			name:         "custom interval elapsed",
			channel:      multiKey(common.ChannelStatusEnabled, map[int]int{0: common.ChannelStatusAutoDisabled}, map[int]int64{0: now - 31*60}),
			setting:      dto.ChannelSettings{AutoRecoveryEnabled: true, AutoRecoveryIntervalMinutes: 30},
			lastTestedAt: never,
			want:         []int{0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectChannelRecoveryTargets(tc.channel, tc.setting, now, tc.lastTestedAt)
			if len(tc.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// 默认间隔契约：未配置间隔时按 10 分钟重测，前端提示与调度都依赖该默认值。
func TestChannelSettingsAutoRecoveryIntervalDefault(t *testing.T) {
	assert.Equal(t, 10.0, dto.ChannelSettings{}.AutoRecoveryInterval().Minutes())
	assert.Equal(t, 45.0, dto.ChannelSettings{AutoRecoveryIntervalMinutes: 45}.AutoRecoveryInterval().Minutes())
}
