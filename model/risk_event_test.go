package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertRiskEventNormalizesFields(t *testing.T) {
	truncateTables(t)

	event := &RiskEvent{
		EventType: RiskEventBlockUa,
		UserId:    7,
		Ua:        strings.Repeat("长", 600),
		Reason:    strings.Repeat("r", 600),
	}
	require.NoError(t, InsertRiskEvent(event))

	var saved RiskEvent
	require.NoError(t, DB.First(&saved, event.Id).Error)
	assert.Greater(t, saved.CreatedAt, int64(0), "CreatedAt 未设置时应自动补当前时间")
	assert.Equal(t, 1, saved.Count, "Count 未设置时应默认为 1")
	assert.Equal(t, 512, len([]rune(saved.Ua)), "UA 应按列宽截断")
	assert.Equal(t, 512, len([]rune(saved.Reason)), "Reason 应按列宽截断")
}

func TestGetRiskEventsFiltersAndPaginates(t *testing.T) {
	truncateTables(t)

	base := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBlockUa, UserId: 1, Ip: "1.1.1.1", CreatedAt: base - 30},
		{EventType: RiskEventBlockIp, UserId: 2, Ip: "2.2.2.2", CreatedAt: base - 20},
		{EventType: RiskEventBanManual, UserId: 2, Reason: "违规", OperatorId: 9, CreatedAt: base - 10},
		{EventType: RiskEventBanAuto, UserId: 3, Ip: "1.1.1.1", CreatedAt: base},
	}
	for _, e := range seed {
		require.NoError(t, InsertRiskEvent(e))
	}

	// 无过滤:全量按时间倒序
	events, total, err := GetRiskEvents("", 0, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	require.Len(t, events, 4)
	assert.Equal(t, RiskEventBanAuto, events[0].EventType)

	// 按类型过滤
	events, total, err = GetRiskEvents(RiskEventBanManual, 0, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, events, 1)
	assert.Equal(t, "违规", events[0].Reason)
	assert.Equal(t, 9, events[0].OperatorId)

	// 按用户过滤
	_, total, err = GetRiskEvents("", 2, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)

	// 按 IP 过滤
	_, total, err = GetRiskEvents("", 0, "1.1.1.1", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)

	// 分页:total 不变,第二页取到更早的记录
	events, total, err = GetRiskEvents("", 0, "", 2, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	require.Len(t, events, 2)
	assert.Equal(t, RiskEventBlockIp, events[0].EventType)
	assert.Equal(t, RiskEventBlockUa, events[1].EventType)
}

func TestCleanupRiskEventsKeepsBanRecords(t *testing.T) {
	truncateTables(t)

	old := time.Now().Add(-40 * 24 * time.Hour).Unix()
	recent := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBlockUa, CreatedAt: old},
		{EventType: RiskEventBlockIp, CreatedAt: old},
		{EventType: RiskEventAlert, CreatedAt: old},
		{EventType: RiskEventBanAuto, CreatedAt: old},
		{EventType: RiskEventBanManual, CreatedAt: old},
		{EventType: RiskEventUnban, CreatedAt: old},
		{EventType: RiskEventBlockUa, CreatedAt: recent},
	}
	for _, e := range seed {
		require.NoError(t, InsertRiskEvent(e))
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	deleted, err := CleanupRiskEvents(cutoff)
	require.NoError(t, err)
	assert.EqualValues(t, 3, deleted, "只清理过期的拦截与告警事件")

	var remaining []RiskEvent
	require.NoError(t, DB.Find(&remaining).Error)
	require.Len(t, remaining, 4)
	for _, e := range remaining {
		if e.CreatedAt == old {
			assert.Contains(t, []string{RiskEventBanAuto, RiskEventBanManual, RiskEventUnban}, e.EventType,
				"过期后保留的只能是封禁/解禁审计记录")
		}
	}
}

func TestLastIpBanEventAt(t *testing.T) {
	truncateTables(t)

	// 从未封禁过:0
	last, err := LastIpBanEventAt("198.51.100.80")
	require.NoError(t, err)
	assert.EqualValues(t, 0, last)

	base := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBanIp, Ip: "198.51.100.80", CreatedAt: base - 600},
		{EventType: RiskEventBanIp, Ip: "198.51.100.80", CreatedAt: base - 60},
		{EventType: RiskEventBanIp, Ip: "198.51.100.81", CreatedAt: base},   // 别的目标
		{EventType: RiskEventUnbanIp, Ip: "198.51.100.80", CreatedAt: base}, // 解封不算封禁
	}
	for _, e := range seed {
		require.NoError(t, InsertRiskEvent(e))
	}

	last, err = LastIpBanEventAt("198.51.100.80")
	require.NoError(t, err)
	assert.EqualValues(t, base-60, last, "取最近一次 ban_ip 事件,忽略其他目标与解封记录")
}
