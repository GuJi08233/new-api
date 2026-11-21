package flood

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeterministicAndSolvable(t *testing.T) {
	for diff := 1; diff <= 3; diff++ {
		level := Levels[diff]
		for seed := uint64(1); seed <= 30; seed++ {
			g1, err := New(diff, seed)
			require.NoError(t, err)
			g2, err := New(diff, seed)
			require.NoError(t, err)
			assert.Equal(t, g1.Board, g2.Board)
			assert.Equal(t, level.Size*level.Size, len(g1.Board))
			assert.False(t, g1.Solved())

			steps := greedySolve(g1.Board, g1.Size, g1.Colors, level.MaxMoves)
			assert.LessOrEqual(t, steps, level.MaxMoves, "diff=%d seed=%d should be solvable within limit", diff, seed)
		}
	}
}

func TestFloodRulesAndTermination(t *testing.T) {
	_, err := New(0, 1)
	assert.ErrorIs(t, err, ErrMove)
	_, err = New(4, 1)
	assert.ErrorIs(t, err, ErrMove)

	g := &Game{
		Difficulty: 1,
		Size:       2,
		Colors:     3,
		MaxMoves:   2,
		Board:      []int{0, 1, 1, 2},
	}
	assert.Equal(t, 1, g.Captured())
	assert.ErrorIs(t, g.Flood(0), ErrMove)
	assert.ErrorIs(t, g.Flood(-1), ErrMove)
	assert.ErrorIs(t, g.Flood(3), ErrMove)

	require.NoError(t, g.Flood(1))
	assert.Equal(t, 3, g.Captured())
	assert.Equal(t, 1, g.Moves)
	assert.False(t, g.Solved())
	assert.False(t, g.Over())

	require.NoError(t, g.Flood(2))
	assert.Equal(t, 4, g.Captured())
	assert.Equal(t, 2, g.Moves)
	assert.True(t, g.Solved())
	assert.True(t, g.Over())
	assert.ErrorIs(t, g.Flood(0), ErrMove)
}

func TestFloodOutOfMovesLoss(t *testing.T) {
	g := &Game{
		Difficulty: 1,
		Size:       2,
		Colors:     4,
		MaxMoves:   1,
		Board:      []int{0, 1, 2, 3},
	}
	require.NoError(t, g.Flood(1))
	assert.False(t, g.Solved())
	assert.True(t, g.Over())
	assert.Equal(t, 0, g.MovesLeft())
	assert.ErrorIs(t, g.Flood(2), ErrMove)
}
