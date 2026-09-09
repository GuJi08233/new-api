package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRewriteContext(t *testing.T, upstreamModel string, requestModel string, rewriteEnabled bool) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)

	settings := model_setting.GetGlobalSettings()
	original := settings.RewriteResponseModelEnabled
	t.Cleanup(func() { settings.RewriteResponseModelEnabled = original })
	settings.RewriteResponseModelEnabled = rewriteEnabled

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetModelRewrite(c, nil, upstreamModel, requestModel)
	return c
}

// TestRewriteResponseModelNameCoversEveryRelayFormat pins the response-side
// contract: with the option on, every relay format hands the client back the
// model name it asked for, and nothing else in the payload moves.
func TestRewriteResponseModelNameCoversEveryRelayFormat(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "openai stream chunk",
			body: `{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"delta":{"content":"hi"}}]}`,
			want: `{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"delta":{"content":"hi"}}]}`,
		},
		{
			name: "claude message_start",
			body: `{"type":"message_start","message":{"id":"msg_1","model":"gpt-4o","role":"assistant"}}`,
			want: `{"type":"message_start","message":{"id":"msg_1","model":"gpt-4","role":"assistant"}}`,
		},
		{
			name: "responses stream event",
			body: `{"type":"response.created","response":{"id":"resp_1","model":"gpt-4o"}}`,
			want: `{"type":"response.created","response":{"id":"resp_1","model":"gpt-4"}}`,
		},
		{
			name: "gemini modelVersion",
			body: `{"candidates":[],"modelVersion":"gpt-4o"}`,
			want: `{"candidates":[],"modelVersion":"gpt-4"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRewriteContext(t, "gpt-4o", "gpt-4", true)

			got, changed := RewriteResponseModelName(c, []byte(tt.body))

			assert.True(t, changed)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

// TestRewriteResponseModelNameLeavesPayloadAlone covers the cases where the
// upstream bytes must reach the client untouched, including the ones a naive
// string replacement would corrupt.
func TestRewriteResponseModelNameLeavesPayloadAlone(t *testing.T) {
	const openAIChunk = `{"model":"gpt-4o","choices":[]}`

	tests := []struct {
		name           string
		upstreamModel  string
		requestModel   string
		rewriteEnabled bool
		body           string
	}{
		{
			name:           "option disabled",
			upstreamModel:  "gpt-4o",
			requestModel:   "gpt-4",
			rewriteEnabled: false,
			body:           openAIChunk,
		},
		{
			name:           "no redirect happened",
			upstreamModel:  "",
			requestModel:   "",
			rewriteEnabled: true,
			body:           openAIChunk,
		},
		{
			name:           "redirect target equals request model",
			upstreamModel:  "gpt-4",
			requestModel:   "gpt-4",
			rewriteEnabled: true,
			body:           `{"model":"gpt-4","choices":[]}`,
		},
		{
			name:           "model only appears inside message content",
			upstreamModel:  "gpt-4o",
			requestModel:   "gpt-4",
			rewriteEnabled: true,
			body:           `{"object":"list","data":[{"text":"I am gpt-4o"}]}`,
		},
		{
			name:           "no model path in payload",
			upstreamModel:  "gpt-4o",
			requestModel:   "gpt-4",
			rewriteEnabled: true,
			body:           `{"type":"content_block_delta","delta":{"text":"hi"}}`,
		},
		{
			name:           "binary payload",
			upstreamModel:  "gpt-4o",
			requestModel:   "gpt-4",
			rewriteEnabled: true,
			body:           "ID3\x04gpt-4o\x00audio",
		},
		{
			name:           "sse done frame",
			upstreamModel:  "gpt-4o",
			requestModel:   "gpt-4",
			rewriteEnabled: true,
			body:           "[DONE]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRewriteContext(t, tt.upstreamModel, tt.requestModel, tt.rewriteEnabled)

			got, changed := RewriteResponseModelName(c, []byte(tt.body))

			assert.False(t, changed)
			assert.Equal(t, tt.body, string(got))
			assert.Equal(t, tt.body, RewriteResponseModelNameString(c, tt.body))
		})
	}
}

// TestRewriteResponseModelNameIgnoresUpstreamNameDrift is the regression for the
// case the rewrite exists for: adaptors keep normalizing info.UpstreamModelName
// after ModelMappedHelper snapshots it (stripping -thinking, reasoning-effort
// suffixes, Claude overwriting it from message_start), and upstreams answer with
// their own dated variant. The response name therefore rarely equals the
// snapshot, and matching on it would silently skip exactly these requests.
func TestRewriteResponseModelNameIgnoresUpstreamNameDrift(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "adaptor stripped the thinking suffix",
			body: `{"model":"gemini-2.5-pro","choices":[]}`,
			want: `{"model":"gpt-4","choices":[]}`,
		},
		{
			name: "upstream answered with its dated variant",
			body: `{"model":"gpt-4o-2024-08-06","choices":[]}`,
			want: `{"model":"gpt-4","choices":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Snapshot taken at mapping time; neither response carries it.
			c := newRewriteContext(t, "gemini-2.5-pro-thinking", "gpt-4", true)

			got, changed := RewriteResponseModelName(c, []byte(tt.body))

			assert.True(t, changed)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

// TestMaskUpstreamModelInTextFollowsHideOption verifies user-facing error text
// only loses the upstream model name while the option is on.
func TestMaskUpstreamModelInTextFollowsHideOption(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })

	const upstreamError = "The model `gpt-4o-2024-08-06` does not exist"
	c := newRewriteContext(t, "gpt-4o-2024-08-06", "gpt-4", false)

	common.SetHideModelMappingForUser(true)
	assert.Equal(t, "The model `gpt-4` does not exist", MaskUpstreamModelInText(c, upstreamError))

	common.SetHideModelMappingForUser(false)
	assert.Equal(t, upstreamError, MaskUpstreamModelInText(c, upstreamError))
}

// TestSetModelRewriteIgnoresNonRedirects makes sure the context marker only
// exists for a real redirect, which is the short-circuit every rewrite path
// relies on to stay free when no mapping is configured.
func TestSetModelRewriteIgnoresNonRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	SetModelRewrite(c, nil, "gpt-4", "gpt-4")
	_, ok := GetModelRewrite(c)
	require.False(t, ok)

	SetModelRewrite(c, nil, "gpt-4o", "")
	_, ok = GetModelRewrite(c)
	require.False(t, ok)

	SetModelRewrite(c, nil, "gpt-4o", "gpt-4")
	rewrite, ok := GetModelRewrite(c)
	require.True(t, ok)
	assert.Equal(t, ModelRewrite{Upstream: "gpt-4o", Request: "gpt-4"}, rewrite)
}

// TestModelRewriteDoesNotSurviveChannelRetry guards the retry path: once the
// request moves to a channel that does not redirect, the previous channel's
// mapping must be gone, otherwise its upstream name would be rewritten out of
// a response that never went through that channel.
func TestModelRewriteDoesNotSurviveChannelRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	SetModelRewrite(c, nil, "gpt-4o", "gpt-4")
	_, ok := GetModelRewrite(c)
	require.True(t, ok)

	// Second channel serves the requested model directly.
	SetModelRewrite(c, nil, "gpt-4", "gpt-4")

	_, ok = GetModelRewrite(c)
	assert.False(t, ok)
}

// TestMaskUpstreamModelInTextCoversNormalizedUpstreamName guards the drift between
// the name recorded at mapping time and the name actually sent upstream: adaptors
// strip -thinking/-nothinking or reasoning-effort suffixes afterwards, and upstream
// error text names the stripped form. Both spellings must be masked, and the error
// log must record the same upstream name the consume log does.
func TestMaskUpstreamModelInTextCoversNormalizedUpstreamName(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })
	common.SetHideModelMappingForUser(true)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash-nothinking"}}
	SetModelRewrite(c, info, info.UpstreamModelName, "my-flash")
	// The Gemini adaptor strips the suffix after the mapping was recorded.
	info.UpstreamModelName = "gemini-2.5-flash"

	text := "models/gemini-2.5-flash is not found; channel target gemini-2.5-flash-nothinking rejected"
	assert.Equal(t, "models/my-flash is not found; channel target my-flash rejected", MaskUpstreamModelInText(c, text))

	rewrite, ok := GetModelRewrite(c)
	require.True(t, ok)
	assert.Equal(t, "gemini-2.5-flash", rewrite.UpstreamModelName())
}

// TestHideTaskModelMappingScrubsUserFacingTask covers every field of a user-side
// task DTO that can carry the redirect: the properties marker, the failure reason
// and the raw upstream submit response, while the stored task keeps the real name
// for admins.
func TestHideTaskModelMappingScrubsUserFacingTask(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })
	common.SetHideModelMappingForUser(true)

	task := &model.Task{
		FailReason: "upstream sora-2-pro rejected the prompt",
		Data:       []byte(`{"id":"video_1","model":"sora-2-pro","status":"failed"}`),
	}
	task.Properties.UpstreamModelName = "sora-2-pro"
	task.Properties.OriginModelName = "my-video"
	taskDto := &dto.TaskDto{FailReason: task.FailReason, Properties: task.Properties, Data: task.Data}

	HideTaskModelMapping(taskDto, task)

	properties, ok := taskDto.Properties.(model.Properties)
	require.True(t, ok)
	assert.Empty(t, properties.UpstreamModelName)
	assert.Equal(t, "my-video", properties.OriginModelName)
	assert.Equal(t, "upstream my-video rejected the prompt", taskDto.FailReason)
	assert.JSONEq(t, `{"id":"video_1","model":"my-video","status":"failed"}`, string(taskDto.Data))
	assert.Equal(t, "sora-2-pro", task.Properties.UpstreamModelName)
	assert.Contains(t, string(task.Data), "sora-2-pro")
}

// TestHideModelMappingInTaskBodyLeavesBodyAlone pins when the OpenAI-video fetch
// body must pass through untouched: option off, or no redirect recorded on the task.
func TestHideModelMappingInTaskBodyLeavesBodyAlone(t *testing.T) {
	original := common.IsHideModelMappingForUserEnabled()
	t.Cleanup(func() { common.SetHideModelMappingForUser(original) })

	const body = `{"id":"video_1","model":"sora-2-pro"}`
	redirected := &model.Task{}
	redirected.Properties.UpstreamModelName = "sora-2-pro"
	redirected.Properties.OriginModelName = "my-video"
	direct := &model.Task{}
	direct.Properties.UpstreamModelName = "sora-2-pro"
	direct.Properties.OriginModelName = "sora-2-pro"

	common.SetHideModelMappingForUser(false)
	assert.Equal(t, body, string(HideModelMappingInTaskBody(redirected, []byte(body))))

	common.SetHideModelMappingForUser(true)
	assert.Equal(t, body, string(HideModelMappingInTaskBody(direct, []byte(body))))
	assert.JSONEq(t, `{"id":"video_1","model":"my-video"}`, string(HideModelMappingInTaskBody(redirected, []byte(body))))
}
