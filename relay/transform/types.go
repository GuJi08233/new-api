// Package transform provides pure JSON transformation utilities for format conversion
// between different AI provider APIs. It follows the design principles from cc-switch:
// no DTO dependency, provider-specific flags, and pure JSON manipulation.
package transform

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"strings"
)

// TransformFlags contains provider-specific transformation flags
type TransformFlags struct {
	// IsCodexOAuth indicates if this is a Codex OAuth request (special handling)
	IsCodexOAuth bool
	// CodexFastMode enables service_tier=priority for Codex
	CodexFastMode bool
	// PreserveCacheControl preserves cache_control fields in messages
	PreserveCacheControl bool
	// OSeriesMode indicates o-series model needs max_completion_tokens instead of max_tokens
	OSeriesMode bool
}

// RequestTransform converts an OpenAI Chat request body to Claude format
// Returns transformed JSON bytes directly, no DTO intermediate layer.
func RequestOpenAIToClaude(body []byte, flags TransformFlags) ([]byte, error) {
	result := make(map[string]any)

	// Parse to map instead of DTO for maximum flexibility
	var req map[string]any
	if err := common.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	// Model - preserve directly
	if model, ok := req["model"].(string); ok {
		result["model"] = model
	}

	// Temperature - pass through if present
	if temp, ok := req["temperature"]; ok {
		result["temperature"] = temp
	}

	// TopP - pass through
	if topP, ok := req["top_p"]; ok {
		result["top_p"] = topP
	}

	// Stop sequences
	if stop, ok := req["stop"]; ok {
		switch s := stop.(type) {
		case string:
			if s != "" {
				result["stop_sequences"] = []string{s}
			}
		case []any:
			result["stop_sequences"] = s
		}
	}

	// Stream
	if stream, ok := req["stream"]; ok {
		result["stream"] = stream
	}

	// Max tokens / Max completion tokens handling
	// O-series models need max_completion_tokens, regular models use max_tokens
	handleMaxTokens(result, req, flags)

	// Reasoning effort mapping - multi-level resolution
	handleReasoningEffort(result, req)

	// Handle tools
	handleTools(result, req)

	// Handle tool_choice
	handleToolChoice(result, req)

	// Handle messages and system prompt
	var systemMessages []any
	var messages []any
	if msgs, ok := req["messages"].([]any); ok {
		systemMessages, messages = processMessages(msgs, flags)
	}

	// Set system if we have system messages
	if len(systemMessages) > 0 {
		result["system"] = systemMessages
	}
	result["messages"] = messages

	// Codex OAuth special handling
	if flags.IsCodexOAuth {
		delete(result, "max_tokens")
		delete(result, "temperature")
		delete(result, "top_p")
		result["store"] = false
		result["include"] = []string{"reasoning.encrypted_content"}
		result["stream"] = true
		if flags.CodexFastMode {
			result["service_tier"] = "priority"
		}
	}

	return common.Marshal(result)
}

// handleMaxTokens handles the difference between max_tokens (normal) and max_completion_tokens (o-series)
func handleMaxTokens(result, req map[string]any, flags TransformFlags) {
	maxTokens := 0
	if mt, ok := req["max_tokens"]; ok {
		maxTokens = interface2Int(mt)
	}
	if mct, ok := req["max_completion_tokens"]; ok && interface2Int(mct) > maxTokens {
		maxTokens = interface2Int(mct)
	}

	if maxTokens > 0 {
		if flags.OSeriesMode {
			result["max_completion_tokens"] = maxTokens
		} else {
			result["max_tokens"] = maxTokens
		}
	}
}

func interface2Int(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	case uint:
		return int(val)
	case uint32:
		return int(val)
	case uint64:
		return int(val)
	default:
		return 0
	}
}

// handleReasoningEffort implements multi-level reasoning effort mapping
// Priority 1: explicit output_config.effort
// Priority 2: thinking.type + budget_tokens
// Priority 3: reasoning_effort field
func handleReasoningEffort(result, req map[string]any) {
	effort := resolveReasoningEffort(req)
	if effort != "" {
		if effort == "low" || effort == "medium" || effort == "high" {
			// Claude thinking type with budget tokens
			budgetTokens := map[string]int{
				"low":    1024,
				"medium": 2048,
				"high":   4096,
			}[effort]
			result["thinking"] = map[string]any{
				"type":          "enabled",
				"budget_tokens": budgetTokens,
			}
		} else if effort == "xhigh" || effort == "max" {
			// Adaptive thinking for highest effort
			result["thinking"] = map[string]any{
				"type":    "adaptive",
				"display": "summarized",
			}
			result["output_config"] = map[string]any{
				"effort": "high",
			}
		}
	}
}

// resolveReasoningEffort implements the multi-level resolution logic from cc-switch
func resolveReasoningEffort(req map[string]any) string {
	// Priority 1: explicit output_config.effort
	if outputConfig, ok := req["output_config"].(map[string]any); ok {
		if effort, ok := outputConfig["effort"].(string); ok && effort != "" {
			return effort
		}
	}

	// Priority 2: thinking.type
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if typ, ok := thinking["type"].(string); ok {
			if typ == "adaptive" {
				return "xhigh"
			}
			if typ == "enabled" {
				if budget, ok := thinking["budget_tokens"].(float64); ok {
					switch {
					case budget < 4000:
						return "low"
					case budget < 16000:
						return "medium"
					default:
						return "high"
					}
				}
				return "medium"
			}
		}
	}

	// Priority 3: reasoning_effort field
	if effort, ok := req["reasoning_effort"].(string); ok && effort != "" {
		return effort
	}

	return ""
}

// handleTools transforms OpenAI tools to Claude format
func handleTools(result, req map[string]any) {
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		var claudeTools []any
		for _, tool := range tools {
			if t, ok := tool.(map[string]any); ok {
				if t["type"] == "function" {
					if function, ok := t["function"].(map[string]any); ok {
						claudeTool := map[string]any{
							"name":        function["name"],
							"description": function["description"],
							"input_schema": map[string]any{
								"type": "object",
							},
						}
						if params, ok := function["parameters"].(map[string]any); ok {
							claudeTool["input_schema"] = params
						}
						claudeTools = append(claudeTools, claudeTool)
					}
				}
			}
		}
		if len(claudeTools) > 0 {
			result["tools"] = claudeTools
		}
	}
}

// handleToolChoice transforms OpenAI tool_choice to Claude format
func handleToolChoice(result, req map[string]any) {
	if tc, ok := req["tool_choice"]; ok {
		switch choice := tc.(type) {
		case string:
			switch choice {
			case "auto":
				result["tool_choice"] = map[string]any{"type": "auto"}
			case "required":
				result["tool_choice"] = map[string]any{"type": "any"}
			case "none":
				result["tool_choice"] = map[string]any{"type": "none"}
			}
		case map[string]any:
			if choice["type"] == "function" {
				if function, ok := choice["function"].(map[string]any); ok {
					if name, ok := function["name"].(string); ok {
						result["tool_choice"] = map[string]any{
							"type": "tool",
							"name": name,
						}
					}
				} else if name, ok := choice["name"].(string); ok {
					result["tool_choice"] = map[string]any{
						"type": "tool",
						"name": name,
					}
				}
			}
		}

		// Handle parallel_tool_calls
		if ptc, ok := req["parallel_tool_calls"].(bool); ok {
			if tc, ok := result["tool_choice"].(map[string]any); ok && tc["type"] != "none" {
				tc["disable_parallel_tool_use"] = !ptc
			}
		}
	}
}

// processMessages extracts system messages and transforms OpenAI messages to Claude format
func processMessages(messages []any, flags TransformFlags) (systemMessages []any, resultMessages []any) {
	var lastRole string

	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}

		role, _ := m["role"].(string)

		// System messages go to the system array
		if role == "system" || role == "developer" {
			if content, ok := m["content"].(string); ok && strings.TrimSpace(content) != "" {
				systemMessages = append(systemMessages, map[string]any{
					"type": "text",
					"text": content,
				})
			} else if parts, ok := m["content"].([]any); ok {
				for _, part := range parts {
					if p, ok := part.(map[string]any); ok && p["type"] == "text" {
						if text, ok := p["text"].(string); ok && strings.TrimSpace(text) != "" {
							systemMessages = append(systemMessages, map[string]any{
								"type": "text",
								"text": text,
							})
						}
					}
				}
			}
			continue
		}

		// Handle interleaved role case - ensure first message is user
		if lastRole == "" && role != "user" {
			resultMessages = append(resultMessages, map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "...",
					},
				},
			})
		}

		// Tool messages become tool_result items
		if role == "tool" {
			toolResult := map[string]any{
				"type":      "tool_result",
				"tool_use_id": m["tool_call_id"],
				"content":   m["content"],
			}
			if len(resultMessages) > 0 {
				if last, ok := resultMessages[len(resultMessages)-1].(map[string]any); ok && last["role"] == "user" {
					// Append to last user message
					if content, ok := last["content"].([]any); ok {
						last["content"] = append(content, toolResult)
					} else if content, ok := last["content"].(string); ok {
						last["content"] = []any{
							map[string]any{"type": "text", "text": content},
							toolResult,
						}
					}
					continue
				}
			}
			// Create a new user message with the tool result
			resultMessages = append(resultMessages, map[string]any{
				"role":    "user",
				"content": []any{toolResult},
			})
			lastRole = "user"
			continue
		}

		// Handle assistant messages with tool calls
		if role == "assistant" {
			claudeMsg := map[string]any{
				"role": "assistant",
			}
			var content []any

			// Text content
			if text, ok := m["content"].(string); ok && text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": text,
				})
			} else if parts, ok := m["content"].([]any); ok {
				for _, part := range parts {
					content = append(content, part)
				}
			}

			// Tool calls
			if toolCalls, ok := m["tool_calls"].([]any); ok {
				for _, tc := range toolCalls {
					if toolCall, ok := tc.(map[string]any); ok {
						if function, ok := toolCall["function"].(map[string]any); ok {
							var args map[string]any
							_ = common.Unmarshal([]byte(function["arguments"].(string)), &args)
							content = append(content, map[string]any{
								"type":  "tool_use",
								"id":    toolCall["id"],
								"name":  function["name"],
								"input": args,
							})
						}
					}
				}
			}

			if len(content) > 0 {
				claudeMsg["content"] = content
			}
			resultMessages = append(resultMessages, claudeMsg)
			lastRole = role
			continue
		}

		// User messages
		if role == "user" {
			claudeMsg := map[string]any{
				"role": "user",
			}

			if text, ok := m["content"].(string); ok && text != "" {
				claudeMsg["content"] = text
			} else if parts, ok := m["content"].([]any); ok {
				var content []any
				for _, part := range parts {
					if p, ok := part.(map[string]any); ok {
						switch p["type"] {
						case "text":
							content = append(content, map[string]any{
								"type": "text",
								"text": p["text"],
							})
						case "image_url":
							if imageURL, ok := p["image_url"].(map[string]any); ok {
								if url, ok := imageURL["url"].(string); ok {
									content = append(content, map[string]any{
										"type": "image",
										"source": map[string]any{
											"type": "url",
											"url":  url,
										},
									})
								}
							}
						}
					}
				}
				if len(content) > 0 {
					claudeMsg["content"] = content
				}
			}

			// Merge with previous user message if role is same
			if lastRole == "user" && len(resultMessages) > 0 {
				if last, ok := resultMessages[len(resultMessages)-1].(map[string]any); ok {
					if existingText, ok := last["content"].(string); ok {
						if newText, ok := claudeMsg["content"].(string); ok {
							last["content"] = existingText + "\n" + newText
						}
					}
					continue
				}
			}

			resultMessages = append(resultMessages, claudeMsg)
			lastRole = role
			continue
		}
	}

	return systemMessages, resultMessages
}

// StreamResponseClaudeToOpenAI converts Claude streaming response chunks to OpenAI format
func StreamResponseClaudeToOpenAI(ctx context.Context, body []byte) ([]byte, error) {
	var data map[string]any
	if err := common.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	responseType, _ := data["type"].(string)
	result := map[string]any{
		"object":  "chat.completion.chunk",
		"choices": []any{},
	}

	switch responseType {
	case "message_start":
		if msg, ok := data["message"].(map[string]any); ok {
			result["id"] = msg["id"]
			result["model"] = msg["model"]
		}
		choice := map[string]any{
			"delta":        map[string]any{"role": "assistant", "content": ""},
			"finish_reason": nil,
		}
		result["choices"] = []any{choice}

	case "content_block_start":
		if block, ok := data["content_block"].(map[string]any); ok {
			if block["type"] == "text" {
				if text, ok := block["text"].(string); ok {
					choice := map[string]any{
						"delta": map[string]any{
							"content": text,
						},
					}
					result["choices"] = []any{choice}
				}
			} else if block["type"] == "tool_use" {
				choice := map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0,
								"id":    block["id"],
								"type":  "function",
								"function": map[string]any{
									"name":      block["name"],
									"arguments": "",
								},
							},
						},
					},
				}
				result["choices"] = []any{choice}
			}
		}

	case "content_block_delta":
		if delta, ok := data["delta"].(map[string]any); ok {
			if text, ok := delta["text"].(string); ok {
				choice := map[string]any{
					"delta": map[string]any{
						"content": text,
					},
				}
				result["choices"] = []any{choice}
			}
			if delta["type"] == "input_json_delta" {
				if partial, ok := delta["partial_json"].(string); ok {
					choice := map[string]any{
						"delta": map[string]any{
							"tool_calls": []any{
								map[string]any{
									"index": 0,
									"function": map[string]any{
										"arguments": partial,
									},
								},
							},
						},
					}
					result["choices"] = []any{choice}
				}
			}
			if thinking, ok := delta["thinking"].(string); ok {
				choice := map[string]any{
					"delta": map[string]any{
						"reasoning_content": thinking,
					},
				}
				result["choices"] = []any{choice}
			}
		}

	case "message_delta":
		if delta, ok := data["delta"].(map[string]any); ok {
			finishReason, _ := delta["stop_reason"].(string)
			choice := map[string]any{
				"delta":        map[string]any{},
				"finish_reason": mapClaudeStopReason(finishReason),
			}
			result["choices"] = []any{choice}
		}

	case "message_stop":
		// Return nil to indicate we should skip this event
		return nil, nil

	default:
		return nil, nil
	}

	return common.Marshal(result)
}

// ResponseClaudeToOpenAI converts Claude non-streaming response to OpenAI format
func ResponseClaudeToOpenAI(body []byte) ([]byte, error) {
	var data map[string]any
	if err := common.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	result := map[string]any{
		"id":      data["id"],
		"object":  "chat.completion",
		"created": common.GetTimestamp(),
		"model":   data["model"],
	}

	var textContent string
	var reasoningContent string
	var toolCalls []any

	if content, ok := data["content"].([]any); ok {
		for _, item := range content {
			if block, ok := item.(map[string]any); ok {
				switch block["type"] {
				case "text":
					textContent, _ = block["text"].(string)
				case "thinking":
					if thinking, ok := block["thinking"].(string); ok {
						reasoningContent = thinking
					}
				case "tool_use":
					args, _ := common.Marshal(block["input"])
					toolCalls = append(toolCalls, map[string]any{
						"id":   block["id"],
						"type": "function",
						"function": map[string]any{
							"name":      block["name"],
							"arguments": string(args),
						},
					})
				}
			}
		}
	}

	stopReason, _ := data["stop_reason"].(string)
	finishReason := mapClaudeStopReason(stopReason)

	message := map[string]any{
		"role": "assistant",
	}

	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		message["content"] = ""
	} else {
		message["content"] = textContent
	}
	if reasoningContent != "" {
		message["reasoning_content"] = reasoningContent
	}

	choice := map[string]any{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}

	result["choices"] = []any{choice}

	if usage, ok := data["usage"].(map[string]any); ok {
		result["usage"] = mapClaudeUsageToOpenAI(usage)
	}

	return common.Marshal(result)
}

func mapClaudeStopReason(reason string) string {
	switch reason {
	case "end_turn", "":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

func mapClaudeUsageToOpenAI(usage map[string]any) map[string]any {
	result := make(map[string]any)
	input := 0
	output := 0
	if inputTokens, ok := usage["input_tokens"]; ok {
		input = interface2Int(inputTokens)
		result["prompt_tokens"] = input
	}
	if outputTokens, ok := usage["output_tokens"]; ok {
		output = interface2Int(outputTokens)
		result["completion_tokens"] = output
	}
	result["total_tokens"] = input + output
	return result
}

// ============================================================================
// Reverse Direction: Claude Messages API -> OpenAI Chat Completions API
// Use case: Client calls Claude format, upstream is OpenAI-compatible channel
// ============================================================================

// RequestClaudeToOpenAI converts Claude Messages request to OpenAI Chat format
// This is for the scenario: Client speaks Claude, upstream is OpenAI
func RequestClaudeToOpenAI(body []byte, flags TransformFlags) ([]byte, error) {
	var req map[string]any
	if err := common.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	result := make(map[string]any)

	// Model
	if model, ok := req["model"].(string); ok {
		result["model"] = model
	}

	// Temperature
	if temp, ok := req["temperature"]; ok {
		result["temperature"] = temp
	}

	// Top P
	if topP, ok := req["top_p"]; ok {
		result["top_p"] = topP
	}

	// Max tokens - handle both field names
	if maxTokens, ok := req["max_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}
	if maxTokens, ok := req["max_output_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}

	// Stop sequences
	if stop, ok := req["stop_sequences"].([]any); ok && len(stop) > 0 {
		result["stop"] = stop
	}

	// Stream
	if stream, ok := req["stream"]; ok {
		result["stream"] = stream
	}

	// System prompt -> prepend as system message
	var messages []any
	if system, ok := req["system"]; ok {
		if sysStr, ok := system.(string); ok && sysStr != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": sysStr,
			})
		} else if sysArr, ok := system.([]any); ok {
			// Array system format from newer Claude API
			var sb strings.Builder
			for _, item := range sysArr {
				if m, ok := item.(map[string]any); ok {
					if m["type"] == "text" {
						if text, ok := m["text"].(string); ok {
							sb.WriteString(text)
							sb.WriteString("\n")
						}
					}
				}
			}
			if sb.Len() > 0 {
				messages = append(messages, map[string]any{
					"role":    "system",
					"content": strings.TrimSpace(sb.String()),
				})
			}
		}
	}

	// Regular messages
	if claudeMessages, ok := req["messages"].([]any); ok {
		for _, msg := range claudeMessages {
			if m, ok := msg.(map[string]any); ok {
				openAIMsgs := convertClaudeMessageToOpenAI(m)
				messages = append(messages, openAIMsgs...)
			}
		}
	}

	result["messages"] = messages

	// Tools
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		var openAITools []any
		for _, tool := range tools {
			if t, ok := tool.(map[string]any); ok {
				openAITool := map[string]any{
					"type": "function",
					"function": map[string]any{
						"name":        t["name"],
						"description": t["description"],
						"parameters":  t["input_schema"],
					},
				}
				openAITools = append(openAITools, openAITool)
			}
		}
		result["tools"] = openAITools
	}

	// Tool choice
	if tc, ok := req["tool_choice"].(map[string]any); ok {
		if disabled, ok := tc["disable_parallel_tool_use"].(bool); ok {
			result["parallel_tool_calls"] = !disabled
		}
		switch tc["type"] {
		case "auto", "none":
			result["tool_choice"] = tc["type"]
		case "any":
			result["tool_choice"] = "required"
		case "tool":
			result["tool_choice"] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": tc["name"],
				},
			}
		}
	}

	// Thinking -> reasoning_effort
	if thinking, ok := req["thinking"].(map[string]any); ok {
		if typ, ok := thinking["type"].(string); ok {
			if typ == "adaptive" {
				result["reasoning_effort"] = "high"
			} else if typ == "enabled" {
				result["reasoning_effort"] = "medium"
			} else if typ == "disabled" {
				result["reasoning_effort"] = "none"
			}
		}
	}
	if config, ok := req["output_config"].(map[string]any); ok {
		if effort, ok := config["effort"].(string); ok && effort != "" {
			result["reasoning_effort"] = effort
		}
	}

	return common.Marshal(result)
}

// convertClaudeMessageToOpenAI converts a single Claude message to OpenAI format(s)
// Handles: text content, images, tool_use, tool_result
func convertClaudeMessageToOpenAI(msg map[string]any) []any {
	var result []any

	role, _ := msg["role"].(string)
	content := msg["content"]

	// Simple string content
	if text, ok := content.(string); ok {
		result = append(result, map[string]any{
			"role":    role,
			"content": text,
		})
		return result
	}

	// Array content - may have text, images, tool_use, tool_result
	if parts, ok := content.([]any); ok {
		var textContent strings.Builder
		var reasoningContent strings.Builder
		var multiModal []any
		var toolCalls []any

		for _, part := range parts {
			if p, ok := part.(map[string]any); ok {
				switch p["type"] {
				case "thinking":
					if thinking, ok := p["thinking"].(string); ok && role == "assistant" {
						reasoningContent.WriteString(thinking)
					}
				case "text":
					if text, ok := p["text"].(string); ok {
						textContent.WriteString(text)
					}

				case "image":
					imgUrl := ""
					if source, ok := p["source"].(map[string]any); ok {
						if source["type"] == "url" {
							imgUrl, _ = source["url"].(string)
						} else if source["type"] == "base64" {
							mediaType, _ := source["media_type"].(string)
							data, _ := source["data"].(string)
							imgUrl = "data:" + mediaType + ";base64," + data
						}
					}
					if imgUrl != "" {
						multiModal = append(multiModal, map[string]any{
							"type": "image_url",
							"image_url": map[string]any{
								"url": imgUrl,
							},
						})
					}

				case "tool_use":
					argsBytes, _ := common.Marshal(p["input"])
					toolCalls = append(toolCalls, map[string]any{
						"id":   p["id"],
						"type": "function",
						"function": map[string]any{
							"name":      p["name"],
							"arguments": string(argsBytes),
						},
					})

				case "tool_result":
					// Tool result becomes a separate tool message
					var contentStr string
					if c, ok := p["content"].(string); ok {
						contentStr = c
					} else if c, ok := p["content"].([]any); ok && len(c) > 0 {
						var text strings.Builder
						for _, item := range c {
							if block, ok := item.(map[string]any); ok && block["type"] == "text" {
								if value, ok := block["text"].(string); ok {
									text.WriteString(value)
								}
							}
						}
						contentStr = text.String()
					}
					toolMsg := map[string]any{
						"role":         "tool",
						"content":      contentStr,
						"tool_call_id": p["tool_use_id"],
					}
					result = append(result, toolMsg)
				}
			}
		}

		// Build the main message
		mainMsg := map[string]any{
			"role": role,
		}

		// Text + multi-modal content
		if textContent.Len() > 0 || len(multiModal) > 0 {
			if len(multiModal) == 0 {
				mainMsg["content"] = textContent.String()
			} else {
				if textContent.Len() > 0 {
					multiModal = append([]any{map[string]any{
						"type": "text",
						"text": textContent.String(),
					}}, multiModal...)
				}
				mainMsg["content"] = multiModal
			}
		} else {
			mainMsg["content"] = ""
		}

		// Tool calls for assistant messages
		if len(toolCalls) > 0 && role == "assistant" {
			mainMsg["tool_calls"] = toolCalls
		}
		if reasoningContent.Len() > 0 {
			mainMsg["reasoning_content"] = reasoningContent.String()
		}

		if textContent.Len() > 0 || len(multiModal) > 0 || len(toolCalls) > 0 || reasoningContent.Len() > 0 || len(result) == 0 {
			result = append(result, mainMsg)
		}
	}

	return result
}

// ResponseOpenAIToClaude converts OpenAI Chat response to Claude Messages format
// This is for the scenario: upstream returns OpenAI, client expects Claude
func ResponseOpenAIToClaude(body []byte) ([]byte, error) {
	var resp map[string]any
	if err := common.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	result := map[string]any{
		"id":           resp["id"],
		"type":         "message",
		"role":         "assistant",
		"model":        resp["model"],
		"stop_reason":  "end_turn",
		"content":      []any{},
		"usage":        map[string]any{},
	}

	// Extract content from choices
	if choices, ok := resp["choices"].([]any); ok && len(choices) > 0 {
		choice := choices[0].(map[string]any)
		if message, ok := choice["message"].(map[string]any); ok {
			var content []any

			// Text content
			if text, ok := message["content"].(string); ok && text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": text,
				})
			}

			// Tool calls
			if toolCalls, ok := message["tool_calls"].([]any); ok {
				for _, tc := range toolCalls {
					if toolCall, ok := tc.(map[string]any); ok {
						if function, ok := toolCall["function"].(map[string]any); ok {
							var args map[string]any
							_ = common.Unmarshal([]byte(function["arguments"].(string)), &args)
							content = append(content, map[string]any{
								"type":  "tool_use",
								"id":    toolCall["id"],
								"name":  function["name"],
								"input": args,
							})
						}
					}
				}
			}

			result["content"] = content

			// Stop reason mapping
			if finishReason, ok := choice["finish_reason"].(string); ok {
				switch finishReason {
				case "stop", "length", "content_filter":
					result["stop_reason"] = "end_turn"
				case "tool_calls":
					result["stop_reason"] = "tool_use"
				default:
					result["stop_reason"] = finishReason
				}
			}
		}
	}

	// Usage mapping
	if usage, ok := resp["usage"].(map[string]any); ok {
		claudeUsage := map[string]any{
			"input_tokens":  usage["prompt_tokens"],
			"output_tokens": usage["completion_tokens"],
		}
		if cached, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			claudeUsage["cache_read_input_tokens"] = cached["cached_tokens"]
		}
		result["usage"] = claudeUsage
	}

	return common.Marshal(result)
}

// StreamResponseOpenAIToClaude converts OpenAI streaming chunk to Claude SSE format
func StreamResponseOpenAIToClaude(body []byte) ([][]byte, error) {
	var chunk map[string]any
	if err := common.Unmarshal(body, &chunk); err != nil {
		return nil, err
	}

	var result [][]byte
	chunkId, _ := chunk["id"].(string)

	// First message event - only on stream start
	if firstChunk, ok := chunk["first_chunk"].(bool); ok && firstChunk {
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    chunkId,
				"type":  "message",
				"role":  "assistant",
				"model": chunk["model"],
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}
		startBytes, _ := common.Marshal(msgStart)
		result = append(result, startBytes)
	}

	// Content block delta
	if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
		choice := choices[0].(map[string]any)
		if delta, ok := choice["delta"].(map[string]any); ok {
			// Text content
			if text, ok := delta["content"].(string); ok && text != "" {
				contentDelta := map[string]any{
					"type": "content_block_delta",
					"index": 0,
					"delta": map[string]any{
						"type": "text_delta",
						"text": text,
					},
				}
				deltaBytes, _ := common.Marshal(contentDelta)
				result = append(result, deltaBytes)
			}

			// Tool calls - simplified
			if toolCalls, ok := delta["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				for _, tc := range toolCalls {
					if toolCall, ok := tc.(map[string]any); ok {
						if function, ok := toolCall["function"].(map[string]any); ok {
							toolDelta := map[string]any{
								"type":  "content_block_delta",
								"index": toolCall["index"],
								"delta": map[string]any{
									"type":         "input_json_delta",
									"partial_json": function["arguments"],
								},
							}
							deltaBytes, _ := common.Marshal(toolDelta)
							result = append(result, deltaBytes)
						}
					}
				}
			}
		}

		// Finish reason
		if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
			stopReason := "end_turn"
			if finishReason == "tool_calls" {
				stopReason = "tool_use"
			}
			msgDelta := map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason": stopReason,
				},
				"usage": map[string]any{
					"output_tokens": 0,
				},
			}
			deltaBytes, _ := common.Marshal(msgDelta)
			result = append(result, deltaBytes)

			// Message stop
			msgStop := map[string]any{
				"type": "message_stop",
			}
			stopBytes, _ := common.Marshal(msgStop)
			result = append(result, stopBytes)
		}
	}

	return result, nil
}
