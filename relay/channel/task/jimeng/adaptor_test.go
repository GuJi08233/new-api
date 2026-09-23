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

func TestSignRequestUsesOverriddenRequestHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://original.example.com/?Action=CVSync2AsyncSubmitTask", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Host = "override.example.com"
	req.Header.Set("Content-Type", "application/json")

	adaptor := &TaskAdaptor{}
	require.NoError(t, adaptor.signRequest(req, "access-key", "secret-key"))
	assert.Equal(t, "override.example.com", req.Host)
	assert.Equal(t, "override.example.com", req.Header.Get("Host"))
	assert.Contains(t, req.Header.Get("Authorization"), "SignedHeaders=content-type;host;x-content-sha256;x-date")
}

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
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "access-key|secret-key",
		ChannelBaseUrl: server.URL,
		HeadersOverride: map[string]any{
			"Authorization":    "Bearer forged-authorization",
			"Host":             "signed-task.example.com",
			"X-Content-Sha256": "forged-payload-hash",
			"X-Date":           "19700101T000000Z",
			"X-Trace-Id":       "trace-task",
		},
	}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	body := `{"req_key":"jimeng_vgfm_t2v_l20"}`

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	payloadHash := sha256.Sum256([]byte(body))
	upstream := <-received
	assert.Equal(t, "signed-task.example.com", upstream.host)
	assert.Equal(t, hex.EncodeToString(payloadHash[:]), upstream.header.Get("X-Content-Sha256"))
	assert.NotEqual(t, "19700101T000000Z", upstream.header.Get("X-Date"))
	assert.NotEqual(t, "Bearer forged-authorization", upstream.header.Get("Authorization"))
	assert.Contains(t, upstream.header.Get("Authorization"), "SignedHeaders=content-type;host;x-content-sha256;x-date")
	assert.Equal(t, "trace-task", upstream.header.Get("X-Trace-Id"))
}

func TestDoRequestNewAPIRelayKeepsExplicitAuthorizationOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:         "sk-relay-key",
		ChannelBaseUrl: server.URL,
		HeadersOverride: map[string]any{
			"Authorization": "Bearer explicit-override",
		},
	}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"req_key":"jimeng_vgfm_t2v_l20"}`))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "Bearer explicit-override", (<-received).Get("Authorization"))
}

// req_key 就是即梦的模型字段；预扣费按 info.UpstreamModelName 计价，metadata.req_key
// 不能把出站模型换成更贵的型号，3.0 的 req_key 转换也必须基于计价的那个模型。
func TestConvertToRequestPayloadKeepsPricedReqKey(t *testing.T) {
	tests := []struct {
		name        string
		pricedModel string
		wantReqKey  string
	}{
		{"普通模型", "jimeng_vgfm_t2v_l20", "jimeng_vgfm_t2v_l20"},
		{"3.0 文生视频不会被换成 pro", "jimeng_v30", "jimeng_t2v_v30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &relaycommon.TaskSubmitReq{
				Prompt:   "a cat",
				Metadata: map[string]any{"req_key": "jimeng_v30_pro", "seed": 7},
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.pricedModel}}

			payload, err := (&TaskAdaptor{}).convertToRequestPayload(req, info)

			require.NoError(t, err)
			assert.Equal(t, tc.wantReqKey, payload.ReqKey)
			assert.Equal(t, int64(7), payload.Seed, "其余 metadata 字段仍应透传")
		})
	}
}
