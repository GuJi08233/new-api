package mistral

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// requestResponses2Mistral keeps only the fields documented for Mistral's
// stateless POST /v1/responses: https://docs.mistral.ai/api/endpoint/responses
// Constrained options are validated before using the upstream defaults.
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
	if isJSONValuePresent(request.Store) && !bytes.Equal(bytes.TrimSpace(request.Store), []byte("false")) {
		return nil, errors.New("mistral responses API is stateless: store must be false or null")
	}
	if isJSONValuePresent(request.Truncation) && !bytes.Equal(bytes.TrimSpace(request.Truncation), []byte(`"disabled"`)) {
		return nil, errors.New("mistral responses API does not support automatic truncation: truncation must be disabled")
	}
	if request.Background != nil && *request.Background {
		return nil, errors.New("mistral responses API does not support background execution")
	}
	if request.TopLogProbs != nil && *request.TopLogProbs != 0 {
		return nil, errors.New("mistral responses API requires top_logprobs to be zero")
	}
	if isJSONValuePresent(request.Moderation) {
		return nil, errors.New("mistral responses API does not support moderation")
	}
	if request.FrequencyPenalty != nil && *request.FrequencyPenalty != 0 {
		return nil, errors.New("mistral responses API does not support frequency_penalty; use chat completions")
	}
	if request.PresencePenalty != nil && *request.PresencePenalty != 0 {
		return nil, errors.New("mistral responses API does not support presence_penalty; use chat completions")
	}

	switch common.GetJsonType(request.Input) {
	case "string":
	case "array":
		var items []map[string]json.RawMessage
		if err := common.Unmarshal(request.Input, &items); err != nil {
			return nil, fmt.Errorf("invalid mistral responses input: %w", err)
		}
		changed := false
		for i, item := range items {
			if isJSONValuePresent(item["type"]) {
				var itemType string
				if err := common.Unmarshal(item["type"], &itemType); err != nil {
					return nil, fmt.Errorf("input[%d].type must be a string", i)
				}
				switch itemType {
				case "message", "reasoning", "function_call", "function_call_output":
				case "item_reference":
					return nil, fmt.Errorf("input[%d]: mistral responses API is stateless; send the full referenced item instead of item_reference", i)
				default:
					return nil, fmt.Errorf("input[%d]: unsupported mistral responses item type %q", i, itemType)
				}
				continue
			}
			if !isJSONValuePresent(item["role"]) || !isJSONValuePresent(item["content"]) {
				return nil, fmt.Errorf("input[%d]: type is required except for messages with role and content", i)
			}
			// Easy messages are allowed, but assistant output_text histories need
			// the discriminator to avoid Mistral's "type None" validation error.
			item["type"] = json.RawMessage(`"message"`)
			changed = true
		}
		if changed {
			input, err := common.Marshal(items)
			if err != nil {
				return nil, fmt.Errorf("failed to encode mistral responses input: %w", err)
			}
			request.Input = input
		}
	default:
		return nil, errors.New("mistral responses input must be a string or an array of complete input items")
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
