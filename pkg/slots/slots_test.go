package slots

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 穷举三个转轮的全部 27000 种停点组合：平均每下注 1 个游戏币只派彩 21720/27000 ≈ 0.804 个，玩家长期是输的。
// 返还率写在玩家看到的规则里，改转轮或赔付表时要同步更新这里，必须明显低于 1。
func TestPayback(t *testing.T) {
	paid, combos := Payback()
	assert.Equal(t, 27000, combos)
	assert.Equal(t, 21720, paid)
}

// 派彩只看中线上从最左边起连续相同的符号：三个相同、只有前两个相同各有倍数，只有第一个相同不派彩；
// 不从第一个转轮开始的组合不算。
func TestStopAtPaysRunsFromTheLeft(t *testing.T) {
	cases := []struct {
		line      [3]string
		wantMulti int
		wantMatch int
	}{
		{line: [3]string{Seven, Seven, Seven}, wantMulti: 50, wantMatch: 3},
		{line: [3]string{Seven, Seven, Cherry}, wantMulti: 10, wantMatch: 2},
		{line: [3]string{Diamond, Diamond, Diamond}, wantMulti: 28, wantMatch: 3},
		{line: [3]string{Grape, Grape, Seven}, wantMulti: 2, wantMatch: 2},
		{line: [3]string{Cherry, Cherry, Cherry}, wantMulti: 10, wantMatch: 3},
		{line: [3]string{Cherry, Cherry, Lemon}, wantMulti: 3, wantMatch: 2},
		{line: [3]string{Cherry, Lemon, Cherry}, wantMulti: 0, wantMatch: 0},
		{line: [3]string{Lemon, Cherry, Cherry}, wantMulti: 0, wantMatch: 0},
		{line: [3]string{Bell, Lemon, Lemon}, wantMulti: 0, wantMatch: 0},
	}
	for _, c := range cases {
		result := StopAt(findStops(t, c.line))
		assert.Equal(t, c.line, result.Line)
		assert.Equal(t, c.wantMulti, result.Multiplier, "%v", c.line)
		assert.Equal(t, c.wantMatch, result.Matched, "%v", c.line)
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
