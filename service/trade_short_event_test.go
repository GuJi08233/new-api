package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
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
	assert.Equal(t, "B2", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("101.2"), 150))
	assert.Equal(t, "B3", EvaluateShortRangeBucket(decimal.NewFromInt(100), decimal.RequireFromString("102.0"), 150))
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
