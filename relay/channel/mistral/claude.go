package mistral

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// claudeRequestToOpenAIChat converts an Anthropic /v1/messages request into an
// intermediate OpenAI Chat request for Mistral without rejecting unsupported
// Anthropic-only features (server tools, redacted_thinking, mcp_servers,
// container, context_management, etc.).
func claudeRequestToOpenAIChat(request *dto.ClaudeRequest) (*dto.GeneralOpenAIRequest, error) {
	raw, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	var req map[string]any
	if err := common.Unmarshal(raw, &req); err != nil {
		return nil, err
	}

	result := make(map[string]any)
	if model, ok := req["model"].(string); ok {
		result["model"] = model
	}
	if temp, ok := req["temperature"]; ok && temp != nil {
		result["temperature"] = temp
	}
	if topP, ok := req["top_p"]; ok && topP != nil {
		result["top_p"] = topP
	}
	if topK, ok := req["top_k"]; ok && topK != nil {
		result["top_k"] = topK
	}
	if metadata, ok := req["metadata"]; ok && metadata != nil {
		result["metadata"] = metadata
	}
	if maxTokens, ok := req["max_tokens"]; ok && maxTokens != nil {
		result["max_tokens"] = maxTokens
	}
	if stream, ok := req["stream"]; ok && stream != nil {
		result["stream"] = stream
	}
	if stopSeqs, ok := req["stop_sequences"].([]any); ok && len(stopSeqs) > 0 {
		result["stop"] = stopSeqs
	}

	var messages []any
	if sys := req["system"]; sys != nil {
		if sysText := extractClaudeSystemText(sys); sysText != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": sysText,
			})
		}
	}
	if claudeMessages, ok := req["messages"].([]any); ok {
		for _, msg := range claudeMessages {
			m, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			messages = append(messages, convertClaudeMessageForMistral(m)...)
		}
	}
	result["messages"] = messages

	// Tools: keep client function tools; ignore Anthropic server tools.
	allowedToolNames := make(map[string]bool)
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		openAITools := make([]any, 0, len(tools))
		for _, tool := range tools {
			t, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := t["type"].(string); typ != "" && typ != "custom" {
				continue
			}
			name, _ := t["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			allowedToolNames[name] = true
			params := t["input_schema"]
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			fn := map[string]any{
				"name":        name,
				"description": t["description"],
				"parameters":  params,
			}
			if strict, exists := t["strict"]; exists {
				fn["strict"] = strict
			}
			openAITools = append(openAITools, map[string]any{
				"type":     "function",
				"function": fn,
			})
		}
		if len(openAITools) > 0 {
			result["tools"] = openAITools
		}
	}

	if tc, ok := req["tool_choice"].(map[string]any); ok {
		if disabled, ok := tc["disable_parallel_tool_use"].(bool); ok {
			result["parallel_tool_calls"] = !disabled
		}
		if len(allowedToolNames) > 0 {
			switch tc["type"] {
			case "auto", "none":
				result["tool_choice"] = tc["type"]
			case "any":
				result["tool_choice"] = "required"
			case "tool":
				if name, _ := tc["name"].(string); allowedToolNames[name] {
					result["tool_choice"] = map[string]any{
						"type": "function",
						"function": map[string]any{
							"name": name,
						},
					}
				}
			}
		}
	}

	// Thinking & output_config.effort -> reasoning_effort
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if typ, _ := thinking["type"].(string); typ != "" {
			switch typ {
			case "adaptive", "enabled":
				result["reasoning_effort"] = "high"
			case "disabled":
				result["reasoning_effort"] = "none"
			}
		}
	}

	formatValue := req["output_format"]
	if config, ok := req["output_config"].(map[string]any); ok {
		if effort, ok := config["effort"].(string); ok && effort != "" {
			result["reasoning_effort"] = effort
		}
		if current, exists := config["format"]; exists && current != nil {
			formatValue = current
		}
	}
	if format, ok := formatValue.(map[string]any); ok {
		switch format["type"] {
		case "json_schema":
			if format["schema"] != nil {
				result["response_format"] = map[string]any{
					"type": "json_schema",
					"json_schema": map[string]any{
						"name":   "response",
						"schema": format["schema"],
						"strict": true,
					},
				}
			}
		case "json_object":
			result["response_format"] = map[string]any{
				"type": "json_object",
			}
		}
	}

	encoded, err := common.Marshal(result)
	if err != nil {
		return nil, err
	}
	var chat dto.GeneralOpenAIRequest
	if err := common.Unmarshal(encoded, &chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func extractClaudeSystemText(sys any) string {
	switch s := sys.(type) {
	case string:
		return s
	case []any:
		var parts []string
		for _, item := range s {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func convertClaudeMessageForMistral(msg map[string]any) []any {
	role, _ := msg["role"].(string)
	content := msg["content"]

	if text, ok := content.(string); ok {
		return []any{map[string]any{"role": role, "content": text}}
	}

	parts, ok := content.([]any)
	if !ok {
		return []any{map[string]any{"role": role, "content": ""}}
	}

	var result []any
	var reasoningContent strings.Builder
	var contentParts []any
	var toolCalls []any

	for _, part := range parts {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		switch p["type"] {
		case "thinking":
			if thinking, ok := p["thinking"].(string); ok && role == "assistant" {
				reasoningContent.WriteString(thinking)
			}
		case "redacted_thinking":
			// Opaque Anthropic signatures cannot be replayed on Mistral; skip safely.
		case "text", "image", "document":
			contentParts = append(contentParts, p)
		case "tool_use":
			argsBytes, err := common.Marshal(p["input"])
			if err != nil || len(argsBytes) == 0 || string(argsBytes) == "null" {
				argsBytes = json.RawMessage(`{}`)
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   p["id"],
				"type": "function",
				"function": map[string]any{
					"name":      p["name"],
					"arguments": string(argsBytes),
				},
			})
		case "tool_result":
			if len(contentParts) > 0 {
				result = append(result, map[string]any{
					"role":    role,
					"content": convertClaudeContentForMistral(contentParts),
				})
				contentParts = nil
			}
			converted := convertClaudeContentForMistral(p["content"])
			if isError, _ := p["is_error"].(bool); isError {
				const marker = "Tool execution failed (is_error=true): "
				if text, ok := converted.(string); ok {
					converted = marker + text
				} else if mediaParts, ok := converted.([]any); ok {
					converted = append([]any{map[string]any{"type": "text", "text": marker}}, mediaParts...)
				}
			}
			result = append(result, map[string]any{
				"role":         "tool",
				"content":      converted,
				"tool_call_id": p["tool_use_id"],
			})
		default:
			// Preserve text from unrecognized blocks if present; otherwise skip safely.
			if text, ok := p["text"].(string); ok && text != "" {
				contentParts = append(contentParts, map[string]any{"type": "text", "text": text})
			}
		}
	}

	mainMsg := map[string]any{
		"role":    role,
		"content": convertClaudeContentForMistral(contentParts),
	}
	if len(toolCalls) > 0 && role == "assistant" {
		mainMsg["tool_calls"] = toolCalls
	}
	if reasoningContent.Len() > 0 {
		mainMsg["reasoning_content"] = reasoningContent.String()
	}
	if len(contentParts) > 0 || len(toolCalls) > 0 || reasoningContent.Len() > 0 || len(result) == 0 {
		result = append(result, mainMsg)
	}
	return result
}

func convertClaudeContentForMistral(content any) any {
	if content == nil {
		return ""
	}
	if text, ok := content.(string); ok {
		return text
	}
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}

	parts := make([]any, 0, len(blocks))
	var text strings.Builder
	hasMedia := false
	for _, block := range blocks {
		part, ok := block.(map[string]any)
		if !ok {
			continue
		}
		switch part["type"] {
		case "text":
			value, _ := part["text"].(string)
			text.WriteString(value)
			parts = append(parts, map[string]any{"type": "text", "text": value})
		case "image", "document":
			source, ok := part["source"].(map[string]any)
			if !ok {
				continue
			}
			var data string
			switch source["type"] {
			case "url":
				data, _ = source["url"].(string)
			case "base64":
				mediaType, _ := source["media_type"].(string)
				encoded, _ := source["data"].(string)
				if mediaType != "" && encoded != "" {
					data = "data:" + mediaType + ";base64," + encoded
				}
			case "text":
				if part["type"] == "document" {
					if value, _ := source["data"].(string); value != "" {
						text.WriteString(value)
						parts = append(parts, map[string]any{"type": "text", "text": value})
					}
				}
				continue
			case "content":
				if part["type"] == "document" {
					nested := convertClaudeContentForMistral(source["content"])
					if s, ok := nested.(string); ok && s != "" {
						text.WriteString(s)
						parts = append(parts, map[string]any{"type": "text", "text": s})
					}
				}
				continue
			}
			if data == "" {
				continue
			}
			hasMedia = true
			if part["type"] == "image" {
				parts = append(parts, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": data},
				})
			} else {
				file := map[string]any{}
				if source["type"] == "url" {
					file["file_url"] = data
				} else {
					file["file_data"] = data
				}
				if title, _ := part["title"].(string); title != "" {
					file["filename"] = title
				}
				parts = append(parts, map[string]any{"type": "file", "file": file})
			}
		default:
			if value, ok := part["text"].(string); ok && value != "" {
				text.WriteString(value)
				parts = append(parts, map[string]any{"type": "text", "text": value})
			}
		}
	}
	if !hasMedia {
		return text.String()
	}
	return parts
}
