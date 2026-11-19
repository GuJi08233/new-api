package mistral

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// moderationRequest keeps moderation separate from the chat DTO so channel
// system prompts and chat-only options are not added to a classification request.
type moderationRequest struct {
	Model    string          `json:"model"`
	Input    any             `json:"input"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

func badMistralModerationResponse(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
}

func (a *Adaptor) convertModerationRequest(request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, invalidMistralRequest(errors.New("moderation request is required"))
	}
	if request.Stream != nil && *request.Stream {
		return nil, invalidMistralRequest(errors.New("Mistral moderation does not support streaming"))
	}
	var inputs []string
	switch input := request.Input.(type) {
	case string:
		inputs = []string{input}
	case []string:
		inputs = input
	case []any:
		for _, value := range input {
			text, ok := value.(string)
			if !ok {
				return nil, invalidMistralRequest(errors.New("Mistral moderation input must be a string or array of strings; images and token IDs are not supported"))
			}
			inputs = append(inputs, text)
		}
	default:
		return nil, invalidMistralRequest(errors.New("Mistral moderation input must be a string or array of strings"))
	}
	if len(inputs) == 0 {
		return nil, invalidMistralRequest(errors.New("Mistral moderation input must not be empty"))
	}
	for _, input := range inputs {
		if input == "" {
			return nil, invalidMistralRequest(errors.New("Mistral moderation input must contain non-empty strings"))
		}
	}
	a.moderationInputs = len(inputs)
	return &moderationRequest{
		Model:    request.Model,
		Input:    request.Input,
		Metadata: request.Metadata,
	}, nil
}

func (a *Adaptor) moderationResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	data, err := service.ReadResponseBody(c, resp)
	if err != nil {
		return nil, badMistralModerationResponse(err)
	}
	var response map[string]json.RawMessage
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, badMistralModerationResponse(err)
	}
	if response == nil {
		return nil, badMistralModerationResponse(errors.New("empty Mistral moderation response"))
	}
	var objectType string
	if common.Unmarshal(response["object"], &objectType) == nil && objectType == "error" {
		var message string
		_ = common.Unmarshal(response["message"], &message)
		if message == "" {
			message = "upstream Mistral moderation error"
		}
		return nil, badMistralModerationResponse(errors.New(message))
	}
	for _, field := range []string{"id", "model"} {
		var value string
		if common.Unmarshal(response[field], &value) != nil || value == "" {
			return nil, badMistralModerationResponse(errors.New("Mistral moderation response is missing " + field))
		}
	}
	var results []map[string]json.RawMessage
	if err := common.Unmarshal(response["results"], &results); err != nil || len(results) == 0 || (a.moderationInputs > 0 && len(results) != a.moderationInputs) {
		return nil, badMistralModerationResponse(errors.New("invalid Mistral moderation result count"))
	}
	for _, result := range results {
		var categories map[string]*bool
		var scores map[string]*float64
		if common.Unmarshal(result["categories"], &categories) != nil || len(categories) == 0 ||
			common.Unmarshal(result["category_scores"], &scores) != nil || len(scores) == 0 {
			return nil, badMistralModerationResponse(errors.New("invalid Mistral moderation categories or scores"))
		}
		flagged := false
		for _, value := range categories {
			if value == nil {
				return nil, badMistralModerationResponse(errors.New("invalid Mistral moderation category"))
			}
			flagged = flagged || *value
		}
		for _, score := range scores {
			if score == nil || math.IsNaN(*score) || math.IsInf(*score, 0) {
				return nil, badMistralModerationResponse(errors.New("invalid Mistral moderation score"))
			}
		}
		if flagged {
			result["flagged"] = json.RawMessage("true")
		} else {
			result["flagged"] = json.RawMessage("false")
		}
	}
	usage := &dto.Usage{}
	if raw := response["usage"]; len(raw) > 0 && string(raw) != "null" {
		if err := common.Unmarshal(raw, usage); err != nil {
			return nil, badMistralModerationResponse(err)
		}
		if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	} else {
		usage = service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	response["results"], err = common.Marshal(results)
	if err != nil {
		return nil, badMistralModerationResponse(err)
	}
	data, err = common.Marshal(response)
	if err != nil {
		return nil, badMistralModerationResponse(err)
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, data)
	return usage, nil
}
