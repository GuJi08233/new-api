package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGameJackpotPoolLimitRejectsEntryWithoutCharging(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2401, 0)
	seedGameUser(t, 2402, 0)
	_, err := AdjustGameCoins(2401, common.MaxQuota-1, "seed boundary balance")
	require.NoError(t, err)
	_, err = AdjustGameCoins(2402, 2, "seed boundary balance")
	require.NoError(t, err)
	rule := GameJackpotEntryRule{MaxBet: common.MaxQuota - 1, RoundSeconds: 60, RakePercent: 10}
	round, _, err := EnterGameJackpot(2401, common.MaxQuota-2, rule, gameTestIp)
	require.NoError(t, err)
	_, _, err = EnterGameJackpot(2402, 2, rule, gameTestOtherIp)
	require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	assert.Equal(t, GameCoins(2), gameBalance(t, 2402))
	stillOpen, err := GetOpenGameJackpotRound()
	require.NoError(t, err)
	require.NotNil(t, stillOpen)
	assert.Equal(t, *round, *stillOpen, "拒绝过大的投入不关闭本轮，较小金额仍可投入")
	round, _, err = EnterGameJackpot(2402, 1, rule, gameTestOtherIp)
	require.NoError(t, err)
	require.Equal(t, GameCoins(common.MaxQuota-1), round.Pot)

	before := *round
	accountBefore, err := GetGameCoinAccount(2402)
	require.NoError(t, err)
	entriesBefore, err := ListGameJackpotEntries(round.Id)
	require.NoError(t, err)
	var logsBefore []GameCoinLog
	require.NoError(t, DB.Order("id").Find(&logsBefore).Error)

	_, _, err = EnterGameJackpot(2402, 1, rule, gameTestOtherIp)
	require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	after, err := GetOpenGameJackpotRound()
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before, *after)
	accountAfter, err := GetGameCoinAccount(2402)
	require.NoError(t, err)
	assert.Equal(t, accountBefore, accountAfter)
	entriesAfter, err := ListGameJackpotEntries(round.Id)
	require.NoError(t, err)
	assert.Equal(t, entriesBefore, entriesAfter)
	var logsAfter []GameCoinLog
	require.NoError(t, DB.Order("id").Find(&logsAfter).Error)
	assert.Equal(t, logsBefore, logsAfter)

	require.NoError(t, DB.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Update("deadline", 1).Error)
	drawn, err := DrawGameJackpot(func(tickets int) (int, error) {
		assert.Equal(t, common.MaxQuota-1, tickets)
		return 0, nil
	}, "2026-10-15")
	require.NoError(t, err)
	require.NotNil(t, drawn)
	assert.Equal(t, 2401, drawn.WinnerId)
	assert.Equal(t, GameCoins(common.MaxQuota-1), drawn.Payout+drawn.Rake)
	assert.Equal(t, 1+drawn.Payout, gameBalance(t, 2401))
	assert.Equal(t, GameCoins(1), gameBalance(t, 2402))
}

func TestGameJackpotRejectsInvalidExistingPool(t *testing.T) {
	for _, pot := range []GameCoins{-1, common.MaxQuota} {
		t.Run(pot.Decimal().String(), func(t *testing.T) {
			truncateTables(t)
			seedGameUser(t, 2403, 1)
			round := GameJackpotRound{Slot: gameJackpotOpenSlot, Status: GameJackpotStatusOpen, Pot: pot}
			require.NoError(t, DB.Create(&round).Error)
			balance := gameBalance(t, 2403)
			_, _, err := EnterGameJackpot(2403, 1, GameJackpotEntryRule{MaxBet: coin(1), RoundSeconds: 60}, gameTestIp)
			require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
			assert.Equal(t, balance, gameBalance(t, 2403))
			after, err := GetOpenGameJackpotRound()
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, round, *after)
			entries, err := ListGameJackpotEntries(round.Id)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestGameJackpotDrawRejectsInvalidPoolBeforePicking(t *testing.T) {
	for _, test := range []struct {
		name string
		pot  GameCoins
		rake int
	}{
		{name: "negative pool", pot: -1},
		{name: "empty pool", pot: 0},
		{name: "pool at quota limit", pot: common.MaxQuota},
		{name: "negative rake", pot: 10, rake: -1},
		{name: "rake exceeds pool", pot: 10, rake: 101},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			round := GameJackpotRound{Slot: gameJackpotOpenSlot, Status: GameJackpotStatusOpen,
				Pot: test.pot, RakePercent: test.rake, Deadline: 1}
			require.NoError(t, DB.Create(&round).Error)
			called := false
			drawn, err := DrawGameJackpot(func(int) (int, error) {
				called = true
				return 0, nil
			}, "2026-10-15")
			require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
			assert.Nil(t, drawn)
			assert.False(t, called, "invalid money must not reach ticket selection")
			after, err := GetOpenGameJackpotRound()
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, round, *after)
		})
	}
}
