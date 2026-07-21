package tencent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedTencentRequest struct {
	body    []byte
	request *http.Request
	err     error
}

func newTencentCaptureServer(t *testing.T) (string, <-chan capturedTencentRequest) {
	t.Helper()
	received := make(chan capturedTencentRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		request := r.Clone(r.Context())
		request.Body = nil
		received <- capturedTencentRequest{body: body, request: request, err: err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, received
}

func TestDoRequestPreservesTC3OwnedHeadersAfterOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	serverURL, received := newTencentCaptureServer(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "text/plain")
	c.Request.Header.Set("Accept", "text/event-stream")

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "123|secret-id|secret-key",
		ChannelBaseUrl: serverURL,
		HeadersOverride: map[string]any{
			"Authorization":  "Bearer forged-authorization",
			"Content-Type":   "application/forged",
			"Host":           "forged.example.com",
			"X-TC-Action":    "ForgedAction",
			"X-TC-Timestamp": "1",
			"X-TC-Version":   "1900-01-01",
			"X-Trace-Id":     "trace-123",
			"Accept":         "application/vnd.tencent+json",
		},
	}}

	adaptor := &Adaptor{}
	adaptor.Init(info)
	model := "hunyuan-lite"
	request := TencentChatRequest{
		Model:    &model,
		Messages: []*TencentMessage{{Role: "user", Content: "hello"}},
	}
	body, err := common.Marshal(request)
	require.NoError(t, err)

	response, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
	require.NoError(t, err)
	resp, ok := response.(*http.Response)
	require.True(t, ok)
	require.NoError(t, resp.Body.Close())

	upstream := <-received
	require.NoError(t, upstream.err)
	assert.Equal(t, tencentAPIHost, upstream.request.Host)
	assert.Equal(t, "application/json", upstream.request.Header.Get("Content-Type"))
	assert.Equal(t, adaptor.Sign, upstream.request.Header.Get("Authorization"))
	assert.Contains(t, upstream.request.Header.Get("Authorization"), "Credential=secret-id/")
	assert.Equal(t, adaptor.Action, upstream.request.Header.Get("X-TC-Action"))
	assert.Equal(t, adaptor.Version, upstream.request.Header.Get("X-TC-Version"))
	assert.Equal(t, strconv.FormatInt(adaptor.Timestamp, 10), upstream.request.Header.Get("X-TC-Timestamp"))
	assert.Equal(t, "trace-123", upstream.request.Header.Get("X-Trace-Id"))
	assert.Equal(t, "application/vnd.tencent+json", upstream.request.Header.Get("Accept"))
}

func TestDoRequestSignsBodyAfterParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	serverURL, received := newTencentCaptureServer(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "123|secret-id|secret-key",
		ChannelBaseUrl: serverURL,
		ParamOverride: map[string]any{
			"Temperature": 0.25,
		},
	}}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	temperature := 0.8
	request := &dto.GeneralOpenAIRequest{
		Model:       "hunyuan-lite",
		Messages:    []dto.Message{{Role: "user", Content: "hello"}},
		Temperature: &temperature,
	}
	converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	initialBody, err := common.Marshal(converted)
	require.NoError(t, err)
	finalBody, err := relaycommon.ApplyParamOverrideWithRelayInfo(initialBody, info)
	require.NoError(t, err)
	require.NotEqual(t, string(initialBody), string(finalBody))

	response, err := adaptor.DoRequest(c, info, strings.NewReader(string(finalBody)))
	require.NoError(t, err)
	resp, ok := response.(*http.Response)
	require.True(t, ok)
	require.NoError(t, resp.Body.Close())

	upstream := <-received
	require.NoError(t, upstream.err)
	assert.JSONEq(t, string(finalBody), string(upstream.body))
	expectedFinalSign, err := getTencentSign(upstream.request, upstream.body, "secret-id", "secret-key")
	require.NoError(t, err)
	expectedInitialSign, err := getTencentSign(upstream.request, initialBody, "secret-id", "secret-key")
	require.NoError(t, err)
	assert.Equal(t, expectedFinalSign, upstream.request.Header.Get("Authorization"))
	assert.NotEqual(t, expectedInitialSign, upstream.request.Header.Get("Authorization"))
}

func TestDoRequestSignsPassthroughBodyWithoutConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	serverURL, received := newTencentCaptureServer(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "123|secret-id|secret-key",
		ChannelBaseUrl: serverURL,
	}}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	passthroughBody := []byte(`{"Model":"hunyuan-lite","Messages":[{"Role":"user","Content":"passthrough"}]}`)

	response, err := adaptor.DoRequest(c, info, strings.NewReader(string(passthroughBody)))
	require.NoError(t, err)
	resp, ok := response.(*http.Response)
	require.True(t, ok)
	require.NoError(t, resp.Body.Close())

	upstream := <-received
	require.NoError(t, upstream.err)
	assert.Equal(t, passthroughBody, upstream.body)
	expectedSign, err := getTencentSign(upstream.request, passthroughBody, "secret-id", "secret-key")
	require.NoError(t, err)
	assert.Equal(t, expectedSign, upstream.request.Header.Get("Authorization"))
	assert.NotEmpty(t, upstream.request.Header.Get("Authorization"))
}
