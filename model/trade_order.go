package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TradeOrder 是一笔委托。市价单下单时就按盘口成交完，吃不完的部分撤销；限价单没成交的部分挂着，等盘口满足限价时由撮合成交。
type TradeOrder struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"index:idx_trade_order_user_status,priority:1"`
	Status string `json:"status" gorm:"type:varchar(10);index:idx_trade_order_user_status,priority:2;index:idx_trade_order_status_symbol,priority:1"`
	Symbol string `json:"symbol" gorm:"type:varchar(20);index:idx_trade_order_status_symbol,priority:2"`
	Side   string `json:"side" gorm:"type:varchar(4)"`
	Type   string `json:"type" gorm:"type:varchar(8)"`
	// Price 是限价，市价单为空。
	Price string `json:"price" gorm:"type:varchar(40)"`
	// Qty 是委托数量(10^-8)。按金额买入的市价单下单时不知道数量，记成实际成交的数量，预算记在 Budget(含手续费)。
	Qty    int64 `json:"qty" gorm:"bigint"`
	Budget int   `json:"budget" gorm:"type:bigint"`
	// FilledQty、FilledAmount、Fee 是累计成交的数量、成交金额(不含手续费)与手续费。
	FilledQty    int64 `json:"filled_qty" gorm:"bigint"`
	FilledAmount int   `json:"filled_amount" gorm:"type:bigint"`
	Fee          int   `json:"fee" gorm:"type:bigint"`
	// Frozen 是限价买单还冻结着的资金，成交时从这里付款，结束时退回剩下的。
	Frozen       int    `json:"frozen" gorm:"type:bigint"`
	CancelReason string `json:"cancel_reason" gorm:"type:varchar(16)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
	FinishedAt   int64  `json:"finished_at" gorm:"bigint"`
}

// TradeOrderInput 是一笔新委托与下单时立刻成交的部分。成交由调用方按盘口算好，这里只检查账户能不能承担并记账。
type TradeOrderInput struct {
	UserId int
	Symbol string
	Side   string
	Type   string
	Price  string
	Qty    int64
	Budget int
	// Fill 是下单时立刻成交的部分，可以为空。
	Fill TradeFill
	// Rest 为真时没成交的部分挂单等待(限价单)，否则撤销(市价单)。
	Rest bool
	// Freeze 是限价买单挂单部分要冻结的资金(按限价算，含手续费)。
	Freeze int
	// MaxPositionCost 是这个交易对的持仓成本加上买单冻结的资金最多多少(额度单位)，0 表示不限制。
	MaxPositionCost int
}

func loadTradePositionTx(tx *gorm.DB, userId int, symbol string) (*TradePosition, error) {
	var positions []TradePosition
	if err := tx.Where("user_id = ? AND symbol = ?", userId, symbol).Limit(1).Find(&positions).Error; err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return &TradePosition{UserId: userId, Symbol: symbol}, nil
	}
	return &positions[0], nil
}

func saveTradePositionTx(tx *gorm.DB, position *TradePosition) error {
	if position.Qty < 0 || position.FrozenQty < 0 || position.FrozenQty > position.Qty || position.Cost < 0 || position.Cost >= common.MaxQuota {
		return ErrTradePositionInsufficient
	}
	position.UpdatedAt = common.GetTimestamp()
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "symbol"}},
		DoUpdates: clause.AssignmentColumns([]string{"qty", "frozen_qty", "cost", "updated_at"}),
	}).Create(position).Error
}

func saveTradeOrderTx(tx *gorm.DB, order *TradeOrder) error {
	if order.Frozen < 0 || order.FilledQty > order.Qty {
		return ErrTradeAmountInvalid
	}
	order.UpdatedAt = common.GetTimestamp()
	return tx.Save(order).Error
}

// applyTradeFillTx 把一次成交记进账户、持仓与委托，并写一条账单。买入先用委托冻结的资金付款，不够的从可用资金里扣；
// 卖出按数量比例结转持仓成本，挂单冻结的数量由调用方先释放。
func applyTradeFillTx(tx *gorm.DB, account *TradeAccount, position *TradePosition, order *TradeOrder, fill TradeFill) error {
	if fill.Qty <= 0 || fill.Amount < 0 || fill.Fee < 0 || fill.Amount >= common.MaxQuota || fill.Fee >= common.MaxQuota {
		return ErrTradeAmountInvalid
	}
	entry := TradeLedger{Symbol: order.Symbol, OrderId: order.Id, Qty: fill.Qty, Price: fill.Price, Fee: fill.Fee}
	if order.Side == TradeSideBuy {
		cost := fill.Amount + fill.Fee
		fromFrozen := min(cost, order.Frozen)
		if cost-fromFrozen > account.Cash {
			return ErrTradeCashInsufficient
		}
		order.Frozen -= fromFrozen
		account.Frozen -= fromFrozen
		account.Cash -= cost - fromFrozen
		position.Qty += fill.Qty
		position.Cost += cost
		entry.Type, entry.Amount = TradeLedgerBuy, -cost
	} else {
		if fill.Qty > position.Qty-position.FrozenQty || fill.Fee > fill.Amount {
			return ErrTradePositionInsufficient
		}
		released := tradeCostShare(position.Cost, fill.Qty, position.Qty)
		position.Qty -= fill.Qty
		position.Cost -= released
		proceeds := fill.Amount - fill.Fee
		account.Cash += proceeds
		entry.Type, entry.Amount = TradeLedgerSell, proceeds
	}
	order.FilledQty += fill.Qty
	order.FilledAmount += fill.Amount
	order.Fee += fill.Fee
	return tradeLedgerTx(tx, account, entry)
}

// finishTradeOrder 结束一笔委托：限价买单退回还冻结着的资金。status 是 filled 或 canceled。
func finishTradeOrder(account *TradeAccount, order *TradeOrder, status string, reason string) {
	account.Frozen -= order.Frozen
	account.Cash += order.Frozen
	order.Frozen = 0
	order.Status = status
	order.CancelReason = reason
	order.FinishedAt = common.GetTimestamp()
}

// PlaceTradeOrder 在一个事务里检查资金、持仓与上限，记下委托和下单时立刻成交的部分；限价单没成交的部分冻结资金或数量后挂单。
func PlaceTradeOrder(in TradeOrderInput) (*TradeOrder, error) {
	if in.Qty < 0 || in.Budget < 0 || in.Freeze < 0 || in.Budget >= common.MaxQuota || in.Freeze >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	if !in.Rest && in.Fill.Qty <= 0 {
		return nil, ErrTradeNoFill
	}
	// 挂单要有限价，否则撮合永远成交不了它，冻结的资金只能等用户撤单。
	if in.Rest && in.Price == "" {
		return nil, ErrTradeAmountInvalid
	}
	now := common.GetTimestamp()
	order := &TradeOrder{
		UserId:    in.UserId,
		Status:    TradeOrderStatusOpen,
		Symbol:    in.Symbol,
		Side:      in.Side,
		Type:      in.Type,
		Price:     in.Price,
		Qty:       in.Qty,
		Budget:    in.Budget,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if in.Budget > 0 {
		order.Qty = in.Fill.Qty
	}
	remaining := order.Qty - in.Fill.Qty
	if remaining < 0 {
		return nil, ErrTradeAmountInvalid
	}
	resting := in.Rest && remaining > 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, in.UserId)
		if err != nil {
			return err
		}
		if resting {
			var open int64
			if err = tx.Model(&TradeOrder{}).Where("user_id = ? AND status = ?", in.UserId, TradeOrderStatusOpen).Count(&open).Error; err != nil {
				return err
			}
			if open >= TradeMaxOpenOrders {
				return ErrTradeOpenOrderLimit
			}
		}
		position, err := loadTradePositionTx(tx, in.UserId, in.Symbol)
		if err != nil {
			return err
		}
		freeze := 0
		if in.Side == TradeSideBuy {
			if resting {
				freeze = in.Freeze
			}
			need := in.Fill.Amount + in.Fill.Fee + freeze
			if need > account.Cash {
				return ErrTradeCashInsufficient
			}
			if in.MaxPositionCost > 0 {
				var frozenBuys int
				if err = tx.Model(&TradeOrder{}).
					Where("user_id = ? AND symbol = ? AND side = ? AND status = ?", in.UserId, in.Symbol, TradeSideBuy, TradeOrderStatusOpen).
					Select("COALESCE(SUM(frozen), 0)").Scan(&frozenBuys).Error; err != nil {
					return err
				}
				if position.Cost+frozenBuys+need > in.MaxPositionCost {
					return ErrTradePositionLimit
				}
			}
		} else {
			reserve := in.Fill.Qty
			if resting {
				reserve = order.Qty
			}
			if reserve > position.Qty-position.FrozenQty {
				return ErrTradePositionInsufficient
			}
		}
		if err = tx.Create(order).Error; err != nil {
			return err
		}
		if in.Fill.Qty > 0 {
			if err = applyTradeFillTx(tx, account, position, order, in.Fill); err != nil {
				return err
			}
		}
		switch {
		case resting && in.Side == TradeSideBuy:
			account.Cash -= freeze
			account.Frozen += freeze
			order.Frozen = freeze
		case resting:
			position.FrozenQty += remaining
		case remaining == 0:
			finishTradeOrder(account, order, TradeOrderStatusFilled, "")
		default:
			finishTradeOrder(account, order, TradeOrderStatusCanceled, TradeCancelByDepth)
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = saveTradePositionTx(tx, position); err != nil {
			return err
		}
		return saveTradeOrderTx(tx, order)
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// FillTradeOrder 把撮合算出的一次成交记到一笔挂着的限价委托上，成交完就结束委托并退回多冻结的资金。委托已经不在挂单状态时
// 返回 ErrTradeOrderNotOpen；限价买单冻结的资金加上可用资金不够付这次成交时返回 ErrTradeCashInsufficient，都什么都不改。
func FillTradeOrder(orderId int, fill TradeFill) (*TradeOrder, error) {
	var order TradeOrder
	if err := DB.Where("id = ?", orderId).First(&order).Error; err != nil {
		return nil, err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, order.UserId)
		if err != nil {
			return err
		}
		var orders []TradeOrder
		if err = lockForUpdate(tx).Where("id = ? AND status = ?", orderId, TradeOrderStatusOpen).Limit(1).Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) == 0 {
			return ErrTradeOrderNotOpen
		}
		order = orders[0]
		if fill.Qty > order.Qty-order.FilledQty {
			return ErrTradeAmountInvalid
		}
		position, err := loadTradePositionTx(tx, order.UserId, order.Symbol)
		if err != nil {
			return err
		}
		if order.Side == TradeSideSell {
			if fill.Qty > position.FrozenQty {
				return ErrTradePositionInsufficient
			}
			position.FrozenQty -= fill.Qty
		}
		if err = applyTradeFillTx(tx, account, position, &order, fill); err != nil {
			return err
		}
		if order.FilledQty == order.Qty {
			finishTradeOrder(account, &order, TradeOrderStatusFilled, "")
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = saveTradePositionTx(tx, position); err != nil {
			return err
		}
		return saveTradeOrderTx(tx, &order)
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// CancelTradeOrder 撤销一笔挂着的限价委托，退回冻结的资金或数量。userId 为 0 时是系统撤单(例如交易对下架)，不核对用户。
// 委托已经不在挂单状态时返回 ErrTradeOrderNotOpen。
func CancelTradeOrder(userId int, orderId int, reason string) (*TradeOrder, error) {
	var order TradeOrder
	query := DB.Where("id = ?", orderId)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if err := query.First(&order).Error; err != nil {
		return nil, err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, order.UserId)
		if err != nil {
			return err
		}
		var orders []TradeOrder
		if err = lockForUpdate(tx).Where("id = ? AND status = ?", orderId, TradeOrderStatusOpen).Limit(1).Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) == 0 {
			return ErrTradeOrderNotOpen
		}
		order = orders[0]
		if order.Side == TradeSideSell {
			position, err := loadTradePositionTx(tx, order.UserId, order.Symbol)
			if err != nil {
				return err
			}
			position.FrozenQty -= order.Qty - order.FilledQty
			if err = saveTradePositionTx(tx, position); err != nil {
				return err
			}
		}
		finishTradeOrder(account, &order, TradeOrderStatusCanceled, reason)
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return saveTradeOrderTx(tx, &order)
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetTradeOrders 按创建时间倒序分页列出委托。open 为真时只列挂着的，否则只列已经结束的；symbol 为空时不限交易对。
func GetTradeOrders(userId int, open bool, symbol string, offset int, limit int) ([]TradeOrder, int64, error) {
	query := DB.Model(&TradeOrder{}).Where("user_id = ?", userId)
	if open {
		query = query.Where("status = ?", TradeOrderStatusOpen)
	} else {
		query = query.Where("status IN ?", []string{TradeOrderStatusFilled, TradeOrderStatusCanceled})
	}
	if symbol != "" {
		query = query.Where("symbol = ?", symbol)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []TradeOrder
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&orders).Error
	return orders, total, err
}

// GetTradeOrderFills 返回一笔委托的每一次成交(账单)。
func GetTradeOrderFills(userId int, orderId int) ([]TradeLedger, error) {
	var fills []TradeLedger
	err := DB.Where("user_id = ? AND order_id = ? AND type IN ?", userId, orderId, []string{TradeLedgerBuy, TradeLedgerSell}).
		Order("id").Find(&fills).Error
	return fills, err
}

// GetTradeFills 返回用户在一个交易对上最近的成交，K 线图用它标出买卖点。
func GetTradeFills(userId int, symbol string, limit int) ([]TradeLedger, error) {
	var fills []TradeLedger
	err := DB.Where("user_id = ? AND symbol = ? AND type IN ?", userId, symbol, []string{TradeLedgerBuy, TradeLedgerSell}).
		Order("id DESC").Limit(limit).Find(&fills).Error
	return fills, err
}

// GetTradeLedgers 按时间倒序分页列出账单，ledgerType 为空时不限类型。
func GetTradeLedgers(userId int, ledgerType string, offset int, limit int) ([]TradeLedger, int64, error) {
	query := DB.Model(&TradeLedger{}).Where("user_id = ?", userId)
	if ledgerType != "" {
		query = query.Where("type = ?", ledgerType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []TradeLedger
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&entries).Error
	return entries, total, err
}

// ListOpenTradeOrders 返回所有挂着的限价委托，供撮合建立索引。
func ListOpenTradeOrders() ([]TradeOrder, error) {
	var orders []TradeOrder
	err := DB.Where("status = ?", TradeOrderStatusOpen).Order("id").Find(&orders).Error
	return orders, err
}
