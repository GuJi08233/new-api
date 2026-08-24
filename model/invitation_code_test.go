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
	assert.Equal(t, common.InvitationCodeUsedTypeRegister, used.UsedType)

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

// 邀请码作兑换码核销：记录使用者与用途（UsedType=兑换），但不得进入用户列表的
// 邀请关系——AttachInvitationInfo 只按 used_type=注册 反查 used_invitation_code。
func TestInvitationCodeRedeemRecordsUsageWithoutInviteRelation(t *testing.T) {
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

	// 生成者可追溯去向：记录使用者与兑换用途
	used, err := GetInvitationCodeByCode("redeem-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, used.Status)
	assert.Equal(t, redeemer.Id, used.UsedUserId)
	assert.Equal(t, common.InvitationCodeUsedTypeRedeem, used.UsedType)
	assert.NotZero(t, used.UsedTime)

	var after User
	require.NoError(t, DB.First(&after, "id = ?", redeemer.Id).Error)
	assert.Equal(t, 500, after.Quota)

	// 兑换不产生邀请关系：用户列表反查不得把兑换的码当成注册使用的邀请码
	users := []*User{&redeemer}
	AttachInvitationInfo(users)
	assert.Empty(t, redeemer.UsedInvitationCode)

	// 已核销的码不可再次兑换
	_, err = Redeem("redeem-code", redeemer.Id)
	require.Error(t, err)
}

// 管理端邀请信息展示：inviter_id 未落库的历史注册（OAuth 路径旧数据）
// 必须能按注册邀请码的归属补出邀请人；兑换核销不产生邀请关系。
func TestAttachInvitationInfo_DerivesInviterFromRegisterCode(t *testing.T) {
	truncateTables(t)

	inviter := User{Id: 11, Username: "inviter-user", Password: "password", Status: common.UserStatusEnabled, AffCode: "aff-11"}
	require.NoError(t, DB.Create(&inviter).Error)
	// inviter_id 为 0 的被邀请用户（模拟 OAuth 注册旧数据）
	invited := User{Id: 12, Username: "invited-oauth", Password: "password", Status: common.UserStatusEnabled, AffCode: "aff-12"}
	require.NoError(t, DB.Create(&invited).Error)
	// 只是兑换过码的用户，不应被当成被邀请人
	redeemer := User{Id: 13, Username: "redeemer-user", Password: "password", Status: common.UserStatusEnabled, AffCode: "aff-13"}
	require.NoError(t, DB.Create(&redeemer).Error)

	registerCode := InvitationCode{
		UserId: inviter.Id, Code: "reg-code", Quota: 100,
		Status: common.InvitationCodeStatusUsed, UsedUserId: invited.Id,
		UsedType: common.InvitationCodeUsedTypeRegister, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&registerCode).Error)
	redeemCode := InvitationCode{
		UserId: inviter.Id, Code: "redeem-code", Quota: 100,
		Status: common.InvitationCodeStatusUsed, UsedUserId: redeemer.Id,
		UsedType: common.InvitationCodeUsedTypeRedeem, CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&redeemCode).Error)

	users := []*User{{Id: invited.Id, InviterId: 0}, {Id: redeemer.Id, InviterId: 0}}
	AttachInvitationInfo(users)

	assert.Equal(t, inviter.Id, users[0].InviterId)
	assert.Equal(t, "inviter-user", users[0].InviterName)
	assert.Equal(t, "reg-code", users[0].UsedInvitationCode)

	assert.Zero(t, users[1].InviterId)
	assert.Empty(t, users[1].InviterName)
	assert.Empty(t, users[1].UsedInvitationCode)
}
