package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// TwoFA 用户2FA设置表
type TwoFA struct {
	Id             int        `json:"id" gorm:"primaryKey"`
	UserId         int        `json:"user_id" gorm:"unique;not null;index"`
	Secret         string     `json:"-" gorm:"type:varchar(255);not null"` // TOTP密钥，不返回给前端
	IsEnabled      bool       `json:"is_enabled"`
	FailedAttempts int        `json:"failed_attempts" gorm:"default:0"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	// LastUsedStep 记录最近一次验证成功的 TOTP 时间步。同一时间步的验证码只接受一次（RFC 6238 §5.2），
	// 并发提交同一验证码时只有一个请求能命中条件更新。
	LastUsedStep int64          `json:"-" gorm:"not null;default:0"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

// TwoFABackupCode 备用码使用记录表
type TwoFABackupCode struct {
	Id        int            `json:"id" gorm:"primaryKey"`
	UserId    int            `json:"user_id" gorm:"not null;index"`
	CodeHash  string         `json:"-" gorm:"type:varchar(255);not null"` // 备用码哈希
	IsUsed    bool           `json:"is_used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// IsLocked 检查账户是否被锁定
func (t *TwoFA) IsLocked() bool {
	if t.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*t.LockedUntil)
}

// VerifyTwoFactorCode 用 TOTP 验证码或备用码验证用户的第二因素。
// 校验与使用记录在同一事务内完成：因子行在事务中加锁读取，成功的条件更新要求密钥未被更换、
// 因子仍启用且未锁定、该 TOTP 时间步尚未使用过；条件不命中或写入失败一律视为验证失败，
// 不再出现"验证通过但使用记录静默丢失"的情况。备用码在同一事务内一次性核销。
func VerifyTwoFactorCode(userID int, code string, allowBackupCode bool) (bool, error) {
	now := time.Now()
	verified := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var factor TwoFA
		if err := lockForUpdate(tx).Where("user_id = ? AND is_enabled = ?", userID, true).First(&factor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTwoFANotEnabled
			}
			return err
		}
		if factor.LockedUntil != nil && now.Before(*factor.LockedUntil) {
			return fmt.Errorf("账户已被锁定，请在%v后重试", factor.LockedUntil.Format("2006-01-02 15:04:05"))
		}
		success := tx.Model(&TwoFA{}).Where("id = ? AND secret = ? AND is_enabled = ? AND (locked_until IS NULL OR locked_until <= ?)", factor.Id, factor.Secret, true, now)
		updates := map[string]any{"failed_attempts": 0, "locked_until": nil, "last_used_at": now}
		if cleanCode, err := common.ValidateNumericCode(code); err == nil {
			step, matched := common.MatchTOTPStep(factor.Secret, cleanCode, now)
			if !matched {
				return recordTwoFactorFailure(tx, factor, now)
			}
			updates["last_used_step"] = step
			success = success.Where("COALESCE(last_used_step, 0) < ?", step)
		} else if allowBackupCode && common.ValidateBackupCode(code) {
			normalized := common.NormalizeBackupCode(code)
			var codes []TwoFABackupCode
			if err := tx.Where("user_id = ? AND is_used = ?", userID, false).Find(&codes).Error; err != nil {
				return err
			}
			consumed := false
			for _, backup := range codes {
				if !common.ValidatePasswordAndHash(normalized, backup.CodeHash) {
					continue
				}
				result := tx.Model(&TwoFABackupCode{}).Where("id = ? AND is_used = ?", backup.Id, false).Updates(map[string]any{"is_used": true, "used_at": now})
				if result.Error != nil {
					return result.Error
				}
				consumed = result.RowsAffected == 1
				break
			}
			if !consumed {
				return recordTwoFactorFailure(tx, factor, now)
			}
		} else {
			// 既不是验证码也不是备用码格式，不计入失败次数。
			return nil
		}
		result := success.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		verified = result.RowsAffected == 1
		return nil
	})
	return verified, err
}

// recordTwoFactorFailure 在持有因子行锁的事务内累计失败次数，达到上限后锁定。
func recordTwoFactorFailure(tx *gorm.DB, factor TwoFA, now time.Time) error {
	updates := map[string]any{"failed_attempts": factor.FailedAttempts + 1}
	if factor.FailedAttempts+1 >= common.MaxFailAttempts {
		updates["locked_until"] = now.Add(time.Duration(common.LockoutDuration) * time.Second)
	}
	return tx.Model(&TwoFA{}).Where("id = ?", factor.Id).Updates(updates).Error
}

// GetUnusedBackupCodeCount 获取未使用的备用码数量
func GetUnusedBackupCodeCount(userId int) (int, error) {
	var count int64
	err := ReadDB().Model(&TwoFABackupCode{}).Where("user_id = ? AND is_used = false", userId).Count(&count).Error
	return int(count), err
}

// GetTwoFAStats 获取2FA统计信息（管理员使用）
func GetTwoFAStats() (map[string]interface{}, error) {
	var totalUsers, enabledUsers int64

	// 总用户数
	if err := ReadDB().Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}

	// 启用2FA的用户数
	if err := ReadDB().Model(&TwoFA{}).Where("is_enabled = true").Count(&enabledUsers).Error; err != nil {
		return nil, err
	}

	enabledRate := float64(0)
	if totalUsers > 0 {
		enabledRate = float64(enabledUsers) / float64(totalUsers) * 100
	}

	return map[string]interface{}{
		"total_users":   totalUsers,
		"enabled_users": enabledUsers,
		"enabled_rate":  fmt.Sprintf("%.1f%%", enabledRate),
	}, nil
}
