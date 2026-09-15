package model

import (
	"testing"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 密钥级自动恢复契约：只清除目标密钥的禁用记录并持久化；渠道因"所有密钥被禁"处于自动禁用时
// 随之恢复启用并同步 abilities；渠道已启用时密钥恢复同样落库；越界索引报错。
func TestEnableChannelKey(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)

	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })

	channel := &Channel{
		Id:     1,
		Name:   "multi-key",
		Key:    "k1\nk2\nk3",
		Status: common.ChannelStatusAutoDisabled,
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
			MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusAutoDisabled,
				2: common.ChannelStatusManuallyDisabled,
			},
			MultiKeyDisabledReason: map[int]string{0: "429", 1: "429", 2: "manual"},
			MultiKeyDisabledTime:   map[int]int64{0: 100, 1: 200, 2: 300},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, UpdateAbilityStatus(1, false))

	keyChanged, channelRecovered, err := EnableChannelKey(1, 0)
	require.NoError(t, err)
	assert.True(t, keyChanged)
	assert.True(t, channelRecovered, "first recovered key must bring an auto-disabled channel back")

	var got Channel
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.Equal(t, map[int]int{1: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusManuallyDisabled}, got.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, map[int]string{1: "429", 2: "manual"}, got.ChannelInfo.MultiKeyDisabledReason)
	assert.Equal(t, map[int]int64{1: 200, 2: 300}, got.ChannelInfo.MultiKeyDisabledTime)
	assert.Equal(t, "k1\nk2\nk3", got.Key, "key column must not be touched")
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", 1).First(&ability).Error)
	assert.True(t, ability.Enabled)

	keyChanged, channelRecovered, err = EnableChannelKey(1, 1)
	require.NoError(t, err)
	assert.True(t, keyChanged, "key recovery on an already enabled channel must still persist")
	assert.False(t, channelRecovered)
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.Equal(t, map[int]int{2: common.ChannelStatusManuallyDisabled}, got.ChannelInfo.MultiKeyStatusList)

	keyChanged, _, err = EnableChannelKey(1, 0)
	require.NoError(t, err)
	assert.False(t, keyChanged, "already enabled key reports no change")

	_, _, err = EnableChannelKey(1, 3)
	require.Error(t, err, "out of range key index must be rejected")
}

// 归档渠道对自动恢复冻结：密钥记录与渠道状态都不得改写；单密钥渠道不存在密钥级恢复。
func TestEnableChannelKeyFrozenAndUnsupported(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })

	archived := &Channel{
		Id:     1,
		Name:   "archived",
		Key:    "k1\nk2",
		Status: common.ChannelStatusArchived,
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, DB.Create(archived).Error)
	keyChanged, channelRecovered, err := EnableChannelKey(1, 0)
	require.NoError(t, err)
	assert.False(t, keyChanged)
	assert.False(t, channelRecovered)
	var got Channel
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusArchived, got.Status)
	assert.Equal(t, map[int]int{0: common.ChannelStatusAutoDisabled}, got.ChannelInfo.MultiKeyStatusList)

	single := &Channel{Id: 2, Name: "single", Key: "k1", Status: common.ChannelStatusAutoDisabled, Models: "gpt-4o", Group: "default"}
	require.NoError(t, DB.Create(single).Error)
	_, _, err = EnableChannelKey(2, 0)
	require.Error(t, err)
}

// 渠道设置保存时的校验契约：自动禁用状态码、自动恢复间隔与每日限额 IANA 时区非法即拒绝。
func TestValidateSettingsAutomationFields(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		wantErr bool
	}{
		{name: "valid automation settings", setting: `{"auto_disable_status_codes":"401,429","auto_recovery_enabled":true,"auto_recovery_interval_minutes":30,"daily_request_limit_timezone":"America/Los_Angeles"}`},
		{name: "valid rate limit settings", setting: `{"rate_limit_period_minutes":5,"rate_limit_max_requests":60,"rate_limit_max_success":30,"daily_request_limit":500}`},
		{name: "blank values are accepted", setting: `{"auto_disable_status_codes":" ","daily_request_limit_timezone":""}`},
		{name: "invalid status codes", setting: `{"auto_disable_status_codes":"4xx"}`, wantErr: true},
		{name: "negative interval", setting: `{"auto_recovery_interval_minutes":-1}`, wantErr: true},
		{name: "interval above one day", setting: `{"auto_recovery_interval_minutes":1441}`, wantErr: true},
		{name: "unknown timezone", setting: `{"daily_request_limit_timezone":"Mars/Olympus"}`, wantErr: true},
		{name: "negative rate limit", setting: `{"rate_limit_max_requests":-1}`, wantErr: true},
		{name: "negative success limit", setting: `{"rate_limit_max_success":-1}`, wantErr: true},
		{name: "rate limit period above one day", setting: `{"rate_limit_period_minutes":1441}`, wantErr: true},
		{name: "negative daily limit", setting: `{"daily_request_limit":-5}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setting := tc.setting
			channel := &Channel{Setting: &setting}
			err := channel.ValidateSettings()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// 自动恢复调度信息只读取三列并就地解析，损坏的设置与未开启的渠道被跳过，手动禁用/归档不参与。
func TestGetChannelAutoRecoverySettings(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	enabledSetting := `{"auto_recovery_enabled":true,"auto_recovery_interval_minutes":5}`
	defaultSetting := `{"auto_recovery_enabled":true}`
	offSetting := `{"auto_recovery_enabled":false}`
	brokenSetting := `{broken`
	for _, channel := range []*Channel{
		{Id: 1, Name: "enabled", Key: "k", Status: common.ChannelStatusEnabled, Setting: &enabledSetting},
		{Id: 2, Name: "auto-disabled", Key: "k", Status: common.ChannelStatusAutoDisabled, Setting: &defaultSetting},
		{Id: 3, Name: "manual", Key: "k", Status: common.ChannelStatusManuallyDisabled, Setting: &enabledSetting},
		{Id: 4, Name: "archived", Key: "k", Status: common.ChannelStatusArchived, Setting: &enabledSetting},
		{Id: 5, Name: "off", Key: "k", Status: common.ChannelStatusEnabled, Setting: &offSetting},
		{Id: 6, Name: "broken", Key: "k", Status: common.ChannelStatusEnabled, Setting: &brokenSetting},
		{Id: 7, Name: "no-setting", Key: "k", Status: common.ChannelStatusEnabled},
	} {
		require.NoError(t, DB.Create(channel).Error)
	}

	settings, err := GetChannelAutoRecoverySettings()
	require.NoError(t, err)
	require.Len(t, settings, 2)
	assert.Equal(t, 1, settings[0].ChannelId)
	assert.Equal(t, 5.0, settings[0].Interval.Minutes())
	assert.Equal(t, 2, settings[1].ChannelId)
	assert.Equal(t, 10.0, settings[1].Interval.Minutes(), "missing interval falls back to the default")

	var broken Channel
	require.NoError(t, DB.First(&broken, 6).Error)
	assert.Equal(t, brokenSetting, *broken.Setting, "broken setting must not be rewritten")
}
