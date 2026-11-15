package helper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIDecisionsMapsGatewayAliasToNativeModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6-luna-decisions")
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIDecisions, RelayMode: relayconstant.RelayModeOpenAIDecisions, OriginModelName: "gpt-6-luna-decisions",
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-6-luna-decisions"},
	}
	request := &dto.OpenAIDecisionsRequest{Model: "gpt-6-luna-decisions", Input: json.RawMessage(`"hello"`)}
	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "gpt-6-luna", request.Model)
	assert.Equal(t, "gpt-6-luna", info.UpstreamModelName)
	assert.Equal(t, "gpt-6-luna-decisions", info.OriginModelName)
	assert.True(t, info.IsModelMapped)
}

func TestDecisionsTieredPreconsumeDoesNotReserveCompletionTokens(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousQuotaPerUnit })
	for _, format := range []types.RelayFormat{types.RelayFormatGeminiDecisions, types.RelayFormatOpenAIDecisions} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/decisions", nil)
		info := &relaycommon.RelayInfo{RelayFormat: format, OriginModelName: "decisions-test"}
		price, err := modelPriceHelperTieredWithExpr(c, info, 1000, &types.TokenCountMeta{}, types.GroupRatioInfo{GroupRatio: 1}, `tier("base", p * 0.1 + c * 0.5)`)
		require.NoError(t, err)
		assert.Equal(t, 50, price.QuotaToPreConsume)
	}
}
