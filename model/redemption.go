package model

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

type Redemption struct {
	Id           int            `json:"id"`
	UserId       int            `json:"user_id"`
	Key          string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status       int            `json:"status" gorm:"default:1"`
	Name         string         `json:"name" gorm:"index"`
	Quota        int            `json:"quota" gorm:"default:100"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime int64          `json:"redeemed_time" gorm:"bigint"`
	Count        int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId   int            `json:"used_user_id"`       // 最后一次核销的用户
	MaxUses      int            `json:"max_uses"`           // 可核销次数,0/1 均为单次(兼容旧数据)
	UsedCount    int            `json:"used_count"`         // 已核销次数
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	ExpiredTime  int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

// MaxCodeUses 单个兑换码/邀请码可设置的最大核销次数。
const MaxCodeUses = 1000

// resolveMaxUses 归一化可核销次数:旧数据与未设置(0)按单次处理。
func resolveMaxUses(maxUses int) int {
	if maxUses < 1 {
		return 1
	}
	return maxUses
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取总数
	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, status string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&Redemption{})

	if keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
		} else {
			query = query.Where("name LIKE ?", keyword+"%")
		}
	}

	if status != "" {
		now := common.GetTimestamp()
		switch status {
		case "expired":
			query = query.Where(
				"status = ? AND expired_time != 0 AND expired_time < ?",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusEnabled):
			query = query.Where(
				"status = ? AND (expired_time = 0 OR expired_time >= ?)",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusDisabled):
			query = query.Where("status = ?", common.RedemptionCodeStatusDisabled)
		case strconv.Itoa(common.RedemptionCodeStatusUsed):
			query = query.Where("status = ?", common.RedemptionCodeStatusUsed)
		}
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = ReadDB().First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(source LogSource, key string, userId int) (quota int, err error) {
	if key == "" {
		return 0, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return 0, errors.New("无效的 user id")
	}
	redemption := &Redemption{}
	logContent := ""

	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			// 如果不是兑换码，尝试作为邀请码兑换
			invitationCode := &InvitationCode{}
			err = lockForUpdate(tx).Where("code = ?", key).First(invitationCode).Error
			if err != nil {
				return errors.New("无效的兑换码或邀请码")
			}
			if invitationCode.Status != common.InvitationCodeStatusEnabled {
				return errors.New("该邀请码已被使用或已禁用")
			}
			// 不能使用自己生成的邀请码
			if invitationCode.UserId == userId {
				return errors.New("不能使用自己生成的邀请码")
			}
			maxUses := resolveMaxUses(invitationCode.MaxUses)
			// 多用码限制每用户核销一次,防止单个用户独占全部次数刷奖励
			if maxUses > 1 {
				used, err := hasCodeUseInTx(tx, CodeUseTypeInvitation, invitationCode.Id, userId)
				if err != nil {
					return err
				}
				if used {
					return errors.New("已使用过该邀请码")
				}
			}
			// CAS 递增核销次数:只有把 used_count 推进一格(且未超上限)的事务
			// 才能发放奖励,SQLite 等无行锁场景下并发兑换同样不会超发。
			result := tx.Model(&InvitationCode{}).
				Where("id = ? AND status = ? AND used_count < ?", invitationCode.Id, common.InvitationCodeStatusEnabled, maxUses).
				Updates(map[string]interface{}{
					"used_count":   gorm.Expr("used_count + 1"),
					"used_user_id": userId,
					"used_type":    common.InvitationCodeUsedTypeRedeem,
					"used_time":    common.GetTimestamp(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return errors.New("该邀请码已被使用或已禁用")
			}
			// 核销次数用满后关闭该码(幂等)
			if err := tx.Model(&InvitationCode{}).
				Where("id = ? AND status = ? AND used_count >= ?", invitationCode.Id, common.InvitationCodeStatusEnabled, maxUses).
				Update("status", common.InvitationCodeStatusUsed).Error; err != nil {
				return err
			}
			if maxUses > 1 {
				if err := insertCodeUseInTx(tx, CodeUseTypeInvitation, invitationCode.Id, userId); err != nil {
					return errors.New("已使用过该邀请码")
				}
			}
			// 按比例给使用者增加余额;Quota 为单次奖励基数,不随次数变化
			rewardQuota := invitationCode.Quota
			if common.InvitationCodeRewardRatio > 0 {
				rewardQuota = invitationCode.Quota * common.InvitationCodeRewardRatio / 100
			}
			if rewardQuota > 0 {
				err = tx.Model(&User{}).Where("id = ?", userId).
					Update("quota", gorm.Expr("quota + ?", rewardQuota)).Error
				if err != nil {
					return err
				}
			}
			quota = rewardQuota
			logContent = fmt.Sprintf("使用邀请码获得 %s，邀请码ID %d", logger.LogQuota(rewardQuota), invitationCode.Id)
			return nil
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("该兑换码已被使用")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("该兑换码已过期")
		}
		maxUses := resolveMaxUses(redemption.MaxUses)
		// 多用码限制每用户核销一次,防止单个用户独占全部次数
		if maxUses > 1 {
			used, err := hasCodeUseInTx(tx, CodeUseTypeRedemption, redemption.Id, userId)
			if err != nil {
				return err
			}
			if used {
				return errors.New("已使用过该兑换码")
			}
		}
		// Compare-and-swap on used_count: only the transaction that advances
		// the counter (while below the cap) may credit quota, so concurrent
		// redeems never over-issue even without a row lock (e.g. on SQLite).
		result := tx.Model(&Redemption{}).
			Where("id = ? AND status = ? AND used_count < ?", redemption.Id, common.RedemptionCodeStatusEnabled, maxUses).
			Updates(map[string]interface{}{
				"used_count":    gorm.Expr("used_count + 1"),
				"redeemed_time": common.GetTimestamp(),
				"used_user_id":  userId,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("该兑换码已被使用")
		}
		// 核销次数用满后关闭该码(幂等)
		if err := tx.Model(&Redemption{}).
			Where("id = ? AND status = ? AND used_count >= ?", redemption.Id, common.RedemptionCodeStatusEnabled, maxUses).
			Update("status", common.RedemptionCodeStatusUsed).Error; err != nil {
			return err
		}
		if maxUses > 1 {
			if err := insertCodeUseInTx(tx, CodeUseTypeRedemption, redemption.Id, userId); err != nil {
				return errors.New("已使用过该兑换码")
			}
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error; err != nil {
			return err
		}
		quota = redemption.Quota
		logContent = fmt.Sprintf("通过兑换码充值 %s，兑换码ID %d", logger.LogQuota(redemption.Quota), redemption.Id)
		return nil
	})
	if err != nil {
		common.SysError("redemption failed: " + err.Error())
		return 0, ErrRedeemFailed
	}
	if logContent != "" {
		RecordLog(source, userId, LogTypeTopup, logContent)
	}
	return quota, nil
}

func (redemption *Redemption) Insert() error {
	var err error
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	// This can update zero values
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	var err error
	err = DB.Model(redemption).Select("name", "status", "quota", "redeemed_time", "expired_time", "max_uses").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
