package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// PrepareImageBillingForRequest 按最终出站数量预扣，重试不会叠加旧渠道的倍率。
func PrepareImageBillingForRequest(c *gin.Context, info *relaycommon.RelayInfo, count int, promptExtend bool) *types.NewAPIError {
	if count < 1 || count > dto.MaxImageN {
		return types.NewErrorWithStatusCode(fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	info.ImageRequestCount = count
	info.ForcePreConsume = true
	// main 的表达式只按自身定义计费；不能再隐式乘数量。
	if info.TieredBillingSnapshot != nil {
		return ReserveBillingForRequest(c, info, info.PriceData.QuotaToPreConsume)
	}
	quantity := 1
	if info.PriceData.UsePrice || info.ChannelType == constant.ChannelTypeAli {
		quantity = count
	}
	info.PriceData.AddOtherRatio("n", float64(quantity))
	extensionRatio := 1.0
	if info.ChannelType == constant.ChannelTypeAli && strings.Contains(info.UpstreamModelName, "z-image") && promptExtend {
		extensionRatio = 2
	}
	info.PriceData.AddOtherRatio("prompt_extend", extensionRatio)
	base := info.ImageQuotaBeforeGroup
	if info.PriceData.UsePrice {
		base = info.PriceData.ModelPrice * common.QuotaPerUnit
	}
	quota, err := common.QuotaFromFloatStrict(info.PriceData.ApplyOtherRatiosToFloat(base * info.PriceData.GroupRatioInfo.GroupRatio))
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	info.PriceData.QuotaToPreConsume = quota
	return ReserveBillingForRequest(c, info, quota)
}
