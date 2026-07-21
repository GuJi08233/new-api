package coze

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCozeFollowUpRequestsApplyRuntimeHeaderOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	type capturedRequest struct {
		path   string
		header http.Header
	}
	received := make(chan capturedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- capturedRequest{path: r.URL.Path, header: r.Header.Clone()}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v3/chat/retrieve" {
			_, _ = w.Write([]byte(`{"data":{"status":"completed","usage":{"token_count":3,"input_count":1,"output_count":2}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("coze_conversation_id", "conversation-id")
	c.Set("coze_chat_id", "chat-id")
	info := &relaycommon.RelayInfo{
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"Authorization": "Bearer custom-token",
			"X-Trace-Id":    "trace-coze",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "provider-token",
		},
	}

	err, complete := checkIfChatComplete(&Adaptor{}, c, info)
	require.NoError(t, err)
	assert.True(t, complete)

	resp, err := getChatDetail(&Adaptor{}, c, info)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())

	for range 2 {
		request := <-received
		assert.Contains(t, []string{"/v3/chat/retrieve", "/v3/chat/message/list"}, request.path)
		assert.Equal(t, "Bearer custom-token", request.header.Get("Authorization"))
		assert.Equal(t, "trace-coze", request.header.Get("X-Trace-Id"))
	}
}
