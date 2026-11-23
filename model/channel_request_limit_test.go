package model

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var admissionTestChannelSequence atomic.Int32

func nextAdmissionTestChannelID() int {
	return 950000 + int(admissionTestChannelSequence.Add(1))
}

func TestChannelAttemptAdmission(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			enabled, client := common.RedisEnabled, common.RDB
			t.Cleanup(func() { common.RedisEnabled, common.RDB = enabled, client })
			common.RedisEnabled = backend == "redis"
			if common.RedisEnabled {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = common.RDB.Close() })
			}
			// sync.Map-backed memory counters outlive a subtest, including when
			// the suite is run with -count. Allocate a distinct channel ID.
			id := nextAdmissionTestChannelID()
			ctx := context.Background()

			t.Run("success is reserved and failures release it", func(t *testing.T) {
				config := ChannelRequestLimitConfig{DailySuccessLimit: 1, Timezone: "America/Los_Angeles"}
				finish, err := BeginChannelAttempt(ctx, id, config)
				require.NoError(t, err)
				_, err = BeginChannelAttempt(ctx, id, config)
				require.ErrorIs(t, err, ErrChannelRequestLimited)
				finish(false)
				finish(true) // a deferred release or a second call cannot change the result
				finish, err = BeginChannelAttempt(ctx, id, config)
				require.NoError(t, err)
				finish(true)
				_, err = BeginChannelAttempt(ctx, id, config)
				require.ErrorIs(t, err, ErrChannelRequestLimited)
				assert.EqualValues(t, 1, GetChannelDailySuccessCount(id, config))
				assert.EqualValues(t, 2, GetChannelDailyRequestCount(id, config))
			})

			t.Run("concurrent admission is atomic and cancellation releases", func(t *testing.T) {
				id := nextAdmissionTestChannelID()
				config := ChannelRequestLimitConfig{ConcurrencyLimit: 2}
				start := make(chan struct{})
				results := make(chan struct {
					finish func(bool)
					err    error
				}, 8)
				var calls sync.WaitGroup
				for range cap(results) {
					calls.Go(func() {
						<-start
						finish, err := BeginChannelAttempt(ctx, id, config)
						results <- struct {
							finish func(bool)
							err    error
						}{finish, err}
					})
				}
				close(start)
				calls.Wait()
				close(results)
				var admitted []func(bool)
				for result := range results {
					if errors.Is(result.err, ErrChannelRequestLimited) {
						continue
					}
					require.NoError(t, result.err)
					admitted = append(admitted, result.finish)
				}
				require.Len(t, admitted, 2)
				for _, finish := range admitted {
					finish(false)
				}
				reqCtx, cancel := context.WithCancel(ctx)
				finish, err := BeginChannelAttempt(reqCtx, id, config)
				require.NoError(t, err)
				cancel()
				finish(false)
				finish, err = BeginChannelAttempt(ctx, id, ChannelRequestLimitConfig{ConcurrencyLimit: 1})
				require.NoError(t, err)
				finish(true)
			})
		})
	}
}

func TestRedisChannelAttemptRollover(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	const id = 940002
	config := ChannelRequestLimitConfig{DailySuccessLimit: 1, MaxRequests: 1, Timezone: "America/Los_Angeles"}
	// The day after the spring DST transition starts at 07:00 UTC, not 08:00.
	midnight := time.Date(2026, 3, 9, 7, 0, 0, 0, time.UTC)
	before := midnight.Add(-time.Second)
	finishOld, _, err := beginRedisChannelAttempt(ctx, rdb, id, config, 0, before)
	require.NoError(t, err)
	_, _, err = beginRedisChannelAttempt(ctx, rdb, id, config, 0, before)
	require.ErrorIs(t, err, ErrChannelRequestLimited)
	finishNew, _, err := beginRedisChannelAttempt(ctx, rdb, id, config, 0, midnight)
	require.NoError(t, err)
	finishOld(true)
	finishNew(false)

	finishNew, _, err = beginRedisChannelAttempt(ctx, rdb, id, config, 0, midnight.Add(time.Minute))
	require.NoError(t, err, "the previous day's success must not consume today's quota")
	finishNew(true)
	_, _, err = beginRedisChannelAttempt(ctx, rdb, id, config, 0, midnight.Add(2*time.Minute))
	require.ErrorIs(t, err, ErrChannelRequestLimited, "daily quota must survive an RPM window reset")

	rpm := ChannelRequestLimitConfig{MaxRequests: 1}
	legacyKey := channelWindowRedisKey(channelPeriodCountKeyPrefix, rpm.periodWindowAt(before), id+1)
	require.NoError(t, rdb.Set(ctx, legacyKey, 1, 2*time.Minute).Err())
	_, _, err = beginRedisChannelAttempt(ctx, rdb, id+1, rpm, 0, before)
	require.ErrorIs(t, err, ErrChannelRequestLimited, "RPM must use the existing period counter")
	finishNextMinute, _, err := beginRedisChannelAttempt(ctx, rdb, id+1, rpm, 0, midnight)
	require.NoError(t, err)
	finishNextMinute(false)
}

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

	rateLimited := `{"rate_limit_period_minutes": 5, "rate_limit_max_requests": 60, "rate_limit_max_success": 30, "rate_limit_max_tokens": 10000}`
	channel.Setting = &rateLimited
	config = channel.GetRequestLimitConfig()
	assert.True(t, config.Enabled(), "rate limit alone enables the config")
	assert.Equal(t, 5*time.Minute, config.Period())
	assert.EqualValues(t, 10000, config.MaxTokens)
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

func TestChannelPeriodTokenLimit(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			enabled, client := common.RedisEnabled, common.RDB
			t.Cleanup(func() { common.RedisEnabled, common.RDB = enabled, client })
			common.RedisEnabled = backend == "redis"
			if common.RedisEnabled {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = common.RDB.Close() })
			}

			id := nextAdmissionTestChannelID()
			ctx := context.Background()
			config := ChannelRequestLimitConfig{PeriodMinutes: 1, MaxTokens: 1000}

			finish, err := BeginChannelAttempt(ctx, id, config)
			require.NoError(t, err)
			IncrChannelTokenCount(id, config, 600)
			finish(true)
			assert.EqualValues(t, 600, GetChannelPeriodTokenCount(id, config))
			assert.False(t, IsChannelRequestLimitReached(id, config))

			finish, err = BeginChannelAttempt(ctx, id, config)
			require.NoError(t, err)
			IncrChannelTokenCount(id, config, 500)
			finish(true)
			assert.EqualValues(t, 1100, GetChannelPeriodTokenCount(id, config))
			assert.True(t, IsChannelRequestLimitReached(id, config))

			_, err = BeginChannelAttempt(ctx, id, config)
			require.ErrorIs(t, err, ErrChannelRequestLimited)
		})
	}
}

func TestChannelTokenReservationPrecheckSettlementAndStartWindow(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			enabled, client := common.RedisEnabled, common.RDB
			t.Cleanup(func() { common.RedisEnabled, common.RDB = enabled, client })
			common.RedisEnabled = backend == "redis"
			if common.RedisEnabled {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = common.RDB.Close() })
			}

			ctx := context.Background()
			config := ChannelRequestLimitConfig{PeriodMinutes: 1, MaxTokens: 800_000}

			t.Run("settled plus estimated exceeding limit is rejected before upstream", func(t *testing.T) {
				id := nextAdmissionTestChannelID()
				finish1, settle1, err := BeginChannelAttemptWithEstimate(ctx, id, config, 500_000)
				require.NoError(t, err)
				// Concurrent second large request (500k + 500k > 800k) must be blocked while first is in-flight.
				_, _, err = BeginChannelAttemptWithEstimate(ctx, id, config, 500_000)
				require.ErrorIs(t, err, ErrChannelRequestLimited)

				// Settle request 1 at 510k actual tokens.
				settle1(510_000)
				finish1(true)
				assert.EqualValues(t, 510_000, GetChannelPeriodTokenCount(id, config))

				// Next request estimating 515k: 510k + 515k > 800k -> blocked immediately.
				_, _, err = BeginChannelAttemptWithEstimate(ctx, id, config, 515_000)
				require.ErrorIs(t, err, ErrChannelRequestLimited)

				// Smaller request estimating 290k: 510k + 290k == 800k -> allowed!
				finish2, settle2, err := BeginChannelAttemptWithEstimate(ctx, id, config, 290_000)
				require.NoError(t, err)
				// Failed attempt releases its 290k reservation without adding to settled count.
				finish2(false)
				settle2(999_999) // already finished; must be a no-op
				assert.EqualValues(t, 510_000, GetChannelPeriodTokenCount(id, config))

				// Since reservation was released, another 290k request fits again.
				finish3, settle3, err := BeginChannelAttemptWithEstimate(ctx, id, config, 290_000)
				require.NoError(t, err)
				settle3(290_000)
				finish3(true)
				assert.EqualValues(t, 800_000, GetChannelPeriodTokenCount(id, config))
				assert.True(t, IsChannelRequestLimitReached(id, config))
			})

			t.Run("tokens settle into admission-time window across minute boundary", func(t *testing.T) {
				id := nextAdmissionTestChannelID()
				startWindowTime := time.Date(2026, 6, 1, 12, 0, 50, 0, time.UTC)
				nextWindowTime := time.Date(2026, 6, 1, 12, 1, 34, 0, time.UTC) // 44s later, next minute
				require.NotEqual(t, config.periodWindowAt(startWindowTime), config.periodWindowAt(nextWindowTime))

				var finish func(bool)
				var settle func(int64)
				var err error
				if common.RedisEnabled {
					finish, settle, err = beginRedisChannelAttempt(ctx, common.RDB, id, config, 400_000, startWindowTime)
				} else {
					finish, settle, err = beginMemoryChannelAttempt(id, config, 400_000, startWindowTime)
				}
				require.NoError(t, err)

				// Settle after crossing into the next minute: tokens must land in startWindowTime's period, not nextWindowTime's.
				settle(800_000)
				finish(true)

				startPeriod := config.periodWindowAt(startWindowTime)
				nextPeriod := config.periodWindowAt(nextWindowTime)
				assert.EqualValues(t, 800_000, getChannelWindowCount(channelPeriodTokenKeyPrefix, id, startPeriod))
				assert.EqualValues(t, 0, getChannelWindowCount(channelPeriodTokenKeyPrefix, id, nextPeriod))
			})
		})
	}
}
