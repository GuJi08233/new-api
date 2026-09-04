package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createRiskTestUser 建一个可被风控处置的普通账号。
func createRiskTestUser(t *testing.T, username string) *model.User {
	t.Helper()
	user := &model.User{
		Username: username,
		Password: "12345678",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

// seedUserLog 为账号写一条消费日志,作为「上次处置之后仍在活动」的证据。
func seedUserLog(t *testing.T, userId int, createdAt int64) {
	t.Helper()
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    userId,
		Username:  "risky",
		CreatedAt: createdAt,
		Type:      model.LogTypeConsume,
		Ip:        "198.51.100.90",
	}).Error)
}

func userBanTestSetting() *operation_setting.RiskControlSetting {
	return &operation_setting.RiskControlSetting{
		Enabled:                true,
		IpBanEscalationMinutes: []int{10, 60},
		IpBanPermanentOffense:  3,
	}
}

// TestEscalateUserBanFollowsLadder 覆盖账号封禁的阶梯语义:临时禁用带到期时间、
// 到期后由后台任务恢复、再犯逐级加时,达到配置次数后转为不再自动恢复的永久禁用。
func TestEscalateUserBanFollowsLadder(t *testing.T) {
	setupRealtimeGuardTest(t)
	setting := userBanTestSetting()
	operation_setting.SetRiskControlSettingForTest(setting)

	user := createRiskTestUser(t, "ladder-victim")

	// 首次违规:阶梯第一级 10 分钟
	require.NoError(t, escalateUserBan(setting, user.Id, "微量请求超阈值",
		model.RiskBanSourceProbeGuard, 0, "203.0.113.30", ""))

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, saved.Status)
	assert.Contains(t, saved.DisableReason, "临时禁用 10 分钟(首次违规)")
	assert.InDelta(t, time.Now().Add(10*time.Minute).Unix(), saved.DisableExpiresAt, 5)

	// 未到期时后台任务不放人
	restoreExpiredUserBans(time.Now())
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, saved.Status)

	// 到期后恢复:状态回到启用,封禁原因与到期时间一并清空
	restoreExpiredUserBans(time.Now().Add(11 * time.Minute))
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusEnabled, saved.Status)
	assert.Empty(t, saved.DisableReason)
	assert.Zero(t, saved.DisableExpiresAt)

	_, total, err := model.GetRiskEvents(model.RiskEventUnbanAuto, user.Id, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "自动解禁应留下审计记录")

	// 第二次违规:阶梯第二级 60 分钟
	require.NoError(t, escalateUserBan(setting, user.Id, "微量请求超阈值",
		model.RiskBanSourceProbeGuard, 0, "203.0.113.30", ""))
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Contains(t, saved.DisableReason, "临时禁用 60 分钟(第 2 次违规)")
	assert.InDelta(t, time.Now().Add(60*time.Minute).Unix(), saved.DisableExpiresAt, 5)

	// 第三次违规:达到永久次数,不再自动恢复
	restoreExpiredUserBans(time.Now().Add(61 * time.Minute))
	require.NoError(t, escalateUserBan(setting, user.Id, "微量请求超阈值",
		model.RiskBanSourceProbeGuard, 0, "203.0.113.30", ""))
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, saved.Status)
	assert.Contains(t, saved.DisableReason, "永久禁用(第 3 次违规)")
	assert.Zero(t, saved.DisableExpiresAt)

	restoreExpiredUserBans(time.Now().Add(365 * 24 * time.Hour))
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, saved.Status, "永久禁用不该被到期任务放出来")
}

// TestEscalateUserBanUsesRuleFixedMinutes 覆盖规则级固定时长:填了时长就用它,不走阶梯。
func TestEscalateUserBanUsesRuleFixedMinutes(t *testing.T) {
	setupRealtimeGuardTest(t)
	setting := userBanTestSetting()
	operation_setting.SetRiskControlSettingForTest(setting)

	user := createRiskTestUser(t, "fixed-victim")
	require.NoError(t, escalateUserBan(setting, user.Id, "错误请求超阈值",
		model.RiskBanSourceErrorGuard, 30, "203.0.113.31", ""))

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Contains(t, saved.DisableReason, "临时禁用 30 分钟(第 1 次违规,规则固定时长)")
	assert.InDelta(t, time.Now().Add(30*time.Minute).Unix(), saved.DisableExpiresAt, 5)
}

// TestScanUserRuleEscalatesOnlyOnNewActivity 覆盖账号侧扫描规则的累犯语义,
// 与 IP 侧一致:临时禁用到期后,同一批旧日志再次被扫到不算再犯,
// 否则一次滥用会在每次到期后把账号再推一级,直到永久。
func TestScanUserRuleEscalatesOnlyOnNewActivity(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(userBanTestSetting())

	user := createRiskTestUser(t, "scan-victim")
	rule := operation_setting.RiskAutoBanRule{
		Enabled:     true,
		Metric:      operation_setting.RiskMetricUserErrorBurst,
		WindowHours: 24,
		Threshold:   1,
		Action:      operation_setting.RiskRuleActionDisableUser,
	}
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    user.Id,
		Username:  user.Username,
		CreatedAt: time.Now().Add(-time.Hour).Unix(),
		Type:      model.LogTypeError,
		Ip:        "198.51.100.90",
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    user.Id,
		Username:  user.Username,
		CreatedAt: time.Now().Add(-time.Hour).Unix(),
		Type:      model.LogTypeError,
		Ip:        "198.51.100.90",
	}).Error)

	scanUserErrorBurst(rule, rule.WindowHours, nil)
	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, saved.Status)
	assert.Contains(t, saved.DisableReason, "首次违规")

	// 到期恢复后再扫一轮:证据还是那批旧日志,不重新禁用
	restoreExpiredUserBans(time.Now().Add(11 * time.Minute))
	scanUserErrorBurst(rule, rule.WindowHours, nil)
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, saved.Status, "同一批旧证据不构成再犯")

	// 上次处置之后出现新流量:这才是累犯,升到第 2 档
	seedUserLog(t, user.Id, time.Now().Unix()+1)
	scanUserErrorBurst(rule, rule.WindowHours, nil)
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, saved.Status)
	assert.Contains(t, saved.DisableReason, "第 2 次违规")
}

// TestRealtimeGuardBansIpButSparesAccountWhitelistedUser 覆盖两级白名单的分工:
// 用户级白名单只保护账号,来源 IP 照封 —— 这正是共享密钥场景要的效果。
func TestRealtimeGuardBansIpButSparesAccountWhitelistedUser(t *testing.T) {
	setupRealtimeGuardTest(t)
	user := createRiskTestUser(t, "shared-key-owner")

	setting := probeGuardTestSetting()
	setting.ProbeGuardAction = operation_setting.RiskRuleActionBanBoth
	setting.PublicKeyUserIds = []int{user.Id}
	operation_setting.SetRiskControlSettingForTest(setting)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.RemoteAddr = "203.0.113.40:12345"
	c.Set("id", user.Id)

	require.NoError(t, CheckProbeGuard(c, "model-a"))
	require.NoError(t, CheckProbeGuard(c, "model-b"))
	require.ErrorIs(t, CheckProbeGuard(c, "model-c"), ErrProbeGuardBlocked)

	_, matched := model.MatchActiveIpBan("203.0.113.40")
	assert.True(t, matched, "用户级白名单不豁免 IP 封禁")

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, saved.Status, "用户级白名单账号不该被禁用")

	events, total, err := model.GetRiskEvents(model.RiskEventAlert, user.Id, "203.0.113.40", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "跳过账号处置必须留下告警")
	assert.Contains(t, events[0].Reason, "白名单")
}
