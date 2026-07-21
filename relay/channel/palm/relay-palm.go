package palm

import (
	"context"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#request-body
// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#response-body

func responsePaLM2OpenAI(response *PaLMChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	for i, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: i,
			Message: dto.Message{
				Role:    "assistant",
				Content: candidate.Content,
			},
			FinishReason: "stop",
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponsePaLM2OpenAI(palmResponse *PaLMChatResponse) *dto.ChatCompletionsStreamResponse {
	var choice dto.ChatCompletionsStreamResponseChoice
	if len(palmResponse.Candidates) > 0 {
		choice.Delta.SetContentString(palmResponse.Candidates[0].Content)
	}
	choice.FinishReason = &constant.FinishReasonStop
	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Model = "palm2"
	response.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
	return &response
}

func palmStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, string) {
	responseText := ""
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	stopStream := helper.StartStreamSession(c, info, resp)
	type streamResult struct {
		data         string
		responseText string
	}
	dataChan := make(chan streamResult, 1)
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	streamCtx, cancel := context.WithCancel(requestCtx)
	workerDone := make(chan struct{})
	var workerErr error
	go func() {
		defer close(workerDone)
		defer close(dataChan)
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			workerErr = err
			common.SysLog("error reading stream response: " + err.Error())
			return
		}
		var palmResponse PaLMChatResponse
		err = common.Unmarshal(responseBody, &palmResponse)
		if err != nil {
			workerErr = err
			common.SysLog("error unmarshalling stream response: " + err.Error())
			return
		}
		fullTextResponse := streamResponsePaLM2OpenAI(&palmResponse)
		fullTextResponse.Id = responseId
		fullTextResponse.Created = createdTime
		jsonResponse, err := common.Marshal(fullTextResponse)
		if err != nil {
			workerErr = err
			common.SysLog("error marshalling stream response: " + err.Error())
			return
		}
		result := streamResult{data: string(jsonResponse)}
		if len(palmResponse.Candidates) > 0 {
			result.responseText = palmResponse.Candidates[0].Content
		}
		select {
		case dataChan <- result:
		case <-streamCtx.Done():
		}
	}()
	defer func() {
		cancel()
		service.CloseResponseBodyGracefully(resp)
		<-workerDone
	}()
	defer stopStream()
	clientGone := c.Writer.CloseNotify()
	responseWritten := false
	for {
		select {
		case result, ok := <-dataChan:
			if !ok {
				if workerErr == nil && responseWritten {
					stopStream()
					helper.Done(c)
				}
				return nil, responseText
			}
			responseText = result.responseText
			if err := helper.StringData(c, result.data); err != nil {
				return nil, responseText
			}
			responseWritten = true
		case <-streamCtx.Done():
			return nil, responseText
		case <-clientGone:
			return nil, responseText
		}
	}
}

func palmHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	var palmResponse PaLMChatResponse
	err = common.Unmarshal(responseBody, &palmResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if palmResponse.Error.Code != 0 || len(palmResponse.Candidates) == 0 {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: palmResponse.Error.Message,
			Type:    palmResponse.Error.Status,
			Param:   "",
			Code:    palmResponse.Error.Code,
		}, resp.StatusCode)
	}
	fullTextResponse := responsePaLM2OpenAI(&palmResponse)
	usage := service.ResponseText2Usage(c, palmResponse.Candidates[0].Content, info.UpstreamModelName, info.GetEstimatePromptTokens())
	fullTextResponse.Usage = *usage
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}
