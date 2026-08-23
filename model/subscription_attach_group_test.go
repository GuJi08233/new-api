package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertAttachPlanForTest(t *testing.T, id int, group string) *SubscriptionPlan {
	t.Helper()
	plan := insertPlanForGroupBillingTest(t, id, "Attach Plan "+group, false, 0)
	plan.UpgradeGroup = group
	plan.GroupMode = SubscriptionGroupModeAttach
	require.NoError(t, DB.Save(plan).Error)
	return plan
}

func TestAttachModeSubscription_GrantsGroupWithoutChangingUserGroup(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3301, "default")
	plan := insertAttachPlanForTest(t, 1301, "embedding")

	sub, renewed := createSubscriptionFromPlanForRenewalTest(t, 3301, plan)
	require.False(t, renewed)
	assert.Equal(t, SubscriptionStatusActive, sub.Status)
	assert.Equal(t, SubscriptionGroupModeAttach, sub.GroupMode)
	assert.Empty(t, sub.PrevUserGroup)

	// 用户分组不变，仅获得附加分组使用权
	var user User
	require.NoError(t, DB.Where("id = ?", 3301).First(&user).Error)
	assert.Equal(t, "default", user.Group)
	assert.True(t, UserHasAttachedGroup(3301, "embedding"))
	assert.False(t, UserHasAttachedGroup(3301, "vip"))
	attached, err := GetUserAttachedGroups(3301)
	require.NoError(t, err)
	assert.Equal(t, []string{"embedding"}, attached)

	// 计费只对附加分组的请求生效
	result, err := PreConsumeUserSubscription("req-attach-embed", 3301, "text-embedding", 0, 100, "embedding")
	require.NoError(t, err)
	assert.Equal(t, sub.Id, result.UserSubscriptionId)
	_, err = PreConsumeUserSubscription("req-attach-default", 3301, "gpt-test", 0, 100, "default")
	require.Error(t, err)

	// 到期后使用权自动回收，用户分组不受影响
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", sub.Id).
		Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)
	_, err = ExpireDueSubscriptions(10)
	require.NoError(t, err)
	require.NoError(t, DB.Where("id = ?", 3301).First(&user).Error)
	assert.Equal(t, "default", user.Group)
	assert.False(t, UserHasAttachedGroup(3301, "embedding"))
}

func TestAttachModeSubscription_CoexistsWithUpgradeSubscription(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3302, "default")
	vipPlan := insertPlanForGroupBillingTest(t, 1302, "VIP Upgrade Plan", false, 0)
	vipPlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(vipPlan).Error)
	attachPlan := insertAttachPlanForTest(t, 1303, "embedding")

	// 已有生效的升级订阅时，追加订阅仍直接生效（不排队）
	vipSub, _ := createSubscriptionFromPlanForRenewalTest(t, 3302, vipPlan)
	attachSub, _ := createSubscriptionFromPlanForRenewalTest(t, 3302, attachPlan)
	assert.Equal(t, SubscriptionStatusActive, vipSub.Status)
	assert.Equal(t, SubscriptionStatusActive, attachSub.Status)
	var user User
	require.NoError(t, DB.Where("id = ?", 3302).First(&user).Error)
	assert.Equal(t, "vip", user.Group)
	assert.True(t, UserHasAttachedGroup(3302, "embedding"))

	// 反向顺序：追加订阅在先，升级订阅照常立即生效并升级分组
	insertUserForSubscriptionTest(t, 3303, "default")
	attachFirst, _ := createSubscriptionFromPlanForRenewalTest(t, 3303, attachPlan)
	vipSecond, _ := createSubscriptionFromPlanForRenewalTest(t, 3303, vipPlan)
	assert.Equal(t, SubscriptionStatusActive, attachFirst.Status)
	assert.Equal(t, SubscriptionStatusActive, vipSecond.Status)
	var secondUser User
	require.NoError(t, DB.Where("id = ?", 3303).First(&secondUser).Error)
	assert.Equal(t, "vip", secondUser.Group)

	// 升级订阅到期：即使追加订阅仍在生效，分组也要正常回退
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", vipSub.Id).
		Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)
	_, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	var downgraded User
	require.NoError(t, DB.Where("id = ?", 3302).First(&downgraded).Error)
	assert.Equal(t, "default", downgraded.Group)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, attachSub.Id).Status)
	assert.True(t, UserHasAttachedGroup(3302, "embedding"))
}

func TestSwitchUserActiveSubscription_KeepsAttachSubscriptionsActive(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3304, "default")
	attachPlan := insertAttachPlanForTest(t, 1304, "embedding")
	vipPlan := insertPlanForGroupBillingTest(t, 1305, "VIP Plan", false, 0)
	vipPlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(vipPlan).Error)
	svipPlan := insertPlanForGroupBillingTest(t, 1306, "SVIP Plan", false, 0)
	svipPlan.UpgradeGroup = "svip"
	require.NoError(t, DB.Save(svipPlan).Error)

	attachSub, _ := createSubscriptionFromPlanForRenewalTest(t, 3304, attachPlan)
	vipSub, _ := createSubscriptionFromPlanForRenewalTest(t, 3304, vipPlan)
	svipSub, _ := createSubscriptionFromPlanForRenewalTest(t, 3304, svipPlan)
	assert.Equal(t, SubscriptionStatusActive, attachSub.Status)
	assert.Equal(t, SubscriptionStatusActive, vipSub.Status)
	assert.Equal(t, SubscriptionStatusInactive, svipSub.Status)

	// 切换普通订阅：vip 停用、svip 生效，追加订阅不受影响
	_, err := SwitchUserActiveSubscription(3304, svipSub.Id)
	require.NoError(t, err)
	assert.Equal(t, SubscriptionStatusInactive, getUserSubscriptionForGroupBillingTest(t, vipSub.Id).Status)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, svipSub.Id).Status)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, attachSub.Id).Status)
	var user User
	require.NoError(t, DB.Where("id = ?", 3304).First(&user).Error)
	assert.Equal(t, "svip", user.Group)

	// 对追加订阅执行切换是无害操作，不停用其他生效订阅
	_, err = SwitchUserActiveSubscription(3304, attachSub.Id)
	require.NoError(t, err)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, svipSub.Id).Status)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, attachSub.Id).Status)
}

func TestAttachModeWithoutGroupFallsBackToUpgradeSemantics(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3305, "default")
	plan := insertPlanForGroupBillingTest(t, 1307, "Broken Attach Plan", false, 0)
	plan.GroupMode = SubscriptionGroupModeAttach
	require.NoError(t, DB.Save(plan).Error)

	sub, _ := createSubscriptionFromPlanForRenewalTest(t, 3305, plan)
	// 未绑定分组的追加模式退化为普通订阅：不追加分组、参与单生效排队
	assert.Empty(t, sub.GroupMode)
	attached, err := GetUserAttachedGroups(3305)
	require.NoError(t, err)
	assert.Empty(t, attached)
}
