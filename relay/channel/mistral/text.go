package mistral

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	randomSeed := request.RandomSeed
	if randomSeed == nil && request.Seed != nil {
		seed := *request.Seed
		if !math.IsNaN(seed) && !math.IsInf(seed, 0) && seed >= -1<<63 && seed < 1<<63 {
			randomSeed = common.GetPointer(int64(math.Trunc(seed)))
		}
	}

	tools := normalizeMistralChatTools(request.Tools, request.Functions)
	toolChoice := normalizeMistralChatToolChoice(request.ToolChoice, request.FunctionCall, len(tools) > 0)

	// Pre-collect existing valid 9-char IDs and map non-conforming IDs
	// deterministically via SHA-256 so multi-turn tool histories produce stable
	// prompt prefixes and preserve Mistral prompt caching.
	usedIDs := make(map[string]bool)
	for _, message := range request.Messages {
		for _, tc := range message.ParseToolCalls() {
			if mistralToolCallIdRegexp.MatchString(tc.ID) {
				usedIDs[tc.ID] = true
			}
		}
		if mistralToolCallIdRegexp.MatchString(message.ToolCallId) {
			usedIDs[message.ToolCallId] = true
		}
	}
	idMap := make(map[string]string)
	convertToolCallID := func(id string) string {
		if mistralToolCallIdRegexp.MatchString(id) {
			return id
		}
		if mapped, ok := idMap[id]; ok {
			return mapped
		}
		for salt := 0; ; salt++ {
			digest := sha256.Sum256([]byte(id + ":" + strconv.Itoa(salt)))
			candidate := fmt.Sprintf("%x", digest)[:9]
			if !usedIDs[candidate] {
				usedIDs[candidate] = true
				idMap[id] = candidate
				return candidate
			}
		}
	}

	messages := make([]dto.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		role := message.Role
		switch role {
		case "developer":
			role = "system"
		case "function":
			role = "tool"
		}

		// 1. tool_calls.id (only valid on assistant messages)
		var toolCallsRaw json.RawMessage
		if role == "assistant" && len(message.ToolCalls) > 0 {
			var toolCalls []dto.ToolCallRequest
			if err := common.Unmarshal(message.ToolCalls, &toolCalls); err == nil {
				for i := range toolCalls {
					if toolCalls[i].Type == "" {
						toolCalls[i].Type = "function"
					}
					toolCalls[i].ID = convertToolCallID(toolCalls[i].ID)
				}
				message.SetToolCalls(toolCalls)
				toolCallsRaw = message.ToolCalls
			}
		}

		// 2. tool_call_id (only valid on tool messages)
		toolCallId := ""
		if role == "tool" && message.ToolCallId != "" {
			toolCallId = convertToolCallID(message.ToolCallId)
		}

		content := message.Content
		var chunks []map[string]json.RawMessage
		if content != nil && !message.IsStringContent() {
			rawContent, err := common.Marshal(content)
			if err != nil {
				return nil, err
			}
			var rawChunks []map[string]json.RawMessage
			if err := common.Unmarshal(rawContent, &rawChunks); err == nil {
				chunks = normalizeMistralChatChunks(rawChunks)
			}
		}
		if role == "assistant" && message.GetReasoningContent() != "" {
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
			content = chunks
		}
		// Mistral SystemMessage / UserMessage / ToolMessage require non-null content
		// (only AssistantMessage accepts omitted/null content with tool_calls).
		if content == nil && role == "system" {
			content = ""
		}

		outMsg := dto.Message{
			Role:    role,
			Content: content,
		}
		// Mistral schemas set additionalProperties=false per role:
		// only ToolMessage permits name/tool_call_id, and only AssistantMessage permits prefix/tool_calls.
		if role == "tool" {
			outMsg.Name = message.Name
			outMsg.ToolCallId = toolCallId
		} else if role == "assistant" {
			outMsg.Prefix = message.Prefix
			outMsg.ToolCalls = toolCallsRaw
		}
		messages = append(messages, outMsg)
	}

	out := &dto.GeneralOpenAIRequest{
		Model:             request.Model,
		Stream:            request.Stream,
		Messages:          messages,
		Temperature:       request.Temperature,
		TopP:              request.TopP,
		Tools:             tools,
		ToolChoice:        toolChoice,
		Stop:              request.Stop,
		N:                 request.N,
		FrequencyPenalty:  request.FrequencyPenalty,
		PresencePenalty:   request.PresencePenalty,
		ResponseFormat:    request.ResponseFormat,
		ParallelTooCalls:  request.ParallelTooCalls,
		ReasoningEffort:   normalizeMistralReasoningEffort(request.Model, request.ReasoningEffort),
		PromptCacheKey:    request.PromptCacheKey,
		Prediction:        request.Prediction,
		Metadata:          request.Metadata,
		RandomSeed:        randomSeed,
		RepetitionPenalty: request.RepetitionPenalty,
		PromptMode:        request.PromptMode,
		SafePrompt:        request.SafePrompt,
		Guardrails:        request.Guardrails,
		ServiceTier:       normalizeMistralChatServiceTier(request.ServiceTier),
	}
	if !modelRejectsTopK(request.Model) {
		out.TopK = request.TopK
	}
	if !modelRejectsLogprobs(request.Model) {
		out.LogProbs = request.LogProbs
		out.TopLogProbs = request.TopLogProbs
		out.PromptLogProbs = request.PromptLogProbs
		out.TopPromptLogProbs = request.TopPromptLogProbs
	}
	if request.MaxCompletionTokens != nil {
		out.MaxTokens = request.MaxCompletionTokens
	} else {
		out.MaxTokens = request.MaxTokens
	}
	if !modelRejectsMinTokens(request.Model) && request.MinTokens != nil {
		minTokens := *request.MinTokens
		if out.MaxTokens != nil && minTokens > *out.MaxTokens {
			minTokens = *out.MaxTokens
		}
		out.MinTokens = &minTokens
	}
	return out, nil
}

func normalizeMistralChatServiceTier(tier json.RawMessage) json.RawMessage {
	if !isJSONValuePresent(tier) {
		return nil
	}
	var value string
	if err := common.Unmarshal(tier, &value); err != nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "priority":
		return json.RawMessage(`"auto"`)
	case "standard_only":
		return json.RawMessage(`"standard_only"`)
	default:
		return nil
	}
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
			return v
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

func normalizeMistralChatChunks(rawChunks []map[string]json.RawMessage) []map[string]json.RawMessage {
	normalized := make([]map[string]json.RawMessage, 0, len(rawChunks))
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
			var imageURL string
			if common.GetJsonType(chunk["image_url"]) == "object" {
				var image dto.MessageImageUrl
				if err := common.Unmarshal(chunk["image_url"], &image); err == nil {
					imageURL = image.Url
				}
			} else {
				_ = common.Unmarshal(chunk["image_url"], &imageURL)
			}
			if imageURL == "" {
				continue
			}
			encodedURL, _ := common.Marshal(imageURL)
			normalized = append(normalized, map[string]json.RawMessage{
				"type":      json.RawMessage(`"image_url"`),
				"image_url": encodedURL,
			})
		case dto.ContentTypeFile, "input_file":
			fileObj := chunk
			if isJSONValuePresent(chunk["file"]) && common.GetJsonType(chunk["file"]) == "object" {
				var nested map[string]json.RawMessage
				if err := common.Unmarshal(chunk["file"], &nested); err == nil {
					fileObj = nested
				}
			}
			if isJSONValuePresent(fileObj["file_id"]) {
				normalized = append(normalized, map[string]json.RawMessage{
					"type":    json.RawMessage(`"file"`),
					"file_id": fileObj["file_id"],
				})
				continue
			}
			url := fileObj["file_url"]
			if !isJSONValuePresent(url) {
				url = fileObj["file_data"]
			}
			if !isJSONValuePresent(url) {
				continue
			}
			doc := map[string]json.RawMessage{
				"type":         json.RawMessage(`"document_url"`),
				"document_url": url,
			}
			if isJSONValuePresent(fileObj["filename"]) {
				doc["document_name"] = fileObj["filename"]
			}
			normalized = append(normalized, doc)
		case "thinking", "document_url", "reference", "input_audio", "resource", "resource_link":
			normalized = append(normalized, chunk)
		default:
			if isJSONValuePresent(chunk["text"]) {
				normalized = append(normalized, map[string]json.RawMessage{
					"type": json.RawMessage(`"text"`),
					"text": chunk["text"],
				})
			}
		}
	}
	return normalized
}
