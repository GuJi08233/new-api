package connect4

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seeded(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x5eed))
}

// 普通与困难难度能一步赢就赢；对手下一步能赢时必须堵住；不会投在对手赢棋格子的正下方把这一格让给对手。
// 每个局面换几个随机种子，不是碰巧挑中。节点上限小到每次搜索都要作废、一直退到只搜一步时也一样：作废的搜索算出的分数
// 不会拿来选棋。
func TestBestMoveTakesWinsAndStopsLosses(t *testing.T) {
	cases := map[string]struct {
		moves   []int
		allowed []int
	}{
		// 先手底行 0、1、2 列连着三子，后手在它们上面也连着三子：先手投第 3 列直接赢，不必去堵。
		"take the win": {moves: []int{0, 0, 1, 1, 2, 2}, allowed: []int{3}},
		// 先手底行三子，后手只能堵第 3 列。
		"block the loss": {moves: []int{0, 6, 1, 6, 2}, allowed: []int{3}},
		// 先手第二行 0、1、2 列三子，第 3 列还空着：后手投第 3 列会让先手接着在它上面连成 4 子。
		"do not play under the threat": {moves: []int{1, 0, 0, 2, 1, 6, 2, 6, 5}, allowed: []int{0, 1, 2, 4, 5, 6}},
	}
	defaultLimit := nodeLimit
	t.Cleanup(func() { nodeLimit = defaultLimit })
	for _, limit := range []int{defaultLimit, 1} {
		nodeLimit = limit
		for name, c := range cases {
			for _, level := range []int{2, 3} {
				for seed := uint64(1); seed <= 3; seed++ {
					g, err := Replay(c.moves)
					require.NoError(t, err)
					require.False(t, g.Over())
					col, err := BestMove(g, level, seeded(seed))
					require.NoError(t, err)
					assert.Contains(t, c.allowed, col, "%s level %d seed %d node limit %d", name, level, seed, limit)
				}
			}
		}
	}
}

// 困难难度执先、执后都能赢简单难度：几个固定种子下的整局。
func TestHardBeatsEasy(t *testing.T) {
	for seed := uint64(1); seed <= 3; seed++ {
		for _, hard := range []Disc{First, Second} {
			rng := seeded(seed)
			g := New()
			for !g.Over() {
				level := 1
				if g.Turn() == hard {
					level = 3
				}
				col, err := BestMove(g, level, rng)
				require.NoError(t, err)
				require.NoError(t, g.Play(col))
			}
			assert.Equal(t, hard, g.Winner(), "seed %d, hard plays %d, moves %v", seed, hard, g.Moves())
		}
	}
}

// 难度只能是 1 到 3，结束的对局没有下一步。
func TestBestMoveRejectsBadLevelAndFinishedGame(t *testing.T) {
	_, err := BestMove(New(), 4, seeded(1))
	assert.ErrorIs(t, err, ErrInvalidLevel)
	_, err = BestMove(New(), 0, seeded(1))
	assert.ErrorIs(t, err, ErrInvalidLevel)
	won, err := Replay([]int{0, 1, 0, 1, 0, 1, 0})
	require.NoError(t, err)
	_, err = BestMove(won, 3, seeded(1))
	assert.ErrorIs(t, err, ErrIllegalMove)
}
