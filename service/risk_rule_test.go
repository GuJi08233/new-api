package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRiskRulePreviewSharesScanThresholdAndHasNoSideEffects(t *testing.T) {
	setupRealtimeGuardTest(t)
	rule := operation_setting.RiskAutoBanRule{Id: "shared-ip", Name: "shared network", Enabled: true,
		Metric: operation_setting.RiskMetricIpMultiUser, WindowHours: 24, Threshold: 2, Action: operation_setting.RiskRuleActionBanBoth}
	seedIpLogs(t, "198.51.100.21", []int{10, 11}, time.Now().Add(-time.Hour).Unix())
	seedIpLogs(t, "198.51.100.22", []int{10, 11, 12}, time.Now().Add(-time.Hour).Unix())
	preview, err := PreviewRiskRule(rule, 16, nil)
	require.NoError(t, err)
	require.Len(t, preview.Hits, 1)
	assert.Equal(t, "198.51.100.22", preview.Hits[0].Value)
	assert.Equal(t, 3, preview.Hits[0].Observed)
	var count int64
	require.NoError(t, model.DB.Model(&model.IpBan{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, model.DB.Model(&model.RiskEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	preview, err = PreviewRiskRule(rule, 16, []int{12})
	require.NoError(t, err)
	assert.Empty(t, preview.Hits, "扫描与预览均排除全局白名单的流量")
}

func TestRiskScanRecordsFailuresAndPreservesHistoricalRuleEvidence(t *testing.T) {
	setupRealtimeGuardTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.RiskScanRun{}))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.RiskScanRun{}).Error)
	rule := operation_setting.RiskAutoBanRule{Id: "scan-ip", Name: "IP rule", Enabled: true,
		Metric: operation_setting.RiskMetricIpMultiUser, WindowHours: 24, Threshold: 2, Action: operation_setting.RiskRuleActionBanIp}
	setting := probeGuardTestSetting()
	setting.AutoBanRules = []operation_setting.RiskAutoBanRule{rule}
	operation_setting.SetRiskControlSettingForTest(setting)
	seedIpLogs(t, "198.51.100.33", []int{10, 11, 12}, time.Now().Add(-time.Hour).Unix())
	runRiskScan(setting)
	run, err := model.GetLatestRiskScan()
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "success", run.Status)
	assert.Equal(t, 1, run.Hits)
	assert.GreaterOrEqual(t, run.CompletedAt, run.StartedAt)
	events, _, err := model.QueryRiskEvents(model.RiskEventQuery{RuleId: rule.Id}, 0, 20)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Evidence)
	version := events[0].Evidence.Version
	assert.Equal(t, 2, events[0].Evidence.Threshold)
	assert.Equal(t, 3, events[0].Evidence.Observed)
	assert.Equal(t, ">", events[0].Evidence.Comparison)
	assert.Equal(t, 1, events[0].Evidence.Offense)
	assert.Greater(t, events[0].Evidence.ExpiresAt, int64(0))
	setting.AutoBanRules[0].Threshold = 20
	preview, err := PreviewRiskRule(setting.AutoBanRules[0], 16, nil)
	require.NoError(t, err)
	assert.NotEqual(t, version, preview.Version)
	event, err := model.GetRiskEventById(events[0].Id)
	require.NoError(t, err)
	assert.Equal(t, 2, event.Evidence.Threshold, "规则修改不会覆盖历史证据")
	require.NoError(t, model.LOG_DB.Callback().Query().Before("gorm:query").Register("risk_scan_query_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			tx.AddError(errors.New("log database unavailable"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.LOG_DB.Callback().Query().Remove("risk_scan_query_failure")) })
	runRiskScan(setting)
	run, err = model.GetLatestRiskScan()
	require.NoError(t, err)
	assert.Equal(t, "failed", run.Status)
	assert.Contains(t, run.Error, "log database unavailable")
}

func TestRiskScanReportsEventWriteFailures(t *testing.T) {
	setupRealtimeGuardTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.RiskScanRun{}))
	rule := operation_setting.RiskAutoBanRule{Id: "audit-rule", Enabled: true, Metric: "ip_multi_user", WindowHours: 24, Threshold: 2, Action: "alert"}
	seedIpLogs(t, "198.51.100.44", []int{10, 11, 12}, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("risk_event_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "risk_events" {
			tx.AddError(errors.New("event write unavailable"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove("risk_event_audit_failure")) })
	runRiskScan(&operation_setting.RiskControlSetting{Enabled: true, AutoBanRules: []operation_setting.RiskAutoBanRule{rule}})
	run, err := model.GetLatestRiskScan()
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "failed", run.Status)
	assert.Contains(t, run.Error, "event write unavailable")
	assert.Contains(t, GetRiskRuntimeStatus().EventError, "event write unavailable")
}
