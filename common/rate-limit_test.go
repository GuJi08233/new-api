package common

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
