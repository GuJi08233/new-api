package keno

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每一行的理论返还率在 92% 到 95% 之间，最高派彩不超过 1000 倍，各行的中奖概率合计为 1。
func TestPaytableKeepsTheHouseEdge(t *testing.T) {
	for picks := 1; picks <= MaxPicks; picks++ {
		row := Paytable[picks]
		require.Len(t, row, picks+1, "picks %d", picks)
		total := 0.0
		for hits := 0; hits <= picks; hits++ {
			total += HitChance(picks, hits)
			assert.LessOrEqual(t, row[hits], 100000, "picks %d hits %d", picks, hits)
		}
		assert.InDelta(t, 1, total, 1e-9, "picks %d", picks)
		payback := Payback(picks)
		assert.Greater(t, payback, 0.92, "picks %d", picks)
		assert.LessOrEqual(t, payback, 0.95, "picks %d", picks)
	}
}

func TestPicksValidation(t *testing.T) {
	assert.True(t, ValidPicks([]int{1, 40}))
	assert.True(t, ValidPicks([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}))
	for _, picks := range [][]int{nil, {}, {0}, {41}, {3, 3}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}} {
		assert.False(t, ValidPicks(picks), "%v", picks)
	}
	assert.Equal(t, 2, Hits([]int{1, 2, 3}, []int{3, 9, 1}))
	assert.Equal(t, 0, Multiplier(3, 1))
	assert.Equal(t, 4200, Multiplier(3, 3))
	assert.Equal(t, 0, Multiplier(3, 4))
}

func TestDrawReturnsDistinctNumbers(t *testing.T) {
	for i := 0; i < 50; i++ {
		drawn, err := Draw()
		require.NoError(t, err)
		require.Len(t, drawn, Draws)
		assert.True(t, ValidPicks(drawn), "%v", drawn)
	}
}
