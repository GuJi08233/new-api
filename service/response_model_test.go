package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseModelLogPreservesUpstreamIDBeforeRewriting(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		model   string
		mapped  bool
		rewrite bool
	}{
		{name: "same as requested without mapping", body: `{"model":"client-model","choices":[]}`, model: "client-model"},
		{name: "versioned chat model without mapping", body: `{"model":"actual-version","choices":[]}`, model: "actual-version"},
		{name: "mapped chat with rewriting enabled", body: `{"model":"actual-version","choices":[]}`, model: "actual-version", mapped: true, rewrite: true},
		{name: "mapped chat with rewriting disabled", body: `{"model":"actual-version","choices":[]}`, model: "actual-version", mapped: true},
		{name: "claude message start", body: `{"type":"message_start","message":{"model":"claude-version"}}`, model: "claude-version", mapped: true, rewrite: true},
		{name: "responses event", body: `{"type":"response.completed","response":{"model":"responses-version"}}`, model: "responses-version", mapped: true, rewrite: true},
		{name: "gemini model at end of response", body: `{"candidates":[],"usageMetadata":{},"modelVersion":"gemini-version"}`, model: "gemini-version", mapped: true, rewrite: true},
		{name: "realtime session", body: `{"type":"session.created","session":{"model":"realtime-version"}}`, model: "realtime-version"},
		{name: "no model does not use mapped model", body: `{"choices":[]}`, mapped: true, rewrite: true},
		{name: "content model is not metadata", body: `{"content":{"model":"user-content"}}`, mapped: true},
		{name: "empty or non string model", body: `{"model":null,"message":{"model":" "}}`},
		{name: "incomplete response", body: `{"model":"incomplete"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := "client-model"
			if tt.mapped {
				upstream = "mapped-model"
			}
			c := newRewriteContext(t, upstream, "client-model", tt.rewrite)
			info := &relaycommon.RelayInfo{
				OriginModelName:   "client-model",
				StartTime:         time.Unix(100, 0),
				FirstResponseTime: time.Unix(100, 0),
				ChannelMeta:       &relaycommon.ChannelMeta{UpstreamModelName: upstream, IsModelMapped: tt.mapped},
			}
			body, err := ReadResponseBody(c, &http.Response{Body: io.NopCloser(strings.NewReader(tt.body))})
			require.NoError(t, err)
			assert.Equal(t, tt.body, string(body))
			RewriteResponseModelName(c, body)
			other := GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 0, 0, 1)
			admin, ok := other["admin_info"].(map[string]interface{})
			require.True(t, ok)
			if tt.model == "" {
				assert.NotContains(t, admin, "response_model_name")
			} else {
				assert.Equal(t, tt.model, admin["response_model_name"])
			}
			assert.NotContains(t, other, "response_model_name")
			assert.Equal(t, "client-model", info.OriginModelName)
			assert.Equal(t, upstream, info.UpstreamModelName)
		})
	}
}

func TestResponseModelLogRetainsModelAcrossUsageEvents(t *testing.T) {
	c := newRewriteContext(t, "mapped-model", "client-model", true)
	CaptureResponseModelName(c, []byte(`{"model":"actual-version","choices":[]}`))
	CaptureResponseModelName(c, []byte(`{"usage":{"total_tokens":10}}`))
	CaptureResponseModelName(c, []byte(`{"model":""}`))
	assert.Equal(t, "actual-version", common.GetContextKeyString(c, constant.ContextKeyResponseModelName))
}
