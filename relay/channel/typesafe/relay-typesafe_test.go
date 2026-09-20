package typesafe

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSystemOneTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
	return c, recorder
}

func newSystemOneUpstreamResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// The answers map carries typed probability distributions the gateway has no
// schema for; forwarding it verbatim is the client-facing contract.
func TestSystemOneHandlerForwardsBodyVerbatim(t *testing.T) {
	c, recorder := newSystemOneTestContext(t)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	body := `{"model":"jev-1.13.0","answers":{"is_urgent":{"type":"noul","noul":0.92}},"usage":{"input_tokens":312,"output_tokens":48}}`

	usage, apiErr := SystemOneHandler(c, info, newSystemOneUpstreamResponse(body))

	require.Nil(t, apiErr)
	assert.JSONEq(t, body, recorder.Body.String())
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 312, usage.PromptTokens)
	assert.Equal(t, 48, usage.CompletionTokens)
	assert.Equal(t, 360, usage.TotalTokens)
}

// An upstream that reports no usage must not turn the request into a free one.
func TestSystemOneHandlerFallsBackToEstimateWhenUsageMissing(t *testing.T) {
	c, _ := newSystemOneTestContext(t)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.SetEstimatePromptTokens(128)
	body := `{"model":"jev-1.13.0","answers":{"is_urgent":{"type":"noul","noul":0.92}}}`

	usage, apiErr := SystemOneHandler(c, info, newSystemOneUpstreamResponse(body))

	require.Nil(t, apiErr)
	assert.Equal(t, 128, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
	assert.Equal(t, 128, usage.TotalTokens)
}

// Upstream token counts are not trusted: a negative count would otherwise flow
// into quota math as a credit.
func TestSystemOneHandlerClampsNegativeUpstreamUsage(t *testing.T) {
	c, _ := newSystemOneTestContext(t)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.SetEstimatePromptTokens(7)
	body := `{"model":"jev-1.13.0","usage":{"input_tokens":-5000,"output_tokens":-1}}`

	usage, apiErr := SystemOneHandler(c, info, newSystemOneUpstreamResponse(body))

	require.Nil(t, apiErr)
	assert.Equal(t, 7, usage.PromptTokens)
	assert.Equal(t, 0, usage.CompletionTokens)
	assert.Equal(t, 7, usage.TotalTokens)
}

func TestSystemOneHandlerRejectsNonJSONBody(t *testing.T) {
	c, _ := newSystemOneTestContext(t)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	usage, apiErr := SystemOneHandler(c, info, newSystemOneUpstreamResponse("<html>gateway timeout</html>"))

	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
}
