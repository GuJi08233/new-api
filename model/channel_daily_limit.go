package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/go-redis/redis/v8"
)

// 渠道每日请求计数，支撑渠道级"每日请求上限"（dto.ChannelSettings.DailyRequestLimit）。
// 计数口径：每次确定由该渠道承接一次上游调用（含失败后的重试、同渠道重试与渠道测试）
// 各计一次，与上游按请求扣减免费额度的口径一致。
// 计数的"日切"按渠道配置的时区（DailyRequestLimitUTCOffset）计算，用于对齐
// 上游免费额度的重置时间；未配置时跟随服务器本地时区。
// Redis 可用时以 Redis 为准（多实例一致、重启不丢），本地仅作短 TTL 缓存；
// 否则退化为进程内计数，重启后当日计数清零（宁可放行也不误杀）。

const (
	channelDailyCountKeyPrefix = "channel_daily_req"
	// channelDailyCountSyncInterval 是 Redis 计数的本地缓存时长。读取超过该间隔才
	// 回源 Redis，因此多实例部署下达到上限后最多再放行该窗口内的少量请求。
	channelDailyCountSyncInterval = 3 * time.Second
	// 计数 key 按日期区分，只需存活到当天结束，48 小时留足余量后自动过期。
	channelDailyCountExpiration = 48 * time.Hour
	// UTC 偏移的合法范围与现实时区一致：UTC-12 ~ UTC+14。
	minDailyLimitUTCOffsetMinutes = -12 * 60
	maxDailyLimitUTCOffsetMinutes = 14 * 60
)

// ChannelDailyLimitConfig 渠道每日请求上限配置，从渠道 setting 解析。
type ChannelDailyLimitConfig struct {
	Limit int64
	// UTCOffsetMinutes 日切时区（相对 UTC 的分钟偏移）；nil 表示跟随服务器本地时区。
	UTCOffsetMinutes *int
}

func (config ChannelDailyLimitConfig) Enabled() bool {
	return config.Limit > 0
}

type channelDailyCounter struct {
	mu       sync.Mutex
	date     string
	count    int64
	syncedAt time.Time // 上次与 Redis 同步的时间，仅 Redis 模式使用
}

// rollover 在渠道时区的自然日变更时清零计数；调用方必须持有 mu。
func (counter *channelDailyCounter) rollover(date string) {
	if counter.date != date {
		counter.date = date
		counter.count = 0
		counter.syncedAt = time.Time{}
	}
}

var channelDailyCounters sync.Map // channelId -> *channelDailyCounter

func getChannelDailyCounter(channelId int) *channelDailyCounter {
	if v, ok := channelDailyCounters.Load(channelId); ok {
		return v.(*channelDailyCounter)
	}
	v, _ := channelDailyCounters.LoadOrStore(channelId, &channelDailyCounter{})
	return v.(*channelDailyCounter)
}

// channelDailyDateAt 计算 now 在指定日切时区下的日期字符串。
// 越界或未设置的偏移视为跟随服务器本地时区。
func channelDailyDateAt(now time.Time, utcOffsetMinutes *int) string {
	if utcOffsetMinutes != nil &&
		*utcOffsetMinutes >= minDailyLimitUTCOffsetMinutes &&
		*utcOffsetMinutes <= maxDailyLimitUTCOffsetMinutes {
		return now.In(time.FixedZone("", *utcOffsetMinutes*60)).Format("2006-01-02")
	}
	return now.Local().Format("2006-01-02")
}

func channelDailyDate(utcOffsetMinutes *int) string {
	return channelDailyDateAt(time.Now(), utcOffsetMinutes)
}

func channelDailyRedisKey(date string, channelId int) string {
	return fmt.Sprintf("%s:%s:%d", channelDailyCountKeyPrefix, date, channelId)
}

func channelDailyRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

// IncrChannelDailyRequestCount 在渠道被确定承接一次上游调用时递增当日计数。
func IncrChannelDailyRequestCount(channelId int, utcOffsetMinutes *int) {
	date := channelDailyDate(utcOffsetMinutes)
	counter := getChannelDailyCounter(channelId)
	if !channelDailyRedisAvailable() {
		counter.mu.Lock()
		counter.rollover(date)
		counter.count++
		counter.mu.Unlock()
		return
	}

	ctx := context.Background()
	key := channelDailyRedisKey(date, channelId)
	val, err := common.RDB.Incr(ctx, key).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to incr channel daily request count: channel_id=%d, error=%v", channelId, err))
		// Redis 故障时退化为本地计数，保证上限仍近似生效
		counter.mu.Lock()
		counter.rollover(date)
		counter.count++
		counter.mu.Unlock()
		return
	}
	if val == 1 {
		common.RDB.Expire(ctx, key, channelDailyCountExpiration)
	}
	counter.mu.Lock()
	counter.rollover(date)
	// 并发下 INCR 返回可能乱序到达，取最大值保证计数单调
	if val > counter.count {
		counter.count = val
	}
	counter.syncedAt = time.Now()
	counter.mu.Unlock()
}

// GetChannelDailyRequestCount 返回渠道当日（按渠道日切时区）已承接的请求次数。
func GetChannelDailyRequestCount(channelId int, utcOffsetMinutes *int) int64 {
	date := channelDailyDate(utcOffsetMinutes)
	counter := getChannelDailyCounter(channelId)
	counter.mu.Lock()
	counter.rollover(date)
	count := counter.count
	syncedAt := counter.syncedAt
	counter.mu.Unlock()

	if !channelDailyRedisAvailable() {
		return count
	}
	if time.Since(syncedAt) < channelDailyCountSyncInterval {
		return count
	}

	val, err := common.RDB.Get(context.Background(), channelDailyRedisKey(date, channelId)).Int64()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			// Redis 故障时沿用本地计数
			return count
		}
		val = 0
	}
	counter.mu.Lock()
	counter.rollover(date)
	if val > counter.count {
		counter.count = val
	}
	// key 不存在（今日尚无计数）同样刷新同步时间，避免每个请求都回源 Redis
	counter.syncedAt = time.Now()
	count = counter.count
	counter.mu.Unlock()
	return count
}

// IsChannelDailyLimitReached 判断渠道是否已达每日请求上限；Limit <= 0 表示不限制。
func IsChannelDailyLimitReached(channelId int, config ChannelDailyLimitConfig) bool {
	if !config.Enabled() {
		return false
	}
	return GetChannelDailyRequestCount(channelId, config.UTCOffsetMinutes) >= config.Limit
}

// GetDailyLimitConfig 返回渠道的每日请求上限配置。
// 与 GetSetting 不同，解析失败时不回写数据库，可安全用于选路热路径
// 以及只加载了部分列的渠道对象。
func (channel *Channel) GetDailyLimitConfig() ChannelDailyLimitConfig {
	if channel.Setting == nil || *channel.Setting == "" {
		return ChannelDailyLimitConfig{}
	}
	setting := dto.ChannelSettings{}
	if err := common.Unmarshal([]byte(*channel.Setting), &setting); err != nil {
		return ChannelDailyLimitConfig{}
	}
	return ChannelDailyLimitConfig{
		Limit:            setting.DailyRequestLimit,
		UTCOffsetMinutes: setting.DailyRequestLimitUTCOffset,
	}
}
