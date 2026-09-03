package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRealtimeGuardTest 迁移相关表并重置实时防护(Probe / Error Guard)的内存状态、
// IP 封禁缓存与配置快照。两个 guard 共享同一套环境需求。
func setupRealtimeGuardTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.RiskEvent{}, &model.IpBan{}, &model.User{}))
	require.NoError(t, model.DB.Exec("DELETE FROM risk_events").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM ip_bans").Error)
	// 账号处置用例会写 users;不清理会与按固定主键建用户的其它测试冲突
	require.NoError(t, model.DB.Exec("DELETE FROM users").Error)
	// 白名单地址豁免用例靠 logs 判定该地址是否有白名单账号在用
	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, model.ReloadIpBanCache())
	resetRealtimeGuardState()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM risk_events")
		model.DB.Exec("DELETE FROM ip_bans")
		model.DB.Exec("DELETE FROM users")
		model.LOG_DB.Exec("DELETE FROM logs")
		_ = model.ReloadIpBanCache()
		resetRealtimeGuardState()
		operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{})
	})
}

func resetRealtimeGuardState() {
	probeGuardWindow.reset()
	errorGuardWindow.reset()
}

func probeGuardTestSetting() *operation_setting.RiskControlSetting {
	return &operation_setting.RiskControlSetting{
		Enabled:                 true,
		ProbeGuardEnabled:       true,
		ProbeGuardDryRun:        false,
		ProbeGuardWindowSeconds: 60,
		ProbeGuardModelCount:    3,
		IpBanFirstMinutes:       10,
		IpBanSecondMinutes:      60,
		IpBanPermanentOffense:   3,
	}
}

func guardTestContext(remoteIP string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.RemoteAddr = remoteIP + ":12345"
	c.Set("id", 42)
	return c
}

func TestRecordProbeGuardRequestWindowAndCooldown(t *testing.T) {
	resetRealtimeGuardState()
	setting := probeGuardTestSetting()
	base := time.Unix(1700000000, 0)

	// 同一模型反复请求不触发(distinct 数不涨)
	for i := 0; i < 10; i++ {
		distinct, triggered := recordProbeGuardRequest(setting, "8.8.8.8", "gpt-4o", base.Add(time.Duration(i)*time.Second))
		assert.Equal(t, 1, distinct)
		assert.False(t, triggered)
	}

	// 不同模型数达到阈值(3)时触发
	_, triggered := recordProbeGuardRequest(setting, "8.8.8.8", "claude-3", base.Add(11*time.Second))
	assert.False(t, triggered)
	distinct, triggered := recordProbeGuardRequest(setting, "8.8.8.8", "gemini-pro", base.Add(12*time.Second))
	assert.Equal(t, 3, distinct)
	assert.True(t, triggered)

	// 冷却期内再次达到阈值不重复触发
	_, triggered = recordProbeGuardRequest(setting, "8.8.8.8", "gpt-4o-mini", base.Add(13*time.Second))
	assert.False(t, triggered)

	// 冷却期(60 秒)过后窗口内再次凑满不同模型 → 再次触发
	_, _ = recordProbeGuardRequest(setting, "8.8.8.8", "m-a", base.Add(80*time.Second))
	_, _ = recordProbeGuardRequest(setting, "8.8.8.8", "m-b", base.Add(81*time.Second))
	_, triggered = recordProbeGuardRequest(setting, "8.8.8.8", "m-c", base.Add(82*time.Second))
	assert.True(t, triggered)

	// 窗口滑出:旧模型不再计入
	distinct, _ = recordProbeGuardRequest(setting, "9.9.9.9", "m1", base)
	assert.Equal(t, 1, distinct)
	distinct, _ = recordProbeGuardRequest(setting, "9.9.9.9", "m2", base.Add(2*time.Minute))
	assert.Equal(t, 1, distinct, "60 秒窗口外的模型应被丢弃")

	// 不同 IP 互不影响
	distinct, _ = recordProbeGuardRequest(setting, "7.7.7.7", "m1", base)
	assert.Equal(t, 1, distinct)
}

func TestCheckProbeGuardBansIpAndBlocks(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(probeGuardTestSetting())

	c := guardTestContext("203.0.113.9")
	require.NoError(t, CheckProbeGuard(c, "model-a"))
	require.NoError(t, CheckProbeGuard(c, "model-b"))
	err := CheckProbeGuard(c, "model-c")
	require.ErrorIs(t, err, ErrProbeGuardBlocked)

	// 来源 IP 被临时封禁并写入 ban_ip 事件
	ban, matched := model.MatchActiveIpBan("203.0.113.9")
	require.True(t, matched)
	assert.Greater(t, ban.ExpiresAt, time.Now().Unix())

	events, total, err := model.GetRiskEvents(model.RiskEventBanIp, 0, "203.0.113.9", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, model.IpBanSourceProbeGuard, events[0].Rule)
}

func TestCheckProbeGuardDryRunOnlyAlerts(t *testing.T) {
	setupRealtimeGuardTest(t)
	setting := probeGuardTestSetting()
	setting.ProbeGuardDryRun = true
	operation_setting.SetRiskControlSettingForTest(setting)

	c := guardTestContext("203.0.113.10")
	require.NoError(t, CheckProbeGuard(c, "model-a"))
	require.NoError(t, CheckProbeGuard(c, "model-b"))
	require.NoError(t, CheckProbeGuard(c, "model-c"), "演练模式不应拒绝请求")

	_, matched := model.MatchActiveIpBan("203.0.113.10")
	assert.False(t, matched, "演练模式不应封禁 IP")

	_, total, err := model.GetRiskEvents(model.RiskEventAlert, 0, "203.0.113.10", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "演练模式应记录告警事件")
}

func TestCheckProbeGuardSkipsPrivateIpAndDisabled(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(probeGuardTestSetting())

	// 私网 IP 不参与检测
	c := guardTestContext("192.168.1.5")
	require.NoError(t, CheckProbeGuard(c, "model-a"))
	require.NoError(t, CheckProbeGuard(c, "model-b"))
	require.NoError(t, CheckProbeGuard(c, "model-c"))

	// 未启用时零处理
	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{Enabled: true})
	c2 := guardTestContext("203.0.113.11")
	require.NoError(t, CheckProbeGuard(c2, "model-a"))
	require.NoError(t, CheckProbeGuard(c2, "model-b"))
	require.NoError(t, CheckProbeGuard(c2, "model-c"))
}

func TestEscalateIpBanLadder(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(probeGuardTestSetting())

	// 首次违规:临时封禁 first_minutes
	action, err := EscalateIpBan("198.51.100.7", "违规一", model.IpBanSourceAutoRule, 0)
	require.NoError(t, err)
	assert.Contains(t, action, "首次")
	ban, matched := model.MatchActiveIpBan("198.51.100.7")
	require.True(t, matched)
	firstExpiry := ban.ExpiresAt
	assert.Greater(t, firstExpiry, time.Now().Unix())

	// 第二次违规:加时至 second_minutes
	action, err = EscalateIpBan("198.51.100.7", "违规二", model.IpBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.Contains(t, action, "第 2 次")
	ban, matched = model.MatchActiveIpBan("198.51.100.7")
	require.True(t, matched)
	assert.Greater(t, ban.ExpiresAt, firstExpiry)

	// 第三次违规:永久
	action, err = EscalateIpBan("198.51.100.7", "违规三", model.IpBanSourceAutoRule, 0)
	require.NoError(t, err)
	assert.Contains(t, action, "永久")
	ban, matched = model.MatchActiveIpBan("198.51.100.7")
	require.True(t, matched)
	assert.EqualValues(t, 0, ban.ExpiresAt)

	// 永久封禁后再触发:幂等,不再写新事件
	action, err = EscalateIpBan("198.51.100.7", "违规四", model.IpBanSourceAutoRule, 0)
	require.NoError(t, err)
	assert.Empty(t, action)

	_, total, err := model.GetRiskEvents(model.RiskEventBanIp, 0, "198.51.100.7", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total, "三次升级各写一条 ban_ip 事件,幂等触发不追加")
}

// TestEscalateIpBanSkipsWhitelistedIp 覆盖全局白名单的兜底:该地址近期有白名单账号
// 在用时,所有自动封禁路径都放弃封禁并留下告警,避免封掉运营者自己的出口地址后
// 连带拦下同出口的其他正常用户。
func TestEscalateIpBanSkipsWhitelistedIp(t *testing.T) {
	setupRealtimeGuardTest(t)

	setting := probeGuardTestSetting()
	setting.WhitelistUserIds = []int{9}
	operation_setting.SetRiskControlSettingForTest(setting)

	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    9,
		Username:  "owner",
		CreatedAt: time.Now().Add(-time.Hour).Unix(),
		Type:      model.LogTypeConsume,
		Ip:        "198.51.100.9",
	}).Error)

	action, err := EscalateIpBan("198.51.100.9", "疑似批量测活", model.IpBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.Empty(t, action, "白名单账号在用的地址不产生封禁动作")
	_, matched := model.MatchActiveIpBan("198.51.100.9")
	assert.False(t, matched)

	// 跳过不是静默的:记一条告警,便于发现规则在空转
	events, total, err := model.GetRiskEvents(model.RiskEventAlert, 0, "198.51.100.9", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Contains(t, events[0].Reason, "跳过封禁")

	// 规则固定时长同样受豁免约束,不能绕过
	action, err = EscalateIpBan("198.51.100.9", "疑似批量测活", model.IpBanSourceAutoRule, 30)
	require.NoError(t, err)
	assert.Empty(t, action)
	_, matched = model.MatchActiveIpBan("198.51.100.9")
	assert.False(t, matched)

	// 没有白名单账号在用的地址照常封禁
	action, err = EscalateIpBan("198.51.100.10", "疑似批量测活", model.IpBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, action)
	_, matched = model.MatchActiveIpBan("198.51.100.10")
	assert.True(t, matched)
}

func TestCheckRequestRiskBlocksActiveBanRegardlessOfSwitch(t *testing.T) {
	setupRealtimeGuardTest(t)

	_, _, err := model.UpsertIpBan("203.0.113.77", "动态封禁", 0, model.IpBanSourceManual, 1)
	require.NoError(t, err)

	// 风控总开关关闭:动态封禁仍生效
	c := guardTestContext("203.0.113.77")
	c.Request.Header.Set("User-Agent", "any-client")
	err = checkRequestRisk(c, &operation_setting.RiskControlSetting{Enabled: false})
	assert.ErrorIs(t, err, ErrRiskBlocked)

	// 白名单用户豁免动态封禁
	whitelisted := &operation_setting.RiskControlSetting{Enabled: false, WhitelistUserIds: []int{42}}
	c2 := guardTestContext("203.0.113.77")
	assert.NoError(t, checkRequestRisk(c2, whitelisted))
}
