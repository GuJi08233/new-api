package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 合约(U 本位永续)只有逐仓、双向持仓：每人每个合约多空各一个仓位，各有自己的杠杆与保证金。开仓从可用资金里拿出保证金和
// 手续费，平仓把释放的保证金加上盈亏、减去手续费放回可用资金；一个仓位最多亏掉它的保证金，账户不会变成负数，
// 价格跳空越过强平价时多出来的亏损由站点承担。资金变动与现货一样先锁住账户行，仓位与委托不再单独加锁。

const (
	TradeFuturesLong  = "long"
	TradeFuturesShort = "short"

	TradeFuturesOpen  = "open"
	TradeFuturesClose = "close"

	// 系统替用户下的平仓委托的来源，用户自己下的委托为空。
	TradeFuturesTriggerTakeProfit  = "tp"
	TradeFuturesTriggerStopLoss    = "sl"
	TradeFuturesTriggerLiquidation = "liquidation"

	// TradeCancelByPosition 是平仓委托因为仓位已经平掉或强平而撤销。
	TradeCancelByPosition = "position"

	// TradeFuturesCloseByUser 与 TradeFuturesCloseByLiquidation 是仓位结束的方式，记在历史里。
	TradeFuturesCloseByUser        = "close"
	TradeFuturesCloseByLiquidation = "liquidation"
)

// 合约账单的类型。资金费与强平不动可用资金(资金费记在仓位的保证金上，强平亏掉的是保证金)，账单的资金变动为 0，
// 盈亏记在 Pnl 上。
const (
	TradeLedgerFuturesOpen    = "futures_open"
	TradeLedgerFuturesClose   = "futures_close"
	TradeLedgerFuturesMargin  = "futures_margin"
	TradeLedgerFuturesFunding = "futures_funding"
	TradeLedgerLiquidation    = "liquidation"
)

var (
	ErrTradeFuturesNoPosition       = errors.New("trade futures position does not exist")
	ErrTradeFuturesLeverageMismatch = errors.New("trade futures leverage differs from the open position")
	ErrTradeFuturesMarginTooLow     = errors.New("trade futures margin would fall below the requirement")
	ErrTradeFuturesTriggerChanged   = errors.New("trade futures take profit or stop loss has changed")
)

// TradeFuturesPosition 是一个合约一个方向上的逐仓仓位，以用户、合约与方向为主键。数量为 0 表示没有仓位，这一行留着下次开仓
// 复用；仓位平完(或强平)时把这一段的汇总写进 TradeFuturesHistory。
type TradeFuturesPosition struct {
	UserId   int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Symbol   string `json:"symbol" gorm:"type:varchar(20);primaryKey"`
	Side     string `json:"side" gorm:"type:varchar(5);primaryKey"`
	Leverage int    `json:"leverage"`
	// Qty 是持仓数量(10^-8)，含 FrozenQty；FrozenQty 是挂着的限价平仓委托占着的数量。
	Qty       int64 `json:"qty" gorm:"bigint;not null;default:0"`
	FrozenQty int64 `json:"frozen_qty" gorm:"bigint;not null;default:0"`
	// EntryValue 是还持有的部分的开仓价值(USDT 小数)：各次开仓的成交价 × 数量之和，减去平仓时按比例结转的部分。开仓均价 = EntryValue / Qty。
	EntryValue string `json:"entry_value" gorm:"type:varchar(64)"`
	// Margin 是逐仓保证金(额度单位)：开仓投入、追加或减少、资金费的收付都记在这里，平仓按数量比例释放。
	Margin int `json:"margin" gorm:"type:bigint;not null;default:0"`
	// TakeProfit 与 StopLoss 是止盈、止损的触发价(按标记价格)，空表示没设。触发时按盘口市价平掉整个仓位。
	TakeProfit string `json:"take_profit" gorm:"type:varchar(40)"`
	StopLoss   string `json:"stop_loss" gorm:"type:varchar(40)"`
	// 下面是这一段仓位(从开仓到平完)的累计：开仓与平仓的数量和价值、毛盈亏、手续费、资金费(收到为正)与投入的保证金。
	OpenQty     int64  `json:"open_qty" gorm:"bigint"`
	OpenValue   string `json:"open_value" gorm:"type:varchar(64)"`
	CloseQty    int64  `json:"close_qty" gorm:"bigint"`
	CloseValue  string `json:"close_value" gorm:"type:varchar(64)"`
	RealizedPnl int    `json:"realized_pnl" gorm:"type:bigint"`
	Fees        int    `json:"fees" gorm:"type:bigint"`
	Funding     int    `json:"funding" gorm:"type:bigint"`
	MarginIn    int    `json:"margin_in" gorm:"type:bigint"`
	OpenedAt    int64  `json:"opened_at" gorm:"bigint"`
	// FundingAt 是这个仓位已经结算到的资金费时间(毫秒)，开仓时记成开仓时间：只结算之后的资金费，同一次不会结算两遍。
	FundingAt int64 `json:"funding_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// TradeFuturesHistory 是一段已经结束的仓位：从开仓到平完或强平的汇总。
type TradeFuturesHistory struct {
	Id       int    `json:"id" gorm:"index:idx_trade_futures_history_user,priority:2"`
	UserId   int    `json:"user_id" gorm:"index:idx_trade_futures_history_user,priority:1"`
	Symbol   string `json:"symbol" gorm:"type:varchar(20)"`
	Side     string `json:"side" gorm:"type:varchar(5)"`
	Leverage int    `json:"leverage"`
	// Qty 是这一段累计开仓的数量，EntryPrice 与 ClosePrice 是开仓、平仓均价(强平按强平时的标记价格)。
	Qty        int64  `json:"qty" gorm:"bigint"`
	EntryPrice string `json:"entry_price" gorm:"type:varchar(40)"`
	ClosePrice string `json:"close_price" gorm:"type:varchar(40)"`
	// Pnl 是净盈亏 = RealizedPnl - Fees + Funding，正好是这一段从可用资金拿出去与放回来的差。
	RealizedPnl int    `json:"realized_pnl" gorm:"type:bigint"`
	Fees        int    `json:"fees" gorm:"type:bigint"`
	Funding     int    `json:"funding" gorm:"type:bigint"`
	Pnl         int    `json:"pnl" gorm:"type:bigint"`
	MarginIn    int    `json:"margin_in" gorm:"type:bigint"`
	CloseReason string `json:"close_reason" gorm:"type:varchar(16)"`
	OpenedAt    int64  `json:"opened_at" gorm:"bigint"`
	ClosedAt    int64  `json:"closed_at" gorm:"bigint"`
}

// TradeFuturesOrder 是一笔合约委托：开仓或平仓，市价或限价。市价单下单时就按盘口成交完，吃不完的部分撤销；限价单没成交的部分挂着，
// 由主节点的撮合在盘口满足限价时成交。止盈止损与强平由系统下市价平仓委托，Trigger 记下原因。
type TradeFuturesOrder struct {
	Id       int    `json:"id"`
	UserId   int    `json:"user_id" gorm:"index:idx_trade_futures_order_user_status,priority:1"`
	Status   string `json:"status" gorm:"type:varchar(10);index:idx_trade_futures_order_user_status,priority:2;index:idx_trade_futures_order_status_symbol,priority:1"`
	Symbol   string `json:"symbol" gorm:"type:varchar(20);index:idx_trade_futures_order_status_symbol,priority:2"`
	Side     string `json:"side" gorm:"type:varchar(5)"`
	Action   string `json:"action" gorm:"type:varchar(5)"`
	Type     string `json:"type" gorm:"type:varchar(8)"`
	Leverage int    `json:"leverage"`
	// Price 是限价，市价单为空。
	Price string `json:"price" gorm:"type:varchar(40)"`
	Qty   int64  `json:"qty" gorm:"bigint"`
	// FilledQty、FilledValue(成交价 × 数量之和，USDT 小数)、Fee 与 RealizedPnl(平仓的毛盈亏)是累计的成交。
	FilledQty   int64  `json:"filled_qty" gorm:"bigint"`
	FilledValue string `json:"filled_value" gorm:"type:varchar(64)"`
	Fee         int    `json:"fee" gorm:"type:bigint"`
	RealizedPnl int    `json:"realized_pnl" gorm:"type:bigint"`
	// Frozen 是限价开仓还冻结着的保证金与手续费，Reserved 是还没成交部分的名义价值(额度单位，算持仓上限用)。
	Frozen   int `json:"frozen" gorm:"type:bigint"`
	Reserved int `json:"reserved" gorm:"type:bigint"`
	// TakeProfit 与 StopLoss 是开仓委托成交后要设到仓位上的止盈止损，空表示不改。
	TakeProfit   string `json:"take_profit" gorm:"type:varchar(40)"`
	StopLoss     string `json:"stop_loss" gorm:"type:varchar(40)"`
	Trigger      string `json:"trigger" gorm:"type:varchar(16)"`
	CancelReason string `json:"cancel_reason" gorm:"type:varchar(16)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
	FinishedAt   int64  `json:"finished_at" gorm:"bigint"`
}

// TradeFuturesFill 是一次合约成交：数量(10^-8)、成交价值(成交价 × 数量之和，USDT)、手续费(额度单位)与成交均价。
type TradeFuturesFill struct {
	Qty   int64
	Value decimal.Decimal
	Fee   int
	Price string
}

// TradeFuturesOrderInput 是一笔新的合约委托与下单时立刻成交的部分。成交由调用方按盘口算好，这里只检查账户能不能承担并记账。
type TradeFuturesOrderInput struct {
	UserId   int
	Symbol   string
	Side     string
	Action   string
	Type     string
	Leverage int
	Price    string
	Qty      int64
	// Fill 是下单时立刻成交的部分，可以为空。
	Fill TradeFuturesFill
	// Rest 为真时没成交的部分挂单等待(限价单)，否则撤销(市价单)。
	Rest bool
	// Freeze 与 Reserve 是限价开仓挂单部分要冻结的保证金加手续费、占用的名义价值(额度单位，按限价算)。
	Freeze  int
	Reserve int
	// MaxPositionValue 是这个合约多空仓位的开仓价值加上挂着的开仓委托最多多少(额度单位)，0 表示不限制。
	MaxPositionValue int
	TakeProfit       string
	StopLoss         string
	// Trigger 是系统下的平仓委托的来源(止盈、止损)，TriggerPrice 是触发它的那个止盈或止损价：事务里核对仓位上的设置没被改掉，
	// 并先撤掉仓位挂着的平仓委托，按市价平掉整个仓位。
	Trigger      string
	TriggerPrice string
}

// tradeFuturesPricing 是合约记账用的换算参数，手续费由调用方算好传进来。
func tradeFuturesPricing() (tradesim.Pricing, error) {
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return tradesim.Pricing{}, err
	}
	return tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}, nil
}

// parseTradeValue 解析库里记的 USDT 小数，空串是 0。
func parseTradeValue(value string) (decimal.Decimal, error) {
	if value == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(value)
}

func loadTradeFuturesPositionTx(tx *gorm.DB, userId int, symbol string, side string) (*TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	if err := tx.Where("user_id = ? AND symbol = ? AND side = ?", userId, symbol, side).Limit(1).Find(&positions).Error; err != nil {
		return nil, err
	}
	if len(positions) == 0 {
		return &TradeFuturesPosition{UserId: userId, Symbol: symbol, Side: side}, nil
	}
	return &positions[0], nil
}

func saveTradeFuturesPositionTx(tx *gorm.DB, position *TradeFuturesPosition) error {
	if position.Qty < 0 || position.FrozenQty < 0 || position.FrozenQty > position.Qty || position.Margin < 0 || position.Margin >= common.MaxQuota {
		return ErrTradePositionInsufficient
	}
	position.UpdatedAt = common.GetTimestamp()
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "symbol"}, {Name: "side"}},
		DoUpdates: clause.AssignmentColumns([]string{"leverage", "qty", "frozen_qty", "entry_value", "margin", "take_profit", "stop_loss",
			"open_qty", "open_value", "close_qty", "close_value", "realized_pnl", "fees", "funding", "margin_in", "opened_at", "funding_at", "updated_at"}),
	}).Create(position).Error
}

func saveTradeFuturesOrderTx(tx *gorm.DB, order *TradeFuturesOrder) error {
	if order.Frozen < 0 || order.Reserved < 0 || order.FilledQty > order.Qty {
		return ErrTradeAmountInvalid
	}
	order.UpdatedAt = common.GetTimestamp()
	return tx.Save(order).Error
}

// addTradeValue 把一个 USDT 小数加到库里记的小数上。
func addTradeValue(stored string, delta decimal.Decimal) (string, error) {
	value, err := parseTradeValue(stored)
	if err != nil {
		return "", err
	}
	return value.Add(delta).String(), nil
}

// startTradeFuturesPosition 在没有仓位时开始新的一段：清掉上一段的累计，记下杠杆、开仓时间与资金费的起点。
func startTradeFuturesPosition(position *TradeFuturesPosition, leverage int) {
	now := common.GetTimestamp()
	*position = TradeFuturesPosition{
		UserId:     position.UserId,
		Symbol:     position.Symbol,
		Side:       position.Side,
		Leverage:   leverage,
		EntryValue: "0",
		OpenValue:  "0",
		CloseValue: "0",
		OpenedAt:   now,
		FundingAt:  now * 1000,
	}
}

// applyTradeFuturesOpenTx 把一次开仓成交记进账户、仓位与委托：保证金按成交价值与仓位杠杆算，保证金和手续费先从委托冻结的资金里付，
// 不够的从可用资金里扣。
func applyTradeFuturesOpenTx(tx *gorm.DB, pricing tradesim.Pricing, account *TradeAccount, position *TradeFuturesPosition, order *TradeFuturesOrder, fill TradeFuturesFill) error {
	if fill.Qty <= 0 || !fill.Value.IsPositive() || fill.Fee < 0 || fill.Fee >= common.MaxQuota {
		return ErrTradeAmountInvalid
	}
	if position.Qty == 0 {
		startTradeFuturesPosition(position, order.Leverage)
	}
	margin, err := pricing.Margin(fill.Value, position.Leverage)
	if err != nil {
		return ErrTradeAmountInvalid
	}
	cost := margin + fill.Fee
	fromFrozen := min(cost, order.Frozen)
	if cost-fromFrozen > account.Cash {
		return ErrTradeCashInsufficient
	}
	order.Frozen -= fromFrozen
	account.Frozen -= fromFrozen
	account.Cash -= cost - fromFrozen

	if position.EntryValue, err = addTradeValue(position.EntryValue, fill.Value); err != nil {
		return err
	}
	if position.OpenValue, err = addTradeValue(position.OpenValue, fill.Value); err != nil {
		return err
	}
	position.Qty += fill.Qty
	position.OpenQty += fill.Qty
	position.Margin += margin
	position.MarginIn += margin
	position.Fees += fill.Fee
	if order.TakeProfit != "" {
		position.TakeProfit = order.TakeProfit
	}
	if order.StopLoss != "" {
		position.StopLoss = order.StopLoss
	}
	if order.FilledValue, err = addTradeValue(order.FilledValue, fill.Value); err != nil {
		return err
	}
	order.FilledQty += fill.Qty
	order.Fee += fill.Fee
	return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerFuturesOpen, Symbol: order.Symbol, OrderId: order.Id,
		Qty: fill.Qty, Price: fill.Price, Amount: -cost, Fee: fill.Fee})
}

// applyTradeFuturesCloseTx 把一次平仓成交记进账户、仓位与委托：按数量比例结转开仓价值、释放保证金，可用资金收回
// 释放的保证金加上盈亏再减去手续费。这部分仓位亏掉的超过分到的保证金时，先用仓位剩下的保证金补，还补不上的部分由站点承担，
// 盈亏按实际承担的记，账户不会变成负数。平仓的数量不能超过可平的数量，挂着的平仓委托占着的数量由调用方先释放。
func applyTradeFuturesCloseTx(tx *gorm.DB, pricing tradesim.Pricing, account *TradeAccount, position *TradeFuturesPosition, order *TradeFuturesOrder, fill TradeFuturesFill) error {
	if fill.Qty <= 0 || fill.Value.IsNegative() || fill.Fee < 0 || fill.Fee >= common.MaxQuota {
		return ErrTradeAmountInvalid
	}
	if fill.Qty > position.Qty-position.FrozenQty {
		return ErrTradePositionInsufficient
	}
	entryValue, err := parseTradeValue(position.EntryValue)
	if err != nil {
		return err
	}
	share := tradesim.EntryShare(entryValue, fill.Qty, position.Qty)
	pnl, err := pricing.Pnl(tradesim.PositionSide(position.Side), share, fill.Value)
	if err != nil {
		return ErrTradeAmountInvalid
	}
	released := tradesim.ShareQuota(position.Margin, fill.Qty, position.Qty)
	position.Qty -= fill.Qty
	position.EntryValue = entryValue.Sub(share).String()
	position.Margin -= released
	returned := released + pnl - fill.Fee
	if returned < 0 {
		cover := min(-returned, position.Margin)
		position.Margin -= cover
		pnl += -returned - cover
		returned = 0
	}
	account.Cash += returned
	if position.CloseValue, err = addTradeValue(position.CloseValue, fill.Value); err != nil {
		return err
	}
	position.CloseQty += fill.Qty
	position.RealizedPnl += pnl
	position.Fees += fill.Fee
	if order.FilledValue, err = addTradeValue(order.FilledValue, fill.Value); err != nil {
		return err
	}
	order.FilledQty += fill.Qty
	order.Fee += fill.Fee
	order.RealizedPnl += pnl
	return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerFuturesClose, Symbol: order.Symbol, OrderId: order.Id,
		Qty: fill.Qty, Price: fill.Price, Amount: returned, Fee: fill.Fee, Pnl: pnl})
}

// finishTradeFuturesPositionTx 在仓位平完或强平之后把这一段写进历史，并把仓位清零留给下次开仓。
func finishTradeFuturesPositionTx(tx *gorm.DB, position *TradeFuturesPosition, reason string) error {
	if position.Qty != 0 || position.OpenQty <= 0 {
		return nil
	}
	history := TradeFuturesHistory{
		UserId:      position.UserId,
		Symbol:      position.Symbol,
		Side:        position.Side,
		Leverage:    position.Leverage,
		Qty:         position.OpenQty,
		RealizedPnl: position.RealizedPnl,
		Fees:        position.Fees,
		Funding:     position.Funding,
		Pnl:         position.RealizedPnl - position.Fees + position.Funding,
		MarginIn:    position.MarginIn,
		CloseReason: reason,
		OpenedAt:    position.OpenedAt,
		ClosedAt:    common.GetTimestamp(),
	}
	for _, average := range []struct {
		value  string
		qty    int64
		target *string
	}{
		{position.OpenValue, position.OpenQty, &history.EntryPrice},
		{position.CloseValue, position.CloseQty, &history.ClosePrice},
	} {
		value, err := parseTradeValue(average.value)
		if err != nil {
			return err
		}
		if average.qty > 0 {
			*average.target = value.Div(tradesim.QtyFromUnits(average.qty)).Round(tradesim.QtyDecimals).String()
		}
	}
	if err := tx.Create(&history).Error; err != nil {
		return err
	}
	leverage := position.Leverage
	startTradeFuturesPosition(position, leverage)
	position.OpenedAt, position.FundingAt = 0, 0
	return nil
}

// finishTradeFuturesOrder 结束一笔委托：限价开仓退回还冻结着的资金。status 是 filled 或 canceled。
func finishTradeFuturesOrder(account *TradeAccount, order *TradeFuturesOrder, status string, reason string) {
	account.Frozen -= order.Frozen
	account.Cash += order.Frozen
	order.Frozen = 0
	order.Reserved = 0
	order.Status = status
	order.CancelReason = reason
	order.FinishedAt = common.GetTimestamp()
}

// tradeFuturesValueUsedTx 是用户在这个合约上已经占用的名义价值(额度单位)：多空仓位的开仓价值加上挂着的开仓委托。
func tradeFuturesValueUsedTx(tx *gorm.DB, pricing tradesim.Pricing, userId int, symbol string) (int, error) {
	var positions []TradeFuturesPosition
	if err := tx.Where("user_id = ? AND symbol = ? AND qty > 0", userId, symbol).Find(&positions).Error; err != nil {
		return 0, err
	}
	used := 0
	for _, position := range positions {
		value, err := parseTradeValue(position.EntryValue)
		if err != nil {
			return 0, err
		}
		quota, err := pricing.DebitQuota(value)
		if err != nil {
			return 0, ErrTradeAmountInvalid
		}
		used += quota
	}
	var reserved int
	if err := tx.Model(&TradeFuturesOrder{}).
		Where("user_id = ? AND symbol = ? AND action = ? AND status = ?", userId, symbol, TradeFuturesOpen, TradeOrderStatusOpen).
		Select("COALESCE(SUM(reserved), 0)").Scan(&reserved).Error; err != nil {
		return 0, err
	}
	return used + reserved, nil
}

// PlaceTradeFuturesOrder 在一个事务里检查资金、仓位、杠杆与上限，记下委托和下单时立刻成交的部分；限价单没成交的部分冻结资金
// (开仓)或数量(平仓)后挂单。
func PlaceTradeFuturesOrder(in TradeFuturesOrderInput) (*TradeFuturesOrder, error) {
	if in.Qty <= 0 || in.Freeze < 0 || in.Reserve < 0 || in.Freeze >= common.MaxQuota || in.Reserve >= common.MaxQuota || in.Fill.Qty > in.Qty {
		return nil, ErrTradeAmountInvalid
	}
	if in.Side != TradeFuturesLong && in.Side != TradeFuturesShort || in.Action != TradeFuturesOpen && in.Action != TradeFuturesClose {
		return nil, ErrTradeAmountInvalid
	}
	if !in.Rest && in.Fill.Qty <= 0 {
		return nil, ErrTradeNoFill
	}
	// 挂单要有限价，否则撮合永远成交不了它。
	if in.Rest && in.Price == "" {
		return nil, ErrTradeAmountInvalid
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	order := &TradeFuturesOrder{
		UserId:      in.UserId,
		Status:      TradeOrderStatusOpen,
		Symbol:      in.Symbol,
		Side:        in.Side,
		Action:      in.Action,
		Type:        in.Type,
		Leverage:    in.Leverage,
		Price:       in.Price,
		Qty:         in.Qty,
		FilledValue: "0",
		TakeProfit:  in.TakeProfit,
		StopLoss:    in.StopLoss,
		Trigger:     in.Trigger,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	remaining := order.Qty - in.Fill.Qty
	resting := in.Rest && remaining > 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, in.UserId)
		if err != nil {
			return err
		}
		if resting {
			var open int64
			if err = tx.Model(&TradeFuturesOrder{}).Where("user_id = ? AND status = ?", in.UserId, TradeOrderStatusOpen).Count(&open).Error; err != nil {
				return err
			}
			if open >= TradeMaxOpenOrders {
				return ErrTradeOpenOrderLimit
			}
		}
		position, err := loadTradeFuturesPositionTx(tx, in.UserId, in.Symbol, in.Side)
		if err != nil {
			return err
		}
		if in.Action == TradeFuturesClose {
			if position.Qty <= 0 {
				return ErrTradeFuturesNoPosition
			}
			if in.Trigger == TradeFuturesTriggerTakeProfit || in.Trigger == TradeFuturesTriggerStopLoss {
				current := position.TakeProfit
				if in.Trigger == TradeFuturesTriggerStopLoss {
					current = position.StopLoss
				}
				if current == "" || current != in.TriggerPrice {
					return ErrTradeFuturesTriggerChanged
				}
				if err = cancelTradeFuturesClosingTx(tx, account, position, TradeCancelByPosition); err != nil {
					return err
				}
			}
			reserve := in.Fill.Qty
			if resting {
				reserve = order.Qty
			}
			if reserve > position.Qty-position.FrozenQty {
				return ErrTradePositionInsufficient
			}
			order.Leverage = position.Leverage
		} else {
			if err = checkTradeFuturesLeverageTx(tx, position, in.Leverage); err != nil {
				return err
			}
			if in.MaxPositionValue > 0 {
				used, err := tradeFuturesValueUsedTx(tx, pricing, in.UserId, in.Symbol)
				if err != nil {
					return err
				}
				fillValue, err := pricing.DebitQuota(in.Fill.Value)
				if err != nil {
					return ErrTradeAmountInvalid
				}
				if resting {
					fillValue += in.Reserve
				}
				if used+fillValue > in.MaxPositionValue {
					return ErrTradePositionLimit
				}
			}
		}
		if err = tx.Create(order).Error; err != nil {
			return err
		}
		if in.Fill.Qty > 0 {
			if in.Action == TradeFuturesOpen {
				err = applyTradeFuturesOpenTx(tx, pricing, account, position, order, in.Fill)
			} else {
				err = applyTradeFuturesCloseTx(tx, pricing, account, position, order, in.Fill)
			}
			if err != nil {
				return err
			}
		}
		switch {
		case resting && in.Action == TradeFuturesOpen:
			if in.Freeze > account.Cash {
				return ErrTradeCashInsufficient
			}
			account.Cash -= in.Freeze
			account.Frozen += in.Freeze
			order.Frozen, order.Reserved = in.Freeze, in.Reserve
		case resting:
			position.FrozenQty += remaining
		case remaining == 0:
			finishTradeFuturesOrder(account, order, TradeOrderStatusFilled, "")
		default:
			finishTradeFuturesOrder(account, order, TradeOrderStatusCanceled, TradeCancelByDepth)
		}
		if err = finishTradeFuturesPositionTx(tx, position, TradeFuturesCloseByUser); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		return saveTradeFuturesOrderTx(tx, order)
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// checkTradeFuturesLeverageTx 检查开仓的杠杆与这个方向上已有的仓位、挂着的开仓委托一致：同一个仓位只有一个杠杆，
// 挂单成交后要并进仓位。
func checkTradeFuturesLeverageTx(tx *gorm.DB, position *TradeFuturesPosition, leverage int) error {
	if leverage < 1 {
		return ErrTradeAmountInvalid
	}
	if position.Qty > 0 && position.Leverage != leverage {
		return ErrTradeFuturesLeverageMismatch
	}
	var mismatched int64
	if err := tx.Model(&TradeFuturesOrder{}).
		Where("user_id = ? AND symbol = ? AND side = ? AND action = ? AND status = ? AND leverage <> ?",
			position.UserId, position.Symbol, position.Side, TradeFuturesOpen, TradeOrderStatusOpen, leverage).
		Count(&mismatched).Error; err != nil {
		return err
	}
	if mismatched > 0 {
		return ErrTradeFuturesLeverageMismatch
	}
	return nil
}

// lockOpenTradeFuturesOrderTx 锁住一笔还挂着的委托，已经结束时返回 ErrTradeOrderNotOpen。
func lockOpenTradeFuturesOrderTx(tx *gorm.DB, orderId int) (*TradeFuturesOrder, error) {
	var orders []TradeFuturesOrder
	if err := lockForUpdate(tx).Where("id = ? AND status = ?", orderId, TradeOrderStatusOpen).Limit(1).Find(&orders).Error; err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, ErrTradeOrderNotOpen
	}
	return &orders[0], nil
}

// FillTradeFuturesOrder 把撮合算出的一次成交记到一笔挂着的限价委托上，成交完就结束委托并退回多冻结的资金。委托已经不在挂单状态时
// 返回 ErrTradeOrderNotOpen；冻结的资金加上可用资金不够付开仓时返回 ErrTradeCashInsufficient，都什么都不改。
func FillTradeFuturesOrder(orderId int, fill TradeFuturesFill) (*TradeFuturesOrder, error) {
	var stored TradeFuturesOrder
	if err := DB.Where("id = ?", orderId).First(&stored).Error; err != nil {
		return nil, err
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return nil, err
	}
	var order *TradeFuturesOrder
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, stored.UserId)
		if err != nil {
			return err
		}
		if order, err = lockOpenTradeFuturesOrderTx(tx, orderId); err != nil {
			return err
		}
		remaining := order.Qty - order.FilledQty
		if fill.Qty > remaining {
			return ErrTradeAmountInvalid
		}
		position, err := loadTradeFuturesPositionTx(tx, order.UserId, order.Symbol, order.Side)
		if err != nil {
			return err
		}
		if order.Action == TradeFuturesOpen {
			if position.Qty > 0 && position.Leverage != order.Leverage {
				return ErrTradeFuturesLeverageMismatch
			}
			order.Reserved -= tradesim.ShareQuota(order.Reserved, fill.Qty, remaining)
			err = applyTradeFuturesOpenTx(tx, pricing, account, position, order, fill)
		} else {
			if fill.Qty > position.FrozenQty {
				return ErrTradePositionInsufficient
			}
			position.FrozenQty -= fill.Qty
			err = applyTradeFuturesCloseTx(tx, pricing, account, position, order, fill)
		}
		if err != nil {
			return err
		}
		if order.FilledQty == order.Qty {
			finishTradeFuturesOrder(account, order, TradeOrderStatusFilled, "")
		}
		if err = finishTradeFuturesPositionTx(tx, position, TradeFuturesCloseByUser); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		return saveTradeFuturesOrderTx(tx, order)
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// cancelTradeFuturesClosingTx 撤掉一个仓位挂着的全部平仓委托，释放它们占着的数量。强平与止盈止损要平掉整个仓位时先调用。
func cancelTradeFuturesClosingTx(tx *gorm.DB, account *TradeAccount, position *TradeFuturesPosition, reason string) error {
	var closing []TradeFuturesOrder
	if err := lockForUpdate(tx).Where("user_id = ? AND symbol = ? AND side = ? AND action = ? AND status = ?",
		position.UserId, position.Symbol, position.Side, TradeFuturesClose, TradeOrderStatusOpen).Find(&closing).Error; err != nil {
		return err
	}
	for i := range closing {
		finishTradeFuturesOrder(account, &closing[i], TradeOrderStatusCanceled, reason)
		if err := saveTradeFuturesOrderTx(tx, &closing[i]); err != nil {
			return err
		}
	}
	position.FrozenQty = 0
	return nil
}

// cancelTradeFuturesOrderTx 撤销一笔锁住的挂单：开仓退回冻结的资金，平仓释放占着的数量。
func cancelTradeFuturesOrderTx(tx *gorm.DB, account *TradeAccount, order *TradeFuturesOrder, reason string) error {
	if order.Action == TradeFuturesClose {
		position, err := loadTradeFuturesPositionTx(tx, order.UserId, order.Symbol, order.Side)
		if err != nil {
			return err
		}
		position.FrozenQty = max(0, position.FrozenQty-(order.Qty-order.FilledQty))
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
	}
	finishTradeFuturesOrder(account, order, TradeOrderStatusCanceled, reason)
	return saveTradeFuturesOrderTx(tx, order)
}

// CancelTradeFuturesOrder 撤销一笔挂着的合约委托。userId 为 0 时是系统撤单，不核对用户。委托已经不在挂单状态时返回
// ErrTradeOrderNotOpen。
func CancelTradeFuturesOrder(userId int, orderId int, reason string) (*TradeFuturesOrder, error) {
	var stored TradeFuturesOrder
	query := DB.Where("id = ?", orderId)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if err := query.First(&stored).Error; err != nil {
		return nil, err
	}
	var order *TradeFuturesOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, stored.UserId)
		if err != nil {
			return err
		}
		if order, err = lockOpenTradeFuturesOrderTx(tx, orderId); err != nil {
			return err
		}
		if err = cancelTradeFuturesOrderTx(tx, account, order, reason); err != nil {
			return err
		}
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// LiquidateTradeFuturesPosition 在标记价格 mark 下强平一个仓位：先撤掉它挂着的平仓委托，仓位剩下的保证金全部亏掉，可用资金不变。
// 事务里按锁住后的仓位重新判断，已经不满足强平条件(用户刚追加了保证金、仓位已经平掉)时什么都不做，返回 false。
func LiquidateTradeFuturesPosition(userId int, symbol string, side string, mark decimal.Decimal, mmrBps int) (bool, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	liquidated := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockTradeAccountTx(tx, userId)
		if err != nil {
			return err
		}
		position, err := loadTradeFuturesPositionTx(tx, userId, symbol, side)
		if err != nil || position.Qty <= 0 {
			return err
		}
		entryValue, err := parseTradeValue(position.EntryValue)
		if err != nil {
			return err
		}
		hit, err := pricing.Liquidatable(tradesim.PositionSide(side), entryValue, position.Qty, position.Margin, mark, mmrBps)
		if err != nil || !hit {
			return err
		}
		if err = cancelTradeFuturesClosingTx(tx, account, position, TradeCancelByPosition); err != nil {
			return err
		}
		qty := position.Qty
		value := mark.Mul(tradesim.QtyFromUnits(qty))
		loss := -position.Margin
		now := common.GetTimestamp()
		order := &TradeFuturesOrder{
			UserId:      userId,
			Status:      TradeOrderStatusFilled,
			Symbol:      symbol,
			Side:        side,
			Action:      TradeFuturesClose,
			Type:        TradeOrderTypeMarket,
			Leverage:    position.Leverage,
			Qty:         qty,
			FilledQty:   qty,
			FilledValue: value.String(),
			RealizedPnl: loss,
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
		position.RealizedPnl += loss
		position.Qty, position.FrozenQty, position.Margin, position.EntryValue = 0, 0, 0, "0"
		if err = tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerLiquidation, Symbol: symbol, OrderId: order.Id,
			Qty: qty, Price: mark.String(), Pnl: loss}); err != nil {
			return err
		}
		if err = finishTradeFuturesPositionTx(tx, position, TradeFuturesCloseByLiquidation); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		liquidated = true
		return saveTradeFuturesPositionTx(tx, position)
	})
	if err != nil {
		return false, err
	}
	return liquidated, nil
}

// ApplyTradeFuturesFunding 给一个仓位结算 fundingTime(毫秒)这一次的资金费：按结算时的标记价格与费率算出收付，记在仓位的保证金上
// (付不起时付完剩下的保证金为止)。仓位已经没有了、结算过这一次或者是在这之后才开的，什么都不做，返回 false。
func ApplyTradeFuturesFunding(userId int, symbol string, side string, fundingTime int64, rate decimal.Decimal, mark decimal.Decimal) (bool, error) {
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return false, err
	}
	applied := false
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
		amount = max(amount, -position.Margin)
		position.Margin += amount
		position.Funding += amount
		position.FundingAt = fundingTime
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		applied = true
		return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerFuturesFunding, Symbol: symbol, Qty: position.Qty,
			Price: rate.String(), Pnl: amount})
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

// AdjustTradeFuturesMargin 给仓位追加(amount > 0)或减少(amount < 0)保证金(额度单位)。减少后的保证金加上浮动亏损(按标记价格 mark)
// 不能低于按标记价格算的起始保证金，否则返回 ErrTradeFuturesMarginTooLow。
func AdjustTradeFuturesMargin(userId int, symbol string, side string, amount int, mark decimal.Decimal) (*TradeFuturesPosition, error) {
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
		if amount > 0 {
			if amount > account.Cash {
				return ErrTradeCashInsufficient
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
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerFuturesMargin, Symbol: symbol, Amount: -amount})
	})
	if err != nil {
		return nil, err
	}
	return position, nil
}

// SetTradeFuturesTpSl 设置仓位的止盈止损触发价，空串表示不设。价格与方向是否合理由调用方按标记价格检查。
func SetTradeFuturesTpSl(userId int, symbol string, side string, takeProfit string, stopLoss string) (*TradeFuturesPosition, error) {
	var position *TradeFuturesPosition
	err := DB.Transaction(func(tx *gorm.DB) error {
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
		position.TakeProfit, position.StopLoss = takeProfit, stopLoss
		return saveTradeFuturesPositionTx(tx, position)
	})
	if err != nil {
		return nil, err
	}
	return position, nil
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

// GetTradeFuturesHistory 按结束时间倒序分页列出已经结束的仓位。
func GetTradeFuturesHistory(userId int, offset int, limit int) ([]TradeFuturesHistory, int64, error) {
	query := DB.Model(&TradeFuturesHistory{}).Where("user_id = ?", userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []TradeFuturesHistory
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
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

// ListTradeFuturesPositionsOf 返回这些用户还持有的合约仓位，拍快照用。
func ListTradeFuturesPositionsOf(userIds []int) ([]TradeFuturesPosition, error) {
	var positions []TradeFuturesPosition
	if len(userIds) == 0 {
		return positions, nil
	}
	err := DB.Where("user_id IN ? AND qty > 0", userIds).Find(&positions).Error
	return positions, err
}
