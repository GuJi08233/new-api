package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type predictionOrderFixture struct {
	window int64
	mu     sync.Mutex
	book   tradePredictionBook
}

func setupPredictionOrderTest(t *testing.T) *predictionOrderFixture {
	t.Helper()
	setupTradeMarketTest(t, nil)
	setting := operation_setting.GetTradeSetting().Clone()
	setting.PredictionEnabled = true
	operation_setting.SetTradeSettingForTest(setting)
	fixture := &predictionOrderFixture{
		window: common.GetTimestamp() / model.TradePredictionWindow * model.TradePredictionWindow,
		book: tradePredictionBook{
			Market: "prediction-order-test", Timestamp: strconv.FormatInt(time.Now().UnixMilli(), 10),
			Asks: []tradePredictionLevel{{Price: "0.5", Size: "10"}, {Price: "0.6", Size: "5"}},
			Bids: []tradePredictionLevel{{Price: "0.4", Size: "6"}},
		},
	}
	require.NoError(t, model.SaveTradePredictionRound(model.TradePredictionRound{
		WindowStart: fixture.window, EndTime: fixture.window + model.TradePredictionWindow,
		Slug: "prediction-order-test", ConditionId: fixture.book.Market, UpToken: "111", DownToken: "222",
	}))
	previous := tradePredictions
	tradePredictions = predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		book := fixture.book
		fixture.mu.Unlock()
		book.AssetId = r.URL.Query().Get("token_id")
		data, err := common.Marshal(book)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write(data)
		assert.NoError(t, err)
	})
	t.Cleanup(func() {
		tradePredictions = previous
		require.NoError(t, model.DB.Where("window_start = ?", fixture.window).Delete(&model.TradePredictionPosition{}).Error)
		require.NoError(t, model.DB.Where("window_start = ?", fixture.window).Delete(&model.TradePredictionRound{}).Error)
		require.NoError(t, model.DB.Where("user_id IN ?", []int{6281, 6282}).Delete(&model.TradeSpotMargin{}).Error)
	})
	fundTradeTestUser(t, 6281, "1000")
	fundTradeTestUser(t, 6282, "1000")
	return fixture
}

func (fixture *predictionOrderFixture) buy(userId int, amount string, side string) (*model.TradePredictionPosition, error) {
	return BuyTradePrediction(context.Background(), userId, TradePredictionOrderRequest{
		WindowStart: fixture.window, Side: side, Amount: tradeTestDecimal(amount), LimitPrice: tradeTestDecimal("0.7"),
	})
}

func TestTradePredictionOrdersConsumeOnlyRemainingDepth(t *testing.T) {
	fixture := setupPredictionOrderTest(t)
	first, err := fixture.buy(6281, "2.07", model.TradePredictionUp)
	require.NoError(t, err)
	assert.EqualValues(t, 4*100_000_000, first.Qty, "含费预算只消费买一的四份")
	second, err := fixture.buy(6282, "100", model.TradePredictionUp)
	require.NoError(t, err)
	assert.EqualValues(t, 11*100_000_000, second.Qty, "另一用户只能买剩余六份加下一档五份")
	assert.Equal(t, tradeTestUsd("6.189"), second.Cost)
	_, err = fixture.buy(6281, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, ErrTradeNoLiquidity)

	first, err = SellTradePrediction(context.Background(), 6281, first.Id, decimal.NewFromInt(2), tradeTestDecimal("0.3"))
	require.NoError(t, err)
	assert.EqualValues(t, 2*100_000_000, first.Qty, "买入已吃完卖盘，仍能卖到独立的买盘")
	second, err = SellTradePrediction(context.Background(), 6282, second.Id, decimal.Zero, tradeTestDecimal("0.3"))
	require.NoError(t, err)
	assert.EqualValues(t, 7*100_000_000, second.Qty, "六份买盘只剩四份可卖，剩余持仓保留")
	_, err = SellTradePrediction(context.Background(), 6281, first.Id, decimal.Zero, tradeTestDecimal("0.3"))
	require.ErrorIs(t, err, ErrTradeNoLiquidity)
	assert.Equal(t, tradeTestUsd("998.6964"), tradeTestCash(t, 6281))
	assert.Equal(t, tradeTestUsd("995.3438"), tradeTestCash(t, 6282))
}

func TestTradePredictionSnapshotIdentityRejectsDepthReplays(t *testing.T) {
	fixture := setupPredictionOrderTest(t)
	_, err := fixture.buy(6281, "100", model.TradePredictionUp)
	require.NoError(t, err)
	stamp, err := strconv.ParseInt(fixture.book.Timestamp, 10, 64)
	require.NoError(t, err)

	fixture.mu.Lock()
	fixture.book.Asks = []tradePredictionLevel{{"0.60", "5.0000"}, {"0.500", "4"}, {"0.5", "6.0"}}
	fixture.mu.Unlock()
	_, err = fixture.buy(6282, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, ErrTradeNoLiquidity, "重排、重复价位及小数格式不能伪装成新深度")

	fixture.mu.Lock()
	fixture.book.Timestamp = strconv.FormatInt(stamp+1, 10)
	fixture.mu.Unlock()
	_, err = fixture.buy(6282, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, ErrTradeNoLiquidity, "只更新时间戳不补充挂单")

	fixture.mu.Lock()
	fixture.book.Asks = []tradePredictionLevel{{"0.5", "12"}, {"0.6", "5"}}
	fixture.book.Timestamp = strconv.FormatInt(stamp, 10)
	fixture.mu.Unlock()
	_, err = fixture.buy(6282, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, model.ErrTradePredictionQuote, "旧盘口不能回退已确认的时间戳")
	fixture.mu.Lock()
	fixture.book.Timestamp = strconv.FormatInt(stamp+1, 10)
	fixture.mu.Unlock()
	_, err = fixture.buy(6282, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, model.ErrTradePredictionQuote, "同一时间戳内容矛盾时拒绝成交")

	fixture.mu.Lock()
	fixture.book.Timestamp = strconv.FormatInt(stamp+2, 10)
	fixture.mu.Unlock()
	updated, err := fixture.buy(6282, "100", model.TradePredictionUp)
	require.NoError(t, err)
	assert.EqualValues(t, 17*100_000_000, updated.Qty)
	other, err := fixture.buy(6281, "100", model.TradePredictionDown)
	require.NoError(t, err)
	assert.EqualValues(t, 17*100_000_000, other.Qty, "UP 和 DOWN 是独立的 token 盘口")
}

func TestTradePredictionFailedAccountingDoesNotConsumeDepth(t *testing.T) {
	fixture := setupPredictionOrderTest(t)
	failed := errors.New("prediction ledger unavailable")
	failLedger := true
	const callback = "test:prediction_order_ledger"
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if failLedger && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "trade_ledgers" {
			tx.AddError(failed)
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove(callback)) })
	_, err := fixture.buy(6281, "100", model.TradePredictionUp)
	require.ErrorIs(t, err, failed)
	assert.Equal(t, tradeTestUsd("1000"), tradeTestCash(t, 6281))
	var count int64
	require.NoError(t, model.DB.Model(&model.TradePredictionPosition{}).Where("user_id = ?", 6281).Count(&count).Error)
	assert.Zero(t, count, "买入和流水一起回滚")
	failLedger = false
	position, err := fixture.buy(6281, "100", model.TradePredictionUp)
	require.NoError(t, err)
	assert.EqualValues(t, 15*100_000_000, position.Qty, "失败买入没有吃掉卖盘")
	cash := tradeTestCash(t, 6281)
	failLedger = true
	_, err = SellTradePrediction(context.Background(), 6281, position.Id, decimal.Zero, tradeTestDecimal("0.3"))
	require.ErrorIs(t, err, failed)
	assert.Equal(t, cash, tradeTestCash(t, 6281))
	stored, err := model.GetTradePredictionPosition(6281, position.Id)
	require.NoError(t, err)
	assert.Equal(t, position.Qty, stored.Qty)
	failLedger = false
	stored, err = SellTradePrediction(context.Background(), 6281, position.Id, decimal.Zero, tradeTestDecimal("0.3"))
	require.NoError(t, err)
	assert.EqualValues(t, 9*100_000_000, stored.Qty, "失败卖出没有吃掉买盘")
}

func TestTradePredictionConcurrentBuyersShareTokenDepth(t *testing.T) {
	fixture := setupPredictionOrderTest(t)
	type result struct {
		position *model.TradePredictionPosition
		err      error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, userId := range []int{6281, 6282} {
		go func() {
			<-start
			position, err := fixture.buy(userId, "100", model.TradePredictionUp)
			results <- result{position, err}
		}()
	}
	close(start)
	filled, empty := 0, 0
	for range 2 {
		got := <-results
		if got.err != nil {
			assert.ErrorIs(t, got.err, ErrTradeNoLiquidity)
			empty++
			continue
		}
		assert.EqualValues(t, 15*100_000_000, got.position.Qty)
		filled++
	}
	assert.Equal(t, 1, filled)
	assert.Equal(t, 1, empty)
	var quantity int64
	require.NoError(t, model.DB.Model(&model.TradePredictionPosition{}).Select("COALESCE(SUM(qty), 0)").Scan(&quantity).Error)
	assert.EqualValues(t, 15*100_000_000, quantity)
}

func TestTradePredictionBuyConfirmsSpotCollateralOnFollower(t *testing.T) {
	fixture := setupPredictionOrderTest(t)
	common.IsMasterNode = false
	setTradeTestBook(1, tradeTestLevels("99", "20"), tradeTestLevels("100", "20"))
	tradeMarket.mu.Lock()
	previousSync := tradeMarket.bookSyncedAt
	tradeMarket.bookSyncedAt = time.Time{}
	tradeMarket.mu.Unlock()
	t.Cleanup(func() {
		tradeMarket.mu.Lock()
		tradeMarket.bookSyncedAt = previousSync
		tradeMarket.mu.Unlock()
	})
	require.NoError(t, model.DB.Create(&model.TradePosition{UserId: 6281, Symbol: "BTCUSDT", Qty: 100_000_000, Cost: tradeTestUsd("100")}).Error)
	require.NoError(t, model.DB.Create(&model.TradeSpotMargin{UserId: 6281, Principal: tradeTestUsd("10"), DailyRate: "0", ChargedUntil: common.GetTimestamp() + 3600}).Error)
	_, fresh := freshSpotTop("BTCUSDT", false)
	assert.False(t, fresh)
	position, err := fixture.buy(6281, "100", model.TradePredictionUp)
	require.NoError(t, err)
	assert.EqualValues(t, 15*100_000_000, position.Qty)
	_, fresh = freshSpotTop("BTCUSDT", false)
	assert.True(t, fresh, "预测买入自己确认了现货抵押价格，无需先操作现货")
}
