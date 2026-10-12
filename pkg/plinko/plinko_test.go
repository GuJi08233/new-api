package plinko

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按落点权重加总：倍率两档每投 1 个游戏币平均派彩 0.8 个；1 币档每颗球先扣 2 个、平均派彩 7370/4096 个，
// 平均每颗球输 822/4096 ≈ 0.2 个。这些数写在玩家看到的规则里，改派彩表时要同步更新这里。
func TestPayback(t *testing.T) {
	cases := []struct {
		stake    int
		wantPaid int
		wantCost int
	}{
		{stake: 1, wantPaid: 7370, wantCost: 8192},
		{stake: 10, wantPaid: 32768, wantCost: 40960},
		{stake: 100, wantPaid: 327680, wantCost: 409600},
	}
	for _, c := range cases {
		tier, ok := TierOf(c.stake)
		require.True(t, ok, "stake %d", c.stake)
		paid, cost := Payback(tier)
		assert.Equal(t, c.wantPaid, paid, "stake %d", c.stake)
		assert.Equal(t, c.wantCost, cost, "stake %d", c.stake)
	}
	_, ok := TierOf(5)
	assert.False(t, ok, "只有 1、10、100 三档")
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

// 派彩从不为负，1 币档最低的一格派彩 0：先扣的 2 个就是一颗球最多扣掉的数量，页面按它提示余额至少要有几个。
func TestPayoutsNeverTakeMoreThanCost(t *testing.T) {
	for _, tier := range Tiers {
		lowest := tier.Payouts[0]
		for _, payout := range tier.Payouts {
			lowest = min(lowest, payout)
		}
		require.GreaterOrEqual(t, lowest, 0, "stake %d", tier.Stake)
		if tier.Delta {
			assert.Equal(t, 0, lowest, "stake %d", tier.Stake)
		} else {
			assert.Equal(t, tier.Stake, tier.Cost, "倍率档扣的就是挡位")
		}
	}
}
