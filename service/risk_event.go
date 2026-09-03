package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 拦截类风控事件按 (类型+用户+IP+规则) 聚合后落库:窗口内首次命中立即写一条,
// 其后命中只在内存累加,窗口滚动或定时冲刷时合并为一条带 Count 的事件。
// 避免被拦截的客户端重试循环把 risk_events 表刷爆。聚合按节点独立进行,
// 多节点部署时各节点分别落库,事件次数为各节点视角的累计。

const (
	// riskBlockEventWindow 黑名单拦截事件的聚合窗口。
	riskBlockEventWindow = time.Minute
	// riskAlertEventWindow 自动规则告警事件的聚合窗口:
	// 扫描每轮都会重复命中同一目标,窗口取长避免告警刷屏。
	riskAlertEventWindow = 6 * time.Hour
	// riskEventFlushInterval 后台冲刷挂起计数的周期。
	riskEventFlushInterval = 30 * time.Second
	// riskEventBucketIdleTTL 空闲聚合桶的回收时限。
	riskEventBucketIdleTTL = 30 * time.Minute
)

type riskEventBucket struct {
	pending     int             // 窗口内被抑制、尚未落库的命中次数
	window      time.Duration   // 该键的聚合窗口
	windowStart time.Time       // 当前窗口起点(上次落库时间)
	template    model.RiskEvent // 最近一次被抑制命中的事件内容,冲刷时落库
}

var (
	riskEventMu      sync.Mutex
	riskEventBuckets = map[string]*riskEventBucket{}
)

func insertRiskEventLogged(event model.RiskEvent) {
	if err := model.InsertRiskEvent(&event); err != nil {
		common.SysLog("risk control: failed to insert risk event: " + err.Error())
	}
}

// recordRiskEventThrottled 按 key 聚合写入一条风控事件。
// 窗口内首次命中立即落库(Count 含此前窗口挂起的次数),其余命中仅累加。
func recordRiskEventThrottled(key string, window time.Duration, event model.RiskEvent, now time.Time) {
	riskEventMu.Lock()
	bucket, ok := riskEventBuckets[key]
	if !ok || now.Sub(bucket.windowStart) >= bucket.window {
		pending := 0
		if ok {
			pending = bucket.pending
		}
		riskEventBuckets[key] = &riskEventBucket{window: window, windowStart: now}
		riskEventMu.Unlock()

		event.Count = pending + 1
		insertRiskEventLogged(event)
		return
	}
	bucket.pending++
	bucket.template = event
	riskEventMu.Unlock()
}

// flushStaleRiskEvents 落库所有窗口已过期的挂起计数,并回收空闲桶。
// 保证攻击停止后,窗口尾部被抑制的命中也能写入事件表而非静默丢失。
func flushStaleRiskEvents(now time.Time) {
	var toInsert []model.RiskEvent

	riskEventMu.Lock()
	for key, bucket := range riskEventBuckets {
		if now.Sub(bucket.windowStart) < bucket.window {
			continue
		}
		if bucket.pending > 0 {
			event := bucket.template
			event.Count = bucket.pending
			toInsert = append(toInsert, event)
			bucket.pending = 0
			bucket.windowStart = now
			continue
		}
		if now.Sub(bucket.windowStart) >= riskEventBucketIdleTTL {
			delete(riskEventBuckets, key)
		}
	}
	riskEventMu.Unlock()

	for _, event := range toInsert {
		insertRiskEventLogged(event)
	}
}
