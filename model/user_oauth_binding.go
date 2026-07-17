package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// UserOAuthBinding stores the binding relationship between users and custom OAuth providers
type UserOAuthBinding struct {
	Id             int       `json:"id" gorm:"primaryKey"`
	UserId         int       `json:"user_id" gorm:"not null;uniqueIndex:ux_user_provider"`                                    // User ID - one binding per user per provider
	ProviderId     int       `json:"provider_id" gorm:"not null;uniqueIndex:ux_user_provider;uniqueIndex:ux_provider_userid"` // Custom OAuth provider ID
	ProviderUserId string    `json:"provider_user_id" gorm:"type:varchar(256);not null;uniqueIndex:ux_provider_userid"`       // User ID from OAuth provider - one OAuth account per provider
	IsRegistration bool      `json:"is_registration"`                                                                         // True when the account was registered via this binding; such bindings cannot be unbound by the user
	CreatedAt      time.Time `json:"created_at"`
}

func (UserOAuthBinding) TableName() string {
	return "user_oauth_bindings"
}

// GetUserOAuthBindingsByUserId returns all OAuth bindings for a user
func GetUserOAuthBindingsByUserId(userId int) ([]*UserOAuthBinding, error) {
	var bindings []*UserOAuthBinding
	err := DB.Where("user_id = ?", userId).Find(&bindings).Error
	return bindings, err
}

// GetUserOAuthBinding returns a specific binding for a user and provider
func GetUserOAuthBinding(userId, providerId int) (*UserOAuthBinding, error) {
	var binding UserOAuthBinding
	err := DB.Where("user_id = ? AND provider_id = ?", userId, providerId).First(&binding).Error
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// GetUserByOAuthBinding finds a user by provider ID and provider user ID
func GetUserByOAuthBinding(providerId int, providerUserId string) (*User, error) {
	var binding UserOAuthBinding
	err := DB.Where("provider_id = ? AND provider_user_id = ?", providerId, providerUserId).First(&binding).Error
	if err != nil {
		return nil, err
	}

	var user User
	err = DB.First(&user, binding.UserId).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// IsProviderUserIdTaken checks if a provider user ID is already bound to any user
func IsProviderUserIdTaken(providerId int, providerUserId string) bool {
	var count int64
	DB.Model(&UserOAuthBinding{}).Where("provider_id = ? AND provider_user_id = ?", providerId, providerUserId).Count(&count)
	return count > 0
}

// CreateUserOAuthBinding creates a new OAuth binding
func CreateUserOAuthBinding(binding *UserOAuthBinding) error {
	if binding.UserId == 0 {
		return errors.New("user ID is required")
	}
	if binding.ProviderId == 0 {
		return errors.New("provider ID is required")
	}
	if binding.ProviderUserId == "" {
		return errors.New("provider user ID is required")
	}

	// Check if this provider user ID is already taken
	if IsProviderUserIdTaken(binding.ProviderId, binding.ProviderUserId) {
		return errors.New("this OAuth account is already bound to another user")
	}

	binding.CreatedAt = time.Now()
	return DB.Create(binding).Error
}

// CreateUserOAuthBindingWithTx creates a new OAuth binding within a transaction
func CreateUserOAuthBindingWithTx(tx *gorm.DB, binding *UserOAuthBinding) error {
	if binding.UserId == 0 {
		return errors.New("user ID is required")
	}
	if binding.ProviderId == 0 {
		return errors.New("provider ID is required")
	}
	if binding.ProviderUserId == "" {
		return errors.New("provider user ID is required")
	}

	// Check if this provider user ID is already taken (use tx to check within the same transaction)
	var count int64
	tx.Model(&UserOAuthBinding{}).Where("provider_id = ? AND provider_user_id = ?", binding.ProviderId, binding.ProviderUserId).Count(&count)
	if count > 0 {
		return errors.New("this OAuth account is already bound to another user")
	}

	binding.CreatedAt = time.Now()
	return tx.Create(binding).Error
}

// UpdateUserOAuthBinding updates an existing OAuth binding (e.g., rebind to different OAuth account)
func UpdateUserOAuthBinding(userId, providerId int, newProviderUserId string) error {
	// Check if the new provider user ID is already taken by another user
	var existingBinding UserOAuthBinding
	err := DB.Where("provider_id = ? AND provider_user_id = ?", providerId, newProviderUserId).First(&existingBinding).Error
	if err == nil && existingBinding.UserId != userId {
		return errors.New("this OAuth account is already bound to another user")
	}

	// Check if user already has a binding for this provider
	var binding UserOAuthBinding
	err = DB.Where("user_id = ? AND provider_id = ?", userId, providerId).First(&binding).Error
	if err != nil {
		// No existing binding, create new one
		return CreateUserOAuthBinding(&UserOAuthBinding{
			UserId:         userId,
			ProviderId:     providerId,
			ProviderUserId: newProviderUserId,
		})
	}

	// Update existing binding
	return DB.Model(&binding).Update("provider_user_id", newProviderUserId).Error
}

// DeleteUserOAuthBinding deletes an OAuth binding
func DeleteUserOAuthBinding(userId, providerId int) error {
	return DB.Where("user_id = ? AND provider_id = ?", userId, providerId).Delete(&UserOAuthBinding{}).Error
}

func deleteUserOAuthBindingsByUserId(tx *gorm.DB, userId int) error {
	return tx.Where("user_id = ?", userId).Delete(&UserOAuthBinding{}).Error
}

// migrateLegacyOAuthRegistrationBindings 保护 IsRegistration 字段上线前由
// 自定义 OAuth 创建的历史账号。没有密码、内置 OAuth 身份或 Passkey 的账号
// 视为仅依赖自定义 OAuth；存在多个绑定时只标记最早的注册来源，其余仍可解绑。
func migrateLegacyOAuthRegistrationBindings() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// AutoMigrate 在存量数据库新增布尔列时可能产生 NULL。统一由业务迁移
		// 归一化，避免依赖不同数据库对布尔默认值的方言实现。
		if err := tx.Model(&CustomOAuthProvider{}).
			Where("disable_unbind IS NULL").
			UpdateColumn("disable_unbind", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserOAuthBinding{}).
			Where("is_registration IS NULL").
			UpdateColumn("is_registration", false).Error; err != nil {
			return err
		}

		var candidateUserIds []int
		if err := tx.Model(&User{}).
			Where("password = ?", "").
			Where("(github_id = ? OR github_id IS NULL)", "").
			Where("(discord_id = ? OR discord_id IS NULL)", "").
			Where("(oidc_id = ? OR oidc_id IS NULL)", "").
			Where("(wechat_id = ? OR wechat_id IS NULL)", "").
			Where("(telegram_id = ? OR telegram_id IS NULL)", "").
			Where("(linux_do_id = ? OR linux_do_id IS NULL)", "").
			Pluck("id", &candidateUserIds).Error; err != nil {
			return err
		}
		if len(candidateUserIds) == 0 {
			return nil
		}

		var passkeyUserIds []int
		if err := tx.Model(&PasskeyCredential{}).
			Where("user_id IN ?", candidateUserIds).
			Pluck("user_id", &passkeyUserIds).Error; err != nil {
			return err
		}
		passkeyUsers := make(map[int]struct{}, len(passkeyUserIds))
		for _, userId := range passkeyUserIds {
			passkeyUsers[userId] = struct{}{}
		}

		var alreadyProtectedUserIds []int
		if err := tx.Model(&UserOAuthBinding{}).
			Where("user_id IN ? AND is_registration = ?", candidateUserIds, true).
			Pluck("user_id", &alreadyProtectedUserIds).Error; err != nil {
			return err
		}
		protectedUsers := make(map[int]struct{}, len(passkeyUsers)+len(alreadyProtectedUserIds))
		for userId := range passkeyUsers {
			protectedUsers[userId] = struct{}{}
		}
		for _, userId := range alreadyProtectedUserIds {
			protectedUsers[userId] = struct{}{}
		}

		unprotectedUserIds := make([]int, 0, len(candidateUserIds))
		for _, userId := range candidateUserIds {
			if _, excluded := protectedUsers[userId]; !excluded {
				unprotectedUserIds = append(unprotectedUserIds, userId)
			}
		}
		if len(unprotectedUserIds) == 0 {
			return nil
		}

		var bindings []UserOAuthBinding
		if err := tx.Where("user_id IN ? AND is_registration = ?", unprotectedUserIds, false).
			Order("user_id ASC, created_at ASC, id ASC").
			Find(&bindings).Error; err != nil {
			return err
		}

		bindingIds := make([]int, 0, len(unprotectedUserIds))
		seenUsers := make(map[int]struct{}, len(unprotectedUserIds))
		for _, binding := range bindings {
			if _, seen := seenUsers[binding.UserId]; seen {
				continue
			}
			seenUsers[binding.UserId] = struct{}{}
			bindingIds = append(bindingIds, binding.Id)
		}

		const updateBatchSize = 500
		for start := 0; start < len(bindingIds); start += updateBatchSize {
			end := min(start+updateBatchSize, len(bindingIds))
			if err := tx.Model(&UserOAuthBinding{}).
				Where("id IN ?", bindingIds[start:end]).
				UpdateColumn("is_registration", true).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetBindingCountByProviderId returns the number of bindings for a provider
func GetBindingCountByProviderId(providerId int) (int64, error) {
	var count int64
	err := DB.Model(&UserOAuthBinding{}).Where("provider_id = ?", providerId).Count(&count).Error
	return count, err
}
