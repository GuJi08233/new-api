package mistral

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Mistral reasoning models return chat `content` as an array of chunks, e.g.
// [{"type":"thinking","thinking":[{"type":"text","text":"..."}]},{"type":"text","text":"..."}].
// OpenAI-compatible clients (and our DTOs) expect a string, so thinking chunks are
// moved into `reasoning_content` and text chunks are joined into `content`.
type mistralContentChunk struct {
	Type     string                `json:"type"`
	Text     string                `json:"text"`
	Thinking []mistralContentChunk `json:"thinking"`
}

func flattenMistralChunks(chunks []mistralContentChunk, text, reasoning *strings.Builder) {
	for _, chunk := range chunks {
		switch chunk.Type {
		case "thinking":
			for _, inner := range chunk.Thinking {
				reasoning.WriteString(inner.Text)
			}
		case "text":
			text.WriteString(chunk.Text)
		}
	}
}

// normalizeMistralChatPayload rewrites array-form content in choices[].message / choices[].delta.
// It returns the original payload unchanged when nothing needs rewriting or parsing fails.
func normalizeMistralChatPayload(payload []byte, stream bool) []byte {
	if !bytes.Contains(payload, []byte(`"content"`)) {
		return payload
	}
	var root map[string]json.RawMessage
	if err := common.Unmarshal(payload, &root); err != nil {
		return payload
	}
	var choices []map[string]json.RawMessage
	if err := common.Unmarshal(root["choices"], &choices); err != nil {
		return payload
	}
	messageKey := "message"
	if stream {
		messageKey = "delta"
	}
	changed := false
	for _, choice := range choices {
		var message map[string]json.RawMessage
		if err := common.Unmarshal(choice[messageKey], &message); err != nil || message == nil {
			continue
		}
		var chunks []mistralContentChunk
		if common.GetJsonType(message["content"]) != "array" || common.Unmarshal(message["content"], &chunks) != nil {
			continue
		}
		var text, reasoning strings.Builder
		flattenMistralChunks(chunks, &text, &reasoning)
		if text.Len() > 0 || !stream {
			message["content"], _ = common.Marshal(text.String())
		} else {
			delete(message, "content")
		}
		if reasoning.Len() > 0 {
			message["reasoning_content"], _ = common.Marshal(reasoning.String())
		}
		encoded, err := common.Marshal(message)
		if err != nil {
			return payload
		}
		choice[messageKey] = encoded
		changed = true
	}
	if !changed {
		return payload
	}
	encodedChoices, err := common.Marshal(choices)
	if err != nil {
		return payload
	}
	root["choices"] = encodedChoices
	out, err := common.Marshal(root)
	if err != nil {
		return payload
	}
	return out
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
