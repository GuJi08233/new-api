package mistral

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestKeepsOnlyMistralFields(t *testing.T) {
	stream := true
	maxOutput := uint(512)
	request := dto.OpenAIResponsesRequest{
		Model:           "mistral-large-latest",
		Input:           json.RawMessage(`"hi"`),
		Instructions:    json.RawMessage(`"be brief"`),
		MaxOutputTokens: &maxOutput,
		Stream:          &stream,
		Reasoning:       &dto.Reasoning{Effort: "high", Summary: "auto"},
		Store:           json.RawMessage(`true`),
		Truncation:      json.RawMessage(`"auto"`),
		Include:         json.RawMessage(`["reasoning.encrypted_content"]`),
		ServiceTier:     "priority",
		Conversation:    json.RawMessage(`null`),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	assert.Equal(t, map[string]any{
		"model":             "mistral-large-latest",
		"input":             "hi",
		"instructions":      "be brief",
		"max_output_tokens": float64(512),
		"stream":            true,
		"reasoning":         map[string]any{"effort": "high"},
	}, got)
}

func TestConvertOpenAIResponsesRequestRejectsStatefulFields(t *testing.T) {
	tests := []struct {
		name    string
		request dto.OpenAIResponsesRequest
	}{
		{name: "previous_response_id", request: dto.OpenAIResponsesRequest{PreviousResponseID: "resp_1"}},
		{name: "conversation", request: dto.OpenAIResponsesRequest{Conversation: json.RawMessage(`"conv_1"`)}},
		{name: "prompt", request: dto.OpenAIResponsesRequest{Prompt: json.RawMessage(`{"id":"pmpt_1"}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.request.Model = "mistral-large-latest"
			_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}, tt.request)
			assert.ErrorContains(t, err, tt.name)
		})
	}
}

func TestNormalizeMistralChatPayloadFlattensContentChunks(t *testing.T) {
	tests := []struct {
		name          string
		payload       string
		stream        bool
		wantContent   any
		wantReasoning any
	}{
		{
			name:          "non-stream thinking and text",
			payload:       `{"id":"x","created":1795071161,"choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"think "},{"type":"text","text":"more"}],"closed":true},{"type":"text","text":"Hello"}]}}]}`,
			wantContent:   "Hello",
			wantReasoning: "think more",
		},
		{
			name:          "stream thinking-only delta omits content",
			payload:       `{"id":"x","choices":[{"index":0,"delta":{"index":0,"content":[{"type":"thinking","thinking":[{"type":"text","text":"The"}],"closed":true}]}}]}`,
			stream:        true,
			wantReasoning: "The",
		},
		{
			name:        "string content is untouched",
			payload:     `{"id":"x","choices":[{"index":0,"delta":{"content":"Hey"}}]}`,
			stream:      true,
			wantContent: "Hey",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Created int64 `json:"created"`
				Choices []struct {
					Message map[string]any `json:"message"`
					Delta   map[string]any `json:"delta"`
				} `json:"choices"`
			}
			require.NoError(t, common.Unmarshal(normalizeMistralChatPayload([]byte(tt.payload), tt.stream), &got))
			require.Len(t, got.Choices, 1)
			message := got.Choices[0].Message
			if tt.stream {
				message = got.Choices[0].Delta
			}
			assert.Equal(t, tt.wantContent, message["content"])
			assert.Equal(t, tt.wantReasoning, message["reasoning_content"])
			if !tt.stream {
				assert.Equal(t, int64(1795071161), got.Created)
			}
		})
	}
}

func TestNormalizeMistralChatBodyRewritesStreamLines(t *testing.T) {
	upstream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"hm\"}]}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	body, err := normalizeMistralChatBody(io.NopCloser(strings.NewReader(upstream)), true)
	require.NoError(t, err)
	defer body.Close()
	out, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hm\"},\"index\":0}]}\n\n"+
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n", string(out))
}
