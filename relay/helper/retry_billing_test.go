package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type retryBillingRecorder struct {
	reserved int
	calls    int
	err      error
}

func (s *retryBillingRecorder) Reserve(target int) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.reserved = max(s.reserved, target)
	return nil
}
func (s *retryBillingRecorder) GetPreConsumedQuota() int { return s.reserved }
func (s *retryBillingRecorder) Settle(int) error         { return nil }
func (s *retryBillingRecorder) Refund(*gin.Context)      {}
func (s *retryBillingRecorder) NeedsRefund() bool        { return false }

func TestRetryBillingSwitchesIndependentGroupExpressions(t *testing.T) {
	savedModes := ratio_setting.GroupBillingMode2JSONString()
	savedExprs := ratio_setting.GroupBillingExpr2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupBillingModeByJSONString(savedModes))
		require.NoError(t, ratio_setting.UpdateGroupBillingExprByJSONString(savedExprs))
	})
	require.NoError(t, ratio_setting.UpdateGroupBillingModeByJSONString(`{"retry-a":{"retry-model":"tiered_expr"},"retry-b":{"retry-model":"tiered_expr"}}`))
	require.NoError(t, ratio_setting.UpdateGroupBillingExprByJSONString(`{"retry-a":{"retry-model":"tier(\"a\",p*2)"},"retry-b":{"retry-model":"tier(\"b\",p*8)"}}`))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("auto_group", "retry-a")
	at := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	info := &relaycommon.RelayInfo{OriginModelName: "retry-model", UserGroup: "default", UsingGroup: "retry-a", BillingRequestInput: &billingexpr.RequestInput{EvaluatedAt: at}}
	meta := &types.TokenCountMeta{MaxTokens: 1}
	_, err := ModelPriceHelper(c, info, 100, meta)
	require.NoError(t, err)
	session := &retryBillingRecorder{reserved: info.PriceData.QuotaToPreConsume}
	info.Billing = session
	c.Set("auto_group", "retry-b")
	require.Nil(t, PrepareBillingForSelectedGroup(c, info, 100, meta))
	assert.Equal(t, "retry-b", info.BillingGroup)
	assert.Equal(t, `tier("b",p*8)`, info.TieredBillingSnapshot.ExprString)
	assert.Equal(t, at, info.BillingRequestInput.EvaluatedAt)
	assert.True(t, info.ForcePreConsume)
	ok, quota, _ := service.TryTieredSettle(info, billingexpr.TokenParams{P: 100})
	require.True(t, ok)
	assert.Equal(t, common.QuotaFromFloat(800.0/1_000_000*common.QuotaPerUnit*info.TieredBillingSnapshot.GroupRatio), quota)
	assert.Equal(t, quota, session.reserved)
	require.Nil(t, PrepareBillingForSelectedGroup(c, info, 100, meta))
	assert.Equal(t, 1, session.calls)

	c.Set("auto_group", "retry-a")
	session.err = errors.New("quota unavailable")
	errAPI := PrepareBillingForSelectedGroup(c, info, 100, meta)
	require.NotNil(t, errAPI)
	assert.Equal(t, http.StatusForbidden, errAPI.StatusCode)
}
