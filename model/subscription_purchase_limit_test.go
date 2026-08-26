package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPurchaseLimit_GlobalLimitedPlanRejectsRepurchaseWhileHoldingSeat(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3401, "default")
	// 名额充足（10 席）但持有者已占一席：重复购买必须被拒，否则一人占两席，
	// 且第二份只是排队订阅——有效期从购买时刻起算，到期时也不会自动接棒。
	plan := insertPlanForGroupBillingTest(t, 1401, "Ten Seat Plan", false, 10)
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3401, plan)
	require.False(t, renewed)

	err := CheckSubscriptionPlanPurchaseAllowed(3401, plan, true)
	require.Error(t, err)
	assert.Equal(t, "已持有该套餐，到期后可重新购买", err.Error())

	err = DB.Transaction(func(tx *gorm.DB) error {
		_, _, err := CreateUserSubscriptionFromPlanTx(tx, 3401, plan, "order")
		return err
	})
	require.Error(t, err)
	assert.Equal(t, "已持有该套餐，到期后可重新购买", err.Error())

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("plan_id = ?", plan.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	// 其余名额对其他用户仍然开放
	insertUserForSubscriptionTest(t, 3402, "default")
	require.NoError(t, CheckSubscriptionPlanPurchaseAllowed(3402, plan, true))

	// 到期释放名额后本人可以重新购买
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("id = ?", first.Id).
		Updates(map[string]interface{}{
			"status":   SubscriptionStatusExpired,
			"end_time": time.Now().Add(-time.Hour).Unix(),
		}).Error)
	require.NoError(t, CheckSubscriptionPlanPurchaseAllowed(3401, plan, true))
}

// 同一套餐一人同时只允许一条订阅，attach 模式也不例外：无全局限购时再买是续期，
// 有全局限购时持有期内直接拒绝，避免重复追加同一分组。
func TestPurchaseLimit_AttachPlanRenewsInsteadOfStacking(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3403, "default")
	plan := insertAttachPlanForTest(t, 1402, "embedding")

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3403, plan)
	require.False(t, renewed)
	assert.Equal(t, SubscriptionStatusActive, first.Status)
	assert.Equal(t, SubscriptionGroupModeAttach, first.GroupMode)
	firstEnd := first.EndTime

	require.NoError(t, CheckSubscriptionPlanPurchaseAllowed(3403, plan, true))
	second, renewed := createSubscriptionFromPlanForRenewalTest(t, 3403, plan)
	require.True(t, renewed)
	assert.Equal(t, first.Id, second.Id)
	assert.Equal(t, time.Unix(firstEnd, 0).AddDate(0, 1, 0).Unix(), second.EndTime)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ? AND plan_id = ?", 3403, plan.Id).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)
	saved := getUserSubscriptionForGroupBillingTest(t, first.Id)
	assert.Equal(t, SubscriptionGroupModeAttach, saved.GroupMode)
}

func TestPurchaseLimit_GlobalLimitedAttachPlanRejectsRepurchase(t *testing.T) {
	truncateTables(t)

	insertUserForSubscriptionTest(t, 3404, "default")
	plan := insertPlanForGroupBillingTest(t, 1403, "Attach Seat Plan", false, 10)
	plan.GroupMode = SubscriptionGroupModeAttach
	plan.UpgradeGroup = "embedding"
	plan.MaxPurchaseResetPeriod = SubscriptionResetActive
	require.NoError(t, DB.Save(plan).Error)

	first, renewed := createSubscriptionFromPlanForRenewalTest(t, 3404, plan)
	require.False(t, renewed)
	assert.Equal(t, SubscriptionGroupModeAttach, first.GroupMode)

	err := CheckSubscriptionPlanPurchaseAllowed(3404, plan, true)
	require.Error(t, err)
	assert.Equal(t, "已持有该套餐，到期后可重新购买", err.Error())

	err = DB.Transaction(func(tx *gorm.DB) error {
		_, _, err := CreateUserSubscriptionFromPlanTx(tx, 3404, plan, "order")
		return err
	})
	require.Error(t, err)
	assert.Equal(t, "已持有该套餐，到期后可重新购买", err.Error())
}

// 人均上限与全局上限共用套餐的名额窗口：名额怎么释放，两个维度口径一致。
func TestPurchaseLimit_PerUserWindowFollowsPlanResetPeriod(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		period      string
		subStart    int64
		subEnd      int64
		expectError string
	}{
		{
			name:     "到期释放名额：已过期订阅不再占用人均名额",
			period:   SubscriptionResetActive,
			subStart: now.Add(-40 * 24 * time.Hour).Unix(),
			subEnd:   now.Add(-10 * 24 * time.Hour).Unix(),
		},
		{
			name:     "按月刷新：上个周期的购买不占本周期名额",
			period:   SubscriptionResetMonthly,
			subStart: now.AddDate(0, -1, 0).Unix(),
			subEnd:   now.Add(-24 * time.Hour).Unix(),
		},
		{
			name:        "按月刷新：本周期内的购买仍占名额",
			period:      SubscriptionResetMonthly,
			subStart:    now.Unix(),
			subEnd:      now.Unix(),
			expectError: "已达到该套餐购买上限",
		},
		{
			name:        "不刷新：购买次数按套餐生命周期累计",
			period:      SubscriptionResetNever,
			subStart:    now.Add(-40 * 24 * time.Hour).Unix(),
			subEnd:      now.Add(-10 * 24 * time.Hour).Unix(),
			expectError: "已达到该套餐购买上限",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)

			userId := 3410 + i
			insertUserForSubscriptionTest(t, userId, "default")
			plan := insertPlanForGroupBillingTest(t, 1410+i, tc.name, false, 0)
			plan.MaxPurchasePerUser = 1
			plan.MaxPurchaseResetPeriod = tc.period
			require.NoError(t, DB.Save(plan).Error)

			require.NoError(t, DB.Create(&UserSubscription{
				UserId:      userId,
				PlanId:      plan.Id,
				AmountTotal: 1000,
				Status:      SubscriptionStatusExpired,
				StartTime:   tc.subStart,
				EndTime:     tc.subEnd,
			}).Error)

			err := CheckSubscriptionPlanPurchaseAllowed(userId, plan, true)
			if tc.expectError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.expectError, err.Error())
		})
	}
}
