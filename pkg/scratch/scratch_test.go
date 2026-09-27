package scratch

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按权重加总：平均每花 1 个游戏币派彩 8000/10000 = 0.8 个，中奖(含 1 倍回本)的概率 3153/10000 ≈ 31.5%，最高奖 100 倍。
// 这些数写在玩家看到的规则里，改奖品表时要同步更新这里；无论怎么改，返还率都要在 78%–82% 之间、中奖概率在 25%–40% 之间、
// 最高奖不超过票价的 100 倍。
func TestPayback(t *testing.T) {
	paid, total := Payback()
	assert.Equal(t, 10000, total)
	assert.Equal(t, 8000, paid)
	assert.GreaterOrEqual(t, paid*100, 78*total)
	assert.LessOrEqual(t, paid*100, 82*total)

	wins, previous := 0, 0
	for _, prize := range Prizes {
		require.Positive(t, prize.Weight, "multiple %d", prize.Multiple)
		require.Greater(t, prize.Multiple, previous, "奖品按倍数从小到大排列、不重复")
		wins += prize.Weight
		previous = prize.Multiple
	}
	assert.Equal(t, 3153, wins)
	assert.GreaterOrEqual(t, wins*100, 25*total)
	assert.LessOrEqual(t, wins*100, 40*total)
	assert.Equal(t, 1, Prizes[0].Multiple, "最小的奖是回本")
	assert.Equal(t, 100, previous, "最高奖 100 倍")
	// 不中奖的票要从其他金额里凑出 9 格、每个金额最多 2 次，奖品表至少要有 5 个金额。
	assert.GreaterOrEqual(t, len(Prizes), 5)
}

// 权重合计上的每个点都落在唯一一行奖品或不中奖里，每行分到的点数正好是它的权重：页面按权重公示的概率就是实际抽中的概率。
func TestPrizeAtMatchesWeights(t *testing.T) {
	hits := map[int]int{}
	for point := 0; point < TotalWeight; point++ {
		hits[PrizeAt(point)]++
	}
	for _, prize := range Prizes {
		assert.Equal(t, prize.Weight, hits[prize.Multiple], "multiple %d", prize.Multiple)
	}
	assert.Equal(t, TotalWeight-3153, hits[0], "不中奖")
	assert.Len(t, hits, len(Prizes)+1)
}

// 每种奖品(含不中奖)用固定的随机源各生成一批票面：所有格子都是奖品表里的金额；中奖金额恰好出现 3 次、其他金额都不超过 2 次，
// 按"3 格相同即中"兑奖正好兑出这张票的奖品；不中奖的票没有任何金额出现 3 次。中奖的 3 格会出现在 9 格里的每个位置，
// 玩家没法只刮固定的几格。奖品表里没有的倍数生成不了票面。
func TestNewTicketMatchesPrize(t *testing.T) {
	source := rand.New(rand.NewPCG(20260928, 9))
	random := func(n int) (int, error) { return source.IntN(n), nil }
	inTable := map[int]bool{}
	multiples := []int{0}
	for _, prize := range Prizes {
		inTable[prize.Multiple] = true
		multiples = append(multiples, prize.Multiple)
	}

	for _, multiple := range multiples {
		var winningAt [CellCount]bool
		for i := 0; i < 60; i++ {
			ticket, err := NewTicket(multiple, random)
			require.NoError(t, err)
			require.Equal(t, multiple, ticket.Multiple)
			counts := map[int]int{}
			for cell, value := range ticket.Cells {
				require.True(t, inTable[value], "multiple %d ticket %v", multiple, ticket.Cells)
				counts[value]++
				if value == multiple {
					winningAt[cell] = true
				}
			}
			prize := 0
			for value, count := range counts {
				require.LessOrEqual(t, count, 3, "multiple %d ticket %v", multiple, ticket.Cells)
				if count == 3 {
					require.Zero(t, prize, "一张票最多中一个金额: %v", ticket.Cells)
					prize = value
				}
			}
			require.Equal(t, multiple, prize, "ticket %v", ticket.Cells)
		}
		if multiple > 0 {
			assert.Equal(t, [CellCount]bool{true, true, true, true, true, true, true, true, true}, winningAt, "multiple %d", multiple)
		}
	}

	_, err := NewTicket(7, random)
	assert.ErrorIs(t, err, ErrUnknownPrize)
}
