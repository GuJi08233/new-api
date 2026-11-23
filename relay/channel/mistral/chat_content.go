package mistral

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// normalizeMistralChatPayload normalizes Mistral-specific Chat response shapes:
// - strips provider SSE padding field "p"
// - maps finish_reason "model_length" to "length"
// - flattens array-form content (thinking + text chunks) into content / reasoning_content
// - ensures tool_calls[].function.arguments is always a JSON string
func normalizeMistralChatPayload(payload []byte, stream bool) []byte {
	if !bytes.Contains(payload, []byte(`"content"`)) &&
		!bytes.Contains(payload, []byte(`"finish_reason"`)) &&
		!bytes.Contains(payload, []byte(`"tool_calls"`)) &&
		!bytes.Contains(payload, []byte(`"p":`)) {
		return payload
	}
	var root map[string]json.RawMessage
	if err := common.Unmarshal(payload, &root); err != nil {
		return payload
	}
	changed := false
	if _, hasPadding := root["p"]; hasPadding {
		delete(root, "p")
		changed = true
	}
	var choices []map[string]json.RawMessage
	if err := common.Unmarshal(root["choices"], &choices); err == nil {
		messageKey := "message"
		if stream {
			messageKey = "delta"
		}
		choicesChanged := false
		for _, choice := range choices {
			if bytes.Equal(bytes.TrimSpace(choice["finish_reason"]), []byte(`"model_length"`)) {
				choice["finish_reason"] = json.RawMessage(`"length"`)
				choicesChanged = true
			}
			var message map[string]json.RawMessage
			if err := common.Unmarshal(choice[messageKey], &message); err != nil || message == nil {
				continue
			}
			msgChanged := false
			if common.GetJsonType(message["content"]) == "array" {
				var rawChunks []any
				if common.Unmarshal(message["content"], &rawChunks) == nil {
					var text, reasoning strings.Builder
					for _, blockValue := range rawChunks {
						switch block := blockValue.(type) {
						case string:
							text.WriteString(block)
						case map[string]any:
							if block["type"] == "thinking" {
								appendMistralThinkingText(&reasoning, block["thinking"])
								continue
							}
							if s, ok := block["text"].(string); ok {
								text.WriteString(s)
							}
						}
					}
					if text.Len() > 0 || !stream {
						message["content"], _ = common.Marshal(text.String())
					} else {
						delete(message, "content")
					}
					if reasoning.Len() > 0 {
						var existing string
						_ = common.Unmarshal(message["reasoning_content"], &existing)
						message["reasoning_content"], _ = common.Marshal(existing + reasoning.String())
					}
					msgChanged = true
				}
			}
			if common.GetJsonType(message["tool_calls"]) == "array" {
				var toolCalls []map[string]json.RawMessage
				if common.Unmarshal(message["tool_calls"], &toolCalls) == nil {
					tcChanged := false
					for _, tc := range toolCalls {
						if common.GetJsonType(tc["function"]) != "object" {
							continue
						}
						var fn map[string]json.RawMessage
						if common.Unmarshal(tc["function"], &fn) != nil {
							continue
						}
						if common.GetJsonType(fn["arguments"]) == "object" {
							if encoded, err := common.Marshal(string(fn["arguments"])); err == nil {
								fn["arguments"] = encoded
								if fnRaw, err := common.Marshal(fn); err == nil {
									tc["function"] = fnRaw
									tcChanged = true
								}
							}
						}
					}
					if tcChanged {
						if encodedCalls, err := common.Marshal(toolCalls); err == nil {
							message["tool_calls"] = encodedCalls
							msgChanged = true
						}
					}
				}
			}
			if msgChanged {
				encoded, err := common.Marshal(message)
				if err != nil {
					return payload
				}
				choice[messageKey] = encoded
				choicesChanged = true
			}
		}
		if choicesChanged {
			encodedChoices, err := common.Marshal(choices)
			if err != nil {
				return payload
			}
			root["choices"] = encodedChoices
			changed = true
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

func appendMistralThinkingText(builder *strings.Builder, thinkingValue any) {
	switch thinking := thinkingValue.(type) {
	case string:
		builder.WriteString(thinking)
	case []any:
		for _, itemValue := range thinking {
			switch item := itemValue.(type) {
			case string:
				builder.WriteString(item)
			case map[string]any:
				if text, ok := item["text"].(string); ok {
					builder.WriteString(text)
				}
			}
		}
	}
}

type normalizedBody struct {
	io.Reader
	closers []io.Closer
}

func (b *normalizedBody) Close() error {
	var firstErr error
	for _, c := range b.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func normalizeMistralChatBody(body io.ReadCloser, stream bool) (io.ReadCloser, error) {
	if !stream {
		data, err := io.ReadAll(body)
		_ = body.Close()
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(normalizeMistralChatPayload(data, false))), nil
	}
	pr, pw := io.Pipe()
	go func() {
		reader := bufio.NewReader(body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
					trimmed := bytes.TrimSpace(data)
					normalized := normalizeMistralChatPayload(trimmed, true)
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
