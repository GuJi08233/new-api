package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveXunfeiWebSocketHeadersUsesSafePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Trace-Id", "trace-123")
	c.Request.Header.Set("Authorization", "Bearer client-credential")
	c.Request.Header.Set("Cookie", "session=gateway-session")
	c.Request.Header.Set("Sec-WebSocket-Protocol", "insecure-client-token")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}

	headers, err := resolveXunfeiWebSocketHeaders(c, info)
	require.NoError(t, err)
	assert.Equal(t, "trace-123", headers.Get("X-Trace-Id"))
	assert.Empty(t, headers.Get("Authorization"))
	assert.Empty(t, headers.Get("Cookie"))
	assert.Empty(t, headers.Get("Sec-WebSocket-Protocol"))
}

func TestXunfeiMakeRequestPassesResolvedWebSocketHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receivedHeaders := make(chan http.Header, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders <- r.Header.Clone()
		responseHeaders := http.Header{
			"X-Upstream-Trace": {"trace-response"},
			"Set-Cookie":       {"session=must-not-forward"},
		}
		conn, err := upgrader.Upgrade(w, r, responseHeaders)
		if err != nil {
			return
		}
		_, _, _ = conn.ReadMessage()
		_ = conn.Close()
	}))
	defer server.Close()

	requestHeaders := http.Header{}
	requestHeaders.Set("X-Trace-Id", "trace-456")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	dataChan, requestDone, responseHeaders, stopRequest, err := xunfeiMakeRequestWithContext(
		context.Background(),
		dto.GeneralOpenAIRequest{Model: "v1.1"},
		"general",
		wsURL,
		"app-id",
		requestHeaders,
	)
	require.NoError(t, err)
	defer stopRequest()
	require.NotNil(t, dataChan)
	assert.Equal(t, "trace-response", responseHeaders.Get("X-Upstream-Trace"))
	assert.Equal(t, "session=must-not-forward", responseHeaders.Get("Set-Cookie"))

	select {
	case headers := <-receivedHeaders:
		assert.Equal(t, "trace-456", headers.Get("X-Trace-Id"))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket handshake")
	}

	select {
	case requestErr := <-requestDone:
		require.Error(t, requestErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket reader to stop")
	}
}

func TestXunfeiMakeRequestReportsNormalCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		response := XunfeiChatResponse{}
		response.Payload.Choices.Status = 2
		response.Payload.Choices.Text = []XunfeiChatResponseTextItem{{Content: "done"}}
		_ = conn.WriteJSON(response)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	dataChan, requestDone, _, stopRequest, err := xunfeiMakeRequestWithContext(
		context.Background(),
		dto.GeneralOpenAIRequest{Model: "v1.1"},
		"general",
		wsURL,
		"app-id",
		nil,
	)
	require.NoError(t, err)
	defer stopRequest()

	select {
	case response := <-dataChan:
		assert.Equal(t, 2, response.Payload.Choices.Status)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket response")
	}
	select {
	case requestErr := <-requestDone:
		require.NoError(t, requestErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for normal websocket completion")
	}
}
