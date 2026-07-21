package channel

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type websocketHeaderTestAdaptor struct {
	Adaptor
	url string
}

type failingPreResponsePingWriter struct {
	gin.ResponseWriter
	err error
}

func (w *failingPreResponsePingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *failingPreResponsePingWriter) WriteString(string) (int, error) {
	return 0, w.err
}

func (a websocketHeaderTestAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (websocketHeaderTestAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func TestDoRequest_CopiesSafeHeadersBeforeAdaptor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	for _, testCase := range []struct {
		name     string
		isStream bool
		upstream string
	}{
		{name: "declared stream", isStream: true, upstream: "application/json"},
		{name: "detected event stream", isStream: false, upstream: "text/event-stream"},
		{name: "non-stream response", isStream: false, upstream: "application/json"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Upstream-Metadata", "available-before-handler")
				w.Header().Set("Set-Cookie", "session=upstream")
				w.Header().Set("Access-Control-Allow-Origin", "https://upstream.example")
				w.Header().Set("Content-Type", testCase.upstream)
				_, _ = w.Write([]byte("data: [DONE]\n"))
			}))
			defer upstream.Close()

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
			request, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
			require.NoError(t, err)

			info := &relaycommon.RelayInfo{
				IsStream: testCase.isStream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
				},
			}
			resp, err := doRequest(c, request, info)
			require.NoError(t, err)
			require.NotNil(t, resp)
			defer resp.Body.Close()

			assert.Equal(t, "available-before-handler", recorder.Header().Get("X-Upstream-Metadata"))
			assert.Empty(t, recorder.Header().Values("Set-Cookie"))
			assert.Empty(t, recorder.Header().Values("Access-Control-Allow-Origin"))
			assert.NotEqual(t, testCase.upstream, recorder.Header().Get("Content-Type"))
		})
	}
}

func TestDoRequest_AppliesPassthroughAtSharedHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	received := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	c.Request.Header.Set("X-Trace-Id", "trace-123")
	c.Request.Header.Set("Connection", "X-Client-Hop")
	c.Request.Header.Set("X-Client-Hop", "must-not-forward")
	request, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}
	resp, err := doRequest(c, request, info)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	headers := <-received
	assert.Equal(t, "trace-123", headers.Get("X-Trace-Id"))
	assert.Empty(t, headers.Get("X-Client-Hop"))
}

func TestDoRequestPreResponsePingFailureCancelsUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	setting := operation_setting.GetGeneralSetting()
	oldEnabled := setting.PingIntervalEnabled
	oldSeconds := setting.PingIntervalSeconds
	setting.PingIntervalEnabled = true
	setting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		setting.PingIntervalEnabled = oldEnabled
		setting.PingIntervalSeconds = oldSeconds
	})

	requestStarted := make(chan struct{})
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		select {
		case <-r.Context().Done():
			close(upstreamCanceled)
		case <-time.After(3 * time.Second):
			w.WriteHeader(http.StatusGatewayTimeout)
		}
	}))
	defer upstream.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Writer = &failingPreResponsePingWriter{
		ResponseWriter: c.Writer,
		err:            errors.New("downstream write failed"),
	}
	request, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	_, err = doRequest(c, request, info)

	require.Error(t, err)
	select {
	case <-requestStarted:
	default:
		t.Fatal("upstream request did not start")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream request was not canceled after the downstream ping failed")
	}
}

func TestDoWssRequestCopiesSafeHandshakeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseHeaders := http.Header{
			"X-Upstream-Trace": {"trace-websocket"},
			"Set-Cookie":       {"session=must-not-forward"},
		}
		conn, err := upgrader.Upgrade(w, r, responseHeaders)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}
	adaptor := websocketHeaderTestAdaptor{url: "ws" + strings.TrimPrefix(server.URL, "http")}

	conn, err := DoWssRequest(adaptor, c, info, nil)
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, "trace-websocket", recorder.Header().Get("X-Upstream-Trace"))
	assert.Empty(t, recorder.Header().Values("Set-Cookie"))
	assert.Empty(t, recorder.Header().Values("Sec-Websocket-Accept"))
}

func TestDoRequest_ResponseHeaderCopyDoesNotLatchErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.Header().Set("X-Upstream-Metadata", "failed-attempt")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("error"))
			return
		}
		w.Header().Set("X-Upstream-Metadata", "successful-attempt")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}

	firstRequest, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	firstResponse, err := doRequest(c, firstRequest, info)
	require.NoError(t, err)
	require.NotNil(t, firstResponse)
	_ = firstResponse.Body.Close()
	assert.Empty(t, recorder.Header().Get("X-Upstream-Metadata"))

	secondRequest, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	secondResponse, err := doRequest(c, secondRequest, info)
	require.NoError(t, err)
	require.NotNil(t, secondResponse)
	defer secondResponse.Body.Close()
	assert.Equal(t, "successful-attempt", recorder.Header().Get("X-Upstream-Metadata"))
}

func TestDoRequest_ResponseHeaderCopyReplacesPreviousSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("Content-Type", "text/event-stream")
		if attempt == 1 {
			w.Header().Set("X-Old-Metadata", "old")
		} else {
			w.Header().Set("X-New-Metadata", "new")
		}
		_, _ = w.Write([]byte("data: [DONE]\n"))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	info := &relaycommon.RelayInfo{
		IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}

	firstRequest, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	firstResponse, err := doRequest(c, firstRequest, info)
	require.NoError(t, err)
	require.NotNil(t, firstResponse)
	_ = firstResponse.Body.Close()
	assert.Equal(t, "old", recorder.Header().Get("X-Old-Metadata"))

	secondRequest, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	secondResponse, err := doRequest(c, secondRequest, info)
	require.NoError(t, err)
	require.NotNil(t, secondResponse)
	defer secondResponse.Body.Close()
	assert.Empty(t, recorder.Header().Get("X-Old-Metadata"))
	assert.Equal(t, "new", recorder.Header().Get("X-New-Metadata"))
}

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_ChannelPassthroughSkipsUnsafeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	unsafeHeaders := map[string]string{
		"Authorization":            "Bearer client-secret",
		"Mj-Api-Secret":            "gateway-user-token",
		"New-Api-User":             "42",
		"X-Api-Key":                "client-api-key",
		"X-Goog-Api-Key":           "client-google-key",
		"Cookie":                   "session=gateway-session",
		"Cookie2":                  "legacy-session=gateway-session",
		"Proxy-Authorization":      "Basic client-proxy-credential",
		"Proxy-Connection":         "keep-alive",
		"Content-Type":             "multipart/form-data; boundary=client-boundary",
		"Content-Encoding":         "gzip",
		"Accept":                   "application/problem+json",
		"Accept-Encoding":          "gzip, br",
		"Expect":                   "100-continue",
		"Sec-WebSocket-Accept":     "client-handshake-value",
		"Sec-WebSocket-Key":        "client-websocket-key",
		"Sec-WebSocket-Version":    "13",
		"Sec-WebSocket-Extensions": "permessage-deflate",
		"Sec-WebSocket-Protocol":   "openai-insecure-api-key.client-token",
	}
	for name, value := range unsafeHeaders {
		ctx.Request.Header.Set(name, value)
	}

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				PassThroughHeadersEnabled: true,
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	assert.Equal(t, "trace-123", headers["x-trace-id"])
	for name := range unsafeHeaders {
		assert.NotContains(t, headers, strings.ToLower(name), "%s must not be forwarded", name)
	}
}

func TestProcessHeaderOverride_RegexMatchesCanonicalHeaderNamesCaseInsensitively(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Other-Header", "must-not-forward")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				`re:^\Qx-trace-id\E$`: "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	assert.Equal(t, "trace-123", headers["x-trace-id"])
	assert.NotContains(t, headers, "other-header")
}

func TestProcessHeaderOverride_ExplicitEntityHeaderStillWins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=client")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
			HeadersOverride: map[string]any{
				"Content-Type": "application/custom+json",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	assert.Equal(t, "application/custom+json", headers["content-type"])
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
