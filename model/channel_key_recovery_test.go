package model

import (
	"errors"
	"testing"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newChannelRecoveryTestChannel(t *testing.T, multiKey bool) *Channel {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)

	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })

	setting := `{"auto_recovery_enabled":true}`
	channel := &Channel{
		Id:        1,
		Name:      "recovery-channel",
		Key:       "k1",
		Status:    common.ChannelStatusAutoDisabled,
		Models:    "gpt-4o",
		Group:     "default",
		Setting:   &setting,
		OtherInfo: `{"status_reason":"429","status_time":100,"operator_note":"keep"}`,
	}
	if multiKey {
		channel.Key = "k1\nk2\nk3"
		channel.ChannelInfo = ChannelInfo{
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
		}
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, UpdateAbilityStatus(1, false))
	return channel
}

// 同一轮创建的多个目标分别恢复：首把密钥恢复渠道后，其他目标仍能正常恢复。
func TestRecoverChannelKeys(t *testing.T) {
	channel := newChannelRecoveryTestChannel(t, true)
	first, ok := NewChannelRecoveryTarget(channel, 0)
	require.True(t, ok)
	second, ok := NewChannelRecoveryTarget(channel, 1)
	require.True(t, ok)

	keyChanged, channelRecovered, err := RecoverChannel(first)
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
	assert.Equal(t, "keep", got.GetOtherInfo()["operator_note"])

	keyChanged, channelRecovered, err = RecoverChannel(second)
	require.NoError(t, err)
	assert.True(t, keyChanged, "key recovery on an already enabled channel must still persist")
	assert.False(t, channelRecovered)
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, common.ChannelStatusEnabled, got.Status)
	assert.Equal(t, map[int]int{2: common.ChannelStatusManuallyDisabled}, got.ChannelInfo.MultiKeyStatusList)

	keyChanged, channelRecovered, err = RecoverChannel(first)
	require.NoError(t, err)
	assert.False(t, keyChanged, "already enabled key reports no change")
	assert.False(t, channelRecovered)
}

func TestNewChannelRecoveryTargetSelectsTestedKey(t *testing.T) {
	channel := &Channel{
		Id:     1,
		Status: common.ChannelStatusAutoDisabled,
		Key:    "k1\nk2\nk3",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled, 2: common.ChannelStatusManuallyDisabled},
			MultiKeyDisabledTime: map[int]int64{0: 100},
		},
	}
	target, ok := NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
	require.True(t, ok)
	assert.Equal(t, "k2", target.TestedKey, "channel probes use the first enabled key from the snapshot")

	target, ok = NewChannelRecoveryTarget(channel, 0)
	require.True(t, ok)
	assert.Equal(t, "k1", target.TestedKey)

	for _, index := range []int{-2, 1, 2, 3} {
		_, ok = NewChannelRecoveryTarget(channel, index)
		assert.False(t, ok, "index %d is not an automatically disabled key", index)
	}
	channel.ChannelInfo.MultiKeyStatusList[1] = common.ChannelStatusAutoDisabled
	_, ok = NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
	assert.False(t, ok, "a channel probe needs an enabled key")

	channel.ChannelInfo.IsMultiKey = false
	channel.Key = "single-key"
	target, ok = NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
	require.True(t, ok)
	assert.Equal(t, "single-key", target.TestedKey)
	_, ok = NewChannelRecoveryTarget(channel, 0)
	assert.False(t, ok)
}

// 探测后发生的人工操作、密钥身份变更及重新禁用，必须使旧成功结果失效。
func TestRecoverChannelRejectsStaleKeyProbe(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Channel)
	}{
		{name: "key manually disabled", mutate: func(channel *Channel) {
			channel.ChannelInfo.MultiKeyStatusList[1] = common.ChannelStatusManuallyDisabled
		}},
		{name: "channel manually disabled", mutate: func(channel *Channel) {
			channel.Status = common.ChannelStatusManuallyDisabled
		}},
		{name: "channel archived", mutate: func(channel *Channel) {
			channel.Status = common.ChannelStatusArchived
		}},
		{name: "key disabled again", mutate: func(channel *Channel) {
			channel.ChannelInfo.MultiKeyDisabledTime[1]++
		}},
		{name: "channel disabled again during key probe", mutate: func(channel *Channel) {
			channel.OtherInfo = `{"status_reason":"balance exhausted","status_time":500}`
		}},
		{name: "disable reason changed", mutate: func(channel *Channel) {
			channel.ChannelInfo.MultiKeyDisabledReason[1] = "401"
		}},
		{name: "key replaced", mutate: func(channel *Channel) {
			channel.Key = "k1\nK2\nk3"
		}},
		{name: "previous key deleted", mutate: func(channel *Channel) {
			channel.Key = "k2\nk3"
			channel.ChannelInfo.MultiKeySize = 2
			channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled}
			channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 200, 1: 200}
			channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "429", 1: "429"}
		}},
		{name: "keys reordered", mutate: func(channel *Channel) {
			channel.Key = "k2\nk1\nk3"
			channel.ChannelInfo.MultiKeyDisabledTime[0] = 200
			channel.ChannelInfo.MultiKeyDisabledTime[1] = 200
		}},
		{name: "multi key mode removed", mutate: func(channel *Channel) {
			channel.ChannelInfo.IsMultiKey = false
		}},
		{name: "automatic recovery turned off", mutate: func(channel *Channel) {
			setting := `{"auto_recovery_enabled":false}`
			channel.Setting = &setting
		}},
		{name: "automatic recovery setting removed", mutate: func(channel *Channel) {
			channel.Setting = nil
		}},
		{name: "invalid setting must not be rewritten", mutate: func(channel *Channel) {
			setting := `{broken`
			channel.Setting = &setting
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			channel := newChannelRecoveryTestChannel(t, true)
			target, ok := NewChannelRecoveryTarget(channel, 1)
			require.True(t, ok)
			tc.mutate(channel)
			require.NoError(t, DB.Save(channel).Error)

			changed, recovered, err := RecoverChannel(target)
			require.NoError(t, err)
			assert.False(t, changed)
			assert.False(t, recovered)
			var got Channel
			require.NoError(t, DB.First(&got, channel.Id).Error)
			assert.Equal(t, channel.Status, got.Status)
			assert.Equal(t, channel.Key, got.Key)
			assert.Equal(t, channel.ChannelInfo, got.ChannelInfo)
			assert.Equal(t, channel.Setting, got.Setting)
			assert.Equal(t, channel.OtherInfo, got.OtherInfo)
			var ability Ability
			require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.False(t, ability.Enabled)
		})
	}
}

// 整渠道恢复同样使用探测前的状态时间；新禁用不能被旧探测结果覆盖。
func TestRecoverChannelRejectsStaleChannelProbe(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Channel)
	}{
		{name: "manually disabled", mutate: func(channel *Channel) {
			channel.Status = common.ChannelStatusManuallyDisabled
		}},
		{name: "archived", mutate: func(channel *Channel) {
			channel.Status = common.ChannelStatusArchived
		}},
		{name: "disabled again", mutate: func(channel *Channel) {
			channel.OtherInfo = `{"status_reason":"429","status_time":200}`
		}},
		{name: "key replaced", mutate: func(channel *Channel) {
			channel.Key = "replacement"
		}},
		{name: "automatic recovery turned off", mutate: func(channel *Channel) {
			setting := `{"auto_recovery_enabled":false}`
			channel.Setting = &setting
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			channel := newChannelRecoveryTestChannel(t, false)
			target, ok := NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
			require.True(t, ok)
			tc.mutate(channel)
			require.NoError(t, DB.Save(channel).Error)

			changed, recovered, err := RecoverChannel(target)
			require.NoError(t, err)
			assert.False(t, changed)
			assert.False(t, recovered)
			var got Channel
			require.NoError(t, DB.First(&got, channel.Id).Error)
			assert.Equal(t, channel.Status, got.Status)
			assert.Equal(t, channel.Key, got.Key)
			assert.Equal(t, channel.OtherInfo, got.OtherInfo)
			var ability Ability
			require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.False(t, ability.Enabled)
		})
	}
}

func TestRecoverChannelPreservesOtherKeyChanges(t *testing.T) {
	channel := newChannelRecoveryTestChannel(t, true)
	target, ok := NewChannelRecoveryTarget(channel, 0)
	require.True(t, ok)
	channel.ChannelInfo.MultiKeyStatusList[1] = common.ChannelStatusManuallyDisabled
	channel.ChannelInfo.MultiKeyDisabledReason[1] = "manual after probe started"
	channel.ChannelInfo.MultiKeyDisabledTime[1] = 500
	require.NoError(t, DB.Save(channel).Error)

	changed, recovered, err := RecoverChannel(target)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, recovered)
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, map[int]int{1: common.ChannelStatusManuallyDisabled, 2: common.ChannelStatusManuallyDisabled}, got.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, "manual after probe started", got.ChannelInfo.MultiKeyDisabledReason[1])
	assert.Equal(t, int64(500), got.ChannelInfo.MultiKeyDisabledTime[1])
}

func TestRecoverChannelWholeChannel(t *testing.T) {
	for _, multiKey := range []bool{false, true} {
		name := "single key"
		if multiKey {
			name = "multi key"
		}
		t.Run(name, func(t *testing.T) {
			channel := newChannelRecoveryTestChannel(t, multiKey)
			if multiKey {
				delete(channel.ChannelInfo.MultiKeyStatusList, 1)
				require.NoError(t, DB.Save(channel).Error)
			}
			target, ok := NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
			require.True(t, ok)
			common.MemoryCacheEnabled = true
			InitChannelCache()

			changed, recovered, err := RecoverChannel(target)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.True(t, recovered)
			var got Channel
			require.NoError(t, DB.First(&got, channel.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, got.Status)
			assert.Equal(t, channel.ChannelInfo, got.ChannelInfo, "a channel probe preserves every key status")
			assert.Equal(t, "keep", got.GetOtherInfo()["operator_note"])
			var ability Ability
			require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.True(t, ability.Enabled)
			routed, err := GetRandomSatisfiedChannel("default", "gpt-4o", 0, "")
			require.NoError(t, err)
			require.NotNil(t, routed, "a recovered channel must immediately rejoin cached routing")
			assert.Equal(t, channel.Id, routed.Id)

			changed, recovered, err = RecoverChannel(target)
			require.NoError(t, err)
			assert.False(t, changed)
			assert.False(t, recovered)
		})
	}
}

func TestRecoverChannelRejectsDisabledChannelProbeKey(t *testing.T) {
	for _, status := range []int{common.ChannelStatusManuallyDisabled, common.ChannelStatusAutoDisabled} {
		channel := newChannelRecoveryTestChannel(t, true)
		delete(channel.ChannelInfo.MultiKeyStatusList, 1)
		require.NoError(t, DB.Save(channel).Error)
		target, ok := NewChannelRecoveryTarget(channel, ChannelRecoveryChannelTarget)
		require.True(t, ok)
		channel.ChannelInfo.MultiKeyStatusList[1] = status
		require.NoError(t, DB.Save(channel).Error)

		changed, recovered, err := RecoverChannel(target)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, recovered)
		var got Channel
		require.NoError(t, DB.First(&got, channel.Id).Error)
		assert.Equal(t, common.ChannelStatusAutoDisabled, got.Status)
		assert.Equal(t, channel.ChannelInfo, got.ChannelInfo)
	}
}

func TestRecoverChannelDoesNotOverwriteUnrelatedFields(t *testing.T) {
	channel := newChannelRecoveryTestChannel(t, true)
	target, ok := NewChannelRecoveryTarget(channel, 0)
	require.True(t, ok)
	const callbackName = "test:channel_recovery_concurrent_metadata"
	injected := false
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "channels" {
			return
		}
		injected = true
		// 固定发生在恢复读取与写入之间，验证恢复仅写必要字段。
		err := tx.Session(&gorm.Session{NewDB: true}).Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]interface{}{
			"used_quota":    1234,
			"balance":       12.5,
			"response_time": 42,
			"name":          "edited during recovery",
		}).Error
		tx.AddError(err)
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove(callbackName)) })

	changed, recovered, err := RecoverChannel(target)
	require.NoError(t, err)
	assert.True(t, injected)
	assert.True(t, changed)
	assert.True(t, recovered)
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, int64(1234), got.UsedQuota)
	assert.Equal(t, 12.5, got.Balance)
	assert.Equal(t, 42, got.ResponseTime)
	assert.Equal(t, "edited during recovery", got.Name)
}

func TestRecoverChannelRollsBackWhenAbilityUpdateFails(t *testing.T) {
	channel := newChannelRecoveryTestChannel(t, true)
	target, ok := NewChannelRecoveryTarget(channel, 0)
	require.True(t, ok)
	const callbackName = "test:channel_recovery_ability_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "abilities" {
			tx.AddError(errors.New("injected ability update failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, DB.Callback().Update().Remove(callbackName)) })

	changed, recovered, err := RecoverChannel(target)
	require.ErrorContains(t, err, "injected ability update failure")
	assert.False(t, changed)
	assert.False(t, recovered)
	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, got.Status)
	assert.Equal(t, channel.ChannelInfo, got.ChannelInfo)
	assert.Equal(t, channel.OtherInfo, got.OtherInfo)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.False(t, ability.Enabled)
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
