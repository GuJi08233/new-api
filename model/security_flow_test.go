package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func securityTestDB(t *testing.T) (*gorm.DB, SecurityIdentity) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "security.db")), &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = db
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previous
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&User{}, &SecurityFlow{}, &PasskeyCredential{}, &UserOAuthBinding{}))
	user := User{Username: "security", Password: "existing hash", Status: common.UserStatusEnabled, Role: common.RoleAdminUser}
	require.NoError(t, db.Create(&user).Error)
	token, err := CreateSecurityFlow(SecurityFlow{UserID: user.Id, Kind: "session", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	return db, SecurityIdentity{UserID: user.Id, SessionID: SecurityTokenHash(token)}
}

func TestSecurityProofBoundAndSingleUse(t *testing.T) {
	_, identity := securityTestDB(t)
	digest := SecurityTokenHash("7")
	proof, err := CreateSecurityFlow(SecurityFlow{UserID: identity.UserID, SessionID: identity.SessionID, Kind: "proof", Scope: "channel.key.read", ContextHash: digest, ExpiresAt: time.Now().Add(time.Minute).Unix()})
	require.NoError(t, err)
	for _, mismatch := range []struct {
		identity       SecurityIdentity
		scope, context string
	}{
		{SecurityIdentity{UserID: identity.UserID + 1, SessionID: identity.SessionID}, "channel.key.read", digest},
		{SecurityIdentity{UserID: identity.UserID, SessionID: "another-session"}, "channel.key.read", digest},
		{identity, "account.delete", digest},
		{identity, "channel.key.read", SecurityTokenHash("8")},
	} {
		_, err := ConsumeSecurityFlow(proof, "proof", mismatch.identity, mismatch.scope, mismatch.context)
		require.ErrorIs(t, err, ErrSecurityProof)
	}
	_, err = ConsumeSecurityFlow(proof, "proof", identity, "channel.key.read", digest)
	require.NoError(t, err)
	_, err = ConsumeSecurityFlow(proof, "proof", identity, "channel.key.read", digest)
	assert.ErrorIs(t, err, ErrSecurityProof)
}

func TestSecurityIdentityUsesCurrentUserAndRevocation(t *testing.T) {
	db, identity := securityTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", identity.UserID).Updates(map[string]any{"role": common.RoleCommonUser, "group": "current"}).Error)
	user, err := ReadSecurityIdentity(identity)
	require.NoError(t, err)
	assert.Equal(t, common.RoleCommonUser, user.Role)
	assert.Equal(t, "current", user.Group)
	require.NoError(t, db.Model(&User{}).Where("id = ?", identity.UserID).Update("status", common.UserStatusDisabled).Error)
	_, err = ReadSecurityIdentity(identity)
	assert.ErrorIs(t, err, ErrSecurityIdentity)
	require.NoError(t, db.Model(&User{}).Where("id = ?", identity.UserID).Update("status", common.UserStatusEnabled).Error)
	require.NoError(t, RevokeSecuritySession(identity.SessionID))
	_, err = ReadSecurityIdentity(identity)
	assert.ErrorIs(t, err, ErrSecurityIdentity)
}

func TestSecurityCredentialChangeRevokesOtherSessions(t *testing.T) {
	db, identity := securityTestDB(t)
	token, err := CreateSecurityFlow(SecurityFlow{UserID: identity.UserID, Kind: "session", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	other := SecurityIdentity{UserID: identity.UserID, SessionID: SecurityTokenHash(token)}
	version, err := BindAccountColumn(identity, "github_id", "new-provider-id")
	require.NoError(t, err)
	_, err = ReadSecurityIdentity(other)
	assert.ErrorIs(t, err, ErrSecurityIdentity)
	identity.Version = version
	_, err = ReadSecurityIdentity(identity)
	require.NoError(t, err)
	_, err = BindAccountColumn(other, "github_id", "stale-provider-id")
	require.ErrorIs(t, err, ErrSecurityIdentity)
	var user User
	require.NoError(t, db.First(&user, identity.UserID).Error)
	assert.Equal(t, "new-provider-id", user.GitHubId)
	assert.Equal(t, common.RoleAdminUser, user.Role)
	assert.Equal(t, "existing hash", user.Password)
}

func TestSecurityPasskeyReplacementInvalidatesProofVersion(t *testing.T) {
	db, identity := securityTestDB(t)
	old := PasskeyCredential{UserID: identity.UserID, CredentialID: "old", PublicKey: "old-public"}
	require.NoError(t, db.Create(&old).Error)
	version, err := SaveAccountPasskey(identity, &PasskeyCredential{UserID: identity.UserID, CredentialID: "new", PublicKey: "new-public"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), version)
	_, err = ReadSecurityIdentity(identity)
	assert.ErrorIs(t, err, ErrSecurityIdentity)
	identity.Version = version
	_, err = ReadSecurityIdentity(identity)
	require.NoError(t, err)
	old.LastUsedAt = new(time.Time)
	assert.ErrorIs(t, RefreshPasskeyCredential(&old), ErrSecurityIdentity, "旧认证器响应不能恢复已替换凭证")
	var stored PasskeyCredential
	require.NoError(t, db.Where("user_id = ?", identity.UserID).First(&stored).Error)
	assert.Equal(t, "new", stored.CredentialID)
}

func TestSecurityPasswordUpdateChecksOriginalInsideTransaction(t *testing.T) {
	db, identity := securityTestDB(t)
	hash, err := common.Password2Hash("current-password")
	require.NoError(t, err)
	require.NoError(t, db.Model(&User{}).Where("id = ?", identity.UserID).Update("password", hash).Error)
	_, err = ChangeAccountPassword(identity, "wrong-password", &User{Password: "new-password"})
	require.Error(t, err)
	version, err := ChangeAccountPassword(identity, "current-password", &User{Password: "new-password"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), version)
	var stored User
	require.NoError(t, db.First(&stored, identity.UserID).Error)
	assert.True(t, common.ValidatePasswordAndHash("new-password", stored.Password))
	_, err = ChangeAccountPassword(identity, "current-password", &User{Password: "attacker-password"})
	assert.ErrorIs(t, err, ErrSecurityIdentity)
}

func TestSecurityVerifiedPasswordlessAccountCanSetFirstPassword(t *testing.T) {
	db, identity := securityTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", identity.UserID).Update("password", "").Error)
	_, err := ChangeAccountPassword(identity, "", &User{Password: "first-password"})
	require.NoError(t, err)
	var stored User
	require.NoError(t, db.First(&stored, identity.UserID).Error)
	assert.True(t, common.ValidatePasswordAndHash("first-password", stored.Password))
}
