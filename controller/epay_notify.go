package controller

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const unifiedEpayNotifyPath = "/api/epay/notify"

func getUnifiedEpayNotifyURL() string {
	return strings.TrimRight(service.GetCallbackAddress(), "/") + unifiedEpayNotifyPath
}

func getUnifiedEpayNotifyURLParsed() *url.URL {
	notifyURL, err := url.Parse(getUnifiedEpayNotifyURL())
	if err != nil {
		return nil
	}
	return notifyURL
}

func parseEpayParams(c *gin.Context) map[string]string {
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			log.Println("易支付回调POST解析失败:", err)
			return nil
		}
		return lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, key string, _ int) map[string]string {
			r[key] = c.Request.PostForm.Get(key)
			return r
		}, map[string]string{})
	}

	return lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, key string, _ int) map[string]string {
		r[key] = c.Request.URL.Query().Get(key)
		return r
	}, map[string]string{})
}

func writeEpayNotifyResult(c *gin.Context, success bool) {
	result := "fail"
	if success {
		result = "success"
	}
	if _, err := c.Writer.Write([]byte(result)); err != nil {
		log.Println("易支付回调写入失败:", err)
	}
}

func completeTopUpEpayOrder(source model.LogSource, verifyInfo *epay.VerifyRes) error {
	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	topUp := model.GetTopUpByTradeNo(verifyInfo.ServiceTradeNo)
	if topUp == nil {
		return fmt.Errorf("topup order not found: %s", verifyInfo.ServiceTradeNo)
	}
	if topUp.PaymentMethod == "stripe" || topUp.PaymentMethod == "creem" || topUp.PaymentMethod == "waffo" {
		return fmt.Errorf("payment method mismatch: %s", topUp.PaymentMethod)
	}
	if topUp.Status != common.TopUpStatusPending {
		return nil
	}

	topUp.Status = common.TopUpStatusSuccess
	if err := topUp.Update(); err != nil {
		return err
	}

	dAmount := decimal.NewFromInt(topUp.Amount)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		return err
	}

	model.RecordLog(
		source,
		topUp.UserId,
		model.LogTypeTopup,
		fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money),
	)
	log.Printf("易支付统一回调更新余额成功 %v", topUp)
	return nil
}

func completeSubscriptionEpayOrder(source model.LogSource, verifyInfo *epay.VerifyRes) error {
	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)
	return model.CompleteSubscriptionOrder(source, verifyInfo.ServiceTradeNo, common.GetJsonString(verifyInfo), model.PaymentProviderEpay, verifyInfo.Type)
}

func EpayUnifiedNotify(c *gin.Context) {
	params := parseEpayParams(c)
	if len(params) == 0 {
		log.Println("易支付统一回调参数为空")
		writeEpayNotifyResult(c, false)
		return
	}

	client := GetEpayClient()
	if client == nil {
		log.Println("易支付统一回调失败 未找到配置信息")
		writeEpayNotifyResult(c, false)
		return
	}

	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		log.Println("易支付统一回调签名验证失败")
		writeEpayNotifyResult(c, false)
		return
	}

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		log.Printf("易支付统一回调忽略非成功订单: %s, status=%s", verifyInfo.ServiceTradeNo, verifyInfo.TradeStatus)
		writeEpayNotifyResult(c, true)
		return
	}

	// 回调由支付网关发起，来源即网关的地址与 UA，据此可核对回调来路是否可疑
	source := model.ClientLogSource(c)
	switch {
	case strings.HasPrefix(verifyInfo.ServiceTradeNo, "SUBUSR"):
		err = completeSubscriptionEpayOrder(source, verifyInfo)
	case strings.HasPrefix(verifyInfo.ServiceTradeNo, "USR"):
		err = completeTopUpEpayOrder(source, verifyInfo)
	default:
		err = fmt.Errorf("unknown epay trade prefix: %s", verifyInfo.ServiceTradeNo)
	}
	if err != nil {
		log.Printf("易支付统一回调处理失败: %v", err)
		writeEpayNotifyResult(c, false)
		return
	}

	writeEpayNotifyResult(c, true)
}
