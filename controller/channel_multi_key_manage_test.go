package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManageMultiKeysMasksEveryKeyPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelMultiKeyConversionTestDB(t)
	rawKeys := []string{
		"x",
		"tiny",
		"abcdefgh",
		"abcdefghi",
		"abcdefghijklmnop",
		"abcdefghijklmnopq",
		"密钥短值",
	}
	channel := model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    strings.Join(rawKeys, "\n"),
		Status: common.ChannelStatusEnabled,
		Name:   "multi-key-preview-test",
		Models: "gpt-4o",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: len(rawKeys),
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}
	require.NoError(t, db.Create(&channel).Error)

	body, err := common.Marshal(MultiKeyManageRequest{
		ChannelId: channel.Id,
		Action:    "get_key_status",
		Page:      1,
		PageSize:  len(rawKeys),
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/multi_key/manage", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ManageMultiKeys(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
		Data    MultiKeyStatusResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Len(t, response.Data.Keys, len(rawKeys))

	expectedPreviews := []string{
		"*",
		"****",
		"********",
		"ab****hi",
		"ab****op",
		"abcd********nopq",
		"****",
	}
	for i, keyStatus := range response.Data.Keys {
		assert.Equal(t, i, keyStatus.Index)
		assert.Equal(t, expectedPreviews[i], keyStatus.KeyPreview)
		assert.NotEqual(t, rawKeys[i], keyStatus.KeyPreview)
		assert.Contains(t, keyStatus.KeyPreview, "*")
	}
}

func TestMaskMultiKeyPreviewBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{name: "empty", key: "", expected: ""},
		{name: "one character", key: "a", expected: "*"},
		{name: "four characters", key: "abcd", expected: "****"},
		{name: "eight characters", key: "abcdefgh", expected: "********"},
		{name: "nine characters", key: "abcdefghi", expected: "ab****hi"},
		{name: "sixteen characters", key: "abcdefghijklmnop", expected: "ab****op"},
		{name: "seventeen characters", key: "abcdefghijklmnopq", expected: "abcd********nopq"},
		{name: "unicode uses characters", key: "密钥短值九字符测试", expected: "密钥****测试"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview := maskMultiKeyPreview(test.key)

			assert.Equal(t, test.expected, preview)
			if test.key != "" {
				assert.NotEqual(t, test.key, preview)
				assert.Contains(t, preview, "*")
			}
		})
	}
}
