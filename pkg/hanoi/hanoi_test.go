package hanoi

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestThreeDisksSolveInSevenLegalMoves(t *testing.T) {
	g, err := New(1)
	require.NoError(t, err)
	assert.Equal(t, 7, g.MinimumMoves())
	for _, move := range [][2]int{{0, 2}, {0, 1}, {2, 1}, {0, 2}, {1, 0}, {1, 2}, {0, 2}} {
		require.NoError(t, g.Move(move[0], move[1]))
	}
	assert.True(t, g.Solved())
	assert.Equal(t, []int{3, 2, 1}, g.Towers[2])
	assert.Equal(t, 7, g.Moves)
	assert.ErrorIs(t, g.Move(2, 0), ErrMove)
}

func TestIllegalMovesDoNotChangeTowers(t *testing.T) {
	g, err := New(2)
	require.NoError(t, err)
	for _, move := range [][2]int{{-1, 0}, {0, 3}, {0, 0}, {1, 2}} {
		assert.ErrorIs(t, g.Move(move[0], move[1]), ErrMove)
	}
	require.NoError(t, g.Move(0, 1))
	assert.ErrorIs(t, g.Move(0, 1), ErrMove, "不能把大盘放在小盘上")
	assert.Equal(t, []int{4, 3, 2}, g.Towers[0])
	assert.Equal(t, []int{1}, g.Towers[1])
	assert.Equal(t, 1, g.Moves)
	g.Moves = MaxMoves
	assert.ErrorIs(t, g.Move(1, 2), ErrMove)
	_, err = New(0)
	assert.ErrorIs(t, err, ErrMove)
}
