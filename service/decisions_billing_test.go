package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestDecisionsBillingChargesInputAndSeparatesCacheReadsAndWrites(t *testing.T) {
	for _, tt := range []struct {
		format               types.RelayFormat
		model                string
		cached, write, quota int
	}{
		{types.RelayFormatGeminiDecisions, "gemini-decision-1", 0, 0, 50},
		{types.RelayFormatOpenAIDecisions, "gpt-6-luna-decisions", 200, 100, 42},
	} {
		t.Run(tt.model, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				RelayFormat: tt.format, OriginModelName: tt.model, StartTime: time.Now(),
				PriceData: types.PriceData{ModelRatio: 0.05, CompletionRatio: 0, CacheRatio: 0.1, CacheCreationRatio: 1.25, ImageRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
			}
			usage := &dto.Usage{PromptTokens: 1000, TotalTokens: 1000, UsageSemantic: "openai", UsageSource: "upstream", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: tt.cached, CacheWriteTokens: tt.write}}
			summary := calculateTextQuotaSummary(c, info, usage)
			assert.Equal(t, tt.quota, summary.Quota)
			assert.Equal(t, tt.cached, summary.CacheTokens)
			assert.Equal(t, tt.write, summary.CacheCreationTokens)
			assert.Zero(t, summary.CompletionTokens)
		})
	}
}
