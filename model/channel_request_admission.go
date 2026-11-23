package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

var ErrChannelRequestLimited = errors.New("候选渠道已达请求上限")

// BeginChannelAttempt admits one attempt, not a routing candidate. Finish must
// be called even on failure or panic; only successful attempts consume the
// reserved daily success slot. A request crossing midnight owns its start day.
func BeginChannelAttempt(ctx context.Context, id int, config ChannelRequestLimitConfig) (finish func(bool), err error) {
	finish, _, err = BeginChannelAttemptWithEstimate(ctx, id, config, 0)
	return finish, err
}

// BeginChannelAttemptWithEstimate admits one attempt while pre-checking and
// reserving estimatedTokens against MaxTokens in the start period window.
// Call settleTokens with actual consumed tokens on billing settlement to
// atomically release the reservation and credit the start period; finish
// always releases any unsettled reservation on failure, panic, or zero usage.
func BeginChannelAttemptWithEstimate(ctx context.Context, id int, config ChannelRequestLimitConfig, estimatedTokens int64) (finish func(bool), settleTokens func(int64), err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if id <= 0 {
		return func(bool) {}, func(int64) {}, nil
	}
	if !config.Enabled() {
		IncrChannelRequestCount(id, config)
		return func(bool) {}, func(int64) {}, nil
	}
	if estimatedTokens < 0 || config.MaxTokens <= 0 {
		estimatedTokens = 0
	}
	if channelCountRedisAvailable() {
		return beginRedisChannelAttempt(ctx, common.RDB, id, config, estimatedTokens, time.Now())
	}
	return beginMemoryChannelAttempt(id, config, estimatedTokens, time.Now())
}

type channelActiveAttempts struct {
	mu           sync.Mutex
	active       int64
	daily        map[string]int64
	periodTokens map[string]int64
}

var activeChannelAttempts sync.Map // channel id -> *channelActiveAttempts

func getChannelActiveAttempts(id int) *channelActiveAttempts {
	if state, ok := activeChannelAttempts.Load(id); ok {
		return state.(*channelActiveAttempts)
	}
	state, _ := activeChannelAttempts.LoadOrStore(id, &channelActiveAttempts{})
	return state.(*channelActiveAttempts)
}

func getMemoryChannelPendingTokens(id int, period string) int64 {
	v, ok := activeChannelAttempts.Load(id)
	if !ok {
		return 0
	}
	state := v.(*channelActiveAttempts)
	state.mu.Lock()
	pending := state.periodTokens[period]
	state.mu.Unlock()
	return pending
}

func beginMemoryChannelAttempt(id int, config ChannelRequestLimitConfig, estimatedTokens int64, now time.Time) (func(bool), func(int64), error) {
	state := getChannelActiveAttempts(id)
	state.mu.Lock()
	defer state.mu.Unlock()
	day := config.dailyWindowAt(now)
	period := config.periodWindowAt(now)
	if (config.ConcurrencyLimit > 0 && state.active >= config.ConcurrencyLimit) ||
		config.countLimitReachedAt(id, now, false) {
		return nil, nil, ErrChannelRequestLimited
	}
	if config.MaxTokens > 0 {
		used := getChannelWindowCount(channelPeriodTokenKeyPrefix, id, period)
		pending := state.periodTokens[period]
		if used+pending >= config.MaxTokens || (estimatedTokens > 0 && used+pending+estimatedTokens > config.MaxTokens) {
			return nil, nil, ErrChannelRequestLimited
		}
	}
	if config.DailySuccessLimit > 0 {
		used := getChannelWindowCount(channelDailySuccessKeyPrefix, id, day)
		if state.daily[day] >= config.DailySuccessLimit-used {
			return nil, nil, ErrChannelRequestLimited
		}
		if state.daily == nil {
			state.daily = make(map[string]int64)
		}
		state.daily[day]++
	}
	if config.MaxTokens > 0 && estimatedTokens > 0 {
		if state.periodTokens == nil {
			state.periodTokens = make(map[string]int64)
		}
		state.periodTokens[period] += estimatedTokens
	}
	if config.ConcurrencyLimit > 0 {
		state.active++
	}
	incrChannelRequestCountAt(id, config, now)

	var tokenOnce sync.Once
	settleTokens := func(actualTokens int64) {
		tokenOnce.Do(func() {
			state.mu.Lock()
			defer state.mu.Unlock()
			if config.MaxTokens > 0 && estimatedTokens > 0 {
				state.periodTokens[period] -= estimatedTokens
				if state.periodTokens[period] <= 0 {
					delete(state.periodTokens, period)
				}
			}
			if config.MaxTokens > 0 && actualTokens > 0 {
				incrChannelTokenCountInPeriod(id, config, period, actualTokens)
			}
		})
	}

	var once sync.Once
	finish := func(success bool) {
		once.Do(func() {
			settleTokens(0)
			state.mu.Lock()
			defer state.mu.Unlock()
			if config.ConcurrencyLimit > 0 {
				state.active--
			}
			if config.DailySuccessLimit > 0 {
				state.daily[day]--
				if state.daily[day] == 0 {
					delete(state.daily, day)
				}
				if success && config.dailyWindowAt(time.Now()) == day {
					incrChannelWindowCount(channelDailySuccessKeyPrefix, id, day, channelDailyCountExpiration)
				}
			}
			if success {
				IncrChannelSuccessCount(id, config)
			}
		})
	}
	return finish, settleTokens, nil
}

const (
	channelAttemptLease        = 60 * time.Second
	channelAttemptHeartbeat    = channelAttemptLease / 3
	channelAttemptRedisTimeout = 2 * time.Second
)

// Use the original daily/period keys. Only in-flight slots need new keys;
// expiring leases prevent a crashed instance from holding them indefinitely.
var acquireChannelAttemptScript = redis.NewScript(`
redis.replicate_commands()
local member, lease = ARGV[1], tonumber(ARGV[2])
local dailyLimit, periodLimit, periodSuccess, dailySuccess, concurrency, periodTokens, estTokens =
  tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7]), tonumber(ARGV[10]), tonumber(ARGV[11])
local function count(key)
  return tonumber(redis.call('GET', key) or '0')
end
if (dailyLimit > 0 and count(KEYS[1]) >= dailyLimit) or
   (periodLimit > 0 and count(KEYS[2]) >= periodLimit) or
   (periodSuccess > 0 and count(KEYS[3]) >= periodSuccess) then
  return 0
end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local function active(key)
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
  return redis.call('ZCARD', key)
end
local function pendingTokens(key)
  if key == '' then return 0 end
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
  local members = redis.call('ZRANGE', key, 0, -1)
  local sum = 0
  for _, m in ipairs(members) do
    local sep = string.find(m, ':', 1, true)
    if sep then
      sum = sum + (tonumber(string.sub(m, sep + 1)) or 0)
    end
  end
  return sum
end
if (dailySuccess > 0 and count(KEYS[4]) + active(KEYS[5]) >= dailySuccess) or
   (concurrency > 0 and active(KEYS[6]) >= concurrency) then
  return 0
end
if periodTokens > 0 then
  local usedTokens = count(KEYS[7]) + pendingTokens(KEYS[8])
  if usedTokens >= periodTokens or (estTokens > 0 and usedTokens + estTokens > periodTokens) then
    return 0
  end
end
local function reserve(key)
  redis.call('ZADD', key, now + lease, member)
  redis.call('PEXPIRE', key, lease * 2)
end
local function increment(key, ttl)
  if redis.call('INCR', key) == 1 then
    redis.call('EXPIRE', key, ttl)
  end
end
increment(KEYS[1], ARGV[8])
if periodLimit > 0 then increment(KEYS[2], ARGV[9]) end
if dailySuccess > 0 then reserve(KEYS[5]) end
if concurrency > 0 then reserve(KEYS[6]) end
if periodTokens > 0 and estTokens > 0 then reserve(KEYS[8]) end
return 1
`)

var refreshChannelAttemptScript = redis.NewScript(`
redis.replicate_commands()
local member, lease = ARGV[1], tonumber(ARGV[2])
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
for _, key in ipairs(KEYS) do
  local expiry = tonumber(redis.call('ZSCORE', key, member))
  if expiry and expiry > now then
    redis.call('ZADD', key, now + lease, member)
    redis.call('PEXPIRE', key, lease * 2)
  end
end
`)

var finishChannelAttemptScript = redis.NewScript(`
local member, success, ttl = ARGV[1], ARGV[2] == '1', ARGV[3]
if KEYS[2] ~= '' then
  if redis.call('ZREM', KEYS[2], member) == 1 and success then
    if redis.call('INCR', KEYS[1]) == 1 then redis.call('EXPIRE', KEYS[1], ttl) end
  end
end
if KEYS[3] ~= '' then redis.call('ZREM', KEYS[3], member) end
return 1
`)

var settleChannelTokensScript = redis.NewScript(`
local member, actual, ttl = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
if KEYS[2] ~= '' then
  redis.call('ZREM', KEYS[2], member)
end
if actual > 0 and KEYS[1] ~= '' then
  local val = redis.call('INCRBY', KEYS[1], actual)
  if redis.call('TTL', KEYS[1]) < 0 then
    redis.call('EXPIRE', KEYS[1], ttl)
  end
  return val
end
return -1
`)

func beginRedisChannelAttempt(ctx context.Context, rdb *redis.Client, id int, config ChannelRequestLimitConfig, estimatedTokens int64, now time.Time) (func(bool), func(int64), error) {
	day, period := config.dailyWindowAt(now), config.periodWindowAt(now)
	dailySuccessKey := channelWindowRedisKey(channelDailySuccessKeyPrefix, day, id)
	periodTokenKey := channelWindowRedisKey(channelPeriodTokenKeyPrefix, period, id)
	var pendingKey, concurrencyKey, tokenPendingKey string
	var leaseKeys []string
	if config.DailySuccessLimit > 0 {
		pendingKey = channelWindowRedisKey("channel_daily_pending", day, id)
		leaseKeys = append(leaseKeys, pendingKey)
	}
	if config.ConcurrencyLimit > 0 {
		concurrencyKey = fmt.Sprintf("channel_inflight:%d", id)
		leaseKeys = append(leaseKeys, concurrencyKey)
	}
	if config.MaxTokens > 0 {
		tokenPendingKey = channelWindowRedisKey(channelPeriodTokenPendingKeyPrefix, period, id)
		if estimatedTokens > 0 {
			leaseKeys = append(leaseKeys, tokenPendingKey)
		}
	}
	keys := []string{
		channelWindowRedisKey(channelDailyCountKeyPrefix, day, id),
		channelWindowRedisKey(channelPeriodCountKeyPrefix, period, id),
		channelWindowRedisKey(channelPeriodSuccessKeyPrefix, period, id),
		dailySuccessKey, pendingKey, concurrencyKey,
		periodTokenKey, tokenPendingKey,
	}
	member := fmt.Sprintf("%s:%d", common.GetUUID(), estimatedTokens)
	periodTTLSeconds := max(int64(2*config.Period()/time.Second), 1)
	acquireCtx, cancel := context.WithTimeout(ctx, channelAttemptRedisTimeout)
	allowed, err := acquireChannelAttemptScript.Run(acquireCtx, rdb, keys, member, channelAttemptLease.Milliseconds(),
		config.DailyLimit, config.MaxRequests, config.MaxSuccess, config.DailySuccessLimit, config.ConcurrencyLimit,
		int64(channelDailyCountExpiration/time.Second), periodTTLSeconds, config.MaxTokens, estimatedTokens).Int()
	cancel()
	if err != nil {
		return nil, nil, fmt.Errorf("channel %d admission: %w", id, err)
	}
	if allowed == 0 {
		return nil, nil, ErrChannelRequestLimited
	}

	stop := func() {}
	if len(leaseKeys) != 0 {
		heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
		stop = cancelHeartbeat
		go func() {
			ticker := time.NewTicker(channelAttemptHeartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatCtx.Done():
					return
				case <-ticker.C:
					refreshCtx, done := context.WithTimeout(heartbeatCtx, channelAttemptRedisTimeout)
					err := refreshChannelAttemptScript.Run(refreshCtx, rdb, leaseKeys, member, channelAttemptLease.Milliseconds()).Err()
					done()
					if err != nil && heartbeatCtx.Err() == nil {
						common.SysError(fmt.Sprintf("channel %d lease refresh: %v", id, err))
					}
				}
			}
		}()
	}

	var tokenOnce sync.Once
	settleTokens := func(actualTokens int64) {
		tokenOnce.Do(func() {
			if config.MaxTokens <= 0 || (estimatedTokens <= 0 && actualTokens <= 0) {
				return
			}
			pendingArgKey := ""
			if estimatedTokens > 0 {
				pendingArgKey = tokenPendingKey
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), channelAttemptRedisTimeout)
			val, err := settleChannelTokensScript.Run(cleanupCtx, rdb, []string{periodTokenKey, pendingArgKey},
				member, actualTokens, periodTTLSeconds).Int64()
			cancel()
			if err != nil {
				common.SysError(fmt.Sprintf("channel %d token settle: %v", id, err))
				return
			}
			syncChannelWindowCountCache(channelPeriodTokenKeyPrefix, id, period, val)
		})
	}

	var once sync.Once
	finish := func(success bool) {
		once.Do(func() {
			stop()
			settleTokens(0)
			if pendingKey != "" || concurrencyKey != "" {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), channelAttemptRedisTimeout)
				_, err := finishChannelAttemptScript.Run(cleanupCtx, rdb, []string{dailySuccessKey, pendingKey, concurrencyKey},
					member, success, int64(channelDailyCountExpiration/time.Second)).Result()
				cancel()
				if err != nil {
					common.SysError(fmt.Sprintf("channel %d lease release: %v", id, err))
				}
			}
			if success {
				IncrChannelSuccessCount(id, config)
			}
		})
	}
	return finish, settleTokens, nil
}
