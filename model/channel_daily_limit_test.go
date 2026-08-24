package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 内存模式下的每日计数与上限判定契约：达到上限即拦截、0 表示不限、自然日翻转清零。
func TestChannelDailyRequestLimit(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	t.Run("count increments and limit boundary", func(t *testing.T) {
		const channelId = 910001
		require.EqualValues(t, 0, GetChannelDailyRequestCount(channelId, nil))

		for i := 0; i < 3; i++ {
			IncrChannelDailyRequestCount(channelId, nil)
		}
		assert.EqualValues(t, 3, GetChannelDailyRequestCount(channelId, nil))

		assert.False(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: 4}), "count below limit must pass")
		assert.True(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: 3}), "count at limit must block")
		assert.True(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: 1}))
	})

	t.Run("non-positive limit means unlimited", func(t *testing.T) {
		const channelId = 910002
		for i := 0; i < 5; i++ {
			IncrChannelDailyRequestCount(channelId, nil)
		}
		assert.False(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: 0}))
		assert.False(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: -1}))
	})

	t.Run("count resets on date rollover", func(t *testing.T) {
		const channelId = 910003
		IncrChannelDailyRequestCount(channelId, nil)
		require.EqualValues(t, 1, GetChannelDailyRequestCount(channelId, nil))

		counter := getChannelDailyCounter(channelId)
		counter.mu.Lock()
		counter.date = "2000-01-01"
		counter.count = 99
		counter.mu.Unlock()

		assert.EqualValues(t, 0, GetChannelDailyRequestCount(channelId, nil), "stale date must reset to zero")
		assert.False(t, IsChannelDailyLimitReached(channelId, ChannelDailyLimitConfig{Limit: 1}))
	})
}

// 日切时区契约：同一时刻在不同 UTC 偏移下归属不同的自然日；
// 未设置或越界的偏移跟随服务器本地时区。
func TestChannelDailyDateAt(t *testing.T) {
	// UTC 2026-01-01 20:00
	now := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)

	utc := 0
	assert.Equal(t, "2026-01-01", channelDailyDateAt(now, &utc))

	cst := 8 * 60 // UTC+8 已跨入次日
	assert.Equal(t, "2026-01-02", channelDailyDateAt(now, &cst))

	india := 330 // UTC+5:30 -> 01:30 次日
	assert.Equal(t, "2026-01-02", channelDailyDateAt(now, &india))

	westmost := -12 * 60 // UTC-12 -> 08:00 当日
	assert.Equal(t, "2026-01-01", channelDailyDateAt(now, &westmost))

	local := now.Local().Format("2006-01-02")
	assert.Equal(t, local, channelDailyDateAt(now, nil), "nil offset must follow server local time")
	outOfRange := 15 * 60
	assert.Equal(t, local, channelDailyDateAt(now, &outOfRange), "out-of-range offset must fall back to server local time")
}

// GetDailyLimitConfig 必须无 GetSetting 的错误回写副作用，且容忍空/损坏配置。
func TestChannelGetDailyLimitConfig(t *testing.T) {
	channel := &Channel{Id: 910010}
	assert.False(t, channel.GetDailyLimitConfig().Enabled(), "nil setting means unlimited")

	setting := `{"daily_request_limit": 700, "daily_request_limit_utc_offset": 480}`
	channel.Setting = &setting
	config := channel.GetDailyLimitConfig()
	assert.EqualValues(t, 700, config.Limit)
	require.NotNil(t, config.UTCOffsetMinutes)
	assert.Equal(t, 480, *config.UTCOffsetMinutes)

	noOffset := `{"daily_request_limit": 700}`
	channel.Setting = &noOffset
	assert.Nil(t, channel.GetDailyLimitConfig().UTCOffsetMinutes, "absent offset must stay nil (server local time)")

	broken := `{invalid json`
	channel.Setting = &broken
	assert.False(t, channel.GetDailyLimitConfig().Enabled(), "broken setting must fail open")
	assert.Equal(t, broken, *channel.Setting, "must not mutate the stored setting")
}
