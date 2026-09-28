package sokoban

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestDailyPuzzlesAreStableUnsolvedAndSolvable(t *testing.T) {
	for _, day := range []string{"2026-09-28", "2026-09-29", "2027-01-01", "2028-02-29"} {
		game, err := New(day)
		require.NoError(t, err)
		require.False(t, game.Solved())
		again, err := New(day)
		require.NoError(t, err)
		assert.Equal(t, game, again)
		solved, err := game.Replay(game.solution)
		require.NoError(t, err, day)
		assert.True(t, solved.Solved(), day)
		assert.False(t, game.Solved(), "验证走法不能改变开局地图")
	}
	_, err := New("not-a-date")
	assert.Error(t, err)
}

func TestReplayRejectsBlockedPushesAndInvalidRecords(t *testing.T) {
	game := &Game{Width: 5, Height: 4, Walls: []int{0, 1, 2, 3, 4, 5, 9, 10, 14, 15, 16, 17, 18, 19}, Player: 6, Boxes: []int{7}, Goals: []int{8}}
	won, err := game.Replay("R")
	require.NoError(t, err)
	assert.True(t, won.Solved())
	assert.Equal(t, 1, won.Pushes)
	assert.Equal(t, []int{7}, game.Boxes)
	for _, moves := range []string{"", "U", "RR", "X", strings.Repeat("LR", MaxMoves)} {
		_, err := game.Replay(moves)
		assert.ErrorIs(t, err, ErrMove, moves[:min(len(moves), 10)])
	}
	game.Boxes = []int{7, 8}
	_, err = game.Replay("R")
	assert.ErrorIs(t, err, ErrMove, "不能一次推动两只箱子")
}
