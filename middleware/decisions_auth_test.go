package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiDecisionsAcceptsNativeAPIKeyHeader(t *testing.T) {
	previousDB := model.DB
	previousReadDB := model.RO_DB
	previousRedis, previousMemoryCache := common.RedisEnabled, common.MemoryCacheEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLitePath := common.IsMasterNode, common.SQLitePath
	t.Setenv("SQL_DSN", "local")
	t.Setenv("SQL_DSN_READONLY", "")
	common.IsMasterNode, common.SQLitePath = false, ":memory:"
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		model.DB = previousDB
		model.RO_DB = previousReadDB
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemoryCache
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLitePath
		common.SetDatabaseTypes(previousMain, previousLog)
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	user := model.User{Username: "decisions-auth", Password: "fixture", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	key := strings.Repeat("d", 48)
	token := model.Token{UserId: user.Id, Key: key, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)
	engine := gin.New()
	engine.POST("/v1beta/decisions", TokenAuth(), func(c *gin.Context) {
		assert.Equal(t, user.Id, c.GetInt("id"))
		c.Status(http.StatusNoContent)
	})
	for _, header := range []string{"x-goog-api-key", "Authorization"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1beta/decisions", nil)
		value := "sk-" + key
		if header == "Authorization" {
			value = "Bearer " + value
		}
		request.Header.Set(header, value)
		engine.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	}
}
