package ali

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAliSamplingOmissionAndMappedImageProtocol(t *testing.T) {
	assert.Nil(t, requestOpenAI2Ali(dto.GeneralOpenAIRequest{}).TopP)
	assert.Equal(t, 0.01, *requestOpenAI2Ali(dto.GeneralOpenAIRequest{TopP: common.GetPointer(0.0)}).TopP)
	assert.Equal(t, 0.99, *requestOpenAI2Ali(dto.GeneralOpenAIRequest{TopP: common.GetPointer(1.0)}).TopP)
	info := &relaycommon.RelayInfo{OriginModelName: "my-image", RelayMode: relayconstant.RelayModeImagesGenerations, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen-image", ChannelBaseUrl: "https://dashscope.example"}}
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	assert.Contains(t, url, "multimodal-generation")
}
