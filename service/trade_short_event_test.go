package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateShortPKAndRangeOutcomes(t *testing.T) {
	// A 涨 5%，B 涨 2% -> A 胜
	assert.Equal(t, "A", EvaluateShortPKOutcome(
		decimal.NewFromInt(100), decimal.NewFromInt(105),
		decimal.NewFromInt(200), decimal.NewFromInt(204),
	))
	// A 跌 2%，B 跌 1% -> B 胜
	assert.Equal(t, "B", EvaluateShortPKOutcome(
		decimal.NewFromInt(100), decimal.NewFromInt(98),
		decimal.NewFromInt(100), decimal.NewFromInt(99),
	))
	// 涨幅完全一致 -> 退本
	assert.Equal(t, model.TradeShortOutcomeRefund, EvaluateShortPKOutcome(
		decimal.NewFromInt(100), decimal.NewFromInt(110),
		decimal.NewFromInt(50), decimal.NewFromInt(55),
	))

	// 4 档区间测试 (阈值 150 bps = 1.5%)
	assert.Equal(t, "B0", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("98.0"), 150))
	assert.Equal(t, "B1", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("99.2"), 150))
	// 价格完全持平(0%)必须判退本，杜绝休市/横盘品种白送 B2 获胜。
	assert.Equal(t, model.TradeShortOutcomeRefund, EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.NewFromInt(100), 150))
	assert.Equal(t, "B2", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("101.2"), 150))
	assert.Equal(t, "B3", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("102.0"), 150))
}

func TestShortEventCanonicalMarketAndFreshMidSpread(t *testing.T) {
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled = true
	setting.FuturesEnabled = false

	// 合约未开放时，BTCUSDT 走现货；仅合约品种 XAUUSDT 不允许开放。
	src, m, sym, ok := resolveShortEventMarket("BTCUSDT", setting)
	require.True(t, ok)
	assert.Equal(t, model.TradeShortSourceSpot, src)
	assert.Equal(t, tradeMarket, m)
	assert.Equal(t, "BTCUSDT", sym)

	_, _, _, ok = resolveShortEventMarket("XAUUSDT", setting)
	assert.False(t, ok)

	// 开放合约后，XAUUSDT 规范绑定 futures；同名 BTCUSDT 仍一致绑定 spot，绝不下单现货、结算合约。
	setting.FuturesEnabled = true
	src, m, sym, ok = resolveShortEventMarket("XAUUSDT", setting)
	require.True(t, ok)
	assert.Equal(t, model.TradeShortSourceFutures, src)
	assert.Equal(t, futuresMarket, m)
	assert.Equal(t, "XAUUSDT", sym)

	src, _, _, ok = resolveShortEventMarket("BTCUSDT", setting)
	require.True(t, ok)
	assert.Equal(t, model.TradeShortSourceSpot, src)

	// FreshMidPrice 返回真实买一/卖一中间价与点差，未连接或倒挂盘口拒绝。
	testMkt := newTradeMarket(false)
	testMkt.connected = true
	testMkt.books["BTCUSDT"] = &tradeBook{
		bids: []tradesim.Level{{Price: decimal.NewFromInt(99900), Qty: decimal.NewFromInt(1)}},
		asks: []tradesim.Level{{Price: decimal.NewFromInt(100100), Qty: decimal.NewFromInt(1)}},
	}
	mid, spreadBps, ok := testMkt.FreshMidPrice("BTCUSDT")
	require.True(t, ok)
	assert.True(t, mid.Equal(decimal.NewFromInt(100000)))
	assert.True(t, spreadBps.Equal(decimal.NewFromInt(20)))
}

func TestResolveTradePredictionMarketSpec(t *testing.T) {
	spec, ok := ResolveTradePredictionMarketSpec("", "")
	require.True(t, ok)
	assert.True(t, spec.DefaultBtc5m)
	assert.Equal(t, "btc-5m", spec.MarketKey)

	spec, ok = ResolveTradePredictionMarketSpec("eth", "15m")
	require.True(t, ok)
	assert.False(t, spec.DefaultBtc5m)
	assert.Equal(t, "eth-15m", spec.MarketKey)
	assert.Equal(t, int64(900), spec.IntervalSec)

	_, ok = ResolveTradePredictionMarketSpec("DOGE", "5m")
	assert.False(t, ok)
}
