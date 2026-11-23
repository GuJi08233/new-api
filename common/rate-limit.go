package common

import (
	"container/list"
	"context"
	"fmt"
	"strings"
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

const ModelRequestRateLimitTokenMark = "MRRLT"

// ModelRateLimitTokenTarget carries the resolved rate-limit subject and window
// duration from ModelRequestRateLimit middleware to post-request token recording.
type ModelRateLimitTokenTarget struct {
	Subject  string
	Duration int64
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
	} else {
		q.tail.next = node
	}
	q.tail = node
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
	key        string
}

// InMemoryTokenRateLimiter tracks sliding-window token consumption with idle-key LRU eviction.
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
		if now.Sub(entry.lastActive) < l.expirationDuration {
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
	return entry.usage.totalTokens < maxTokens
}

func (l *InMemoryTokenRateLimiter) Record(key string, tokens int64, duration int64) {
	if tokens <= 0 || duration <= 0 {
		return
	}
	l.recordAt(key, tokens, duration, time.Now())
}

func (l *InMemoryTokenRateLimiter) recordAt(key string, tokens int64, duration int64, now time.Time) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.store == nil {
		l.store = make(map[string]*tokenRateLimitEntry)
		l.lru = list.New()
	}
	entry, ok := l.store[key]
	if ok {
		entry.usage.removeExpired(now.Unix(), duration)
		entry.lastActive = now
		l.lru.MoveToFront(entry.element)
	} else {
		entry = &tokenRateLimitEntry{key: key, lastActive: now}
		entry.element = l.lru.PushFront(entry)
		l.store[key] = entry
	}
	entry.usage.append(now.Unix(), tokens)
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
)

func redisModelTokenWindowKey(subject string, windowIndex int64) string {
	return fmt.Sprintf("rateLimit:%s:%s:%d", ModelRequestRateLimitTokenMark, subject, windowIndex)
}

func parseRedisCounterInt64(val any) int64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case string:
		var n int64
		fmt.Sscan(strings.TrimSpace(v), &n)
		return n
	case []byte:
		var n int64
		fmt.Sscan(strings.TrimSpace(string(v)), &n)
		return n
	case int64:
		return v
	default:
		return 0
	}
}

func checkRedisTokenRateLimitAt(ctx context.Context, rdb *redis.Client, subject string, maxTokens int64, duration int64, nowUnix int64) (bool, error) {
	if maxTokens <= 0 || duration <= 0 {
		return true, nil
	}
	winIdx := nowUnix / duration
	elapsed := nowUnix % duration
	curKey := redisModelTokenWindowKey(subject, winIdx)
	prevKey := redisModelTokenWindowKey(subject, winIdx-1)

	vals, err := rdb.MGet(ctx, prevKey, curKey).Result()
	if err != nil {
		return false, err
	}
	var prevCount, curCount int64
	if len(vals) > 0 {
		prevCount = parseRedisCounterInt64(vals[0])
	}
	if len(vals) > 1 {
		curCount = parseRedisCounterInt64(vals[1])
	}
	effectiveUsed := float64(prevCount)*float64(duration-elapsed)/float64(duration) + float64(curCount)
	return effectiveUsed < float64(maxTokens), nil
}

func recordRedisTokenRateLimitAt(ctx context.Context, rdb *redis.Client, subject string, tokens int64, duration int64, nowUnix int64) error {
	if tokens <= 0 || duration <= 0 || rdb == nil {
		return nil
	}
	winIdx := nowUnix / duration
	curKey := redisModelTokenWindowKey(subject, winIdx)
	return incrRedisTokenScript.Run(ctx, rdb, []string{curKey}, tokens, duration*2).Err()
}

// CheckModelTokenRateLimit checks whether the subject's token consumption in the
// current sliding window is still below maxTokens.
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

// RecordModelTokenRateLimit records consumed tokens into the subject's current window.
func RecordModelTokenRateLimit(subject string, tokens int64, duration int64) {
	if subject == "" || tokens <= 0 || duration <= 0 {
		return
	}
	if RedisEnabled && RDB != nil {
		if err := recordRedisTokenRateLimitAt(context.Background(), RDB, subject, tokens, duration, time.Now().Unix()); err != nil {
			SysError(fmt.Sprintf("failed to record model token rate limit: subject=%s, error=%v", subject, err))
		}
		return
	}
	modelTokenMemoryLimiter.Init(RateLimitKeyExpirationDuration)
	modelTokenMemoryLimiter.Record(ModelRequestRateLimitTokenMark+subject, tokens, duration)
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
