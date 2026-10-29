package nonogram

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunsDescribeALine(t *testing.T) {
	assert.Equal(t, []int{}, runs([]bool{false, false}))
	assert.Equal(t, []int{2, 1}, runs([]bool{true, true, false, true}))
	assert.Equal(t, []int{3}, runs([]bool{false, true, true, true}))
}

// 每一档按种子出的题都能只靠逐行逐列推理解出，照推出的答案涂完就过关；同一个种子出同一道题。
func TestGeneratedPuzzlesAreLineSolvable(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		for seed := uint64(1); seed <= 5; seed++ {
			g, err := New(difficulty, seed)
			require.NoError(t, err)
			again, err := New(difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, g.Puzzle, again.Puzzle)
			assert.Equal(t, Sizes[difficulty], g.Size)
			solution, ok := lineSolve(g.Puzzle)
			require.True(t, ok)
			assert.False(t, g.Solved() && len(g.Rows[0])+len(g.Cols[0]) > 0, "空盘不应当已经过关")
			for cell, on := range solution {
				if on {
					require.NoError(t, g.Toggle(cell))
				}
			}
			assert.True(t, g.Solved(), "difficulty %d seed %d", difficulty, seed)
			assert.ErrorIs(t, g.Toggle(0), ErrMove, "过关后不能再涂")
		}
	}
	_, err := New(4, 1)
	assert.ErrorIs(t, err, ErrDifficulty)
}

// 只要每行每列都和提示相符就过关，不要求和出题时的图一样。
func TestAnyGridMatchingTheCluesWins(t *testing.T) {
	// 两种涂法的提示一样：对角线。
	g := &Game{Puzzle: Puzzle{Size: 2, Rows: [][]int{{1}, {1}}, Cols: [][]int{{1}, {1}}}, Filled: make([]bool, 4)}
	require.NoError(t, g.Toggle(1))
	require.NoError(t, g.Toggle(2))
	assert.True(t, g.Solved())
	assert.False(t, LineSolvable(g.Puzzle), "对角线有两种解，逐行推理推不出来")
	g = &Game{Puzzle: g.Puzzle, Filled: make([]bool, 4)}
	assert.ErrorIs(t, g.Toggle(4), ErrMove)
	assert.ErrorIs(t, g.Toggle(-1), ErrMove)
}
