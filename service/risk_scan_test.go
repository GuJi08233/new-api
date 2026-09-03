package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedIpLogs 为一组用户在同一 IP 上各写一条消费日志。
func seedIpLogs(t *testing.T, ip string, userIds []int, createdAt int64) {
	t.Helper()
	for _, userId := range userIds {
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			UserId:    userId,
			Username:  fmt.Sprintf("u%d", userId),
			CreatedAt: createdAt,
			Type:      model.LogTypeConsume,
			Ip:        ip,
		}).Error)
	}
}

// TestScanIpRuleEscalatesOnlyOnNewActivity 覆盖扫描规则的累犯语义。
// 扫描的证据是窗口内的日志行,同一批行会在之后的每轮扫描反复命中:封禁中再扫到
// 不延长,到期后再扫到同一批旧证据也不算再犯——否则默认配置下一次滥用会在三轮扫描内
// 被推到永久;只有上次封禁之后出现新流量才升级。
func TestScanIpRuleEscalatesOnlyOnNewActivity(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(probeGuardTestSetting())

	const ip = "198.51.100.70"
	rule := operation_setting.RiskAutoBanRule{
		Enabled:     true,
		Metric:      operation_setting.RiskMetricIpMultiUser,
		WindowHours: 24,
		Threshold:   2,
		Action:      operation_setting.RiskRuleActionBanIp,
	}
	// 三个用户在同一 IP 上留下日志,超过阈值 2
	seedIpLogs(t, ip, []int{101, 102, 103}, time.Now().Add(-time.Hour).Unix())

	scanIpMultiUser(rule, rule.WindowHours, nil)
	ban, matched := model.MatchActiveIpBan(ip)
	require.True(t, matched, "首轮扫描封禁")
	firstExpiry := ban.ExpiresAt
	events, total, err := model.GetRiskEvents(model.RiskEventBanIp, 0, ip, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Contains(t, events[0].Reason, "首次")

	// 封禁未到期再扫一轮:不延长、不记事件
	scanIpMultiUser(rule, rule.WindowHours, nil)
	ban, matched = model.MatchActiveIpBan(ip)
	require.True(t, matched)
	assert.Equal(t, firstExpiry, ban.ExpiresAt)
	_, total, err = model.GetRiskEvents(model.RiskEventBanIp, 0, ip, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// 到期后再扫:证据还是那批旧日志,不重新封禁
	expireIpBanForTest(t, ip)
	scanIpMultiUser(rule, rule.WindowHours, nil)
	_, matched = model.MatchActiveIpBan(ip)
	assert.False(t, matched, "同一批旧证据不构成再犯")
	_, total, err = model.GetRiskEvents(model.RiskEventBanIp, 0, ip, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// 上次封禁之后出现新流量:这才是累犯,升到第 2 档
	seedIpLogs(t, ip, []int{104}, time.Now().Unix()+1)
	scanIpMultiUser(rule, rule.WindowHours, nil)
	ban, matched = model.MatchActiveIpBan(ip)
	require.True(t, matched, "有新活动后重新封禁")
	assert.Greater(t, ban.ExpiresAt, firstExpiry)
	events, total, err = model.GetRiskEvents(model.RiskEventBanIp, 0, ip, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	assert.Contains(t, events[0].Reason, "第 2 次")
}
