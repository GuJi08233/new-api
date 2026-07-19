package controller

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func newRetryError(statusCode int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("upstream error %d", statusCode),
		types.ErrorCodeBadResponseStatusCode,
		statusCode,
	)
}

func TestShouldRetryWithChannelSetting_NilError(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{}
	require.False(t, shouldRetryWithChannelSetting(c, nil, 3, setting))
}

func TestShouldRetryWithChannelSetting_SkipRetryError(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{}
	err := types.NewErrorWithStatusCode(
		fmt.Errorf("bad request"),
		types.ErrorCodeInvalidRequest,
		400,
		types.ErrOptionWithSkipRetry(),
	)
	require.False(t, shouldRetryWithChannelSetting(c, err, 3, setting))
}

func TestShouldRetryWithChannelSetting_ChannelError(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{}
	err := types.NewErrorWithStatusCode(
		fmt.Errorf("no available key"),
		types.ErrorCodeChannelNoAvailableKey,
		500,
	)
	// Channel errors always retry regardless of retryTimes
	require.True(t, shouldRetryWithChannelSetting(c, err, 0, setting))
}

func TestShouldRetryWithChannelSetting_ZeroRetryTimes(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{}
	err := newRetryError(500)
	require.False(t, shouldRetryWithChannelSetting(c, err, 0, setting))
}

func TestShouldRetryWithChannelSetting_SpecificChannelId(t *testing.T) {
	c := newTestContext()
	c.Set("specific_channel_id", 42)
	setting := dto.ChannelSettings{}
	err := newRetryError(500)
	require.False(t, shouldRetryWithChannelSetting(c, err, 3, setting))
}

func TestShouldRetryWithChannelSetting_SuccessCode(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{}
	err := newRetryError(200)
	require.False(t, shouldRetryWithChannelSetting(c, err, 3, setting))
}

func TestShouldRetryWithChannelSetting_ChannelCodesOverrideGlobal(t *testing.T) {
	// Global default retries 429 and 500-503 etc.
	// Channel configures only 429 → 500 should NOT retry
	c := newTestContext()
	setting := dto.ChannelSettings{
		RetryStatusCodes: "429",
	}

	err429 := newRetryError(429)
	assert.True(t, shouldRetryWithChannelSetting(c, err429, 3, setting), "429 should retry with channel code 429")

	err500 := newRetryError(500)
	assert.False(t, shouldRetryWithChannelSetting(c, err500, 3, setting), "500 should NOT retry when channel only allows 429")
}

func TestShouldRetryWithChannelSetting_ChannelCodesRange(t *testing.T) {
	c := newTestContext()
	setting := dto.ChannelSettings{
		RetryStatusCodes: "500-503",
	}

	assert.True(t, shouldRetryWithChannelSetting(c, newRetryError(500), 3, setting))
	assert.True(t, shouldRetryWithChannelSetting(c, newRetryError(502), 3, setting))
	assert.True(t, shouldRetryWithChannelSetting(c, newRetryError(503), 3, setting))
	assert.False(t, shouldRetryWithChannelSetting(c, newRetryError(429), 3, setting), "429 not in 500-503 range")
	assert.False(t, shouldRetryWithChannelSetting(c, newRetryError(504), 3, setting), "504 not in 500-503 range")
}

func TestShouldRetryWithChannelSetting_EmptyCodesFallbackGlobal(t *testing.T) {
	// When RetryStatusCodes is empty, global rules apply
	c := newTestContext()
	setting := dto.ChannelSettings{
		RetryStatusCodes: "",
	}

	// Global default retries 429
	err429 := newRetryError(429)
	assert.True(t, shouldRetryWithChannelSetting(c, err429, 3, setting), "429 should retry with global rules")

	// Global default retries 500
	err500 := newRetryError(500)
	assert.True(t, shouldRetryWithChannelSetting(c, err500, 3, setting), "500 should retry with global rules")

	// Global default does NOT retry 400
	err400 := newRetryError(400)
	assert.False(t, shouldRetryWithChannelSetting(c, err400, 3, setting), "400 should not retry with global rules")
}

func TestShouldRetryWithChannelSetting_InvalidCodesFallbackGlobal(t *testing.T) {
	// Invalid RetryStatusCodes should fall back to global
	c := newTestContext()
	setting := dto.ChannelSettings{
		RetryStatusCodes: "invalid_codes",
	}

	// Falls back to global: 429 is retryable
	err429 := newRetryError(429)
	assert.True(t, shouldRetryWithChannelSetting(c, err429, 3, setting))
}

func TestShouldRetryWithChannelSetting_AlwaysSkipRetryCode(t *testing.T) {
	// ErrorCodeBadResponseBody is always skip retry
	c := newTestContext()
	setting := dto.ChannelSettings{
		RetryStatusCodes: "500",
	}
	err := types.NewErrorWithStatusCode(
		fmt.Errorf("bad response body"),
		types.ErrorCodeBadResponseBody,
		500,
	)
	require.False(t, shouldRetryWithChannelSetting(c, err, 3, setting))
}

func TestGetChannelRetrySettings_FromContext(t *testing.T) {
	c := newTestContext()
	expected := dto.ChannelSettings{
		RetryTimes:         5,
		RetryOnSameChannel: true,
		RetryStatusCodes:   "429,500",
	}
	c.Set(string(constant.ContextKeyChannelSetting), expected)

	channel := &model.Channel{Id: 1}
	result := getChannelRetrySettings(c, channel)
	assert.Equal(t, expected.RetryTimes, result.RetryTimes)
	assert.Equal(t, expected.RetryOnSameChannel, result.RetryOnSameChannel)
	assert.Equal(t, expected.RetryStatusCodes, result.RetryStatusCodes)
}

func TestGetChannelRetrySettings_FromChannelObject(t *testing.T) {
	c := newTestContext()
	// No setting in context → falls back to channel.GetSetting()
	settingJSON := `{"retry_times":3,"retry_on_same_channel":true,"retry_status_codes":"500-503"}`
	channel := &model.Channel{
		Id:      1,
		Setting: &settingJSON,
	}
	result := getChannelRetrySettings(c, channel)
	assert.Equal(t, 3, result.RetryTimes)
	assert.True(t, result.RetryOnSameChannel)
	assert.Equal(t, "500-503", result.RetryStatusCodes)
}

func TestShouldRetryWithChannelSetting_ExhaustedTransferConfig(t *testing.T) {
	// Verify the RetryExhaustedTransfer field is correctly parsed
	transferFalse := false
	transferTrue := true

	settingNoTransfer := dto.ChannelSettings{
		RetryOnSameChannel:     true,
		RetryExhaustedTransfer: &transferFalse,
	}
	assert.False(t, *settingNoTransfer.RetryExhaustedTransfer)

	settingWithTransfer := dto.ChannelSettings{
		RetryOnSameChannel:     true,
		RetryExhaustedTransfer: &transferTrue,
	}
	assert.True(t, *settingWithTransfer.RetryExhaustedTransfer)

	// nil means default (transfer)
	settingDefault := dto.ChannelSettings{
		RetryOnSameChannel: true,
	}
	assert.Nil(t, settingDefault.RetryExhaustedTransfer)
}

func TestMatchStatusCodeRangesExported(t *testing.T) {
	ranges, err := operation_setting.ParseHTTPStatusCodeRanges("429,500-503")
	require.NoError(t, err)
	require.Len(t, ranges, 2)

	assert.True(t, operation_setting.MatchStatusCodeRanges(ranges, 429))
	assert.True(t, operation_setting.MatchStatusCodeRanges(ranges, 500))
	assert.True(t, operation_setting.MatchStatusCodeRanges(ranges, 502))
	assert.False(t, operation_setting.MatchStatusCodeRanges(ranges, 400))
	assert.False(t, operation_setting.MatchStatusCodeRanges(ranges, 504))
}
