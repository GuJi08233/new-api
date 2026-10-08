package service

import (
	"context"
	"errors"

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
	// Cash 是资金，有全仓合约仓位时可以暂时是负的。
	Cash        int `json:"cash"`
	Frozen      int `json:"frozen"`
	CryptoValue int `json:"crypto_value"`
	StockValue  int `json:"stock_value"`
	// FuturesValue 是合约仓位算进总资产的价值合计：逐仓是保证金加按标记价格的浮动盈亏(最少为 0)，全仓只有浮动盈亏。
	FuturesValue int `json:"futures_value"`
	// PredictionValue 是 BTC 预测持仓按能卖出的价格的估值。
	PredictionValue int `json:"prediction_value"`
	// SpotDebt 是现货借款的本金加利息(算上到期还没收的)，SpotMargin 是借款本身，SpotRisk 与 SpotLevel 是借款的风险与风险率
	// (见 model.TradeSpotRisk)。SpotPricesFresh 为假时现货盘口不新鲜，风险率算不出来。
	SpotDebt        int                   `json:"spot_debt"`
	SpotMargin      model.TradeSpotMargin `json:"spot_margin"`
	SpotRisk        model.TradeSpotRisk   `json:"spot_risk"`
	SpotLevel       string                `json:"spot_level"`
	SpotPricesFresh bool                  `json:"spot_prices_fresh"`
	// Equity 是总资产，最少为 0。
	Equity   int               `json:"equity"`
	Cross    TradeCrossSummary `json:"cross"`
	Holdings []TradeHolding    `json:"holdings"`
}

// ValueTradeAccount 按当前行情给账户估值。现货市值按最新价乘数量向下取整，没有行情的交易对按成本计，不让估值凭空涨跌；
// 合约仓位按标记价格估值(见 ValueTradeFutures，crossPending 是挂着的全仓开仓委托冻结的钱)。估值只用于展示和快照，转出看资金与
// 全仓占用，不看它。
func ValueTradeAccount(account model.TradeAccount, crossPending int, positions []model.TradePosition, futures []model.TradeFuturesPosition) (TradeValuation, error) {
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
	if _, valuation.FuturesValue, valuation.Cross, err = ValueTradeFutures(account.Cash, crossPending, futures); err != nil {
		return valuation, err
	}
	valuation.Equity = max(0, valuation.Cash+valuation.Frozen+valuation.CryptoValue+valuation.StockValue+valuation.FuturesValue)
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
	pending, err := model.ListTradeFuturesCrossPending([]int{userId})
	if err != nil {
		return account, TradeValuation{}, err
	}
	valuation, err := ValueTradeAccount(account, pending[userId], positions, futures)
	if err != nil {
		return account, valuation, err
	}
	loan, err := model.GetTradeSpotMargin(userId)
	if err != nil {
		return account, valuation, err
	}
	predictions, err := model.ListTradePredictionPositionsOf([]int{userId})
	if err != nil {
		return account, valuation, err
	}
	err = valueTradeFinancing(&valuation, loan, positions, predictions)
	return account, valuation, err
}

// valueTradeFinancing 把预测持仓和现货借款算进总资产，算出借款的风险率。有借款时把钱转出去(转回额度、给合约或预测用)之后
// 风险率不能低于 2，要留下的资金从全仓可用与可转出里扣掉；现货盘口不新鲜、风险率算不出来时这两项按 0 算。预测持仓不能作抵押。
func valueTradeFinancing(valuation *TradeValuation, loan model.TradeSpotMargin, spots []model.TradePosition, predictions []model.TradePredictionPosition) error {
	loan, err := model.AccruedTradeSpotMargin(loan, common.GetTimestamp())
	if err != nil {
		return err
	}
	valuation.SpotMargin, valuation.SpotDebt = loan, loan.Principal+loan.Interest
	valuation.PredictionValue, err = ValueTradePredictions(predictions)
	if err != nil {
		return err
	}
	valuation.Equity = max(0, valuation.Cash+valuation.Frozen+valuation.CryptoValue+valuation.StockValue+valuation.FuturesValue+valuation.PredictionValue-valuation.SpotDebt)
	valuation.SpotPricesFresh = true
	if valuation.SpotDebt == 0 {
		return nil
	}
	risk, err := model.TradeSpotRiskOf(loan, valuation.Cross.Withdrawable, spots, tradeFuturesMarks{market: futuresMarket})
	if errors.Is(err, model.ErrTradeMarkUnavailable) {
		valuation.SpotPricesFresh = false
		valuation.Cross.Available = min(valuation.Cross.Available, 0)
		valuation.Cross.Withdrawable = 0
		return nil
	}
	if err != nil {
		return err
	}
	valuation.SpotRisk, valuation.SpotLevel = risk, risk.Level().StringFixed(4)
	reserve := risk.Reserve(model.TradeSpotTransferLevel)
	valuation.Cross.Available -= reserve
	valuation.Cross.Withdrawable = max(0, valuation.Cross.Withdrawable-reserve)
	return nil
}

// valueTradeAccounts 读出一批账户的现货持仓、合约仓位与全仓开仓委托冻结的钱，按当前行情逐个估值，顺序与 accounts 相同。
func valueTradeAccounts(accounts []model.TradeAccount) ([]TradeValuation, error) {
	userIds := make([]int, len(accounts))
	for i, account := range accounts {
		userIds[i] = account.UserId
	}
	positions, err := model.ListTradePositionsOf(userIds)
	if err != nil {
		return nil, err
	}
	futures, err := model.ListTradeFuturesPositionsOf(userIds)
	if err != nil {
		return nil, err
	}
	pending, err := model.ListTradeFuturesCrossPending(userIds)
	if err != nil {
		return nil, err
	}
	loans, err := model.ListTradeSpotMarginsOf(userIds)
	if err != nil {
		return nil, err
	}
	predictions, err := model.ListTradePredictionPositionsOf(userIds)
	if err != nil {
		return nil, err
	}
	loansByUser := make(map[int]model.TradeSpotMargin, len(loans))
	for _, loan := range loans {
		loansByUser[loan.UserId] = loan
	}
	predictionsByUser := make(map[int][]model.TradePredictionPosition)
	for _, position := range predictions {
		predictionsByUser[position.UserId] = append(predictionsByUser[position.UserId], position)
	}
	positionsByUser := map[int][]model.TradePosition{}
	for _, position := range positions {
		positionsByUser[position.UserId] = append(positionsByUser[position.UserId], position)
	}
	futuresByUser := map[int][]model.TradeFuturesPosition{}
	for _, position := range futures {
		futuresByUser[position.UserId] = append(futuresByUser[position.UserId], position)
	}
	valuations := make([]TradeValuation, len(accounts))
	for i, account := range accounts {
		valuations[i], err = ValueTradeAccount(account, pending[account.UserId], positionsByUser[account.UserId], futuresByUser[account.UserId])
		if err != nil {
			return nil, err
		}
		if err = valueTradeFinancing(&valuations[i], loansByUser[account.UserId], positionsByUser[account.UserId], predictionsByUser[account.UserId]); err != nil {
			return nil, err
		}
	}
	return valuations, nil
}

// tradeSnapshotOf 按估值与这一天的资金流拼出一份快照：现货的资金流按交易对的种类分到加密货币与美股代币。
func tradeSnapshotOf(account model.TradeAccount, valuation TradeValuation, day string, flows []model.TradeSymbolFlow, futuresFlow int) model.TradeSnapshot {
	snapshot := model.TradeSnapshot{
		UserId:          account.UserId,
		Day:             day,
		Equity:          valuation.Equity,
		Cash:            valuation.Cash + valuation.Frozen,
		CryptoValue:     valuation.CryptoValue,
		StockValue:      valuation.StockValue,
		FuturesValue:    valuation.FuturesValue,
		FuturesFlow:     futuresFlow,
		PredictionValue: valuation.PredictionValue,
		SpotDebt:        valuation.SpotDebt,
		NetIn:           account.TotalIn - account.TotalOut,
		CreatedAt:       common.GetTimestamp(),
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
	snapshot := tradeSnapshotOf(account, valuation, day, flows, futuresFlow)
	financialFlows, err := model.ListTradeFinancialFlows([]int{userId}, dayStart, now+1)
	if err != nil {
		return model.TradeSnapshot{}, err
	}
	for _, flow := range financialFlows {
		if flow.Type == model.TradeLedgerSpotLoan || flow.Type == model.TradeLedgerSpotRepay {
			snapshot.SpotFinanceFlow += flow.Amount
		} else {
			snapshot.PredictionFlow += flow.Amount
		}
	}
	return snapshot, nil
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
		valuations, err := valueTradeAccounts(accounts)
		if err != nil {
			return total, err
		}
		userIds := make([]int, len(accounts))
		for i, account := range accounts {
			userIds[i] = account.UserId
		}
		flows, err := model.ListTradeSymbolFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		futuresFlows, err := model.ListTradeFuturesFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		flowsByUser := map[int][]model.TradeSymbolFlow{}
		for _, flow := range flows {
			flowsByUser[flow.UserId] = append(flowsByUser[flow.UserId], flow)
		}
		futuresFlowByUser := map[int]int{}
		for _, flow := range futuresFlows {
			futuresFlowByUser[flow.UserId] += flow.Amount
		}
		financialFlows, err := model.ListTradeFinancialFlows(userIds, start, end)
		if err != nil {
			return total, err
		}
		predictionFlowByUser, financingFlowByUser := map[int]int{}, map[int]int{}
		for _, flow := range financialFlows {
			if flow.Type == model.TradeLedgerSpotLoan || flow.Type == model.TradeLedgerSpotRepay {
				financingFlowByUser[flow.UserId] += flow.Amount
			} else {
				predictionFlowByUser[flow.UserId] += flow.Amount
			}
		}
		snapshots := make([]model.TradeSnapshot, 0, len(accounts))
		for i, account := range accounts {
			snapshot := tradeSnapshotOf(account, valuations[i], day, flowsByUser[account.UserId], futuresFlowByUser[account.UserId])
			snapshot.PredictionFlow, snapshot.SpotFinanceFlow = predictionFlowByUser[account.UserId], financingFlowByUser[account.UserId]
			snapshots = append(snapshots, snapshot)
		}
		if err := model.SaveTradeSnapshots(snapshots); err != nil {
			return total, err
		}
		total += len(accounts)
		after = accounts[len(accounts)-1].UserId
	}
	return total, model.MarkTradeSnapshotDay(day, total)
}
