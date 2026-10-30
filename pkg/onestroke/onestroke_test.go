package onestroke

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同一种子总是同一盘，石头数在这一档的范围里，起点是空格，并且一定有一条走遍空格的路线。
func TestNewIsDeterministicAndSolvable(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		level := Levels[difficulty]
		for seed := uint64(1); seed <= 30; seed++ {
			a, err := New(difficulty, seed)
			require.NoError(t, err)
			b, err := New(difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, a.Stones, b.Stones)
			assert.Equal(t, a.Path, b.Path)
			stones := level.Size*level.Size - a.Open()
			assert.GreaterOrEqual(t, stones, level.MinStones)
			assert.LessOrEqual(t, stones, level.MaxStones)
			require.Len(t, a.Path, 1)
			assert.False(t, a.Stones[a.Path[0]])
			require.Equal(t, a.Path[0], a.route[0])
			for _, cell := range a.route[1:] {
				require.NoError(t, a.Step(cell))
			}
			assert.True(t, a.Solved())
		}
	}
}

// 只能走到末端相邻、没走过的空格；走不动时算卡住。
func TestStepRules(t *testing.T) {
	// 3×3，中间是石头，从左上角出发。
	g := &Game{Size: 3, Stones: []bool{false, false, false, false, true, false, false, false, false}, Path: []int{0}}
	assert.ErrorIs(t, g.Step(4), ErrMove, "石头")
	assert.ErrorIs(t, g.Step(2), ErrMove, "不相邻")
	assert.ErrorIs(t, g.Step(0), ErrMove, "走过")
	require.NoError(t, g.Step(1))
	require.NoError(t, g.Step(2))
	require.NoError(t, g.Step(5))
	assert.ErrorIs(t, g.Step(-1), ErrMove)
	for _, cell := range []int{8, 7, 6, 3} {
		require.NoError(t, g.Step(cell))
	}
	assert.True(t, g.Solved())
	assert.False(t, g.Stuck())

	stuck := &Game{Size: 3, Stones: make([]bool, 9), Path: []int{4, 1, 0, 3}}
	require.NoError(t, stuck.Step(6))
	require.NoError(t, stuck.Step(7))
	require.NoError(t, stuck.Step(8))
	require.NoError(t, stuck.Step(5))
	require.NoError(t, stuck.Step(2))
	assert.True(t, stuck.Solved())
	dead := &Game{Size: 3, Stones: make([]bool, 9), Path: []int{1, 0, 3, 4}}
	require.NoError(t, dead.Step(5))
	require.NoError(t, dead.Step(2))
	assert.True(t, dead.Stuck())
	assert.False(t, slices.Contains(dead.Path, 8))
}
