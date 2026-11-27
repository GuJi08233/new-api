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
		StartTime:   now + 300,
		EndTime:     now + 1200,
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

func TestTradeShortPoolRefundsSingleSideWithZeroFeeAndNoWagerCredit(t *testing.T) {
	setupTradeShortTest(t)
	require.NoError(t, DB.AutoMigrate(&GameCoinAccount{}, &GameCoinLog{}))
	seedTradeFuturesAccount(t, 7104, 100)
	require.NoError(t, DB.Create(&GameCoinAccount{
		UserId:      7104,
		Balance:     GameCoins(tradeUsd(100)),
		LockedWager: GameCoins(tradeUsd(100)),
	}).Error)
	t.Cleanup(func() {
		_ = DB.Where("user_id = ?", 7104).Delete(&GameCoinAccount{}).Error
	})

	now := time.Now().Unix()
	round, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:    "pk:btc_vs_eth:900:solo",
		Kind:        TradeShortKindPK,
		SymbolA:     "BTCUSDT",
		SymbolB:     "ETHUSDT",
		DurationSec: 900,
		LockTime:    now + 300,
		StartTime:   now + 300,
		EndTime:     now + 1200,
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

	// 下单瞬间绝不提前解锁游戏币流水。
	coinAcc, err := GetGameCoinAccount(7104)
	require.NoError(t, err)
	assert.Equal(t, GameCoins(tradeUsd(100)), coinAcc.LockedWager)

	require.NoError(t, SettleTradeShortPoolRound(round.Id, "B", "100", "100", "98", "105"))
	// 单边下注不论结果如何均 100% 退回本金，0 手续费，且不解锁游戏币流水。
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, 7104).Cash)
	coinAcc, err = GetGameCoinAccount(7104)
	require.NoError(t, err)
	assert.Equal(t, GameCoins(tradeUsd(100)), coinAcc.LockedWager)
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

	// 同标的反向对冲应被拒绝。
	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7105, Mode: TradeShortModeInstant, Kind: TradeShortKindFlash,
		SymbolA: "NVDAUSDT", Choice: "DOWN", DurationSec: 60,
		EntryPriceA: "140.0", Stake: tradeUsd(10), PayoutBps: 18500,
		MaxActive: tradeUsd(60), ExpiresAt: now + 60,
	})
	require.ErrorIs(t, err, ErrTradeShortHedgeForbidden)

	// 超出在途上限(40 + 30 > 60)应被拒绝。
	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7105, Mode: TradeShortModeInstant, Kind: TradeShortKindFlash,
		SymbolA: "BTCUSDT", Choice: "UP", DurationSec: 60,
		EntryPriceA: "90000", Stake: tradeUsd(30), PayoutBps: 18500,
		MaxActive: tradeUsd(60), ExpiresAt: now + 60,
	})
	require.ErrorIs(t, err, ErrTradeShortLimitReached)

	// 获胜按 1.85x 派彩(40 * 1.85 = 74)，余额变为 60 + 74 = 134。
	require.NoError(t, SettleTradeShortInstantOrder(order.Id, "UP", "141.5", ""))
	require.NoError(t, SettleTradeShortInstantOrder(order.Id, "UP", "141.5", ""))
	assert.Equal(t, tradeUsd(134), tradeTestAccount(t, 7105).Cash)
}

func TestTradeShortAntiExploitGuards(t *testing.T) {
	setupTradeShortTest(t)
	seedTradeFuturesAccount(t, 7106, 200)
	seedTradeFuturesAccount(t, 7107, 200)
	// 给 7107 写入体验金本金，模拟持有未结清体验金的账户。
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 7107).
		Updates(map[string]any{"bonus_principal": tradeUsd(100), "bonus_mode": TradeBonusModeFirst}).Error)

	now := time.Now().Unix()

	// 1. Range / Touch 禁止走固定赔率即时模式。
	_, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, Mode: TradeShortModeInstant, Kind: TradeShortKindRange,
		SymbolA: "XAUUSDT", Choice: "B2", DurationSec: 3600, ThresholdBps: 150,
		EntryPriceA: "2600", Stake: tradeUsd(50), PayoutBps: 32375,
		MaxActive: tradeUsd(500), ExpiresAt: now + 3600,
	})
	require.ErrorIs(t, err, ErrTradeShortModeNotAllowed)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, Mode: TradeShortModeInstant, Kind: TradeShortKindTouch,
		SymbolA: "XAUUSDT", Choice: "NO_TOUCH", DurationSec: 900, ThresholdBps: 100,
		EntryPriceA: "2600", Stake: tradeUsd(50), PayoutBps: 17000,
		MaxActive: tradeUsd(500), ExpiresAt: now + 900,
	})
	require.ErrorIs(t, err, ErrTradeShortModeNotAllowed)

	// 2. 已开赛的奖池局(now >= StartTime)即使 LockTime 写大也必须拒绝下注，彻底堵死盘中偷跑。
	startedRound, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:    "pk:nvda_vs_amd:900:started",
		Kind:        TradeShortKindPK,
		SymbolA:     "NVDAUSDT",
		SymbolB:     "AMDUSDT",
		DurationSec: 900,
		LockTime:    now + 200,
		StartTime:   now - 10,
		EndTime:     now + 890,
		FeeBps:      300,
		Status:      TradeShortRoundOpen,
	})
	require.NoError(t, err)
	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, RoundId: startedRound.Id, Mode: TradeShortModePool, Kind: TradeShortKindPK,
		SymbolA: "NVDAUSDT", SymbolB: "AMDUSDT", Choice: "A", DurationSec: 900,
		Stake: tradeUsd(20), MaxActive: tradeUsd(500), ExpiresAt: startedRound.EndTime,
	})
	require.ErrorIs(t, err, ErrTradeShortRoundClosed)

	// 3. 未来预售奖池：体验金账户禁止参与；普通账户不可同局押多个档位。
	futureRound, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:     "range:btc:3600:future",
		Kind:         TradeShortKindRange,
		SymbolA:      "BTCUSDT",
		DurationSec:  3600,
		ThresholdBps: 150,
		LockTime:     now + 300,
		StartTime:    now + 300,
		EndTime:      now + 3900,
		FeeBps:       300,
		Status:       TradeShortRoundOpen,
	})
	require.NoError(t, err)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7107, RoundId: futureRound.Id, Mode: TradeShortModePool, Kind: TradeShortKindRange,
		SymbolA: "BTCUSDT", Choice: "B1", DurationSec: 3600,
		Stake: tradeUsd(20), MaxActive: tradeUsd(500), ExpiresAt: futureRound.EndTime,
	})
	require.ErrorIs(t, err, ErrTradeShortBonusNotAllowed)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, RoundId: futureRound.Id, Mode: TradeShortModePool, Kind: TradeShortKindRange,
		SymbolA: "BTCUSDT", Choice: "B1", DurationSec: 3600,
		Stake: tradeUsd(20), MaxActive: tradeUsd(500), ExpiresAt: futureRound.EndTime,
	})
	require.NoError(t, err)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, RoundId: futureRound.Id, Mode: TradeShortModePool, Kind: TradeShortKindRange,
		SymbolA: "BTCUSDT", Choice: "B2", DurationSec: 3600,
		Stake: tradeUsd(20), MaxActive: tradeUsd(500), ExpiresAt: futureRound.EndTime,
	})
	require.ErrorIs(t, err, ErrTradeShortHedgeForbidden)

	// 4. 即时 PK 反向对调 A/B 押对立方向也应被识别为对冲并拒绝。
	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, Mode: TradeShortModeInstant, Kind: TradeShortKindPK,
		SymbolA: "NVDAUSDT", SymbolB: "AMDUSDT", Choice: "A", DurationSec: 300,
		EntryPriceA: "140", EntryPriceB: "150", Stake: tradeUsd(20), PayoutBps: 18500,
		MaxActive: tradeUsd(500), ExpiresAt: now + 300,
	})
	require.NoError(t, err)

	_, err = PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7106, Mode: TradeShortModeInstant, Kind: TradeShortKindPK,
		SymbolA: "AMDUSDT", SymbolB: "NVDAUSDT", Choice: "A", DurationSec: 300,
		EntryPriceA: "150", EntryPriceB: "140", Stake: tradeUsd(20), PayoutBps: 18500,
		MaxActive: tradeUsd(500), ExpiresAt: now + 300,
	})
	require.ErrorIs(t, err, ErrTradeShortHedgeForbidden)
}

func TestTradeShortPoolCrashRecoveryPreservesExactRatio(t *testing.T) {
	setupTradeShortTest(t)
	seedTradeFuturesAccount(t, 7108, 200)
	seedTradeFuturesAccount(t, 7109, 200)

	now := time.Now().Unix()
	round, err := EnsureTradeShortRound(TradeShortRound{
		RoundKey:    "touch:btc:900:crash",
		Kind:        TradeShortKindTouch,
		SymbolA:     "BTCUSDT",
		DurationSec: 900,
		LockTime:    now + 300,
		StartTime:   now + 300,
		EndTime:     now + 1200,
		FeeBps:      300,
		Status:      TradeShortRoundOpen,
	})
	require.NoError(t, err)

	oWin, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7108, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindTouch,
		SymbolA: "BTCUSDT", Choice: "TOUCH_UP", DurationSec: 900,
		Stake: tradeUsd(100), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)
	oLose, err := PlaceTradeShortOrder(TradeShortOrderInput{
		UserId: 7109, RoundId: round.Id, Mode: TradeShortModePool, Kind: TradeShortKindTouch,
		SymbolA: "BTCUSDT", Choice: "NO_TOUCH", DurationSec: 900,
		Stake: tradeUsd(100), MaxActive: tradeUsd(500), ExpiresAt: round.EndTime,
	})
	require.NoError(t, err)

	// 模拟崩溃：局状态已落库为 settled 且胜方已结算，败方注单尚未更新；恢复不得误将败方当作单边退本。
	require.NoError(t, DB.Model(&TradeShortRound{}).Where("id = ?", round.Id).Updates(map[string]any{
		"status": TradeShortRoundSettled, "outcome": "TOUCH_UP", "end_price_a": "91000",
	}).Error)
	require.NoError(t, DB.Model(&TradeShortOrder{}).Where("id = ?", oWin.Id).Updates(map[string]any{
		"status": TradeShortStatusWon, "payout": tradeUsd(194), "outcome": "TOUCH_UP",
	}).Error)

	ids, err := ListUnsettledTradeShortPoolRoundIds(10)
	require.NoError(t, err)
	require.Equal(t, []int{round.Id}, ids)

	require.NoError(t, SettleTradeShortPoolRound(round.Id, "", "", "", "", ""))

	var settledLose TradeShortOrder
	require.NoError(t, DB.First(&settledLose, oLose.Id).Error)
	assert.Equal(t, TradeShortStatusLost, settledLose.Status)
	assert.Equal(t, 0, settledLose.Payout)
	assert.Equal(t, tradeUsd(100), tradeTestAccount(t, 7109).Cash)
}
