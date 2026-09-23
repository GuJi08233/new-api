package kling

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Kling 上游按 model_name 选模型；预扣费按 info.UpstreamModelName 计价，
// metadata.model_name 不能把出站模型换成更贵的型号。
func TestConvertToRequestPayloadKeepsPricedModel(t *testing.T) {
	req := &relaycommon.TaskSubmitReq{
		Prompt:   "a cat",
		Metadata: map[string]any{"model_name": "kling-v2-master", "negative_prompt": "blur"},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "kling-v1"}}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(req, info)

	require.NoError(t, err)
	assert.Equal(t, "kling-v1", payload.ModelName)
	assert.Equal(t, "kling-v1", payload.Model)
	assert.Equal(t, "blur", payload.NegativePrompt, "其余 metadata 字段仍应透传")
}
