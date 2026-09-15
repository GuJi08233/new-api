package model

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 内存模式下的每日计数与上限判定契约：达到上限即拦截、0 表示不限、自然日翻转清零。
func TestChannelDailyRequestLimit(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	serverLocal := ChannelRequestLimitConfig{}

	t.Run("count increments and limit boundary", func(t *testing.T) {
		const channelId = 910001
		require.EqualValues(t, 0, GetChannelDailyRequestCount(channelId, serverLocal))

		for i := 0; i < 3; i++ {
			IncrChannelRequestCount(channelId, serverLocal)
		}
		assert.EqualValues(t, 3, GetChannelDailyRequestCount(channelId, serverLocal))

		assert.False(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: 4}), "count below limit must pass")
		assert.True(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: 3}), "count at limit must block")
		assert.True(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: 1}))
	})

	t.Run("non-positive limit means unlimited", func(t *testing.T) {
		const channelId = 910002
		for i := 0; i < 5; i++ {
			IncrChannelRequestCount(channelId, serverLocal)
		}
		assert.False(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: 0}))
		assert.False(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: -1}))
	})

	t.Run("count resets on date rollover", func(t *testing.T) {
		const channelId = 910003
		IncrChannelRequestCount(channelId, serverLocal)
		require.EqualValues(t, 1, GetChannelDailyRequestCount(channelId, serverLocal))

		counter := getChannelWindowCounter(channelDailyCountKeyPrefix, channelId)
		counter.mu.Lock()
		counter.window = "2000-01-01"
		counter.count = 99
		counter.mu.Unlock()

		assert.EqualValues(t, 0, GetChannelDailyRequestCount(channelId, serverLocal), "stale date must reset to zero")
		assert.False(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{DailyLimit: 1}))
	})
}

// 频率限制契约：请求计数在承接时递增、成功计数只在成功后递增，各自独立达到上限即拦截；
// 未配置成功上限时不记录成功计数；周期窗口切换后清零。
func TestChannelPeriodRequestLimit(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	t.Run("requests per period", func(t *testing.T) {
		const channelId = 920001
		config := ChannelRequestLimitConfig{PeriodMinutes: 1, MaxRequests: 3}
		for i := 0; i < 2; i++ {
			IncrChannelRequestCount(channelId, config)
		}
		assert.EqualValues(t, 2, GetChannelPeriodRequestCount(channelId, config))
		assert.False(t, IsChannelRequestLimitReached(channelId, config))

		IncrChannelRequestCount(channelId, config)
		assert.True(t, IsChannelRequestLimitReached(channelId, config), "count at limit must block")
		assert.False(t, IsChannelRequestLimitReached(channelId, ChannelRequestLimitConfig{PeriodMinutes: 1}), "zero limit means unlimited")
	})

	t.Run("successes per period are counted separately", func(t *testing.T) {
		const channelId = 920002
		config := ChannelRequestLimitConfig{MaxSuccess: 2}
		for i := 0; i < 3; i++ {
			IncrChannelRequestCount(channelId, config)
		}
		assert.EqualValues(t, 0, GetChannelPeriodSuccessCount(channelId, config))
		assert.False(t, IsChannelRequestLimitReached(channelId, config), "accepted requests must not consume the success budget")

		IncrChannelSuccessCount(channelId, config)
		IncrChannelSuccessCount(channelId, config)
		assert.EqualValues(t, 2, GetChannelPeriodSuccessCount(channelId, config))
		assert.True(t, IsChannelRequestLimitReached(channelId, config))
	})

	t.Run("success count is skipped without a success limit", func(t *testing.T) {
		const channelId = 920003
		IncrChannelSuccessCount(channelId, ChannelRequestLimitConfig{MaxRequests: 5})
		assert.EqualValues(t, 0, GetChannelPeriodSuccessCount(channelId, ChannelRequestLimitConfig{MaxSuccess: 1}))
	})

	t.Run("count resets when the period window changes", func(t *testing.T) {
		const channelId = 920004
		config := ChannelRequestLimitConfig{PeriodMinutes: 5, MaxRequests: 1}
		IncrChannelRequestCount(channelId, config)
		require.True(t, IsChannelRequestLimitReached(channelId, config))

		counter := getChannelWindowCounter(channelPeriodCountKeyPrefix, channelId)
		counter.mu.Lock()
		counter.window = "stale"
		counter.mu.Unlock()

		assert.EqualValues(t, 0, GetChannelPeriodRequestCount(channelId, config))
		assert.False(t, IsChannelRequestLimitReached(channelId, config))
	})
}

// 周期窗口契约：按自然周期对齐，同一周期内的时刻共享窗口，跨周期即切换；未配置周期按 1 分钟。
func TestChannelPeriodWindowAt(t *testing.T) {
	// 整日边界必然是任何整分钟周期的边界
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	fiveMinutes := ChannelRequestLimitConfig{PeriodMinutes: 5}
	assert.Equal(t, fiveMinutes.periodWindowAt(start), fiveMinutes.periodWindowAt(start.Add(4*time.Minute+59*time.Second)))
	assert.NotEqual(t, fiveMinutes.periodWindowAt(start), fiveMinutes.periodWindowAt(start.Add(5*time.Minute)))

	defaultPeriod := ChannelRequestLimitConfig{}
	assert.Equal(t, time.Minute, defaultPeriod.Period())
	assert.Equal(t, defaultPeriod.periodWindowAt(start), defaultPeriod.periodWindowAt(start.Add(59*time.Second)))
	assert.NotEqual(t, defaultPeriod.periodWindowAt(start), defaultPeriod.periodWindowAt(start.Add(time.Minute)))
}

// 日切时区契约：同一时刻在不同 UTC 偏移下归属不同的自然日；
// 未设置或越界的偏移跟随服务器本地时区。
func TestChannelDailyWindowAt(t *testing.T) {
	// UTC 2026-01-01 20:00
	now := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	offset := func(minutes int) ChannelRequestLimitConfig {
		return ChannelRequestLimitConfig{UTCOffsetMinutes: &minutes}
	}

	assert.Equal(t, "2026-01-01", offset(0).dailyWindowAt(now))
	assert.Equal(t, "2026-01-02", offset(8*60).dailyWindowAt(now), "UTC+8 已跨入次日")
	assert.Equal(t, "2026-01-02", offset(330).dailyWindowAt(now), "UTC+5:30 -> 01:30 次日")
	assert.Equal(t, "2026-01-01", offset(-12*60).dailyWindowAt(now), "UTC-12 -> 08:00 当日")

	local := now.Local().Format("2006-01-02")
	assert.Equal(t, local, ChannelRequestLimitConfig{}.dailyWindowAt(now), "nil offset must follow server local time")
	assert.Equal(t, local, offset(15*60).dailyWindowAt(now), "out-of-range offset must fall back to server local time")
}

// IANA 时区日切契约：太平洋时间自动切换夏令时（冬令时 UTC-8、夏令时 UTC-7），
// 时区优先于固定偏移；无法加载的时区名退回固定偏移，再退回服务器本地时区。
func TestChannelDailyWindowAtTimezone(t *testing.T) {
	pacific := ChannelRequestLimitConfig{Timezone: "America/Los_Angeles"}

	// 冬令时（PST）：UTC 08:00 才是当地午夜
	assert.Equal(t, "2026-01-14", pacific.dailyWindowAt(time.Date(2026, 1, 15, 7, 59, 0, 0, time.UTC)))
	assert.Equal(t, "2026-01-15", pacific.dailyWindowAt(time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)))
	// 夏令时（PDT）：UTC 07:00 即当地午夜
	assert.Equal(t, "2026-06-30", pacific.dailyWindowAt(time.Date(2026, 7, 1, 6, 59, 0, 0, time.UTC)))
	assert.Equal(t, "2026-07-01", pacific.dailyWindowAt(time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC)))

	summerMidnightPacific := time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC)
	cst := 8 * 60
	assert.Equal(t, "2026-07-01", ChannelRequestLimitConfig{Timezone: "America/Los_Angeles", UTCOffsetMinutes: &cst}.dailyWindowAt(summerMidnightPacific), "timezone must take precedence over fixed offset")
	assert.Equal(t, "2026-07-01", ChannelRequestLimitConfig{Timezone: "Mars/Olympus", UTCOffsetMinutes: &cst}.dailyWindowAt(summerMidnightPacific), "unknown timezone must fall back to fixed offset")
	assert.Equal(t, summerMidnightPacific.Local().Format("2006-01-02"), ChannelRequestLimitConfig{Timezone: "Mars/Olympus"}.dailyWindowAt(summerMidnightPacific), "unknown timezone without offset must fall back to server local time")
}

// GetRequestLimitConfig 必须无 GetSetting 的错误回写副作用，且容忍空/损坏配置。
func TestChannelGetRequestLimitConfig(t *testing.T) {
	channel := &Channel{Id: 910010}
	assert.False(t, channel.GetRequestLimitConfig().Enabled(), "nil setting means unlimited")

	setting := `{"daily_request_limit": 700, "daily_request_limit_utc_offset": 480}`
	channel.Setting = &setting
	config := channel.GetRequestLimitConfig()
	assert.EqualValues(t, 700, config.DailyLimit)
	require.NotNil(t, config.UTCOffsetMinutes)
	assert.Equal(t, 480, *config.UTCOffsetMinutes)
	assert.Empty(t, config.Timezone)

	withTimezone := `{"daily_request_limit": 700, "daily_request_limit_timezone": " America/Los_Angeles "}`
	channel.Setting = &withTimezone
	assert.Equal(t, "America/Los_Angeles", channel.GetRequestLimitConfig().Timezone, "timezone must be trimmed")

	rateLimited := `{"rate_limit_period_minutes": 5, "rate_limit_max_requests": 60, "rate_limit_max_success": 30}`
	channel.Setting = &rateLimited
	config = channel.GetRequestLimitConfig()
	assert.True(t, config.Enabled(), "rate limit alone enables the config")
	assert.Equal(t, 5*time.Minute, config.Period())
	assert.EqualValues(t, 60, config.MaxRequests)
	assert.EqualValues(t, 30, config.MaxSuccess)
	assert.EqualValues(t, 0, config.DailyLimit)

	successOnly := `{"rate_limit_max_success": 30}`
	channel.Setting = &successOnly
	assert.True(t, channel.GetRequestLimitConfig().Enabled(), "success limit alone enables the config")

	noOffset := `{"daily_request_limit": 700}`
	channel.Setting = &noOffset
	assert.Nil(t, channel.GetRequestLimitConfig().UTCOffsetMinutes, "absent offset must stay nil (server local time)")

	broken := `{invalid json`
	channel.Setting = &broken
	assert.False(t, channel.GetRequestLimitConfig().Enabled(), "broken setting must fail open")
	assert.Equal(t, broken, *channel.Setting, "must not mutate the stored setting")
}
