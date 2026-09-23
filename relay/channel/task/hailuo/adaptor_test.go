package hailuo

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预扣费按 info.UpstreamModelName 计价，metadata.model 不能把出站模型换成更贵的型号。
func TestConvertToRequestPayloadKeepsPricedModel(t *testing.T) {
	req := &relaycommon.TaskSubmitReq{
		Prompt:   "a cat",
		Metadata: map[string]any{"model": "MiniMax-Hailuo-02", "prompt_optimizer": false},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "T2V-01"}}

	payload, err := (&TaskAdaptor{}).convertToRequestPayload(req, info)

	require.NoError(t, err)
	assert.Equal(t, "T2V-01", payload.Model)
	require.NotNil(t, payload.PromptOptimizer, "其余 metadata 字段仍应透传")
	assert.False(t, *payload.PromptOptimizer)
}
