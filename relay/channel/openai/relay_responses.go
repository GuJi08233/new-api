package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	// 写入新的 response body
	service.IOCopyRawBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := relayconvert.UsageFromResponsesUsage(responsesResponse.Usage)
	for i := range responsesResponse.Output {
		if responsesResponse.Output[i].Type == dto.ResponsesOutputTypeImageGenerationCall && !responsesImageCallsBillable(responsesResponse.Status) {
			continue
		}
		info.CountResponsesToolCall(&responsesResponse.Output[i])
	}
	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var toolOutputs []dto.ResponsesOutput
	seenToolIDs := make(map[string]bool)
	imageCallsBillable := true
	terminalOutputSeen := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		switch streamResponse.Type {
		case "response.completed", "response.done":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					if streamResponse.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResponse.Response.Usage.InputTokens
					}
					if streamResponse.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
					}
					if streamResponse.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
					}
					if streamResponse.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails = dto.MergeInputTokenDetails(usage.PromptTokensDetails, *streamResponse.Response.Usage.InputTokensDetails)
					}
				}
				imageCallsBillable = responsesImageCallsBillable(streamResponse.Response.Status)
				// 完整终态优先，避免 item.done 与终态重复收费；保留每张图片的实际质量。
				if streamResponse.Response.Output != nil {
					toolOutputs = streamResponse.Response.Output
					terminalOutputSeen = true
				}
			}
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			imageCallsBillable = false
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil && !terminalOutputSeen {
				item := streamResponse.Item
				key := item.ID
				if key == "" && streamResponse.OutputIndex != nil {
					key = fmt.Sprintf("index:%d", *streamResponse.OutputIndex)
				}
				if key != "" && seenToolIDs[key] {
					break
				}
				if key != "" {
					seenToolIDs[key] = true
				}
				toolOutputs = append(toolOutputs, *item)
			}
		}
		if err := helper.ResponseChunkData(c, streamResponse, data); err != nil {
			sr.Stop(err)
		}
	})

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	for i := range toolOutputs {
		if toolOutputs[i].Type == dto.ResponsesOutputTypeImageGenerationCall && !imageCallsBillable {
			continue
		}
		info.CountResponsesToolCall(&toolOutputs[i])
	}

	return usage, nil
}

func responsesImageCallsBillable(status []byte) bool {
	var value string
	_ = common.Unmarshal(status, &value)
	switch value {
	case "failed", "incomplete", "cancelled", "canceled":
		return false
	default:
		return true
	}
}
