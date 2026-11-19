package mistral

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	randomSeed := request.RandomSeed
	if request.Seed != nil {
		seed := *request.Seed
		if math.IsNaN(seed) || math.IsInf(seed, 0) || math.Trunc(seed) != seed || seed < -1<<63 || seed >= 1<<63 {
			return nil, fmt.Errorf("seed must be an integer in the int64 range")
		}
		if randomSeed != nil && *randomSeed != int64(seed) {
			return nil, fmt.Errorf("seed and random_seed must match when both are supplied")
		}
		randomSeed = common.GetPointer(int64(seed))
	}
	if isJSONValuePresent(request.Functions) || isJSONValuePresent(request.FunctionCall) {
		return nil, fmt.Errorf("mistral chat requires tools/tool_choice instead of legacy functions/function_call")
	}
	tools := append([]dto.ToolCallRequest(nil), request.Tools...)
	for i := range tools {
		if tools[i].Type != "function" {
			return nil, fmt.Errorf("mistral chat relay does not support tool type %q; use function tools", tools[i].Type)
		}
		if tools[i].Function.Name == "" {
			return nil, fmt.Errorf("tools[%d].function.name is required", i)
		}
		if tools[i].Function.Parameters == nil {
			tools[i].Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}
	messages := make([]dto.Message, 0, len(request.Messages))
	idMap := make(map[string]string)
	for _, message := range request.Messages {
		// 1. tool_calls.id
		var toolCalls []dto.ToolCallRequest
		if len(message.ToolCalls) > 0 {
			if err := common.Unmarshal(message.ToolCalls, &toolCalls); err != nil {
				return nil, fmt.Errorf("invalid tool_calls: %w", err)
			}
			for i := range toolCalls {
				if !mistralToolCallIdRegexp.MatchString(toolCalls[i].ID) {
					if newId, ok := idMap[toolCalls[i].ID]; ok {
						toolCalls[i].ID = newId
					} else {
						newId, err := common.GenerateRandomCharsKey(9)
						if err != nil {
							return nil, fmt.Errorf("generate Mistral tool call ID: %w", err)
						}
						idMap[toolCalls[i].ID] = newId
						toolCalls[i].ID = newId
					}
				}
			}
			message.SetToolCalls(toolCalls)
		}

		// 2. tool_call_id
		if message.ToolCallId != "" {
			if newId, ok := idMap[message.ToolCallId]; ok {
				message.ToolCallId = newId
			} else {
				if !mistralToolCallIdRegexp.MatchString(message.ToolCallId) {
					newId, err := common.GenerateRandomCharsKey(9)
					if err != nil {
						return nil, fmt.Errorf("generate Mistral tool call ID: %w", err)
					}
					idMap[message.ToolCallId] = newId
					message.ToolCallId = newId
				}
			}
		}

		if message.Role == "developer" {
			message.Role = "system"
		}
		var chunks []map[string]json.RawMessage
		if message.Content != nil && !message.IsStringContent() {
			content, err := common.Marshal(message.Content)
			if err != nil {
				return nil, err
			}
			if err := common.Unmarshal(content, &chunks); err != nil {
				return nil, fmt.Errorf("invalid Mistral message content: %w", err)
			}
			for i, chunk := range chunks {
				var contentType string
				if err := common.Unmarshal(chunk["type"], &contentType); err != nil {
					return nil, fmt.Errorf("invalid Mistral content type: %w", err)
				}
				if contentType == dto.ContentTypeImageURL && common.GetJsonType(chunk["image_url"]) == "object" {
					var image dto.MessageImageUrl
					if err := common.Unmarshal(chunk["image_url"], &image); err != nil {
						return nil, fmt.Errorf("invalid image_url: %w", err)
					}
					chunk["image_url"], _ = common.Marshal(image.Url)
				}
				if contentType == dto.ContentTypeFile && isJSONValuePresent(chunk["file"]) {
					var file map[string]json.RawMessage
					if err := common.Unmarshal(chunk["file"], &file); err != nil {
						return nil, fmt.Errorf("invalid file: %w", err)
					}
					if isJSONValuePresent(file["file_id"]) {
						chunks[i] = map[string]json.RawMessage{"type": json.RawMessage(`"file"`), "file_id": file["file_id"]}
					} else {
						url := file["file_url"]
						if !isJSONValuePresent(url) {
							url = file["file_data"]
						}
						if !isJSONValuePresent(url) {
							return nil, fmt.Errorf("file requires file_id, file_url or file_data")
						}
						chunks[i] = map[string]json.RawMessage{"type": json.RawMessage(`"document_url"`), "document_url": url}
						if isJSONValuePresent(file["filename"]) {
							chunks[i]["document_name"] = file["filename"]
						}
					}
				}
			}
		}
		if message.Role == "assistant" && message.GetReasoningContent() != "" {
			if message.IsStringContent() && message.StringContent() != "" {
				text, _ := common.Marshal(message.StringContent())
				chunks = append(chunks, map[string]json.RawMessage{"type": json.RawMessage(`"text"`), "text": text})
			}
			thinking, err := common.Marshal([]mistralContentChunk{{Type: "text", Text: message.GetReasoningContent()}})
			if err != nil {
				return nil, err
			}
			chunk := map[string]json.RawMessage{
				"type":     json.RawMessage(`"thinking"`),
				"thinking": thinking,
			}
			chunks = append([]map[string]json.RawMessage{chunk}, chunks...)
		}
		if chunks != nil {
			message.Content = chunks
		}
		messages = append(messages, dto.Message{
			Role:       message.Role,
			Content:    message.Content,
			Name:       message.Name,
			Prefix:     message.Prefix,
			ToolCalls:  message.ToolCalls,
			ToolCallId: message.ToolCallId,
		})
	}
	out := &dto.GeneralOpenAIRequest{
		Model:             request.Model,
		Stream:            request.Stream,
		Messages:          messages,
		Temperature:       request.Temperature,
		TopP:              request.TopP,
		Tools:             tools,
		ToolChoice:        request.ToolChoice,
		Stop:              request.Stop,
		N:                 request.N,
		FrequencyPenalty:  request.FrequencyPenalty,
		PresencePenalty:   request.PresencePenalty,
		ResponseFormat:    request.ResponseFormat,
		ParallelTooCalls:  request.ParallelTooCalls,
		ReasoningEffort:   request.ReasoningEffort,
		PromptCacheKey:    request.PromptCacheKey,
		Prediction:        request.Prediction,
		Metadata:          request.Metadata,
		LogProbs:          request.LogProbs,
		TopLogProbs:       request.TopLogProbs,
		TopK:              request.TopK,
		RandomSeed:        randomSeed,
		MinTokens:         request.MinTokens,
		RepetitionPenalty: request.RepetitionPenalty,
		PromptLogProbs:    request.PromptLogProbs,
		TopPromptLogProbs: request.TopPromptLogProbs,
		PromptMode:        request.PromptMode,
		SafePrompt:        request.SafePrompt,
		Guardrails:        request.Guardrails,
		ServiceTier:       request.ServiceTier,
	}
	if request.MaxCompletionTokens != nil {
		out.MaxTokens = request.MaxCompletionTokens
	} else {
		out.MaxTokens = request.MaxTokens
	}
	if out.MinTokens != nil && out.MaxTokens != nil && *out.MinTokens > *out.MaxTokens {
		return nil, fmt.Errorf("min_tokens must not exceed max_tokens")
	}
	return out, nil
}
