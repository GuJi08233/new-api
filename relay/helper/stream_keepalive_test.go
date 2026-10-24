package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestKeepaliveSurvivesNestedAttemptsAndHandsOffBeforeStream(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	stop := StartStreamKeepAlive(c, time.Hour)
	defer stop()
	writer, ok := c.Writer.(*streamKeepAliveWriter)
	require.True(t, ok)
	require.NoError(t, writer.ping())
	assert.Contains(t, recorder.Body.String(), "PING")
	nestedStop := StartStreamKeepAlive(c, time.Hour)
	nestedStop()
	select {
	case <-writer.done:
		t.Fatal("an attempt cleanup stopped the request keepalive")
	default:
	}
	require.NoError(t, writer.ping())
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}
	StreamScannerHandler(c, response, info, func(string, *StreamResult) {})
	select {
	case <-writer.done:
	default:
		t.Fatal("request keepalive did not stop before response handling")
	}
	assert.Equal(t, 2, strings.Count(recorder.Body.String(), "PING"))
}
