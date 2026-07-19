package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelUpdateResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func setupChannelMultiKeyConversionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func updateChannelForMultiKeyTest(t *testing.T, payload map[string]any) channelUpdateResponse {
	t.Helper()

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelUpdateResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func insertSingleKeyChannel(t *testing.T, db *gorm.DB, channelType int, key string) model.Channel {
	t.Helper()

	channel := model.Channel{
		Type:   channelType,
		Key:    key,
		Status: common.ChannelStatusEnabled,
		Name:   "single-key-channel",
		Models: "gpt-4o",
		Group:  "default",
	}
	require.NoError(t, db.Create(&channel).Error)
	return channel
}

func TestUpdateChannelConvertsSingleKeyAndAppendsNewKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := insertSingleKeyChannel(t, db, constant.ChannelTypeOpenAI, "sk-existing")

	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":                channel.Id,
		"multi_key_enabled": true,
		"multi_key_mode":    "polling",
		"key_mode":          "append",
		"key":               " sk-new-one\n\nsk-existing\nsk-new-two ",
	})

	require.True(t, response.Success, response.Message)
	updated, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "sk-existing\nsk-new-one\nsk-new-two", updated.Key)
	assert.True(t, updated.ChannelInfo.IsMultiKey)
	assert.Equal(t, 3, updated.ChannelInfo.MultiKeySize)
	assert.Equal(t, constant.MultiKeyModePolling, updated.ChannelInfo.MultiKeyMode)
	assert.NotNil(t, updated.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, updated.ChannelInfo.MultiKeyStatusList)
	assert.Zero(t, updated.ChannelInfo.MultiKeyPollingIndex)
}

func TestUpdateChannelConversionPreservesExistingKeyByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := insertSingleKeyChannel(t, db, constant.ChannelTypeOpenAI, "sk-existing")

	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":                channel.Id,
		"multi_key_enabled": true,
	})

	require.True(t, response.Success, response.Message)
	updated, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "sk-existing", updated.Key)
	assert.True(t, updated.ChannelInfo.IsMultiKey)
	assert.Equal(t, 1, updated.ChannelInfo.MultiKeySize)
	assert.Equal(t, constant.MultiKeyModeRandom, updated.ChannelInfo.MultiKeyMode)
}

func TestUpdateChannelConversionTreatsWhitespaceKeyAsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := insertSingleKeyChannel(t, db, constant.ChannelTypeOpenAI, "sk-existing")

	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":                channel.Id,
		"multi_key_enabled": true,
		"key":               "   ",
	})

	require.True(t, response.Success, response.Message)
	updated, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "sk-existing", updated.Key)
	assert.True(t, updated.ChannelInfo.IsMultiKey)
}

func TestUpdateChannelRejectsInvalidMultiKeyConversion(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		payload     map[string]any
		message     string
	}{
		{
			name:        "unsupported strategy",
			channelType: constant.ChannelTypeOpenAI,
			payload: map[string]any{
				"multi_key_enabled": true,
				"multi_key_mode":    "first",
			},
			message: "不支持的密钥聚合模式",
		},
		{
			name:        "unsupported key update mode",
			channelType: constant.ChannelTypeOpenAI,
			payload: map[string]any{
				"multi_key_enabled": true,
				"key_mode":          "merge",
			},
			message: "不支持的密钥更新模式",
		},
		{
			name:        "codex",
			channelType: constant.ChannelTypeCodex,
			payload: map[string]any{
				"multi_key_enabled": true,
			},
			message: "Codex 渠道不支持密钥聚合模式",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupChannelMultiKeyConversionTestDB(t)
			channel := insertSingleKeyChannel(t, db, tt.channelType, "sk-existing")
			tt.payload["id"] = channel.Id

			response := updateChannelForMultiKeyTest(t, tt.payload)

			assert.False(t, response.Success)
			assert.Contains(t, response.Message, tt.message)
			unchanged, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.False(t, unchanged.ChannelInfo.IsMultiKey)
			assert.Equal(t, "sk-existing", unchanged.Key)
		})
	}
}

func TestUpdateChannelDoesNotDowngradeMultiKeyChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := insertSingleKeyChannel(t, db, constant.ChannelTypeOpenAI, "sk-one\nsk-two")
	channel.ChannelInfo = model.ChannelInfo{
		IsMultiKey:         true,
		MultiKeySize:       2,
		MultiKeyStatusList: map[int]int{},
		MultiKeyMode:       constant.MultiKeyModeRandom,
	}
	require.NoError(t, db.Model(&channel).Update("channel_info", channel.ChannelInfo).Error)

	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":                channel.Id,
		"multi_key_enabled": false,
	})

	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "不支持将多密钥渠道转换为单密钥模式")
	unchanged, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.True(t, unchanged.ChannelInfo.IsMultiKey)
	assert.Equal(t, 2, unchanged.ChannelInfo.MultiKeySize)
	assert.Equal(t, "sk-one\nsk-two", unchanged.Key)
}

func TestUpdateChannelChangesMultiKeyStrategyWithoutChangingKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-one\nsk-two",
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-channel",
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyMode:       constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{1: common.ChannelStatusManuallyDisabled},
		},
	}
	require.NoError(t, db.Create(&channel).Error)

	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":             channel.Id,
		"multi_key_mode": "polling",
	})

	require.True(t, response.Success, response.Message)
	updated, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "sk-one\nsk-two", updated.Key)
	assert.Equal(t, constant.MultiKeyModePolling, updated.ChannelInfo.MultiKeyMode)
	assert.Equal(t, map[int]int{1: common.ChannelStatusManuallyDisabled}, updated.ChannelInfo.MultiKeyStatusList)
}

func TestMergeChannelKeysCompactsVertexCredentials(t *testing.T) {
	existing := "{\n  \"type\": \"service_account\",\n  \"private_key\": \"old\"\n}"
	newKeys := `[{"type":"service_account","private_key":"new"}]`

	merged, err := mergeChannelKeys(existing, newKeys, true)

	require.NoError(t, err)
	assert.Equal(t,
		"{\"private_key\":\"old\",\"type\":\"service_account\"}\n{\"private_key\":\"new\",\"type\":\"service_account\"}",
		merged,
	)
}

func TestMergeChannelKeysParsesExistingVertexLines(t *testing.T) {
	existing := `{"type":"service_account","id":"old-1"}
{"type":"service_account","id":"old-2"}`
	newKeys := `[{"type":"service_account","id":"new-1"}]`

	merged, err := mergeChannelKeys(existing, newKeys, true)

	require.NoError(t, err)
	assert.Equal(t,
		`{"id":"old-1","type":"service_account"}
{"id":"old-2","type":"service_account"}
{"id":"new-1","type":"service_account"}`,
		merged,
	)
}

func TestMergeChannelKeysPreservesExistingDuplicates(t *testing.T) {
	merged, err := mergeChannelKeys("sk-a\nsk-a\nsk-b", "sk-a\nsk-c\nsk-c", false)

	require.NoError(t, err)
	assert.Equal(t, "sk-a\nsk-a\nsk-b\nsk-c", merged)
}

func TestParseChannelKeysSupportsLegacyArraysAndNewlineValues(t *testing.T) {
	regular, err := parseChannelKeys(" sk-one\n\nsk-two ", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-one", "sk-two"}, regular)

	regular, err = parseChannelKeys(`["sk-one", "sk-two"]`, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-one", "sk-two"}, regular)

	vertex, err := parseChannelKeys(
		"{\"type\":\"service_account\",\"id\":\"one\"}\n{\"type\":\"service_account\",\"id\":\"two\"}",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t,
		[]string{
			"{\"id\":\"one\",\"type\":\"service_account\"}",
			"{\"id\":\"two\",\"type\":\"service_account\"}",
		},
		vertex,
	)

	vertex, err = parseChannelKeys(
		`[{"type":"service_account","id":"one"},{"type":"service_account","id":"two"}]`,
		true,
	)
	require.NoError(t, err)
	assert.Len(t, vertex, 2)
}
