// Package tradesim 是模拟盘的成交计算：按 Binance 盘口逐档成交，把成交金额与手续费换算成额度单位。这里只有纯函数，
// 不读数据库也不读行情，方便逐条验算。
//
// 模拟盘里的钱能转出成真实额度，所有取整都朝着站点有利的方向：扣款向上取整、入账向下取整、手续费向上取整。
// 成交只吃盘口上真实挂着的数量，不会在最优价上无限量成交：美股代币的盘口常常只挂几十美元，有人在 Binance 挂个小单
// 把价格拉偏，能在偏离的价格上成交的数量也就那么多。
package tradesim

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// Side 是买卖方向。
type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

// QtyDecimals 是数量在数据库里的精度：数量按 10^-8 记成整数，Binance 现货最小的数量步长就是 10^-8。
const QtyDecimals = 8

// Level 是盘口的一档：价格与这一价位上挂着的数量。
type Level struct {
	Price decimal.Decimal
	Qty   decimal.Decimal
}

// Fill 是一次逐档成交的结果。Takes 与盘口的前 len(Takes) 档一一对应，是每一档吃掉的数量。
type Fill struct {
	Qty      decimal.Decimal
	Notional decimal.Decimal
	Takes    []decimal.Decimal
}

// AvgPrice 是成交均价，没有成交时为 0。
func (f Fill) AvgPrice() decimal.Decimal {
	if f.Qty.IsZero() {
		return decimal.Zero
	}
	return f.Notional.Div(f.Qty)
}

// acceptable 表示限价单可以吃这一档：买单吃价格不高于限价的卖档，卖单吃价格不低于限价的买档。limit 为零表示市价。
func acceptable(side Side, price decimal.Decimal, limit decimal.Decimal) bool {
	if limit.IsZero() {
		return true
	}
	if side == Buy {
		return price.LessThanOrEqual(limit)
	}
	return price.GreaterThanOrEqual(limit)
}

// Walk 在盘口 levels(最优价在前：买单传卖档，卖单传买档)上从最优价开始逐档成交，最多成交 qty。limit 不为零时只吃
// 价格不劣于它的档位。step 大于零时成交数量向下取到 step 的整数倍，零头从最后吃的档位里退回。
func Walk(levels []Level, side Side, qty decimal.Decimal, limit decimal.Decimal, step decimal.Decimal) Fill {
	var fill Fill
	remaining := qty
	for _, level := range levels {
		if !remaining.IsPositive() || !acceptable(side, level.Price, limit) {
			break
		}
		take := decimal.Min(level.Qty, remaining)
		if take.IsNegative() {
			take = decimal.Zero
		}
		fill.Takes = append(fill.Takes, take)
		remaining = remaining.Sub(take)
	}
	total := qty.Sub(remaining)
	if step.IsPositive() {
		excess := total.Sub(total.Div(step).Floor().Mul(step))
		for i := len(fill.Takes) - 1; i >= 0 && excess.IsPositive(); i-- {
			back := decimal.Min(fill.Takes[i], excess)
			fill.Takes[i] = fill.Takes[i].Sub(back)
			excess = excess.Sub(back)
			total = total.Sub(back)
		}
	}
	fill.Qty = total
	for i, take := range fill.Takes {
		fill.Notional = fill.Notional.Add(levels[i].Price.Mul(take))
	}
	return fill
}

// Depth 是盘口上价格不劣于 limit 的档位一共挂着多少数量。
func Depth(levels []Level, side Side, limit decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, level := range levels {
		if !acceptable(side, level.Price, limit) {
			break
		}
		total = total.Add(level.Qty)
	}
	return total
}

// Pricing 是把美元金额换算成额度单位所需的参数。
type Pricing struct {
	// QuotaPerUsd 是 1 美元等于多少额度单位，即 common.QuotaPerUnit。
	QuotaPerUsd decimal.Decimal
	// FeeBps 是手续费，单位万分之一。
	FeeBps int
}

// DebitQuota 把要扣的美元金额换成额度单位，向上取整。
func (p Pricing) DebitQuota(usd decimal.Decimal) (int, error) {
	return common.QuotaFromDecimalStrict(usd.Mul(p.QuotaPerUsd).Ceil())
}

// CreditQuota 把要入账的美元金额换成额度单位，向下取整。
func (p Pricing) CreditQuota(usd decimal.Decimal) (int, error) {
	return common.QuotaFromDecimalStrict(usd.Mul(p.QuotaPerUsd).Floor())
}

// FeeQuota 按成交金额(额度单位)收手续费，向上取整。
func (p Pricing) FeeQuota(amount int) int {
	if amount <= 0 || p.FeeBps <= 0 {
		return 0
	}
	return common.QuotaFromDecimal(decimal.NewFromInt(int64(amount)).Mul(decimal.NewFromInt(int64(p.FeeBps))).Div(decimal.NewFromInt(10000)).Ceil())
}

// BuyCost 是买入成交 fill 要扣的成交金额与手续费(额度单位)。
func (p Pricing) BuyCost(fill Fill) (amount int, fee int, err error) {
	amount, err = p.DebitQuota(fill.Notional)
	if err != nil {
		return 0, 0, err
	}
	return amount, p.FeeQuota(amount), nil
}

// SellProceeds 是卖出成交 fill 入账的成交金额与要扣的手续费(额度单位)，到账的是两者之差。
func (p Pricing) SellProceeds(fill Fill) (amount int, fee int, err error) {
	amount, err = p.CreditQuota(fill.Notional)
	if err != nil {
		return 0, 0, err
	}
	return amount, p.FeeQuota(amount), nil
}

// BuyWithBudget 在卖档上用最多 budget 个额度单位(含手续费)买入，返回成交、成交金额与手续费。limit 与 step 的含义同 Walk。
// 先按每一档的价格加上手续费估出能买多少，取到步长之后逐档成交；取整让总价超过预算时每次少买一个步长再算。
func (p Pricing) BuyWithBudget(asks []Level, budget int, limit decimal.Decimal, step decimal.Decimal) (Fill, int, int, error) {
	if budget <= 0 || !p.QuotaPerUsd.IsPositive() {
		return Fill{}, 0, 0, nil
	}
	rate := decimal.NewFromInt(int64(p.FeeBps)).Div(decimal.NewFromInt(10000))
	remaining := decimal.NewFromInt(int64(budget)).Div(p.QuotaPerUsd)
	qty := decimal.Zero
	for _, level := range asks {
		if !remaining.IsPositive() || !acceptable(Buy, level.Price, limit) || !level.Price.IsPositive() {
			break
		}
		unit := level.Price.Mul(decimal.NewFromInt(1).Add(rate))
		cost := unit.Mul(level.Qty)
		if cost.LessThanOrEqual(remaining) {
			qty = qty.Add(level.Qty)
			remaining = remaining.Sub(cost)
			continue
		}
		qty = qty.Add(remaining.Div(unit).Truncate(QtyDecimals))
		break
	}
	if step.IsPositive() {
		qty = qty.Div(step).Floor().Mul(step)
	}
	for qty.IsPositive() {
		fill := Walk(asks, Buy, qty, limit, step)
		amount, fee, err := p.BuyCost(fill)
		if err != nil {
			return Fill{}, 0, 0, err
		}
		if amount+fee <= budget {
			return fill, amount, fee, nil
		}
		if !step.IsPositive() {
			step = decimal.New(1, -QtyDecimals)
		}
		qty = qty.Sub(step)
	}
	return Fill{}, 0, 0, nil
}

// SellForProceeds 在买档 bids(最优价在前)上卖出到账(成交金额减手续费)够 need 个额度单位的最少数量，用于强平：数量向上取到
// step 的整数倍、再多卖一个 step 抵消取整，不超过 maxQty；盘口不够时有多少卖多少。卖光 maxQty 时不按步长取整，不留零头。
// 返回成交、成交金额与手续费。
func (p Pricing) SellForProceeds(bids []Level, maxQty decimal.Decimal, need int, step decimal.Decimal) (Fill, int, int, error) {
	net := decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(p.FeeBps)).Div(decimal.NewFromInt(10000)))
	if need <= 0 || !maxQty.IsPositive() || !p.QuotaPerUsd.IsPositive() || !net.IsPositive() {
		return Fill{}, 0, 0, nil
	}
	remaining := decimal.NewFromInt(int64(need)).Div(p.QuotaPerUsd)
	qty := decimal.Zero
	for _, level := range bids {
		if !remaining.IsPositive() || !level.Price.IsPositive() {
			break
		}
		unit := level.Price.Mul(net)
		if value := unit.Mul(level.Qty); value.LessThan(remaining) {
			qty = qty.Add(level.Qty)
			remaining = remaining.Sub(value)
			continue
		}
		qty = qty.Add(remaining.Div(unit).RoundUp(QtyDecimals))
		remaining = decimal.Zero
	}
	walkStep := step
	if step.IsPositive() && !remaining.IsPositive() {
		qty = qty.Div(step).Ceil().Add(decimal.NewFromInt(1)).Mul(step)
	}
	if qty.GreaterThanOrEqual(maxQty) {
		qty, walkStep = maxQty, decimal.Zero
	}
	fill := Walk(bids, Sell, qty, decimal.Zero, walkStep)
	amount, fee, err := p.SellProceeds(fill)
	return fill, amount, fee, err
}

// QtyUnits 把数量换成数据库里的整数(10^-8)，向下取整。
func QtyUnits(qty decimal.Decimal) int64 {
	return qty.Shift(QtyDecimals).Floor().IntPart()
}

// QtyFromUnits 把数据库里的整数换回数量。
func QtyFromUnits(units int64) decimal.Decimal {
	return decimal.New(units, -QtyDecimals)
}
