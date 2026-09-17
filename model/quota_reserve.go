package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrQuotaOutOfRange = errors.New("quota exceeds the supported wallet range")

// ValidateWalletQuota 校验一个将要写入 quota 列的绝对值。增量写入由
// applyUserQuotaDelta / creditTopUpQuota 的 CAS 条件兜底，但管理员覆盖、
// 兑换码面额、新用户赠送这类直接赋值的入口不经过那条路径，需共用同一边界。
func ValidateWalletQuota(quota int) error {
	if quota < 0 || quota >= common.MaxQuota {
		return ErrQuotaOutOfRange
	}
	return nil
}

// 余额是账务状态，始终读取主库；缓存和批量统计不得决定可消费额度。
func getUserQuotaForRead(id int, _ bool) (int, error) {
	var user User
	err := DB.Select("quota").Where("id = ?", id).First(&user).Error
	return user.Quota, err
}

// TryReserveUserQuota 在同一条主库 UPDATE 中检查并扣减初始预扣额度。
func TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 || quota >= common.MaxQuota {
		return false, ErrQuotaOutOfRange
	}
	if quota == 0 {
		return true, nil
	}
	result := DB.Model(&User{}).Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error == nil && result.RowsAffected == 1 {
		syncCreditUserQuotaCache(id, -quota, "reserve")
	}
	return result.RowsAffected == 1, result.Error
}

// applyUserQuotaDelta 也供退款和结算使用。结算可形成欠费，但不能越过数据库边界。
// BatchUpdate 仅保留用量统计；余额写入必须同步，避免 Redis 失效后复用未落库额度。
func applyUserQuotaDelta(id int, delta int, _ bool) error {
	if delta <= common.MinQuota || delta >= common.MaxQuota {
		return ErrQuotaOutOfRange
	}
	if delta == 0 {
		return nil
	}
	query := DB.Model(&User{}).Where("id = ?", id)
	if delta > 0 {
		query = query.Where("quota <= ?", common.MaxQuota-1-delta)
	} else {
		query = query.Where("quota >= ?", common.MinQuota+1-delta)
	}
	result := query.Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrQuotaOutOfRange
	}
	syncCreditUserQuotaCache(id, delta, "wallet adjustment")
	return nil
}

func TryReserveTokenQuota(id int, userID int, key string, quota int) (bool, error) {
	if quota < 0 || quota >= common.MaxQuota {
		return false, ErrQuotaOutOfRange
	}
	if quota == 0 {
		return true, nil
	}
	query := DB.Model(&Token{}).Where("id = ? AND user_id = ? AND status = ?", id, userID, common.TokenStatusEnabled).Where(map[string]interface{}{"key": key}).
		Where("expired_time < 0 OR expired_time >= ?", common.GetTimestamp()).
		Where("unlimited_quota = ? OR remain_quota >= ?", true, quota)
	// 无限令牌仍记录剩余/已用账目，只跳过可用余额检查。
	query = query.Where("remain_quota >= ? AND used_quota <= ?", common.MinQuota+1+quota, common.MaxQuota-1-quota)
	result := query.Updates(map[string]interface{}{
		"remain_quota":  gorm.Expr("remain_quota - ?", quota),
		"used_quota":    gorm.Expr("used_quota + ?", quota),
		"accessed_time": common.GetTimestamp(),
	})
	if result.Error == nil && result.RowsAffected == 1 {
		_ = cacheDeleteToken(key)
	}
	return result.RowsAffected == 1, result.Error
}

func applyTokenQuotaDelta(id int, key string, delta int) error {
	if delta <= common.MinQuota || delta >= common.MaxQuota {
		return ErrQuotaOutOfRange
	}
	if delta == 0 {
		return nil
	}
	query := DB.Model(&Token{}).Where("id = ?", id)
	if delta > 0 {
		query = query.Where("remain_quota <= ? AND used_quota >= ?", common.MaxQuota-1-delta, common.MinQuota+1+delta)
	} else {
		query = query.Where("remain_quota >= ? AND used_quota <= ?", common.MinQuota+1-delta, common.MaxQuota-1+delta)
	}
	result := query.Updates(map[string]interface{}{
		"remain_quota":  gorm.Expr("remain_quota + ?", delta),
		"used_quota":    gorm.Expr("used_quota - ?", delta),
		"accessed_time": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrQuotaOutOfRange
	}
	if err := cacheDeleteToken(key); err != nil {
		common.SysError("failed to invalidate token quota cache: " + err.Error())
	}
	return nil
}

// UpdateUserUsedQuota 结算与退款只调整已用额，不重复累计请求次数。
func UpdateUserUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		return
	}
	if err := DB.Model(&User{}).Where("id = ?", id).
		Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
		common.SysError("failed to update user used quota: " + err.Error())
	}
}

// ValidateSubscriptionFundingGroup 防止重试换组后继续使用原组专属订阅。
func ValidateSubscriptionFundingGroup(subscriptionID, userID int, usingGroup string) error {
	var sub UserSubscription
	if err := DB.Where("id = ? AND user_id = ?", subscriptionID, userID).First(&sub).Error; err != nil {
		return err
	}
	if sub.Status != SubscriptionStatusActive || sub.EndTime <= GetDBTimestamp() || !subscriptionMatchesUsingGroup(sub.UpgradeGroup, usingGroup) {
		return errors.New("subscription does not apply to the selected group")
	}
	return nil
}
