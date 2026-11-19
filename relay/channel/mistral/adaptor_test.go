package mistral

import (
	"encoding/json"
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
