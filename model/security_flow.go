package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrSecurityIdentity = errors.New("登录状态已失效，请重新登录")
var ErrSecurityProof = errors.New("安全验证已失效，请重新验证")

var securityCleanupAfter atomic.Int64

// SecurityFlow 在数据库核销会话和短时证明，避免签名 Cookie 被重放后恢复验证状态。
type SecurityFlow struct {
	ID          string `gorm:"type:varchar(64);primaryKey"`
	UserID      int    `gorm:"index"`
	Version     int64
	SessionID   string `gorm:"type:varchar(64);index"`
	Kind        string `gorm:"type:varchar(32)"`
	Scope       string `gorm:"type:varchar(255)"`
	ContextHash string `gorm:"type:varchar(64)"`
	Payload     string `gorm:"type:text"`
	ExpiresAt   int64  `gorm:"index"`
	Consumed    bool
}

type SecurityIdentity struct {
	UserID    int
	Version   int64
	SessionID string
}

func SecurityTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func CreateSecurityFlow(flow SecurityFlow) (string, error) {
	if flow.Kind == "" || flow.ExpiresAt <= time.Now().Unix() {
		return "", ErrSecurityProof
	}
	token, err := common.GenerateRandomCharsKey(64)
	if err != nil {
		return "", err
	}
	flow.ID = SecurityTokenHash(token)
	if err := DB.Create(&flow).Error; err != nil {
		return "", err
	}
	// 每个进程至多每分钟清理 256 条，避免长期运行累积失效证明或长事务扫描。
	now := time.Now().Unix()
	previous := securityCleanupAfter.Load()
	if now >= previous && securityCleanupAfter.CompareAndSwap(previous, now+60) {
		var expired []string
		if err := DB.Model(&SecurityFlow{}).Where("expires_at <= ?", now).Limit(256).Pluck("id", &expired).Error; err == nil && len(expired) > 0 {
			if err := DB.Where("id IN ?", expired).Delete(&SecurityFlow{}).Error; err != nil {
				common.SysError("failed to clean expired security flows: " + err.Error())
			}
		}
	}
	return token, nil
}

func GetSecurityFlow(token, kind string) (*SecurityFlow, error) {
	if len(token) != 64 {
		return nil, ErrSecurityProof
	}
	var flow SecurityFlow
	err := DB.Where("id = ? AND kind = ? AND consumed = ? AND expires_at > ?", SecurityTokenHash(token), kind, false, time.Now().Unix()).First(&flow).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSecurityProof
	}
	return &flow, err
}

func ConsumeSecurityFlow(token, kind string, identity SecurityIdentity, scope, contextHash string) (*SecurityFlow, error) {
	flow, err := GetSecurityFlow(token, kind)
	if err != nil {
		return nil, err
	}
	if flow.UserID != identity.UserID || flow.Version != identity.Version || flow.SessionID != identity.SessionID || flow.Scope != scope || flow.ContextHash != contextHash {
		return nil, ErrSecurityProof
	}
	result := DB.Model(&SecurityFlow{}).Where("id = ? AND consumed = ? AND expires_at > ?", flow.ID, false, time.Now().Unix()).Update("consumed", true)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrSecurityProof
	}
	return flow, nil
}

// ReadSecurityIdentity 总是读主库，封禁/降权不能等待只读副本或异步缓存刷新。
func ReadSecurityIdentity(identity SecurityIdentity) (*User, error) {
	var user User
	if identity.UserID <= 0 || len(identity.SessionID) != 64 {
		return nil, ErrSecurityIdentity
	}
	if err := DB.Omit("password", "access_token").First(&user, identity.UserID).Error; err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion != identity.Version {
		return nil, ErrSecurityIdentity
	}
	var count int64
	err := DB.Model(&SecurityFlow{}).Where("id = ? AND user_id = ? AND version = ? AND kind = ? AND consumed = ? AND expires_at > ?", identity.SessionID, identity.UserID, identity.Version, "session", false, time.Now().Unix()).Count(&count).Error
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrSecurityIdentity
	}
	return &user, nil
}

// MutateAccountSecurity 将当前身份复核与凭据更新放进同一个事务。
func MutateAccountSecurity(identity SecurityIdentity, mutate func(*gorm.DB, *User) error) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, identity.UserID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled || user.AuthVersion != identity.Version {
			return ErrSecurityIdentity
		}
		var count int64
		if err := tx.Model(&SecurityFlow{}).Where("id = ? AND user_id = ? AND version = ? AND kind = ? AND consumed = ? AND expires_at > ?", identity.SessionID, identity.UserID, identity.Version, "session", false, time.Now().Unix()).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return ErrSecurityIdentity
		}
		return mutate(tx, &user)
	})
}

func RevokeSecuritySession(sessionID string) error {
	return DB.Model(&SecurityFlow{}).Where("id = ? AND kind = ?", sessionID, "session").Update("consumed", true).Error
}

// RotateSecurityIdentity 撤销旧登录与证明，仅续接完成本次安全操作的会话。
func RotateSecurityIdentity(identity SecurityIdentity) (int64, error) {
	return CommitAccountSecurity(identity, func(_ *gorm.DB, _ *User) error { return nil })
}

func CommitAccountSecurity(identity SecurityIdentity, mutation func(*gorm.DB, *User) error) (int64, error) {
	if identity.Version < 0 || identity.Version == math.MaxInt64 {
		return 0, ErrSecurityIdentity
	}
	err := MutateAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		if err := mutation(tx, user); err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
			return err
		}
		return tx.Model(&SecurityFlow{}).Where("id = ?", identity.SessionID).Update("version", identity.Version+1).Error
	})
	if err == nil {
		if cacheErr := invalidateUserCache(identity.UserID); cacheErr != nil {
			common.SysError("failed to invalidate security user cache: " + cacheErr.Error())
		}
	}
	return identity.Version + 1, err
}

func ChangeAccountPassword(identity SecurityIdentity, original string, update *User) (int64, error) {
	hash, err := common.Password2Hash(update.Password)
	if err != nil {
		return 0, err
	}
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		if user.Password != "" && !common.ValidatePasswordAndHash(original, user.Password) {
			return errors.New("原密码错误")
		}
		changes := map[string]any{"password": hash}
		if update.Username != "" {
			changes["username"] = update.Username
		}
		if update.DisplayName != "" {
			changes["display_name"] = update.DisplayName
		}
		return tx.Model(&User{}).Where("id = ?", user.Id).Updates(changes).Error
	})
}

func BindAccountEmail(identity SecurityIdentity, email string) (int64, error) {
	email = NormalizeEmail(email)
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		return withNormalizedEmailLock(tx, email, func(tx *gorm.DB) error {
			if err := ensureEmailAvailableWithTx(tx, email, user.Id); err != nil {
				return err
			}
			return tx.Model(&User{}).Where("id = ?", user.Id).Update("email", email).Error
		})
	})
}

func SaveAccountPasskey(identity SecurityIdentity, credential *PasskeyCredential) (int64, error) {
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		if credential.UserID != user.Id {
			return ErrSecurityIdentity
		}
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&PasskeyCredential{}).Error; err != nil {
			return err
		}
		return tx.Create(credential).Error
	})
}

func BindAccountColumn(identity SecurityIdentity, column, value string) (int64, error) {
	switch column {
	case "github_id", "discord_id", "oidc_id", "linux_do_id", "wechat_id", "telegram_id", "steam_openid":
	default:
		return 0, ErrSecurityProof
	}
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		var count int64
		if err := tx.Model(&User{}).Where(column+" = ? AND id <> ?", value, user.Id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("该登录方式已被其他账户绑定")
		}
		return tx.Model(&User{}).Where("id = ?", user.Id).Update(column, value).Error
	})
}

func BindAccountOAuth(identity SecurityIdentity, providerID int, subject string) (int64, error) {
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		var binding UserOAuthBinding
		err := tx.Where("user_id = ? AND provider_id = ?", user.Id, providerID).First(&binding).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&UserOAuthBinding{UserId: user.Id, ProviderId: providerID, ProviderUserId: subject}).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&binding).Update("provider_user_id", subject).Error
	})
}

func UnbindAccountOAuth(identity SecurityIdentity, providerID int) (int64, error) {
	return CommitAccountSecurity(identity, func(tx *gorm.DB, user *User) error {
		var binding UserOAuthBinding
		if err := tx.Where("user_id = ? AND provider_id = ?", user.Id, providerID).First(&binding).Error; err != nil {
			return err
		}
		if binding.IsRegistration {
			return errors.New("该账号由此登录方式注册，无法解除绑定")
		}
		var provider CustomOAuthProvider
		if err := tx.First(&provider, providerID).Error; err != nil {
			return err
		}
		if provider.DisableUnbind {
			return errors.New("管理员已禁止解除该登录方式的绑定")
		}
		return tx.Delete(&binding).Error
	})
}

// RevokeAccountAuthentication 在管理员撤销认证因素时同时失效全部旧会话和证明。
func RevokeAccountAuthentication(userID int, kind string, providerID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		switch kind {
		case "passkey":
			if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&PasskeyCredential{}).Error; err != nil {
				return err
			}
		case "2fa":
			var factorCount int64
			if err := tx.Model(&TwoFA{}).Where("user_id = ?", userID).Count(&factorCount).Error; err != nil {
				return err
			}
			if factorCount == 0 {
				return ErrTwoFANotEnabled
			}
			if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&TwoFA{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&TwoFABackupCode{}).Error; err != nil {
				return err
			}
		case "oauth":
			if err := tx.Where("user_id = ? AND provider_id = ?", userID, providerID).Delete(&UserOAuthBinding{}).Error; err != nil {
				return err
			}
		default:
			return ErrSecurityProof
		}
		return tx.Model(&User{}).Where("id = ?", userID).Update("auth_version", gorm.Expr("auth_version + 1")).Error
	})
}

func RefreshPasskeyCredential(credential *PasskeyCredential) error {
	result := DB.Model(&PasskeyCredential{}).Where("user_id = ? AND credential_id = ?", credential.UserID, credential.CredentialID).Updates(map[string]any{
		"last_used_at": credential.LastUsedAt, "sign_count": credential.SignCount, "clone_warning": credential.CloneWarning,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrSecurityIdentity
	}
	return nil
}
