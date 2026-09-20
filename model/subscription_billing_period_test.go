package model

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedPeriodicBillingSubscription(t *testing.T, multiTier bool) (*SubscriptionPlan, *UserSubscription) {
	t.Helper()
	truncateTables(t)
	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Title: "period billing", DurationUnit: SubscriptionDurationDay, DurationValue: 30,
		TotalAmount: 100, QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 60,
	}
	if multiTier {
		data, err := common.Marshal([]QuotaTier{
			{Period: TierPeriodDaily, Limit: 100},
			{Period: TierPeriodCustom, CustomSeconds: 60, Limit: 100},
			{Period: TierPeriodNone, Limit: 1000},
		})
		require.NoError(t, err)
		plan.QuotaTiers = string(data)
	}
	require.NoError(t, DB.Create(plan).Error)
	sub := &UserSubscription{
		UserId: 1, PlanId: plan.Id, Status: SubscriptionStatusActive,
		AmountTotal: 100, StartTime: now, EndTime: now + 30*86400,
		LastResetTime: now, NextResetTime: now + 60,
	}
	require.NoError(t, DB.Create(sub).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return initTierUsage(tx, sub.Id, plan.GetQuotaTiers(), now)
	}))
	return plan, sub
}

func advanceBillingSubscriptionPeriod(t *testing.T, plan *SubscriptionPlan, sub *UserSubscription) {
	t.Helper()
	// 使用重置流程已有的时间参数推进周期，避免依赖等待或真实午夜。
	now := GetDBTimestamp() + 86400
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(sub, sub.Id).Error; err != nil {
			return err
		}
		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}
		return checkTierLimits(tx, sub.Id, plan.GetQuotaTiers(), 0, now)
	}))
}

func TestSubscriptionSettlementRecordsOverageAndBlocksNewReservations(t *testing.T) {
	for _, multiTier := range []bool{false, true} {
		name := "single"
		if multiTier {
			name = "multiple"
		}
		t.Run(name, func(t *testing.T) {
			_, sub := seedPeriodicBillingSubscription(t, multiTier)
			_, err := PreConsumeUserSubscription("overage", sub.UserId, "test", 0, 60, "default")
			require.NoError(t, err)
			require.Error(t, ReserveUserSubscriptionQuota("overage", 50))
			require.NoError(t, DB.First(sub, sub.Id).Error)
			assert.EqualValues(t, 60, sub.AmountUsed)

			require.NoError(t, SettleUserSubscription("overage", 150))
			require.NoError(t, SettleUserSubscription("overage", 150))
			require.NoError(t, RefundSubscriptionPreConsume("overage"))
			require.NoError(t, DB.First(sub, sub.Id).Error)
			assert.EqualValues(t, 150, sub.AmountUsed)
			var record SubscriptionPreConsumeRecord
			require.NoError(t, DB.Where("request_id = ?", "overage").First(&record).Error)
			assert.EqualValues(t, 150, record.SettledAmount)
			assert.Equal(t, "settled", record.Status)
			if multiTier {
				var usages []UserSubscriptionTierUsage
				require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Find(&usages).Error)
				require.Len(t, usages, 3)
				for _, usage := range usages {
					assert.EqualValues(t, 150, usage.UsageInPeriod+usage.UsageInWindow)
				}
			}
			_, err = PreConsumeUserSubscription("after-overage", sub.UserId, "test", 0, 1, "default")
			require.ErrorContains(t, err, "subscription quota insufficient")
		})
	}
}

func TestSubscriptionOldPeriodAdjustmentsPreserveNewPeriodUsage(t *testing.T) {
	for _, multiTier := range []bool{false, true} {
		for _, operation := range []string{"refund", "settle-more", "settle-less"} {
			name := "single/" + operation
			if multiTier {
				name = "multiple/" + operation
			}
			t.Run(name, func(t *testing.T) {
				plan, sub := seedPeriodicBillingSubscription(t, multiTier)
				_, err := PreConsumeUserSubscription("old", sub.UserId, "test", 0, 60, "default")
				require.NoError(t, err)
				advanceBillingSubscriptionPeriod(t, plan, sub)
				_, err = PreConsumeUserSubscription("new", sub.UserId, "test", 0, 40, "default")
				require.NoError(t, err)
				require.Error(t, ReserveUserSubscriptionQuota("old", 10))
				var actual int64
				switch operation {
				case "refund":
					require.NoError(t, RefundSubscriptionPreConsume("old"))
					require.NoError(t, RefundSubscriptionPreConsume("old"))
				case "settle-more":
					actual = 80
					require.NoError(t, SettleUserSubscription("old", actual))
				case "settle-less":
					actual = 20
					require.NoError(t, SettleUserSubscription("old", actual))
				}
				require.NoError(t, DB.First(sub, sub.Id).Error)
				assert.EqualValues(t, 40, sub.AmountUsed)
				var record SubscriptionPreConsumeRecord
				require.NoError(t, DB.Where("request_id = ?", "old").First(&record).Error)
				assert.Equal(t, actual, record.SettledAmount)
				if multiTier {
					var usages []UserSubscriptionTierUsage
					require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index").Find(&usages).Error)
					require.Len(t, usages, 3)
					assert.EqualValues(t, 40, usages[0].UsageInPeriod)
					assert.EqualValues(t, 40, usages[1].UsageInWindow)
					assert.EqualValues(t, 40+actual, usages[2].UsageInWindow)
				}
			})
		}
	}
}

func TestSubscriptionExtraReservationIsRefundedInItsOriginalPeriod(t *testing.T) {
	_, sub := seedPeriodicBillingSubscription(t, true)
	_, err := PreConsumeUserSubscription("reserved", sub.UserId, "test", 0, 30, "default")
	require.NoError(t, err)
	require.NoError(t, ReserveUserSubscriptionQuota("reserved", 50))
	require.NoError(t, ReserveUserSubscriptionQuota("reserved", -20))
	require.NoError(t, RefundSubscriptionPreConsume("reserved"))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	assert.Zero(t, sub.AmountUsed)
	var usages []UserSubscriptionTierUsage
	require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Find(&usages).Error)
	require.Len(t, usages, 3)
	for _, usage := range usages {
		assert.Zero(t, usage.UsageInPeriod+usage.UsageInWindow)
	}
}

func TestSubscriptionManualResetSeparatesInFlightRequestsWithoutChangingSchedule(t *testing.T) {
	for _, resetPeriod := range []string{SubscriptionResetCustom, SubscriptionResetNever} {
		for _, advance := range []bool{false, true} {
			name := resetPeriod + "/preserve-schedule"
			if advance {
				name = resetPeriod + "/advance-schedule"
			}
			t.Run(name, func(t *testing.T) {
				plan, sub := seedPeriodicBillingSubscription(t, false)
				if resetPeriod == SubscriptionResetNever {
					require.NoError(t, DB.Model(plan).Update("quota_reset_period", resetPeriod).Error)
					InvalidateSubscriptionPlanCache(plan.Id)
					require.NoError(t, DB.Model(sub).Updates(map[string]interface{}{"last_reset_time": 0, "next_reset_time": 0}).Error)
				}
				require.NoError(t, DB.First(sub, sub.Id).Error)
				lastReset, nextReset := sub.LastResetTime, sub.NextResetTime
				_, err := PreConsumeUserSubscription("before-first-reset", sub.UserId, "test", 0, 60, "default")
				require.NoError(t, err)
				_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, advance)
				require.NoError(t, err)
				_, err = PreConsumeUserSubscription("before-second-reset", sub.UserId, "test", 0, 50, "default")
				require.NoError(t, err)
				_, err = AdminResetUserSubscriptionsByPlan(sub.UserId, plan.Id, advance)
				require.NoError(t, err)
				_, err = PreConsumeUserSubscription("after-resets", sub.UserId, "test", 0, 40, "default")
				require.NoError(t, err)
				require.NoError(t, RefundSubscriptionPreConsume("before-first-reset"))
				require.NoError(t, SettleUserSubscription("before-second-reset", 20))
				require.NoError(t, DB.First(sub, sub.Id).Error)
				assert.EqualValues(t, 40, sub.AmountUsed)
				if !advance {
					assert.Equal(t, lastReset, sub.LastResetTime)
					assert.Equal(t, nextReset, sub.NextResetTime)
				}
			})
		}
	}
}

func TestLegacySubscriptionRefundDoesNotBorrowCurrentPeriod(t *testing.T) {
	_, sub := seedPeriodicBillingSubscription(t, true)
	_, err := PreConsumeUserSubscription("legacy", sub.UserId, "test", 0, 60, "default")
	require.NoError(t, err)
	// 模拟升级前记录：同秒周期归属不明确时应保守跳过，永不抵扣其他请求。
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", "legacy").Updates(map[string]interface{}{
		"quota_snapshot": "", "created_at": sub.LastResetTime - 1,
	}).Error)
	require.NoError(t, RefundSubscriptionPreConsume("legacy"))
	require.NoError(t, DB.First(sub, sub.Id).Error)
	assert.EqualValues(t, 60, sub.AmountUsed)
	var usages []UserSubscriptionTierUsage
	require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index").Find(&usages).Error)
	require.Len(t, usages, 3)
	assert.EqualValues(t, 60, usages[1].UsageInWindow)
	assert.Zero(t, usages[2].UsageInWindow, "永不重置的累计档位可以正常退款")
}

func TestSubscriptionBillingRejectsOutOfRangeAmounts(t *testing.T) {
	_, sub := seedPeriodicBillingSubscription(t, true)
	_, err := PreConsumeUserSubscription("oversized", sub.UserId, "test", 0, common.MaxQuota, "default")
	require.ErrorIs(t, err, ErrQuotaOutOfRange)
	_, err = PreConsumeUserSubscription("bounded", sub.UserId, "test", 0, 60, "default")
	require.NoError(t, err)
	require.ErrorIs(t, ReserveUserSubscriptionQuota("bounded", math.MaxInt64), ErrQuotaOutOfRange)
	require.ErrorIs(t, SettleUserSubscription("bounded", math.MaxInt64), ErrQuotaOutOfRange)
	require.ErrorIs(t, PostConsumeUserSubscriptionDelta(sub.Id, math.MinInt64, "", time.Now().Unix()), ErrQuotaOutOfRange)
	require.NoError(t, DB.First(sub, sub.Id).Error)
	assert.EqualValues(t, 60, sub.AmountUsed)
}

func TestSubscriptionSettlementCounterOverflowRollsBack(t *testing.T) {
	for _, counter := range []string{"total", "tier"} {
		t.Run(counter, func(t *testing.T) {
			_, sub := seedPeriodicBillingSubscription(t, true)
			_, err := PreConsumeUserSubscription("counter-boundary", sub.UserId, "test", 0, 60, "default")
			require.NoError(t, err)
			used := int64(common.MaxQuota - 20)
			if counter == "total" {
				require.NoError(t, DB.Model(sub).Update("amount_used", used).Error)
			} else {
				require.NoError(t, DB.Model(&UserSubscriptionTierUsage{}).Where("user_subscription_id = ? AND tier_index = ?", sub.Id, 0).Update("usage_in_period", used).Error)
			}
			require.ErrorIs(t, SettleUserSubscription("counter-boundary", 90), ErrQuotaOutOfRange)
			require.NoError(t, DB.First(sub, sub.Id).Error)
			if counter == "total" {
				assert.Equal(t, used, sub.AmountUsed)
			} else {
				assert.EqualValues(t, 60, sub.AmountUsed, "档位失败必须回滚同笔总额调整")
			}
			var record SubscriptionPreConsumeRecord
			require.NoError(t, DB.Where("request_id = ?", "counter-boundary").First(&record).Error)
			assert.Equal(t, "consumed", record.Status)
			assert.Zero(t, record.SettledAmount)
		})
	}
}

func TestSubscriptionTaskBillingKeepsOriginalPeriodsAfterRecordCleanup(t *testing.T) {
	for _, operation := range []string{"refund", "settle"} {
		t.Run(operation, func(t *testing.T) {
			plan, sub := seedPeriodicBillingSubscription(t, true)
			require.NoError(t, DB.Create(&User{Id: sub.UserId, Username: "period-task", Quota: 1000, UsedQuota: 60}).Error)
			token := &Token{UserId: sub.UserId, Key: "period-task", RemainQuota: 940, UsedQuota: 60}
			require.NoError(t, DB.Create(token).Error)
			reserved, err := PreConsumeUserSubscription("task-period", sub.UserId, "test", 0, 60, "default")
			require.NoError(t, err)
			require.NoError(t, SettleUserSubscription("task-period", 60))
			task := InitTask("test", &relaycommon.RelayInfo{
				UserId: sub.UserId, ChannelMeta: &relaycommon.ChannelMeta{},
				SubscriptionQuotaSnapshot: reserved.QuotaSnapshot,
			})
			task.Quota = 60
			task.PrivateData.BillingSource = "subscription"
			task.PrivateData.SubscriptionId = sub.Id
			task.PrivateData.TokenId = token.Id
			require.NoError(t, DB.Create(task).Error)
			// 长任务必须依靠自身快照结算，不依赖已过清理期限的预扣记录。
			require.NoError(t, DB.Where("request_id = ?", "task-period").Delete(&SubscriptionPreConsumeRecord{}).Error)
			advanceBillingSubscriptionPeriod(t, plan, sub)
			_, err = PreConsumeUserSubscription("new-task-period", sub.UserId, "test", 0, 40, "default")
			require.NoError(t, err)
			actual := 80
			if operation == "refund" {
				actual = 0
				_, refunded, err := RefundTaskBilling(task.ID)
				require.NoError(t, err)
				assert.Equal(t, 60, refunded)
			} else {
				_, delta, err := SettleTaskBilling(task.ID, actual)
				require.NoError(t, err)
				assert.Equal(t, 20, delta)
			}
			require.NoError(t, DB.First(sub, sub.Id).Error)
			assert.EqualValues(t, 40, sub.AmountUsed)
			var usages []UserSubscriptionTierUsage
			require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("tier_index").Find(&usages).Error)
			require.Len(t, usages, 3)
			assert.EqualValues(t, 40, usages[0].UsageInPeriod)
			assert.EqualValues(t, 40, usages[1].UsageInWindow)
			assert.EqualValues(t, 40+actual, usages[2].UsageInWindow)
			require.NoError(t, DB.First(token, token.Id).Error)
			assert.Equal(t, 1000-actual, token.RemainQuota)
			var user User
			require.NoError(t, DB.First(&user, sub.UserId).Error)
			assert.Equal(t, actual, user.UsedQuota)
			assert.Equal(t, 1000, user.Quota)
			require.NoError(t, DB.First(task, task.ID).Error)
			assert.Equal(t, actual, task.Quota)
		})
	}
}
