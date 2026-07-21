package jimeng

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestDoesNotReapplyOverridesAfterSigning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	type receivedRequest struct {
		header http.Header
		host   string
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- receivedRequest{header: r.Header.Clone(), host: r.Host}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "access-key|secret-key",
		ChannelBaseUrl: server.URL,
		HeadersOverride: map[string]any{
			"Authorization":    "Bearer forged-authorization",
			"Host":             "signed.example.com",
			"X-Content-Sha256": "forged-payload-hash",
			"X-Date":           "19700101T000000Z",
			"X-Trace-Id":       "trace-123",
		},
	}}
	body := `{"prompt":"hello"}`

	response, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(body))
	require.NoError(t, err)
	resp, ok := response.(*http.Response)
	require.True(t, ok)
	require.NoError(t, resp.Body.Close())

	payloadHash := sha256.Sum256([]byte(body))
	upstream := <-received
	assert.Equal(t, "signed.example.com", upstream.host)
	assert.Equal(t, hex.EncodeToString(payloadHash[:]), upstream.header.Get("X-Content-Sha256"))
	assert.NotEqual(t, "19700101T000000Z", upstream.header.Get("X-Date"))
	assert.NotEqual(t, "Bearer forged-authorization", upstream.header.Get("Authorization"))
	assert.Contains(t, upstream.header.Get("Authorization"), "SignedHeaders=content-type;host;x-content-sha256;x-date")
	assert.Equal(t, "trace-123", upstream.header.Get("X-Trace-Id"))
}
