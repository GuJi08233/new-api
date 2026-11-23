package common

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// rateLimitRequest 是滑动窗口里一次已接受的请求。
type rateLimitRequest struct {
	next      *rateLimitRequest
	timestamp int64
}

// rateLimitQueue 按从旧到新的顺序保存已接受的请求，按需分配节点，不再为配置上限预分配容量。
type rateLimitQueue struct {
	head   *rateLimitRequest
	tail   *rateLimitRequest
	length int
}

func (q *rateLimitQueue) append(timestamp int64) {
	request := &rateLimitRequest{timestamp: timestamp}
	if q.tail == nil {
		q.head = request
	} else {
		q.tail.next = request
	}
	q.tail = request
	q.length++
}

// removeExpired 释放队首所有已经离开窗口的请求。
func (q *rateLimitQueue) removeExpired(now int64, duration int64) {
	if q.head == nil || now-q.head.timestamp < duration {
		return
	}
	// 请求按时间有序：队尾已过期说明整条链都过期了。
	if now-q.tail.timestamp >= duration {
		q.clear()
		return
	}
	for now-q.head.timestamp >= duration {
		expired := q.head
		q.head = expired.next
		expired.next = nil
		q.length--
	}
}

func (q *rateLimitQueue) clear() {
	q.head = nil
	q.tail = nil
	q.length = 0
}

// rateLimitEntry 既是一个 key 的限流桶，也是 key 级 LRU 链表中的节点。
type rateLimitEntry struct {
	lastActive time.Time
	element    *list.Element
	requests   rateLimitQueue
	key        string
}

// InMemoryRateLimiter 是带空闲 key 淘汰的滑动窗口限流器。
type InMemoryRateLimiter struct {
	store              map[string]*rateLimitEntry
	lru                *list.List
	mutex              sync.Mutex
	expirationDuration time.Duration
}

// Init 只初始化一次；重复调用保持首次的配置不变。
func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store != nil {
		return
	}
	l.store = make(map[string]*rateLimitEntry)
	l.lru = list.New()
	l.expirationDuration = expirationDuration
	if expirationDuration > 0 {
		go l.clearExpiredItems(time.NewTicker(expirationDuration).C)
	}
}

func (l *InMemoryRateLimiter) clearExpiredItems(ticks <-chan time.Time) {
	for now := range ticks {
		l.deleteExpiredEntries(now)
	}
}

// deleteExpiredEntries 只从 LRU 尾部回收空闲超过过期时长的 key，遇到第一个仍活跃的 key 即停止，
// 不再每次全量遍历所有 key。
func (l *InMemoryRateLimiter) deleteExpiredEntries(now time.Time) {
	if l.expirationDuration <= 0 {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for {
		oldest := l.lru.Back()
		if oldest == nil {
			return
		}
		entry := oldest.Value.(*rateLimitEntry)
		if now.Sub(entry.lastActive) < l.expirationDuration {
			return
		}
		delete(l.store, entry.key)
		l.lru.Remove(oldest)
		entry.element = nil
		entry.requests.clear()
	}
}

// Request 的 duration 单位为秒。
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := time.Now()
	entry, ok := l.store[key]
	if ok {
		entry.requests.removeExpired(now.Unix(), duration)
		entry.lastActive = now
		l.lru.MoveToFront(entry.element)
	} else {
		entry = &rateLimitEntry{key: key, lastActive: now}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	}

	allowed := entry.requests.length < maxRequestNum
	if allowed {
		entry.requests.append(now.Unix())
	}
	return allowed
}

const (
	ModelRequestRateLimitTokenMark        = "MRRLT"
	ModelRequestRateLimitTokenPendingMark = "MRRLTP"
)

// ModelRateLimitTokenTarget carries the resolved rate-limit subject, limits,
// start timestamp, and optional reservation settler from middleware/relay to
// post-request token recording.
type ModelRateLimitTokenTarget struct {
	Subject   string
	Duration  int64
	MaxTokens int64
	StartUnix int64
	Settle    func(actualTokens int64)
}

type tokenRateLimitNode struct {
	next      *tokenRateLimitNode
	timestamp int64
	tokens    int64
}

type tokenRateLimitQueue struct {
	head        *tokenRateLimitNode
	tail        *tokenRateLimitNode
	totalTokens int64
}

func (q *tokenRateLimitQueue) append(timestamp, tokens int64) {
	node := &tokenRateLimitNode{timestamp: timestamp, tokens: tokens}
	if q.tail == nil {
		q.head = node
		q.tail = node
	} else if timestamp >= q.tail.timestamp {
		q.tail.next = node
		q.tail = node
	} else if timestamp < q.head.timestamp {
		node.next = q.head
		q.head = node
	} else {
		curr := q.head
		for curr.next != nil && curr.next.timestamp <= timestamp {
			curr = curr.next
		}
		node.next = curr.next
		curr.next = node
	}
	q.totalTokens += tokens
}

func (q *tokenRateLimitQueue) removeExpired(now, duration int64) {
	if q.head == nil || now-q.head.timestamp < duration {
		return
	}
	if now-q.tail.timestamp >= duration {
		q.clear()
		return
	}
	for q.head != nil && now-q.head.timestamp >= duration {
		expired := q.head
		q.head = expired.next
		expired.next = nil
		q.totalTokens -= expired.tokens
	}
	if q.head == nil {
		q.tail = nil
		q.totalTokens = 0
	} else if q.totalTokens < 0 {
		q.totalTokens = 0
	}
}

func (q *tokenRateLimitQueue) clear() {
	q.head = nil
	q.tail = nil
	q.totalTokens = 0
}

type tokenRateLimitEntry struct {
	lastActive time.Time
	element    *list.Element
	usage      tokenRateLimitQueue
	pending    int64
	key        string
}

// InMemoryTokenRateLimiter tracks sliding-window token consumption with in-flight
// token reservations and idle-key LRU eviction.
type InMemoryTokenRateLimiter struct {
	store              map[string]*tokenRateLimitEntry
	lru                *list.List
	mutex              sync.Mutex
	expirationDuration time.Duration
}

func (l *InMemoryTokenRateLimiter) Init(expirationDuration time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store != nil {
		return
	}
	l.store = make(map[string]*tokenRateLimitEntry)
	l.lru = list.New()
	l.expirationDuration = expirationDuration
	if expirationDuration > 0 {
		go l.clearExpiredItems(time.NewTicker(expirationDuration).C)
	}
}

func (l *InMemoryTokenRateLimiter) clearExpiredItems(ticks <-chan time.Time) {
	for now := range ticks {
		l.deleteExpiredEntries(now)
	}
}

func (l *InMemoryTokenRateLimiter) deleteExpiredEntries(now time.Time) {
	if l.expirationDuration <= 0 {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	for {
		oldest := l.lru.Back()
		if oldest == nil {
			return
		}
		entry := oldest.Value.(*tokenRateLimitEntry)
		if entry.pending > 0 || now.Sub(entry.lastActive) < l.expirationDuration {
			return
		}
		delete(l.store, entry.key)
		l.lru.Remove(oldest)
		entry.element = nil
		entry.usage.clear()
	}
}

func (l *InMemoryTokenRateLimiter) Allow(key string, maxTokens int64, duration int64) bool {
	if maxTokens <= 0 || duration <= 0 {
		return true
	}
	return l.allowAt(key, maxTokens, duration, time.Now())
}

func (l *InMemoryTokenRateLimiter) allowAt(key string, maxTokens int64, duration int64, now time.Time) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store == nil {
		return true
	}
	entry, ok := l.store[key]
	if !ok {
		return true
	}
	entry.usage.removeExpired(now.Unix(), duration)
	entry.lastActive = now
	l.lru.MoveToFront(entry.element)
	return entry.usage.totalTokens+entry.pending < maxTokens
}

func (l *InMemoryTokenRateLimiter) reserveAt(key string, maxTokens int64, duration int64, estimatedTokens int64, now time.Time) (settle func(actualTokens int64), release func(), allowed bool) {
	if maxTokens <= 0 || duration <= 0 {
		return func(int64) {}, func() {}, true
	}
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	startUnix := now.Unix()
	l.mutex.Lock()
	if l.store == nil {
		l.store = make(map[string]*tokenRateLimitEntry)
		l.lru = list.New()
	}
	entry, ok := l.store[key]
	if ok {
		entry.usage.removeExpired(startUnix, duration)
		entry.lastActive = now
		l.lru.MoveToFront(entry.element)
	} else {
		entry = &tokenRateLimitEntry{key: key, lastActive: now}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	}
	used := entry.usage.totalTokens + entry.pending
	if used >= maxTokens || (estimatedTokens > 0 && used+estimatedTokens > maxTokens) {
		l.mutex.Unlock()
		return nil, nil, false
	}
	entry.pending += estimatedTokens
	l.mutex.Unlock()

	var once sync.Once
	finish := func(actualTokens int64) {
		once.Do(func() {
			l.mutex.Lock()
			defer l.mutex.Unlock()
			nowTime := time.Now()
			if e, exists := l.store[key]; exists {
				e.pending -= estimatedTokens
				if e.pending < 0 {
					e.pending = 0
				}
				e.lastActive = nowTime
				l.lru.MoveToFront(e.element)
				if actualTokens > 0 {
					nowUnix := nowTime.Unix()
					e.usage.removeExpired(nowUnix, duration)
					if nowUnix-startUnix < duration {
						e.usage.append(startUnix, actualTokens)
					}
				}
			} else if actualTokens > 0 && nowTime.Unix()-startUnix < duration {
				e = &tokenRateLimitEntry{key: key, lastActive: nowTime}
				e.element = l.lru.PushFront(e)
				l.store[key] = e
				e.usage.append(startUnix, actualTokens)
			}
		})
	}
	return finish, func() { finish(0) }, true
}

func (l *InMemoryTokenRateLimiter) Record(key string, tokens int64, duration int64) {
	if tokens <= 0 || duration <= 0 {
		return
	}
	l.recordAt(key, tokens, duration, time.Now())
}

func (l *InMemoryTokenRateLimiter) recordAt(key string, tokens int64, duration int64, now time.Time) {
	l.recordWithTimestamp(key, tokens, duration, now.Unix(), now)
}

func (l *InMemoryTokenRateLimiter) recordWithTimestamp(key string, tokens int64, duration int64, eventUnix int64, now time.Time) {
	if tokens <= 0 || duration <= 0 {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store == nil {
		l.store = make(map[string]*tokenRateLimitEntry)
		l.lru = list.New()
	}
	nowUnix := now.Unix()
	if eventUnix <= 0 || eventUnix > nowUnix {
		eventUnix = nowUnix
	}
	entry, ok := l.store[key]
	if ok {
		entry.usage.removeExpired(nowUnix, duration)
		entry.lastActive = now
		l.lru.MoveToFront(entry.element)
	} else {
		entry = &tokenRateLimitEntry{key: key, lastActive: now}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	}
	if nowUnix-eventUnix < duration {
		entry.usage.append(eventUnix, tokens)
	}
}

var (
	modelTokenMemoryLimiter InMemoryTokenRateLimiter
	incrRedisTokenScript    = redis.NewScript(`
local val = redis.call('INCRBY', KEYS[1], ARGV[1])
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return val
`)
	reserveRedisTokenScript = redis.NewScript(`
redis.replicate_commands()
local prevKey, curKey, pendingKey = KEYS[1], KEYS[2], KEYS[3]
local member, lease, maxTokens, duration, elapsed, est =
  ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6])
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('ZREMRANGEBYSCORE', pendingKey, '-inf', now)
local members = redis.call('ZRANGE', pendingKey, 0, -1)
local pending = 0
for _, m in ipairs(members) do
  local sep = string.find(m, ':', 1, true)
  if sep then
    pending = pending + (tonumber(string.sub(m, sep + 1)) or 0)
  end
end
local prevCount = tonumber(redis.call('GET', prevKey) or '0')
local curCount = tonumber(redis.call('GET', curKey) or '0')
local effectiveUsed = prevCount * (duration - elapsed) / duration + curCount + pending
if effectiveUsed >= maxTokens or (est > 0 and effectiveUsed + est > maxTokens) then
  return 0
end
if est > 0 then
  redis.call('ZADD', pendingKey, now + lease, member)
  redis.call('PEXPIRE', pendingKey, lease * 2)
end
return 1
`)
	settleRedisTokenScript = redis.NewScript(`
local curKey, pendingKey = KEYS[1], KEYS[2]
local member, actual, ttl = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
if pendingKey ~= '' and member ~= '' then
  redis.call('ZREM', pendingKey, member)
end
if actual > 0 and curKey ~= '' then
  local val = redis.call('INCRBY', curKey, actual)
  if redis.call('TTL', curKey) < 0 then
    redis.call('EXPIRE', curKey, ttl)
  end
  return val
end
return 0
`)
)

func redisModelTokenWindowKey(subject string, windowIndex int64) string {
	return fmt.Sprintf("rateLimit:%s:%s:%d", ModelRequestRateLimitTokenMark, subject, windowIndex)
}

func redisModelTokenPendingKey(subject string) string {
	return fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitTokenPendingMark, subject)
}

func checkRedisTokenRateLimitAt(ctx context.Context, rdb *redis.Client, subject string, maxTokens int64, duration int64, nowUnix int64) (bool, error) {
	if maxTokens <= 0 || duration <= 0 {
		return true, nil
	}
	winIdx := nowUnix / duration
	elapsed := nowUnix % duration
	prevKey := redisModelTokenWindowKey(subject, winIdx-1)
	curKey := redisModelTokenWindowKey(subject, winIdx)
	pendingKey := redisModelTokenPendingKey(subject)
	allowed, err := reserveRedisTokenScript.Run(ctx, rdb, []string{prevKey, curKey, pendingKey},
		"", modelConcurrencyLease.Milliseconds(), maxTokens, duration, elapsed, 0).Int()
	if err != nil {
		return false, err
	}
	return allowed == 1, nil
}

func reserveRedisTokenRateLimitAt(ctx context.Context, rdb *redis.Client, subject string, maxTokens int64, duration int64, estimatedTokens int64, nowUnix int64) (settle func(int64), release func(), allowed bool, err error) {
	if maxTokens <= 0 || duration <= 0 {
		return func(int64) {}, func() {}, true, nil
	}
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	winIdx := nowUnix / duration
	elapsed := nowUnix % duration
	prevKey := redisModelTokenWindowKey(subject, winIdx-1)
	curKey := redisModelTokenWindowKey(subject, winIdx)
	pendingKey := redisModelTokenPendingKey(subject)

	var member string
	if estimatedTokens > 0 {
		member = fmt.Sprintf("%s:%d", GetUUID(), estimatedTokens)
	}
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	acquireCtx, cancel := context.WithTimeout(baseCtx, modelConcurrencyRedisTimeout)
	res, err := reserveRedisTokenScript.Run(acquireCtx, rdb, []string{prevKey, curKey, pendingKey},
		member, modelConcurrencyLease.Milliseconds(), maxTokens, duration, elapsed, estimatedTokens).Int()
	cancel()
	if err != nil {
		return nil, nil, false, err
	}
	if res == 0 {
		return nil, nil, false, nil
	}

	stop := func() {}
	if member != "" {
		heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
		stop = cancelHeartbeat
		go func() {
			ticker := time.NewTicker(modelConcurrencyHeartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-heartbeatCtx.Done():
					return
				case <-ticker.C:
					refreshCtx, done := context.WithTimeout(heartbeatCtx, modelConcurrencyRedisTimeout)
					err := refreshModelConcurrencyScript.Run(refreshCtx, rdb, []string{pendingKey}, member, modelConcurrencyLease.Milliseconds()).Err()
					done()
					if err != nil && heartbeatCtx.Err() == nil {
						SysError(fmt.Sprintf("model token reservation lease refresh: subject=%s, error=%v", subject, err))
					}
				}
			}
		}()
	}

	var once sync.Once
	finish := func(actualTokens int64) {
		once.Do(func() {
			stop()
			if member == "" && actualTokens <= 0 {
				return
			}
			cleanupCtx, done := context.WithTimeout(context.Background(), modelConcurrencyRedisTimeout)
			if err := settleRedisTokenScript.Run(cleanupCtx, rdb, []string{curKey, pendingKey}, member, actualTokens, duration*2).Err(); err != nil {
				SysError(fmt.Sprintf("model token reservation settle: subject=%s, error=%v", subject, err))
			}
			done()
		})
	}
	return finish, func() { finish(0) }, true, nil
}

func recordRedisTokenRateLimitAt(ctx context.Context, rdb *redis.Client, subject string, tokens int64, duration int64, nowUnix int64) error {
	if tokens <= 0 || duration <= 0 || rdb == nil {
		return nil
	}
	winIdx := nowUnix / duration
	curKey := redisModelTokenWindowKey(subject, winIdx)
	return incrRedisTokenScript.Run(ctx, rdb, []string{curKey}, tokens, duration*2).Err()
}

// CheckModelTokenRateLimit checks whether the subject's token consumption
// (settled + currently reserved) in the sliding window is still below maxTokens.
func CheckModelTokenRateLimit(subject string, maxTokens int64, duration int64) (bool, error) {
	if maxTokens <= 0 || duration <= 0 {
		return true, nil
	}
	if RedisEnabled && RDB != nil {
		return checkRedisTokenRateLimitAt(context.Background(), RDB, subject, maxTokens, duration, time.Now().Unix())
	}
	modelTokenMemoryLimiter.Init(RateLimitKeyExpirationDuration)
	return modelTokenMemoryLimiter.Allow(ModelRequestRateLimitTokenMark+subject, maxTokens, duration), nil
}

// ReserveModelTokenRateLimit checks whether settled + in-flight + estimatedTokens
// fits within maxTokens, reserves estimatedTokens until settle/release is called,
// and pins the start timestamp for actual token settlement.
func ReserveModelTokenRateLimit(ctx context.Context, subject string, maxTokens int64, duration int64, estimatedTokens int64) (settle func(actualTokens int64), release func(), allowed bool, err error) {
	if maxTokens <= 0 || duration <= 0 || subject == "" {
		return func(int64) {}, func() {}, true, nil
	}
	now := time.Now()
	if RedisEnabled && RDB != nil {
		return reserveRedisTokenRateLimitAt(ctx, RDB, subject, maxTokens, duration, estimatedTokens, now.Unix())
	}
	modelTokenMemoryLimiter.Init(RateLimitKeyExpirationDuration)
	settle, release, allowed = modelTokenMemoryLimiter.reserveAt(ModelRequestRateLimitTokenMark+subject, maxTokens, duration, estimatedTokens, now)
	return settle, release, allowed, nil
}

// RecordModelTokenRateLimit records consumed tokens into the subject's current window.
func RecordModelTokenRateLimit(subject string, tokens int64, duration int64) {
	RecordModelTokenRateLimitAt(subject, tokens, duration, time.Now().Unix())
}

// RecordModelTokenRateLimitAt records consumed tokens attributed to eventUnix's window.
func RecordModelTokenRateLimitAt(subject string, tokens int64, duration int64, eventUnix int64) {
	if subject == "" || tokens <= 0 || duration <= 0 {
		return
	}
	if eventUnix <= 0 {
		eventUnix = time.Now().Unix()
	}
	if RedisEnabled && RDB != nil {
		if err := recordRedisTokenRateLimitAt(context.Background(), RDB, subject, tokens, duration, eventUnix); err != nil {
			SysError(fmt.Sprintf("failed to record model token rate limit: subject=%s, error=%v", subject, err))
		}
		return
	}
	modelTokenMemoryLimiter.Init(RateLimitKeyExpirationDuration)
	modelTokenMemoryLimiter.recordWithTimestamp(ModelRequestRateLimitTokenMark+subject, tokens, duration, eventUnix, time.Now())
}

const (
	ModelRequestRateLimitConcurrencyMark = "MRRLC"
	modelConcurrencyLease                = 60 * time.Second
	modelConcurrencyHeartbeat            = modelConcurrencyLease / 3
	modelConcurrencyRedisTimeout         = 2 * time.Second
)

type inMemoryModelConcurrencyState struct {
	mu     sync.Mutex
	active int
}

var inMemoryModelConcurrency sync.Map // subject -> *inMemoryModelConcurrencyState

func getInMemoryModelConcurrencyState(subject string) *inMemoryModelConcurrencyState {
	if v, ok := inMemoryModelConcurrency.Load(subject); ok {
		return v.(*inMemoryModelConcurrencyState)
	}
	v, _ := inMemoryModelConcurrency.LoadOrStore(subject, &inMemoryModelConcurrencyState{})
	return v.(*inMemoryModelConcurrencyState)
}

var (
	acquireModelConcurrencyScript = redis.NewScript(`
redis.replicate_commands()
local key = KEYS[1]
local member, lease, limit = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
if redis.call('ZCARD', key) >= limit then
  return 0
end
redis.call('ZADD', key, now + lease, member)
redis.call('PEXPIRE', key, lease * 2)
return 1
`)
	refreshModelConcurrencyScript = redis.NewScript(`
redis.replicate_commands()
local key = KEYS[1]
local member, lease = ARGV[1], tonumber(ARGV[2])
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local expiry = tonumber(redis.call('ZSCORE', key, member))
if expiry and expiry > now then
  redis.call('ZADD', key, now + lease, member)
  redis.call('PEXPIRE', key, lease * 2)
end
`)
)

// AcquireModelConcurrency acquires an in-flight concurrency slot for subject.
// Callers must invoke the returned release function when the request finishes.
func AcquireModelConcurrency(ctx context.Context, subject string, maxConcurrency int) (func(), bool, error) {
	if maxConcurrency <= 0 || subject == "" {
		return func() {}, true, nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
	}
	if !RedisEnabled || RDB == nil {
		state := getInMemoryModelConcurrencyState(subject)
		state.mu.Lock()
		if state.active >= maxConcurrency {
			state.mu.Unlock()
			return nil, false, nil
		}
		state.active++
		state.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				state.mu.Lock()
				if state.active > 0 {
					state.active--
				}
				state.mu.Unlock()
			})
		}, true, nil
	}

	key := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitConcurrencyMark, subject)
	member := GetUUID()
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	acquireCtx, cancel := context.WithTimeout(baseCtx, modelConcurrencyRedisTimeout)
	allowed, err := acquireModelConcurrencyScript.Run(acquireCtx, RDB, []string{key}, member, modelConcurrencyLease.Milliseconds(), maxConcurrency).Int()
	cancel()
	if err != nil {
		return nil, false, err
	}
	if allowed == 0 {
		return nil, false, nil
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(modelConcurrencyHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				refreshCtx, done := context.WithTimeout(heartbeatCtx, modelConcurrencyRedisTimeout)
				err := refreshModelConcurrencyScript.Run(refreshCtx, RDB, []string{key}, member, modelConcurrencyLease.Milliseconds()).Err()
				done()
				if err != nil && heartbeatCtx.Err() == nil {
					SysError(fmt.Sprintf("model concurrency lease refresh: subject=%s, error=%v", subject, err))
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancelHeartbeat()
			cleanupCtx, done := context.WithTimeout(context.Background(), modelConcurrencyRedisTimeout)
			if err := RDB.ZRem(cleanupCtx, key, member).Err(); err != nil {
				SysError(fmt.Sprintf("model concurrency lease release: subject=%s, error=%v", subject, err))
			}
			done()
		})
	}, true, nil
}
