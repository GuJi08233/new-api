package openai

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realtimeReservation struct {
	targets []int
	err     error
}

func (r *realtimeReservation) Reserve(quota int) error {
	r.targets = append(r.targets, quota)
	return r.err
}
func (r *realtimeReservation) GetPreConsumedQuota() int { return 0 }
func (r *realtimeReservation) Settle(int) error         { return nil }
func (r *realtimeReservation) Refund(*gin.Context)      {}
func (r *realtimeReservation) NeedsRefund() bool        { return false }

func TestRealtimeReserveFailureKeepsDeliveredUsageExactlyOnce(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	billing := &realtimeReservation{}
	info := &relaycommon.RelayInfo{
		Billing:     billing,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-realtime-preview"},
		PriceData:   types.PriceData{ModelRatio: 2.5, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	total := &dto.RealtimeUsage{}
	usage := &dto.RealtimeUsage{TotalTokens: 40, InputTokens: 40}
	usage.InputTokenDetails.TextTokens = 40
	require.NoError(t, preConsumeUsage(c, info, usage, total))
	assert.Zero(t, *usage)
	billing.err = errors.New("insufficient quota")
	usage.TotalTokens, usage.InputTokens, usage.InputTokenDetails.TextTokens = 60, 60, 60
	require.ErrorIs(t, preConsumeUsage(c, info, usage, total), billing.err)
	assert.Zero(t, *usage, "失败退出时不能把同轮用量再次加入最终结算")
	assert.Equal(t, []int{100, 250}, billing.targets)
	assert.Equal(t, 100, total.TotalTokens)
	assert.Equal(t, 100, total.InputTokens)
	assert.Equal(t, 100, total.InputTokenDetails.TextTokens)
}
