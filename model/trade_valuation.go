package model

// TradeFinancialFlow 区分预测资金流与现货融资资金流，借款和还本不会被当作交易盈利。
type TradeFinancialFlow struct {
	UserId int
	Type   string
	Amount int
}

func ListTradeFinancialFlows(userIds []int, start, end int64) ([]TradeFinancialFlow, error) {
	var flows []TradeFinancialFlow
	if len(userIds) == 0 {
		return flows, nil
	}
	err := DB.Model(&TradeLedger{}).
		Select("user_id, type, COALESCE(SUM(amount), 0) AS amount").
		Where("user_id IN ? AND type IN ? AND created_at >= ? AND created_at < ?", userIds,
			[]string{TradeLedgerPredictionBuy, TradeLedgerPredictionSell, TradeLedgerPredictionSettle, TradeLedgerSpotLoan, TradeLedgerSpotRepay}, start, end).
		Group("user_id, type").Scan(&flows).Error
	return flows, err
}
