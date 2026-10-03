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
	Cash        int `json:"cash"`
	Frozen      int `json:"frozen"`
	CryptoValue int `json:"crypto_value"`
	StockValue  int `json:"stock_value"`
	// FuturesValue 是合约仓位的价值合计：每个仓位的保证金加按标记价格的浮动盈亏，最少为 0。
	FuturesValue int            `json:"futures_value"`
	Equity       int            `json:"equity"`
	Holdings     []TradeHolding `json:"holdings"`
}

// ValueTradeAccount 按当前行情给账户估值。现货市值按最新价乘数量向下取整，没有行情的交易对按成本计，不让估值凭空涨跌；
// 合约仓位按标记价格估值(见 ValueTradeFutures)。估值只用于展示和快照，转出只看可用资金，不看它。
func ValueTradeAccount(account model.TradeAccount, positions []model.TradePosition, futures []model.TradeFuturesPosition) (TradeValuation, error) {
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
	if _, valuation.FuturesValue, err = ValueTradeFutures(futures); err != nil {
		return valuation, err
	}
	valuation.Equity = valuation.Cash + valuation.Frozen + valuation.CryptoValue + valuation.StockValue + valuation.FuturesValue
	return valuation, nil
}

// ValueTradeUser 读出用户的账户、现货持仓与合约仓位，按当前行情估值。
func ValueTradeUser(userId int) (model.TradeAccount, TradeValuation, error) {
	account, err := model.GetTradeAccount(userId)
	if err != nil {
		return account, TradeValuation{}, err
	}
	positions, err := model.GetTradePositions(userId)
	if err != nil {
		return account, TradeValuation{}, err
	}
	futures, err := model.GetTradeFuturesPositions(userId)
	if err != nil {
		return account, TradeValuation{}, err
	}
	valuation, err := ValueTradeAccount(account, positions, futures)
	return account, valuation, err
}

// tradeSnapshotOf 按估值与这一天的资金流拼出一份快照：现货的资金流按交易对的种类分到加密货币与美股代币。
func tradeSnapshotOf(account model.TradeAccount, valuation TradeValuation, day string, flows []model.TradeSymbolFlow, futuresFlow int) model.TradeSnapshot {
	snapshot := model.TradeSnapshot{
		UserId:       account.UserId,
		Day:          day,
		Equity:       valuation.Equity,
		Cash:         valuation.Cash + valuation.Frozen,
		CryptoValue:  valuation.CryptoValue,
		StockValue:   valuation.StockValue,
		FuturesValue: valuation.FuturesValue,
		FuturesFlow:  futuresFlow,
		NetIn:        account.TotalIn - account.TotalOut,
		CreatedAt:    common.GetTimestamp(),
	}
	for _, flow := range flows {
		if info, _ := operation_setting.TradeSymbolOf(flow.Symbol); info.Kind == operation_setting.TradeKindStock {
			snapshot.StockFlow += flow.Amount
		} else {
			snapshot.CryptoFlow += flow.Amount
		}
	}
	return snapshot
}

// LiveTradeSnapshot 按此刻的估值和今天到现在的资金流算出今天的"快照"，不落库。
func LiveTradeSnapshot(userId int, day string, dayStart int64, now int64) (model.TradeSnapshot, error) {
	account, valuation, err := ValueTradeUser(userId)
	if err != nil {
		return model.TradeSnapshot{}, err
	}
	flows, err := model.ListTradeSymbolFlows([]int{userId}, dayStart, now+1)
	if err != nil {
		return model.TradeSnapshot{}, err
	}
	futuresFlows, err := model.ListTradeFuturesFlows([]int{userId}, dayStart, now+1)
	if err != nil {
		return model.TradeSnapshot{}, err
	}
	futuresFlow := 0
	for _, flow := range futuresFlows {
		futuresFlow += flow.Amount
	}
	return tradeSnapshotOf(account, valuation, day, flows, futuresFlow), nil
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
		futures, err := model.ListTradeFuturesPositionsOf(userIds)
		if err != nil {
			return total, err
		}
		flows, err := model.ListTradeSymbolFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		futuresFlows, err := model.ListTradeFuturesFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		positionsByUser := map[int][]model.TradePosition{}
		for _, position := range positions {
			positionsByUser[position.UserId] = append(positionsByUser[position.UserId], position)
		}
		futuresByUser := map[int][]model.TradeFuturesPosition{}
		for _, position := range futures {
			futuresByUser[position.UserId] = append(futuresByUser[position.UserId], position)
		}
		flowsByUser := map[int][]model.TradeSymbolFlow{}
		for _, flow := range flows {
			flowsByUser[flow.UserId] = append(flowsByUser[flow.UserId], flow)
		}
		futuresFlowByUser := map[int]int{}
		for _, flow := range futuresFlows {
			futuresFlowByUser[flow.UserId] += flow.Amount
		}
		snapshots := make([]model.TradeSnapshot, 0, len(accounts))
		for _, account := range accounts {
			valuation, err := ValueTradeAccount(account, positionsByUser[account.UserId], futuresByUser[account.UserId])
			if err != nil {
				return total, err
			}
			snapshots = append(snapshots, tradeSnapshotOf(account, valuation, day, flowsByUser[account.UserId], futuresFlowByUser[account.UserId]))
		}
		if err := model.SaveTradeSnapshots(snapshots); err != nil {
			return total, err
		}
		total += len(accounts)
		after = accounts[len(accounts)-1].UserId
	}
	return total, model.MarkTradeSnapshotDay(day, total)
}
