package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanQuotaConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name string
		plan model.SubscriptionPlan
		err  string
	}{
		{name: "unlimited"},
		{name: "total allowance", plan: model.SubscriptionPlan{TotalAmount: 100}},
		{name: "periodic allowance", plan: model.SubscriptionPlan{TotalAmount: 100, QuotaResetPeriod: model.SubscriptionResetDaily}},
		{name: "multiple limits", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":100},{"period":"none","limit":1000}]`}},
		{name: "mixed total", plan: model.SubscriptionPlan{TotalAmount: 1000, QuotaTiers: `[{"period":"daily","limit":100}]`}, err: "不能同时设置"},
		{name: "mixed reset", plan: model.SubscriptionPlan{QuotaResetPeriod: model.SubscriptionResetDaily, QuotaTiers: `[{"period":"daily","limit":100}]`}, err: "不能同时设置"},
		{name: "empty periodic allowance", plan: model.SubscriptionPlan{QuotaResetPeriod: model.SubscriptionResetDaily}, err: "周期额度需大于0"},
		{name: "unsafe total", plan: model.SubscriptionPlan{TotalAmount: common.MaxQuota}, err: "最大安全额度"},
		{name: "negative total", plan: model.SubscriptionPlan{TotalAmount: -1}, err: "大于等于0"},
		{name: "zero tier", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":0}]`}, err: "限额需大于0"},
		{name: "unsafe tier", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":9007199254740992}]`}, err: "最大安全额度"},
		{name: "duplicate period", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":100},{"period":"daily","limit":200}]`}, err: "重复配置"},
		{name: "duplicate custom period", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"custom","custom_seconds":7200,"limit":100},{"period":"custom","custom_seconds":7200,"limit":200}]`}, err: "重复配置"},
		{name: "hourly duplicates one hour custom window", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"hourly","limit":100},{"period":"custom","custom_seconds":3600,"limit":200}]`}, err: "重复配置"},
		{name: "calendar day and custom day are distinct", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":100},{"period":"custom","custom_seconds":86400,"limit":200}]`}},
		{name: "missing custom duration", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"custom","limit":100}]`}, err: "自定义周期秒数需大于0"},
		{name: "decreasing allowance", plan: model.SubscriptionPlan{QuotaTiers: `[{"period":"daily","limit":200},{"period":"weekly","limit":100}]`}, err: "不能小于"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubscriptionPlanQuota(&tt.plan, nil)
			if tt.err != "" {
				assert.Contains(t, err, tt.err)
				return
			}
			assert.Empty(t, err)
		})
	}
}

func TestSubscriptionPlanCustomWindowsCanCoexistAndSortByDuration(t *testing.T) {
	plan := model.SubscriptionPlan{QuotaTiers: `[{"period":"custom","custom_seconds":7200,"limit":200},{"period":"none","limit":1000},{"period":"custom","custom_seconds":1800,"limit":50},{"period":"hourly","limit":100}]`}
	require.Empty(t, validateSubscriptionPlanQuota(&plan, nil))
	var tiers []model.QuotaTier
	require.NoError(t, common.UnmarshalJsonStr(plan.QuotaTiers, &tiers))
	require.Len(t, tiers, 4)
	assert.Equal(t, []model.QuotaTier{
		{Period: model.TierPeriodCustom, CustomSeconds: 1800, Limit: 50, SortPriority: 10},
		{Period: model.TierPeriodHourly, Limit: 100, SortPriority: 20},
		{Period: model.TierPeriodCustom, CustomSeconds: 7200, Limit: 200, SortPriority: 30},
		{Period: model.TierPeriodNone, Limit: 1000, SortPriority: 40},
	}, tiers)
}

func TestSubscriptionPlanHistoricalQuotaCanBePreservedButNotChangedToAnotherMix(t *testing.T) {
	existing := model.SubscriptionPlan{
		TotalAmount: 100, QuotaResetPeriod: model.SubscriptionResetDaily,
		QuotaTiers: `[{"period":"daily","limit":100,"sort_priority":100},{"period":"none","limit":0,"sort_priority":200}]`,
	}
	request := existing
	request.Title = "Renamed plan"
	require.Empty(t, validateSubscriptionPlanQuota(&request, &existing))
	assert.Equal(t, existing.QuotaTiers, request.QuotaTiers)

	request.TotalAmount = 200
	assert.NotEmpty(t, validateSubscriptionPlanQuota(&request, &existing))

	request.QuotaTiers = "[]"
	require.Empty(t, validateSubscriptionPlanQuota(&request, &existing))
	assert.EqualValues(t, 200, request.TotalAmount)
}

func TestAdminSubscriptionQuotaResetPreservesScheduleByDefault(t *testing.T) {
	assert.False(t, resolveAdvanceResetTime(nil))
	assert.False(t, resolveAdvanceResetTime(common.GetPointer(false)))
	assert.True(t, resolveAdvanceResetTime(common.GetPointer(true)))
}
