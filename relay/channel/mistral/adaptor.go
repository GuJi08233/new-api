package mistral

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
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
	chat, err := claudeRequestToOpenAIChat(request)
	if err != nil {
		return nil, invalidMistralRequest(err)
	}
	info.InitRequestConversionChain()

	forceResponses := info != nil && info.ChannelMeta != nil &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName)
	// Responses has no stop parameter. Route to Chat only when stop_sequences
	// is supplied (and forced Responses conversion is not enabled); keep a
	// single direct hop in RequestConversionChain either way.
	if len(request.StopSequences) > 0 && !forceResponses {
		info.AppendRequestConversion(types.RelayFormatOpenAI)
		return a.ConvertOpenAIRequest(c, info, chat)
	}

	// Pass nil info so the internal Chat->Responses helper does not append an
	// intermediate OpenAI Compatible hop into the logged conversion chain.
	result, err := relayconvert.ConvertRequest(c, nil, types.RelayFormatOpenAIResponses, chat)
	if err != nil {
		return nil, err
	}
	responses, ok := result.Value.(*dto.OpenAIResponsesRequest)
	if !ok {
		return nil, errors.New("expected OpenAI responses request")
	}
	info.AppendRequestConversion(types.RelayFormatOpenAIResponses)
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
	baseURL := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/"), "/v1")
	if info.RelayFormat == types.RelayFormatClaude {
		switch info.GetFinalRequestRelayFormat() {
		case types.RelayFormatOpenAIResponses:
			return relaycommon.GetFullRequestURL(baseURL, "/v1/responses", info.ChannelType), nil
		case types.RelayFormatOpenAI:
			return relaycommon.GetFullRequestURL(baseURL, "/v1/chat/completions", info.ChannelType), nil
		}
	}
	return relaycommon.GetFullRequestURL(baseURL, info.RequestURLPath, info.ChannelType), nil
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
	if request.Model == "" && info != nil {
		request.Model = info.UpstreamModelName
	}
	if info != nil && info.RelayMode == relayconstant.RelayModeResponsesInputTokens {
		converted, err := requestResponsesInputTokens2Mistral(request)
		if err != nil {
			return nil, invalidMistralRequest(err)
		}
		return converted, nil
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
	case info.RelayMode == relayconstant.RelayModeResponsesInputTokens:
		defer service.CloseResponseBodyGracefully(resp)
		body, readErr := service.ReadResponseBody(c, resp)
		if readErr != nil {
			return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		service.IOCopyRawBytesGracefully(c, resp, body)
		return &dto.Usage{}, nil
	case info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIResponses:
		if info.IsStream {
			return openai.OaiResponsesToChatStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesToChatHandler(c, info, resp)
	case info.RelayMode == relayconstant.RelayModeResponses && info.IsStream:
		body, normalizeErr := normalizeMistralResponsesBody(resp.Body, true)
		if normalizeErr != nil {
			return nil, types.NewOpenAIError(normalizeErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		resp.Body = body
		usage, err = openai.OaiResponsesStreamHandler(c, info, resp)
	case info.RelayMode == relayconstant.RelayModeResponses:
		body, normalizeErr := normalizeMistralResponsesBody(resp.Body, false)
		if normalizeErr != nil {
			return nil, types.NewOpenAIError(normalizeErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		resp.Body = body
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
