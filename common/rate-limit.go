package common

import (
	"container/list"
	"sync"
	"time"
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
