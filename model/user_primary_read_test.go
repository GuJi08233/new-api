package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// access token 鉴权直接使用查到的角色和状态。从节点开启读写分离时，副本可能还停在
// 令牌轮换或降权之前，这两种变更都必须立即生效，不能等副本追上。
func TestValidateAccessTokenIgnoresStaleReplica(t *testing.T) {
	tests := []struct {
		name        string
		staleToken  string
		token       string
		wantUser    bool
		wantRoleNow int
	}{
		{"已轮换掉的旧令牌立即失效", "revoked-token", "revoked-token", false, 0},
		{"降权立即生效", "current-token", "current-token", true, common.RoleCommonUser},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			currentToken := "current-token"
			require.NoError(t, DB.Create(&User{Id: 42, Username: "target", AffCode: "aff-42", Role: common.RoleCommonUser, AccessToken: &currentToken}).Error)

			replica, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "replica.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, replica.AutoMigrate(&User{}))
			staleToken := tc.staleToken
			require.NoError(t, replica.Create(&User{Id: 42, Username: "target", AffCode: "aff-42", Role: common.RoleAdminUser, AccessToken: &staleToken}).Error)
			previous := RO_DB
			RO_DB = replica
			t.Cleanup(func() {
				RO_DB = previous
				sqlDB, err := replica.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})

			user, err := ValidateAccessToken("Bearer " + tc.token)

			require.NoError(t, err)
			if !tc.wantUser {
				assert.Nil(t, user)
				return
			}
			require.NotNil(t, user)
			assert.Equal(t, tc.wantRoleNow, user.Role)
		})
	}
}

// 硬删除在同一事务内确认目标角色仍是做权限判断时读到的值：
// 判断之后被提升为管理员的用户，不能再按普通用户的权限被删除。
func TestHardDeleteUserByIdRejectsChangedRole(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 42, Username: "target", AffCode: "aff-42", Role: common.RoleAdminUser}).Error)

	err := HardDeleteUserById(42, common.RoleCommonUser)

	require.ErrorIs(t, err, ErrUserRoleChanged)
	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", 42).Count(&count).Error)
	assert.Equal(t, int64(1), count, "角色已变化时不能删除")
}
