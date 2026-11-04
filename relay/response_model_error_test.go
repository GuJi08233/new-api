package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextHelperCapturesModelFromUpstreamError(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassthrough := settings.PassThroughRequestEnabled
	previousResponsesPolicy := settings.ChatCompletionsToResponsesPolicy
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy.Enabled = false
	t.Cleanup(func() {
		settings.PassThroughRequestEnabled = previousPassthrough
		settings.ChatCompletionsToResponsesPolicy = previousResponsesPolicy
	})
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"model":"actual-provider-version","error":{"message":"invalid input","type":"invalid_request_error","code":"invalid_input"}}`))
	}))
	t.Cleanup(upstream.Close)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "client-model")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-model",
		RequestURLPath:  "/v1/chat/completions",
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		Request: &dto.GeneralOpenAIRequest{
			Model:    "client-model",
			Messages: []dto.Message{{Role: "user", Content: "hello"}},
		},
	}

	apiErr := TextHelper(c, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "invalid input", apiErr.Error())
	assert.Equal(t, "actual-provider-version", common.GetContextKeyString(c, constant.ContextKeyResponseModelName))
	assert.Equal(t, "client-model", info.OriginModelName)
}
