package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestKeepaliveDoesNotCancelReturnedResponseBody(t *testing.T) {
	service.InitHttpClient()
	settings := operation_setting.GetGeneralSetting()
	oldEnabled, oldInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 60
	t.Cleanup(func() { settings.PingIntervalEnabled, settings.PingIntervalSeconds = oldEnabled, oldInterval })
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: hello\n\n")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, nil)
	require.NoError(t, err)
	response, err := doRequest(c, request, &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	releaseOnce.Do(func() { close(release) })
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "data: hello\n\n", string(data))
}
