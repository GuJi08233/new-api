package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupClientUaRuleDB 拒绝路径会写风控拦截事件并渲染本地化文案，需要内存库与 i18n。
func setupClientUaRuleDB(t *testing.T) *gorm.DB {
	t.Helper()
	require.NoError(t, i18n.Init())
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.RiskEvent{}))
	originalDB, originalLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestDistributeRejectsClientUaBeforeRouting(t *testing.T) {
	db := setupClientUaRuleDB(t)
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest([]operation_setting.ClientUaRule{
		{Name: "opus-cli-only", ModelRegex: []string{"^claude-opus"}, UaAllow: []string{"(external, cli)"}},
		{Name: "no-sdk-cli", ModelRegex: []string{"^gpt-"}, UaDeny: []string{"sdk-cli"}, Message: "SDK 模式不可用", StatusCode: http.StatusConflict},
	}))

	t.Run("claude messages get claude error shape", func(t *testing.T) {
		ctx, recorder := newDocumentRouteTestContext(t, "/v1/messages", `{"model":"claude-opus-4-1","max_tokens":16,"messages":[]}`)
		ctx.Request.Header.Set("User-Agent", "Claude-Desktop/0.14.10 (Windows)")
		common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
		ctx.Set(common.RequestIdKey, "ua-test")
		ctx.Set("id", 42)

		Distribute()(ctx)

		assert.True(t, ctx.IsAborted())
		assert.Equal(t, http.StatusForbidden, recorder.Code)
		assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRiskBlocked))
		var response struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, "error", response.Type)
		assert.Contains(t, response.Error.Message, "opus-cli-only")
		assert.True(t, strings.HasSuffix(response.Error.Message, "(request id: ua-test)"))

		var events []model.RiskEvent
		require.NoError(t, db.Where("event_type = ?", model.RiskEventBlockClientUa).Find(&events).Error)
		require.Len(t, events, 1)
		assert.Equal(t, 42, events[0].UserId)
		assert.Equal(t, "opus-cli-only", events[0].Rule)
		assert.Equal(t, "Claude-Desktop/0.14.10 (Windows)", events[0].Ua)
	})

	t.Run("openai format uses custom status and message", func(t *testing.T) {
		ctx, recorder := newDocumentRouteTestContext(t, "/v1/chat/completions", `{"model":"gpt-4o","messages":[]}`)
		ctx.Request.Header.Set("User-Agent", "claude-cli/2.1.296 (external, sdk-cli)")
		common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
		ctx.Set(common.RequestIdKey, "ua-test-2")

		Distribute()(ctx)

		assert.True(t, ctx.IsAborted())
		assert.Equal(t, http.StatusConflict, recorder.Code)
		var response struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, "SDK 模式不可用 (request id: ua-test-2)", response.Error.Message)
		assert.Equal(t, string(types.ErrorCodeAccessDenied), response.Error.Code)
	})
}

// Playground 对所有登录用户开放，切换到受限分组后必须按新分组判定，不能凭入口时的用户分组放行。
func TestDistributeRechecksClientUaAfterPlaygroundGroupSwitch(t *testing.T) {
	db := setupClientUaRuleDB(t)
	previousGroups := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"默认分组","vip":"vip分组"}`))
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups)) })
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest([]operation_setting.ClientUaRule{
		{Name: "vip-cli-only", GroupRegex: []string{"^vip$"}, UaAllow: []string{"claude-cli/"}},
	}))

	ctx, recorder := newDocumentRouteTestContext(t, "/pg/chat/completions", `{"model":"claude-opus-4-1","group":"vip","messages":[]}`)
	ctx.Request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	ctx.Set("id", 7)

	Distribute()(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	var events []model.RiskEvent
	require.NoError(t, db.Where("event_type = ?", model.RiskEventBlockClientUa).Find(&events).Error)
	require.Len(t, events, 1)
	assert.Equal(t, "vip-cli-only", events[0].Rule)
}
