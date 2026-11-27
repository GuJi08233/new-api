package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTradeShortTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TradeShortRound{}, &TradeShortOrder{}, &TradePredictionSeriesRound{}, &TradePredictionSeriesPosition{}))
	truncateTables(t)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&TradeShortOrder{}).Error)
		require.NoError(t, DB.Where("1 = 1").Delete(&TradeShortRound{}).Error)
	})
}

func TestTradeShortPoolSettlementConservationAndIdempotency(t *testing.T) {
	setupTradeShortTest(t)
	seedTradeFuturesAccount(t, 7101, 200)
	seedTradeFuturesAccount(t, 7102, 200)
	seedTradeFuturesAccount(t, 7103, 200)

	now := time.Now().Unix()
	round, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:    "pk:ant_vs_openai:900:test1",
		Kind:        TradeShortKindPK,
		Title:       "Anthropic vs OpenAI",
		SymbolA:     "ANTUSDT",
		SymbolB:     "OPENAIUSDT",
		DurationSec: 900,
		LockTime:    now + 300,
		StartTime:   now,
		EndTime:     now + 900,
		FeeBps:      300, // 3%
		Status:      TradeShortRoundOpen,
	})
	require.NoError(t, err)

	// 7101 押 A 60 USDT，7102 押 A 40 USDT，7103 押 B 100 USDT -> 总奖池 200，扣 3% = 194，A 胜方按 60/100 与 40/100 瓜分。
	o1, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7101, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindPK,
		SymbolA: "ANTUSDT", SymbolB: "OPENAIUSDT", Choice: "A", DurationSec: 900,
		Stake: tradeUsd(60), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)
	o2, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7102, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindPK,
		SymbolA: "ANTUSDT", SymbolB: "OPENAIUSDT", Choice: "A", DurationSec: 900,
		Stake: tradeUsd(40), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)
	o3, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7103, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindPK,
		SymbolA: "ANTUSDT", SymbolB: "OPENAIUSDT", Choice: "B", DurationSec: 900,
		Stake: tradeUsd(100), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)

	require.NoError(t, SettleTradeShortPoolRound(round.Id, "A", "100", "200", "105", "202"))
	// 重复调用必须幂等，不重复入账。
	require.NoError(t, SettleTradeShortPoolRound(round.Id, "A", "100", "200", "105", "202"))

	// 194 * 0.6 = 116.4；194 * 0.4 = 77.6；总派彩 194 <= 200。
	assert.Equal(t, tradeUsd(200)-tradeUsd(60)+tradeUsd(116.4), tradeTestAccount(t, 7101).Cash)
	assert.Equal(t, tradeUsd(200)-tradeUsd(40)+tradeUsd(77.6), tradeTestAccount(t, 7102).Cash)
	assert.Equal(t, tradeUsd(200)-tradeUsd(100), tradeTestAccount(t, 7103).Cash)

	var updated1, updated2, updated3 TradeShortOrder
	require.NoError(t, DB.First(&updated1, o1.Id).Error)
	require.NoError(t, DB.First(&updated2, o2.Id).Error)
	require.NoError(t, DB.First(&updated3, o3.Id).Error)
	assert.Equal(t, TradeShortStatusWon, updated1.Status)
	assert.Equal(t, TradeShortStatusWon, updated2.Status)
	assert.Equal(t, TradeShortStatusLost, updated3.Status)
	assert.LessOrEqual(t, updated1.Payout+updated2.Payout+updated3.Payout, tradeUsd(194))
}

func TestTradeShortPoolRefundsSingleSideWithZeroFee(t *testing.T) {
	setupTradeShortTest(t)
	seedTradeFuturesAccount(t, 7104, 100)

	now := time.Now().Unix()
	round, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:    "pk:btc_vs_eth:900:solo",
		Kind:        TradeShortKindPK,
		SymbolA:     "BTCUSDT",
		SymbolB:     "ETHUSDT",
		DurationSec: 900,
		LockTime:    now + 300,
		StartTime:   now,
		EndTime:     now + 900,
		FeeBps:      300,
		Status:      TradeShortRoundOpen,
	})
	require.NoError(t, err)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7104, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindPK,
		SymbolA: "BTCUSDT", SymbolB: "ETHUSDT", Choice: "A", DurationSec: 900,
		Stake: tradeUsd(50), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)

	require.NoError(t, SettleTradeShortPoolRound(round.Id, "B", "100", "100", "98", "105"))
	// 单边下注不论结果如何均 100% 退回本金，0 手续费。
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, 7104).Cash)
}

func TestTradeShortInstantOrderLimitsAndSettlement(t *testing.T) {
	setupTradeShortTest(t)
	seedTradeFuturesAccount(t, 7105, 100)

	now := time.Now().Unix()
	order, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7105, Mode: TradeShortModeInstant, Kind: TradeShortKindFlash,
		SymbolA: "NVDAUSDT", Choice: "UP", DurationSec: 60,
		EntryPriceA: "140.0", Stake: tradeUsd(40), PayoutBps: 18500,
		MaxActive: tradeUsd(60), ExpiresAt: now + 60,
	})
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(60), tradeTestAccount(t, 7105).Cash)

	// 超出在途上限(40 + 30 > 60)应被拒绝。
	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7105, Mode: TradeShortModeInstant, Kind: TradeShortKindFlash,
		SymbolA: "NVDAUSDT", Choice: "DOWN", DurationSec: 60,
		EntryPriceA: "140.0", Stake: tradeUsd(30), PayoutBps: 18500,
		MaxActive: tradeUsd(60), ExpiresAt: now + 60,
	})
	require.ErrorIs(t, err, ErrTradeShortLimitReached)

	// 获胜按 1.85x 派彩(40 * 1.85 = 74)，余额变为 60 + 74 = 134。
	require.NoError(t, SettleTradeShortInstantOrder(order.Id, "UP", "141.5", ""))
	require.NoError(t, SettleTradeShortInstantOrder(order.Id, "UP", "141.5", ""))
	assert.Equal(t, tradeUsd(134), tradeTestAccount(t, 7105).Cash)
}
