package mistral

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func isJSONValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// normalizeMistralImageDetail maps image detail values onto the subset
// accepted by Mistral's Chat and Responses APIs ('low', 'auto', 'high').
func normalizeMistralImageDetail(detail string) string {
	switch strings.ToLower(strings.TrimSpace(detail)) {
	case "low", "auto", "high":
		return strings.ToLower(strings.TrimSpace(detail))
	case "original":
		return "high"
	default:
		return ""
	}
}

// normalizeMistralReasoningEffort maps standard OpenAI/Claude reasoning effort
// values onto the subset accepted by each Mistral-hosted model family so
// standard clients do not fail with upstream 400 validation errors.
func normalizeMistralReasoningEffort(model, effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return ""
	}
	if effort == "off" {
		effort = "none"
	}
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return ""
	}

	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "deepseek-v4"):
		// Verified live: supported values are ['none', 'low', 'high', 'xhigh', 'max'].
		switch effort {
		case "minimal":
			return "low"
		case "medium":
			return "high"
		default:
			return effort
		}
	case strings.HasPrefix(m, "zai-glm-5-3") || m == "zai-glm-5" || m == "zai-glm-latest":
		// Verified live: supported values are ['low', 'high', 'max'].
		switch effort {
		case "none", "minimal":
			return "low"
		case "medium":
			return "high"
		case "xhigh":
			return "max"
		default:
			return effort
		}
	case strings.HasPrefix(m, "mistral-large-4") || m == "mistral-large-latest":
		// Verified live: supported values are ['low', 'medium', 'high'].
		switch effort {
		case "none", "minimal":
			return "low"
		case "xhigh", "max":
			return "high"
		default:
			return effort
		}
	case strings.HasPrefix(m, "mistral-medium-3-5") ||
		m == "mistral-medium-3.5" ||
		m == "mistral-medium-2604" ||
		m == "mistral-medium-latest" ||
		m == "mistral-medium" ||
		strings.HasPrefix(m, "mistral-small-2603") ||
		m == "mistral-small-latest":
		// Verified live: supported values are ['none', 'high'].
		switch effort {
		case "none", "minimal":
			return "none"
		default:
			return "high"
		}
	default:
		return effort
	}
}

// stringifyMistralMetadata ensures every metadata value is a string as required
// by Mistral's OpenAI-compatible Chat and Responses APIs (which reject
// numeric/boolean/object values).
func stringifyMistralMetadata(raw json.RawMessage) json.RawMessage {
	if !isJSONValuePresent(raw) {
		return nil
	}
	var obj map[string]any
	if err := common.Unmarshal(raw, &obj); err != nil || len(obj) == 0 {
		return nil
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case string:
			out[k] = val
		default:
			if encoded, err := common.Marshal(val); err == nil {
				out[k] = string(encoded)
			} else {
				out[k] = fmt.Sprint(val)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	encoded, err := common.Marshal(out)
	if err != nil {
		return nil
	}
	return encoded
}
