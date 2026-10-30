package horserace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每一注的理论返还率都在 94% 到 96% 之间、低于 100%；实力合计 100，跑第一与跑进前二的概率各自合计 1、2。
func TestPaybackIsBelowOneHundredPercent(t *testing.T) {
	strength, win, place := 0, 0.0, 0.0
	for horse := 0; horse < Horses; horse++ {
		strength += Strength[horse]
		win += WinChance(horse)
		place += PlaceChance(horse)
		for _, kind := range []string{BetWin, BetPlace} {
			payback := Payback(kind, horse)
			assert.Greater(t, payback, 0.94, "%s %d", kind, horse)
			assert.Less(t, payback, 0.96, "%s %d", kind, horse)
		}
	}
	assert.Equal(t, 100, strength)
	assert.InDelta(t, 1, win, 1e-9)
	assert.InDelta(t, 2, place, 1e-9)
}

// 名次按实力依次抽签：签落在哪匹马的区间就是哪匹马，抽出的马从下一签里去掉；每匹马出现一次。
func TestRaceDrawsByStrength(t *testing.T) {
	draw := func(rolls ...int) []int {
		order, err := raceWith(func(n int) (int, error) {
			roll := rolls[0]
			rolls = rolls[1:]
			require.Less(t, roll, n)
			return roll, nil
		})
		require.NoError(t, err)
		return order
	}
	// 第一签 0..29 是第 0 匹；之后每签都是 0，依次抽到剩下的第一匹。
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5}, draw(29, 0, 0, 0, 0, 0))
	// 第一签 30 是第 1 匹(30..51)；剩下 78，签 77 是最后一匹第 5 匹。
	assert.Equal(t, []int{1, 5, 0, 2, 3, 4}, draw(30, 77, 0, 0, 0, 0))

	for n := 0; n < 50; n++ {
		order, err := Race()
		require.NoError(t, err)
		assert.ElementsMatch(t, []int{0, 1, 2, 3, 4, 5}, order)
	}
}

// 独赢只认第一名，位置认前两名；不存在的马与玩法不中。
func TestBetsSettleByOrder(t *testing.T) {
	order := []int{3, 0, 5, 1, 2, 4}
	assert.Equal(t, Odds[BetWin][3], Multiplier(BetWin, 3, order))
	assert.Zero(t, Multiplier(BetWin, 0, order))
	assert.Equal(t, Odds[BetPlace][0], Multiplier(BetPlace, 0, order))
	assert.Equal(t, Odds[BetPlace][3], Multiplier(BetPlace, 3, order))
	assert.Zero(t, Multiplier(BetPlace, 5, order))
	assert.Zero(t, Multiplier("show", 3, order))
	assert.Zero(t, Multiplier(BetWin, 6, order))
	assert.False(t, ValidBet(BetWin, -1))
}
