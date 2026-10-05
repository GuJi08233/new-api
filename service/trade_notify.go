package service

import (
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"

	"github.com/bytedance/gopkg/util/gopool"
)

func init() {
	model.TradeDeficitNotifier = notifyTradeDeficit
}

// sendTradeNotices 从已提交的站内事件发送通知，不阻塞撮合。只补发最近 15 分钟的积压，且每个事件仅尝试一次。
func sendTradeNotices() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := dispatchTradeNotices(notifyTradeNotice); err != nil {
			common.SysError("trade notifications: " + err.Error())
		}
		<-ticker.C
	}
}

// dispatchTradeNotices 的发送回调便于离线验证：测试不会访问用户配置的真实通知渠道。
func dispatchTradeNotices(send func(model.TradeNotice) error) error {
	notices, result := model.ClaimTradeNoticeNotifications(time.Now().Add(-15*time.Minute).Unix(), 20)
	for _, notice := range notices {
		err := send(notice)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("notice %d user %d: %w", notice.Id, notice.UserId, err))
		}
		if finishErr := model.FinishTradeNoticeNotification(notice.Id, err == nil); finishErr != nil {
			result = errors.Join(result, fmt.Errorf("record notice %d delivery: %w", notice.Id, finishErr))
		}
	}
	return result
}

func notifyTradeNotice(notice model.TradeNotice) error {
	user, err := model.GetUserById(notice.UserId, true)
	if err != nil {
		return fmt.Errorf("load user: %w", err)
	}
	setting := user.GetSetting()
	data, err := tradeNoticeNotification(notice, setting.NotifyType)
	if err != nil {
		return err
	}
	return NotifyUser(user.Id, user.Email, setting, data)
}

// tradeNoticeNotification 让事件种类分别限流，普通成交不会用完强平或止损的通知配额。
func tradeNoticeNotification(notice model.TradeNotice, channel string) (dto.Notify, error) {
	var kind, title string
	switch notice.Kind {
	case model.TradeNoticeTakeProfit:
		kind, title = dto.NotifyTypeTradeTakeProfit, "模拟盘止盈已触发"
	case model.TradeNoticeStopLoss:
		kind, title = dto.NotifyTypeTradeStopLoss, "模拟盘止损已触发"
	case model.TradeNoticeLiquidation:
		kind, title = dto.NotifyTypeTradeLiquidation, "模拟盘仓位已强平"
	case model.TradeNoticeFill:
		kind, title = dto.NotifyTypeTradeFill, "模拟盘挂单已成交"
	default:
		return dto.Notify{}, fmt.Errorf("unsupported trade notice kind %q", notice.Kind)
	}
	market, side := "现货", "买入"
	if notice.Side == model.TradeSideSell {
		side = "卖出"
	}
	if notice.Market == model.TradeNoticeFutures {
		market, side = "合约", "多仓"
		if notice.Side == model.TradeFuturesShort {
			side = "空仓"
		}
		switch notice.Action {
		case model.TradeFuturesOpen:
			side = "开" + side
		case model.TradeFuturesClose:
			side = "平" + side
		}
	}
	content := "{{value}}：{{value}} {{value}}，数量 {{value}}，成交价 {{value}} USDT"
	values := []interface{}{market, notice.Symbol, side, tradesim.QtyFromUnits(notice.Qty).String(), notice.Price}
	if notice.Market == model.TradeNoticeFutures && (notice.Action == model.TradeFuturesClose || notice.Kind == model.TradeNoticeLiquidation) {
		content += "，已实现盈亏 {{value}}"
		values = append(values, logger.FormatQuota(notice.Pnl))
	}
	assets := PaymentReturnURL("/trade/assets")
	if channel == dto.NotifyTypeBark || channel == dto.NotifyTypeGotify {
		content += "。查看模拟盘：{{value}}"
		values = append(values, assets)
	} else {
		for i, value := range values {
			values[i] = html.EscapeString(fmt.Sprint(value))
		}
		content += "。<br/>查看模拟盘：<a href='{{value}}'>{{value}}</a>"
		values = append(values, html.EscapeString(assets), html.EscapeString(assets))
	}
	return dto.NewNotify(kind, title, content, values), nil
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
