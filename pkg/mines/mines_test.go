package mines

import (
	"math/big"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 兑现派彩 = floor(下注 × RTP × C(25,k) / (100 × C(25−m,k)))，整数运算，向下取整，最多下注的 1000 倍。
func TestPayout(t *testing.T) {
	cases := []struct {
		name  string
		bet   int
		rtp   int
		mines int
		safe  int
		want  int
	}{
		{name: "1 个雷翻 1 格正好回本", bet: 100, rtp: 96, mines: 1, safe: 1, want: 100},
		{name: "24 个雷翻 1 格是 24 倍", bet: 100, rtp: 96, mines: 24, safe: 1, want: 2400},
		{name: "向下取整：7×96×300/19000 = 10.61", bet: 7, rtp: 96, mines: 5, safe: 2, want: 10},
		{name: "向下取整：10×96×25/2200 = 10.91", bet: 10, rtp: 96, mines: 3, safe: 1, want: 10},
		{name: "99% 返还率：99×53130/128700 = 40.87", bet: 1, rtp: 99, mines: 12, safe: 5, want: 40},
		{name: "50% 返还率", bet: 2, rtp: 50, mines: 24, safe: 1, want: 25},
		{name: "封顶：按公式 2208 倍", bet: 5, rtp: 96, mines: 22, safe: 3, want: 5000},
		{name: "封顶：按公式 1044 倍", bet: 1, rtp: 96, mines: 10, safe: 10, want: 1000},
		{name: "安全格没有那么多", bet: 10, rtp: 96, mines: 24, safe: 2, want: 0},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Payout(c.bet, c.rtp, c.mines, c.safe), c.name)
	}
}

// 前 k 格都安全的概率是 C(25−m,k)/C(25,k)，乘上兑现的倍数正好是 RTP/100。逐一检查每个雷数、每个"翻开 k 格就兑现"的玩法：
// 向下取整、封顶之后期望返还不超过 RTP；没封顶时比 RTP 少不到 1 个游戏币(除以下注)，派彩确实按公式算出。
// 任何玩法都是这些玩法的混合，所以挖宝的理论返还率不超过 RTP。
func TestExpectedReturnNeverExceedsRtp(t *testing.T) {
	bets := []int{1, 2, 3, 7, 10, 99, 100, 999, 1000}
	for _, rtp := range []int{50, 96, 99} {
		limit := big.NewRat(int64(rtp), 100)
		for mines := MinMines; mines <= MaxMines; mines++ {
			for safe := 1; safe <= Cells-mines; safe++ {
				survive := new(big.Rat).SetFrac(
					new(big.Int).Binomial(int64(Cells-mines), int64(safe)),
					new(big.Int).Binomial(Cells, int64(safe)),
				)
				for _, bet := range bets {
					payout := Payout(bet, rtp, mines, safe)
					expected := new(big.Rat).Mul(survive, big.NewRat(int64(payout), int64(bet)))
					require.LessOrEqual(t, expected.Cmp(limit), 0, "rtp %d mines %d safe %d bet %d", rtp, mines, safe, bet)
					if payout == bet*MaxMultiplier {
						continue
					}
					floorLoss := new(big.Rat).Mul(survive, big.NewRat(1, int64(bet)))
					lowest := new(big.Rat).Sub(limit, floorLoss)
					require.Positive(t, expected.Cmp(lowest), "rtp %d mines %d safe %d bet %d", rtp, mines, safe, bet)
				}
			}
		}
	}
}

// 布雷得到恰好 mines 个不同的格子，都在盘面之内并按升序排列；雷数不在 1 到 24 之间时拒绝。
func TestPlace(t *testing.T) {
	for mines := MinMines; mines <= MaxMines; mines++ {
		layout, err := Place(mines)
		require.NoError(t, err)
		require.Len(t, layout, mines)
		assert.True(t, slices.IsSorted(layout), "mines %d", mines)
		assert.Len(t, slices.Compact(slices.Clone(layout)), mines, "mines %d 的格子不重复", mines)
		assert.GreaterOrEqual(t, layout[0], 0)
		assert.Less(t, layout[len(layout)-1], Cells)
	}
	for _, mines := range []int{0, 25, -1} {
		_, err := Place(mines)
		assert.Error(t, err, "mines %d", mines)
	}
}

// 翻开安全格记下顺序；翻到雷这一局就输了，之后什么都不能翻；越界或已经翻开的格子不能翻。
func TestPick(t *testing.T) {
	round := NewRound(96, []int{0, 1, 2})
	mine, err := round.Pick(5)
	require.NoError(t, err)
	assert.False(t, mine)
	for _, index := range []int{5, -1, Cells} {
		_, err := round.Pick(index)
		assert.ErrorIs(t, err, ErrIllegalPick, "index %d", index)
	}
	mine, err = round.Pick(12)
	require.NoError(t, err)
	assert.False(t, mine)
	assert.Equal(t, []int{5, 12}, round.Revealed)
	assert.False(t, round.Lost())

	mine, err = round.Pick(1)
	require.NoError(t, err)
	assert.True(t, mine)
	assert.True(t, round.Lost())
	assert.Equal(t, 1, round.Exploded)
	_, err = round.Pick(6)
	assert.ErrorIs(t, err, ErrIllegalPick, "踩雷之后不能再翻")
	assert.Equal(t, []int{5, 12}, round.Revealed)
}

// 翻开所有安全格、或派彩到了下注的 1000 倍时自动兑现。
func TestAutoCashout(t *testing.T) {
	// 24 个雷只有第 7 格是安全格：翻开它就翻完了。
	layout := make([]int, 0, MaxMines)
	for cell := range Cells {
		if cell != 7 {
			layout = append(layout, cell)
		}
	}
	cleared := NewRound(96, layout)
	assert.False(t, cleared.AutoCashout(10))
	_, err := cleared.Pick(7)
	require.NoError(t, err)
	assert.True(t, cleared.Cleared())
	assert.True(t, cleared.AutoCashout(10))
	assert.Equal(t, 240, cleared.Cashout(10))
	_, err = cleared.Pick(8)
	assert.ErrorIs(t, err, ErrIllegalPick, "翻完之后不能再翻")

	// 20 个雷(第 0 到 19 格)：翻开 3 格是 220.8 倍，第 4 格按公式 2428.8 倍，还剩一个安全格就先到了封顶。
	capped := NewRound(96, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19})
	for _, cell := range []int{20, 21, 22} {
		_, err := capped.Pick(cell)
		require.NoError(t, err)
	}
	assert.Equal(t, 1104, capped.Cashout(5))
	assert.Equal(t, 5000, capped.NextCashout(5))
	assert.False(t, capped.AutoCashout(5))
	_, err = capped.Pick(23)
	require.NoError(t, err)
	assert.False(t, capped.Cleared())
	assert.True(t, capped.AutoCashout(5))
	assert.Equal(t, 5000, capped.Cashout(5))
}
