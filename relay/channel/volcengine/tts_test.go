package volcengine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTTSWebSocketHeadersUsesSafePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	c.Request.Header.Set("X-Trace-Id", "trace-tts")
	c.Request.Header.Set("Authorization", "Bearer client-credential")
	c.Request.Header.Set("Cookie", "session=gateway-session")
	c.Request.Header.Set("Sec-WebSocket-Protocol", "insecure-client-token")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}

	headers, err := buildTTSWebSocketHeaders(c, info, "provider-token")
	require.NoError(t, err)
	assert.Equal(t, "trace-tts", headers.Get("X-Trace-Id"))
	assert.Equal(t, "Bearer;provider-token", headers.Get("Authorization"))
	assert.Empty(t, headers.Get("Cookie"))
	assert.Empty(t, headers.Get("Sec-WebSocket-Protocol"))
}
