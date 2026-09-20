package dto

import (
	"encoding/json"
	"errors"
	"fmt"
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

	return &types.TokenCountMeta{
		TokenType:     types.TokenTypeTokenizer,
		CombineText:   strings.Join(texts, "\n"),
		MessagesCount: len(questionIds),
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
