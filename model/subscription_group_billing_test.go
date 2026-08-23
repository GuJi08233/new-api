package model

import (
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertPlanForGroupBillingTest(t *testing.T, id int, title string, disableBalance bool, maxPurchaseTotal int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:                      id,
		Title:                   title,
		PriceAmount:             9.99,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationMonth,
		DurationValue:           1,
		Enabled:                 true,
		TotalAmount:             1000,
		DisableBalanceDeduction: disableBalance,
		MaxPurchaseTotal:        maxPurchaseTotal,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertUserForSubscriptionTest(t *testing.T, id int, group string) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "user-" + strconv.Itoa(id),
		Password: "password123",
		Group:    group,
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
		AffCode:  "aff-" + strconv.Itoa(id),
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func insertUserSubscriptionForGroupBillingTest(t *testing.T, id int, userId int, planId int, upgradeGroup string, totalAmount int64) *UserSubscription {
	t.Helper()
	sub := &UserSubscription{
		Id:           id,
		UserId:       userId,
		PlanId:       planId,
		AmountTotal:  totalAmount,
		AmountUsed:   0,
		Status:       "active",
		StartTime:    time.Now().Unix(),
		EndTime:      time.Now().Add(30 * 24 * time.Hour).Unix(),
		UpgradeGroup: upgradeGroup,
	}
	require.NoError(t, DB.Create(sub).Error)
	return sub
}

func getUserSubscriptionForGroupBillingTest(t *testing.T, id int) *UserSubscription {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", id).First(&sub).Error)
	return &sub
}

func TestHasDisableBalanceDeductionSubscriptionForUsingGroup(t *testing.T) {
	truncateTables(t)

	vipPlan := insertPlanForGroupBillingTest(t, 1001, "VIP Plan", true, 0)
	insertUserSubscriptionForGroupBillingTest(t, 2001, 3001, vipPlan.Id, "vip", 1000)

	hasVipLock, err := HasDisableBalanceDeductionSubscriptionForUsingGroup(3001, "vip")
	require.NoError(t, err)
	assert.True(t, hasVipLock)

	hasDefaultLock, err := HasDisableBalanceDeductionSubscriptionForUsingGroup(3001, "default")
	require.NoError(t, err)
	assert.False(t, hasDefaultLock)
}

func TestPreConsumeUserSubscription_MatchesUsingGroup(t *testing.T) {
	truncateTables(t)

	vipPlan := insertPlanForGroupBillingTest(t, 1002, "VIP Plan", true, 0)
	vipSub := insertUserSubscriptionForGroupBillingTest(t, 2002, 3002, vipPlan.Id, "vip", 1000)

	_, err := PreConsumeUserSubscription("req-default", 3002, "gpt-test", 0, 100, "default")
	require.Error(t, err)

	result, err := PreConsumeUserSubscription("req-vip", 3002, "gpt-test", 0, 100, "vip")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, vipSub.Id, result.UserSubscriptionId)
	assert.EqualValues(t, 100, result.PreConsumed)

	updatedVipSub := getUserSubscriptionForGroupBillingTest(t, vipSub.Id)
	assert.EqualValues(t, 100, updatedVipSub.AmountUsed)
}

func TestCheckSubscriptionPlanPurchaseAllowed_GlobalLimitIncludesPendingOrders(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1003, "Limited Plan", false, 2)
	insertUserSubscriptionForGroupBillingTest(t, 2003, 3003, plan.Id, "", 1000)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:     3004,
		PlanId:     plan.Id,
		Money:      9.99,
		TradeNo:    "pending-plan-limit",
		Status:     common.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}).Error)

	err := CheckSubscriptionPlanPurchaseAllowed(3005, plan, true)
	require.Error(t, err)
	assert.Equal(t, "该套餐已售罄", err.Error())
}

func TestCheckSubscriptionPlanPurchaseAllowed_GlobalLimitResetsByDay(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1005, "Daily Limited Plan", false, 1)
	plan.MaxPurchaseResetPeriod = SubscriptionResetDaily
	require.NoError(t, DB.Save(plan).Error)

	yesterday := time.Now().Add(-24 * time.Hour).Unix()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2005,
		UserId:      3008,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusActive,
		StartTime:   yesterday,
		EndTime:     time.Now().Add(29 * 24 * time.Hour).Unix(),
	}).Error)

	err := CheckSubscriptionPlanPurchaseAllowed(3009, plan, true)
	require.NoError(t, err)
}

func TestCountSubscriptionPlanPurchaseCounts_UsesCurrentWindowAndPendingOrders(t *testing.T) {
	truncateTables(t)

	now := time.Now()
	plan := insertPlanForGroupBillingTest(t, 1006, "Rolling Seats", false, 2)
	plan.MaxPurchaseResetPeriod = SubscriptionResetDaily
	require.NoError(t, DB.Save(plan).Error)

	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2006,
		UserId:      3013,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusActive,
		StartTime:   now.Unix(),
		EndTime:     now.Add(30 * 24 * time.Hour).Unix(),
	}).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2007,
		UserId:      3014,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusExpired,
		StartTime:   now.Add(-48 * time.Hour).Unix(),
		EndTime:     now.Add(-24 * time.Hour).Unix(),
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:     3015,
		PlanId:     plan.Id,
		Money:      9.99,
		TradeNo:    "pending-current-window",
		Status:     common.TopUpStatusPending,
		CreateTime: now.Unix(),
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:     3016,
		PlanId:     plan.Id,
		Money:      9.99,
		TradeNo:    "pending-old-window",
		Status:     common.TopUpStatusPending,
		CreateTime: now.Add(-48 * time.Hour).Unix(),
	}).Error)

	counts, err := CountSubscriptionPlanPurchaseCounts([]SubscriptionPlan{*plan}, true)
	require.NoError(t, err)
	assert.EqualValues(t, 2, counts[plan.Id])
}

func TestCheckSubscriptionPlanPurchaseAllowed_IgnoresExpiredPendingOrders(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1007, "Hold Limited Plan", false, 1)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:     3017,
		PlanId:     plan.Id,
		Money:      9.99,
		TradeNo:    "pending-expired-window",
		Status:     common.TopUpStatusPending,
		CreateTime: time.Now().Add(-31 * time.Minute).Unix(),
	}).Error)

	err := CheckSubscriptionPlanPurchaseAllowed(3018, plan, true)
	require.NoError(t, err)
}

func TestCompleteSubscriptionOrder_ExpiresStalePendingOrder(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1008, "Timeout Plan", false, 1)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:          3019,
		PlanId:          plan.Id,
		Money:           9.99,
		TradeNo:         "pending-timeout-complete",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Add(-31 * time.Minute).Unix(),
	}).Error)

	err := CompleteSubscriptionOrder("pending-timeout-complete", "{}", PaymentProviderStripe, PaymentMethodStripe)
	require.ErrorIs(t, err, ErrSubscriptionOrderExpired)

	order := GetSubscriptionOrderByTradeNo("pending-timeout-complete")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusExpired, order.Status)
}

func TestCheckSubscriptionPlanPurchaseAllowed_ActiveLimitBlocksWhileSubscriptionIsLive(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1009, "Single Active Seat", false, 1)
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	insertUserSubscriptionForGroupBillingTest(t, 2008, 3020, plan.Id, "", 1000)

	err := CheckSubscriptionPlanPurchaseAllowed(3021, plan, true)
	require.Error(t, err)
	assert.Equal(t, "该套餐已售罄", err.Error())
}

func TestCheckSubscriptionPlanPurchaseAllowed_ActiveLimitReleasesAfterExpiry(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1010, "Single Active Seat", false, 1)
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2009,
		UserId:      3022,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusExpired,
		StartTime:   time.Now().Add(-10 * time.Hour).Unix(),
		EndTime:     time.Now().Add(-5 * time.Hour).Unix(),
	}).Error)

	err := CheckSubscriptionPlanPurchaseAllowed(3023, plan, true)
	require.NoError(t, err)
}

func TestCountSubscriptionPlanPurchaseCounts_ActiveLimitCountsOnlyLiveSubscriptions(t *testing.T) {
	truncateTables(t)

	now := time.Now()
	plan := insertPlanForGroupBillingTest(t, 1011, "Concurrent Seat Plan", false, 2)
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2010,
		UserId:      3024,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusActive,
		StartTime:   now.Add(-1 * time.Hour).Unix(),
		EndTime:     now.Add(4 * time.Hour).Unix(),
	}).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		Id:          2011,
		UserId:      3025,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		Status:      SubscriptionStatusExpired,
		StartTime:   now.Add(-10 * time.Hour).Unix(),
		EndTime:     now.Add(-5 * time.Hour).Unix(),
	}).Error)

	counts, err := CountSubscriptionPlanPurchaseCounts([]SubscriptionPlan{*plan}, true)
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[plan.Id])
}

func TestCreateUserSubscriptionFromPlanTx_GlobalLimitBlocksFurtherCreation(t *testing.T) {
	truncateTables(t)

	plan := insertPlanForGroupBillingTest(t, 1004, "Single Seat Plan", false, 1)
	insertUserSubscriptionForGroupBillingTest(t, 2004, 3006, plan.Id, "", 1000)

	err := DB.Transaction(func(tx *gorm.DB) error {
		_, err := CreateUserSubscriptionFromPlanTx(tx, 3007, plan, "order")
		return err
	})
	require.Error(t, err)
	assert.Equal(t, "该套餐已售罄", err.Error())
}

func TestCreateUserSubscriptionFromPlanTx_SecondSubscriptionBecomesInactive(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3010, "default")
	firstPlan := insertPlanForGroupBillingTest(t, 1010, "VIP Plan", false, 0)
	firstPlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(firstPlan).Error)
	secondPlan := insertPlanForGroupBillingTest(t, 1011, "SVIP Plan", false, 0)
	secondPlan.UpgradeGroup = "svip"
	require.NoError(t, DB.Save(secondPlan).Error)

	var firstSub *UserSubscription
	var secondSub *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		firstSub, err = CreateUserSubscriptionFromPlanTx(tx, 3010, firstPlan, "order")
		if err != nil {
			return err
		}
		secondSub, err = CreateUserSubscriptionFromPlanTx(tx, 3010, secondPlan, "order")
		return err
	})
	require.NoError(t, err)
	require.NotNil(t, firstSub)
	require.NotNil(t, secondSub)

	firstSaved := getUserSubscriptionForGroupBillingTest(t, firstSub.Id)
	secondSaved := getUserSubscriptionForGroupBillingTest(t, secondSub.Id)
	assert.Equal(t, SubscriptionStatusActive, firstSaved.Status)
	assert.Equal(t, SubscriptionStatusInactive, secondSaved.Status)
	assert.Equal(t, "default", firstSaved.PrevUserGroup)
	assert.Empty(t, secondSaved.PrevUserGroup)

	var user User
	require.NoError(t, DB.Where("id = ?", 3010).First(&user).Error)
	assert.Equal(t, "vip", user.Group)
}

func TestSwitchUserActiveSubscription_ChangesActiveAndGroup(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3011, "default")
	vipPlan := insertPlanForGroupBillingTest(t, 1012, "VIP Plan", false, 0)
	vipPlan.UpgradeGroup = "vip"
	require.NoError(t, DB.Save(vipPlan).Error)
	svipPlan := insertPlanForGroupBillingTest(t, 1013, "SVIP Plan", false, 0)
	svipPlan.UpgradeGroup = "svip"
	require.NoError(t, DB.Save(svipPlan).Error)

	var vipSub *UserSubscription
	var svipSub *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		vipSub, err = CreateUserSubscriptionFromPlanTx(tx, 3011, vipPlan, "order")
		if err != nil {
			return err
		}
		svipSub, err = CreateUserSubscriptionFromPlanTx(tx, 3011, svipPlan, "order")
		return err
	})
	require.NoError(t, err)

	msg, err := SwitchUserActiveSubscription(3011, svipSub.Id)
	require.NoError(t, err)
	assert.Contains(t, msg, "svip")

	vipSaved := getUserSubscriptionForGroupBillingTest(t, vipSub.Id)
	svipSaved := getUserSubscriptionForGroupBillingTest(t, svipSub.Id)
	assert.Equal(t, SubscriptionStatusInactive, vipSaved.Status)
	assert.Equal(t, SubscriptionStatusActive, svipSaved.Status)
	assert.Equal(t, "default", svipSaved.PrevUserGroup)

	var user User
	require.NoError(t, DB.Where("id = ?", 3011).First(&user).Error)
	assert.Equal(t, "svip", user.Group)

	var activeCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND status = ?", 3011, SubscriptionStatusActive).
		Count(&activeCount).Error)
	assert.EqualValues(t, 1, activeCount)
}

func TestExpireDueSubscriptions_ExpiresInactiveSubscription(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3012, "default")
	plan := insertPlanForGroupBillingTest(t, 1014, "Inactive Plan", false, 0)
	sub := &UserSubscription{
		Id:           2014,
		UserId:       3012,
		PlanId:       plan.Id,
		AmountTotal:  1000,
		AmountUsed:   100,
		Status:       SubscriptionStatusInactive,
		StartTime:    time.Now().Add(-48 * time.Hour).Unix(),
		EndTime:      time.Now().Add(-1 * time.Hour).Unix(),
		UpgradeGroup: "vip",
	}
	require.NoError(t, DB.Create(sub).Error)

	n, err := ExpireDueSubscriptions(10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	saved := getUserSubscriptionForGroupBillingTest(t, sub.Id)
	assert.Equal(t, SubscriptionStatusExpired, saved.Status)

	var user User
	require.NoError(t, DB.Where("id = ?", 3012).First(&user).Error)
	assert.Equal(t, "default", user.Group)
}
