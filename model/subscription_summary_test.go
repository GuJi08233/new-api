package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionSummaryIncludesUnavailablePlanAndCurrentQuotaWithoutWritingCounters(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.Local).Unix()
	tiers, err := common.Marshal([]QuotaTier{
		{Period: TierPeriodDaily, Limit: 100, SortPriority: 10},
		{Period: TierPeriodCustom, CustomSeconds: 3600, Limit: 200, SortPriority: 20},
		{Period: TierPeriodNone, Limit: 1000, SortPriority: 30},
	})
	require.NoError(t, err)
	plan := SubscriptionPlan{
		Title: "Unavailable historical plan", TotalAmount: 100,
		QuotaResetPeriod: SubscriptionResetDaily, QuotaTiers: string(tiers),
	}
	require.NoError(t, DB.Create(&plan).Error)
	require.NoError(t, DB.Model(&plan).Update("enabled", false).Error)
	sub := UserSubscription{
		UserId: 1, PlanId: plan.Id, Status: SubscriptionStatusActive,
		AmountTotal: 100, AmountUsed: 65, StartTime: now - 2*86400, EndTime: now + 86400,
		LastResetTime: now - 2*86400, NextResetTime: now - 86400,
	}
	require.NoError(t, DB.Create(&sub).Error)
	usages := []UserSubscriptionTierUsage{
		{UserSubscriptionId: sub.Id, TierIndex: 0, PeriodStart: now - 2*86400, NextPeriodStart: now - 86400, UsageInPeriod: 65},
		{UserSubscriptionId: sub.Id, TierIndex: 1, WindowStart: now - 7200, WindowSeconds: 3600, UsageInWindow: 65},
		{UserSubscriptionId: sub.Id, TierIndex: 2, UsageInWindow: 265},
	}
	require.NoError(t, DB.Create(&usages).Error)

	summaries, err := buildSubscriptionSummaries([]UserSubscription{sub}, now)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	summary := summaries[0]
	assert.Equal(t, plan.Title, summary.PlanTitle)
	assert.Equal(t, plan.QuotaTiers, summary.QuotaTiers)
	assert.Equal(t, SubscriptionResetDaily, summary.QuotaResetPeriod)
	assert.Zero(t, summary.Subscription.AmountUsed)
	start, end := calcTierPeriodBoundaries(time.Unix(now, 0), TierPeriodDaily)
	assert.Equal(t, start.Unix(), summary.Subscription.LastResetTime)
	assert.Equal(t, end.Unix(), summary.Subscription.NextResetTime)
	require.Len(t, summary.TierUsages, 3)
	assert.Zero(t, summary.TierUsages[0].UsageInPeriod)
	assert.Equal(t, end.Unix(), summary.TierUsages[0].NextPeriodStart)
	assert.Zero(t, summary.TierUsages[1].UsageInWindow)
	assert.Zero(t, summary.TierUsages[1].WindowStart)
	assert.EqualValues(t, 265, summary.TierUsages[2].UsageInWindow)

	var savedSub UserSubscription
	require.NoError(t, DB.First(&savedSub, sub.Id).Error)
	assert.EqualValues(t, 65, savedSub.AmountUsed)
	assert.Equal(t, sub.NextResetTime, savedSub.NextResetTime)
	var savedUsages []UserSubscriptionTierUsage
	require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index asc").Find(&savedUsages).Error)
	assert.Equal(t, usages, savedUsages)
}

func TestSubscriptionSummaryKeepsCurrentWindowAndNonResettingUsage(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.Local).Unix()
	plan := SubscriptionPlan{
		Title: "Current limits", TotalAmount: 1000,
		QuotaTiers: `[{"period":"custom","custom_seconds":3600,"limit":100,"sort_priority":10}]`,
	}
	require.NoError(t, DB.Create(&plan).Error)
	sub := UserSubscription{
		UserId: 1, PlanId: plan.Id, Status: SubscriptionStatusActive,
		AmountTotal: 1000, AmountUsed: 65, StartTime: now - 86400, EndTime: now + 86400,
	}
	require.NoError(t, DB.Create(&sub).Error)
	usage := UserSubscriptionTierUsage{
		UserSubscriptionId: sub.Id, TierIndex: 0, WindowStart: now - 60,
		WindowSeconds: 3600, UsageInWindow: 65,
	}
	require.NoError(t, DB.Create(&usage).Error)

	summaries, err := buildSubscriptionSummaries([]UserSubscription{sub}, now)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.EqualValues(t, 65, summaries[0].Subscription.AmountUsed)
	assert.Equal(t, SubscriptionResetNever, summaries[0].QuotaResetPeriod)
	require.Len(t, summaries[0].TierUsages, 1)
	assert.EqualValues(t, 65, summaries[0].TierUsages[0].UsageInWindow)
	assert.Equal(t, usage.WindowStart, summaries[0].TierUsages[0].WindowStart)
}

func TestSubscriptionSummaryProjectsCustomAllowanceWithoutMovingResetPastExpiry(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.Local).Unix()
	plan := SubscriptionPlan{
		Title: "Custom allowance", TotalAmount: 100,
		QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 60,
	}
	require.NoError(t, DB.Create(&plan).Error)
	for _, tt := range []struct {
		name      string
		endTime   int64
		nextReset int64
	}{
		{name: "next period available", endTime: now + 120, nextReset: now + 55},
		{name: "expires before next period", endTime: now + 30},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sub := UserSubscription{
				PlanId: plan.Id, Status: SubscriptionStatusActive,
				AmountTotal: 100, AmountUsed: 65, StartTime: now - 125, EndTime: tt.endTime,
				LastResetTime: now - 125, NextResetTime: now - 65,
			}
			summaries, err := buildSubscriptionSummaries([]UserSubscription{sub}, now)
			require.NoError(t, err)
			require.Len(t, summaries, 1)
			assert.Zero(t, summaries[0].Subscription.AmountUsed)
			assert.Equal(t, now-5, summaries[0].Subscription.LastResetTime)
			assert.Equal(t, tt.nextReset, summaries[0].Subscription.NextResetTime)
			assert.EqualValues(t, 60, summaries[0].QuotaResetCustomSeconds)
		})
	}
}
