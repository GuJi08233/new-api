package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setGroupHierarchyForTest declares that userGroup covers the given special
// usable groups (the admin-side 分组层级 configuration) and restores the
// previous configuration when the test finishes.
func setGroupHierarchyForTest(t *testing.T, userGroup string, special map[string]string) {
	t.Helper()
	settings := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	prev, had := settings.Get(userGroup)
	settings.Set(userGroup, special)
	t.Cleanup(func() {
		if had {
			settings.Set(userGroup, prev)
		} else {
			settings.Set(userGroup, map[string]string{})
		}
	})
}

func TestCreateUserSubscription_KeepsGroupWhenCurrentGroupCoversUpgrade(t *testing.T) {
	truncateTables(t)
	setGroupHierarchyForTest(t, "svip", map[string]string{"vip": "vip", "default": "默认"})

	insertUserForSubscriptionTest(t, 3101, "svip")
	insertUserForSubscriptionTest(t, 3102, "default")
	plan := insertPlanForGroupBillingTest(t, 1101, "VIP Upgrade Plan", false, 0)
	plan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(plan).Error)

	var svipSub *UserSubscription
	var defaultSub *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		svipSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3101, plan, "order")
		if err != nil {
			return err
		}
		defaultSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3102, plan, "order")
		return err
	})
	require.NoError(t, err)

	svipSaved := getUserSubscriptionForGroupBillingTest(t, svipSub.Id)
	assert.Equal(t, SubscriptionStatusActive, svipSaved.Status)
	assert.Empty(t, svipSaved.PrevUserGroup)
	var svipUser User
	require.NoError(t, DB.Where("id = ?", 3101).First(&svipUser).Error)
	assert.Equal(t, "svip", svipUser.Group)

	defaultSaved := getUserSubscriptionForGroupBillingTest(t, defaultSub.Id)
	assert.Equal(t, SubscriptionStatusActive, defaultSaved.Status)
	assert.Equal(t, "default", defaultSaved.PrevUserGroup)
	var defaultUser User
	require.NoError(t, DB.Where("id = ?", 3102).First(&defaultUser).Error)
	assert.Equal(t, "vip", defaultUser.Group)
}

func TestExpireDueSubscriptions_KeepsGroupThatWasNeverOverwritten(t *testing.T) {
	truncateTables(t)
	setGroupHierarchyForTest(t, "svip", map[string]string{"vip": "vip"})

	insertUserForSubscriptionTest(t, 3103, "svip")
	insertUserForSubscriptionTest(t, 3104, "default")
	plan := insertPlanForGroupBillingTest(t, 1102, "VIP Downgrade Plan", false, 0)
	plan.UpgradeGroup = "vip"
	plan.DowngradeGroup = "default"
	require.NoError(t, DB.Save(plan).Error)

	var svipSub *UserSubscription
	var defaultSub *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		svipSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3103, plan, "order")
		if err != nil {
			return err
		}
		defaultSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3104, plan, "order")
		return err
	})
	require.NoError(t, err)

	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id IN ?", []int{svipSub.Id, defaultSub.Id}).
		Update("end_time", time.Now().Add(-time.Hour).Unix()).Error)

	n, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// svip 用户的分组从未被订阅覆盖，到期时显式降级分组不得生效
	var svipUser User
	require.NoError(t, DB.Where("id = ?", 3103).First(&svipUser).Error)
	assert.Equal(t, "svip", svipUser.Group)

	// default 用户被升级到过 vip，到期按套餐显式降级到 default
	var defaultUser User
	require.NoError(t, DB.Where("id = ?", 3104).First(&defaultUser).Error)
	assert.Equal(t, "default", defaultUser.Group)

	assert.Equal(t, SubscriptionStatusExpired, getUserSubscriptionForGroupBillingTest(t, svipSub.Id).Status)
	assert.Equal(t, SubscriptionStatusExpired, getUserSubscriptionForGroupBillingTest(t, defaultSub.Id).Status)
}

func TestPreConsumeUserSubscription_HierarchyGroupMatching(t *testing.T) {
	truncateTables(t)
	setGroupHierarchyForTest(t, "svip", map[string]string{"vip": "vip"})

	vipPlan := insertPlanForGroupBillingTest(t, 1103, "VIP Bound Plan", false, 0)
	vipSub := insertUserSubscriptionForGroupBillingTest(t, 2101, 3105, vipPlan.Id, "vip", 1000)

	// svip 已包含 vip：svip 请求可消耗 vip 绑定订阅
	result, err := PreConsumeUserSubscription("req-hier-svip", 3105, "gpt-test", 0, 100, "svip")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, vipSub.Id, result.UserSubscriptionId)

	// default 未包含 vip：不可消耗
	_, err = PreConsumeUserSubscription("req-hier-default", 3105, "gpt-test", 0, 100, "default")
	require.Error(t, err)

	// 反向不成立：vip 请求不可消耗 svip 绑定订阅
	svipPlan := insertPlanForGroupBillingTest(t, 1104, "SVIP Bound Plan", false, 0)
	insertUserSubscriptionForGroupBillingTest(t, 2102, 3106, svipPlan.Id, "svip", 1000)
	_, err = PreConsumeUserSubscription("req-hier-vip", 3106, "gpt-test", 0, 100, "vip")
	require.Error(t, err)
}

func TestGroupScopedSubscriptionQueries_HierarchyAware(t *testing.T) {
	truncateTables(t)
	setGroupHierarchyForTest(t, "svip", map[string]string{"vip": "vip"})

	plan := insertPlanForGroupBillingTest(t, 1105, "VIP Strict Plan", true, 0)
	sub := insertUserSubscriptionForGroupBillingTest(t, 2103, 3107, plan.Id, "vip", 1000)
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", sub.Id).
		Update("allow_wallet_overflow", false).Error)

	hasSvip, err := HasActiveUserSubscriptionForUsingGroup(3107, "svip")
	require.NoError(t, err)
	assert.True(t, hasSvip)
	hasDefault, err := HasActiveUserSubscriptionForUsingGroup(3107, "default")
	require.NoError(t, err)
	assert.False(t, hasDefault)

	lockSvip, err := HasDisableBalanceDeductionSubscriptionForUsingGroup(3107, "svip")
	require.NoError(t, err)
	assert.True(t, lockSvip)
	lockDefault, err := HasDisableBalanceDeductionSubscriptionForUsingGroup(3107, "default")
	require.NoError(t, err)
	assert.False(t, lockDefault)

	allowSvip, err := UserActiveSubscriptionsAllowWalletOverflowForUsingGroup(3107, "svip")
	require.NoError(t, err)
	assert.False(t, allowSvip)
	allowDefault, err := UserActiveSubscriptionsAllowWalletOverflowForUsingGroup(3107, "default")
	require.NoError(t, err)
	assert.True(t, allowDefault)
}

func TestSwitchUserActiveSubscription_HierarchyKeepsCoveredGroup(t *testing.T) {
	truncateTables(t)
	setGroupHierarchyForTest(t, "svip", map[string]string{"vip": "vip"})

	insertUserForSubscriptionTest(t, 3108, "svip")
	firstPlan := insertPlanForGroupBillingTest(t, 1106, "VIP Plan A", false, 0)
	firstPlan.UpgradeGroup = "vip"
	firstPlan.DowngradeGroup = "default"
	require.NoError(t, DB.Save(firstPlan).Error)
	secondPlan := insertPlanForGroupBillingTest(t, 1107, "VIP Plan B", false, 0)
	secondPlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(secondPlan).Error)

	var firstSub *UserSubscription
	var secondSub *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		firstSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3108, firstPlan, "order")
		if err != nil {
			return err
		}
		secondSub, _, err = CreateUserSubscriptionFromPlanTx(tx, 3108, secondPlan, "order")
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, firstSub.Id).Status)
	assert.Equal(t, SubscriptionStatusInactive, getUserSubscriptionForGroupBillingTest(t, secondSub.Id).Status)

	// 切换到 B：A 被停用（其显式降级分组不得误降从未被改过的 svip），B 激活（同样不覆盖分组）
	_, err = SwitchUserActiveSubscription(3108, secondSub.Id)
	require.NoError(t, err)

	firstSaved := getUserSubscriptionForGroupBillingTest(t, firstSub.Id)
	secondSaved := getUserSubscriptionForGroupBillingTest(t, secondSub.Id)
	assert.Equal(t, SubscriptionStatusInactive, firstSaved.Status)
	assert.Equal(t, SubscriptionStatusActive, secondSaved.Status)
	assert.Empty(t, firstSaved.PrevUserGroup)
	assert.Empty(t, secondSaved.PrevUserGroup)

	var user User
	require.NoError(t, DB.Where("id = ?", 3108).First(&user).Error)
	assert.Equal(t, "svip", user.Group)

	// 再切回 A，分组依旧保持 svip
	_, err = SwitchUserActiveSubscription(3108, firstSub.Id)
	require.NoError(t, err)
	require.NoError(t, DB.Where("id = ?", 3108).First(&user).Error)
	assert.Equal(t, "svip", user.Group)
	assert.Equal(t, SubscriptionStatusActive, getUserSubscriptionForGroupBillingTest(t, firstSub.Id).Status)
	assert.Equal(t, SubscriptionStatusInactive, getUserSubscriptionForGroupBillingTest(t, secondSub.Id).Status)
}
