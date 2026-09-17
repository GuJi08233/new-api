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

func manageUserQuotaFixture(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manage.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogs, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogs, previousRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	router := gin.New()
	router.POST("/manage", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("role", common.RoleRootUser)
		ManageUser(c)
	})
	return db, router
}

// 管理员覆盖额度是直接赋值，不经过 applyUserQuotaDelta 的 CAS 条件。越界值一旦
// 写入，该账号之后所有加额度操作（充值、退款、兑换码）都会永久失败，只剩扣费
// 可用，所以这条路径必须自己守住 quota 列的可表示范围。
func TestManageUserQuotaOverrideRejectsOutOfRange(t *testing.T) {
	db, router := manageUserQuotaFixture(t)
	require.NoError(t, db.Create(&model.User{
		Id:       42,
		Username: "override-target",
		AffCode:  "aff-42",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    100,
	}).Error)

	override := func(value int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := common.Marshal(ManageRequest{Id: 42, Action: "add_quota", Mode: "override", Value: value})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/manage", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	currentQuota := func() int {
		t.Helper()
		var user model.User
		require.NoError(t, db.First(&user, "id = ?", 42).Error)
		return user.Quota
	}

	assert.Contains(t, override(common.MaxQuota).Body.String(), `"success":false`)
	assert.Equal(t, 100, currentQuota(), "越界覆盖必须被拒绝且不写库")

	assert.Contains(t, override(-1).Body.String(), `"success":false`)
	assert.Equal(t, 100, currentQuota(), "负额度覆盖必须被拒绝且不写库")

	assert.Contains(t, override(common.MaxQuota-1).Body.String(), `"success":true`)
	assert.Equal(t, common.MaxQuota-1, currentQuota(), "边界内的覆盖必须照常生效")
}
