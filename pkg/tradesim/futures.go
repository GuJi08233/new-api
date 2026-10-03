package tradesim

import (
	"math/big"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

// 合约(U 本位永续)的成交计算。仓位只用逐仓：每个仓位有自己的保证金，亏损最多亏掉这份保证金，账户的可用资金不会变成负数。
// 金额的取整同样朝着站点有利的方向：保证金与手续费向上取整，盈亏与资金费按带符号的值向下取整(赚的少记、亏的多记)。

// PositionSide 是合约仓位的方向。
type PositionSide string

const (
	Long  PositionSide = "long"
	Short PositionSide = "short"
)

// OpenSide 是开这个方向的仓位时在盘口上的买卖方向：开多是买，开空是卖。
func (s PositionSide) OpenSide() Side {
	if s == Long {
		return Buy
	}
	return Sell
}

// CloseSide 是平这个方向的仓位时在盘口上的买卖方向：平多是卖，平空是买。
func (s PositionSide) CloseSide() Side {
	if s == Long {
		return Sell
	}
	return Buy
}

// bpsRate 把万分之几换成小数。
func bpsRate(bps int) decimal.Decimal {
	return decimal.NewFromInt(int64(bps)).Div(decimal.NewFromInt(10000))
}

// floorQuota 把带符号的美元金额换成额度单位并向下取整。
func (p Pricing) floorQuota(usd decimal.Decimal) (int, error) {
	return common.QuotaFromDecimalStrict(usd.Mul(p.QuotaPerUsd).Floor())
}

// Margin 是名义价值 notional(USDT)在 leverage 倍杠杆下要占用的保证金(额度单位)，向上取整。
func (p Pricing) Margin(notional decimal.Decimal, leverage int) (int, error) {
	if leverage < 1 {
		leverage = 1
	}
	return p.DebitQuota(notional.Div(decimal.NewFromInt(int64(leverage))))
}

// TradeFee 是一笔名义价值 notional(USDT)的成交按 bps 收的手续费(额度单位)：先把名义价值向上取整成额度单位，再向上取整收费。
func (p Pricing) TradeFee(notional decimal.Decimal, bps int) (int, error) {
	amount, err := p.DebitQuota(notional)
	if err != nil {
		return 0, err
	}
	return Pricing{QuotaPerUsd: p.QuotaPerUsd, FeeBps: bps}.FeeQuota(amount), nil
}

// Pnl 是平仓的毛盈亏(额度单位，不含手续费)：entryValue 是这部分仓位的开仓价值，exitValue 是平仓成交的价值(都是 USDT)。
// 多仓赚 exit - entry，空仓赚 entry - exit；按带符号的值向下取整。
func (p Pricing) Pnl(side PositionSide, entryValue decimal.Decimal, exitValue decimal.Decimal) (int, error) {
	diff := exitValue.Sub(entryValue)
	if side == Short {
		diff = diff.Neg()
	}
	return p.floorQuota(diff)
}

// EntryShare 是平掉 held 里的 qty 时要结转的开仓价值。全部平掉时结转全部；部分平仓按数量比例算，保留 18 位小数：
// 各次结转加起来正好是全部开仓价值(最后一次结转剩下的)，取整只是在各次平仓之间挪动，不会累计偏差。
func EntryShare(entryValue decimal.Decimal, qty int64, held int64) decimal.Decimal {
	if qty >= held || held <= 0 {
		return entryValue
	}
	return entryValue.Mul(decimal.NewFromInt(qty)).DivRound(decimal.NewFromInt(held), 18)
}

// Maintenance 是按标记价值 markValue(USDT)与维持保证金率(万分之几)算的维持保证金(额度单位)，向上取整。
func (p Pricing) Maintenance(markValue decimal.Decimal, mmrBps int) (int, error) {
	return p.DebitQuota(markValue.Mul(bpsRate(mmrBps)))
}

// Liquidatable 表示逐仓仓位在标记价格 mark 下该强平了：保证金加上按标记价格算的浮动盈亏不超过维持保证金。
func (p Pricing) Liquidatable(side PositionSide, entryValue decimal.Decimal, qty int64, margin int, mark decimal.Decimal, mmrBps int) (bool, error) {
	if qty <= 0 {
		return false, nil
	}
	markValue := mark.Mul(QtyFromUnits(qty))
	pnl, err := p.Pnl(side, entryValue, markValue)
	if err != nil {
		return false, err
	}
	maintenance, err := p.Maintenance(markValue, mmrBps)
	if err != nil {
		return false, err
	}
	return margin+pnl <= maintenance, nil
}

// LiquidationPrice 是逐仓仓位的预估强平价：保证金 + 浮动盈亏 = 维持保证金时的标记价格。
//
//	多仓：(开仓价值 - 保证金) / (数量 × (1 - 维持保证金率))
//	空仓：(开仓价值 + 保证金) / (数量 × (1 + 维持保证金率))
//
// 保留 8 位小数，朝着更早触发的方向取整(多仓向上、空仓向下)。多仓的保证金不少于开仓价值时永远不会强平，返回 0。
func (p Pricing) LiquidationPrice(side PositionSide, entryValue decimal.Decimal, qty int64, margin int, mmrBps int) decimal.Decimal {
	if qty <= 0 || !p.QuotaPerUsd.IsPositive() {
		return decimal.Zero
	}
	marginUsd := decimal.NewFromInt(int64(margin)).Div(p.QuotaPerUsd)
	size := QtyFromUnits(qty)
	rate := bpsRate(mmrBps)
	one := decimal.NewFromInt(1)
	if side == Short {
		return entryValue.Add(marginUsd).Div(size.Mul(one.Add(rate))).Truncate(QtyDecimals)
	}
	numerator := entryValue.Sub(marginUsd)
	if !numerator.IsPositive() {
		return decimal.Zero
	}
	price := numerator.Div(size.Mul(one.Sub(rate)))
	if truncated := price.Truncate(QtyDecimals); truncated.LessThan(price) {
		return truncated.Add(decimal.New(1, -QtyDecimals))
	}
	return price
}

// Funding 是一次资金费结算给仓位带来的资金变动(额度单位，正数是收到，负数是付出)：资金费率为正时多仓付给空仓，为负时
// 空仓付给多仓，金额是标记价值 markValue(USDT)乘以资金费率。按带符号的值向下取整。
func (p Pricing) Funding(side PositionSide, markValue decimal.Decimal, rate decimal.Decimal) (int, error) {
	amount := markValue.Mul(rate)
	if side == Long {
		amount = amount.Neg()
	}
	return p.floorQuota(amount)
}

// ShareQuota 按 qty/held 的比例分出 total 的一份(额度单位)，向上取整；全部时就是 total。部分平仓释放保证金时用它，
// 最后一次平仓释放剩下的全部，取整不会累计。
func ShareQuota(total int, qty int64, held int64) int {
	if qty >= held || held <= 0 {
		return total
	}
	product := new(big.Int).Mul(big.NewInt(int64(total)), big.NewInt(qty))
	share, remainder := new(big.Int).QuoRem(product, big.NewInt(held), new(big.Int))
	if remainder.Sign() > 0 {
		share.Add(share, big.NewInt(1))
	}
	return int(share.Int64())
}
