package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 合约仓位的强平、资金费、保证金与杠杆的调整、止盈止损的设置，以及合约的查询。

// liquidateTradeFuturesPositionTx 按标记价格 mark 强平整个仓位(按吃单费率 feeBps 收手续费)，记下系统的强平委托、账单与历史。
// 逐仓收回保证金加盈亏再减手续费，价格跳过了强平价、不够减时从资金里扣；全仓的盈亏与手续费记进资金。资金因此变成负的时由调用方
// 用 settleTradeDeficitTx 补回。调用方先撤掉仓位挂着的平仓委托，之后写回仓位。
func liquidateTradeFuturesPositionTx(tx *gorm.DB, pricing tradesim.Pricing, account *TradeAccount, position *TradeFuturesPosition, mark decimal.Decimal, feeBps int) error {
	entryValue, err := parseTradeValue(position.EntryValue)
	if err != nil {
		return err
	}
	qty := position.Qty
	value := mark.Mul(tradesim.QtyFromUnits(qty))
	pnl, err := pricing.Pnl(tradesim.PositionSide(position.Side), entryValue, value)
	if err != nil {
		return ErrTradeAmountInvalid
	}
	fee, err := pricing.TradeFee(value, feeBps)
	if err != nil {
		return ErrTradeAmountInvalid
	}
	amount := pnl - fee
	if position.MarginMode != TradeFuturesCross {
		amount += position.Margin
	}
	account.Cash += amount
	now := common.GetTimestamp()
	order := &TradeFuturesOrder{
		UserId:      position.UserId,
		Status:      TradeOrderStatusFilled,
		Symbol:      position.Symbol,
		Side:        position.Side,
		Action:      TradeFuturesClose,
		Type:        TradeOrderTypeMarket,
		MarginMode:  position.MarginMode,
		Leverage:    position.Leverage,
		PositionId:  position.PositionId,
		Qty:         qty,
		FilledQty:   qty,
		FilledValue: value.String(),
		Fee:         fee,
		RealizedPnl: pnl,
		Trigger:     TradeFuturesTriggerLiquidation,
		CreatedAt:   now,
		UpdatedAt:   now,
		FinishedAt:  now,
	}
	if err = tx.Create(order).Error; err != nil {
		return err
	}
	if position.CloseValue, err = addTradeValue(position.CloseValue, value); err != nil {
		return err
	}
	position.CloseQty += qty
	position.RealizedPnl += pnl
	position.Fees += fee
	position.Qty, position.FrozenQty, position.Margin, position.EntryValue = 0, 0, 0, "0"
	if err = tradeFuturesLedgerTx(tx, account, position, TradeLedger{Type: TradeLedgerLiquidation, OrderId: order.Id,
		Qty: qty, Price: mark.String(), Amount: amount, Fee: fee, Pnl: pnl}); err != nil {
		return err
	}
	return finishTradeFuturesPositionTx(tx, position, TradeFuturesCloseByLiquidation)
}

// LiquidateTradeFuturesPosition 在标记价格 mark 下强平一个逐仓仓位：保证金加按标记价格的浮动盈亏跌到维持保证金(按档位 brackets 算)时，
// 撤掉它挂着的平仓委托，按标记价格平掉，收回剩下的保证金减去手续费；价格跳过了强平价时超出的亏损从资金里扣，资金不够从站内额度扣。
// 事务里按锁住的仓位重新判断，已经不满足强平条件(用户刚追加了保证金、仓位已经平掉)时什么都不做，返回 false。
func LiquidateTradeFuturesPosition(userId int, symbol string, side string, mark decimal.Decimal, brackets tradesim.Brackets, feeBps int) (bool, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	liquidated, covered := false, 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		position, err := loadTradeFuturesPositionTx(tx, userId, symbol, side)
		if err != nil || position.Qty <= 0 || position.MarginMode == TradeFuturesCross {
			return err
		}
		entryValue, err := parseTradeValue(position.EntryValue)
		if err != nil {
			return err
		}
		hit, err := pricing.Liquidatable(tradesim.PositionSide(side), entryValue, position.Qty, position.Margin, mark, brackets)
		if err != nil || !hit {
			return err
		}
		if err = cancelTradeFuturesClosingTx(tx, account, position, TradeCancelByPosition); err != nil {
			return err
		}
		if err = liquidateTradeFuturesPositionTx(tx, pricing, account, position, mark, feeBps); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		liquidated = true
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return false, err
	}
	if covered > 0 {
		syncCreditUserQuotaCache(userId, -covered, "trade futures deficit")
	}
	return liquidated, nil
}

// LiquidateTradeFuturesCross 检查用户的全仓：全仓权益跌到全仓维持保证金时，按 market 的标记价格平掉全部全仓仓位、撤掉全仓开仓挂单；
// 平完资金还是负的从站内额度补回(见 settleTradeDeficitTx)。全仓权益是资金、挂着的全仓开仓委托冻结的钱(钱还是账户的，与 Binance 一样算进权益)与全仓浮动盈亏
// 之和。market 必须只给出足够新的标记价格，拿不到时什么都不做并返回 ErrTradeMarkUnavailable。
func LiquidateTradeFuturesCross(userId int, market TradeFuturesMarket, feeBps int) (bool, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	liquidated, covered := false, 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		var positions []TradeFuturesPosition
		if err = tx.Where("user_id = ? AND margin_mode = ? AND qty > 0", userId, TradeFuturesCross).Find(&positions).Error; err != nil {
			return err
		}
		state, err := TradeCrossStateOf(positions, market)
		if err != nil {
			return err
		}
		var orders []TradeFuturesOrder
		if err = lockForUpdate(tx).Where("user_id = ? AND margin_mode = ? AND action = ? AND status = ?",
			userId, TradeFuturesCross, TradeFuturesOpen, TradeOrderStatusOpen).Find(&orders).Error; err != nil {
			return err
		}
		pending := 0
		for _, order := range orders {
			pending += order.Frozen
		}
		if state.Positions == 0 || account.Cash+pending+state.Upnl > state.Maintenance {
			return nil
		}
		for i := range positions {
			position := &positions[i]
			mark, _ := market.MarkPrice(position.Symbol)
			if err = cancelTradeFuturesClosingTx(tx, account, position, TradeCancelByPosition); err != nil {
				return err
			}
			if err = liquidateTradeFuturesPositionTx(tx, pricing, account, position, mark, feeBps); err != nil {
				return err
			}
			if err = saveTradeFuturesPositionTx(tx, position); err != nil {
				return err
			}
		}
		for i := range orders {
			finishTradeFuturesOrder(account, &orders[i], TradeOrderStatusCanceled, TradeCancelByLiquidation)
			if err = saveTradeFuturesOrderTx(tx, &orders[i]); err != nil {
				return err
			}
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		liquidated = true
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return false, err
	}
	if covered > 0 {
		syncCreditUserQuotaCache(userId, -covered, "trade futures deficit")
	}
	return liquidated, nil
}

// ApplyTradeFuturesFunding 给一个仓位结算 fundingTime(毫秒)这一次的资金费：按结算时的标记价格与费率算出收付。逐仓记在仓位的保证金上
// (保证金不够付时，不够的部分从资金里扣)，全仓直接进出资金。仓位已经没有了、结算过这一次或者是在这之后才开的，什么都不做，返回 false。
func ApplyTradeFuturesFunding(userId int, symbol string, side string, fundingTime int64, rate decimal.Decimal, mark decimal.Decimal) (bool, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	applied, covered := false, 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		position, err := loadTradeFuturesPositionTx(tx, userId, symbol, side)
		if err != nil || position.Qty <= 0 || position.FundingAt >= fundingTime {
			return err
		}
		amount, err := pricing.Funding(tradesim.PositionSide(side), mark.Mul(tradesim.QtyFromUnits(position.Qty)), rate)
		if err != nil {
			return ErrTradeAmountInvalid
		}
		entry := TradeLedger{Type: TradeLedgerFuturesFunding, Qty: position.Qty, Price: rate.String()}
		if position.MarginMode == TradeFuturesCross {
			account.Cash += amount
			entry.Amount = amount
		} else {
			fromMargin := max(amount, -position.Margin)
			position.Margin += fromMargin
			account.Cash += amount - fromMargin
			entry.Amount = amount - fromMargin
		}
		entry.Pnl = amount
		position.Funding += amount
		position.FundingAt = fundingTime
		if err = tradeFuturesLedgerTx(tx, account, position, entry); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		applied = true
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return false, err
	}
	if covered > 0 {
		syncCreditUserQuotaCache(userId, -covered, "trade futures deficit")
	}
	return applied, nil
}

// AdjustTradeFuturesMargin 给逐仓仓位追加(amount > 0)或减少(amount < 0)保证金(额度单位)。追加要资金与全仓可用都够；减少后的保证金
// 加上浮动亏损(按标记价格 mark)不能低于按标记价格算的起始保证金，否则返回 ErrTradeFuturesMarginTooLow。全仓仓位没有自己的保证金。
func AdjustTradeFuturesMargin(userId int, symbol string, side string, amount int, mark decimal.Decimal, market TradeFuturesMarket) (*TradeFuturesPosition, error) {
	if amount == 0 || amount >= common.MaxQuota || amount <= -common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return nil, err
	}
	var position *TradeFuturesPosition
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		if position, err = loadTradeFuturesPositionTx(tx, userId, symbol, side); err != nil {
			return err
		}
		if position.Qty <= 0 {
			return ErrTradeFuturesNoPosition
		}
		if position.MarginMode == TradeFuturesCross {
			return ErrTradeFuturesCrossMargin
		}
		if amount > 0 {
			if err = checkTradeAffordTx(tx, account, amount, amount, market); err != nil {
				return err
			}
			position.MarginIn += amount
		} else {
			entryValue, err := parseTradeValue(position.EntryValue)
			if err != nil {
				return err
			}
			markValue := mark.Mul(tradesim.QtyFromUnits(position.Qty))
			pnl, err := pricing.Pnl(tradesim.PositionSide(side), entryValue, markValue)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			required, err := pricing.Margin(markValue, position.Leverage)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			if position.Margin+amount+min(pnl, 0) < required {
				return ErrTradeFuturesMarginTooLow
			}
			position.MarginIn = max(0, position.MarginIn+amount)
		}
		account.Cash -= amount
		position.Margin += amount
		if err = tradeFuturesLedgerTx(tx, account, position, TradeLedger{Type: TradeLedgerFuturesMargin, Amount: -amount}); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return nil, err
	}
	return position, nil
}

// SetTradeFuturesLevels 换掉仓位的一组止盈(kind 为 tp)或止损(kind 为 sl)，levels 为空表示全部取消。最多 4 档，每档的数量大于 0，
// 合计不超过仓位数量；触发价在标记价格的哪一边由调用方检查。
func SetTradeFuturesLevels(userId int, symbol string, side string, kind string, levels []TradeFuturesLevel) (*TradeFuturesPosition, error) {
	if kind != TradeFuturesTriggerTakeProfit && kind != TradeFuturesTriggerStopLoss || len(levels) > TradeFuturesMaxLevels {
		return nil, ErrTradeFuturesLevelsInvalid
	}
	for _, level := range levels {
		if level.Qty <= 0 {
			return nil, ErrTradeFuturesLevelsInvalid
		}
	}
	encoded, err := encodeTradeFuturesLevels(newTradeFuturesLevels(levels))
	if err != nil {
		return nil, err
	}
	var position *TradeFuturesPosition
	err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		var err error
		if position, err = loadTradeFuturesPositionTx(tx, userId, symbol, side); err != nil {
			return err
		}
		if position.Qty <= 0 {
			return ErrTradeFuturesNoPosition
		}
		if tradeFuturesLevelsQty(levels) > position.Qty {
			return ErrTradeFuturesLevelsInvalid
		}
		if kind == TradeFuturesTriggerTakeProfit {
			position.TakeProfits = encoded
		} else {
			position.StopLosses = encoded
		}
		return saveTradeFuturesPositionTx(tx, position)
	})
	if err != nil {
		return nil, err
	}
	return position, nil
}

// AdjustTradeFuturesLeverage 调整一个合约(多空两边仓位)的杠杆。合约上要有仓位、不能有开仓挂单；新杠杆不能超过仓位按标记价格的名义价值
// 所在档位的上限。逐仓只能调高：保证金按开仓价值重算，多出来的退回资金，退之前检查剩下的保证金够不够(同减少保证金)；全仓按开仓价值
// 重算占用，占用变多时全仓可用要够。
func AdjustTradeFuturesLeverage(userId int, symbol string, leverage int, market TradeFuturesMarket) ([]TradeFuturesPosition, error) {
	if leverage < 1 || market == nil {
		return nil, ErrTradeAmountInvalid
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return nil, err
	}
	var positions []TradeFuturesPosition
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		if err = tx.Where("user_id = ? AND symbol = ? AND qty > 0", userId, symbol).Find(&positions).Error; err != nil {
			return err
		}
		if len(positions) == 0 {
			return ErrTradeFuturesNoPosition
		}
		var open int64
		if err = tx.Model(&TradeFuturesOrder{}).Where("user_id = ? AND symbol = ? AND action = ? AND status = ?",
			userId, symbol, TradeFuturesOpen, TradeOrderStatusOpen).Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return ErrTradeFuturesOpenOrders
		}
		mark, ok := market.MarkPrice(symbol)
		if !ok {
			return ErrTradeMarkUnavailable
		}
		state, err := tradeCrossStateTx(tx, userId, market)
		if err != nil {
			return err
		}
		brackets := market.Brackets(symbol)
		crossDelta := 0
		for i := range positions {
			position := &positions[i]
			if position.Leverage == leverage {
				continue
			}
			markValue := mark.Mul(tradesim.QtyFromUnits(position.Qty))
			if limit := brackets.For(markValue).MaxLeverage; limit > 0 && leverage > limit {
				return ErrTradeFuturesLeverageTooHigh
			}
			entryValue, err := parseTradeValue(position.EntryValue)
			if err != nil {
				return err
			}
			target, err := pricing.Margin(entryValue, leverage)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			if position.MarginMode == TradeFuturesCross {
				crossDelta += target - position.Margin
				position.Margin, position.Leverage = target, leverage
				continue
			}
			if leverage < position.Leverage {
				return ErrTradeFuturesLeverageDown
			}
			position.Leverage = leverage
			release := position.Margin - target
			if release <= 0 {
				continue
			}
			pnl, err := pricing.Pnl(tradesim.PositionSide(position.Side), entryValue, markValue)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			required, err := pricing.Margin(markValue, leverage)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			maintenance, err := pricing.Maintenance(markValue, brackets)
			if err != nil {
				return ErrTradeAmountInvalid
			}
			if target+min(pnl, 0) < required || target < maintenance {
				return ErrTradeFuturesMarginTooLow
			}
			account.Cash += release
			position.Margin = target
			position.MarginIn = max(0, position.MarginIn-release)
			if err = tradeFuturesLedgerTx(tx, account, position, TradeLedger{Type: TradeLedgerFuturesMargin, Amount: release}); err != nil {
				return err
			}
		}
		if crossDelta > 0 && crossDelta > state.Available(account.Cash) {
			return ErrTradeCashInsufficient
		}
		for i := range positions {
			if err = saveTradeFuturesPositionTx(tx, &positions[i]); err != nil {
				return err
			}
		}
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return nil, err
	}
	return positions, nil
}

// CancelTradeFuturesClosing 撤掉用户一个仓位挂着的全部平仓委托(一键平仓、反手之前)，返回撤掉的委托。
func CancelTradeFuturesClosing(userId int, symbol string, side string) ([]TradeFuturesOrder, error) {
	var closing []TradeFuturesOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		if err = lockForUpdate(tx).Where("user_id = ? AND symbol = ? AND side = ? AND action = ? AND status = ?",
			userId, symbol, side, TradeFuturesClose, TradeOrderStatusOpen).Find(&closing).Error; err != nil {
			return err
		}
		for i := range closing {
			if err = cancelTradeFuturesOrderTx(tx, account, &closing[i], TradeCancelByUser); err != nil {
				return err
			}
		}
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return nil, err
	}
	return closing, nil
}

// GetTradeFuturesPositions 返回用户还持有的合约仓位。
func GetTradeFuturesPositions(userId int) ([]TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	err := DB.Where("user_id = ? AND qty > 0", userId).Order("symbol, side").Find(&positions).Error
	return positions, err
}

// GetTradeFuturesPosition 返回用户一个方向上的仓位，没有时数量为 0。
func GetTradeFuturesPosition(userId int, symbol string, side string) (*TradeFuturesPosition, error) {
	return loadTradeFuturesPositionTx(DB, userId, symbol, side)
}

// GetTradeFuturesOrders 按创建时间倒序分页列出合约委托。open 为真时只列挂着的，否则只列已经结束的；symbol 为空时不限合约。
func GetTradeFuturesOrders(userId int, open bool, symbol string, offset int, limit int) ([]TradeFuturesOrder, int64, error) {
	query := DB.Model(&TradeFuturesOrder{}).Where("user_id = ?", userId)
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
	var orders []TradeFuturesOrder
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&orders).Error
	return orders, total, err
}

// tradeFuturesFillTypes 是记一次合约成交的账单类型。
var tradeFuturesFillTypes = []string{TradeLedgerFuturesOpen, TradeLedgerFuturesClose, TradeLedgerLiquidation}

// GetTradeFuturesOrderFills 返回一笔合约委托的每一次成交(账单)。
func GetTradeFuturesOrderFills(userId int, orderId int) ([]TradeLedger, error) {
	var fills []TradeLedger
	err := DB.Where("user_id = ? AND order_id = ? AND type IN ?", userId, orderId, tradeFuturesFillTypes).Order("id").Find(&fills).Error
	return fills, err
}

// GetTradeFuturesFills 返回用户在一个合约上最近的成交，K 线图用它标出开平仓的位置。
func GetTradeFuturesFills(userId int, symbol string, limit int) ([]TradeLedger, error) {
	var fills []TradeLedger
	err := DB.Where("user_id = ? AND symbol = ? AND type IN ?", userId, symbol, tradeFuturesFillTypes).Order("id DESC").Limit(limit).Find(&fills).Error
	return fills, err
}

// GetTradeFuturesOrdersOf 按委托 id 查用户的这些合约委托(只有方向、开平、类型与触发来源)。
func GetTradeFuturesOrdersOf(userId int, orderIds []int) (map[int]TradeFuturesOrder, error) {
	orders := map[int]TradeFuturesOrder{}
	if len(orderIds) == 0 {
		return orders, nil
	}
	var rows []TradeFuturesOrder
	if err := DB.Select("id", "side", "action", "type", "trigger").Where("user_id = ? AND id IN ?", userId, orderIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, order := range rows {
		orders[order.Id] = order
	}
	return orders, nil
}

// GetTradeFuturesHistory 按结束时间倒序分页列出已经结束的仓位，symbol 为空时不限合约。
func GetTradeFuturesHistory(userId int, symbol string, offset int, limit int) ([]TradeFuturesHistory, int64, error) {
	query := DB.Model(&TradeFuturesHistory{}).Where("user_id = ?", userId)
	if symbol != "" {
		query = query.Where("symbol = ?", symbol)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []TradeFuturesHistory
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// GetTradeFuturesPositionFills 返回这几段仓位的每一次成交(开仓、平仓与强平的账单)，按时间先后。编号 0 不是仓位(现货账单的
// PositionId 都是 0)，跳过。
func GetTradeFuturesPositionFills(userId int, positionIds []int) ([]TradeLedger, error) {
	var fills []TradeLedger
	ids := make([]int, 0, len(positionIds))
	for _, id := range positionIds {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return fills, nil
	}
	err := DB.Where("user_id = ? AND position_id IN ? AND type IN ?", userId, ids, tradeFuturesFillTypes).Order("id").Find(&fills).Error
	return fills, err
}

// ListOpenTradeFuturesPositions 返回所有还持有的合约仓位，供强平与止盈止损建立索引。
func ListOpenTradeFuturesPositions() ([]TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	err := DB.Where("qty > 0").Find(&positions).Error
	return positions, err
}

// ListTradeFuturesPositionSymbols 返回还有人持仓的合约：合约关掉以后，这些合约的行情也要一直连着，直到仓位都平掉。
func ListTradeFuturesPositionSymbols() ([]string, error) {
	var symbols []string
	err := DB.Model(&TradeFuturesPosition{}).Where("qty > 0").Distinct("symbol").Pluck("symbol", &symbols).Error
	return symbols, err
}

// ListTradeFuturesFundingDue 返回这个合约上在 fundingTime(毫秒)之前开仓、还没结算这一次资金费的仓位。
func ListTradeFuturesFundingDue(symbol string, fundingTime int64) ([]TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	err := DB.Where("symbol = ? AND qty > 0 AND funding_at < ?", symbol, fundingTime).Find(&positions).Error
	return positions, err
}

// GetTradeFuturesFundingFrom 返回这个合约上还没结算资金费的仓位里最早的结算起点(毫秒)，没有仓位时 ok 为假。
func GetTradeFuturesFundingFrom(symbol string) (int64, bool, error) {
	var row struct {
		Positions int64
		Earliest  int64
	}
	err := DB.Model(&TradeFuturesPosition{}).Where("symbol = ? AND qty > 0", symbol).
		Select("COUNT(*) AS positions, COALESCE(MIN(funding_at), 0) AS earliest").Scan(&row).Error
	if err != nil {
		return 0, false, err
	}
	return row.Earliest, row.Positions > 0, nil
}

// ListOpenTradeFuturesOrders 返回所有挂着的合约限价委托，供撮合建立索引。
func ListOpenTradeFuturesOrders() ([]TradeFuturesOrder, error) {
	var orders []TradeFuturesOrder
	err := DB.Where("status = ?", TradeOrderStatusOpen).Order("id").Find(&orders).Error
	return orders, err
}

// ListTradeFuturesCrossPending 返回这些用户挂着的全仓开仓委托冻结的钱的合计，全仓权益要算上它(见 LiquidateTradeFuturesCross)。
func ListTradeFuturesCrossPending(userIds []int) (map[int]int, error) {
	pending := map[int]int{}
	if len(userIds) == 0 {
		return pending, nil
	}
	var rows []struct {
		UserId int
		Frozen int
	}
	if err := DB.Model(&TradeFuturesOrder{}).Select("user_id, COALESCE(SUM(frozen), 0) AS frozen").
		Where("user_id IN ? AND margin_mode = ? AND action = ? AND status = ?", userIds, TradeFuturesCross, TradeFuturesOpen, TradeOrderStatusOpen).
		Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		pending[row.UserId] = row.Frozen
	}
	return pending, nil
}

// ListTradeFuturesPositionsOf 返回这些用户还持有的合约仓位，拍快照用。
func ListTradeFuturesPositionsOf(userIds []int) ([]TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	if len(userIds) == 0 {
		return positions, nil
	}
	err := DB.Where("user_id IN ? AND qty > 0", userIds).Find(&positions).Error
	return positions, err
}
