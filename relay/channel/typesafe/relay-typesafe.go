package typesafe

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// SystemOneHandler forwards a System One response to the client unchanged and
// reports the upstream token usage for billing.
func SystemOneHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "typesafe response body: %s", responseBody)

	var systemOneResponse dto.TypeSafeResponse
	if err := common.Unmarshal(responseBody, &systemOneResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// The answers map is typed per question and carries probability
	// distributions the gateway has no reason to reshape, so the upstream entity
	// is written back byte for byte. IOCopyRawBytesGracefully still applies the
	// response model-name rewrite when the channel redirected the model.
	service.IOCopyRawBytesGracefully(c, resp, responseBody)

	usage := &dto.Usage{
		PromptTokens:     max(0, systemOneResponse.Usage.InputTokens),
		CompletionTokens: max(0, systemOneResponse.Usage.OutputTokens),
	}
	// TypeSafe bills input tokens only, but a response that reports no usage at
	// all would otherwise be billed as a free request. Fall back to the request
	// estimate so an upstream that omits usage cannot zero out the charge.
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage, nil
}
