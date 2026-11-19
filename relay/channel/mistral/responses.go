package mistral

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

// requestResponses2Mistral keeps only the fields documented for Mistral's
// stateless POST /v1/responses: https://docs.mistral.ai/api/endpoint/responses
// store, truncation, top_logprobs, background and moderation have a single
// accepted value upstream, so they are omitted and the upstream default applies.
func requestResponses2Mistral(request dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, error) {
	if strings.TrimSpace(request.PreviousResponseID) != "" {
		return nil, errors.New("mistral responses API is stateless: previous_response_id is not supported, send prior output items in input instead")
	}
	if isJSONValuePresent(request.Conversation) {
		return nil, errors.New("mistral responses API is stateless: conversation is not supported, send history in input instead")
	}
	if isJSONValuePresent(request.Prompt) {
		return nil, errors.New("mistral responses API does not support prompt templates: send the prompt in input and instructions")
	}

	out := &dto.OpenAIResponsesRequest{
		Model:             request.Model,
		Input:             request.Input,
		Instructions:      request.Instructions,
		MaxOutputTokens:   request.MaxOutputTokens,
		MaxToolCalls:      request.MaxToolCalls,
		Metadata:          request.Metadata,
		ParallelToolCalls: request.ParallelToolCalls,
		PromptCacheKey:    request.PromptCacheKey,
		Stream:            request.Stream,
		Temperature:       request.Temperature,
		Text:              request.Text,
		ToolChoice:        request.ToolChoice,
		Tools:             request.Tools,
		TopP:              request.TopP,
	}
	if request.Reasoning != nil && request.Reasoning.Effort != "" {
		out.Reasoning = &dto.Reasoning{Effort: request.Reasoning.Effort}
	}
	return out, nil
}

func isJSONValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
