package ali

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWan27TextProtocolUsesResolutionAndPreservesZeroOptions(t *testing.T) {
	a := &TaskAdaptor{}
	info := testRelayInfo()
	info.IsModelMapped = true
	info.UpstreamModelName = "wan2.7-t2v"
	req := relaycommon.TaskSubmitReq{Model: "custom-video", Prompt: "test", Size: "720P", Metadata: map[string]any{
		"parameters": map[string]any{"prompt_extend": false, "watermark": false, "seed": 0, "ratio": "9:16"},
	}}
	converted, err := a.convertToAliRequest(info, req)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, common.Unmarshal(data, &wire))
	params := wire["parameters"].(map[string]any)
	assert.Equal(t, "720P", params["resolution"])
	assert.Equal(t, "9:16", params["ratio"])
	assert.NotContains(t, params, "size")
	assert.Equal(t, false, params["prompt_extend"])
	assert.Equal(t, false, params["watermark"])
	assert.Equal(t, float64(0), params["seed"])
}

func TestWanFrameAndSpeechProtocol(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://upstream.invalid"}
	info := testRelayInfo()
	info.UpstreamModelName = "wan2.2-kf2v-flash"
	url, err := a.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Contains(t, url, "/image2video/video-synthesis")
	converted, err := a.convertToAliRequest(info, relaycommon.TaskSubmitReq{Model: info.UpstreamModelName, Image: "first", Images: []string{"first", "last"}})
	require.NoError(t, err)
	assert.Equal(t, "first", converted.Input.FirstFrameURL)
	assert.Equal(t, "last", converted.Input.LastFrameURL)
	assert.Empty(t, converted.Input.ImgURL)
	info.UpstreamModelName = "wan2.2-s2v"
	converted, err = a.convertToAliRequest(info, relaycommon.TaskSubmitReq{Model: info.UpstreamModelName, Image: "image", Metadata: map[string]any{"input": map[string]any{"audio_url": "audio"}}})
	require.NoError(t, err)
	assert.Equal(t, "image", converted.Input.ImageURL)
	assert.Nil(t, converted.Parameters.Duration)
	assert.Empty(t, converted.Input.ImgURL)
	assert.Equal(t, 50, a.AdjustBillingOnComplete(&model.Task{Quota: 200, Properties: model.Properties{UpstreamModelName: info.UpstreamModelName}, Data: []byte(`{"usage":{"duration":5}}`)}, nil))
}

func TestWanRejectsInvalidMetadataDurationBeforeSubmission(t *testing.T) {
	_, err := (&TaskAdaptor{}).convertToAliRequest(testRelayInfo(), relaycommon.TaskSubmitReq{Model: "wan2.7-t2v", Prompt: "test", Metadata: map[string]any{"parameters": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1}}})
	require.Error(t, err)
	_, err = (&TaskAdaptor{}).convertToAliRequest(testRelayInfo(), relaycommon.TaskSubmitReq{Model: "wan2.7-t2v", Prompt: "test", Metadata: map[string]any{"parameters": nil}})
	require.Error(t, err)
}
