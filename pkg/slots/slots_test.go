package slots

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 老虎机不能凭空产出游戏币：穷举三个转轮的全部 27000 种停点组合，平均每下注 1 个游戏币派彩 25608/27000 ≈ 0.948 个。
// 这个返还率写在玩家看到的规则里；改转轮或赔付表时要确认它仍低于 1，并同步更新这里。
func TestPaybackStaysBelowStake(t *testing.T) {
	paid, combos := Payback()
	assert.Equal(t, 27000, combos)
	assert.Equal(t, 25608, paid)
	assert.Less(t, paid, combos)
}

// 派彩只看中线上从最左边起连续相同的符号：三个相同、只有前两个相同、只有第一个是樱桃各有倍数，
// 不从第一个转轮开始的组合不算。
func TestStopAtPaysRunsFromTheLeft(t *testing.T) {
	cases := []struct {
		line           [3]string
		wantMultiplier int
		wantMatched    int
	}{
		{line: [3]string{Seven, Seven, Seven}, wantMultiplier: 50, wantMatched: 3},
		{line: [3]string{Seven, Seven, Cherry}, wantMultiplier: 10, wantMatched: 2},
		{line: [3]string{Diamond, Diamond, Diamond}, wantMultiplier: 30, wantMatched: 3},
		{line: [3]string{Grape, Grape, Seven}, wantMultiplier: 2, wantMatched: 2},
		{line: [3]string{Cherry, Cherry, Cherry}, wantMultiplier: 10, wantMatched: 3},
		{line: [3]string{Cherry, Cherry, Lemon}, wantMultiplier: 2, wantMatched: 2},
		{line: [3]string{Cherry, Lemon, Cherry}, wantMultiplier: 1, wantMatched: 1},
		{line: [3]string{Lemon, Cherry, Cherry}, wantMultiplier: 0, wantMatched: 0},
		{line: [3]string{Bell, Lemon, Lemon}, wantMultiplier: 0, wantMatched: 0},
	}
	for _, c := range cases {
		result := StopAt(findStops(t, c.line))
		assert.Equal(t, c.line, result.Line)
		assert.Equal(t, c.wantMultiplier, result.Multiplier, c.line)
		assert.Equal(t, c.wantMatched, result.Matched, c.line)
	}
}

// findStops 在每个转轮上找到给定符号的第一个停点。
func findStops(t *testing.T, line [3]string) [3]int {
	t.Helper()
	var stops [3]int
	for reel, symbol := range line {
		stops[reel] = slices.Index(Strips[reel], symbol)
		require.GreaterOrEqual(t, stops[reel], 0, "reel %d has no %s", reel, symbol)
	}
	return stops
}
