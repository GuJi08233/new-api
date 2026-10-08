package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 排行榜只列成交过、账号还在的人：累计盈亏是总资产减去净转入，收益率是累计盈亏除以累计转入，现货持仓按最新价估值；
// 三种排序各自排名，认不出的排序按累计盈亏。
func TestTradeLeaderboardRanksTradersByProfitReturnAndEquity(t *testing.T) {
	setupTradeMarketTest(t, nil)
	users := []model.User{
		{Id: 5101, Username: "alice", DisplayName: "Alice", AffCode: "trade-lb-5101"},
		{Id: 5102, Username: "bob", AffCode: "trade-lb-5102"},
		{Id: 5103, Username: "carol", AffCode: "trade-lb-5103"},
		{Id: 5104, Username: "dave", AffCode: "trade-lb-5104"},
		{Id: 5105, Username: "erin", AffCode: "trade-lb-5105"},
	}
	require.NoError(t, model.DB.Create(&users).Error)
	require.NoError(t, model.DB.Delete(&model.User{}, 5105).Error)
	accounts := []model.TradeAccount{
		// alice 转入 100，花 50 买了 1 BTC，现价 100：总资产 150，赚 50，收益率 50%。
		{UserId: 5101, Cash: tradeTestUsd("50"), TotalIn: tradeTestUsd("100")},
		// bob 转入 1000，现在 1060：赚 60，收益率 6%。
		{UserId: 5102, Cash: tradeTestUsd("1060"), TotalIn: tradeTestUsd("1000")},
		// carol 转入 2000、转出 500，现在 1400：亏 100，收益率 -5%，总资产却最多。
		{UserId: 5103, Cash: tradeTestUsd("1400"), TotalIn: tradeTestUsd("2000"), TotalOut: tradeTestUsd("500")},
		// dave 只转入没成交，erin 成交过但账号删了：都不上榜。
		{UserId: 5104, Cash: tradeTestUsd("5000"), TotalIn: tradeTestUsd("5000")},
		{UserId: 5105, Cash: tradeTestUsd("9000"), TotalIn: tradeTestUsd("100")},
	}
	require.NoError(t, model.DB.Create(&accounts).Error)
	require.NoError(t, model.DB.Create(&model.TradePosition{UserId: 5101, Symbol: "BTCUSDT", Qty: 100_000_000, Cost: tradeTestUsd("50")}).Error)
	require.NoError(t, model.DB.Create(&[]model.TradeLedger{
		{UserId: 5101, Type: model.TradeLedgerBuy, Symbol: "BTCUSDT"},
		{UserId: 5102, Type: model.TradeLedgerFuturesOpen, Symbol: "BTCUSDT"},
		{UserId: 5103, Type: model.TradeLedgerBuy, Symbol: "BTCUSDT"},
		{UserId: 5103, Type: model.TradeLedgerSell, Symbol: "BTCUSDT"},
		{UserId: 5104, Type: model.TradeLedgerQuotaIn},
		{UserId: 5105, Type: model.TradeLedgerBuy, Symbol: "BTCUSDT"},
	}).Error)

	board, err := buildTradeLeaderboard()
	require.NoError(t, err)

	type row struct {
		Name       string
		Equity     int
		Profit     int
		ReturnRate float64
	}
	rows := func(sort string) []row {
		var out []row
		for _, entry := range board.Ranked(sort) {
			out = append(out, row{entry.Name, entry.Equity, entry.Profit, entry.ReturnRate})
		}
		return out
	}
	alice := row{"Alice", tradeTestUsd("150"), tradeTestUsd("50"), 0.5}
	bob := row{"bob", tradeTestUsd("1060"), tradeTestUsd("60"), 0.06}
	carol := row{"carol", tradeTestUsd("1400"), -tradeTestUsd("100"), -0.05}
	assert.Equal(t, []row{bob, alice, carol}, rows(TradeLeaderboardByProfit))
	assert.Equal(t, []row{alice, bob, carol}, rows(TradeLeaderboardByReturn))
	assert.Equal(t, []row{carol, bob, alice}, rows(TradeLeaderboardByEquity))
	assert.Equal(t, rows(TradeLeaderboardByProfit), rows("volume"))
}
