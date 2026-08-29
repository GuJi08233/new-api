package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 邀请码核销必须是"占用 → 归属/释放"两阶段：并发注册不能让同一个码被重复占用，
// 建号失败要能释放，归属后不可再被释放。注册使用只建立邀请关系,
// 不发放兑换奖励(奖励只在兑换路径发放,见 Redeem 相关测试)。
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

	// 重新占用并归属到新用户：只记录使用者与注册用途,不发放奖励
	reserved, err = ReserveInvitationCode("test-code")
	require.NoError(t, err)
	require.NoError(t, FinalizeInvitationCodeUsage(reserved, newUser.Id))

	used, err := GetInvitationCodeByCode("test-code")
	require.NoError(t, err)
	assert.Equal(t, common.InvitationCodeStatusUsed, used.Status)
	assert.Equal(t, newUser.Id, used.UsedUserId)
	assert.Equal(t, common.InvitationCodeUsedTypeRegister, used.UsedType)

	// 注册使用不得触发兑换奖励:即便奖励比例已配置,新用户额度保持不变
	var registered User
	require.NoError(t, DB.First(&registered, "id = ?", newUser.Id).Error)
	assert.Equal(t, 0, registered.Quota, "注册使用邀请码不应发放兑换奖励")

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

// 一码多用的邀请码在注册占用路径下:名额内可被多个用户占用,用满即关闭,
// 释放要回退名额而不是把已用满的码永久锁死。
func TestInvitationCodeMultiUseReserveAndRelease(t *testing.T) {
	truncateTables(t)

	code := InvitationCode{
		UserId:      1,
		Code:        "multi-use-code",
		Quota:       500,
		Status:      common.InvitationCodeStatusEnabled,
		MaxUses:     2,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(&code).Error)

	// 名额内连续占用两次
	first, err := ReserveInvitationCode("multi-use-code")
	require.NoError(t, err)
	require.NotNil(t, first)

	var afterFirst InvitationCode
	require.NoError(t, DB.First(&afterFirst, code.Id).Error)
	assert.Equal(t, 1, afterFirst.UsedCount)
	assert.Equal(t, common.InvitationCodeStatusEnabled, afterFirst.Status, "名额未用满时保持可用")

	second, err := ReserveInvitationCode("multi-use-code")
	require.NoError(t, err)
	require.NotNil(t, second)

	var afterSecond InvitationCode
	require.NoError(t, DB.First(&afterSecond, code.Id).Error)
	assert.Equal(t, 2, afterSecond.UsedCount)
	assert.Equal(t, common.InvitationCodeStatusUsed, afterSecond.Status, "名额用满后自动关闭")

	// 用满后不能再占用
	_, err = ReserveInvitationCode("multi-use-code")
	require.ErrorIs(t, err, ErrInvitationCodeNotUsable)

	// 建号失败释放:回退一个名额并重新开放
	ReleaseInvitationCode(second)
	var afterRelease InvitationCode
	require.NoError(t, DB.First(&afterRelease, code.Id).Error)
	assert.Equal(t, 1, afterRelease.UsedCount)
	assert.Equal(t, common.InvitationCodeStatusEnabled, afterRelease.Status)

	// 释放后名额可被重新占用
	third, err := ReserveInvitationCode("multi-use-code")
	require.NoError(t, err)
	require.NotNil(t, third)
}

// 一码多用邀请码按次数计价:总消耗 = 单价 × 数量 × 次数。
func TestGenerateInvitationCodesChargesPerUse(t *testing.T) {
	truncateTables(t)

	oldPrice := common.InvitationCodePrice
	common.InvitationCodePrice = 100
	t.Cleanup(func() { common.InvitationCodePrice = oldPrice })

	user := User{Username: "code-buyer", Password: "password", Status: common.UserStatusEnabled, Quota: 1000, AffCode: "aff-code-buyer"}
	require.NoError(t, DB.Create(&user).Error)

	// 2 个码 × 每码 3 次 × 单价 100 = 600
	codes, err := GenerateInvitationCodesForUser(user.Id, 2, 3, "batch")
	require.NoError(t, err)
	require.Len(t, codes, 2)
	for _, c := range codes {
		assert.Equal(t, 3, c.MaxUses)
		assert.Equal(t, 100, c.Quota, "Quota 记录单次基数,不随次数放大")
	}

	var afterCharge User
	require.NoError(t, DB.First(&afterCharge, user.Id).Error)
	assert.Equal(t, 400, afterCharge.Quota, "总消耗应为 单价 × 数量 × 次数")

	// 额度不足时整体失败,不产生码也不扣费
	_, err = GenerateInvitationCodesForUser(user.Id, 1, 10, "too-expensive")
	require.Error(t, err)
	var afterReject User
	require.NoError(t, DB.First(&afterReject, user.Id).Error)
	assert.Equal(t, 400, afterReject.Quota, "失败不应扣费")
}
