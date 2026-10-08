package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm/clause"
)

// TradeSnapshot 是每人每天结束时的资产快照，用来画总资产曲线和每日盈亏日历。持仓按拍快照时的价格估值，
// 按现货的种类(加密货币、美股代币)与合约分开记市值和当天的资金流，日历点开某一天时能看出盈亏来自哪一类：
// 某一类当天的盈亏 = 当天市值 - 前一天市值 + 当天的资金流(卖出、平仓到账减去买入、开仓花费)，三类相加正好是总资产的变化减去净转入。
type TradeSnapshot struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Day    string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	// Equity 是总资产：资金合计(可用 + 冻结)加上现货市值与合约仓位的价值(保证金加浮动盈亏)。
	Equity      int `json:"equity" gorm:"type:bigint"`
	Cash        int `json:"cash" gorm:"type:bigint"`
	CryptoValue int `json:"crypto_value" gorm:"type:bigint"`
	StockValue  int `json:"stock_value" gorm:"type:bigint"`
	CryptoFlow  int `json:"crypto_flow" gorm:"type:bigint"`
	StockFlow   int `json:"stock_flow" gorm:"type:bigint"`
	// FuturesValue 是合约仓位的价值合计，FuturesFlow 是当天合约的资金流(平仓、减少保证金到账减去开仓、追加保证金花费)。
	FuturesValue    int `json:"futures_value" gorm:"type:bigint"`
	FuturesFlow     int `json:"futures_flow" gorm:"type:bigint"`
	PredictionValue int `json:"prediction_value" gorm:"type:bigint"`
	PredictionFlow  int `json:"prediction_flow" gorm:"type:bigint"`
	SpotDebt        int `json:"spot_debt" gorm:"type:bigint"`
	SpotFinanceFlow int `json:"spot_finance_flow" gorm:"type:bigint"`
	// NetIn 是截至当天结束的累计净转入(TotalIn - TotalOut)。
	NetIn     int   `json:"net_in" gorm:"type:bigint"`
	CreatedAt int64 `json:"created_at" gorm:"bigint"`
}

// TradeSnapshotDay 记下哪一天的快照已经拍完，定时任务据此判断有没有要做的。
type TradeSnapshotDay struct {
	Day       string `json:"day" gorm:"type:varchar(10);primaryKey"`
	Accounts  int    `json:"accounts"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

// HasTradeSnapshotDay 表示这一天的快照已经拍完。
func HasTradeSnapshotDay(day string) (bool, error) {
	var count int64
	err := DB.Model(&TradeSnapshotDay{}).Where("day = ?", day).Count(&count).Error
	return count > 0, err
}

// MarkTradeSnapshotDay 记下这一天的快照已经拍完。
func MarkTradeSnapshotDay(day string, accounts int) error {
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeSnapshotDay{
		Day:       day,
		Accounts:  accounts,
		CreatedAt: common.GetTimestamp(),
	}).Error
}

// ListTradeAccounts 按 user_id 升序分批列出模拟盘账户，afterUserId 是上一批的最后一个。
func ListTradeAccounts(afterUserId int, limit int) ([]TradeAccount, error) {
	var accounts []TradeAccount
	err := DB.Where("user_id > ?", afterUserId).Order("user_id").Limit(limit).Find(&accounts).Error
	return accounts, err
}

// ListTradeAccountsOf 返回这些用户的模拟盘账户。
func ListTradeAccountsOf(userIds []int) ([]TradeAccount, error) {
	var accounts []TradeAccount
	if len(userIds) == 0 {
		return accounts, nil
	}
	err := DB.Where("user_id IN ?", userIds).Find(&accounts).Error
	return accounts, err
}

// ListTradePositionsOf 返回这些用户还持有的仓位。
func ListTradePositionsOf(userIds []int) ([]TradePosition, error) {
	var positions []TradePosition
	if len(userIds) == 0 {
		return positions, nil
	}
	err := DB.Where("user_id IN ? AND qty > 0", userIds).Find(&positions).Error
	return positions, err
}

// TradeSymbolFlow 是一个用户在一个交易对上一段时间的买卖资金流：卖出到账减去买入花费。
type TradeSymbolFlow struct {
	UserId int    `json:"user_id"`
	Symbol string `json:"symbol"`
	Amount int    `json:"amount"`
}

// ListTradeSymbolFlows 汇总这些用户在 [start, end) 里每个交易对的买卖资金流。
func ListTradeSymbolFlows(userIds []int, start int64, end int64) ([]TradeSymbolFlow, error) {
	var flows []TradeSymbolFlow
	if len(userIds) == 0 {
		return flows, nil
	}
	err := DB.Model(&TradeLedger{}).
		Select("user_id, symbol, COALESCE(SUM(amount), 0) AS amount").
		Where("user_id IN ? AND type IN ? AND created_at >= ? AND created_at < ?", userIds, []string{TradeLedgerBuy, TradeLedgerSell}, start, end).
		Group("user_id, symbol").
		Scan(&flows).Error
	return flows, err
}

// TradeUserFlow 是一个用户一段时间的资金流。
type TradeUserFlow struct {
	UserId int `json:"user_id"`
	Amount int `json:"amount"`
}

// ListTradeFuturesFlows 汇总这些用户在 [start, end) 里合约的资金流：开仓、平仓、强平、调整保证金与资金费动过的资金。
func ListTradeFuturesFlows(userIds []int, start int64, end int64) ([]TradeUserFlow, error) {
	var flows []TradeUserFlow
	if len(userIds) == 0 {
		return flows, nil
	}
	err := DB.Model(&TradeLedger{}).
		Select("user_id, COALESCE(SUM(amount), 0) AS amount").
		Where("user_id IN ? AND type IN ? AND created_at >= ? AND created_at < ?", userIds, tradeFuturesFlowTypes, start, end).
		Group("user_id").
		Scan(&flows).Error
	return flows, err
}

// SaveTradeSnapshots 写入一批快照，同一天重复拍时以后拍的为准。
func SaveTradeSnapshots(snapshots []TradeSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "day"}},
		DoUpdates: clause.AssignmentColumns([]string{"equity", "cash", "crypto_value", "stock_value", "crypto_flow", "stock_flow",
			"futures_value", "futures_flow", "prediction_value", "prediction_flow", "spot_debt", "spot_finance_flow", "net_in", "created_at"}),
	}).Create(&snapshots).Error
}

// GetTradeSnapshots 返回用户在 [fromDay, toDay] 里的快照，按日期升序。
func GetTradeSnapshots(userId int, fromDay string, toDay string) ([]TradeSnapshot, error) {
	var snapshots []TradeSnapshot
	err := DB.Where("user_id = ? AND day >= ? AND day <= ?", userId, fromDay, toDay).Order("day").Find(&snapshots).Error
	return snapshots, err
}

// GetLatestTradeSnapshotBefore 返回用户在 day 之前最近的一份快照，没有时返回 nil。
func GetLatestTradeSnapshotBefore(userId int, day string) (*TradeSnapshot, error) {
	var snapshots []TradeSnapshot
	if err := DB.Where("user_id = ? AND day < ?", userId, day).Order("day DESC").Limit(1).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}
	return &snapshots[0], nil
}
