package common_handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionsHandlerNativeAnswersAndInputUsage(t *testing.T) {
	for _, tt := range []struct {
		name   string
		format types.RelayFormat
		body   string
		cached int
		write  int
	}{
		{"gemini probabilities", types.RelayFormatGeminiDecisions, `{"answers":{"q":{"type":"noul","noul":0.01,"confidence":0.99}},"usage":{"total_input_tokens":1000,"input_tokens_by_modality":[{"modality":"text","tokens":1000}],"total_output_tokens":0,"total_tokens":1000}}`, 0, 0},
		{"openai refusal and fractional score", types.RelayFormatOpenAIDecisions, `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"q1","probability":0},{"type":"refusal","name":"q2"},{"type":"score","name":"q3","score":0.75}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":200,"cache_write_tokens":100},"output_tokens":0,"total_tokens":1000}}`, 200, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/decisions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: tt.format, ChannelMeta: &relaycommon.ChannelMeta{}}
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tt.body))}
			usage, apiErr := DecisionsHandler(c, info, response)
			require.Nil(t, apiErr)
			assert.Equal(t, tt.body, recorder.Body.String())
			assert.Equal(t, 1000, usage.PromptTokens)
			assert.Zero(t, usage.CompletionTokens)
			assert.Equal(t, 1000, usage.TotalTokens)
			assert.Equal(t, tt.cached, usage.PromptTokensDetails.CachedTokens)
			assert.Equal(t, tt.write, usage.PromptTokensDetails.CacheWriteTokens)
		})
	}
}

func TestDecisionsHandlerBoundsUntrustedUsage(t *testing.T) {
	for _, tt := range []struct {
		name  string
		body  string
		want  int
		clamp bool
	}{
		{"missing usage", `{"answers":[]}`, 123, false},
		{"negative usage", `{"answers":[],"usage":{"input_tokens":-50,"input_tokens_details":{"cached_tokens":-20,"cache_write_tokens":-10}}}`, 123, false},
		{"oversized usage", `{"answers":[],"usage":{"input_tokens":1e30}}`, common.MaxQuota, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIDecisions, ChannelMeta: &relaycommon.ChannelMeta{}}
			info.SetEstimatePromptTokens(123)
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tt.body))}
			usage, apiErr := DecisionsHandler(c, info, response)
			require.Nil(t, apiErr)
			assert.Equal(t, tt.want, usage.PromptTokens)
			assert.Zero(t, usage.CompletionTokens)
			assert.Zero(t, usage.PromptTokensDetails.CachedTokens)
			assert.Zero(t, usage.PromptTokensDetails.CacheWriteTokens)
			assert.Equal(t, tt.clamp, info.QuotaClamp != nil)
		})
	}
}

func TestDecisionsHandlerRejectsMalformedResponse(t *testing.T) {
	for _, body := range []string{`<html>upstream error</html>`, `{"error":{"message":"failed"}}`, `{"answers":{}}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", nil)
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIDecisions, ChannelMeta: &relaycommon.ChannelMeta{}}
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		usage, apiErr := DecisionsHandler(c, info, response)
		require.NotNil(t, apiErr)
		assert.Nil(t, usage)
		assert.False(t, c.Writer.Written())
	}
}
