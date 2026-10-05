package model

import (
	"errors"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/tradesim"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 合约(U 本位永续)有逐仓与全仓两种保证金模式，双向持仓：每人每个合约多空各一个仓位。同一个合约上的多空仓位与开仓挂单用同一种
// 保证金模式、同一个杠杆，要换模式得先把这个合约的仓位平掉、挂单撤掉。
//
// 逐仓：开仓从资金里拿出保证金和手续费，平仓把释放的保证金加上盈亏、减去手续费放回资金。强平保证了一个仓位通常最多亏掉它的
// 保证金；价格跳空越过强平价时超出保证金的亏损从资金里扣。
//
// 全仓：保证金不从资金里拿出来，只算占用。全仓可用 = 资金 + 全仓浮动盈亏 − 全仓占用，开仓、买入现货、追加逐仓保证金都要它够；
// 开仓只从资金里付手续费，平仓的盈亏与手续费直接记进资金。全仓权益(资金、全仓开仓挂单冻结的钱与全仓浮动盈亏之和)跌到全仓维持保证金时，
// 全部全仓仓位一起按标记价格强平。
// 有全仓仓位时资金可以暂时是负的(平掉亏损的仓位、付资金费，由其他全仓仓位的浮动盈利撑着)。没有全仓仓位撑着资金还是负的(价格跳空)，
// 就从用户的站内额度里扣回来，额度可以扣成负数(欠费)：亏损都由用户承担，站点不兜底。
//
// 资金变动与现货一样先锁住账户行，仓位与委托不再单独加锁。

const (
	TradeFuturesLong  = "long"
	TradeFuturesShort = "short"

	TradeFuturesOpen  = "open"
	TradeFuturesClose = "close"

	TradeFuturesIsolated = "isolated"
	TradeFuturesCross    = "cross"

	// 系统替用户下的平仓委托的来源，用户自己下的委托为空。
	TradeFuturesTriggerTakeProfit  = "tp"
	TradeFuturesTriggerStopLoss    = "sl"
	TradeFuturesTriggerLiquidation = "liquidation"

	// 合约委托撤销的原因：平仓委托的仓位已经平掉或强平；开仓委托成交时与这个合约的保证金模式或杠杆对不上、超过档位杠杆、
	// 止盈止损放不下；全仓强平时撤掉的全仓开仓委托。
	TradeCancelByPosition    = "position"
	TradeCancelByMismatch    = "mismatch"
	TradeCancelByLiquidation = "liquidation"

	// 仓位结束的方式，记在历史里；止盈止损平掉的记为 tp/sl。
	TradeFuturesCloseByUser        = "close"
	TradeFuturesCloseByLiquidation = "liquidation"

	// TradeFuturesMaxLevels 是一个仓位最多几档止盈、几档止损。
	TradeFuturesMaxLevels = 4
)

// 合约账单的类型。逐仓的资金费记在仓位的保证金上，账单的资金变动为 0，收付记在 Pnl 上；全仓的资金费直接进出资金。
// futures_cover 是没有全仓仓位撑着而资金还是负的时，从站内额度扣来补到 0 的那笔钱，和转入一样算进净转入。
const (
	TradeLedgerFuturesOpen    = "futures_open"
	TradeLedgerFuturesClose   = "futures_close"
	TradeLedgerFuturesMargin  = "futures_margin"
	TradeLedgerFuturesFunding = "futures_funding"
	TradeLedgerLiquidation    = "liquidation"
	TradeLedgerFuturesCover   = "futures_cover"
)

// tradeFuturesFlowTypes 是合约成交与仓位的账单类型，每日盈亏按它们汇总合约动过的资金。从站内额度补的钱是转入，不算在里面。
var tradeFuturesFlowTypes = []string{TradeLedgerFuturesOpen, TradeLedgerFuturesClose, TradeLedgerFuturesMargin, TradeLedgerFuturesFunding,
	TradeLedgerLiquidation}

var (
	ErrTradeFuturesNoPosition       = errors.New("trade futures position does not exist")
	ErrTradeFuturesLeverageMismatch = errors.New("trade futures leverage differs from the positions and orders of the contract")
	ErrTradeFuturesModeMismatch     = errors.New("trade futures margin mode differs from the positions and orders of the contract")
	ErrTradeFuturesLeverageTooHigh  = errors.New("trade futures leverage is above the bracket limit of the position")
	ErrTradeFuturesLeverageDown     = errors.New("trade futures isolated leverage can only be raised")
	ErrTradeFuturesOpenOrders       = errors.New("trade futures contract has open orders")
	ErrTradeFuturesCrossMargin      = errors.New("trade futures cross positions have no margin of their own")
	ErrTradeFuturesMarginTooLow     = errors.New("trade futures margin would fall below the requirement")
	ErrTradeFuturesTriggerChanged   = errors.New("trade futures take profit or stop loss has changed")
	ErrTradeFuturesLevelsInvalid    = errors.New("trade futures take profit or stop loss levels are invalid")
	ErrTradeMarkUnavailable         = errors.New("trade futures mark price is unavailable")
)

// TradeFuturesMarket 是合约记账要用到的行情：合约最新的标记价格与 Binance 的风险限额档位，由 service 的行情中心实现。
type TradeFuturesMarket interface {
	MarkPrice(symbol string) (decimal.Decimal, bool)
	Brackets(symbol string) tradesim.Brackets
}

// TradeFuturesLevel 是一档止盈或止损：触发价与触发时要平的数量(10^-8)。止损按标记价格触发，止盈按最新成交价触发；同一组里
// 到价的几档一起按盘口市价平掉，平掉的数量从这几档里扣，扣完的档位删掉。
type TradeFuturesLevel struct {
	Id        int64  `json:"id"`
	Price     string `json:"price"`
	Qty       int64  `json:"qty"`
	CreatedAt int64  `json:"created_at"`
}

// ParseTradeFuturesLevels 解析库里记的一组止盈或止损，空串是没有。
func ParseTradeFuturesLevels(raw string) ([]TradeFuturesLevel, error) {
	if raw == "" {
		return nil, nil
	}
	var levels []TradeFuturesLevel
	if err := common.UnmarshalJsonStr(raw, &levels); err != nil {
		return nil, err
	}
	return levels, nil
}

func encodeTradeFuturesLevels(levels []TradeFuturesLevel) (string, error) {
	if len(levels) == 0 {
		return "", nil
	}
	data, err := common.Marshal(levels)
	return string(data), err
}

// newTradeFuturesLevels 给新设的一组档位编号并记下时间，编号在一个仓位的档位里不重复。
func newTradeFuturesLevels(levels []TradeFuturesLevel) []TradeFuturesLevel {
	now := time.Now()
	out := make([]TradeFuturesLevel, len(levels))
	for i, level := range levels {
		out[i] = TradeFuturesLevel{Id: now.UnixNano() + int64(i), Price: level.Price, Qty: level.Qty, CreatedAt: now.Unix()}
	}
	return out
}

// tradeFuturesLevelsQty 是一组档位要平的数量合计。
func tradeFuturesLevelsQty(levels []TradeFuturesLevel) int64 {
	var total int64
	for _, level := range levels {
		total += level.Qty
	}
	return total
}

// TradeFuturesPosition 是一个合约一个方向上的仓位，以用户、合约与方向为主键。数量为 0 表示没有仓位，这一行留着下次开仓复用；
// 仓位平完(或强平)时把这一段的汇总写进 TradeFuturesHistory。
type TradeFuturesPosition struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Symbol string `json:"symbol" gorm:"type:varchar(20);primaryKey"`
	Side   string `json:"side" gorm:"type:varchar(5);primaryKey"`
	// PositionId 是这一段仓位的编号(开出它的第一次成交的账单 id)，委托、账单与历史用它关联。
	PositionId int    `json:"position_id"`
	MarginMode string `json:"margin_mode" gorm:"type:varchar(8)"`
	Leverage   int    `json:"leverage"`
	// Qty 是持仓数量(10^-8)，含 FrozenQty；FrozenQty 是挂着的限价平仓委托占着的数量。
	Qty       int64 `json:"qty" gorm:"bigint;not null;default:0"`
	FrozenQty int64 `json:"frozen_qty" gorm:"bigint;not null;default:0"`
	// EntryValue 是还持有的部分的开仓价值(USDT 小数)：各次开仓的成交价 × 数量之和，减去平仓时按比例结转的部分。开仓均价 = EntryValue / Qty。
	EntryValue string `json:"entry_value" gorm:"type:varchar(64)"`
	// Margin 是保证金(额度单位)。逐仓是从资金里拿出来的保证金：开仓投入、追加或减少、资金费的收付都记在这里，平仓按数量比例释放；
	// 全仓是按开仓价值与杠杆算的占用，不从资金里拿出来。
	Margin int `json:"margin" gorm:"type:bigint;not null;default:0"`
	// TakeProfits 与 StopLosses 是止盈、止损档位(TradeFuturesLevel 的 JSON 数组)，空表示没设。
	TakeProfits string `json:"take_profits" gorm:"type:text"`
	StopLosses  string `json:"stop_losses" gorm:"type:text"`
	// 下面是这一段仓位(从开仓到平完)的累计：开仓与平仓的数量和价值、毛盈亏、手续费、资金费(收到为正)、投入的保证金(逐仓的追加与减少
	// 也算在内)与各次开仓的保证金之和(收益率按它算)。
	OpenQty       int64  `json:"open_qty" gorm:"bigint"`
	OpenValue     string `json:"open_value" gorm:"type:varchar(64)"`
	CloseQty      int64  `json:"close_qty" gorm:"bigint"`
	CloseValue    string `json:"close_value" gorm:"type:varchar(64)"`
	RealizedPnl   int    `json:"realized_pnl" gorm:"type:bigint"`
	Fees          int    `json:"fees" gorm:"type:bigint"`
	Funding       int    `json:"funding" gorm:"type:bigint"`
	MarginIn      int    `json:"margin_in" gorm:"type:bigint"`
	InitialMargin int    `json:"initial_margin" gorm:"type:bigint"`
	OpenedAt      int64  `json:"opened_at" gorm:"bigint"`
	// FundingAt 是这个仓位已经结算到的资金费时间(毫秒)，开仓时记成开仓时间：只结算之后的资金费，同一次不会结算两遍。
	FundingAt int64 `json:"funding_at" gorm:"bigint"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint"`
}

// TradeFuturesHistory 是一段已经结束的仓位：从开仓到平完或强平的汇总。
type TradeFuturesHistory struct {
	Id         int    `json:"id" gorm:"index:idx_trade_futures_history_user,priority:2"`
	UserId     int    `json:"user_id" gorm:"index:idx_trade_futures_history_user,priority:1"`
	PositionId int    `json:"position_id" gorm:"index"`
	Symbol     string `json:"symbol" gorm:"type:varchar(20)"`
	Side       string `json:"side" gorm:"type:varchar(5)"`
	MarginMode string `json:"margin_mode" gorm:"type:varchar(8)"`
	Leverage   int    `json:"leverage"`
	// Qty 是这一段累计开仓的数量，EntryPrice 与 ClosePrice 是开仓、平仓均价(强平按强平时的标记价格)。
	Qty        int64  `json:"qty" gorm:"bigint"`
	EntryPrice string `json:"entry_price" gorm:"type:varchar(40)"`
	ClosePrice string `json:"close_price" gorm:"type:varchar(40)"`
	// Pnl 是净盈亏 = RealizedPnl - Fees + Funding，正好是这一段让资金多出或少掉的钱。
	RealizedPnl   int    `json:"realized_pnl" gorm:"type:bigint"`
	Fees          int    `json:"fees" gorm:"type:bigint"`
	Funding       int    `json:"funding" gorm:"type:bigint"`
	Pnl           int    `json:"pnl" gorm:"type:bigint"`
	MarginIn      int    `json:"margin_in" gorm:"type:bigint"`
	InitialMargin int    `json:"initial_margin" gorm:"type:bigint"`
	CloseReason   string `json:"close_reason" gorm:"type:varchar(16)"`
	OpenedAt      int64  `json:"opened_at" gorm:"bigint"`
	ClosedAt      int64  `json:"closed_at" gorm:"bigint"`
}

// TradeFuturesOrder 是一笔合约委托：开仓或平仓，市价或限价。市价单下单时就按盘口成交完，吃不完的部分撤销；限价单没成交的部分挂着，
// 由主节点的撮合在盘口满足限价时成交。止盈止损与强平由系统下市价平仓委托，Trigger 记下原因。
type TradeFuturesOrder struct {
	Id         int    `json:"id"`
	UserId     int    `json:"user_id" gorm:"index:idx_trade_futures_order_user_status,priority:1"`
	Status     string `json:"status" gorm:"type:varchar(10);index:idx_trade_futures_order_user_status,priority:2;index:idx_trade_futures_order_status_symbol,priority:1"`
	Symbol     string `json:"symbol" gorm:"type:varchar(20);index:idx_trade_futures_order_status_symbol,priority:2"`
	Side       string `json:"side" gorm:"type:varchar(5)"`
	Action     string `json:"action" gorm:"type:varchar(5)"`
	Type       string `json:"type" gorm:"type:varchar(8)"`
	MarginMode string `json:"margin_mode" gorm:"type:varchar(8)"`
	Leverage   int    `json:"leverage"`
	// PositionId 是这笔委托最近一次成交所在的那一段仓位，平仓委托下单时就记上。
	PositionId int `json:"position_id"`
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
	// TakeProfits 与 StopLosses 是开仓委托第一次成交时要并进仓位的止盈止损档位，并进去之后清空。
	TakeProfits  string `json:"take_profits" gorm:"type:text"`
	StopLosses   string `json:"stop_losses" gorm:"type:text"`
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
	UserId int
	Symbol string
	Side   string
	Action string
	Type   string
	// MarginMode 与 Leverage 是开仓的保证金模式与杠杆，平仓沿用仓位的。
	MarginMode string
	Leverage   int
	Price      string
	Qty        int64
	// Fill 是下单时立刻成交的部分，可以为空。
	Fill TradeFuturesFill
	// Rest 为真时没成交的部分挂单等待(限价单)，否则撤销(市价单)。
	Rest bool
	// Freeze 与 Reserve 是限价开仓挂单部分要冻结的保证金加手续费、占用的名义价值(额度单位，按限价算)。
	Freeze  int
	Reserve int
	// MaxPositionValue 是这个合约多空仓位的开仓价值加上挂着的开仓委托最多多少(额度单位)，0 表示不限制。
	MaxPositionValue int
	// RefPrice 是查风险限额档位用的价格(市价单是成交均价，限价单是限价)：开仓后这个方向的仓位按它算名义价值，杠杆不能超过所在档位的上限。
	RefPrice decimal.Decimal
	// TakeProfits 与 StopLosses 是开仓时一起设的止盈止损，成交后并进仓位。
	TakeProfits []TradeFuturesLevel
	StopLosses  []TradeFuturesLevel
	// Trigger 是系统下的平仓委托的来源(止盈、止损)，TriggerLevels 是到价的那几档的编号，TriggerPrice 是让它们到价的价格：
	// 事务里核对这几档还在、价格确实越过了它们，平掉的数量不超过这几档的合计。
	Trigger       string
	TriggerLevels []int64
	TriggerPrice  decimal.Decimal
	// Market 提供全仓可用与档位要用的行情。
	Market TradeFuturesMarket
}

// TradeCrossState 是用户全仓仓位的汇总(额度单位)：仓位数、占用的保证金、按标记价格的浮动盈亏与维持保证金。
type TradeCrossState struct {
	Positions   int `json:"positions"`
	Used        int `json:"used"`
	Upnl        int `json:"upnl"`
	Maintenance int `json:"maintenance"`
}

// Available 是全仓可用：资金加全仓浮动盈亏减去全仓占用，可以是负的。没有全仓仓位时就是资金。
func (s TradeCrossState) Available(cash int) int {
	return cash + s.Upnl - s.Used
}

// Withdrawable 是能转出的上限：全仓可用里不算浮动盈利，也不超过资金。
func (s TradeCrossState) Withdrawable(cash int) int {
	return max(0, min(cash, cash+min(s.Upnl, 0)-s.Used))
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

// TradeCrossStateOf 按标记价格汇总这些仓位里的全仓仓位。拿不到某个合约的标记价格时返回 ErrTradeMarkUnavailable。
func TradeCrossStateOf(positions []TradeFuturesPosition, market TradeFuturesMarket) (TradeCrossState, error) {
	var state TradeCrossState
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return state, err
	}
	for _, position := range positions {
		if position.MarginMode != TradeFuturesCross || position.Qty <= 0 {
			continue
		}
		if market == nil {
			return state, ErrTradeMarkUnavailable
		}
		mark, ok := market.MarkPrice(position.Symbol)
		if !ok {
			return state, ErrTradeMarkUnavailable
		}
		entryValue, err := parseTradeValue(position.EntryValue)
		if err != nil {
			return state, err
		}
		markValue := mark.Mul(tradesim.QtyFromUnits(position.Qty))
		upnl, err := pricing.Pnl(tradesim.PositionSide(position.Side), entryValue, markValue)
		if err != nil {
			return state, ErrTradeAmountInvalid
		}
		maintenance, err := pricing.Maintenance(markValue, market.Brackets(position.Symbol))
		if err != nil {
			return state, ErrTradeAmountInvalid
		}
		state.Positions++
		state.Used += position.Margin
		state.Upnl += upnl
		state.Maintenance += maintenance
	}
	return state, nil
}

func tradeCrossStateTx(tx *gorm.DB, userId int, market TradeFuturesMarket) (TradeCrossState, error) {
	var positions []TradeFuturesPosition
	if err := tx.Where("user_id = ? AND margin_mode = ? AND qty > 0", userId, TradeFuturesCross).Find(&positions).Error; err != nil {
		return TradeCrossState{}, err
	}
	if len(positions) == 0 {
		return TradeCrossState{}, nil
	}
	return TradeCrossStateOf(positions, market)
}

// checkTradeAffordTx 检查账户付得起一笔支出：cash 是要从资金里付出去的，collateral 是要占用的(全仓开仓只付手续费，却要占用
// 保证金加手续费)。资金要够付 cash，全仓可用(没有全仓仓位时就是资金)要够 collateral。
func checkTradeAffordTx(tx *gorm.DB, account *TradeAccount, cash int, collateral int, market TradeFuturesMarket) error {
	if cash > account.Cash {
		return ErrTradeCashInsufficient
	}
	state, err := tradeCrossStateTx(tx, account.UserId, market)
	if err != nil {
		return err
	}
	if collateral > state.Available(account.Cash) {
		return ErrTradeCashInsufficient
	}
	return nil
}

// settleTradeDeficitTx 在资金是负的、又没有全仓仓位撑着时(价格跳空越过强平价，全仓或逐仓亏损超过了保证金)，从用户的站内额度里
// 扣回这笔亏空，资金回到 0，记一笔账单。额度可以扣成负数(欠费)，但不能越过数据库能存的范围；扣来的钱和转入一样算进 QuotaPrincipal
// 与 TotalIn。返回扣掉的额度，调用方在事务提交之后刷新用户的额度缓存。要在仓位写回之后调用。
func settleTradeDeficitTx(tx *gorm.DB, account *TradeAccount) (int, error) {
	if account.Cash >= 0 {
		return 0, nil
	}
	var open int64
	if err := tx.Model(&TradeFuturesPosition{}).Where("user_id = ? AND margin_mode = ? AND qty > 0", account.UserId, TradeFuturesCross).
		Count(&open).Error; err != nil {
		return 0, err
	}
	if open > 0 {
		return 0, nil
	}
	deficit := -account.Cash
	// 注销了的用户也要扣，否则这笔亏空没地方记，强平会一直失败。
	result := tx.Unscoped().Model(&User{}).Where("id = ? AND quota >= ?", account.UserId, common.MinQuota+1+deficit).
		Update("quota", gorm.Expr("quota - ?", deficit))
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, ErrQuotaOutOfRange
	}
	account.Cash = 0
	account.QuotaPrincipal += deficit
	account.TotalIn += deficit
	if err := tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerFuturesCover, Amount: deficit}); err != nil {
		return 0, err
	}
	return deficit, addTradeNoticeTx(tx, TradeNotice{UserId: account.UserId, Kind: TradeNoticeCover, Market: TradeNoticeFutures, Amount: deficit})
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
		DoUpdates: clause.AssignmentColumns([]string{"position_id", "margin_mode", "leverage", "qty", "frozen_qty", "entry_value", "margin",
			"take_profits", "stop_losses", "open_qty", "open_value", "close_qty", "close_value", "realized_pnl", "fees", "funding", "margin_in",
			"initial_margin", "opened_at", "funding_at", "updated_at"}),
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

// startTradeFuturesPosition 开始新的一段仓位：清掉上一段的累计，记下保证金模式、杠杆、开仓时间与资金费的起点。编号在第一次成交记账时给。
func startTradeFuturesPosition(position *TradeFuturesPosition, mode string, leverage int) {
	now := common.GetTimestamp()
	*position = TradeFuturesPosition{
		UserId:     position.UserId,
		Symbol:     position.Symbol,
		Side:       position.Side,
		MarginMode: mode,
		Leverage:   leverage,
		EntryValue: "0",
		OpenValue:  "0",
		CloseValue: "0",
		OpenedAt:   now,
		FundingAt:  now * 1000,
	}
}

// tradeFuturesLedgerTx 记一条合约账单并记上仓位的编号。这一段仓位还没有编号时(第一次开仓成交)，用这条账单的 id 当编号。
func tradeFuturesLedgerTx(tx *gorm.DB, account *TradeAccount, position *TradeFuturesPosition, entry TradeLedger) error {
	entry.Symbol, entry.PositionId = position.Symbol, position.PositionId
	if err := tradeLedgerTx(tx, account, &entry); err != nil {
		return err
	}
	if position.PositionId != 0 {
		return nil
	}
	position.PositionId = entry.Id
	return tx.Model(&TradeLedger{}).Where("id = ?", entry.Id).Update("position_id", entry.Id).Error
}

// applyTradeFuturesOpenTx 把一次开仓成交记进账户、仓位与委托。release 是挂单成交时这次成交对应的那份冻结资金，先放回资金。
// 逐仓从资金里付保证金和手续费，全仓只付手续费、保证金记成占用。委托带着止盈止损时，第一次成交就并进仓位，每组最多 4 档。
func applyTradeFuturesOpenTx(tx *gorm.DB, pricing tradesim.Pricing, account *TradeAccount, position *TradeFuturesPosition, order *TradeFuturesOrder, fill TradeFuturesFill, release int) error {
	if fill.Qty <= 0 || !fill.Value.IsPositive() || fill.Fee < 0 || fill.Fee >= common.MaxQuota || release < 0 || release > order.Frozen {
		return ErrTradeAmountInvalid
	}
	if position.Qty == 0 {
		startTradeFuturesPosition(position, order.MarginMode, order.Leverage)
	}
	margin, err := pricing.Margin(fill.Value, position.Leverage)
	if err != nil {
		return ErrTradeAmountInvalid
	}
	order.Frozen -= release
	account.Frozen -= release
	account.Cash += release
	cost := fill.Fee
	if position.MarginMode != TradeFuturesCross {
		cost += margin
	}
	if cost > account.Cash {
		return ErrTradeCashInsufficient
	}
	account.Cash -= cost

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
	position.InitialMargin += margin
	position.Fees += fill.Fee
	for _, pair := range []struct{ from, to *string }{{&order.TakeProfits, &position.TakeProfits}, {&order.StopLosses, &position.StopLosses}} {
		if *pair.from == "" {
			continue
		}
		added, err := ParseTradeFuturesLevels(*pair.from)
		if err != nil {
			return err
		}
		current, err := ParseTradeFuturesLevels(*pair.to)
		if err != nil {
			return err
		}
		if len(current)+len(added) > TradeFuturesMaxLevels {
			return ErrTradeFuturesLevelsInvalid
		}
		if *pair.to, err = encodeTradeFuturesLevels(append(current, added...)); err != nil {
			return err
		}
		*pair.from = ""
	}
	if order.FilledValue, err = addTradeValue(order.FilledValue, fill.Value); err != nil {
		return err
	}
	order.FilledQty += fill.Qty
	order.Fee += fill.Fee
	if err = tradeFuturesLedgerTx(tx, account, position, TradeLedger{Type: TradeLedgerFuturesOpen, OrderId: order.Id,
		Qty: fill.Qty, Price: fill.Price, Amount: -cost, Fee: fill.Fee}); err != nil {
		return err
	}
	order.PositionId = position.PositionId
	return nil
}

// applyTradeFuturesCloseTx 把一次平仓成交记进账户、仓位与委托：按数量比例结转开仓价值、释放保证金。逐仓收回释放的保证金加上盈亏
// 再减去手续费，亏得比分到的保证金还多(价格跳过了强平价)时先用仓位剩下的保证金补，还不够的从资金里扣；全仓的盈亏和手续费直接
// 记进资金。资金可以因此变成负的(见 settleTradeDeficitTx)。平仓的数量不能超过可平的数量，挂着的平仓委托占着的数量由调用方先释放。
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
	amount := pnl - fill.Fee
	if position.MarginMode != TradeFuturesCross {
		amount += released
		if amount < 0 {
			cover := min(-amount, position.Margin)
			position.Margin -= cover
			amount += cover
		}
	}
	account.Cash += amount
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
	order.PositionId = position.PositionId
	return tradeFuturesLedgerTx(tx, account, position, TradeLedger{Type: TradeLedgerFuturesClose, OrderId: order.Id,
		Qty: fill.Qty, Price: fill.Price, Amount: amount, Fee: fill.Fee, Pnl: pnl})
}

// finishTradeFuturesPositionTx 在仓位平完或强平之后把这一段写进历史，并把仓位清零留给下次开仓。
func finishTradeFuturesPositionTx(tx *gorm.DB, position *TradeFuturesPosition, reason string) error {
	if position.Qty != 0 || position.OpenQty <= 0 {
		return nil
	}
	history := TradeFuturesHistory{
		UserId:        position.UserId,
		PositionId:    position.PositionId,
		Symbol:        position.Symbol,
		Side:          position.Side,
		MarginMode:    position.MarginMode,
		Leverage:      position.Leverage,
		Qty:           position.OpenQty,
		RealizedPnl:   position.RealizedPnl,
		Fees:          position.Fees,
		Funding:       position.Funding,
		Pnl:           position.RealizedPnl - position.Fees + position.Funding,
		MarginIn:      position.MarginIn,
		InitialMargin: position.InitialMargin,
		CloseReason:   reason,
		OpenedAt:      position.OpenedAt,
		ClosedAt:      common.GetTimestamp(),
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
	startTradeFuturesPosition(position, "", 0)
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

// checkTradeFuturesSymbolTx 检查开仓的保证金模式与杠杆和这个合约上已有的仓位(多空两边)、挂着的开仓委托一致。
func checkTradeFuturesSymbolTx(tx *gorm.DB, userId int, symbol string, mode string, leverage int) error {
	if leverage < 1 || mode != TradeFuturesIsolated && mode != TradeFuturesCross {
		return ErrTradeAmountInvalid
	}
	var positions []TradeFuturesPosition
	if err := tx.Select("margin_mode", "leverage").Where("user_id = ? AND symbol = ? AND qty > 0", userId, symbol).Find(&positions).Error; err != nil {
		return err
	}
	var orders []TradeFuturesOrder
	if err := tx.Select("margin_mode", "leverage").Where("user_id = ? AND symbol = ? AND action = ? AND status = ?",
		userId, symbol, TradeFuturesOpen, TradeOrderStatusOpen).Find(&orders).Error; err != nil {
		return err
	}
	for _, position := range positions {
		orders = append(orders, TradeFuturesOrder{MarginMode: position.MarginMode, Leverage: position.Leverage})
	}
	for _, existing := range orders {
		if existing.MarginMode != mode {
			return ErrTradeFuturesModeMismatch
		}
		if existing.Leverage != leverage {
			return ErrTradeFuturesLeverageMismatch
		}
	}
	return nil
}

// PlaceTradeFuturesOrder 在一个事务里检查资金、仓位、保证金模式、杠杆与上限，记下委托和下单时立刻成交的部分；限价单没成交的部分
// 冻结资金(开仓)或数量(平仓)后挂单。止盈止损触发的平仓先核对到价的档位，平掉之后从这几档里扣掉平掉的数量。
func PlaceTradeFuturesOrder(in TradeFuturesOrderInput) (*TradeFuturesOrder, error) {
	if in.Qty <= 0 || in.Freeze < 0 || in.Reserve < 0 || in.Freeze >= common.MaxQuota || in.Reserve >= common.MaxQuota || in.Fill.Qty > in.Qty {
		return nil, ErrTradeAmountInvalid
	}
	isOpen := in.Action == TradeFuturesOpen
	if in.Side != TradeFuturesLong && in.Side != TradeFuturesShort || !isOpen && in.Action != TradeFuturesClose {
		return nil, ErrTradeAmountInvalid
	}
	if len(in.TakeProfits) > TradeFuturesMaxLevels || len(in.StopLosses) > TradeFuturesMaxLevels {
		return nil, ErrTradeFuturesLevelsInvalid
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
		MarginMode:  in.MarginMode,
		Leverage:    in.Leverage,
		Price:       in.Price,
		Qty:         in.Qty,
		FilledValue: "0",
		Trigger:     in.Trigger,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if order.TakeProfits, err = encodeTradeFuturesLevels(newTradeFuturesLevels(in.TakeProfits)); err != nil {
		return nil, err
	}
	if order.StopLosses, err = encodeTradeFuturesLevels(newTradeFuturesLevels(in.StopLosses)); err != nil {
		return nil, err
	}
	remaining := order.Qty - in.Fill.Qty
	resting := in.Rest && remaining > 0
	isTrigger := in.Trigger == TradeFuturesTriggerTakeProfit || in.Trigger == TradeFuturesTriggerStopLoss
	covered := 0
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
		var triggered []TradeFuturesLevel
		if isOpen {
			if err = checkTradeFuturesSymbolTx(tx, in.UserId, in.Symbol, in.MarginMode, in.Leverage); err != nil {
				return err
			}
			if in.Market == nil {
				return ErrTradeMarkUnavailable
			}
			notional := in.RefPrice.Mul(tradesim.QtyFromUnits(position.Qty + in.Qty))
			if limit := in.Market.Brackets(in.Symbol).For(notional).MaxLeverage; limit > 0 && in.Leverage > limit {
				return ErrTradeFuturesLeverageTooHigh
			}
			for _, pair := range []struct {
				current string
				added   []TradeFuturesLevel
			}{{position.TakeProfits, in.TakeProfits}, {position.StopLosses, in.StopLosses}} {
				current, err := ParseTradeFuturesLevels(pair.current)
				if err != nil {
					return err
				}
				if len(current)+len(pair.added) > TradeFuturesMaxLevels ||
					tradeFuturesLevelsQty(current)+tradeFuturesLevelsQty(pair.added) > position.Qty+in.Qty {
					return ErrTradeFuturesLevelsInvalid
				}
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
			fillMargin := 0
			if in.Fill.Qty > 0 {
				if fillMargin, err = pricing.Margin(in.Fill.Value, in.Leverage); err != nil {
					return ErrTradeAmountInvalid
				}
			}
			freeze := 0
			if resting {
				freeze = in.Freeze
			}
			cash := in.Fill.Fee + freeze
			if in.MarginMode == TradeFuturesIsolated {
				cash += fillMargin
			}
			if err = checkTradeAffordTx(tx, account, cash, fillMargin+in.Fill.Fee+freeze, in.Market); err != nil {
				return err
			}
		} else {
			if position.Qty <= 0 {
				return ErrTradeFuturesNoPosition
			}
			if isTrigger {
				raw := position.TakeProfits
				if in.Trigger == TradeFuturesTriggerStopLoss {
					raw = position.StopLosses
				}
				if triggered, err = ParseTradeFuturesLevels(raw); err != nil {
					return err
				}
				// 止盈：多仓涨到、空仓跌到触发价；止损反过来。
				rising := (in.Trigger == TradeFuturesTriggerTakeProfit) == (position.Side == TradeFuturesLong)
				var allowed int64
				for _, id := range in.TriggerLevels {
					i := slices.IndexFunc(triggered, func(level TradeFuturesLevel) bool { return level.Id == id })
					if i < 0 {
						return ErrTradeFuturesTriggerChanged
					}
					price, err := decimal.NewFromString(triggered[i].Price)
					if err != nil {
						return err
					}
					if rising && in.TriggerPrice.LessThan(price) || !rising && in.TriggerPrice.GreaterThan(price) {
						return ErrTradeFuturesTriggerChanged
					}
					allowed += triggered[i].Qty
				}
				if len(in.TriggerLevels) == 0 || in.Fill.Qty > min(allowed, position.Qty) {
					return ErrTradeFuturesTriggerChanged
				}
				if in.Fill.Qty > position.Qty-position.FrozenQty {
					if err = cancelTradeFuturesClosingTx(tx, account, position, TradeCancelByPosition); err != nil {
						return err
					}
				}
			}
			reserve := in.Fill.Qty
			if resting {
				reserve = order.Qty
			}
			if reserve > position.Qty-position.FrozenQty {
				return ErrTradePositionInsufficient
			}
			order.MarginMode, order.Leverage, order.PositionId = position.MarginMode, position.Leverage, position.PositionId
		}
		if err = tx.Create(order).Error; err != nil {
			return err
		}
		if in.Fill.Qty > 0 {
			if isOpen {
				err = applyTradeFuturesOpenTx(tx, pricing, account, position, order, in.Fill, 0)
			} else {
				err = applyTradeFuturesCloseTx(tx, pricing, account, position, order, in.Fill)
			}
			if err != nil {
				return err
			}
		}
		if isTrigger {
			left := in.Fill.Qty
			for _, id := range in.TriggerLevels {
				i := slices.IndexFunc(triggered, func(level TradeFuturesLevel) bool { return level.Id == id })
				take := min(triggered[i].Qty, left)
				triggered[i].Qty -= take
				left -= take
			}
			triggered = slices.DeleteFunc(triggered, func(level TradeFuturesLevel) bool { return level.Qty <= 0 })
			encoded, err := encodeTradeFuturesLevels(triggered)
			if err != nil {
				return err
			}
			if in.Trigger == TradeFuturesTriggerTakeProfit {
				position.TakeProfits = encoded
			} else {
				position.StopLosses = encoded
			}
			if err = tradeFuturesFillNoticeTx(tx, in.Trigger, order, in.Fill, order.RealizedPnl); err != nil {
				return err
			}
		}
		switch {
		case resting && isOpen:
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
		reason := TradeFuturesCloseByUser
		if in.Trigger != "" {
			reason = in.Trigger
		}
		if err = finishTradeFuturesPositionTx(tx, position, reason); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return saveTradeFuturesOrderTx(tx, order)
	})
	if err != nil {
		return nil, err
	}
	if covered > 0 {
		syncCreditUserQuotaCache(in.UserId, -covered, "trade futures deficit")
	}
	return order, nil
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
// 返回 ErrTradeOrderNotOpen；开仓委托与这个合约现在的保证金模式、杠杆对不上，超过档位杠杆或者止盈止损放不下，或者资金不够付时
// 返回相应的错误，都什么都不改，由撮合撤掉委托。
func FillTradeFuturesOrder(orderId int, fill TradeFuturesFill, market TradeFuturesMarket) (*TradeFuturesOrder, error) {
	var stored TradeFuturesOrder
	if err := DB.Where("id = ?", orderId).First(&stored).Error; err != nil {
		return nil, err
	}
	pricing, err := tradeFuturesPricing()
	if err != nil {
		return nil, err
	}
	var order *TradeFuturesOrder
	covered := 0
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
		realizedBefore := order.RealizedPnl
		position, err := loadTradeFuturesPositionTx(tx, order.UserId, order.Symbol, order.Side)
		if err != nil {
			return err
		}
		if order.Action == TradeFuturesOpen {
			if err = checkTradeFuturesSymbolTx(tx, order.UserId, order.Symbol, order.MarginMode, order.Leverage); err != nil {
				return err
			}
			price, err := decimal.NewFromString(order.Price)
			if err != nil {
				return err
			}
			if market != nil {
				notional := price.Mul(tradesim.QtyFromUnits(position.Qty + fill.Qty))
				if limit := market.Brackets(order.Symbol).For(notional).MaxLeverage; limit > 0 && order.Leverage > limit {
					return ErrTradeFuturesLeverageTooHigh
				}
			}
			release := tradesim.ShareQuota(order.Frozen, fill.Qty, remaining)
			order.Reserved -= tradesim.ShareQuota(order.Reserved, fill.Qty, remaining)
			err = applyTradeFuturesOpenTx(tx, pricing, account, position, order, fill, release)
			if err != nil {
				return err
			}
		} else {
			if fill.Qty > position.FrozenQty {
				return ErrTradePositionInsufficient
			}
			position.FrozenQty -= fill.Qty
			if err = applyTradeFuturesCloseTx(tx, pricing, account, position, order, fill); err != nil {
				return err
			}
		}
		if err = tradeFuturesFillNoticeTx(tx, TradeNoticeFill, order, fill, order.RealizedPnl-realizedBefore); err != nil {
			return err
		}
		if order.FilledQty == order.Qty {
			finishTradeFuturesOrder(account, order, TradeOrderStatusFilled, "")
		}
		if err = finishTradeFuturesPositionTx(tx, position, TradeFuturesCloseByUser); err != nil {
			return err
		}
		if err = saveTradeFuturesPositionTx(tx, position); err != nil {
			return err
		}
		if covered, err = settleTradeDeficitTx(tx, account); err != nil {
			return err
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return saveTradeFuturesOrderTx(tx, order)
	})
	if err != nil {
		return nil, err
	}
	if covered > 0 {
		syncCreditUserQuotaCache(stored.UserId, -covered, "trade futures deficit")
	}
	return order, nil
}

// cancelTradeFuturesClosingTx 撤掉一个仓位挂着的全部平仓委托，释放它们占着的数量。强平与止盈止损要平掉的数量超过可平的数量时先调用。
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
