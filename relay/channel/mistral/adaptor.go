package mistral

import (
	"errors"
	"io"
	"net/http"

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

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	body, err = transform.RequestClaudeToOpenAI(body, transform.TransformFlags{})
	if err != nil {
		return nil, err
	}
	var chat dto.GeneralOpenAIRequest
	if err := common.Unmarshal(body, &chat); err != nil {
		return nil, err
	}
	if request.Thinking != nil && request.Thinking.Type == "enabled" && request.GetEfforts() == "" {
		chat.ReasoningEffort = "high"
	}
	info.InitRequestConversionChain()
	info.AppendRequestConversion(types.RelayFormatOpenAI)
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
	if info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIResponses {
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/responses", info.ChannelType), nil
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
		return nil, errors.New("request is nil")
	}
	return requestOpenAI2Mistral(request)
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
		return nil, errors.New("mistral does not support /v1/responses/compact")
	}
	if request.Model == "" && info != nil {
		request.Model = info.UpstreamModelName
	}
	return requestResponses2Mistral(request)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() != types.RelayFormatOpenAIResponses {
		return nil, errors.New("Mistral /v1/messages requires conversion to Responses; disable pass-through request body")
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
