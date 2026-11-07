package dto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// TypeSafe System One question types. A question's type selects both the shape
// of the criteria it must carry and the shape of the answer that comes back.
const (
	TypeSafeQuestionNoul   = "noul"
	TypeSafeQuestionChoice = "choice"
	TypeSafeQuestionScore  = "score"
)

// MaxTypeSafeQuestions and MaxTypeSafeCriteria bound the fan-out of a single
// System One request. Every question is evaluated against the same state and
// every criteria entry is part of the prompt, so both maps grow the input token
// count that the request is billed on. Bounding them keeps that count tied to
// fields a request validator has actually looked at.
const (
	MaxTypeSafeQuestions = 512
	MaxTypeSafeCriteria  = 256
)

// TypeSafe image limits mirror what the upstream enforces: at most four images
// per request, 4 MiB decoded each and 8 MiB decoded in total. Images are billed
// as input tokens, so the count is bounded before it reaches the token estimate.
const (
	MaxTypeSafeImages           = 4
	MaxTypeSafeImageBytes       = 4 << 20
	MaxTypeSafeImagesTotalBytes = 8 << 20
)

// TypeSafeImage is one inline image evaluated alongside state. base64 is the raw
// file bytes on one line: no URL and no data: prefix.
type TypeSafeImage struct {
	MediaType string `json:"mediaType"`
	Base64    string `json:"base64"`
}

// TypeSafeQuestion is one typed question in a System One request. Instructions
// and criteria accept JSON structure (string, object, or array), so both stay
// raw and are validated per question type.
type TypeSafeQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// TypeSafeRequest is the body of POST /v1/systemone.
type TypeSafeRequest struct {
	State     json.RawMessage             `json:"state"`
	Model     string                      `json:"model"`
	Images    []TypeSafeImage             `json:"images,omitempty"`
	Questions map[string]TypeSafeQuestion `json:"questions"`
}

// TypeSafeUsage is billed on input tokens only; TypeSafe does not charge for
// output tokens, which is expressed as a completion ratio of 0 in model pricing
// rather than being hardcoded here.
type TypeSafeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TypeSafeResponse carries only the fields the gateway reads out of a System One
// response. The upstream body is forwarded verbatim, so answers and any field
// the gateway does not model still reach the client unchanged.
type TypeSafeResponse struct {
	Model string        `json:"model"`
	Usage TypeSafeUsage `json:"usage"`
}

// TypeSafeModel is one entry of GET /v1/models in TypeSafe's own shape, which
// nests the list under "models" instead of OpenAI's "data".
type TypeSafeModel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// IsStream is always false: System One evaluates the whole state in one shot
// and the endpoint has no streaming form.
func (r *TypeSafeRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *TypeSafeRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *TypeSafeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	questionIds := make([]string, 0, len(r.Questions))
	for id := range r.Questions {
		questionIds = append(questionIds, id)
	}
	sort.Strings(questionIds)

	// Question ids are not sent to the model and take no part in inference, so
	// they are left out of the counted text. Every other field is counted as the
	// text the model receives: JsonRawMessageToString unquotes a plain string and
	// leaves object/array structure as its JSON form.
	texts := make([]string, 0, 1+2*len(questionIds))
	if state := common.JsonRawMessageToString(r.State); state != "" {
		texts = append(texts, state)
	}
	for _, id := range questionIds {
		question := r.Questions[id]
		if instructions := common.JsonRawMessageToString(question.Instructions); instructions != "" {
			texts = append(texts, instructions)
		}
		if criteria := common.JsonRawMessageToString(question.Criteria); criteria != "" {
			texts = append(texts, criteria)
		}
	}

	// Images are billed as input tokens whether or not the media itself is
	// measured locally, so they must reach the estimator as files. Without this,
	// an image-only request would pre-consume as a text-only one.
	files := make([]*types.FileMeta, 0, len(r.Images))
	for _, image := range r.Images {
		if image.Base64 == "" {
			continue
		}
		files = append(files, types.NewImageFileMeta(types.NewBase64FileSource(image.Base64, image.MediaType), ""))
	}

	return &types.TokenCountMeta{
		TokenType:     types.TokenTypeTokenizer,
		CombineText:   strings.Join(texts, "\n"),
		MessagesCount: len(questionIds),
		Files:         files,
	}
}

// Validate rejects a System One request that the upstream would refuse, and
// bounds the two collections that decide how much text is billed.
func (r *TypeSafeRequest) Validate() error {
	if r.Model == "" {
		return errors.New("model is required")
	}
	if len(r.State) == 0 || common.GetJsonType(r.State) == "null" {
		return errors.New("state is required")
	}
	if len(r.Questions) == 0 {
		return errors.New("questions is required")
	}
	if len(r.Questions) > MaxTypeSafeQuestions {
		return fmt.Errorf("questions must contain at most %d entries", MaxTypeSafeQuestions)
	}
	for id, question := range r.Questions {
		if strings.TrimSpace(id) == "" {
			return errors.New("question id must not be empty")
		}
		if err := question.validate(id); err != nil {
			return err
		}
	}
	if len(r.Images) > MaxTypeSafeImages {
		return fmt.Errorf("images must contain at most %d entries", MaxTypeSafeImages)
	}
	totalImageBytes := int64(0)
	for i, image := range r.Images {
		switch image.MediaType {
		case "image/png", "image/jpeg", "image/webp":
		default:
			return fmt.Errorf("images[%d]: mediaType must be image/png, image/jpeg, or image/webp", i)
		}
		// A data: prefix or URL is rejected by the upstream, and both fail the
		// decode below. Decoding into io.Discard reports the decoded size without
		// materializing up to 8 MiB of image bytes per request, and the limit
		// stops the decode as soon as the per-image bound is known to be exceeded.
		encoded := strings.TrimSpace(image.Base64)
		if encoded == "" {
			return fmt.Errorf("images[%d]: base64 is required", i)
		}
		decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
		decoded, err := io.Copy(io.Discard, io.LimitReader(decoder, MaxTypeSafeImageBytes+1))
		if err != nil {
			return fmt.Errorf("images[%d]: base64 must be the raw file bytes on one line", i)
		}
		if decoded > MaxTypeSafeImageBytes {
			return fmt.Errorf("images[%d]: image over %d MiB", i, MaxTypeSafeImageBytes>>20)
		}
		totalImageBytes += decoded
		if totalImageBytes > MaxTypeSafeImagesTotalBytes {
			return fmt.Errorf("images: decoded total over %d MiB", MaxTypeSafeImagesTotalBytes>>20)
		}
	}
	return nil
}

// validate mirrors what the upstream actually enforces, which is looser than the
// prose in TypeSafe's docs: instructions is optional whenever criteria carries
// the question, and a score question is accepted with a single level even though
// the docs ask for two. A gateway that rejected either would fail requests the
// provider would have answered.
func (q TypeSafeQuestion) validate(id string) error {
	switch q.Type {
	case TypeSafeQuestionNoul:
		var criteria map[string]json.RawMessage
		if len(q.Criteria) > 0 {
			if err := common.Unmarshal(q.Criteria, &criteria); err != nil {
				return fmt.Errorf("question %s: noul criteria must be an object", id)
			}
			if len(criteria) > MaxTypeSafeCriteria {
				return fmt.Errorf("question %s: noul criteria must define at most %d entries", id, MaxTypeSafeCriteria)
			}
		}
		if len(criteria) == 0 && common.JsonRawMessageToString(q.Instructions) == "" {
			return fmt.Errorf("question %s: noul question must have criteria or instructions", id)
		}
		return nil
	case TypeSafeQuestionChoice:
		var options map[string]json.RawMessage
		if err := common.Unmarshal(q.Criteria, &options); err != nil {
			return fmt.Errorf("question %s: choice criteria must be an object mapping option to description", id)
		}
		if len(options) == 0 {
			return fmt.Errorf("question %s: choice criteria must define at least one option", id)
		}
		if len(options) > MaxTypeSafeCriteria {
			return fmt.Errorf("question %s: choice criteria must define at most %d options", id, MaxTypeSafeCriteria)
		}
		return nil
	case TypeSafeQuestionScore:
		var levels []json.RawMessage
		if err := common.Unmarshal(q.Criteria, &levels); err != nil {
			return fmt.Errorf("question %s: score criteria must be an array of level descriptions", id)
		}
		if len(levels) == 0 {
			return fmt.Errorf("question %s: score criteria must define at least one level", id)
		}
		if len(levels) > MaxTypeSafeCriteria {
			return fmt.Errorf("question %s: score criteria must define at most %d levels", id, MaxTypeSafeCriteria)
		}
		return nil
	default:
		return fmt.Errorf("question %s: type must be one of %s, %s, %s", id, TypeSafeQuestionNoul, TypeSafeQuestionChoice, TypeSafeQuestionScore)
	}
}
