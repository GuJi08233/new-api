package model

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fundingTimeline 为真实成交产生的事件安排明确的历史时刻，避免通过 sleep 跨越资金费边界。
func fundingTimeline(t *testing.T, positionId int, times ...int64) {
	t.Helper()
	var exposures []TradeFuturesExposure
	require.NoError(t, DB.Where("position_id = ?", positionId).Order("id").Find(&exposures).Error)
	require.Len(t, exposures, len(times))
	for i, exposure := range exposures {
		require.NoError(t, DB.Model(&TradeFuturesExposure{}).Where("id = ?", exposure.Id).Update("occurred_at", times[i]).Error)
	}
	closed := int64(0)
	if exposures[len(exposures)-1].Qty == 0 {
		closed = times[len(times)-1]
	}
	require.NoError(t, DB.Model(&TradeFuturesFundingState{}).Where("position_id = ?", positionId).
		Updates(map[string]any{"opened_at": times[0], "funding_at": times[0], "closed_at": closed}).Error)
	require.NoError(t, DB.Model(&TradeFuturesPosition{}).Where("position_id = ?", positionId).Update("funding_at", times[0]).Error)
}

func TestTradeFuturesFundingUsesQuantityAtTheOfficialTime(t *testing.T) {
	for _, test := range []struct {
		name string
		add  bool
		qty  int64
	}{
		{"increased after boundary", true, 9 * tradeFuturesCoin},
		{"partially closed after boundary", false, tradeFuturesCoin / 2},
		{"fully closed after boundary", false, tradeFuturesCoin},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			const userId = 94501
			seedTradeFuturesAccount(t, userId, 1000)
			market := tradeTestMarket{tradeFuturesSymbol: "100"}
			openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesShort, tradeFuturesCoin, "100", 10, 0, market)
			positionId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesShort).PositionId
			if test.add {
				openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesShort, test.qty, "900", 10, 0, market)
			} else {
				value := "50"
				if test.qty == tradeFuturesCoin {
					value = "100"
				}
				_, err := closeTradeFutures(userId, tradeFuturesSymbol, TradeFuturesShort, test.qty, value, 0)
				require.NoError(t, err)
			}
			base := time.Now().Add(-time.Hour).UnixMilli()
			fundingTimeline(t, positionId, base, base+2000)
			before := tradeTestAccount(t, userId)
			applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesShort, base+1000,
				decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
			require.NoError(t, err)
			assert.True(t, applied)
			after := tradeTestAccount(t, userId)
			assert.Equal(t, before.Cash+tradeUsd(1), after.Cash, "only the original one unit earns funding")
			assert.Greater(t, after.AssetVersion, before.AssetVersion)
			var entries []TradeLedger
			require.NoError(t, DB.Where("position_id = ? AND type = ?", positionId, TradeLedgerFuturesFunding).Find(&entries).Error)
			require.Len(t, entries, 1)
			assert.Equal(t, tradeFuturesCoin, entries[0].Qty)
			assert.Equal(t, tradeUsd(1), entries[0].Pnl)
			assert.Equal(t, base+1000, entries[0].FundingTime)
			applied, err = ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesShort, base+1000,
				decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, after.Cash, tradeTestAccount(t, userId).Cash)
		})
	}
}

func TestTradeFuturesFundingKeepsClosedAndReopenedPositionsSeparate(t *testing.T) {
	truncateTables(t)
	const userId = 94502
	seedTradeFuturesAccount(t, userId, 1000)
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	oldId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	_, err := closeTradeFutures(userId, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin, "100", 0)
	require.NoError(t, err)
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, 2*tradeFuturesCoin, "200", 10, 0, nil)
	newId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	base := time.Now().Add(-time.Hour).UnixMilli()
	fundingTimeline(t, oldId, base, base+2000)
	fundingTimeline(t, newId, base+3000)
	before := tradeTestAccount(t, userId)
	applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+1000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, before.Cash-tradeUsd(1), tradeTestAccount(t, userId).Cash)
	position := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong)
	assert.Equal(t, tradeUsd(20), position.Margin, "the new position's margin does not pay an old position's fee")
	assert.Zero(t, position.Funding)
	history := tradeFuturesHistoryOf(t, userId)
	require.Len(t, history, 1)
	assert.Equal(t, oldId, history[0].PositionId)
	assert.Equal(t, -tradeUsd(1), history[0].Funding)
	assert.Equal(t, -tradeUsd(1), history[0].Pnl)
	applied, err = ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+4000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, tradeUsd(18), tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).Margin)
	var oldState TradeFuturesFundingState
	require.NoError(t, DB.First(&oldState, oldId).Error)
	assert.GreaterOrEqual(t, oldState.FundingAt, oldState.ClosedAt)
}

func TestTradeFuturesFundingRecoversMultiplePeriodsAfterFullClose(t *testing.T) {
	truncateTables(t)
	const userId = 94503
	seedTradeFuturesAccount(t, userId, 1000)
	market := tradeTestMarket{tradeFuturesSymbol: "100"}
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	positionId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, 9*tradeFuturesCoin, "900", 10, 0, market)
	_, err := closeTradeFutures(userId, tradeFuturesSymbol, TradeFuturesLong, 10*tradeFuturesCoin, "1000", 0)
	require.NoError(t, err)
	base := time.Now().Add(-time.Hour).UnixMilli()
	fundingTimeline(t, positionId, base, base+2000, base+4000)
	from, pending, err := GetTradeFuturesFundingFrom(tradeFuturesSymbol)
	require.NoError(t, err)
	assert.True(t, pending)
	assert.Equal(t, base, from)
	symbols, err := ListTradeFuturesPositionSymbols()
	require.NoError(t, err)
	assert.Contains(t, symbols, tradeFuturesSymbol, "closed pending positions keep funding recovery active")
	for _, offset := range []int64{1000, 3000} {
		due, err := ListTradeFuturesFundingDue(tradeFuturesSymbol, base+offset)
		require.NoError(t, err)
		require.Len(t, due, 1)
		applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+offset,
			decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
		require.NoError(t, err)
		assert.True(t, applied)
	}
	assert.Equal(t, tradeUsd(989), tradeTestAccount(t, userId).Cash)
	assert.Equal(t, -tradeUsd(11), tradeFuturesHistoryOf(t, userId)[0].Funding)
	applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+5000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.False(t, applied, "the next official period confirms zero exposure after closing")
	_, pending, err = GetTradeFuturesFundingFrom(tradeFuturesSymbol)
	require.NoError(t, err)
	assert.False(t, pending)
	symbols, err = ListTradeFuturesPositionSymbols()
	require.NoError(t, err)
	assert.NotContains(t, symbols, tradeFuturesSymbol)
}

func TestTradeFuturesFundingAfterLiquidationStillChargesTheOriginalPosition(t *testing.T) {
	truncateTables(t)
	const userId = 94504
	seedTradeFuturesAccount(t, userId, 1000)
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	positionId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	liquidated, err := LiquidateTradeFuturesPosition(userId, tradeFuturesSymbol, TradeFuturesLong,
		decimal.NewFromInt(90), tradeTestMarket{}.Brackets(tradeFuturesSymbol), 0)
	require.NoError(t, err)
	require.True(t, liquidated)
	base := time.Now().Add(-time.Hour).UnixMilli()
	fundingTimeline(t, positionId, base, base+2000)
	applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+1000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, tradeUsd(989), tradeTestAccount(t, userId).Cash)
	history := tradeFuturesHistoryOf(t, userId)
	require.Len(t, history, 1)
	assert.Equal(t, -tradeUsd(1), history[0].Funding)
	assert.Equal(t, -tradeUsd(11), history[0].Pnl)
}

func TestTradeFuturesFundingAfterWithdrawalUsesTheDeficitPath(t *testing.T) {
	truncateTables(t)
	const userId = 94505
	seedTradeFuturesAccount(t, userId, 10)
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	positionId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	_, err := closeTradeFutures(userId, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin, "100", 0)
	require.NoError(t, err)
	_, _, err = TransferQuotaFromTrade(userId, tradeUsd(10), tradeTestDay, 0, nil, TradeEquity{})
	require.NoError(t, err)
	base := time.Now().Add(-time.Hour).UnixMilli()
	fundingTimeline(t, positionId, base, base+2000)
	var covers []TradeDeficitCover
	previousNotifier := TradeDeficitNotifier
	TradeDeficitNotifier = func(id int, cover TradeDeficitCover) {
		assert.Equal(t, userId, id)
		covers = append(covers, cover)
	}
	t.Cleanup(func() { TradeDeficitNotifier = previousNotifier })
	applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+1000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Zero(t, tradeTestAccount(t, userId).Cash)
	quota, err := GetUserQuota(userId, true)
	require.NoError(t, err)
	assert.Equal(t, tradeUsd(9), quota)
	require.Len(t, covers, 1)
	assert.Equal(t, tradeUsd(1), covers[0].Quota)
}

func TestTradeFuturesFundingRebuildsLegacyOpenExposureWithoutRechargingSettledPeriods(t *testing.T) {
	truncateTables(t)
	const userId = 94506
	seedTradeFuturesAccount(t, userId, 1000)
	market := tradeTestMarket{tradeFuturesSymbol: "100"}
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	positionId := tradeTestPosition(t, userId, tradeFuturesSymbol, TradeFuturesLong).PositionId
	openTradeFutures(t, userId, tradeFuturesSymbol, TradeFuturesCross, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, market)
	_, err := closeTradeFutures(userId, tradeFuturesSymbol, TradeFuturesLong, tradeFuturesCoin/2, "50", 0)
	require.NoError(t, err)
	base := time.Now().Add(-time.Hour).Truncate(time.Second).UnixMilli()
	var exposures []TradeFuturesExposure
	require.NoError(t, DB.Where("position_id = ?", positionId).Order("id").Find(&exposures).Error)
	require.Len(t, exposures, 3)
	for i, exposure := range exposures {
		require.NoError(t, DB.Model(&TradeLedger{}).Where("id = ?", exposure.Id).Update("created_at", base/1000+int64(i*2)).Error)
	}
	require.NoError(t, DB.Where("position_id = ?", positionId).Delete(&TradeFuturesExposure{}).Error)
	require.NoError(t, DB.Where("position_id = ?", positionId).Delete(&TradeFuturesFundingState{}).Error)
	require.NoError(t, DB.Model(&TradeFuturesPosition{}).Where("position_id = ?", positionId).
		Updates(map[string]any{"opened_at": base / 1000, "funding_at": base + 1000}).Error)
	from, pending, err := GetTradeFuturesFundingFrom(tradeFuturesSymbol)
	require.NoError(t, err)
	assert.True(t, pending)
	assert.Equal(t, base+1000, from)
	applied, err := ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+1000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.False(t, applied, "the persisted legacy cursor is authoritative")
	applied, err = ApplyTradeFuturesFunding(userId, tradeFuturesSymbol, TradeFuturesLong, base+3000,
		decimal.RequireFromString("0.01"), decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, tradeUsd(998), tradeTestAccount(t, userId).Cash, "the old ledger reconstructs two units at this boundary")
}
