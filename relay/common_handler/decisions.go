package common_handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// DecisionsHandler 原样返回答案，只读取计费需要的输入用量。两个端点均没有输出 token。
func DecisionsHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := service.ReadResponseBody(c, resp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	var response struct {
		Answers json.RawMessage `json:"answers"`
		Usage   struct {
			InputTokens      *float64 `json:"input_tokens"`
			TotalInputTokens *float64 `json:"total_input_tokens"`
			InputDetails     struct {
				CachedTokens     *float64 `json:"cached_tokens"`
				CacheWriteTokens *float64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	inputTokens := response.Usage.InputTokens
	var cachedTokens, cacheWriteTokens *float64
	answerType := "array"
	if info.RelayFormat == types.RelayFormatGeminiDecisions {
		inputTokens = response.Usage.TotalInputTokens
		answerType = "object"
	} else {
		cachedTokens = response.Usage.InputDetails.CachedTokens
		cacheWriteTokens = response.Usage.InputDetails.CacheWriteTokens
	}
	if common.GetJsonType(response.Answers) != answerType {
		return nil, types.NewOpenAIError(errors.New("invalid decisions answers"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	usage := &dto.Usage{UsageSemantic: "openai", UsageSource: "upstream"}
	counts := []struct {
		value  *float64
		target *int
	}{
		{inputTokens, &usage.PromptTokens},
		{cachedTokens, &usage.PromptTokensDetails.CachedTokens},
		{cacheWriteTokens, &usage.PromptTokensDetails.CacheWriteTokens},
	}
	for _, count := range counts {
		if count.value == nil {
			continue
		}
		value, clamp := common.QuotaFromFloatChecked(max(0, *count.value))
		*count.target = value
		if clamp != nil && info.QuotaClamp == nil {
			info.QuotaClamp = clamp
		}
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = max(0, info.GetEstimatePromptTokens())
	}
	usage.InputTokens = usage.PromptTokens
	usage.TotalTokens = usage.PromptTokens
	service.IOCopyRawBytesGracefully(c, resp, responseBody)
	return usage, nil
}
