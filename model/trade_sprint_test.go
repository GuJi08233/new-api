package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSprintReturnBpsNeutralizesDeposits(t *testing.T) {
	minBase := tradeUsd(10)
	// 初始 100 USDT，期间盈利 20 USDT -> +20.00% (+2000 bps)
	bps, profit := ComputeSprintReturnBps(tradeUsd(100), tradeUsd(100), tradeUsd(120), tradeUsd(100), minBase)
	assert.Equal(t, int64(2000), bps)
	assert.Equal(t, tradeUsd(20), profit)

	// 盘中再转入 1000 USDT，无实际盈利 -> 0 bps，绝不会被误算为 +1000%
	bps, profit = ComputeSprintReturnBps(tradeUsd(100), tradeUsd(100), tradeUsd(1100), tradeUsd(1100), minBase)
	assert.Equal(t, int64(0), bps)
	assert.Equal(t, 0, profit)

	// 盘中加金 900 USDT 后赚了 100 USDT -> 分母扩大为 1000 USDT，收益率为 +10% (1000 bps)
	bps, profit = ComputeSprintReturnBps(tradeUsd(100), tradeUsd(100), tradeUsd(1100), tradeUsd(1000), minBase)
	assert.Equal(t, int64(1000), bps)
	assert.Equal(t, tradeUsd(100), profit)
}

func TestTradeSprintJoinAndSettlement(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TradeSprint{}, &TradeSprintEntry{}))
	truncateTables(t)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&TradeSprintEntry{}).Error)
		require.NoError(t, DB.Where("1 = 1").Delete(&TradeSprint{}).Error)
	})

	seedTradeFuturesAccount(t, 7201, 100)
	seedTradeFuturesAccount(t, 7202, 15) // 扣 10 报名费后只剩 5 < 10 最低留存
	seedTradeFuturesAccount(t, 7203, 100)
	seedTradeFuturesAccount(t, 7204, 100)
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 7204).
		Updates(map[string]any{"bonus_principal": tradeUsd(50), "bonus_mode": TradeBonusModeMatch}).Error)

	now := time.Now().Unix()
	sprint, err := EnsureTradeSprint(TradeSprint{
		PeriodKey:        "4h-test-01",
		Kind:             TradeSprintKind4H,
		DurationSec:      14400,
		StartTime:        now - 60,
		RegisterDeadline: now + 3600,
		EndTime:          now + 7200,
		EntryFee:         tradeUsd(10),
		FeeBps:           500,
		Status:           TradeSprintStatusOpen,
	})
	require.NoError(t, err)

	equityOf := func(userId int) TradeEquity {
		acc := tradeTestAccount(t, userId)
		return TradeEquity{Value: acc.Cash, Known: true, Account: acc}
	}

	// 持有未结清体验金的账户拒绝参赛。
	_, err = JoinTradeSprint(sprint.Id, 7204, equityOf(7204), nil)
	require.ErrorIs(t, err, ErrTradeSprintBonusNotAllowed)

	// 报名后余额不足 10 USDT 拒绝参赛。
	_, err = JoinTradeSprint(sprint.Id, 7202, equityOf(7202), nil)
	require.ErrorIs(t, err, ErrTradeSprintEquityTooLow)

	// 并发充值 TOCTOU 竞态：估值后资产版本或 TotalIn 变动必须拒绝。
	staleEquity := equityOf(7201)
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 7201).Updates(map[string]any{
		"cash":          tradeUsd(1100),
		"total_in":      tradeUsd(1100),
		"asset_version": staleEquity.Account.AssetVersion + 1,
	}).Error)
	_, err = JoinTradeSprint(sprint.Id, 7201, staleEquity, nil)
	require.ErrorIs(t, err, ErrTradeValuationStale)
	// 恢复 7201 初始状态后用新鲜估值正常报名。
	require.NoError(t, DB.Model(&TradeAccount{}).Where("user_id = ?", 7201).Updates(map[string]any{
		"cash":     tradeUsd(100),
		"total_in": tradeUsd(100),
	}).Error)

	e1, err := JoinTradeSprint(sprint.Id, 7201, equityOf(7201), nil)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(90), e1.StartEquity)
	assert.Equal(t, tradeUsd(90), tradeTestAccount(t, 7201).Cash)

	// 不可重复报名。
	_, err = JoinTradeSprint(sprint.Id, 7201, equityOf(7201), nil)
	require.ErrorIs(t, err, ErrTradeSprintAlreadyJoined)

	e3, err := JoinTradeSprint(sprint.Id, 7203, equityOf(7203), nil)
	require.NoError(t, err)

	// 两人参赛，奖池 20 USDT，扣 5% = 19 USDT；按 70%/30% 分得 13.3 / 5.7。
	rows := []TradeSprintSettlementRow{
		{EntryId: e1.Id, UserId: 7201, EndEquity: tradeUsd(110), EndNetIn: tradeUsd(100), ReturnBps: 2222, FinalRank: 1, Prize: tradeUsd(13.3)},
		{EntryId: e3.Id, UserId: 7203, EndEquity: tradeUsd(95), EndNetIn: tradeUsd(100), ReturnBps: 556, FinalRank: 2, Prize: tradeUsd(5.7)},
	}
	require.NoError(t, SettleTradeSprint(sprint.Id, rows))
	require.NoError(t, SettleTradeSprint(sprint.Id, rows)) // 幂等
	assert.Equal(t, tradeUsd(90+13.3), tradeTestAccount(t, 7201).Cash)
	assert.Equal(t, tradeUsd(90+5.7), tradeTestAccount(t, 7203).Cash)

	// 模拟崩溃恢复：将一条记录置回 paying，RecoverPendingTradeSprintEntries 补发一次并落库为 settled。
	require.NoError(t, DB.Model(&TradeSprintEntry{}).Where("id = ?", e3.Id).Update("status", TradeSprintEntryPaying).Error)
	beforeCash := tradeTestAccount(t, 7203).Cash
	require.NoError(t, RecoverPendingTradeSprintEntries(10))
	assert.Equal(t, beforeCash+tradeUsd(5.7), tradeTestAccount(t, 7203).Cash)
	var recovered TradeSprintEntry
	require.NoError(t, DB.First(&recovered, e3.Id).Error)
	assert.Equal(t, TradeSprintEntrySettled, recovered.Status)
}
