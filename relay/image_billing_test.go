package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

type imagePreflightBilling struct{ target int }

func (s *imagePreflightBilling) Reserve(target int) error {
	s.target = target
	return errors.New("insufficient quota")
}
func (s *imagePreflightBilling) GetPreConsumedQuota() int { return 0 }
func (s *imagePreflightBilling) Settle(int) error         { return nil }
func (s *imagePreflightBilling) Refund(*gin.Context)      {}
func (s *imagePreflightBilling) NeedsRefund() bool        { return false }

func TestImageHelperChecksOutboundQuantityBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	for _, count := range []int{4, dto.MaxImageN + 1} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat","n":1}`))
		c.Request.Header.Set("Content-Type", "application/json")
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-image-1")
		common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]interface{}{"n": count})
		session := &imagePreflightBilling{}
		info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-1", RelayMode: relayconstant.RelayModeImagesGenerations, Request: &dto.ImageRequest{Model: "gpt-image-1", Prompt: "cat", N: common.GetPointer(uint(1))}, Billing: session, PriceData: types.PriceData{UsePrice: true, ModelPrice: 0.04, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
		apiErr := ImageHelper(c, info)
		require.NotNil(t, apiErr)
		if count <= dto.MaxImageN {
			assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			assert.Equal(t, common.QuotaFromFloat(0.04*common.QuotaPerUnit*float64(count)), session.target)
		} else {
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Zero(t, session.target)
		}
	}
	assert.Zero(t, calls.Load())
}
