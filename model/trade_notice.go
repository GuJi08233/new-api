package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 通知的种类：止盈、止损、强平、挂着的限价单成交、亏空从站内额度扣除、现货借款的风险率跌到追加保证金线。
const (
	TradeNoticeTakeProfit  = "tp"
	TradeNoticeStopLoss    = "sl"
	TradeNoticeLiquidation = "liquidation"
	TradeNoticeFill        = "fill"
	TradeNoticeCover       = "cover"
	TradeNoticeMarginCall  = "margin_call"

	TradeNoticeSpot    = "spot"
	TradeNoticeFutures = "futures"

	TradeNoticeNotifyPending = "pending"
	TradeNoticeNotifySending = "sending"
	TradeNoticeNotifySent    = "sent"
	TradeNoticeNotifyFailed  = "failed"
	TradeNoticeNotifyExpired = "expired"
)

// TradeNotice 是一件不是用户当场操作、在后台发生的事，页面轮询新的通知弹出提示。和引起它的成交在同一个事务里写入。
// Side 是现货的 buy/sell 或合约仓位的 long/short，Action 是合约的 open/close；Qty 是数量(10^-8)，Price 是成交均价或强平价
// (追加保证金提醒是当时的风险率)；Pnl 是平仓的盈亏，Amount 是从站内额度扣的钱(追加保证金提醒是当时的借款本息，额度单位)。
type TradeNotice struct {
	Id        int    `json:"id" gorm:"index:idx_trade_notice_user,priority:2"`
	UserId    int    `json:"user_id" gorm:"index:idx_trade_notice_user,priority:1"`
	Kind      string `json:"kind" gorm:"type:varchar(16)"`
	Market    string `json:"market" gorm:"type:varchar(8)"`
	Symbol    string `json:"symbol" gorm:"type:varchar(20)"`
	Side      string `json:"side" gorm:"type:varchar(5)"`
	Action    string `json:"action" gorm:"type:varchar(5)"`
	OrderId   int    `json:"order_id"`
	Qty       int64  `json:"qty" gorm:"bigint"`
	Price     string `json:"price" gorm:"type:varchar(40)"`
	Pnl       int    `json:"pnl" gorm:"type:bigint"`
	Amount    int    `json:"amount" gorm:"type:bigint"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_trade_notice_notify,priority:2"`
	// 空值是升级前的历史通知，永不补发。发送状态不暴露给用户通知 API。
	NotifyState string `json:"-" gorm:"type:varchar(16);index:idx_trade_notice_notify,priority:1"`
}

// TradeDeficitNotifier 在亏空从用户的站内额度里扣掉、事务提交之后调用，由 service 设成按用户的通知设置(邮件、Webhook 等)
// 通知用户；为空时不发。
var TradeDeficitNotifier func(userId int, covered int)

// afterTradeDeficitCovered 在补亏空的事务提交之后刷新用户的额度缓存并通知用户，covered 为 0 时什么都不做。
func afterTradeDeficitCovered(userId int, covered int) {
	if covered <= 0 {
		return
	}
	syncCreditUserQuotaCache(userId, -covered, "trade deficit")
	if TradeDeficitNotifier != nil {
		TradeDeficitNotifier(userId, covered)
	}
}

func addTradeNoticeTx(tx *gorm.DB, notice TradeNotice) error {
	notice.CreatedAt = common.GetTimestamp()
	switch notice.Kind {
	case TradeNoticeTakeProfit, TradeNoticeStopLoss, TradeNoticeLiquidation, TradeNoticeFill, TradeNoticeMarginCall:
		notice.NotifyState = TradeNoticeNotifyPending
	}
	return tx.Create(&notice).Error
}

// ClaimTradeNoticeNotifications 只取已提交的新通知；CAS 保证多个节点也不会重复领取。
// 过期积压只保留站内通知。领取后不自动重试，避免外部渠道已收到但本地未确认时重复发送。
// 出错时仍返回此前成功领取的通知，调用方必须处理它们。
func ClaimTradeNoticeNotifications(notBefore int64, limit int) ([]TradeNotice, error) {
	if limit <= 0 {
		return nil, nil
	}
	if err := DB.Model(&TradeNotice{}).Where("notify_state = ? AND created_at < ?", TradeNoticeNotifyPending, notBefore).
		Update("notify_state", TradeNoticeNotifyExpired).Error; err != nil {
		return nil, err
	}
	var pending []TradeNotice
	if err := DB.Where("notify_state = ? AND created_at >= ?", TradeNoticeNotifyPending, notBefore).
		Order("id ASC").Limit(limit).Find(&pending).Error; err != nil {
		return nil, err
	}
	claimed := make([]TradeNotice, 0, len(pending))
	for _, notice := range pending {
		result := DB.Model(&TradeNotice{}).Where("id = ? AND notify_state = ?", notice.Id, TradeNoticeNotifyPending).
			Update("notify_state", TradeNoticeNotifySending)
		if result.Error != nil {
			return claimed, result.Error
		}
		if result.RowsAffected == 1 {
			notice.NotifyState = TradeNoticeNotifySending
			claimed = append(claimed, notice)
		}
	}
	return claimed, nil
}

// FinishTradeNoticeNotification 记录一次投递的结果，不把已结束的通知重新放回待发送队列。
func FinishTradeNoticeNotification(id int, delivered bool) error {
	state := TradeNoticeNotifyFailed
	if delivered {
		state = TradeNoticeNotifySent
	}
	return DB.Model(&TradeNotice{}).Where("id = ? AND notify_state = ?", id, TradeNoticeNotifySending).
		Update("notify_state", state).Error
}

// tradeFuturesFillNoticeTx 记一次合约成交的通知：止盈止损触发的平仓、挂着的限价单之后成交。pnl 是这次平仓的盈亏，开仓为 0。
func tradeFuturesFillNoticeTx(tx *gorm.DB, kind string, order *TradeFuturesOrder, fill TradeFuturesFill, pnl int) error {
	return addTradeNoticeTx(tx, TradeNotice{UserId: order.UserId, Kind: kind, Market: TradeNoticeFutures, Symbol: order.Symbol,
		Side: order.Side, Action: order.Action, OrderId: order.Id, Qty: fill.Qty, Price: fill.Price, Pnl: pnl})
}

// GetTradeNotices 返回用户 id 大于 afterId 的通知，新的在前，最多 limit 条，以及用户最新一条通知的 id(没有时为 0)。
func GetTradeNotices(userId int, afterId int, limit int) ([]TradeNotice, int, error) {
	var notices []TradeNotice
	if err := DB.Where("user_id = ? AND id > ?", userId, afterId).Order("id DESC").Limit(limit).Find(&notices).Error; err != nil {
		return nil, 0, err
	}
	var latest []TradeNotice
	if err := DB.Select("id").Where("user_id = ?", userId).Order("id DESC").Limit(1).Find(&latest).Error; err != nil {
		return nil, 0, err
	}
	latestId := 0
	if len(latest) > 0 {
		latestId = latest[0].Id
	}
	return notices, latestId, nil
}
