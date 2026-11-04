package model

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedSubscriptionResetPlan(t *testing.T, plan *SubscriptionPlan) {
	t.Helper()
	require.NoError(t, DB.Create(plan).Error)
}

func seedSubscriptionResetSub(t *testing.T, sub *UserSubscription) {
	t.Helper()
	require.NoError(t, DB.Create(sub).Error)
}

func getSubscriptionResetSub(t *testing.T, id int) UserSubscription {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", id).First(&sub).Error)
	return sub
}

func TestAdminResetUserSubscriptionsByPlanResetsAllActiveMatchesAndAdvancesTime(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9101,
		Title:            "Pro",
		PriceAmount:      10,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      1000,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	otherPlan := &SubscriptionPlan{
		Id:               9102,
		Title:            "Basic",
		PriceAmount:      1,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      100,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetPlan(t, otherPlan)

	activeEnd := now + 30*24*3600
	expiredEnd := now - 1
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9201, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 300, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9202, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9203, UserId: 101, PlanId: otherPlan.Id, AmountTotal: 100, AmountUsed: 60, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9204, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 700, StartTime: now - 7200, EndTime: expiredEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9205, UserId: 102, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 800, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9206, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 120})

	beforeReset := GetDBTimestamp()
	result, err := AdminResetUserSubscriptionsByPlan(101, plan.Id, true)
	afterReset := GetDBTimestamp()

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, 2, result.MatchedCount)
	assert.Equal(t, 2, result.ResetCount)
	assert.Equal(t, 1, result.UserCount)
	assert.Equal(t, []int{101}, result.AffectedUserIds)
	assert.True(t, result.AdvanceResetTime)

	for _, id := range []int{9201, 9202} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.GreaterOrEqual(t, sub.LastResetTime, beforeReset)
		assert.LessOrEqual(t, sub.LastResetTime, afterReset)
		assert.Equal(t, calcNextResetTime(time.Unix(sub.LastResetTime, 0), plan, sub.EndTime), sub.NextResetTime)
	}
	assert.EqualValues(t, 60, getSubscriptionResetSub(t, 9203).AmountUsed)
	assert.EqualValues(t, 700, getSubscriptionResetSub(t, 9204).AmountUsed)
	assert.EqualValues(t, 800, getSubscriptionResetSub(t, 9205).AmountUsed)
	assert.EqualValues(t, 900, getSubscriptionResetSub(t, 9206).AmountUsed)
}

func TestAdminResetUserSubscriptionsByPlanKeepsResetTimes(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9301,
		Title:            "Team",
		PriceAmount:      20,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      2000,
		QuotaResetPeriod: SubscriptionResetMonthly,
	}
	seedSubscriptionResetPlan(t, plan)

	lastReset := now - 86400
	nextReset := now + 86400
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9302, UserId: 201, PlanId: plan.Id, AmountTotal: 2000, AmountUsed: 1200, StartTime: now - 172800, EndTime: now + 30*24*3600, Status: "active", LastResetTime: lastReset, NextResetTime: nextReset})

	result, err := AdminResetUserSubscriptionsByPlan(201, plan.Id, false)

	require.NoError(t, err)
	assert.False(t, result.AdvanceResetTime)
	sub := getSubscriptionResetSub(t, 9302)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, lastReset, sub.LastResetTime)
	assert.Equal(t, nextReset, sub.NextResetTime)
}

func TestAdminResetUserSubscriptionsByPlanNoActiveMatchReturnsError(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:            9401,
		Title:         "Expired",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9402, UserId: 301, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 7200, EndTime: now - 1, Status: "active"})

	result, err := AdminResetUserSubscriptionsByPlan(301, plan.Id, true)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, strings.Contains(err.Error(), "该用户没有有效的此套餐订阅"))
}

func TestAdminResetPlanSubscriptionsResetsAllActiveUsers(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9501,
		Title:            "Business",
		PriceAmount:      30,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      3000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	seedSubscriptionResetPlan(t, plan)

	activeEnd := now + 30*24*3600
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9502, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1000, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9503, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1100, StartTime: now - 3500, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9504, UserId: 402, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1200, StartTime: now - 3400, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9505, UserId: 403, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1300, StartTime: now - 7200, EndTime: now - 1, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9506, UserId: 404, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1400, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 10})

	result, err := AdminResetPlanSubscriptions(plan.Id, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 3, result.MatchedCount)
	assert.Equal(t, 3, result.ResetCount)
	assert.Equal(t, 2, result.UserCount)
	assert.Equal(t, []int{401, 402}, result.AffectedUserIds)
	for _, id := range []int{9502, 9503, 9504} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.Zero(t, sub.LastResetTime)
		assert.Zero(t, sub.NextResetTime)
	}
	assert.EqualValues(t, 1300, getSubscriptionResetSub(t, 9505).AmountUsed)
	assert.EqualValues(t, 1400, getSubscriptionResetSub(t, 9506).AmountUsed)
}

func TestAdminResetPlanSubscriptionsNoMatchSucceeds(t *testing.T) {
	truncateTables(t)

	plan := &SubscriptionPlan{
		Id:            9601,
		Title:         "Empty",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)

	result, err := AdminResetPlanSubscriptions(plan.Id, true)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Zero(t, result.MatchedCount)
	assert.Zero(t, result.ResetCount)
	assert.Zero(t, result.UserCount)
	assert.Empty(t, result.AffectedUserIds)
}

func TestAdminResetSubscriptionClearsEveryTierWithoutChangingEntitlements(t *testing.T) {
	for _, advance := range []bool{false, true} {
		name := "preserve-schedule"
		if advance {
			name = "advance-schedule"
		}
		t.Run(name, func(t *testing.T) {
			truncateTables(t)
			now := time.Date(2026, time.November, 4, 10, 20, 30, 0, time.Local).Unix()
			tiers := []QuotaTier{
				{Period: TierPeriodHourly, Limit: 100, SortPriority: 10},
				{Period: TierPeriodCustom, CustomSeconds: 5 * 3600, Limit: 200, SortPriority: 20},
				{Period: TierPeriodDaily, Limit: 500, SortPriority: 30},
				{Period: TierPeriodWeekly, Limit: 1000, SortPriority: 40},
				{Period: TierPeriodMonthly, Limit: 2000, SortPriority: 50},
				{Period: TierPeriodNone, Limit: 10000, SortPriority: 60},
			}
			encoded, err := common.Marshal(tiers)
			require.NoError(t, err)
			plan := &SubscriptionPlan{
				Title: "tier reset", DurationUnit: SubscriptionDurationDay, DurationValue: 30,
				TotalAmount: 50000, QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 7200,
				QuotaTiers: string(encoded),
			}
			seedSubscriptionResetPlan(t, plan)
			user := &User{Username: "tier-reset", Quota: 1234, UsedQuota: 567}
			require.NoError(t, DB.Create(user).Error)
			sub := &UserSubscription{
				UserId: user.Id, PlanId: plan.Id, Status: SubscriptionStatusActive,
				AmountTotal: plan.TotalAmount, AmountUsed: 321, StartTime: now - 3600, EndTime: now + 30*86400,
				LastResetTime: now - 1000, NextResetTime: now + 1000,
			}
			seedSubscriptionResetSub(t, sub)
			originalSub := *sub
			before := make([]UserSubscriptionTierUsage, len(tiers))
			for i, tier := range tiers {
				usage := UserSubscriptionTierUsage{
					UserSubscriptionId: sub.Id, TierIndex: i,
					UsageInPeriod: int64(11 + i), UsageInWindow: int64(21 + i), ResetVersion: 3,
				}
				if isCalendarAligned(tier) {
					start, end := calcTierPeriodBoundaries(time.Unix(now-40*86400, 0), tier.Period)
					usage.PeriodStart, usage.NextPeriodStart = start.Unix(), end.Unix()
				} else if isSlidingWindow(tier) {
					usage.WindowStart, usage.WindowSeconds = now-300, getSlidingWindowSeconds(tier)
				}
				require.NoError(t, DB.Create(&usage).Error)
				before[i] = usage
			}

			require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
				_, err := adminResetUserSubscriptionsByPlanTx(tx, user.Id, plan, now, advance)
				return err
			}))

			updated := getSubscriptionResetSub(t, sub.Id)
			assert.Zero(t, updated.AmountUsed)
			assert.Equal(t, originalSub.AmountTotal, updated.AmountTotal)
			assert.Equal(t, originalSub.StartTime, updated.StartTime)
			assert.Equal(t, originalSub.EndTime, updated.EndTime)
			assert.Equal(t, originalSub.Status, updated.Status)
			if advance {
				assert.Equal(t, now, updated.LastResetTime)
				assert.Equal(t, now+7200, updated.NextResetTime)
			} else {
				assert.Equal(t, originalSub.LastResetTime, updated.LastResetTime)
				assert.Equal(t, originalSub.NextResetTime, updated.NextResetTime)
			}
			var after []UserSubscriptionTierUsage
			require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index").Find(&after).Error)
			require.Len(t, after, len(tiers))
			for i, usage := range after {
				assert.Zero(t, usage.UsageInPeriod)
				assert.Zero(t, usage.UsageInWindow)
				assert.EqualValues(t, 4, usage.ResetVersion)
				expected := before[i]
				if advance && isCalendarAligned(tiers[i]) {
					start, end := calcTierPeriodBoundaries(time.Unix(now, 0), tiers[i].Period)
					expected.PeriodStart, expected.NextPeriodStart = start.Unix(), end.Unix()
				} else if advance && isSlidingWindow(tiers[i]) {
					expected.WindowStart = now
				}
				assert.Equal(t, expected.PeriodStart, usage.PeriodStart)
				assert.Equal(t, expected.NextPeriodStart, usage.NextPeriodStart)
				assert.Equal(t, expected.WindowStart, usage.WindowStart)
				assert.Equal(t, expected.WindowSeconds, usage.WindowSeconds)
			}
			var savedPlan SubscriptionPlan
			require.NoError(t, DB.First(&savedPlan, plan.Id).Error)
			assert.Equal(t, plan.QuotaTiers, savedPlan.QuotaTiers)
			assert.Equal(t, plan.TotalAmount, savedPlan.TotalAmount)
			require.NoError(t, DB.First(user, user.Id).Error)
			assert.Equal(t, 1234, user.Quota)
			assert.Equal(t, 567, user.UsedQuota)
		})
	}
}

func TestAdminResetInitializesMissingTierUsage(t *testing.T) {
	for _, advance := range []bool{false, true} {
		name := "wait-for-first-use"
		if advance {
			name = "start-window-now"
		}
		t.Run(name, func(t *testing.T) {
			plan, sub := seedPeriodicBillingSubscription(t, true)
			require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Delete(&UserSubscriptionTierUsage{}).Error)
			now := GetDBTimestamp()
			require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
				_, err := adminResetUserSubscriptionsByPlanTx(tx, sub.UserId, plan, now, advance)
				return err
			}))
			var usages []UserSubscriptionTierUsage
			require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index").Find(&usages).Error)
			require.Len(t, usages, 3)
			start, end := calcTierPeriodBoundaries(time.Unix(now, 0), TierPeriodDaily)
			assert.Equal(t, start.Unix(), usages[0].PeriodStart)
			assert.Equal(t, end.Unix(), usages[0].NextPeriodStart)
			assert.EqualValues(t, 60, usages[1].WindowSeconds)
			if advance {
				assert.Equal(t, now, usages[1].WindowStart)
			} else {
				assert.Zero(t, usages[1].WindowStart)
			}
			for _, usage := range usages {
				assert.Zero(t, usage.UsageInPeriod+usage.UsageInWindow)
				assert.EqualValues(t, 1, usage.ResetVersion)
			}
			_, err := PreConsumeUserSubscription("newly-initialized", sub.UserId, "test", 0, 30, "default")
			require.NoError(t, err)
			require.NoError(t, RefundSubscriptionPreConsume("newly-initialized"))
			require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Find(&usages).Error)
			for _, usage := range usages {
				assert.Zero(t, usage.UsageInPeriod+usage.UsageInWindow)
			}
		})
	}
}
