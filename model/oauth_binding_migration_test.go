package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateLegacyOAuthRegistrationBindingsProtectsOnlyOAuthOnlyAccounts(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open("file:oauth-binding-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&User{}, &PasskeyCredential{}, &CustomOAuthProvider{}, &UserOAuthBinding{}))

	provider := CustomOAuthProvider{
		Name:                  "Test Provider",
		Slug:                  "test-provider",
		ClientId:              "client-id",
		AuthorizationEndpoint: "https://example.com/authorize",
		TokenEndpoint:         "https://example.com/token",
		UserInfoEndpoint:      "https://example.com/userinfo",
	}
	require.NoError(t, db.Create(&provider).Error)
	secondProvider := provider
	secondProvider.Id = 0
	secondProvider.Name = "Second Test Provider"
	secondProvider.Slug = "second-test-provider"
	require.NoError(t, db.Create(&secondProvider).Error)
	require.NoError(t, db.Exec("UPDATE custom_oauth_providers SET disable_unbind = NULL WHERE id = ?", provider.Id).Error)

	createUser := func(name, password string, update func(*User)) User {
		t.Helper()
		user := User{
			Username: name,
			Password: password,
			AffCode:  "aff-" + name,
			Status:   common.UserStatusEnabled,
			Role:     common.RoleCommonUser,
		}
		if update != nil {
			update(&user)
		}
		require.NoError(t, db.Create(&user).Error)
		return user
	}
	createBinding := func(userId, providerId int, externalId string, createdAt time.Time, registration bool) UserOAuthBinding {
		t.Helper()
		binding := UserOAuthBinding{
			UserId:         userId,
			ProviderId:     providerId,
			ProviderUserId: externalId,
			CreatedAt:      createdAt,
			IsRegistration: registration,
		}
		require.NoError(t, db.Create(&binding).Error)
		return binding
	}

	baseTime := time.Unix(1_700_000_000, 0)
	oauthOnly := createUser("oauth-only", "", nil)
	oauthOnlyBinding := createBinding(oauthOnly.Id, provider.Id, "oauth-only", baseTime, false)

	passwordUser := createUser("password-user", "password-hash", nil)
	passwordBinding := createBinding(passwordUser.Id, provider.Id, "password-user", baseTime, false)
	require.NoError(t, db.Exec("UPDATE user_oauth_bindings SET is_registration = NULL WHERE id = ?", passwordBinding.Id).Error)

	builtInOAuthUser := createUser("built-in-oauth", "", func(user *User) {
		user.GitHubId = "github-user"
	})
	builtInBinding := createBinding(builtInOAuthUser.Id, provider.Id, "built-in-oauth", baseTime, false)

	passkeyUser := createUser("passkey-user", "", nil)
	require.NoError(t, db.Create(&PasskeyCredential{
		UserID:       passkeyUser.Id,
		CredentialID: "credential-id",
		PublicKey:    "public-key",
	}).Error)
	passkeyBinding := createBinding(passkeyUser.Id, provider.Id, "passkey-user", baseTime, false)

	multipleBindingsUser := createUser("multiple-bindings", "", nil)
	oldestBinding := createBinding(multipleBindingsUser.Id, provider.Id, "multiple-oldest", baseTime, false)
	newerBinding := createBinding(multipleBindingsUser.Id, secondProvider.Id, "multiple-newer", baseTime.Add(time.Hour), false)

	alreadyProtectedUser := createUser("already-protected", "", nil)
	protectedBinding := createBinding(alreadyProtectedUser.Id, provider.Id, "already-protected", baseTime, true)
	extraBinding := createBinding(alreadyProtectedUser.Id, secondProvider.Id, "already-protected-extra", baseTime.Add(time.Hour), false)

	require.NoError(t, migrateLegacyOAuthRegistrationBindings())
	require.NoError(t, migrateLegacyOAuthRegistrationBindings(), "migration must be idempotent")

	bindings := []struct {
		binding UserOAuthBinding
		want    bool
	}{
		{oauthOnlyBinding, true},
		{passwordBinding, false},
		{builtInBinding, false},
		{passkeyBinding, false},
		{oldestBinding, true},
		{newerBinding, false},
		{protectedBinding, true},
		{extraBinding, false},
	}
	for _, testCase := range bindings {
		t.Run(fmt.Sprintf("binding-%d", testCase.binding.Id), func(t *testing.T) {
			var reloaded UserOAuthBinding
			require.NoError(t, db.First(&reloaded, testCase.binding.Id).Error)
			assert.Equal(t, testCase.want, reloaded.IsRegistration)
		})
	}

	var reloadedProvider CustomOAuthProvider
	require.NoError(t, db.First(&reloadedProvider, provider.Id).Error)
	assert.False(t, reloadedProvider.DisableUnbind)
}
