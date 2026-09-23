package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// replicaLagFixture 让 model.DB 指向主库、model.RO_DB 指向独立的副本库，两边各写入一份
// 目标用户，模拟从节点开启读写分离后主库已更新、副本尚未追上的复制延迟窗口。
func replicaLagFixture(t *testing.T, onPrimary model.User, onReplica model.User) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldDB, oldLogs, oldReadDB, oldRedis := model.DB, model.LOG_DB, model.RO_DB, common.RedisEnabled

	primary, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "primary.db")), &gorm.Config{})
	require.NoError(t, err)
	replica, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "replica.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, primary.AutoMigrate(&model.User{}, &model.Log{}, &model.RiskEvent{}))
	require.NoError(t, replica.AutoMigrate(&model.User{}))
	require.NoError(t, primary.Create(&onPrimary).Error)
	require.NoError(t, replica.Create(&onReplica).Error)

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.DB, model.LOG_DB, model.RO_DB, common.RedisEnabled = primary, primary, replica, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, model.RO_DB, common.RedisEnabled = oldDB, oldLogs, oldReadDB, oldRedis
		common.SetDatabaseTypes(oldMainType, oldLogType)
		for _, db := range []*gorm.DB{primary, replica} {
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		}
	})
	return primary
}

func callUserAdminHandler(t *testing.T, handler gin.HandlerFunc, actorRole int, body any) string {
	t.Helper()
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", actorRole)
		handler(c)
	})
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Body.String()
}

// 副本还停在 root 提升目标用户之前时，权限判断必须看主库的当前角色：
// 否则普通管理员能趁复制延迟禁用、重置另一个管理员。
func TestUserAdminActionsCheckTargetRoleOnPrimary(t *testing.T) {
	promoted := model.User{Id: 42, Username: "target", Password: "old-password-hash", AffCode: "aff-42", Role: common.RoleAdminUser}
	staleCopy := promoted
	staleCopy.Role = common.RoleCommonUser

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		body    any
	}{
		{"禁用", ManageUser, ManageRequest{Id: 42, Action: "disable"}},
		{"重置密码", UpdateUser, map[string]any{"id": 42, "username": "target", "password": "new-password-123"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primary := replicaLagFixture(t, promoted, staleCopy)

			body := callUserAdminHandler(t, tc.handler, common.RoleAdminUser, tc.body)

			assert.Contains(t, body, `"success":false`)
			var target model.User
			require.NoError(t, primary.First(&target, 42).Error)
			assert.Equal(t, common.RoleAdminUser, target.Role)
			assert.Equal(t, common.UserStatusEnabled, target.Status)
			assert.Equal(t, "old-password-hash", target.Password)
		})
	}
}

// ManageUser 只能写本次操作改动的列：把读到的整行写回，会把其他请求刚改过的
// 分组、角色等字段覆盖成旧值（副本延迟时读到的整行本身就是旧的）。
func TestManageUserDoesNotWriteBackStaleColumns(t *testing.T) {
	current := model.User{Id: 42, Username: "target", Password: "password-hash", AffCode: "aff-42", Group: "vip"}
	staleCopy := current
	staleCopy.Group = "default"
	primary := replicaLagFixture(t, current, staleCopy)

	body := callUserAdminHandler(t, ManageUser, common.RoleRootUser, ManageRequest{Id: 42, Action: "disable"})

	assert.Contains(t, body, `"success":true`)
	var target model.User
	require.NoError(t, primary.First(&target, 42).Error)
	assert.Equal(t, common.UserStatusDisabled, target.Status)
	assert.Equal(t, "vip", target.Group)
}

func TestManageUserDeleteReportsSuccess(t *testing.T) {
	target := model.User{Id: 42, Username: "target", Password: "password-hash", AffCode: "aff-42"}
	primary := replicaLagFixture(t, target, target)

	body := callUserAdminHandler(t, ManageUser, common.RoleRootUser, ManageRequest{Id: 42, Action: "delete"})

	assert.Contains(t, body, `"success":true`)
	var remaining int64
	require.NoError(t, primary.Model(&model.User{}).Where("id = ?", 42).Count(&remaining).Error)
	assert.Zero(t, remaining, "用户应被软删除")
}
