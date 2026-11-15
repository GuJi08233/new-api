package dto

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	MaxOpenAIDecisionsQuestions = 400
	MaxOpenAIDecisionsChoices   = 255
	MaxOpenAIDecisionsLevels    = 10
	MaxOpenAIDecisionsParts     = 16384
)

type OpenAIDecisionChoice struct {
	Value       json.RawMessage `json:"value"`
	Description *string         `json:"description,omitempty"`
}

type OpenAIDecisionLevel struct {
	Label       *string `json:"label"`
	Description *string `json:"description,omitempty"`
}

type OpenAIDecisionQuestion struct {
	Type         string                 `json:"type"`
	Name         *string                `json:"name,omitempty"`
	Instructions json.RawMessage        `json:"instructions"`
	Choices      []OpenAIDecisionChoice `json:"choices,omitempty"`
	Levels       []OpenAIDecisionLevel  `json:"levels,omitempty"`
}

type OpenAIDecisionsRequest struct {
	Model              string                   `json:"model"`
	Input              json.RawMessage          `json:"input"`
	Questions          []OpenAIDecisionQuestion `json:"questions"`
	PromptCacheKey     *string                  `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions json.RawMessage          `json:"prompt_cache_options,omitempty"`
	SafetyIdentifier   *string                  `json:"safety_identifier,omitempty"`
}

type openaiDecisionInputItem struct {
	Type      string          `json:"type"`
	Content   json.RawMessage `json:"content"`
	Name      *string         `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
}

type openaiDecisionPart struct {
	Type     string          `json:"type"`
	Text     *string         `json:"text,omitempty"`
	Refusal  *string         `json:"refusal,omitempty"`
	ImageURL *string         `json:"image_url,omitempty"`
	Detail   *string         `json:"detail,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

func (r *OpenAIDecisionsRequest) IsStream(c *gin.Context) bool { return false }

func (r *OpenAIDecisionsRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *OpenAIDecisionsRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("model is required")
	}
	if _, _, err := parseOpenAIDecisionInput(r.Input); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if len(r.Questions) == 0 || len(r.Questions) > MaxOpenAIDecisionsQuestions {
		return fmt.Errorf("questions must contain 1 to %d entries", MaxOpenAIDecisionsQuestions)
	}
	for i, question := range r.Questions {
		if _, _, err := parseOpenAIDecisionContent(question.Instructions, true); err != nil {
			return fmt.Errorf("question %d instructions: %w", i, err)
		}
		switch question.Type {
		case "predicate":
		case "choice":
			if len(question.Choices) < 2 || len(question.Choices) > MaxOpenAIDecisionsChoices {
				return fmt.Errorf("question %d: choices must contain 2 to %d options", i, MaxOpenAIDecisionsChoices)
			}
			values := make(map[string]bool, len(question.Choices))
			for _, choice := range question.Choices {
				kind := common.GetJsonType(choice.Value)
				if kind != "string" && kind != "boolean" {
					return fmt.Errorf("question %d: choice values must be strings or booleans", i)
				}
				value := kind + ":" + common.JsonRawMessageToString(choice.Value)
				if values[value] {
					return fmt.Errorf("question %d: choice values must be unique", i)
				}
				values[value] = true
			}
		case "score":
			if len(question.Levels) < 2 || len(question.Levels) > MaxOpenAIDecisionsLevels {
				return fmt.Errorf("question %d: levels must contain 2 to %d entries", i, MaxOpenAIDecisionsLevels)
			}
			for _, level := range question.Levels {
				if level.Label == nil {
					return fmt.Errorf("question %d: each score level requires a label", i)
				}
			}
		default:
			return fmt.Errorf("question %d: type must be predicate, choice, or score", i)
		}
	}
	return nil
}

func (r *OpenAIDecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	texts, files, _ := parseOpenAIDecisionInput(r.Input)
	for _, question := range r.Questions {
		instructions, questionFiles, _ := parseOpenAIDecisionContent(question.Instructions, true)
		texts = append(texts, instructions...)
		files = append(files, questionFiles...)
		if len(question.Choices) > 0 {
			criteria, _ := common.Marshal(question.Choices)
			texts = append(texts, string(criteria))
		}
		if len(question.Levels) > 0 {
			criteria, _ := common.Marshal(question.Levels)
			texts = append(texts, string(criteria))
		}
	}
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: strings.Join(texts, "\n"), Files: files, MessagesCount: len(r.Questions)}
}

func parseOpenAIDecisionInput(raw json.RawMessage) ([]string, []*types.FileMeta, error) {
	if common.GetJsonType(raw) == "string" {
		return parseOpenAIDecisionContent(raw, false)
	}
	if common.GetJsonType(raw) != "array" {
		return nil, nil, errors.New("must be a string or an array of messages and tool calls")
	}
	var items []openaiDecisionInputItem
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, nil, err
	}
	if len(items) > 131072 {
		return nil, nil, errors.New("input must contain at most 131072 items")
	}
	var texts []string
	var files []*types.FileMeta
	for i, item := range items {
		content := item.Content
		switch item.Type {
		case "", "message":
		case "function_call":
			content = item.Arguments
		case "custom_tool_call":
			content = item.Input
		case "function_call_output", "custom_tool_call_output":
			content = item.Output
		default:
			return nil, nil, fmt.Errorf("item %d: unsupported input type %s", i, item.Type)
		}
		partTexts, partFiles, err := parseOpenAIDecisionContent(content, false)
		if err != nil {
			return nil, nil, fmt.Errorf("item %d: %w", i, err)
		}
		if item.Name != nil {
			texts = append(texts, *item.Name)
		}
		texts = append(texts, partTexts...)
		files = append(files, partFiles...)
	}
	return texts, files, nil
}

func parseOpenAIDecisionContent(raw json.RawMessage, question bool) ([]string, []*types.FileMeta, error) {
	if common.GetJsonType(raw) == "string" {
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return nil, nil, err
		}
		return []string{text}, nil, nil
	}
	if common.GetJsonType(raw) != "array" {
		return nil, nil, errors.New("must be a string or an array of content parts")
	}
	var parts []openaiDecisionPart
	if err := common.Unmarshal(raw, &parts); err != nil {
		return nil, nil, err
	}
	if len(parts) > MaxOpenAIDecisionsParts || question && len(parts) == 0 {
		return nil, nil, fmt.Errorf("content parts exceed the allowed range (at most %d)", MaxOpenAIDecisionsParts)
	}
	var texts []string
	var files []*types.FileMeta
	for i, part := range parts {
		switch part.Type {
		case "input_text", "output_text":
			if part.Text == nil || question && part.Type == "output_text" {
				return nil, nil, fmt.Errorf("part %d: input_text with text is required", i)
			}
			texts = append(texts, *part.Text)
		case "refusal":
			if question || part.Refusal == nil {
				return nil, nil, fmt.Errorf("part %d: invalid refusal content", i)
			}
			texts = append(texts, *part.Refusal)
		case "input_json":
			kind := common.GetJsonType(part.Data)
			if kind != "object" && kind != "array" {
				return nil, nil, fmt.Errorf("part %d: input_json data must be an object or array", i)
			}
			texts = append(texts, string(part.Data))
		case "input_image":
			if part.ImageURL == nil || !(strings.HasPrefix(*part.ImageURL, "data:image/") || strings.HasPrefix(*part.ImageURL, "https://") || strings.HasPrefix(*part.ImageURL, "http://")) {
				return nil, nil, fmt.Errorf("part %d: image_url must be an image data URL or HTTP(S) URL", i)
			}
			detail := ""
			if part.Detail != nil {
				detail = *part.Detail
			}
			files = append(files, types.NewImageFileMeta(types.NewFileSourceFromData(*part.ImageURL, ""), detail))
		default:
			return nil, nil, fmt.Errorf("part %d: unsupported content type %s", i, part.Type)
		}
	}
	return texts, files, nil
}
