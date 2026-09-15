package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// refundTaskBalancesTx 与任务退款标记共用事务，失败时资金和统计一起回滚。
func refundTaskBalancesTx(tx *gorm.DB, userID, channelID, tokenID, subscriptionID, quota int) (string, error) {
	if quota <= 0 || quota >= common.MaxQuota {
		return "", ErrQuotaOutOfRange
	}
	if subscriptionID > 0 {
		if err := postConsumeUserSubscriptionDeltaTx(tx, subscriptionID, -int64(quota), true); err != nil {
			return "", err
		}
	} else {
		if err := creditTopUpQuota(tx, userID, quota, nil); err != nil {
			return "", err
		}
	}
	tokenKey := ""
	if tokenID > 0 {
		var token Token
		err := lockForUpdate(tx).Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		// 删除的令牌无须复活；现存令牌的退款失败必须回滚整笔。
		if err == nil {
			tokenKey = token.Key
			result := tx.Model(&Token{}).Where("id = ? AND remain_quota <= ? AND used_quota >= ?", tokenID, common.MaxQuota-1-quota, common.MinQuota+1+quota).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota + ?", quota), "used_quota": gorm.Expr("used_quota - ?", quota), "accessed_time": common.GetTimestamp()})
			if result.Error != nil {
				return "", result.Error
			}
			if result.RowsAffected != 1 {
				return "", ErrQuotaOutOfRange
			}
		}
	}
	if err := tx.Model(&User{}).Where("id = ?", userID).Update("used_quota", gorm.Expr("used_quota - ?", quota)).Error; err != nil {
		return "", err
	}
	if channelID > 0 {
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Update("used_quota", gorm.Expr("used_quota - ?", quota)).Error; err != nil {
			return "", err
		}
	}
	return tokenKey, nil
}

// RefundTaskBilling 以持久化额度为幂等依据，允许退款失败后安全重试。
func RefundTaskBilling(taskID int64) (*Task, int, error) {
	var task Task
	quota := 0
	tokenKey := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Quota == 0 {
			return tx.Model(&task).Update("refund_pending", false).Error
		}
		quota = task.Quota
		subscriptionID := 0
		if task.PrivateData.BillingSource == "subscription" {
			subscriptionID = task.PrivateData.SubscriptionId
		}
		var err error
		tokenKey, err = refundTaskBalancesTx(tx, task.UserId, task.ChannelId, task.PrivateData.TokenId, subscriptionID, quota)
		if err != nil {
			return err
		}
		result := tx.Model(&Task{}).Where("id = ? AND quota = ?", task.ID, quota).Updates(map[string]interface{}{"quota": 0, "refund_pending": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("task refund state changed")
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if quota > 0 {
		syncCreditUserQuotaCache(task.UserId, quota, "task refund")
		if err := cacheDeleteToken(tokenKey); err != nil {
			common.SysError(err.Error())
		}
	}
	return &task, quota, nil
}

func RefundMidjourneyBilling(taskID int) (*Midjourney, int, error) {
	var task Midjourney
	quota := 0
	tokenKey := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Quota == 0 {
			return tx.Model(&task).Update("refund_pending", false).Error
		}
		quota = task.Quota
		channelID := task.BillingChannelId
		if channelID == 0 {
			channelID = task.ChannelId
		}
		var err error
		tokenKey, err = refundTaskBalancesTx(tx, task.UserId, channelID, task.TokenId, 0, quota)
		if err != nil {
			return err
		}
		result := tx.Model(&Midjourney{}).Where("id = ? AND quota = ?", task.Id, quota).Updates(map[string]interface{}{"quota": 0, "refund_pending": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("Midjourney refund state changed")
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if quota > 0 {
		syncCreditUserQuotaCache(task.UserId, quota, "Midjourney refund")
		if err := cacheDeleteToken(tokenKey); err != nil {
			common.SysError(err.Error())
		}
	}
	return &task, quota, nil
}

// SettleTaskBilling 将任务差额与最终 quota 一起提交，旧失败快照不能重新扣费。
func SettleTaskBilling(taskID int64, actualQuota int) (*Task, int, error) {
	if taskID <= 0 || actualQuota <= 0 || actualQuota >= common.MaxQuota {
		return nil, 0, ErrQuotaOutOfRange
	}
	var task Task
	delta := 0
	tokenKey := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Status == TaskStatusFailure || task.RefundPending {
			return errors.New("cannot settle a failed task")
		}
		delta = actualQuota - task.Quota
		if delta == 0 {
			return nil
		}
		subscriptionID := 0
		if task.PrivateData.BillingSource == "subscription" {
			subscriptionID = task.PrivateData.SubscriptionId
		}
		if delta < 0 {
			var err error
			tokenKey, err = refundTaskBalancesTx(tx, task.UserId, task.ChannelId, task.PrivateData.TokenId, subscriptionID, -delta)
			if err != nil {
				return err
			}
		} else {
			if subscriptionID > 0 {
				if err := postConsumeUserSubscriptionDeltaTx(tx, subscriptionID, int64(delta), true); err != nil {
					return err
				}
			} else {
				result := tx.Model(&User{}).Where("id = ? AND quota >= ?", task.UserId, common.MinQuota+1+delta).Update("quota", gorm.Expr("quota - ?", delta))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrQuotaOutOfRange
				}
			}
			if task.PrivateData.TokenId > 0 {
				var token Token
				err := lockForUpdate(tx).Where("id = ? AND user_id = ?", task.PrivateData.TokenId, task.UserId).First(&token).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil {
					tokenKey = token.Key
					result := tx.Model(&Token{}).Where("id = ? AND remain_quota >= ? AND used_quota <= ?", token.Id, common.MinQuota+1+delta, common.MaxQuota-1-delta).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota - ?", delta), "used_quota": gorm.Expr("used_quota + ?", delta), "accessed_time": common.GetTimestamp()})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected != 1 {
						return ErrQuotaOutOfRange
					}
				}
			}
			if err := tx.Model(&User{}).Where("id = ?", task.UserId).Update("used_quota", gorm.Expr("used_quota + ?", delta)).Error; err != nil {
				return err
			}
			if task.ChannelId > 0 {
				if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).Update("used_quota", gorm.Expr("used_quota + ?", delta)).Error; err != nil {
					return err
				}
			}
		}
		result := tx.Model(&Task{}).Where("id = ? AND quota = ? AND status <> ?", task.ID, task.Quota, TaskStatusFailure).Update("quota", actualQuota)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task settlement state changed")
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if delta != 0 {
		syncCreditUserQuotaCache(task.UserId, -delta, "task settlement")
		if err := cacheDeleteToken(tokenKey); err != nil {
			common.SysError(err.Error())
		}
	}
	return &task, delta, nil
}
