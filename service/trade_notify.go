package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

func init() {
	model.TradeDeficitNotifier = notifyTradeDeficit
}

// notifyTradeDeficit 按用户的通知设置(邮件、Webhook、Bark、Gotify)告诉用户：合约穿仓的亏空从站内额度里扣了多少、额度还剩多少。
// 在后台发，不拖慢强平与平仓；发不出去只记日志。
func notifyTradeDeficit(userId int, covered int) {
	gopool.Go(func() {
		user, err := model.GetUserById(userId, true)
		if err != nil {
			common.SysError(fmt.Sprintf("trade deficit notify: failed to load user %d: %v", userId, err))
			return
		}
		setting := user.GetSetting()
		prompt := "模拟盘合约亏空已从额度扣除"
		var content string
		var values []interface{}
		switch setting.NotifyType {
		case dto.NotifyTypeBark, dto.NotifyTypeGotify:
			content = "价格跳空越过强平价，合约亏空 {{value}} 已从站内额度扣除，当前额度 {{value}}"
			values = []interface{}{logger.FormatQuota(covered), logger.FormatQuota(user.Quota)}
		default:
			assets := PaymentReturnURL("/trade/assets")
			content = "{{value}}：价格跳空越过了强平价，合约亏空 {{value}} 已从站内额度扣除，当前额度 {{value}}。<br/>查看模拟盘：<a href='{{value}}'>{{value}}</a>"
			values = []interface{}{prompt, logger.FormatQuota(covered), logger.FormatQuota(user.Quota), assets, assets}
		}
		if err := NotifyUser(userId, user.Email, setting, dto.NewNotify(dto.NotifyTypeTradeDeficit, prompt, content, values)); err != nil {
			common.SysError(fmt.Sprintf("trade deficit notify: failed to notify user %d: %v", userId, err))
		}
	})
}
