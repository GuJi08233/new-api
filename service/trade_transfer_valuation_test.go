package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tradeTransferTestSyncer struct{ calls int }

func (s *tradeTransferTestSyncer) Sync(context.Context) error {
	s.calls++
	return nil
}

func TestTradeTransferValuationSyncsSecondarySpotButNotCashOnlyAccount(t *testing.T) {
	for _, hasSpot := range []bool{false, true} {
		t.Run(strconv.FormatBool(hasSpot), func(t *testing.T) {
			setupTradeMarketTest(t, nil)
			common.IsMasterNode = false
			const userId = 9839
			require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("10")}).Error)
			if hasSpot {
				require.NoError(t, model.DB.Create(&model.TradePosition{UserId: userId, Symbol: "BTCUSDT", Qty: 100_000_000, Cost: tradeTestUsd("100")}).Error)
				setTradeTestBook(1, tradeTestLevels("99", "10"), tradeTestLevels("101", "10"))
			}
			syncer := &tradeTransferTestSyncer{}
			tradeMarket.mu.Lock()
			previousSynced := tradeMarket.bookSyncedAt
			tradeMarket.bookSyncedAt = time.Time{}
			tradeMarket.stream = syncer
			tradeMarket.connected = hasSpot
			tradeMarket.mu.Unlock()
			t.Cleanup(func() {
				tradeMarket.mu.Lock()
				tradeMarket.bookSyncedAt = previousSynced
				tradeMarket.mu.Unlock()
			})
			_, valuation, err := ValueTradeUserForTransfer(context.Background(), userId)
			require.NoError(t, err)
			if hasSpot {
				assert.Equal(t, 1, syncer.calls, "从节点有现货资产时要在本次请求内确认行情")
				assert.Equal(t, tradeTestUsd("109"), valuation.Equity)
			} else {
				assert.Zero(t, syncer.calls, "现金账户不能因为行情连接不可用而无法划转")
				assert.Equal(t, tradeTestUsd("10"), valuation.Equity)
			}
		})
	}
}

// 展示仍可用历史成本或缓存价；划转必须用连接中确认过的新鲜盘口。
func TestTradeTransferValuationRequiresFreshSpotPrices(t *testing.T) {
	for i, state := range []string{"missing", "stale", "disconnected", "fresh"} {
		t.Run(state, func(t *testing.T) {
			var syncErr error
			if state == "stale" {
				syncErr = ErrTradeMarketStale
			}
			setupTradeMarketTest(t, syncErr)
			userId := 9840 + i
			require.NoError(t, model.DB.Create(&model.User{Id: userId, Username: "transfer-spot-" + strconv.Itoa(userId), AffCode: "transfer-spot-" + strconv.Itoa(userId)}).Error)
			require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("100"), QuotaPrincipal: tradeTestUsd("200"), CoinPrincipal: model.GameCoins(tradeTestUsd("100")), TotalIn: tradeTestUsd("200")}).Error)
			require.NoError(t, model.DB.Create(&model.TradePosition{UserId: userId, Symbol: "BTCUSDT", Qty: 100_000_000, Cost: tradeTestUsd("100")}).Error)
			t.Cleanup(func() {
				model.DB.Where("user_id = ?", userId).Delete(&model.GameCoinAccount{})
				model.DB.Where("user_id = ?", userId).Delete(&model.GameCoinLog{})
			})
			setTradeTestBook(1, tradeTestLevels("99", "10"), tradeTestLevels("101", "10"))
			tradeMarket.mu.Lock()
			previousSynced := tradeMarket.bookSyncedAt
			tradeMarket.bookSyncedAt = time.Now()
			switch state {
			case "missing":
				delete(tradeMarket.books, "BTCUSDT")
				delete(tradeMarket.tickers, "BTCUSDT")
			case "stale":
				tradeMarket.bookSyncedAt = time.Now().Add(-time.Minute)
			case "disconnected":
				tradeMarket.connected = false
			}
			tradeMarket.mu.Unlock()
			t.Cleanup(func() {
				tradeMarket.mu.Lock()
				tradeMarket.bookSyncedAt = previousSynced
				tradeMarket.mu.Unlock()
			})
			_, display, err := ValueTradeUser(userId)
			require.NoError(t, err)
			assert.Equal(t, tradeTestUsd("200"), display.Equity)
			seen, valuation, err := ValueTradeUserForTransfer(context.Background(), userId)
			if state != "fresh" {
				require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
				_, _, err = model.TransferGameCoinsFromTrade(userId, model.GameCoins(tradeTestUsd("1")), "2026-10-13", 0, TradeAccountingMarket(), model.TradeEquity{Account: seen})
				require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
				assert.Equal(t, tradeTestUsd("100"), tradeTestCash(t, userId))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tradeTestUsd("199"), valuation.Equity, "本金结算按实际买一估值，不按缓存的成交价")
			equity := model.TradeEquity{Value: valuation.Equity, Known: true, Account: seen}
			_, _, err = model.TransferGameCoinsFromTrade(userId, model.GameCoins(tradeTestUsd("100")), "2026-10-13", 0, TradeAccountingMarket(), equity)
			require.ErrorIs(t, err, model.ErrTradeCoinPrincipal)
			_, _, err = model.TransferGameCoinsFromTrade(userId, model.GameCoins(tradeTestUsd("99")), "2026-10-13", 0, TradeAccountingMarket(), equity)
			require.NoError(t, err)
			wallet, err := model.GetGameCoinAccount(userId)
			require.NoError(t, err)
			assert.Equal(t, model.GameCoins(tradeTestUsd("99")), wallet.Balance)
		})
	}
}

func TestTradeTransferValuationPricesBorrowedCoinsAtFreshAsk(t *testing.T) {
	setupTradeMarketTest(t, nil)
	const userId = 9845
	require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("300")}).Error)
	require.NoError(t, model.DB.Create(&model.TradeSpotAssetLoan{UserId: userId, Symbol: "BTCUSDT", Principal: 100_000_000, Proceeds: tradeTestUsd("100"), ChargedUntil: common.GetTimestamp() + 3600, DailyRate: "0"}).Error)
	t.Cleanup(func() { model.DB.Where("user_id = ?", userId).Delete(&model.TradeSpotAssetLoan{}) })
	setTradeTestBook(1, tradeTestLevels("99", "10"), tradeTestLevels("101", "10"))
	tradeMarket.mu.Lock()
	previousSynced := tradeMarket.bookSyncedAt
	tradeMarket.bookSyncedAt = time.Now()
	tradeMarket.mu.Unlock()
	t.Cleanup(func() {
		tradeMarket.mu.Lock()
		tradeMarket.bookSyncedAt = previousSynced
		tradeMarket.mu.Unlock()
	})
	_, valuation, err := ValueTradeUserForTransfer(context.Background(), userId)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("101"), valuation.CryptoShort)
	assert.Equal(t, tradeTestUsd("199"), valuation.Equity)
	tradeMarket.mu.Lock()
	tradeMarket.connected = false
	tradeMarket.mu.Unlock()
	_, _, err = ValueTradeUserForTransfer(context.Background(), userId)
	require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
	_, display, err := ValueTradeUser(userId)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("200"), display.Equity)
}

func TestTradeTransferValuationRequiresFreshFuturesMark(t *testing.T) {
	for i, state := range []string{"missing", "stale", "disconnected", "fresh"} {
		t.Run(state, func(t *testing.T) {
			setupFuturesMarketTest(t, nil)
			userId := 9850 + i
			require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("10")}).Error)
			require.NoError(t, model.DB.Create(&model.TradeFuturesPosition{UserId: userId, Symbol: "BTCUSDT", Side: model.TradeFuturesLong, MarginMode: model.TradeFuturesIsolated,
				Leverage: 5, Qty: tradesim.QtyUnits(tradeTestDecimal("0.1")), EntryValue: "10", Margin: tradeTestUsd("10")}).Error)
			setFuturesTestMark("50", time.Now().Add(time.Hour).UnixMilli())
			futuresMarket.mu.Lock()
			switch state {
			case "missing":
				delete(futuresMarket.marks, "BTCUSDT")
			case "stale":
				mark := futuresMarket.marks["BTCUSDT"]
				mark.received = time.Now().Add(-time.Minute)
				futuresMarket.marks["BTCUSDT"] = mark
			case "disconnected":
				futuresMarket.dataConnected = false
			}
			futuresMarket.mu.Unlock()
			_, display, err := ValueTradeUser(userId)
			require.NoError(t, err)
			if state == "missing" {
				assert.Equal(t, tradeTestUsd("20"), display.Equity)
			} else {
				assert.Equal(t, tradeTestUsd("15"), display.Equity)
			}
			_, valuation, err := ValueTradeUserForTransfer(context.Background(), userId)
			if state != "fresh" {
				require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tradeTestUsd("15"), valuation.Equity)
		})
	}
}

func TestTradeTransferValuationUsesConfirmedPredictionPayout(t *testing.T) {
	for i, test := range []struct {
		name, outcome, status, bid string
		quoteAge                   int64
		ended                      bool
		want                       string
		unavailable                bool
	}{
		{name: "missing quote", unavailable: true},
		{name: "stale quote", status: "ready", bid: "0.25", quoteAge: 10, unavailable: true},
		{name: "invalid quote", status: "ready", bid: "1.1", unavailable: true},
		{name: "ended unresolved", status: "ready", bid: "0.25", ended: true, unavailable: true},
		{name: "fresh live quote", status: "ready", bid: "0.25", want: "5"},
		{name: "confirmed loss", outcome: model.TradePredictionDown, ended: true, want: "0"},
		{name: "confirmed win", outcome: model.TradePredictionUp, ended: true, want: "20"},
		{name: "confirmed split", outcome: model.TradePredictionSplit, ended: true, want: "10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTradeMarketTest(t, nil)
			userId := 9860 + i
			start := common.GetTimestamp() / 300 * 300
			if test.ended {
				start -= 300
			}
			round := model.TradePredictionRound{WindowStart: start, EndTime: start + 300, Outcome: test.outcome}
			require.NoError(t, model.DB.Create(&round).Error)
			require.NoError(t, model.DB.Create(&model.User{Id: userId, Username: "transfer-prediction-" + strconv.Itoa(userId), AffCode: "transfer-prediction-" + strconv.Itoa(userId)}).Error)
			require.NoError(t, model.DB.Create(&model.TradeAccount{UserId: userId, Cash: tradeTestUsd("10"), QuotaPrincipal: tradeTestUsd("20"), CoinPrincipal: model.GameCoins(tradeTestUsd("10")), TotalIn: tradeTestUsd("20")}).Error)
			require.NoError(t, model.DB.Create(&model.TradePredictionPosition{UserId: userId, WindowStart: start, Side: model.TradePredictionUp, Qty: 20 * 100_000_000,
				InitialQty: 20 * 100_000_000, Cost: tradeTestUsd("10"), TotalCost: tradeTestUsd("10"), Status: model.TradePredictionActive}).Error)
			tradePredictions.mu.Lock()
			previousView := tradePredictions.view
			tradePredictions.view = TradePredictionMarketView{Status: test.status, Round: &TradePredictionRoundView{TradePredictionRound: round},
				Up: TradePredictionPrices{Bid: test.bid}, UpdatedAt: common.GetTimestamp() - test.quoteAge}
			tradePredictions.mu.Unlock()
			t.Cleanup(func() {
				tradePredictions.mu.Lock()
				tradePredictions.view = previousView
				tradePredictions.mu.Unlock()
				model.DB.Where("user_id = ?", userId).Delete(&model.TradePredictionPosition{})
				model.DB.Where("window_start = ?", start).Delete(&model.TradePredictionRound{})
			})
			_, display, err := ValueTradeUser(userId)
			require.NoError(t, err, "展示回退不能因为缺少行情而整页失败")
			if test.status == "" || test.quoteAge > 5 {
				assert.Equal(t, tradeTestUsd("10"), display.PredictionValue)
			}
			seen, valuation, err := ValueTradeUserForTransfer(context.Background(), userId)
			if test.unavailable {
				require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
				_, _, err = model.TransferGameCoinsFromTrade(userId, model.GameCoins(tradeTestUsd("1")), "2026-10-13", 0, TradeAccountingMarket(), model.TradeEquity{Account: seen})
				require.ErrorIs(t, err, model.ErrTradeMarkUnavailable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tradeTestUsd(test.want), valuation.PredictionValue)
			if test.outcome == model.TradePredictionDown {
				_, _, err = model.TransferGameCoinsFromTrade(userId, model.GameCoins(tradeTestUsd("1")), "2026-10-13", 0, TradeAccountingMarket(),
					model.TradeEquity{Value: valuation.Equity, Known: true, Account: seen})
				require.ErrorIs(t, err, model.ErrTradeCoinPrincipal, "已确认输掉的持仓即使尚未结算，也不能按历史成本取回游戏币")
				assert.Equal(t, tradeTestUsd("10"), tradeTestCash(t, userId))
			}
		})
	}
}
