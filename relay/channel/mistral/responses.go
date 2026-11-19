package mistral

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// requestResponses2Mistral adapts any standard OpenAI Responses request to
// Mistral's stateless POST /v1/responses schema without rejecting unsupported
// OpenAI-only or stateful options.
func requestResponses2Mistral(request dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, error) {
	input, err := normalizeMistralResponsesInput(request.Input)
	if err != nil {
		return nil, err
	}
	tools, hasTools := normalizeMistralResponsesTools(request.Tools)
	toolChoice := normalizeMistralResponsesToolChoice(request.ToolChoice, hasTools)

	out := &dto.OpenAIResponsesRequest{
		Model:             request.Model,
		Input:             input,
		Instructions:      request.Instructions,
		MaxOutputTokens:   request.MaxOutputTokens,
		MaxToolCalls:      request.MaxToolCalls,
		Metadata:          stringifyMistralMetadata(request.Metadata),
		ParallelToolCalls: request.ParallelToolCalls,
		PromptCacheKey:    request.PromptCacheKey,
		Stream:            request.Stream,
		Temperature:       request.Temperature,
		Text:              request.Text,
		ToolChoice:        toolChoice,
		Tools:             tools,
		TopP:              request.TopP,
	}
	if request.Reasoning != nil && request.Reasoning.Effort != "" {
		if effort := normalizeMistralReasoningEffort(request.Model, request.Reasoning.Effort); effort != "" {
			out.Reasoning = &dto.Reasoning{Effort: effort}
		}
	}
	return out, nil
}

// requestResponsesInputTokens2Mistral keeps only the fields documented for
// Mistral's POST /v1/responses/input_tokens endpoint.
func requestResponsesInputTokens2Mistral(request dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, error) {
	input, err := normalizeMistralResponsesInput(request.Input)
	if err != nil {
		return nil, err
	}
	tools, hasTools := normalizeMistralResponsesTools(request.Tools)
	toolChoice := normalizeMistralResponsesToolChoice(request.ToolChoice, hasTools)

	out := &dto.OpenAIResponsesRequest{
		Model:             request.Model,
		Input:             input,
		Instructions:      request.Instructions,
		ParallelToolCalls: request.ParallelToolCalls,
		Text:              request.Text,
		ToolChoice:        toolChoice,
		Tools:             tools,
	}
	if request.Reasoning != nil && request.Reasoning.Effort != "" {
		if effort := normalizeMistralReasoningEffort(request.Model, request.Reasoning.Effort); effort != "" {
			out.Reasoning = &dto.Reasoning{Effort: effort}
		}
	}
	return out, nil
}

func normalizeMistralResponsesInput(raw json.RawMessage) (json.RawMessage, error) {
	switch common.GetJsonType(raw) {
	case "string":
		return raw, nil
	case "array":
		var items []map[string]json.RawMessage
		if err := common.Unmarshal(raw, &items); err != nil {
			return json.RawMessage(`""`), nil
		}
		out := make([]map[string]json.RawMessage, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			itemType := ""
			if isJSONValuePresent(item["type"]) {
				if err := common.Unmarshal(item["type"], &itemType); err != nil {
					continue
				}
			}
			if itemType == "" {
				switch {
				case isJSONValuePresent(item["role"]):
					itemType = "message"
					item["type"] = json.RawMessage(`"message"`)
				case isJSONValuePresent(item["call_id"]) && isJSONValuePresent(item["name"]):
					itemType = "function_call"
					item["type"] = json.RawMessage(`"function_call"`)
				case isJSONValuePresent(item["call_id"]) && isJSONValuePresent(item["output"]):
					itemType = "function_call_output"
					item["type"] = json.RawMessage(`"function_call_output"`)
				default:
					continue
				}
			}
			switch itemType {
			case "message":
				if !isJSONValuePresent(item["role"]) {
					continue
				}
				if !isJSONValuePresent(item["content"]) {
					item["content"] = json.RawMessage(`""`)
				} else if common.GetJsonType(item["content"]) == "array" {
					var role string
					_ = common.Unmarshal(item["role"], &role)
					item["content"] = normalizeMistralResponsesMessageContent(item["content"], role)
				}
				out = append(out, item)
			case "reasoning":
				// Mistral rejects encrypted_content with 400; strip it and drop
				// opaque reasoning items that have no plaintext content or summary.
				delete(item, "encrypted_content")
				hasContent := isNonEmptyJSONArray(item["content"])
				hasSummary := isNonEmptyJSONArray(item["summary"])
				if !hasContent && !hasSummary {
					continue
				}
				if !isJSONValuePresent(item["summary"]) {
					item["summary"] = json.RawMessage(`[]`)
				}
				out = append(out, item)
			case "function_call":
				if !isJSONValuePresent(item["call_id"]) || !isJSONValuePresent(item["name"]) {
					continue
				}
				if !isJSONValuePresent(item["arguments"]) {
					item["arguments"] = json.RawMessage(`"{}"`)
				}
				out = append(out, item)
			case "function_call_output":
				if !isJSONValuePresent(item["call_id"]) {
					continue
				}
				if !isJSONValuePresent(item["output"]) {
					item["output"] = json.RawMessage(`""`)
				}
				out = append(out, item)
			default:
				// Skip unsupported OpenAI Responses item types (item_reference,
				// web_search_call, file_search_call, mcp_call, computer_call, etc.).
			}
		}
		encoded, err := common.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("failed to encode mistral responses input: %w", err)
		}
		return encoded, nil
	default:
		return json.RawMessage(`""`), nil
	}
}

func normalizeMistralResponsesMessageContent(raw json.RawMessage, role string) json.RawMessage {
	var parts []map[string]json.RawMessage
	if err := common.Unmarshal(raw, &parts); err != nil {
		return raw
	}
	out := make([]map[string]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		var partType string
		if err := common.Unmarshal(part["type"], &partType); err != nil {
			continue
		}
		switch partType {
		case "input_text", "output_text", "input_image", "input_file", "refusal":
			out = append(out, part)
		case "text":
			if !isJSONValuePresent(part["text"]) {
				continue
			}
			mapped := `"input_text"`
			if role == "assistant" {
				mapped = `"output_text"`
			}
			out = append(out, map[string]json.RawMessage{
				"type": json.RawMessage(mapped),
				"text": part["text"],
			})
		case "image_url":
			var imageURL string
			if common.GetJsonType(part["image_url"]) == "object" {
				var image dto.MessageImageUrl
				if err := common.Unmarshal(part["image_url"], &image); err == nil {
					imageURL = image.Url
				}
			} else {
				_ = common.Unmarshal(part["image_url"], &imageURL)
			}
			if imageURL == "" {
				continue
			}
			encodedURL, _ := common.Marshal(imageURL)
			out = append(out, map[string]json.RawMessage{
				"type":      json.RawMessage(`"input_image"`),
				"image_url": encodedURL,
			})
		default:
			if isJSONValuePresent(part["text"]) {
				mapped := `"input_text"`
				if role == "assistant" {
					mapped = `"output_text"`
				}
				out = append(out, map[string]json.RawMessage{
					"type": json.RawMessage(mapped),
					"text": part["text"],
				})
			}
		}
	}
	encoded, err := common.Marshal(out)
	if err != nil {
		return raw
	}
	return encoded
}

func normalizeMistralResponsesTools(raw json.RawMessage) (json.RawMessage, bool) {
	if !isJSONValuePresent(raw) {
		return nil, false
	}
	var tools []map[string]json.RawMessage
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, false
	}
	filtered := make([]map[string]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		var typ string
		if err := common.Unmarshal(tool["type"], &typ); err != nil {
			continue
		}
		switch typ {
		case "function", "namespace":
			filtered = append(filtered, tool)
		}
	}
	if len(filtered) == 0 {
		return nil, false
	}
	encoded, err := common.Marshal(filtered)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func normalizeMistralResponsesToolChoice(raw json.RawMessage, hasTools bool) json.RawMessage {
	if !isJSONValuePresent(raw) {
		return nil
	}
	switch common.GetJsonType(raw) {
	case "string":
		var mode string
		if err := common.Unmarshal(raw, &mode); err != nil {
			return nil
		}
		switch mode {
		case "none", "auto":
			return raw
		case "required":
			if !hasTools {
				return nil
			}
			return raw
		default:
			return nil
		}
	case "object":
		var obj map[string]json.RawMessage
		if err := common.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		var typ string
		_ = common.Unmarshal(obj["type"], &typ)
		switch typ {
		case "function":
			if !hasTools || !isJSONValuePresent(obj["name"]) {
				return nil
			}
			return raw
		case "allowed_tools":
			var mode string
			_ = common.Unmarshal(obj["mode"], &mode)
			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode == "none" || mode == "auto" || (mode == "required" && hasTools) {
				encoded, _ := common.Marshal(mode)
				return encoded
			}
			return nil
		default:
			return nil
		}
	default:
		return nil
	}
}

func isNonEmptyJSONArray(raw json.RawMessage) bool {
	if common.GetJsonType(raw) != "array" {
		return false
	}
	var items []json.RawMessage
	return common.Unmarshal(raw, &items) == nil && len(items) > 0
}

// normalizeMistralResponsesPayload normalizes whole-number floating point
// timestamps (e.g. "created_at": 1795112261.0) into integers so strict OpenAI
// Responses SDKs can unmarshal Mistral responses cleanly.
func normalizeMistralResponsesPayload(payload []byte) []byte {
	if !bytes.Contains(payload, []byte(`"created_at"`)) && !bytes.Contains(payload, []byte(`"completed_at"`)) {
		return payload
	}
	var root map[string]json.RawMessage
	if err := common.Unmarshal(payload, &root); err != nil {
		return payload
	}
	changed := normalizeResponseTimestampsInObject(root)
	if respRaw, ok := root["response"]; ok && common.GetJsonType(respRaw) == "object" {
		var nested map[string]json.RawMessage
		if err := common.Unmarshal(respRaw, &nested); err == nil && normalizeResponseTimestampsInObject(nested) {
			if encoded, err := common.Marshal(nested); err == nil {
				root["response"] = encoded
				changed = true
			}
		}
	}
	if !changed {
		return payload
	}
	out, err := common.Marshal(root)
	if err != nil {
		return payload
	}
	return out
}

func normalizeResponseTimestampsInObject(obj map[string]json.RawMessage) bool {
	changed := false
	for _, key := range []string{"created_at", "completed_at"} {
		raw, ok := obj[key]
		if !ok || !bytes.ContainsRune(raw, '.') {
			continue
		}
		var f float64
		if err := common.Unmarshal(raw, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		if math.Trunc(f) == f && f >= -1<<53 && f <= 1<<53 {
			if encoded, err := common.Marshal(int64(f)); err == nil {
				obj[key] = encoded
				changed = true
			}
		}
	}
	return changed
}

func normalizeMistralResponsesBody(body io.ReadCloser, stream bool) (io.ReadCloser, error) {
	if !stream {
		data, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(normalizeMistralResponsesPayload(data))), nil
	}
	pr, pw := io.Pipe()
	go func() {
		reader := bufio.NewReader(body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
					trimmed := bytes.TrimSpace(data)
					normalized := normalizeMistralResponsesPayload(trimmed)
					if !bytes.Equal(normalized, trimmed) {
						line = append(append([]byte("data: "), normalized...), '\n')
					}
				}
				if _, werr := pw.Write(line); werr != nil {
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					_ = pw.Close()
				} else {
					_ = pw.CloseWithError(err)
				}
				return
			}
		}
	}()
	return &normalizedBody{Reader: pr, closers: []io.Closer{pr, body}}, nil
}
