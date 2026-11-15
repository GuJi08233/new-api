package dto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	MaxGeminiDecisionsQuestions   = 1000
	MaxGeminiDecisionsCriteria    = 255
	MaxGeminiDecisionsInlineBytes = 100_000_000
	MaxGeminiDecisionsPDFBytes    = 50_000_000
)

type GeminiDecisionQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

type GeminiDecisionsRequest struct {
	Model     string                            `json:"model"`
	Input     json.RawMessage                   `json:"input"`
	Questions map[string]GeminiDecisionQuestion `json:"questions"`
}

// 这些内容块仅用于校验和估算；转发时仍使用原始 input/instructions。
type geminiDecisionContent struct {
	Type       string  `json:"type"`
	Text       *string `json:"text,omitempty"`
	Data       *string `json:"data,omitempty"`
	URI        *string `json:"uri,omitempty"`
	MimeType   *string `json:"mime_type,omitempty"`
	Resolution *string `json:"resolution,omitempty"`
}

func (r *GeminiDecisionsRequest) IsStream(c *gin.Context) bool { return false }

func (r *GeminiDecisionsRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *GeminiDecisionsRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("model is required")
	}
	if _, _, err := parseGeminiDecisionContent(r.Input, false); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if len(r.Questions) == 0 || len(r.Questions) > MaxGeminiDecisionsQuestions {
		return fmt.Errorf("questions must contain 1 to %d entries", MaxGeminiDecisionsQuestions)
	}
	for id, question := range r.Questions {
		if len(id) == 0 || len(id) > 64 || strings.IndexFunc(id, func(ch rune) bool {
			return !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-')
		}) != -1 {
			return errors.New("question keys must contain 1 to 64 ASCII letters, digits, underscores, or hyphens")
		}
		if _, _, err := parseGeminiDecisionContent(question.Instructions, true); err != nil {
			return fmt.Errorf("question %s instructions: %w", id, err)
		}
		switch question.Type {
		case "noul":
			if len(question.Criteria) > 0 && common.GetJsonType(question.Criteria) != "null" {
				var criteria map[string]string
				if err := common.Unmarshal(question.Criteria, &criteria); err != nil || common.GetJsonType(question.Criteria) != "object" {
					return fmt.Errorf("question %s: noul criteria must be an object of descriptions", id)
				}
			}
		case "choice":
			var criteria map[string]string
			if err := common.Unmarshal(question.Criteria, &criteria); err != nil || len(criteria) < 2 || len(criteria) > MaxGeminiDecisionsCriteria {
				return fmt.Errorf("question %s: choice criteria must contain 2 to %d options", id, MaxGeminiDecisionsCriteria)
			}
			for key := range criteria {
				if len(key) == 0 || len(key) > 512 || utf8.RuneCountInString(key) > 128 || strings.TrimSpace(key) != key || strings.IndexFunc(key, func(ch rune) bool {
					return unicode.IsControl(ch) || unicode.IsSpace(ch) && ch != ' '
				}) != -1 {
					return fmt.Errorf("question %s: invalid choice option key", id)
				}
			}
		case "score":
			var criteria []string
			if err := common.Unmarshal(question.Criteria, &criteria); err != nil || len(criteria) < 2 || len(criteria) > MaxGeminiDecisionsCriteria {
				return fmt.Errorf("question %s: score criteria must contain 2 to %d levels", id, MaxGeminiDecisionsCriteria)
			}
			for _, level := range criteria {
				if strings.TrimSpace(level) == "" {
					return fmt.Errorf("question %s: score levels must not be empty", id)
				}
			}
		default:
			return fmt.Errorf("question %s: type must be noul, choice, or score", id)
		}
	}
	return nil
}

func (r *GeminiDecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	inputTexts, files, _ := parseGeminiDecisionContent(r.Input, false)
	texts := []string{strings.Join(inputTexts, "")}
	ids := make([]string, 0, len(r.Questions))
	for id := range r.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// 问题 key 不进入模型；instructions 与 criteria 都计入输入估算。
	for _, id := range ids {
		question := r.Questions[id]
		instructions, _, _ := parseGeminiDecisionContent(question.Instructions, true)
		texts = append(texts, strings.Join(instructions, ""))
		if len(question.Criteria) > 0 {
			texts = append(texts, common.JsonRawMessageToString(question.Criteria))
		}
	}
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: strings.Join(texts, "\n"), Files: files, MessagesCount: len(ids)}
}

func parseGeminiDecisionContent(raw json.RawMessage, textOnly bool) ([]string, []*types.FileMeta, error) {
	if common.GetJsonType(raw) == "string" {
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return nil, nil, err
		}
		if text == "" {
			return nil, nil, errors.New("text must not be empty")
		}
		return []string{text}, nil, nil
	}
	var blocks []geminiDecisionContent
	switch common.GetJsonType(raw) {
	case "object":
		var block geminiDecisionContent
		if err := common.Unmarshal(raw, &block); err != nil {
			return nil, nil, err
		}
		blocks = []geminiDecisionContent{block}
	case "array":
		if err := common.Unmarshal(raw, &blocks); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, errors.New("must be text or content blocks")
	}
	if len(blocks) == 0 {
		return nil, nil, errors.New("content must not be empty")
	}
	var texts []string
	var files []*types.FileMeta
	var totalBytes int64
	for i, block := range blocks {
		if block.Type == "text" {
			if block.Text == nil {
				return nil, nil, fmt.Errorf("block %d: text is required", i)
			}
			texts = append(texts, *block.Text)
			continue
		}
		if textOnly {
			return nil, nil, errors.New("instructions only support text blocks")
		}
		var fileType types.FileType
		switch block.Type {
		case "image":
			fileType = types.FileTypeImage
		case "audio":
			fileType = types.FileTypeAudio
		case "video":
			fileType = types.FileTypeVideo
		case "document":
			fileType = types.FileTypeFile
		default:
			return nil, nil, fmt.Errorf("block %d: unsupported content type %s", i, block.Type)
		}
		if block.MimeType == nil || (block.Type == "document" && *block.MimeType != "application/pdf") || (block.Type != "document" && !strings.HasPrefix(*block.MimeType, block.Type+"/")) {
			return nil, nil, fmt.Errorf("block %d: mime_type must match the content type", i)
		}
		if (block.Data == nil) == (block.URI == nil) {
			return nil, nil, fmt.Errorf("block %d: provide exactly one of data or uri", i)
		}
		var source types.FileSource
		if block.Data != nil {
			if *block.Data == "" {
				return nil, nil, fmt.Errorf("block %d: data must not be empty", i)
			}
			limit := int64(MaxGeminiDecisionsInlineBytes)
			if block.Type == "document" {
				limit = MaxGeminiDecisionsPDFBytes
			}
			decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(*block.Data))
			size, err := io.Copy(io.Discard, io.LimitReader(decoder, limit+1))
			if err != nil || size > limit {
				return nil, nil, fmt.Errorf("block %d: invalid base64 data or inline media size limit exceeded", i)
			}
			totalBytes += size
			if totalBytes > MaxGeminiDecisionsInlineBytes {
				return nil, nil, errors.New("inline media must not exceed 100 MB per request")
			}
			source = types.NewBase64FileSource(*block.Data, *block.MimeType)
		} else {
			mediaURL, err := url.Parse(*block.URI)
			if err != nil || mediaURL.Scheme != "https" || mediaURL.Host == "" {
				return nil, nil, fmt.Errorf("block %d: uri must be an HTTPS URL or Files API URI", i)
			}
			source = types.NewURLFileSource(*block.URI)
		}
		file := types.NewFileMeta(fileType, source)
		if block.Resolution != nil {
			file.Detail = *block.Resolution
		}
		files = append(files, file)
	}
	if len(files) == 0 && strings.Join(texts, "") == "" {
		return nil, nil, errors.New("text must not be empty")
	}
	return texts, files, nil
}
