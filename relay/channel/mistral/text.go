package mistral

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// requestOpenAI2Mistral adapts an OpenAI Chat Completions request for
// Mistral's OpenAI-compatible POST /openai/v1/chat/completions endpoint.
func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	seed := request.Seed
	if seed == nil && request.RandomSeed != nil {
		s := float64(*request.RandomSeed)
		seed = &s
	} else if seed != nil && (math.IsNaN(*seed) || math.IsInf(*seed, 0)) {
		seed = nil
	}

	tools := normalizeMistralChatTools(request.Tools, request.Functions)
	toolChoice := normalizeMistralChatToolChoice(request.ToolChoice, request.FunctionCall, len(tools) > 0)

	messages := make([]dto.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		role := message.Role
		if role == "function" {
			role = "tool"
		}

		var toolCallsRaw json.RawMessage
		if role == "assistant" && len(message.ToolCalls) > 0 {
			var toolCalls []dto.ToolCallRequest
			if err := common.Unmarshal(message.ToolCalls, &toolCalls); err == nil {
				for i := range toolCalls {
					if toolCalls[i].Type == "" {
						toolCalls[i].Type = "function"
					}
				}
				if encoded, err := common.Marshal(toolCalls); err == nil {
					toolCallsRaw = encoded
				}
			} else {
				toolCallsRaw = message.ToolCalls
			}
		}

		content := message.Content
		var extractedReasoning string
		if content != nil && !message.IsStringContent() {
			rawContent, err := common.Marshal(content)
			if err != nil {
				return nil, err
			}
			var rawChunks []map[string]json.RawMessage
			if err := common.Unmarshal(rawContent, &rawChunks); err == nil {
				chunks, thinking := normalizeMistralChatChunks(rawChunks, role)
				extractedReasoning = thinking
				if len(chunks) > 0 {
					content = chunks
				} else if role == "assistant" {
					content = nil
				} else {
					content = ""
				}
			}
		}
		// Mistral /openai/v1/chat/completions requires non-null content on
		// system, developer, user, and tool messages; only assistant accepts null.
		if content == nil && role != "assistant" {
			content = ""
		}

		outMsg := dto.Message{
			Role:    role,
			Content: content,
		}
		switch role {
		case "tool":
			outMsg.ToolCallId = message.ToolCallId
		case "assistant":
			// Only assistant messages accept `name` on /openai/v1/chat/completions.
			outMsg.Name = message.Name
			outMsg.ToolCalls = toolCallsRaw
			reasoning := message.GetReasoningContent()
			if extractedReasoning != "" {
				if reasoning == "" {
					reasoning = extractedReasoning
				} else {
					reasoning += "\n" + extractedReasoning
				}
			}
			if reasoning != "" {
				outMsg.ReasoningContent = common.GetPointer(reasoning)
			}
		}
		messages = append(messages, outMsg)
	}

	out := &dto.GeneralOpenAIRequest{
		Model:               request.Model,
		Stream:              request.Stream,
		Messages:            messages,
		MaxTokens:           request.MaxTokens,
		MaxCompletionTokens: request.MaxCompletionTokens,
		Temperature:         request.Temperature,
		TopP:                request.TopP,
		Tools:               tools,
		ToolChoice:          toolChoice,
		Stop:                request.Stop,
		N:                   request.N,
		FrequencyPenalty:    request.FrequencyPenalty,
		PresencePenalty:     request.PresencePenalty,
		ResponseFormat:      request.ResponseFormat,
		ParallelTooCalls:    request.ParallelTooCalls,
		ReasoningEffort:     normalizeMistralReasoningEffort(request.Model, request.ReasoningEffort),
		PromptCacheKey:      request.PromptCacheKey,
		Metadata:            stringifyMistralMetadata(request.Metadata),
		Seed:                seed,
	}
	if request.Stream != nil && *request.Stream {
		out.StreamOptions = request.StreamOptions
	}
	if isJSONValuePresent(request.User) {
		out.User = request.User
	}
	return out, nil
}

func normalizeMistralChatTools(reqTools []dto.ToolCallRequest, legacyFunctions json.RawMessage) []dto.ToolCallRequest {
	tools := make([]dto.ToolCallRequest, 0, len(reqTools))
	for _, tool := range reqTools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		if strings.TrimSpace(tool.Function.Name) == "" {
			continue
		}
		tool.Type = "function"
		if tool.Function.Parameters == nil {
			tool.Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, tool)
	}
	if len(tools) == 0 && isJSONValuePresent(legacyFunctions) {
		var fns []dto.FunctionRequest
		if err := common.Unmarshal(legacyFunctions, &fns); err == nil {
			for _, fn := range fns {
				if strings.TrimSpace(fn.Name) == "" {
					continue
				}
				if fn.Parameters == nil {
					fn.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
				}
				tools = append(tools, dto.ToolCallRequest{
					Type:     "function",
					Function: fn,
				})
			}
		}
	}
	if len(tools) == 0 {
		return nil
	}
	return tools
}

func normalizeMistralChatToolChoice(toolChoice any, legacyFunctionCall json.RawMessage, hasTools bool) any {
	if toolChoice == nil && isJSONValuePresent(legacyFunctionCall) {
		var str string
		if err := common.Unmarshal(legacyFunctionCall, &str); err == nil {
			toolChoice = str
		} else {
			var obj struct {
				Name string `json:"name"`
			}
			if err := common.Unmarshal(legacyFunctionCall, &obj); err == nil && obj.Name != "" {
				toolChoice = map[string]any{
					"type":     "function",
					"function": map[string]any{"name": obj.Name},
				}
			}
		}
	}
	if toolChoice == nil {
		return nil
	}
	switch v := toolChoice.(type) {
	case string:
		switch v {
		case "none", "auto":
			return v
		case "any", "required":
			if !hasTools {
				return nil
			}
			return "required"
		default:
			return nil
		}
	case map[string]any:
		if !hasTools {
			return nil
		}
		if typ, _ := v["type"].(string); typ != "" && typ != "function" {
			return nil
		}
		return v
	default:
		if !hasTools {
			return nil
		}
		return toolChoice
	}
}

func normalizeMistralChatChunks(rawChunks []map[string]json.RawMessage, role string) ([]map[string]json.RawMessage, string) {
	normalized := make([]map[string]json.RawMessage, 0, len(rawChunks))
	var thinking strings.Builder
	allowMedia := role == "user"
	for _, chunk := range rawChunks {
		if chunk == nil {
			continue
		}
		var contentType string
		if err := common.Unmarshal(chunk["type"], &contentType); err != nil {
			continue
		}
		switch contentType {
		case dto.ContentTypeText, "input_text", "output_text":
			if !isJSONValuePresent(chunk["text"]) {
				continue
			}
			normalized = append(normalized, map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`),
				"text": chunk["text"],
			})
		case dto.ContentTypeImageURL, "input_image":
			if !allowMedia {
				continue
			}
			var image dto.MessageImageUrl
			if common.GetJsonType(chunk["image_url"]) == "object" {
				_ = common.Unmarshal(chunk["image_url"], &image)
			} else {
				_ = common.Unmarshal(chunk["image_url"], &image.Url)
				if isJSONValuePresent(chunk["detail"]) {
					_ = common.Unmarshal(chunk["detail"], &image.Detail)
				}
			}
			if image.Url == "" {
				continue
			}
			image.Detail = normalizeMistralImageDetail(image.Detail)
			encodedImage, err := common.Marshal(image)
			if err != nil {
				continue
			}
			normalized = append(normalized, map[string]json.RawMessage{
				"type":      json.RawMessage(`"image_url"`),
				"image_url": encodedImage,
			})
		case dto.ContentTypeFile, "input_file":
			if !allowMedia {
				continue
			}
			fileObj := chunk
			if isJSONValuePresent(chunk["file"]) && common.GetJsonType(chunk["file"]) == "object" {
				var nested map[string]json.RawMessage
				if err := common.Unmarshal(chunk["file"], &nested); err == nil {
					fileObj = nested
				}
			}
			part := make(map[string]json.RawMessage)
			if isJSONValuePresent(fileObj["file_id"]) {
				part["file_id"] = fileObj["file_id"]
			} else if isJSONValuePresent(fileObj["file_data"]) {
				var fileData string
				if err := common.Unmarshal(fileObj["file_data"], &fileData); err == nil && strings.HasPrefix(fileData, "data:") {
					part["file_data"] = fileObj["file_data"]
					if isJSONValuePresent(fileObj["filename"]) {
						part["filename"] = fileObj["filename"]
					}
				}
			}
			if len(part) == 0 {
				continue
			}
			encodedFile, err := common.Marshal(part)
			if err != nil {
				continue
			}
			normalized = append(normalized, map[string]json.RawMessage{
				"type": json.RawMessage(`"file"`),
				"file": encodedFile,
			})
		case "thinking":
			var rawThinking any
			if common.Unmarshal(chunk["thinking"], &rawThinking) == nil {
				appendMistralThinkingText(&thinking, rawThinking)
			}
		default:
			if isJSONValuePresent(chunk["text"]) {
				normalized = append(normalized, map[string]json.RawMessage{
					"type": json.RawMessage(`"text"`),
					"text": chunk["text"],
				})
			}
		}
	}
	return normalized, thinking.String()
}
