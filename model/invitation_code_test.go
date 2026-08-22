package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 邀请码核销必须是"占用 → 归属/释放"两阶段：并发注册不能让同一个码被重复占用，
// 建号失败要能释放，归属后要发放奖励且不可再被释放。
func TestInvitationCodeReserveFinalizeRelease(t *testing.T) {
	truncateTables(t)

	oldRatio := common.InvitationCodeRewardRatio
	common.InvitationCodeRewardRatio = 50
	t.Cleanup(func() { common.InvitationCodeRewardRatio = oldRatio })

	newUser := User{Id: 2, Username: "invited-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(&newUser).Error)

	code := InvitationCode{
		UserId:      1,
		Code:        "test-code",
		Quota:       1000,
		Status:      common.InvitationCodeStatusEnabled,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, code.Insert())

	// 第一次占用成功，第二次必须失败（并发一码多注册的核心防线）
	reserved, err := ReserveInvitationCode("test-code")
	require.NoError(t, err)
	_, err = ReserveInvitationCode("test-code")
	require.ErrorIs(t, err, ErrInvitationCodeNotUsable)

	// 建号失败后释放，码恢复可用
	ReleaseInvitationCode(reserved)
	restored, err := GetInvitationCodeByCode("test-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusEnabled, restored.Status)

	// 重新占用并归属到新用户：记录使用者并按比例发放奖励
	reserved, err = ReserveInvitationCode("test-code")
	require.NoError(t, err)
	require.NoError(t, FinalizeInvitationCodeUsage(reserved, newUser.Id))

	used, err := GetInvitationCodeByCode("test-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, used.Status)
	assert.Equal(t, newUser.Id, used.UsedUserId)

	var rewarded User
	require.NoError(t, DB.First(&rewarded, "id = ?", newUser.Id).Error)
	assert.Equal(t, 500, rewarded.Quota)

	// 已归属的码不可再被释放
	ReleaseInvitationCode(used)
	final, err := GetInvitationCodeByCode("test-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, final.Status)
	assert.Equal(t, newUser.Id, final.UsedUserId)
}

// 邀请码作兑换码核销：仅改状态与时间，不得写 UsedUserId——该字段专表"注册使用者"，
// 管理端用户列表的邀请关系按 used_user_id 反查，兑换不产生邀请关系。
func TestInvitationCodeRedeemDoesNotRecordUser(t *testing.T) {
	truncateTables(t)

	oldRatio := common.InvitationCodeRewardRatio
	common.InvitationCodeRewardRatio = 50
	t.Cleanup(func() { common.InvitationCodeRewardRatio = oldRatio })

	creator := User{Id: 1, Username: "creator", Password: "password", Status: common.UserStatusEnabled, AffCode: "ic-creator"}
	redeemer := User{Id: 2, Username: "redeemer", Password: "password", Status: common.UserStatusEnabled, Quota: 0, AffCode: "ic-redeemer"}
	require.NoError(t, DB.Create(&creator).Error)
	require.NoError(t, DB.Create(&redeemer).Error)

	code := InvitationCode{
		UserId:      creator.Id,
		Code:        "redeem-code",
		Quota:       1000,
		Status:      common.InvitationCodeStatusEnabled,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, code.Insert())

	// 不能兑换自己生成的码
	_, err := Redeem("redeem-code", creator.Id)
	require.Error(t, err)

	quota, err := Redeem("redeem-code", redeemer.Id)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	used, err := GetInvitationCodeByCode("redeem-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, used.Status)
	assert.Zero(t, used.UsedUserId)
	assert.NotZero(t, used.UsedTime)

	var after User
	require.NoError(t, DB.First(&after, "id = ?", redeemer.Id).Error)
	assert.Equal(t, 500, after.Quota)

	// 已核销的码不可再次兑换
	_, err = Redeem("redeem-code", redeemer.Id)
	require.Error(t, err)
}
