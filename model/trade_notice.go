package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 通知的种类：止盈、止损、强平、挂着的限价单成交、亏空从站内额度扣除。
const (
	TradeNoticeTakeProfit  = "tp"
	TradeNoticeStopLoss    = "sl"
	TradeNoticeLiquidation = "liquidation"
	TradeNoticeFill        = "fill"
	TradeNoticeCover       = "cover"

	TradeNoticeSpot    = "spot"
	TradeNoticeFutures = "futures"
)

// TradeNotice 是一件不是用户当场操作、在后台发生的事，页面轮询新的通知弹出提示。和引起它的成交在同一个事务里写入。
// Side 是现货的 buy/sell 或合约仓位的 long/short，Action 是合约的 open/close；Qty 是数量(10^-8)，Price 是成交均价或强平价；
// Pnl 是平仓的盈亏，Amount 是从站内额度扣的钱(额度单位)。
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
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func addTradeNoticeTx(tx *gorm.DB, notice TradeNotice) error {
	notice.CreatedAt = common.GetTimestamp()
	return tx.Create(&notice).Error
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
