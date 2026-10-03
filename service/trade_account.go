package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
)

// TradeHolding 是一个持仓按当前行情的估值。
type TradeHolding struct {
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	Qty       string `json:"qty"`
	FrozenQty string `json:"frozen_qty"`
	// Cost 是持仓成本(额度单位，含买入手续费)，AvgPrice 是按它算出的持仓均价。
	Cost     int    `json:"cost"`
	AvgPrice string `json:"avg_price"`
	// Price 是估值用的最新价，没有行情时为空，Value 这时按成本计。
	Price string `json:"price"`
	Value int    `json:"value"`
}

// TradeValuation 是模拟盘账户按当前行情的估值，金额都是额度单位。
type TradeValuation struct {
	Cash        int            `json:"cash"`
	Frozen      int            `json:"frozen"`
	CryptoValue int            `json:"crypto_value"`
	StockValue  int            `json:"stock_value"`
	Equity      int            `json:"equity"`
	Holdings    []TradeHolding `json:"holdings"`
}

// ValueTradeAccount 按当前行情给账户估值。市值按最新价乘数量向下取整；没有行情的交易对按成本计，
// 不让估值凭空涨跌。估值只用于展示和快照，转出规则按已实现权益结算，不看它。
func ValueTradeAccount(account model.TradeAccount, positions []model.TradePosition) (TradeValuation, error) {
	valuation := TradeValuation{Cash: account.Cash, Frozen: account.Frozen, Holdings: []TradeHolding{}}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return valuation, err
	}
	pricing := tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}
	for _, position := range positions {
		qty := tradesim.QtyFromUnits(position.Qty)
		info, _ := operation_setting.TradeSymbolOf(position.Symbol)
		holding := TradeHolding{
			Symbol:    position.Symbol,
			Kind:      info.Kind,
			Qty:       qty.String(),
			FrozenQty: tradesim.QtyFromUnits(position.FrozenQty).String(),
			Cost:      position.Cost,
			Value:     position.Cost,
		}
		if qty.IsPositive() {
			holding.AvgPrice = decimal.NewFromInt(int64(position.Cost)).Div(pricing.QuotaPerUsd).Div(qty).Round(tradesim.QtyDecimals).String()
		}
		if price, ok := tradeMarket.LastPrice(position.Symbol); ok {
			value, err := pricing.CreditQuota(price.Mul(qty))
			if err != nil {
				return valuation, err
			}
			holding.Price, holding.Value = price.String(), value
		}
		if info.Kind == operation_setting.TradeKindStock {
			valuation.StockValue += holding.Value
		} else {
			valuation.CryptoValue += holding.Value
		}
		valuation.Holdings = append(valuation.Holdings, holding)
	}
	valuation.Equity = valuation.Cash + valuation.Frozen + valuation.CryptoValue + valuation.StockValue
	return valuation, nil
}

// tradeSnapshotBatch 是拍快照时每批处理的账户数。
const tradeSnapshotBatch = 200

// TakeTradeSnapshots 给所有模拟盘账户拍一份 day 结束时的快照，[start, end) 是这一天的时间范围，持仓按当前行情估值。
// 拍完记下这一天，返回拍了多少个账户。中途失败时已经写入的快照保留，下次重拍会覆盖。
func TakeTradeSnapshots(ctx context.Context, day string, start int64, end int64) (int, error) {
	total, after := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		accounts, err := model.ListTradeAccounts(after, tradeSnapshotBatch)
		if err != nil {
			return total, err
		}
		if len(accounts) == 0 {
			break
		}
		userIds := make([]int, len(accounts))
		for i, account := range accounts {
			userIds[i] = account.UserId
		}
		positions, err := model.ListTradePositionsOf(userIds)
		if err != nil {
			return total, err
		}
		flows, err := model.ListTradeSymbolFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		positionsByUser := map[int][]model.TradePosition{}
		for _, position := range positions {
			positionsByUser[position.UserId] = append(positionsByUser[position.UserId], position)
		}
		stockFlows, cryptoFlows := map[int]int{}, map[int]int{}
		for _, flow := range flows {
			if info, _ := operation_setting.TradeSymbolOf(flow.Symbol); info.Kind == operation_setting.TradeKindStock {
				stockFlows[flow.UserId] += flow.Amount
			} else {
				cryptoFlows[flow.UserId] += flow.Amount
			}
		}
		now := common.GetTimestamp()
		snapshots := make([]model.TradeSnapshot, 0, len(accounts))
		for _, account := range accounts {
			valuation, err := ValueTradeAccount(account, positionsByUser[account.UserId])
			if err != nil {
				return total, err
			}
			snapshots = append(snapshots, model.TradeSnapshot{
				UserId:      account.UserId,
				Day:         day,
				Equity:      valuation.Equity,
				Cash:        valuation.Cash + valuation.Frozen,
				CryptoValue: valuation.CryptoValue,
				StockValue:  valuation.StockValue,
				CryptoFlow:  cryptoFlows[account.UserId],
				StockFlow:   stockFlows[account.UserId],
				NetIn:       account.TotalIn - account.TotalOut,
				CreatedAt:   now,
			})
		}
		if err := model.SaveTradeSnapshots(snapshots); err != nil {
			return total, err
		}
		total += len(accounts)
		after = accounts[len(accounts)-1].UserId
	}
	return total, model.MarkTradeSnapshotDay(day, total)
}
