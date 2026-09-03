package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func errorGuardTestSetting() *operation_setting.RiskControlSetting {
	return &operation_setting.RiskControlSetting{
		Enabled:                 true,
		ErrorGuardEnabled:       true,
		ErrorGuardDryRun:        false,
		ErrorGuardWindowSeconds: 60,
		ErrorGuardThreshold:     3,
		ErrorGuardStatusCodes:   []int{400, 401},
		IpBanEscalationMinutes:  []int{5, 30, 1440},
		IpBanPermanentOffense:   0,
	}
}

func TestRecordErrorGuardEventWindowAndCooldown(t *testing.T) {
	resetRealtimeGuardState()
	setting := errorGuardTestSetting()
	base := time.Unix(1700000000, 0)

	// 阈值 3:前两次不触发,第三次触发
	count, triggered := recordErrorGuardEvent(setting, "8.8.8.8", 400, base)
	assert.Equal(t, 1, count)
	assert.False(t, triggered)
	_, triggered = recordErrorGuardEvent(setting, "8.8.8.8", 401, base.Add(time.Second))
	assert.False(t, triggered)
	count, triggered = recordErrorGuardEvent(setting, "8.8.8.8", 400, base.Add(2*time.Second))
	assert.Equal(t, 3, count)
	assert.True(t, triggered, "同一 IP 的错误累计到阈值即触发,不要求状态码相同")

	// 冷却期内继续报错不重复触发
	_, triggered = recordErrorGuardEvent(setting, "8.8.8.8", 400, base.Add(3*time.Second))
	assert.False(t, triggered)

	// 冷却期(60 秒)过后再次凑满 → 再次触发
	_, _ = recordErrorGuardEvent(setting, "8.8.8.8", 400, base.Add(80*time.Second))
	_, _ = recordErrorGuardEvent(setting, "8.8.8.8", 400, base.Add(81*time.Second))
	_, triggered = recordErrorGuardEvent(setting, "8.8.8.8", 400, base.Add(82*time.Second))
	assert.True(t, triggered)

	// 窗口滑出:超过 60 秒的错误不再计入
	count, _ = recordErrorGuardEvent(setting, "9.9.9.9", 400, base)
	assert.Equal(t, 1, count)
	count, _ = recordErrorGuardEvent(setting, "9.9.9.9", 400, base.Add(2*time.Minute))
	assert.Equal(t, 1, count, "60 秒窗口外的错误应被丢弃")

	// 不同 IP 互不影响
	count, _ = recordErrorGuardEvent(setting, "7.7.7.7", 400, base)
	assert.Equal(t, 1, count)
}

func TestMatchErrorGuardStatusCode(t *testing.T) {
	cases := []struct {
		name       string
		configured []int
		statusCode int
		want       bool
	}{
		{name: "显式配置命中", configured: []int{400, 401}, statusCode: 400, want: true},
		{name: "显式配置未命中", configured: []int{400, 401}, statusCode: 429, want: false},
		{name: "未配置时用默认集合", configured: nil, statusCode: 401, want: true},
		{name: "默认集合不含 5xx", configured: nil, statusCode: 500, want: false},
		{name: "默认集合不含 2xx", configured: nil, statusCode: 200, want: false},
		{name: "非法配置回退默认集合", configured: []int{200, 999}, statusCode: 404, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := &operation_setting.RiskControlSetting{ErrorGuardStatusCodes: tc.configured}
			assert.Equal(t, tc.want, matchErrorGuardStatusCode(setting, tc.statusCode))
		})
	}
}

func TestRecordErrorGuardResponseBansIp(t *testing.T) {
	setupRealtimeGuardTest(t)
	operation_setting.SetRiskControlSettingForTest(errorGuardTestSetting())

	c := guardTestContext("203.0.113.20")
	c.Set("id", 0) // 匿名请求(认证失败),没有用户身份
	for i := 0; i < 3; i++ {
		RecordErrorGuardResponse(c, 401)
	}

	ban, matched := model.MatchActiveIpBan("203.0.113.20")
	require.True(t, matched, "达到阈值后来源 IP 被封禁")
	// 阶梯首档 5 分钟
	remaining := ban.ExpiresAt - time.Now().Unix()
	assert.Greater(t, remaining, int64(0))
	assert.LessOrEqual(t, remaining, int64(5*60))

	events, total, err := model.GetRiskEvents(model.RiskEventBanIp, 0, "203.0.113.20", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, model.IpBanSourceErrorGuard, events[0].Rule)
}

func TestRecordErrorGuardResponseDryRunOnlyAlerts(t *testing.T) {
	setupRealtimeGuardTest(t)
	setting := errorGuardTestSetting()
	setting.ErrorGuardDryRun = true
	operation_setting.SetRiskControlSettingForTest(setting)

	c := guardTestContext("203.0.113.21")
	c.Set("id", 0)
	for i := 0; i < 3; i++ {
		RecordErrorGuardResponse(c, 400)
	}

	_, matched := model.MatchActiveIpBan("203.0.113.21")
	assert.False(t, matched, "演练模式不应封禁 IP")

	_, total, err := model.GetRiskEvents(model.RiskEventAlert, 0, "203.0.113.21", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "演练模式应记录告警事件")
}

func TestRecordErrorGuardResponseSkipPaths(t *testing.T) {
	setupRealtimeGuardTest(t)

	// 未关注的状态码不计入
	operation_setting.SetRiskControlSettingForTest(errorGuardTestSetting())
	c := guardTestContext("203.0.113.22")
	c.Set("id", 0)
	for i := 0; i < 5; i++ {
		RecordErrorGuardResponse(c, 429)
	}
	_, matched := model.MatchActiveIpBan("203.0.113.22")
	assert.False(t, matched, "状态码不在关注集合内")

	// 风控自身的拒绝不计入,否则封禁会自我延长
	resetRealtimeGuardState()
	blocked := guardTestContext("203.0.113.23")
	blocked.Set("id", 0)
	blocked.Set(string(constant.ContextKeyRiskBlocked), true)
	for i := 0; i < 5; i++ {
		RecordErrorGuardResponse(blocked, 401)
	}
	_, matched = model.MatchActiveIpBan("203.0.113.23")
	assert.False(t, matched, "被风控拒绝的请求不参与错误计数")

	// 私网地址不参与
	resetRealtimeGuardState()
	private := guardTestContext("192.168.1.9")
	private.Set("id", 0)
	for i := 0; i < 5; i++ {
		RecordErrorGuardResponse(private, 401)
	}
	_, matched = model.MatchActiveIpBan("192.168.1.9")
	assert.False(t, matched, "私网地址没有封禁意义")

	// 白名单用户豁免
	resetRealtimeGuardState()
	setting := errorGuardTestSetting()
	setting.WhitelistUserIds = []int{42}
	operation_setting.SetRiskControlSettingForTest(setting)
	whitelisted := guardTestContext("203.0.113.24") // guardTestContext 设置 id=42
	for i := 0; i < 5; i++ {
		RecordErrorGuardResponse(whitelisted, 401)
	}
	_, matched = model.MatchActiveIpBan("203.0.113.24")
	assert.False(t, matched, "白名单用户不触发处置")

	// 总开关或 Error Guard 关闭时零处理
	resetRealtimeGuardState()
	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{Enabled: true})
	disabled := guardTestContext("203.0.113.25")
	disabled.Set("id", 0)
	for i := 0; i < 5; i++ {
		RecordErrorGuardResponse(disabled, 401)
	}
	_, matched = model.MatchActiveIpBan("203.0.113.25")
	assert.False(t, matched, "Error Guard 未启用")
}

func TestEscalateIpBanCustomLadder(t *testing.T) {
	setupRealtimeGuardTest(t)
	// 5 分钟 → 30 分钟 → 1 天,且永不升级为永久
	operation_setting.SetRiskControlSettingForTest(errorGuardTestSetting())

	const target = "198.51.100.31"
	steps := []struct {
		minutes  int64
		wantText string
	}{
		{minutes: 5, wantText: "5 分钟"},
		{minutes: 30, wantText: "30 分钟"},
		{minutes: 1440, wantText: "1440 分钟"},
	}
	for offense, step := range steps {
		action, err := EscalateIpBan(target, "违规", model.IpBanSourceErrorGuard, 0)
		require.NoError(t, err, "第 %d 次升级", offense+1)
		assert.Contains(t, action, step.wantText, "第 %d 次应落在对应阶梯", offense+1)

		ban, matched := model.MatchActiveIpBan(target)
		require.True(t, matched)
		require.NotEqualValues(t, 0, ban.ExpiresAt, "permanent_offense=0 时永不永久封禁")
		remaining := ban.ExpiresAt - time.Now().Unix()
		assert.Greater(t, remaining, int64(0))
		assert.LessOrEqual(t, remaining, step.minutes*60)
	}

	// 违规次数超出阶梯长度时停在最后一档:预置 4 条历史封禁事件后触发第 5 次
	const repeat = "198.51.100.32"
	for i := 0; i < 4; i++ {
		require.NoError(t, model.InsertRiskEvent(&model.RiskEvent{
			EventType: model.RiskEventBanIp,
			Ip:        repeat,
		}))
	}
	action, err := EscalateIpBan(repeat, "违规", model.IpBanSourceErrorGuard, 0)
	require.NoError(t, err)
	assert.Contains(t, action, "1440 分钟")
	assert.Contains(t, action, "第 5 次")
}

// TestErrorGuardPublicKeyAccountBansIpOnly 覆盖公开/共享密钥的核心场景:
// 账号主人不该因别人滥用而被禁用,但滥用者的来源 IP 必须被封。
func TestErrorGuardPublicKeyAccountBansIpOnly(t *testing.T) {
	setupRealtimeGuardTest(t)

	user := &model.User{
		Username: "public_key_owner",
		Password: "12345678",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)

	setting := errorGuardTestSetting()
	setting.ErrorGuardAction = operation_setting.RiskRuleActionBanBoth
	setting.PublicKeyUserIds = []int{user.Id}
	operation_setting.SetRiskControlSettingForTest(setting)

	c := guardTestContext("203.0.113.40")
	c.Set("id", user.Id)
	for i := 0; i < 3; i++ {
		RecordErrorGuardResponse(c, 401)
	}

	_, matched := model.MatchActiveIpBan("203.0.113.40")
	assert.True(t, matched, "公开密钥账号不豁免 IP 封禁")

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, saved.Status, "公开密钥账号豁免账号禁用")
	assert.Empty(t, saved.DisableReason)
}

func TestErrorGuardBanBothDisablesRegularUser(t *testing.T) {
	setupRealtimeGuardTest(t)

	user := &model.User{
		Username: "abuser",
		Password: "12345678",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)

	setting := errorGuardTestSetting()
	setting.ErrorGuardAction = operation_setting.RiskRuleActionBanBoth
	operation_setting.SetRiskControlSettingForTest(setting)

	c := guardTestContext("203.0.113.41")
	c.Set("id", user.Id)
	for i := 0; i < 3; i++ {
		RecordErrorGuardResponse(c, 401)
	}

	_, matched := model.MatchActiveIpBan("203.0.113.41")
	assert.True(t, matched, "ban_both 封禁来源 IP")

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, saved.Status, "ban_both 同时禁用普通账号")
	assert.NotEmpty(t, saved.DisableReason)
}

func TestErrorGuardActionAlertDoesNotBan(t *testing.T) {
	setupRealtimeGuardTest(t)
	setting := errorGuardTestSetting()
	setting.ErrorGuardAction = operation_setting.RiskRuleActionAlert
	operation_setting.SetRiskControlSettingForTest(setting)

	c := guardTestContext("203.0.113.42")
	c.Set("id", 0)
	for i := 0; i < 3; i++ {
		RecordErrorGuardResponse(c, 401)
	}

	_, matched := model.MatchActiveIpBan("203.0.113.42")
	assert.False(t, matched, "动作为仅告警时不封禁")

	_, total, err := model.GetRiskEvents(model.RiskEventAlert, 0, "203.0.113.42", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "应记录告警事件")
}

func TestEscalateIpBanFixedMinutesSkipsLadder(t *testing.T) {
	setupRealtimeGuardTest(t)
	// 阶梯为 5/30/1440,固定时长应完全覆盖它,且不因累犯而递增
	operation_setting.SetRiskControlSettingForTest(errorGuardTestSetting())

	const target = "198.51.100.50"
	for offense := 1; offense <= 2; offense++ {
		// 每次都预置一条历史事件,制造"累犯"
		require.NoError(t, model.InsertRiskEvent(&model.RiskEvent{
			EventType: model.RiskEventBanIp,
			Ip:        target,
		}))
		action, err := EscalateIpBan(target, "违规", model.IpBanSourceErrorGuard, 7)
		require.NoError(t, err)
		if action == "" {
			continue // 同一时长的重复处置是幂等的
		}
		assert.Contains(t, action, "7 分钟", "固定时长不参与累犯升级")
	}

	ban, matched := model.MatchActiveIpBan(target)
	require.True(t, matched)
	remaining := ban.ExpiresAt - time.Now().Unix()
	assert.Greater(t, remaining, int64(0))
	assert.LessOrEqual(t, remaining, int64(7*60), "始终是固定的 7 分钟档")
}

func TestAccountBanExemptionMatrix(t *testing.T) {
	setting := &operation_setting.RiskControlSetting{
		WhitelistUserIds: []int{1},
		PublicKeyUserIds: []int{2},
	}

	cases := []struct {
		name           string
		userId         int
		wantWhitelist  bool
		wantAccountFre bool
	}{
		{name: "完全白名单:两者都豁免", userId: 1, wantWhitelist: true, wantAccountFre: true},
		{name: "公开密钥账号:只豁免账号处置", userId: 2, wantWhitelist: false, wantAccountFre: true},
		{name: "普通用户:都不豁免", userId: 3, wantWhitelist: false, wantAccountFre: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantWhitelist, isRiskWhitelisted(setting, tc.userId))
			assert.Equal(t, tc.wantAccountFre, isAccountBanExempt(setting, tc.userId))
		})
	}
}

func TestResolvedIpBanEscalationMinutesFallback(t *testing.T) {
	cases := []struct {
		name    string
		setting *operation_setting.RiskControlSetting
		want    []int
	}{
		{
			name:    "显式阶梯优先",
			setting: &operation_setting.RiskControlSetting{IpBanEscalationMinutes: []int{5, 30, 1440}},
			want:    []int{5, 30, 1440},
		},
		{
			name:    "未配置阶梯时回退旧的两档配置",
			setting: &operation_setting.RiskControlSetting{IpBanFirstMinutes: 10, IpBanSecondMinutes: 60},
			want:    []int{10, 60},
		},
		{
			name:    "阶梯内非法值被剔除",
			setting: &operation_setting.RiskControlSetting{IpBanEscalationMinutes: []int{0, 15, -3}},
			want:    []int{15},
		},
		{
			name:    "阶梯全部非法时回退默认两档",
			setting: &operation_setting.RiskControlSetting{IpBanEscalationMinutes: []int{0, -1}},
			want: []int{
				operation_setting.RiskDefaultIpBanFirstMinutes,
				operation_setting.RiskDefaultIpBanSecondMinutes,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.setting.ResolvedIpBanEscalationMinutes())
		})
	}
}
