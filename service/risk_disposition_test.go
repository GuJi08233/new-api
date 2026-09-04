package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDisableUserForRiskRecordsExemptSkip 覆盖账号处置的豁免契约:
// 用户级白名单账号不被自动禁用,且跳过必须留下告警 —— 否则规则对着一个
// 永远处置不了的账号空转而无从发现。
func TestDisableUserForRiskRecordsExemptSkip(t *testing.T) {
	setupRealtimeGuardTest(t)

	user := &model.User{
		Username: "protected",
		Password: "12345678",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)

	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{
		Enabled:          true,
		PublicKeyUserIds: []int{user.Id},
	})

	require.NoError(t, DisableUserForRisk(user.Id, "微量请求超阈值", model.RiskBanSourceAutoRule, 0, "203.0.113.5"))

	var saved model.User
	require.NoError(t, model.DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, saved.Status)

	// 账号未被禁用,因此不该有自动封禁事件
	_, total, err := model.GetRiskEvents(model.RiskEventBanAuto, user.Id, "", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)

	events, total, err := model.GetRiskEvents(model.RiskEventAlert, user.Id, "203.0.113.5", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Contains(t, events[0].Reason, "已跳过")
}
