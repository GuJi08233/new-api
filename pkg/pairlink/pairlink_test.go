package pairlink

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPathsAllowOnlyTwoTurnsThroughEmptyCells(t *testing.T) {
	for _, test := range []struct {
		name                  string
		board                 []int
		first, second, points int
	}{
		{"straight", []int{1, 0, 1, 0, 0, 0, 0, 0, 0}, 0, 2, 2},
		{"one turn", []int{1, 0, 0, 0, 1, 0, 0, 0, 0}, 0, 4, 3},
		{"outside border", []int{1, 2, 1, 3, 4, 5, 6, 7, 8}, 0, 2, 4},
		{"blocked", []int{9, 1, 9, 9, 2, 9, 9, 1, 9}, 1, 7, 0},
		{"different symbols", []int{1, 2, 0, 0, 0, 0, 0, 0, 0}, 0, 1, 0},
		{"same cell", []int{1, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 0, 0},
		{"invalid position", []int{1, 0, 0, 0, 0, 0, 0, 0, 0}, -1, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			game := &Game{Level: Level{Rows: 3, Cols: 3}, Board: test.board}
			assert.Len(t, game.Path(test.first, test.second), test.points)
		})
	}
}

func TestDealsRemainPlayableUntilEveryPairIsRemoved(t *testing.T) {
	for _, difficulty := range []int{1, 2, 3} {
		game, err := New(difficulty, 20260928)
		require.NoError(t, err)
		pairs := len(game.Board) / 2
		for range pairs {
			hint := game.Hint()
			require.Len(t, hint, 2)
			path, _, err := game.Match(hint[0], hint[1])
			require.NoError(t, err)
			assert.NotEmpty(t, path)
		}
		assert.Equal(t, "won", game.Status)
		assert.Zero(t, game.Remaining)
		for _, tile := range game.Board {
			assert.Zero(t, tile)
		}
		_, _, err = game.Match(0, 1)
		assert.ErrorIs(t, err, ErrMove)
	}
	_, err := New(4, 0)
	assert.ErrorIs(t, err, ErrMove)
}
