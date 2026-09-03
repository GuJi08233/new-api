package service

import (
	"sync"
	"time"
)

// ipEventWindow 是按 IP 分桶的滑动事件窗口,供实时防护(Probe Guard / Error Guard)共用。
// 事件记录、过期裁剪、触发后冷却、空闲桶回收都在这里;使用方只提供窗口长度、
// 冷却时长,以及"窗口内事件是否达到触发条件"的判定。窗口按节点独立统计,
// 多节点部署时攻击流量被分摊会稀释计数,可通过调低阈值补偿。

type ipWindowEvent struct {
	at    time.Time
	label string // 由使用方定义:模型名 / 状态码
}

type ipEventWindow struct {
	mu        sync.Mutex
	events    map[string][]ipWindowEvent
	cooldowns map[string]time.Time
	lastPrune time.Time
}

// ipEventWindowPruneInterval 是空闲桶回收的最小间隔。
const ipEventWindowPruneInterval = 5 * time.Minute

func newIpEventWindow() *ipEventWindow {
	return &ipEventWindow{
		events:    map[string][]ipWindowEvent{},
		cooldowns: map[string]time.Time{},
	}
}

// record 记入一次事件,返回 evaluate 给出的度量值与是否应当触发处置。
// evaluate 在持锁期间对裁剪后的窗口求值,返回 (度量值, 是否达到阈值);
// 达到阈值且不在冷却期时返回 fired=true 并进入冷却,避免一次爆发反复升级封禁。
func (w *ipEventWindow) record(ip string, label string, now time.Time, window time.Duration,
	cooldown time.Duration, evaluate func([]ipWindowEvent) (int, bool)) (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pruneLocked(now, window)

	cutoff := now.Add(-window)
	events := w.events[ip]
	kept := events[:0]
	for _, event := range events {
		if event.at.After(cutoff) {
			kept = append(kept, event)
		}
	}
	kept = append(kept, ipWindowEvent{at: now, label: label})
	w.events[ip] = kept

	measure, reached := evaluate(kept)
	if !reached {
		return measure, false
	}
	if until, ok := w.cooldowns[ip]; ok && until.After(now) {
		return measure, false
	}
	w.cooldowns[ip] = now.Add(cooldown)
	return measure, true
}

// pruneLocked 周期性回收不再活跃的窗口与冷却记录,防止 map 无界增长。
// 调用方必须已持有 w.mu。
func (w *ipEventWindow) pruneLocked(now time.Time, window time.Duration) {
	if now.Sub(w.lastPrune) < ipEventWindowPruneInterval {
		return
	}
	w.lastPrune = now
	cutoff := now.Add(-window)
	for ip, events := range w.events {
		alive := false
		for _, event := range events {
			if event.at.After(cutoff) {
				alive = true
				break
			}
		}
		if !alive {
			delete(w.events, ip)
		}
	}
	for ip, until := range w.cooldowns {
		if !until.After(now) {
			delete(w.cooldowns, ip)
		}
	}
}

// reset 清空全部状态,仅供测试使用。
func (w *ipEventWindow) reset() {
	w.mu.Lock()
	w.events = map[string][]ipWindowEvent{}
	w.cooldowns = map[string]time.Time{}
	w.lastPrune = time.Time{}
	w.mu.Unlock()
}
