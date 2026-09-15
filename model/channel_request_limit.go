package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/go-redis/redis/v8"
)

// 渠道请求计数与上限，支撑渠道级"每日请求上限"与"请求频率限制"（每周期最多请求/成功次数）。
// 计数口径：每次确定由该渠道承接一次上游调用（含失败后的重试、同渠道重试与渠道测试）
// 计一次请求，上游成功返回后再计一次成功，与上游按请求扣减配额的口径一致。
// 每日计数的日切按渠道配置的时区计算：优先 IANA 时区（DailyRequestLimitTimezone，自动处理
// 夏令时，例如 Google AI Studio 的 RPD 按太平洋时间午夜重置），其次固定 UTC 偏移
// （DailyRequestLimitUTCOffset），都未配置时跟随服务器本地时区。
// 频率限制按自然周期计数：周期为 N 分钟时，以 unix 时间除以 N 分钟得到的窗口序号为准，
// 窗口切换即清零；它与用户/分组级的模型请求限速相互独立。
// Redis 可用时以 Redis 为准（多实例一致、重启不丢），本地仅作短 TTL 缓存；
// 否则退化为进程内计数，重启后清零（宁可放行也不误杀）。

const (
	channelDailyCountKeyPrefix    = "channel_daily_req"
	channelPeriodCountKeyPrefix   = "channel_period_req"
	channelPeriodSuccessKeyPrefix = "channel_period_ok"
	// channelCountSyncInterval 是 Redis 计数的本地缓存时长。读取超过该间隔才回源 Redis，
	// 因此多实例部署下达到上限后最多再放行该窗口内的少量请求。
	channelCountSyncInterval = 3 * time.Second
	// 每日计数 key 按日期区分，只需存活到当天结束，48 小时留足余量后自动过期。
	channelDailyCountExpiration = 48 * time.Hour
	// UTC 偏移的合法范围与现实时区一致：UTC-12 ~ UTC+14。
	minDailyLimitUTCOffsetMinutes = -12 * 60
	maxDailyLimitUTCOffsetMinutes = 14 * 60
)

// ChannelRequestLimitConfig 渠道请求上限配置（每日 + 频率），从渠道 setting 解析。
type ChannelRequestLimitConfig struct {
	// DailyLimit 每日请求次数上限，0 表示不限制。
	DailyLimit int64
	// Timezone 日切的 IANA 时区名（如 "America/Los_Angeles"），非空且可加载时优先于 UTCOffsetMinutes。
	Timezone string
	// UTCOffsetMinutes 日切时区（相对 UTC 的分钟偏移）；nil 表示跟随服务器本地时区。
	UTCOffsetMinutes *int
	// PeriodMinutes 频率限制的周期（分钟），<= 0 视为 1 分钟。
	PeriodMinutes int
	// MaxRequests 每周期最多请求次数（含失败），0 表示不限制。
	MaxRequests int64
	// MaxSuccess 每周期最多成功次数，0 表示不限制。
	MaxSuccess int64
}

// NewChannelRequestLimitConfig 从渠道设置构造请求上限配置，计数与判定路径统一由此取窗口与上限。
func NewChannelRequestLimitConfig(setting dto.ChannelSettings) ChannelRequestLimitConfig {
	return ChannelRequestLimitConfig{
		DailyLimit:       setting.DailyRequestLimit,
		Timezone:         strings.TrimSpace(setting.DailyRequestLimitTimezone),
		UTCOffsetMinutes: setting.DailyRequestLimitUTCOffset,
		PeriodMinutes:    setting.RateLimitPeriodMinutes,
		MaxRequests:      setting.RateLimitMaxRequests,
		MaxSuccess:       setting.RateLimitMaxSuccess,
	}
}

// Enabled 是否有任一上限生效；只有生效的配置才会进入选路过滤缓存。
func (config ChannelRequestLimitConfig) Enabled() bool {
	return config.DailyLimit > 0 || config.MaxRequests > 0 || config.MaxSuccess > 0
}

// Period 返回频率限制的周期，未配置时为 1 分钟。
func (config ChannelRequestLimitConfig) Period() time.Duration {
	minutes := config.PeriodMinutes
	if minutes <= 0 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

// location 解析日切时区：IANA 时区 > 固定 UTC 偏移 > 服务器本地时区。
// 无法加载的时区名与越界的偏移都退回下一级，保证计数永远可用。
func (config ChannelRequestLimitConfig) location() *time.Location {
	if config.Timezone != "" {
		if loc, err := loadChannelDailyLimitLocation(config.Timezone); err == nil {
			return loc
		}
	}
	if config.UTCOffsetMinutes != nil &&
		*config.UTCOffsetMinutes >= minDailyLimitUTCOffsetMinutes &&
		*config.UTCOffsetMinutes <= maxDailyLimitUTCOffsetMinutes {
		return time.FixedZone("", *config.UTCOffsetMinutes*60)
	}
	return time.Local
}

// dailyWindowAt 计算 now 在渠道日切时区下的日期窗口。
func (config ChannelRequestLimitConfig) dailyWindowAt(now time.Time) string {
	return now.In(config.location()).Format("2006-01-02")
}

// periodWindowAt 计算 now 所在的自然周期窗口序号，周期切换即换窗口。
func (config ChannelRequestLimitConfig) periodWindowAt(now time.Time) string {
	return strconv.FormatInt(now.Unix()/int64(config.Period().Seconds()), 10)
}

// channelDailyLocationCache 缓存已加载的 IANA 时区：time.LoadLocation 每次都会读取时区数据库，
// 而日切计算位于选路热路径。
var channelDailyLocationCache sync.Map // 时区名 -> *time.Location

func loadChannelDailyLimitLocation(name string) (*time.Location, error) {
	if cached, ok := channelDailyLocationCache.Load(name); ok {
		return cached.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	channelDailyLocationCache.Store(name, loc)
	return loc, nil
}

// channelWindowCounter 是某个渠道在某类窗口（每日 / 周期请求 / 周期成功）内的本地计数。
type channelWindowCounter struct {
	mu       sync.Mutex
	window   string
	count    int64
	syncedAt time.Time // 上次与 Redis 同步的时间，仅 Redis 模式使用
}

// rollover 在窗口变更时清零计数；调用方必须持有 mu。
func (counter *channelWindowCounter) rollover(window string) {
	if counter.window != window {
		counter.window = window
		counter.count = 0
		counter.syncedAt = time.Time{}
	}
}

var channelWindowCounters sync.Map // "prefix:channelId" -> *channelWindowCounter

func getChannelWindowCounter(prefix string, channelId int) *channelWindowCounter {
	key := prefix + ":" + strconv.Itoa(channelId)
	if v, ok := channelWindowCounters.Load(key); ok {
		return v.(*channelWindowCounter)
	}
	v, _ := channelWindowCounters.LoadOrStore(key, &channelWindowCounter{})
	return v.(*channelWindowCounter)
}

func channelWindowRedisKey(prefix string, window string, channelId int) string {
	return fmt.Sprintf("%s:%s:%d", prefix, window, channelId)
}

func channelCountRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

// incrChannelWindowCount 递增渠道在指定窗口内的计数；expiration 决定 Redis key 的存活时间。
func incrChannelWindowCount(prefix string, channelId int, window string, expiration time.Duration) {
	counter := getChannelWindowCounter(prefix, channelId)
	if !channelCountRedisAvailable() {
		counter.mu.Lock()
		counter.rollover(window)
		counter.count++
		counter.mu.Unlock()
		return
	}

	ctx := context.Background()
	key := channelWindowRedisKey(prefix, window, channelId)
	val, err := common.RDB.Incr(ctx, key).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to incr channel request count: key=%s, error=%v", key, err))
		// Redis 故障时退化为本地计数，保证上限仍近似生效
		counter.mu.Lock()
		counter.rollover(window)
		counter.count++
		counter.mu.Unlock()
		return
	}
	if val == 1 {
		common.RDB.Expire(ctx, key, expiration)
	}
	counter.mu.Lock()
	counter.rollover(window)
	// 并发下 INCR 返回可能乱序到达，取最大值保证计数单调
	if val > counter.count {
		counter.count = val
	}
	counter.syncedAt = time.Now()
	counter.mu.Unlock()
}

// getChannelWindowCount 返回渠道在指定窗口内的计数。
func getChannelWindowCount(prefix string, channelId int, window string) int64 {
	counter := getChannelWindowCounter(prefix, channelId)
	counter.mu.Lock()
	counter.rollover(window)
	count := counter.count
	syncedAt := counter.syncedAt
	counter.mu.Unlock()

	if !channelCountRedisAvailable() {
		return count
	}
	if time.Since(syncedAt) < channelCountSyncInterval {
		return count
	}

	val, err := common.RDB.Get(context.Background(), channelWindowRedisKey(prefix, window, channelId)).Int64()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			// Redis 故障时沿用本地计数
			return count
		}
		val = 0
	}
	counter.mu.Lock()
	counter.rollover(window)
	if val > counter.count {
		counter.count = val
	}
	// key 不存在（本窗口尚无计数）同样刷新同步时间，避免每个请求都回源 Redis
	counter.syncedAt = time.Now()
	count = counter.count
	counter.mu.Unlock()
	return count
}

// IncrChannelRequestCount 在渠道被确定承接一次上游调用时递增当日计数与当前周期的请求计数。
// 当日计数始终记录（管理端展示"今日已用"），周期计数只在配置了请求上限时记录。
func IncrChannelRequestCount(channelId int, config ChannelRequestLimitConfig) {
	now := time.Now()
	incrChannelWindowCount(channelDailyCountKeyPrefix, channelId, config.dailyWindowAt(now), channelDailyCountExpiration)
	if config.MaxRequests > 0 {
		incrChannelWindowCount(channelPeriodCountKeyPrefix, channelId, config.periodWindowAt(now), 2*config.Period())
	}
}

// IncrChannelSuccessCount 在渠道成功完成一次上游调用后递增当前周期的成功计数。
func IncrChannelSuccessCount(channelId int, config ChannelRequestLimitConfig) {
	if config.MaxSuccess <= 0 {
		return
	}
	incrChannelWindowCount(channelPeriodSuccessKeyPrefix, channelId, config.periodWindowAt(time.Now()), 2*config.Period())
}

// GetChannelDailyRequestCount 返回渠道当日（按渠道日切时区）已承接的请求次数。
func GetChannelDailyRequestCount(channelId int, config ChannelRequestLimitConfig) int64 {
	return getChannelWindowCount(channelDailyCountKeyPrefix, channelId, config.dailyWindowAt(time.Now()))
}

// GetChannelPeriodRequestCount 返回渠道在当前周期内已承接的请求次数。
func GetChannelPeriodRequestCount(channelId int, config ChannelRequestLimitConfig) int64 {
	return getChannelWindowCount(channelPeriodCountKeyPrefix, channelId, config.periodWindowAt(time.Now()))
}

// GetChannelPeriodSuccessCount 返回渠道在当前周期内成功完成的请求次数。
func GetChannelPeriodSuccessCount(channelId int, config ChannelRequestLimitConfig) int64 {
	return getChannelWindowCount(channelPeriodSuccessKeyPrefix, channelId, config.periodWindowAt(time.Now()))
}

// IsChannelRequestLimitReached 判断渠道是否已达任一请求上限；为 0 的上限表示不限制。
func IsChannelRequestLimitReached(channelId int, config ChannelRequestLimitConfig) bool {
	if config.DailyLimit > 0 && GetChannelDailyRequestCount(channelId, config) >= config.DailyLimit {
		return true
	}
	if config.MaxRequests > 0 && GetChannelPeriodRequestCount(channelId, config) >= config.MaxRequests {
		return true
	}
	if config.MaxSuccess > 0 && GetChannelPeriodSuccessCount(channelId, config) >= config.MaxSuccess {
		return true
	}
	return false
}

// GetRequestLimitConfig 返回渠道的请求上限配置。
// 与 GetSetting 不同，解析失败时不回写数据库，可安全用于选路热路径
// 以及只加载了部分列的渠道对象。
func (channel *Channel) GetRequestLimitConfig() ChannelRequestLimitConfig {
	if channel.Setting == nil || *channel.Setting == "" {
		return ChannelRequestLimitConfig{}
	}
	setting := dto.ChannelSettings{}
	if err := common.Unmarshal([]byte(*channel.Setting), &setting); err != nil {
		return ChannelRequestLimitConfig{}
	}
	return NewChannelRequestLimitConfig(setting)
}
