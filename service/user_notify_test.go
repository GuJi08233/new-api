package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipAdminEmailNotify(t *testing.T) {
	monitor := operation_setting.GetMonitorSetting()
	original := monitor.AdminEmailNotifyEnabled
	t.Cleanup(func() {
		monitor.AdminEmailNotifyEnabled = original
	})

	monitor.AdminEmailNotifyEnabled = false
	assert.True(t, shouldSkipAdminEmailNotify(dto.UserSetting{}), "empty notify type defaults to email and should be skipped")
	assert.True(t, shouldSkipAdminEmailNotify(dto.UserSetting{NotifyType: dto.NotifyTypeEmail}))
	assert.False(t, shouldSkipAdminEmailNotify(dto.UserSetting{NotifyType: dto.NotifyTypeWebhook}))
	assert.False(t, shouldSkipAdminEmailNotify(dto.UserSetting{NotifyType: dto.NotifyTypeBark}))
	assert.False(t, shouldSkipAdminEmailNotify(dto.UserSetting{NotifyType: dto.NotifyTypeGotify}))

	monitor.AdminEmailNotifyEnabled = true
	assert.False(t, shouldSkipAdminEmailNotify(dto.UserSetting{}))
	assert.False(t, shouldSkipAdminEmailNotify(dto.UserSetting{NotifyType: dto.NotifyTypeEmail}))
}

func TestNotifyUserSkipsWhenNotifyTypeNone(t *testing.T) {
	err := NotifyUser(
		999,
		"admin@example.com",
		dto.UserSetting{NotifyType: dto.NotifyTypeNone},
		dto.NewNotify(dto.NotifyTypeChannelTest, "subject", "content", nil),
	)
	require.NoError(t, err)
}
