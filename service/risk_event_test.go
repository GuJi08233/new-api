package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRiskEventTest 迁移事件表并重置聚合器与表数据,保证用例间互不干扰。
func setupRiskEventTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.RiskEvent{}))
	require.NoError(t, model.DB.Exec("DELETE FROM risk_events").Error)
	riskEventMu.Lock()
	riskEventBuckets = map[string]*riskEventBucket{}
	riskEventMu.Unlock()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM risk_events")
		riskEventMu.Lock()
		riskEventBuckets = map[string]*riskEventBucket{}
		riskEventMu.Unlock()
	})
}

func riskEventRows(t *testing.T) []model.RiskEvent {
	t.Helper()
	var rows []model.RiskEvent
	require.NoError(t, model.DB.Order("id asc").Find(&rows).Error)
	return rows
}

func TestRecordRiskEventThrottledAggregatesWithinWindow(t *testing.T) {
	setupRiskEventTest(t)

	base := time.Unix(1700000000, 0)
	event := model.RiskEvent{EventType: model.RiskEventBlockUa, UserId: 1, Ip: "1.2.3.4", Ua: "bad-bot", Rule: "bad-bot"}

	// 首次命中立即落库,Count=1
	recordRiskEventThrottled("k", time.Minute, event, base)
	rows := riskEventRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)

	// 窗口内的命中只在内存累加,不落库
	for i := 0; i < 5; i++ {
		recordRiskEventThrottled("k", time.Minute, event, base.Add(10*time.Second))
	}
	assert.Len(t, riskEventRows(t), 1)

	// 窗口滚动后的下一次命中,把挂起的 5 次连同本次合并为 Count=6 落库
	recordRiskEventThrottled("k", time.Minute, event, base.Add(2*time.Minute))
	rows = riskEventRows(t)
	require.Len(t, rows, 2)
	assert.Equal(t, 6, rows[1].Count)

	// 不同 key 互不影响,首次命中立即落库
	recordRiskEventThrottled("k2", time.Minute, event, base.Add(2*time.Minute))
	assert.Len(t, riskEventRows(t), 3)
}

func TestFlushStaleRiskEventsWritesPendingCounts(t *testing.T) {
	setupRiskEventTest(t)

	base := time.Unix(1700000000, 0)
	event := model.RiskEvent{EventType: model.RiskEventBlockIp, Ip: "5.6.7.8", Rule: "5.6.7.0/24"}

	recordRiskEventThrottled("k", time.Minute, event, base)
	recordRiskEventThrottled("k", time.Minute, event, base.Add(5*time.Second))
	recordRiskEventThrottled("k", time.Minute, event, base.Add(10*time.Second))
	require.Len(t, riskEventRows(t), 1)

	// 窗口未到期:冲刷不产生新事件
	flushStaleRiskEvents(base.Add(30 * time.Second))
	assert.Len(t, riskEventRows(t), 1)

	// 窗口到期:攻击停止后挂起的 2 次命中由冲刷落库,不再静默丢失
	flushStaleRiskEvents(base.Add(2 * time.Minute))
	rows := riskEventRows(t)
	require.Len(t, rows, 2)
	assert.Equal(t, 2, rows[1].Count)
	assert.Equal(t, model.RiskEventBlockIp, rows[1].EventType)
	assert.Equal(t, "5.6.7.8", rows[1].Ip)

	// 无挂起计数时重复冲刷不写库
	flushStaleRiskEvents(base.Add(3 * time.Minute))
	assert.Len(t, riskEventRows(t), 2)
}
