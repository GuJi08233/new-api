package mistral

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/transform"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func invalidMistralRequest(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, invalidMistralRequest(errors.New("request is nil"))
	}
	for name, value := range map[string][]byte{
		"context_management": request.ContextManagement,
		"container":          request.Container,
		"mcp_servers":        request.McpServers,
	} {
		if isJSONValuePresent(value) {
			return nil, invalidMistralRequest(fmt.Errorf("mistral does not support Claude %s", name))
		}
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	body, err = transform.RequestClaudeToOpenAI(body, transform.TransformFlags{})
	if err != nil {
		return nil, invalidMistralRequest(err)
	}
	var chat dto.GeneralOpenAIRequest
	if err := common.Unmarshal(body, &chat); err != nil {
		return nil, invalidMistralRequest(err)
	}
	if request.Thinking != nil && request.Thinking.Type == "enabled" && request.GetEfforts() == "" {
		chat.ReasoningEffort = "high"
	}
	info.InitRequestConversionChain()
	info.AppendRequestConversion(types.RelayFormatOpenAI)
	// Responses has no stop/top_k controls. Choose Chat before sending rather
	// than discarding them or retrying a request on a different endpoint.
	if len(request.StopSequences) > 0 || request.TopK != nil {
		return a.ConvertOpenAIRequest(c, info, &chat)
	}
	result, err := relayconvert.ConvertRequest(c, info, types.RelayFormatOpenAIResponses, &chat)
	if err != nil {
		return nil, err
	}
	responses, ok := result.Value.(*dto.OpenAIResponsesRequest)
	if !ok {
		return nil, errors.New("expected OpenAI responses request")
	}
	return a.ConvertOpenAIResponsesRequest(c, info, *responses)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayFormat == types.RelayFormatClaude {
		switch info.GetFinalRequestRelayFormat() {
		case types.RelayFormatOpenAIResponses:
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/responses", info.ChannelType), nil
		case types.RelayFormatOpenAI:
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/chat/completions", info.ChannelType), nil
		}
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, invalidMistralRequest(errors.New("request is nil"))
	}
	converted, err := requestOpenAI2Mistral(request)
	if err != nil {
		return nil, invalidMistralRequest(err)
	}
	return converted, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if info != nil && info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return nil, invalidMistralRequest(errors.New("mistral does not support /v1/responses/compact"))
	}
	if info != nil {
		var chat *dto.GeneralOpenAIRequest
		switch original := info.Request.(type) {
		case *dto.ClaudeRequest:
			if original != nil && (len(original.StopSequences) > 0 || original.TopK != nil) {
				return nil, invalidMistralRequest(errors.New("mistral Responses cannot preserve Claude stop_sequences/top_k; disable forced Responses conversion"))
			}
		case *dto.GeneralOpenAIRequest:
			chat = original
		}
		if chat != nil {
			var unsupported []string
			for _, option := range []struct {
				name     string
				provided bool
			}{
				{"stop", chat.Stop != nil}, {"top_k", chat.TopK != nil},
				{"seed", chat.Seed != nil}, {"random_seed", chat.RandomSeed != nil},
				{"min_tokens", chat.MinTokens != nil}, {"repetition_penalty", chat.RepetitionPenalty != nil},
				{"prompt_logprobs", chat.PromptLogProbs != nil}, {"top_prompt_logprobs", chat.TopPromptLogProbs != nil},
				{"prompt_mode", chat.PromptMode != nil}, {"safe_prompt", chat.SafePrompt != nil},
				{"guardrails", isJSONValuePresent(chat.Guardrails)}, {"logprobs", chat.LogProbs != nil},
				{"top_logprobs", chat.TopLogProbs != nil}, {"prediction", isJSONValuePresent(chat.Prediction)},
				{"functions", isJSONValuePresent(chat.Functions)}, {"function_call", isJSONValuePresent(chat.FunctionCall)},
			} {
				if option.provided {
					unsupported = append(unsupported, option.name)
				}
			}
			if len(unsupported) > 0 {
				return nil, invalidMistralRequest(fmt.Errorf("mistral Responses cannot preserve %s; disable forced Responses conversion", strings.Join(unsupported, ", ")))
			}
		}
	}
	if request.Model == "" && info != nil {
		request.Model = info.UpstreamModelName
	}
	converted, err := requestResponses2Mistral(request)
	if err != nil {
		return nil, invalidMistralRequest(err)
	}
	return converted, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() != types.RelayFormatOpenAIResponses && info.GetFinalRequestRelayFormat() != types.RelayFormatOpenAI {
		return nil, invalidMistralRequest(errors.New("Mistral /v1/messages requires conversion to Chat or Responses; disable pass-through request body"))
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch {
	case info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIResponses:
		if info.IsStream {
			return openai.OaiResponsesToChatStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesToChatHandler(c, info, resp)
	case info.RelayMode == relayconstant.RelayModeResponses && info.IsStream:
		usage, err = openai.OaiResponsesStreamHandler(c, info, resp)
	case info.RelayMode == relayconstant.RelayModeResponses:
		usage, err = openai.OaiResponsesHandler(c, info, resp)
	default:
		if info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAI {
			mode := info.RelayMode
			info.RelayMode = relayconstant.RelayModeChatCompletions
			defer func() { info.RelayMode = mode }()
		}
		body, normalizeErr := normalizeMistralChatBody(resp.Body, info.IsStream)
		if normalizeErr != nil {
			return nil, types.NewOpenAIError(normalizeErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		resp.Body = body
		if info.IsStream {
			usage, err = openai.OaiStreamHandler(c, info, resp)
		} else {
			usage, err = openai.OpenaiHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
