package dto

import (
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type EmbeddingOptions struct {
	Seed             int      `json:"seed,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopK             int      `json:"top_k,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	NumPredict       int      `json:"num_predict,omitempty"`
	NumCtx           int      `json:"num_ctx,omitempty"`
}

type EmbeddingRequest struct {
	Model            string   `json:"model"`
	Input            any      `json:"input"`
	EncodingFormat   string   `json:"encoding_format,omitempty"`
	Dimensions       *int     `json:"dimensions,omitempty"`
	User             string   `json:"user,omitempty"`
	Seed             *float64 `json:"seed,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
}

func (r *EmbeddingRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts = make([]string, 0)

	inputs := r.ParseInput()
	for _, input := range inputs {
		texts = append(texts, input)
	}

	return &types.TokenCountMeta{
		CombineText: strings.Join(texts, "\n"),
	}
}

func (r *EmbeddingRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *EmbeddingRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *EmbeddingRequest) ParseInput() []string {
	if r.Input == nil {
		return make([]string, 0)
	}
	var input []string
	switch r.Input.(type) {
	case string:
		input = []string{r.Input.(string)}
	case []string:
		input = append([]string(nil), r.Input.([]string)...)
	case []any:
		input = make([]string, 0, len(r.Input.([]any)))
		for _, item := range r.Input.([]any) {
			if str, ok := item.(string); ok {
				input = append(input, str)
			}
		}
	}
	return input
}

// GetInputCount returns the number of top-level embedding inputs. A flat
// numeric array is one tokenized input, while a nested array is a batch whose
// outer length is the document count used by channel routing.
func (r *EmbeddingRequest) GetInputCount() int {
	if r == nil || r.Input == nil {
		return 0
	}

	value := reflect.ValueOf(r.Input)
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return 0
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return 0
	}
	if value.Kind() == reflect.String {
		return 1
	}
	if value.Kind() != reflect.Array && value.Kind() != reflect.Slice {
		return 0
	}
	if value.Len() == 0 {
		return 0
	}

	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
			if item.IsNil() {
				return value.Len()
			}
			item = item.Elem()
		}
		if !item.IsValid() {
			return value.Len()
		}
		switch item.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
		default:
			return value.Len()
		}
	}
	return 1
}

type EmbeddingResponseItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingResponse struct {
	Object string                  `json:"object"`
	Data   []EmbeddingResponseItem `json:"data"`
	Model  string                  `json:"model"`
	Usage  `json:"usage"`
}
