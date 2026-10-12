package plinko

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 按落点权重加总：三档每投 1 个游戏币平均派彩 0.8 个，理论返还率正好 80%。这个数写在玩家看到的规则里，改倍率表时要同步更新这里。
func TestPayback(t *testing.T) {
	paid, cost := Payback()
	assert.Equal(t, 32768, paid)
	assert.Equal(t, 40960, cost)
}

// 1 币档也按倍率派彩，落进中间一格只派回 0.2 个，靠边一格派 13 个；派彩按记账单位算，零头舍去。
func TestPayoutAppliesTheMultiplierToAnyStake(t *testing.T) {
	cases := []struct {
		stake int
		slot  int
		want  int
	}{
		{stake: 500000, slot: 6, want: 100000},
		{stake: 500000, slot: 3, want: 900000},
		{stake: 500000, slot: 0, want: 6500000},
		{stake: 5000000, slot: 5, want: 2500000},
		{stake: 7, slot: 6, want: 1},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Payout(c.stake, c.slot), "stake %d slot %d", c.stake, c.slot)
	}
}

// 穷举每一排向左或向右的全部 2^Rows 条路径：落进每一格的路径数正好是它的权重，页面按权重公示的概率就是实际落点的概率。
func TestWeightsCountEveryPath(t *testing.T) {
	var hits [Slots]int
	for bits := 0; bits < 1<<Rows; bits++ {
		var path [Rows]int
		for row := range path {
			path[row] = bits >> row & 1
		}
		hits[SlotOf(path)]++
	}
	assert.Equal(t, Weights, hits)
}
