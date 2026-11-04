package common

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryClearsUpstreamResponseModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyResponseModelName, "previous-attempt-model")
	info := &RelayInfo{}
	info.InitChannelMeta(c)
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeyResponseModelName))
}

func TestRetryResetsOnlyAttemptConversionAndTools(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &RelayInfo{SendResponseCount: 3, FinalPreConsumedQuota: 200, ClaudeConvertInfo: &ClaudeConvertInfo{Done: true, Index: 7}, ResponsesUsageInfo: &ResponsesUsageInfo{BuiltInTools: map[string]*BuildInToolInfo{"file_search": {CallCount: 2}}}}
	info.InitChannelMeta(c)
	assert.Zero(t, info.SendResponseCount)
	assert.False(t, info.ClaudeConvertInfo.Done)
	assert.Zero(t, info.ClaudeConvertInfo.Index)
	assert.Zero(t, info.BuiltInTools["file_search"].CallCount)
	assert.Equal(t, 200, info.FinalPreConsumedQuota)
}

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}
