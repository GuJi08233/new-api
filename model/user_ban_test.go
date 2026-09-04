package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestoreExpiredUserBansOnlyReleasesExpiredTemporaryBans 覆盖自动解禁的边界:
// 只有到期的临时禁用会被放出来。管理员手动封禁与风控的永久禁用都不带到期时间,
// 一旦被这个任务误放,后台就成了绕过管理员处置的后门。
func TestRestoreExpiredUserBansOnlyReleasesExpiredTemporaryBans(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	expired := &User{Username: "expired-ban", Password: "12345678", Role: common.RoleCommonUser, AffCode: "aff1",
		Status: common.UserStatusDisabled, DisableReason: "临时禁用 10 分钟(首次违规)", DisableExpiresAt: now - 60}
	pending := &User{Username: "pending-ban", Password: "12345678", Role: common.RoleCommonUser, AffCode: "aff2",
		Status: common.UserStatusDisabled, DisableReason: "临时禁用 60 分钟(第 2 次违规)", DisableExpiresAt: now + 3600}
	permanent := &User{Username: "permanent-ban", Password: "12345678", Role: common.RoleCommonUser, AffCode: "aff3",
		Status: common.UserStatusDisabled, DisableReason: "永久禁用(第 3 次违规)"}
	manual := &User{Username: "manual-ban", Password: "12345678", Role: common.RoleCommonUser, AffCode: "aff4",
		Status: common.UserStatusDisabled, DisableReason: "管理员处置"}
	for _, user := range []*User{expired, pending, permanent, manual} {
		require.NoError(t, DB.Create(user).Error)
	}

	restored, err := RestoreExpiredUserBans(now)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, expired.Id, restored[0].UserId)
	assert.Equal(t, "expired-ban", restored[0].Username)

	var saved User
	require.NoError(t, DB.First(&saved, expired.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, saved.Status)
	assert.Empty(t, saved.DisableReason)
	assert.Zero(t, saved.DisableExpiresAt)

	for _, user := range []*User{pending, permanent, manual} {
		var untouched User
		require.NoError(t, DB.First(&untouched, user.Id).Error)
		assert.Equal(t, common.UserStatusDisabled, untouched.Status, "%s 不该被恢复", user.Username)
	}

	// 已恢复的账号不会被重复处理
	restored, err = RestoreExpiredUserBans(now)
	require.NoError(t, err)
	assert.Empty(t, restored)
}

// TestSetUserManualBanStateClearsExpiry 覆盖手动处置的清理契约:管理员手动封禁或解禁后,
// 风控留下的到期时间必须清零,否则下一次手动封禁会被自动解禁任务当作到期的临时禁用放出来。
func TestSetUserManualBanStateClearsExpiry(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "manual-target", Password: "12345678", Role: common.RoleCommonUser,
		Status: common.UserStatusDisabled, DisableReason: "临时禁用 10 分钟(首次违规)",
		DisableExpiresAt: time.Now().Add(-time.Minute).Unix()}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, SetUserManualBanState(user.Id, "管理员处置"))

	var saved User
	require.NoError(t, DB.First(&saved, user.Id).Error)
	assert.Equal(t, "管理员处置", saved.DisableReason)
	assert.Zero(t, saved.DisableExpiresAt)

	restored, err := RestoreExpiredUserBans(time.Now().Unix())
	require.NoError(t, err)
	assert.Empty(t, restored, "手动封禁不该被自动解禁")
}
