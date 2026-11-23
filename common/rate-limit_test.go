package common

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每个 key 只为实际到达的请求分配节点，不按配置上限预分配。
func TestInMemoryRateLimiterAllocatesTimestampsOnDemand(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)

	require.True(t, limiter.Request("client", 1_000_000, 60))
	entry := limiter.store["client"]
	require.NotNil(t, entry)
	assert.Equal(t, 1, entry.requests.length)
	assert.Same(t, entry.requests.head, entry.requests.tail)

	limiter.deleteExpiredEntries(time.Now().Add(time.Hour))
	assert.Contains(t, limiter.store, "client", "expiration 0 disables idle-key eviction")
}

func TestRateLimitQueueRemovesExpiredRequests(t *testing.T) {
	t.Run("window boundary is inclusive", func(t *testing.T) {
		var queue rateLimitQueue
		queue.append(100)
		queue.append(100)
		queue.removeExpired(159, 60)
		assert.Equal(t, 2, queue.length)
		queue.removeExpired(160, 60)
		assert.Zero(t, queue.length)
		assert.Nil(t, queue.head)
		assert.Nil(t, queue.tail)
	})
	t.Run("every expired request leaves in one call", func(t *testing.T) {
		var queue rateLimitQueue
		for range 5 {
			queue.append(100)
		}
		queue.append(110)
		queue.removeExpired(110, 10)
		require.Equal(t, 1, queue.length)
		assert.EqualValues(t, 110, queue.head.timestamp)
		assert.Same(t, queue.head, queue.tail)
	})
	t.Run("requests inside the window stay", func(t *testing.T) {
		var queue rateLimitQueue
		for range 3 {
			queue.append(100)
		}
		queue.removeExpired(109, 10)
		assert.Equal(t, 3, queue.length)
	})
}

// 空闲淘汰只回收 LRU 尾部真正过期的 key，活跃 key 及其窗口保持不变。
func TestInMemoryRateLimiterEvictsOnlyExpiredLRUTail(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)
	limiter.expirationDuration = 10 * time.Second

	require.True(t, limiter.Request("active", 1, 100))
	require.True(t, limiter.Request("idle", 1, 100))
	assert.False(t, limiter.Request("active", 1, 100))
	assert.Equal(t, "active", limiter.lru.Front().Value.(*rateLimitEntry).key)
	assert.Equal(t, "idle", limiter.lru.Back().Value.(*rateLimitEntry).key)

	now := time.Now()
	limiter.store["idle"].lastActive = now.Add(-10 * time.Second)
	limiter.store["active"].lastActive = now
	limiter.deleteExpiredEntries(now)

	assert.NotContains(t, limiter.store, "idle")
	assert.Contains(t, limiter.store, "active")
	assert.Equal(t, 1, limiter.lru.Len())
	assert.False(t, limiter.Request("active", 1, 100), "the surviving key keeps its window")
}

func TestInMemoryRateLimiterCleanupTicksEvictExpiredKeys(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(0)
	limiter.expirationDuration = 10 * time.Second
	require.True(t, limiter.Request("idle", 1, 100))

	now := time.Now()
	limiter.store["idle"].lastActive = now.Add(-10 * time.Second)
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		limiter.clearExpiredItems(ticks)
		close(done)
	}()
	ticks <- now
	close(ticks)
	<-done

	assert.Empty(t, limiter.store)
	assert.Zero(t, limiter.lru.Len())
}

func TestInMemoryRateLimiterInitKeepsFirstConfiguration(t *testing.T) {
	var limiter InMemoryRateLimiter
	limiter.Init(time.Hour)
	limiter.Init(0)

	assert.Equal(t, time.Hour, limiter.expirationDuration)
	require.True(t, limiter.Request("client", 1, 60))
	limiter.deleteExpiredEntries(time.Now().Add(time.Hour))
	assert.NotContains(t, limiter.store, "client")
}

// 并发初始化与请求下，同一 key 恰好放行配置上限次数。
func TestInMemoryRateLimiterConcurrentInitializationAndRequests(t *testing.T) {
	var limiter InMemoryRateLimiter
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Init(0)
			if limiter.Request("client", 10, 60) {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 10, allowed.Load())
	assert.Len(t, limiter.store, 1)
	assert.Equal(t, 10, limiter.store["client"].requests.length)
}

func TestInMemoryTokenRateLimiterSlidingWindowAndEviction(t *testing.T) {
	var limiter InMemoryTokenRateLimiter
	limiter.Init(0)
	limiter.expirationDuration = 10 * time.Minute

	base := time.Unix(1_700_000_000, 0)
	require.True(t, limiter.allowAt("user-1", 1000, 60, base))
	limiter.recordAt("user-1", 600, 60, base)
	require.True(t, limiter.allowAt("user-1", 1000, 60, base.Add(10*time.Second)))
	limiter.recordAt("user-1", 400, 60, base.Add(10*time.Second))
	assert.False(t, limiter.allowAt("user-1", 1000, 60, base.Add(59*time.Second)), "sum 1000 >= max 1000 must block")

	// After 60s, the first 600 tokens expire and 400 remain in window.
	assert.True(t, limiter.allowAt("user-1", 1000, 60, base.Add(60*time.Second)))
	// After 70s, all tokens expire.
	assert.True(t, limiter.allowAt("user-1", 400, 60, base.Add(70*time.Second)))
	assert.EqualValues(t, 0, limiter.store["user-1"].usage.totalTokens)

	// Idle key eviction leaves active keys intact.
	limiter.recordAt("idle", 100, 60, base)
	// Touch user-1 after idle so idle sits at LRU tail.
	require.True(t, limiter.allowAt("user-1", 400, 60, base.Add(70*time.Second)))
	limiter.store["idle"].lastActive = base
	limiter.store["user-1"].lastActive = base.Add(11 * time.Minute)
	limiter.deleteExpiredEntries(base.Add(11 * time.Minute))
	assert.NotContains(t, limiter.store, "idle")
	assert.Contains(t, limiter.store, "user-1")
}

func TestRedisTokenRateLimitSlidingWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	ctx := context.Background()

	// Aligned at start of a 60s window.
	t0 := int64(1_700_000_040) // 1700000040 % 60 == 0
	require.EqualValues(t, 0, t0%60)

	allowed, err := checkRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0)
	require.NoError(t, err)
	require.True(t, allowed)

	require.NoError(t, recordRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0+50))
	allowed, err = checkRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0+55)
	require.NoError(t, err)
	assert.False(t, allowed, "1000 tokens used in current window must block")

	// Halfway through next window (elapsed=30s): previous window weighted at 50% (500 < 1000).
	allowed, err = checkRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0+90)
	require.NoError(t, err)
	assert.True(t, allowed)

	// Adding 400 in current window reaches 1000*0.5 + 400 = 900 < 1000; adding another 200 reaches 1100 >= 1000.
	require.NoError(t, recordRedisTokenRateLimitAt(ctx, client, "u1", 400, 60, t0+90))
	allowed, err = checkRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0+90)
	require.NoError(t, err)
	assert.True(t, allowed)
	require.NoError(t, recordRedisTokenRateLimitAt(ctx, client, "u1", 200, 60, t0+90))
	allowed, err = checkRedisTokenRateLimitAt(ctx, client, "u1", 1000, 60, t0+90)
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAcquireModelConcurrencyMemoryAndRedis(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		name := "memory"
		if useRedis {
			name = "redis"
		}
		t.Run(name, func(t *testing.T) {
			prevRedisEnabled, prevRDB := RedisEnabled, RDB
			t.Cleanup(func() { RedisEnabled, RDB = prevRedisEnabled, prevRDB })
			RedisEnabled = useRedis
			if useRedis {
				server := miniredis.RunT(t)
				RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = RDB.Close() })
			}

			ctx := context.Background()
			subject := "conc-user-" + name
			r1, ok, err := AcquireModelConcurrency(ctx, subject, 2)
			require.NoError(t, err)
			require.True(t, ok)

			r2, ok, err := AcquireModelConcurrency(ctx, subject, 2)
			require.NoError(t, err)
			require.True(t, ok)

			_, ok, err = AcquireModelConcurrency(ctx, subject, 2)
			require.NoError(t, err)
			assert.False(t, ok, "3rd concurrent request must be rejected when limit=2")

			// Double release must not decrement twice.
			r1()
			r1()

			r3, ok, err := AcquireModelConcurrency(ctx, subject, 2)
			require.NoError(t, err)
			assert.True(t, ok, "slot must be available after release")

			// Limit=2 still blocks a 3rd active request while r2 and r3 are held.
			_, ok, err = AcquireModelConcurrency(ctx, subject, 2)
			require.NoError(t, err)
			assert.False(t, ok)

			r2()
			r3()
		})
	}
}

func TestModelTokenReservationPrecheckSettlementAndFailureRelease(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		name := "memory"
		if useRedis {
			name = "redis"
		}
		t.Run(name, func(t *testing.T) {
			prevRedisEnabled, prevRDB := RedisEnabled, RDB
			t.Cleanup(func() { RedisEnabled, RDB = prevRedisEnabled, prevRDB })
			RedisEnabled = useRedis
			if useRedis {
				server := miniredis.RunT(t)
				RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = RDB.Close() })
			}

			ctx := context.Background()
			subject := "reserve-tpm-" + name

			// Request 1 estimates 500k against 800k limit -> allowed.
			settle1, release1, ok, err := ReserveModelTokenRateLimit(ctx, subject, 800_000, 60, 500_000)
			require.NoError(t, err)
			require.True(t, ok)

			// Concurrent Request 2 estimating 400k (500k + 400k > 800k) -> blocked.
			_, _, ok, err = ReserveModelTokenRateLimit(ctx, subject, 800_000, 60, 400_000)
			require.NoError(t, err)
			assert.False(t, ok, "concurrent request exceeding limit with pending reservation must block")

			// Settle Request 1 at 510k actual tokens; release1 afterward is idempotent.
			settle1(510_000)
			release1()

			// Request 3 estimating 515k: 510k + 515k > 800k -> blocked.
			_, _, ok, err = ReserveModelTokenRateLimit(ctx, subject, 800_000, 60, 515_000)
			require.NoError(t, err)
			assert.False(t, ok, "settled + estimated exceeding limit must block")

			// Request 4 estimating 290k: 510k + 290k == 800k -> allowed; failing releases reservation.
			_, release4, ok, err := ReserveModelTokenRateLimit(ctx, subject, 800_000, 60, 290_000)
			require.NoError(t, err)
			require.True(t, ok)
			release4()

			// After release, another 290k request fits and settles cleanly.
			settle5, release5, ok, err := ReserveModelTokenRateLimit(ctx, subject, 800_000, 60, 290_000)
			require.NoError(t, err)
			require.True(t, ok)
			settle5(290_000)
			release5()

			allowed, err := CheckModelTokenRateLimit(subject, 800_000, 60)
			require.NoError(t, err)
			assert.False(t, allowed, "total settled 800k must block subsequent requests")
		})
	}
}

func TestTokenRateLimitQueueMaintainsOrderOnOutOfOrderStartTimestamps(t *testing.T) {
	var q tokenRateLimitQueue
	q.append(110, 200)
	q.append(100, 500) // earlier start timestamp settled later
	q.append(105, 300)
	assert.EqualValues(t, 1000, q.totalTokens)
	assert.EqualValues(t, 100, q.head.timestamp)
	assert.EqualValues(t, 110, q.tail.timestamp)

	// At now=160 with duration=60, only ts=100 expires; 105 and 110 remain.
	q.removeExpired(160, 60)
	assert.EqualValues(t, 500, q.totalTokens)
	assert.EqualValues(t, 105, q.head.timestamp)
}
