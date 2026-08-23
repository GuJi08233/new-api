package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createSubscriptionFromPlanForRenewalTest(t *testing.T, userId int, plan *SubscriptionPlan) (*UserSubscription, bool) {
	t.Helper()
	var sub *UserSubscription
	var renewed bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		sub, renewed, err = CreateUserSubscriptionFromPlanTx(tx, userId, plan, "order")
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, sub)
	return sub, renewed
}

func TestCreateUserSubscriptionFromPlanTx_SamePlanRenewsInsteadOfDuplicating(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3201, "default")
	plan := insertPlanForGroupBillingTest(t, 1201, "Renewable Plan", false, 0)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3201, plan)
	require.False(t, renewed)
	assert.Equal(t, SubscriptionStatusActive, first.Status)
	firstEnd := first.EndTime

	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", first.Id).
		Update("amount_used", 300).Error)

	second, renewed := createSubscriptionFromPlanForRenewalTest(t, 3201, plan)
	require.True(t, renewed)
	assert.Equal(t, first.Id, second.Id)
	// 从原到期时间顺延一个月，提前续期不损失剩余时长
	assert.Equal(t, time.Unix(firstEnd, 0).AddDate(0, 1, 0).Unix(), second.EndTime)
	// 无重置周期：额度池追加一份，已用量保留
	assert.EqualValues(t, 2000, second.AmountTotal)
	saved := getUserSubscriptionForGroupBillingTest(t, first.Id)
	assert.EqualValues(t, 300, saved.AmountUsed)
	assert.Equal(t, SubscriptionStatusActive, saved.Status)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 3201, plan.Id).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)

	// 不同套餐仍然新建（排队为未激活）
	otherPlan := insertPlanForGroupBillingTest(t, 1202, "Another Plan", false, 0)
	third, renewed := createSubscriptionFromPlanForRenewalTest(t, 3201, otherPlan)
	require.False(t, renewed)
	assert.NotEqual(t, first.Id, third.Id)
	assert.Equal(t, SubscriptionStatusInactive, third.Status)
}

func TestRenewal_PeriodicResetPlanKeepsCapAndReschedules(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3202, "default")
	plan := insertPlanForGroupBillingTest(t, 1203, "Daily Reset Plan", false, 0)
	plan.QuotaResetPeriod = SubscriptionResetDaily
	require.NoError(t, DB.Save(plan).Error)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3202, plan)
	require.False(t, renewed)

	// 模拟重置排程已随原到期时间终止
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", first.Id).
		Updates(map[string]interface{}{"next_reset_time": 0, "amount_used": 200}).Error)

	second, renewed := createSubscriptionFromPlanForRenewalTest(t, 3202, plan)
	require.True(t, renewed)
	// 周期重置套餐：TotalAmount 是每周期上限，续期不追加
	assert.EqualValues(t, 1000, second.AmountTotal)
	// 已用量不因续期清零，等待正常的周期重置
	assert.EqualValues(t, 200, second.AmountUsed)
	// 重置排程按新到期时间重新建立
	assert.Greater(t, second.NextResetTime, time.Now().Unix())
	assert.Greater(t, second.LastResetTime, int64(0))
}

func TestRenewal_BypassesPerUserPurchaseLimit(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3203, "default")
	plan := insertPlanForGroupBillingTest(t, 1204, "Per-User Limited Renewable", false, 0)
	plan.MaxPurchasePerUser = 1
	require.NoError(t, DB.Save(plan).Error)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3203, plan)
	require.False(t, renewed)

	// 无全局限购时，持有者续期不受每用户限购阻拦
	require.NoError(t, CheckSubscriptionPlanPurchaseAllowed(3203, plan, true))
	second, renewed := createSubscriptionFromPlanForRenewalTest(t, 3203, plan)
	require.True(t, renewed)
	assert.Equal(t, first.Id, second.Id)

	// 订阅过期后不再是续期，恢复正常限购判定
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", first.Id).
		Updates(map[string]interface{}{
			"status":   SubscriptionStatusExpired,
			"end_time": time.Now().Add(-time.Hour).Unix(),
		}).Error)
	err := CheckSubscriptionPlanPurchaseAllowed(3203, plan, true)
	require.Error(t, err)
	assert.Equal(t, "已达到该套餐购买上限", err.Error())
}

func TestRenewal_GlobalLimitedPlanDoesNotRenewUntilSeatReleased(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3208, "default")
	plan := insertPlanForGroupBillingTest(t, 1208, "Scarce Seat Plan", false, 1)
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3208, plan)
	require.False(t, renewed)

	// 全局限购套餐不提供持有期续期：持有者再次购买走正常限购判定，名额被自己占用
	err := CheckSubscriptionPlanPurchaseAllowed(3208, plan, true)
	require.Error(t, err)
	assert.Equal(t, "该套餐已售罄", err.Error())
	err = DB.Transaction(func(tx *gorm.DB) error {
		_, _, err := CreateUserSubscriptionFromPlanTx(tx, 3208, plan, "order")
		return err
	})
	require.Error(t, err)
	assert.Equal(t, "该套餐已售罄", err.Error())

	// 订阅到期释放名额后可以重新购买，且是新建订阅而非续期
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", first.Id).
		Updates(map[string]interface{}{
			"status":   SubscriptionStatusExpired,
			"end_time": time.Now().Add(-time.Hour).Unix(),
		}).Error)
	require.NoError(t, CheckSubscriptionPlanPurchaseAllowed(3208, plan, true))
	rebought, renewed := createSubscriptionFromPlanForRenewalTest(t, 3208, plan)
	require.False(t, renewed)
	assert.NotEqual(t, first.Id, rebought.Id)
	assert.Equal(t, SubscriptionStatusActive, rebought.Status)
}

func TestRenewal_InactiveSubscriptionExtendsWithoutActivation(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3205, "default")
	activePlan := insertPlanForGroupBillingTest(t, 1205, "Active VIP Plan", false, 0)
	activePlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(activePlan).Error)
	queuedPlan := insertPlanForGroupBillingTest(t, 1206, "Queued Plan", false, 0)

	_, renewed := createSubscriptionFromPlanForRenewalTest(t, 3205, activePlan)
	require.False(t, renewed)
	queued, renewed := createSubscriptionFromPlanForRenewalTest(t, 3205, queuedPlan)
	require.False(t, renewed)
	assert.Equal(t, SubscriptionStatusInactive, queued.Status)
	queuedEnd := queued.EndTime

	extended, renewed := createSubscriptionFromPlanForRenewalTest(t, 3205, queuedPlan)
	require.True(t, renewed)
	assert.Equal(t, queued.Id, extended.Id)
	assert.Equal(t, SubscriptionStatusInactive, extended.Status)
	assert.Equal(t, time.Unix(queuedEnd, 0).AddDate(0, 1, 0).Unix(), extended.EndTime)

	// 续期未激活订阅不影响生效订阅与用户分组
	var activeCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ?", 3205, SubscriptionStatusActive).
		Count(&activeCount).Error)
	assert.EqualValues(t, 1, activeCount)
	var user User
	require.NoError(t, DB.Where("id = ?", 3205).First(&user).Error)
	assert.Equal(t, "vip", user.Group)
}

func TestPurchaseSubscriptionWithBalance_SecondPurchaseRenews(t *testing.T) {
	truncateTables(t)

	requiredQuota, err := calcSubscriptionBalanceQuota(9.99)
	require.NoError(t, err)
	user := insertUserForSubscriptionTest(t, 3207, "default")
	user.Quota = requiredQuota * 3
	require.NoError(t, DB.Save(user).Error)

	plan := insertPlanForGroupBillingTest(t, 1207, "Balance Renewable Plan", false, 0)

	require.NoError(t, PurchaseSubscriptionWithBalance(3207, plan.Id))
	var first UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", 3207, plan.Id).First(&first).Error)
	firstEnd := first.EndTime

	require.NoError(t, PurchaseSubscriptionWithBalance(3207, plan.Id))

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 3207, plan.Id).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)

	renewedSub := getUserSubscriptionForGroupBillingTest(t, first.Id)
	assert.Equal(t, time.Unix(firstEnd, 0).AddDate(0, 1, 0).Unix(), renewedSub.EndTime)
	assert.EqualValues(t, 2000, renewedSub.AmountTotal)

	// 两笔订单都成功入账，余额扣了两次
	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("user_id = ? AND plan_id = ? AND status = ?", 3207, plan.Id, common.TopUpStatusSuccess).
		Count(&orderCount).Error)
	assert.EqualValues(t, 2, orderCount)
	var charged User
	require.NoError(t, DB.Where("id = ?", 3207).First(&charged).Error)
	assert.Equal(t, requiredQuota, charged.Quota)
}
