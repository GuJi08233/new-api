package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRequestBodyReplaysCompletePayload(t *testing.T) {
	payload := []byte(`{"model":"test","messages":[]}`)
	storage, err := common.CreateBodyStorage(payload)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	for _, body := range []io.Reader{common.ReaderOnly(storage), storage, bytes.NewReader(payload)} {
		_, err := storage.Seek(0, io.SeekStart)
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPost, "https://upstream.invalid/v1/chat/completions", body)
		require.NoError(t, err)
		ApplyUpstreamBodyMetadata(req, body)
		assert.Equal(t, int64(len(payload)), req.ContentLength)
		require.NotNil(t, req.GetBody)
		_, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		require.NoError(t, req.Body.Close())
		for range 2 {
			replay, err := req.GetBody()
			require.NoError(t, err)
			actual, err := io.ReadAll(replay)
			require.NoError(t, err)
			assert.Equal(t, payload, actual)
			require.NoError(t, replay.Close())
		}
	}
}

func TestRelayReturnsRedirectWithoutReplayingBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	followed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			followed = true
			return
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, bytes.NewReader([]byte("private request")))
	require.NoError(t, err)
	resp, err := DoRequest(c, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.False(t, followed)
	assert.NotNil(t, service.GetHttpClient().CheckRedirect)
}
