package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type imageBillingRecorder struct {
	quota int
	err   error
}

func (s *imageBillingRecorder) Reserve(quota int) error {
	if s.err != nil {
		return s.err
	}
	s.quota = max(s.quota, quota)
	return nil
}
func (s *imageBillingRecorder) GetPreConsumedQuota() int { return s.quota }
func (s *imageBillingRecorder) Settle(int) error         { return nil }
func (s *imageBillingRecorder) Refund(*gin.Context)      {}
func (s *imageBillingRecorder) NeedsRefund() bool        { return false }

func TestImageBillingReservesEffectiveQuantityAndClearsRetryRatios(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &imageBillingRecorder{}
	info := &relaycommon.RelayInfo{
		Request: &dto.ImageRequest{}, Billing: session,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAli, UpstreamModelName: "z-image-turbo"},
		PriceData:   types.PriceData{UsePrice: true, ModelPrice: 0.04, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	require.Nil(t, PrepareImageBillingForRequest(c, info, 3, true))
	assert.Equal(t, common.QuotaFromFloat(0.04*common.QuotaPerUnit*6), session.quota)
	assert.True(t, info.ForcePreConsume)
	info.ChannelType = constant.ChannelTypeOpenAI
	require.Nil(t, PrepareImageBillingForRequest(c, info, 2, false))
	assert.Equal(t, common.QuotaFromFloat(0.04*common.QuotaPerUnit*2), info.PriceData.QuotaToPreConsume)
	assert.Equal(t, 2, info.ImageRequestCount)
	session.err = errors.New("insufficient quota")
	require.NotNil(t, PrepareImageBillingForRequest(c, info, 4, false))
}
