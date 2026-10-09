package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addChannelForEmptyKeyTest(t *testing.T, channelType int, mode, key string, isMultiKey bool) channelUpdateResponse {
	t.Helper()

	body, err := common.Marshal(AddChannelRequest{
		Mode:         mode,
		MultiKeyMode: constant.MultiKeyModeRandom,
		Channel: &model.Channel{
			Type:   channelType,
			Key:    key,
			Status: common.ChannelStatusEnabled,
			Name:   "empty-key-test-channel",
			Models: "gpt-4o",
			Group:  "default",
			ChannelInfo: model.ChannelInfo{
				IsMultiKey: isMultiKey,
			},
		},
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AddChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelUpdateResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestAddChannelAllowsOA2EmptySingleKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name string
		key  string
	}{
		{name: "empty"},
		{name: "whitespace", key: " \t\n "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupChannelMultiKeyConversionTestDB(t)
			response := addChannelForEmptyKeyTest(t, constant.ChannelTypeOA2, "single", tt.key, false)

			require.True(t, response.Success, response.Message)
			var channels []model.Channel
			require.NoError(t, db.Find(&channels).Error)
			require.Len(t, channels, 1)
			channel := channels[0]
			assert.Equal(t, constant.ChannelTypeOA2, channel.Type)
			assert.Empty(t, channel.Key)
			assert.False(t, channel.ChannelInfo.IsMultiKey)
			key, _, keyErr := channel.GetNextEnabledKey()
			assert.Nil(t, keyErr)
			assert.Empty(t, key)
			models, err := model.GetEnabledModelsWithError()
			require.NoError(t, err)
			assert.Equal(t, []string{"gpt-4o"}, models)
			var ability model.Ability
			require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.Equal(t, "default", ability.Group)
			assert.True(t, ability.Enabled)
		})
	}
}

func TestAddChannelRejectsMissingRequiredKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name        string
		channelType int
		mode        string
		key         string
		isMultiKey  bool
		message     string
	}{
		{name: "OpenAI empty single", channelType: constant.ChannelTypeOpenAI, mode: "single", message: "channel cannot be empty"},
		{name: "OpenAI whitespace single", channelType: constant.ChannelTypeOpenAI, mode: "single", key: " \t\n ", message: "channel cannot be empty"},
		{name: "OA2 empty batch", channelType: constant.ChannelTypeOA2, mode: "batch", message: "密钥不能为空"},
		{name: "OA2 whitespace batch", channelType: constant.ChannelTypeOA2, mode: "batch", key: " \t\n ", message: "密钥不能为空"},
		{name: "OA2 empty multi-key", channelType: constant.ChannelTypeOA2, mode: "multi_to_single", message: "密钥不能为空"},
		{name: "OA2 whitespace multi-key", channelType: constant.ChannelTypeOA2, mode: "multi_to_single", key: " \t\n ", message: "密钥不能为空"},
		{name: "OA2 single marked multi-key", channelType: constant.ChannelTypeOA2, mode: "single", isMultiKey: true, message: "密钥不能为空"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupChannelMultiKeyConversionTestDB(t)
			response := addChannelForEmptyKeyTest(t, tt.channelType, tt.mode, tt.key, tt.isMultiKey)

			assert.False(t, response.Success)
			assert.Equal(t, tt.message, response.Message)
			var count int64
			require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, db.Model(&model.Ability{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestAddChannelBatchSkipsBlankKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, channelType := range []int{constant.ChannelTypeOA2, constant.ChannelTypeOpenAI} {
		t.Run(constant.ChannelTypeNames[channelType], func(t *testing.T) {
			db := setupChannelMultiKeyConversionTestDB(t)
			response := addChannelForEmptyKeyTest(t, channelType, "batch", "\nsk-one\n \t\n\nsk-two\n", false)

			require.True(t, response.Success, response.Message)
			var channels []model.Channel
			require.NoError(t, db.Order("id").Find(&channels).Error)
			require.Len(t, channels, 2)
			assert.Equal(t, "sk-one", channels[0].Key)
			assert.Equal(t, "sk-two", channels[1].Key)
			var count int64
			require.NoError(t, db.Model(&model.Ability{}).Where("enabled = ?", true).Count(&count).Error)
			assert.EqualValues(t, 2, count)
		})
	}
}

func TestAddChannelMultiKeySkipsBlankKeys(t *testing.T) {
	db := setupChannelMultiKeyConversionTestDB(t)
	response := addChannelForEmptyKeyTest(t, constant.ChannelTypeOA2, "multi_to_single", "\n sk-one \n \t\n\nsk-two\n", false)

	require.True(t, response.Success, response.Message)
	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	assert.True(t, channel.ChannelInfo.IsMultiKey)
	assert.Equal(t, 2, channel.ChannelInfo.MultiKeySize)
	assert.Equal(t, []string{"sk-one", "sk-two"}, channel.GetKeys())
}

func TestUpdateChannelOA2EmptyKeyKeepsExistingKey(t *testing.T) {
	db := setupChannelMultiKeyConversionTestDB(t)
	channel := insertSingleKeyChannel(t, db, constant.ChannelTypeOA2, "sk-existing")
	response := updateChannelForMultiKeyTest(t, map[string]any{
		"id":   channel.Id,
		"type": constant.ChannelTypeOA2,
		"key":  "",
	})

	require.True(t, response.Success, response.Message)
	updated, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "sk-existing", updated.Key)
}
