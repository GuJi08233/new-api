package watersort

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同一种子总是同一盘；每种颜色正好 Capacity 层，空管数固定，没有一开始就排好的试管。
func TestNewIsDeterministicAndBalanced(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		for seed := uint64(1); seed <= 20; seed++ {
			a, err := New(difficulty, seed)
			require.NoError(t, err)
			b, err := New(difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, a.Tubes, b.Tubes)
			require.Len(t, a.Tubes, Colors[difficulty]+EmptyTubes)
			counts := map[int8]int{}
			empty := 0
			for _, tube := range a.Tubes {
				if len(tube) == 0 {
					empty++
				}
				for _, layer := range tube {
					counts[layer]++
				}
			}
			assert.Equal(t, EmptyTubes, empty)
			require.Len(t, counts, Colors[difficulty])
			for _, n := range counts {
				assert.Equal(t, Capacity, n)
			}
			assert.False(t, a.anyTubeDone())
		}
	}
	_, err := New(4, 1)
	assert.ErrorIs(t, err, ErrMove)
}

// 倒水只倒顶上连续的同色、只倒得下剩下的空位；顶色不同、目标已满、倒给自己都不行。
func TestPourRules(t *testing.T) {
	g := &Game{Difficulty: 1, Tubes: [][]int8{{1, 2, 2}, {3, 2}, {}, {1, 1, 1, 3}}}
	_, err := g.Pour(0, 3)
	assert.ErrorIs(t, err, ErrMove, "顶色不同")
	_, err = g.Pour(0, 0)
	assert.ErrorIs(t, err, ErrMove)
	_, err = g.Pour(2, 0)
	assert.ErrorIs(t, err, ErrMove, "空管倒不出")
	amount, err := g.Pour(0, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, amount)
	assert.Equal(t, [][]int8{{1}, {3, 2, 2, 2}, {}, {1, 1, 1, 3}}, g.Tubes)
	_, err = g.Pour(0, 1)
	assert.ErrorIs(t, err, ErrMove, "目标已满")
	assert.Equal(t, 1, g.Moves)
}

// 排好之后算过关，过关后不能再倒。
func TestSolvedStopsTheGame(t *testing.T) {
	g := &Game{Difficulty: 1, Tubes: [][]int8{{1, 1, 1}, {2, 2, 2, 2}, {1}}}
	assert.False(t, g.Solved())
	_, err := g.Pour(2, 0)
	require.NoError(t, err)
	assert.True(t, g.Solved())
	_, err = g.Pour(0, 2)
	assert.ErrorIs(t, err, ErrMove)
}

// 生成的盘面都能按求解器找到的解法排好(求解器只在找到解时放行)。
func TestGeneratedPuzzlesAreSolvable(t *testing.T) {
	for seed := uint64(100); seed < 110; seed++ {
		g, err := New(3, seed)
		require.NoError(t, err)
		assert.True(t, solvable(g.Tubes))
	}
}
