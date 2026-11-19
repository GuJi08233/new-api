package mistral

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
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
			for _, chunk := range chunks {
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
		Model:            request.Model,
		Stream:           request.Stream,
		Messages:         messages,
		Temperature:      request.Temperature,
		TopP:             request.TopP,
		Tools:            request.Tools,
		ToolChoice:       request.ToolChoice,
		Stop:             request.Stop,
		N:                request.N,
		FrequencyPenalty: request.FrequencyPenalty,
		PresencePenalty:  request.PresencePenalty,
		ResponseFormat:   request.ResponseFormat,
		ParallelTooCalls: request.ParallelTooCalls,
		ReasoningEffort:  request.ReasoningEffort,
		PromptCacheKey:   request.PromptCacheKey,
		Prediction:       request.Prediction,
		Metadata:         request.Metadata,
		LogProbs:         request.LogProbs,
		TopLogProbs:      request.TopLogProbs,
	}
	if request.MaxCompletionTokens != nil {
		out.MaxTokens = request.MaxCompletionTokens
	} else {
		out.MaxTokens = request.MaxTokens
	}
	return out, nil
}
