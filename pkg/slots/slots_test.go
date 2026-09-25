package slots

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 穷举三个转轮的全部 27000 种停点组合：均衡阶段平均每下注 1 个游戏币派彩正好 1 个，长期不赚不赔；
// 常规阶段只派彩 21720/27000 ≈ 0.804 个，玩家长期是输的。两个返还率都写在玩家看到的规则里，
// 改转轮或赔付表时要同步更新这里，常规阶段必须明显低于 1。
func TestPaybackByStage(t *testing.T) {
	paid, combos := Payback(StageEven)
	assert.Equal(t, 27000, combos)
	assert.Equal(t, 27000, paid)

	paid, combos = Payback(StageHouse)
	assert.Equal(t, 27000, combos)
	assert.Equal(t, 21720, paid)
}

// 常规阶段的每一种组合都不比均衡阶段派得多：玩家进入常规阶段只会更吃亏，不会有哪种组合反而赔得更多。
func TestHouseStageNeverPaysMore(t *testing.T) {
	require.Len(t, Paytables[StageHouse], len(Paytables[StageEven]))
	for i, even := range Paytables[StageEven] {
		house := Paytables[StageHouse][i]
		require.Equal(t, even.Symbol, house.Symbol)
		assert.LessOrEqual(t, house.Three, even.Three, even.Symbol)
		assert.LessOrEqual(t, house.Two, even.Two, even.Symbol)
		assert.LessOrEqual(t, house.One, even.One, even.Symbol)
	}
}

// 派彩只看中线上从最左边起连续相同的符号：三个相同、只有前两个相同各有倍数，只有第一个是樱桃时
// 均衡阶段原数退回、常规阶段不退；不从第一个转轮开始的组合不算。
func TestStopAtPaysRunsFromTheLeft(t *testing.T) {
	cases := []struct {
		line      [3]string
		stage     Stage
		wantMulti int
		wantMatch int
	}{
		{line: [3]string{Seven, Seven, Seven}, stage: StageHouse, wantMulti: 50, wantMatch: 3},
		{line: [3]string{Seven, Seven, Cherry}, stage: StageHouse, wantMulti: 10, wantMatch: 2},
		{line: [3]string{Diamond, Diamond, Diamond}, stage: StageEven, wantMulti: 28, wantMatch: 3},
		{line: [3]string{Grape, Grape, Seven}, stage: StageEven, wantMulti: 2, wantMatch: 2},
		{line: [3]string{Cherry, Cherry, Cherry}, stage: StageHouse, wantMulti: 10, wantMatch: 3},
		{line: [3]string{Cherry, Cherry, Lemon}, stage: StageHouse, wantMulti: 3, wantMatch: 2},
		{line: [3]string{Cherry, Lemon, Cherry}, stage: StageEven, wantMulti: 1, wantMatch: 1},
		{line: [3]string{Cherry, Lemon, Cherry}, stage: StageHouse, wantMulti: 0, wantMatch: 0},
		{line: [3]string{Lemon, Cherry, Cherry}, stage: StageEven, wantMulti: 0, wantMatch: 0},
		{line: [3]string{Bell, Lemon, Lemon}, stage: StageEven, wantMulti: 0, wantMatch: 0},
	}
	for _, c := range cases {
		result := StopAt(findStops(t, c.line), c.stage)
		assert.Equal(t, c.line, result.Line)
		assert.Equal(t, c.wantMulti, result.Multiplier, "%v stage %d", c.line, c.stage)
		assert.Equal(t, c.wantMatch, result.Matched, "%v stage %d", c.line, c.stage)
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
