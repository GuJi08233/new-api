package pipes

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每一盘都能按 Par 指出的最少转法转回生成时的方向并过关；开局不是已解；同一种子同一盘。
func TestNewIsDeterministicScrambledAndSolvable(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		size := Levels[difficulty].Size
		for seed := uint64(1); seed <= 40; seed++ {
			game, err := New(difficulty, seed)
			require.NoError(t, err)
			again, err := New(difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, game.Tiles, again.Tiles)
			require.Len(t, game.Tiles, size*size)
			assert.Equal(t, size*size/2, game.Source)
			assert.False(t, game.Solved(), "difficulty=%d seed=%d", difficulty, seed)

			solution := spanningTree(size, rand.New(rand.NewPCG(seed, 0x9195)))
			turns := 0
			for cell, tile := range game.Tiles {
				require.NotZero(t, tile)
				for k := 0; k < 4 && game.Tiles[cell] != solution[cell]; k++ {
					require.NoError(t, game.Rotate(cell, true))
				}
				require.Equal(t, solution[cell], game.Tiles[cell], "cell %d cannot reach its solved shape", cell)
				turns += turnsBack(solution[cell], tile)
			}
			assert.Equal(t, game.Par, turns)
			assert.True(t, game.Solved(), "difficulty=%d seed=%d", difficulty, seed)
		}
	}
}

func TestRulesOnSmallBoard(t *testing.T) {
	_, err := New(0, 1)
	assert.ErrorIs(t, err, ErrMove)
	_, err = New(4, 1)
	assert.ErrorIs(t, err, ErrMove)

	// 2×2：水源在格 2(左下)，树为 2-0、2-3、0-1。
	game := &Game{Size: 2, Source: 2, Tiles: []int{Down | Right, Left, Up | Right, Left}}
	assert.True(t, game.Solved())
	assert.Equal(t, 0, game.Leaks())

	require.NoError(t, game.Rotate(1, true))
	assert.Equal(t, Up, game.Tiles[1])
	assert.False(t, game.Solved())
	assert.Equal(t, 2, game.Leaks(), "the turned pipe leaks off the board and its neighbour leaks into it")
	assert.Equal(t, []bool{true, false, true, true}, game.Lit())

	require.NoError(t, game.Rotate(1, false))
	assert.True(t, game.Solved())

	assert.ErrorIs(t, game.Rotate(-1, true), ErrMove)
	assert.ErrorIs(t, game.Rotate(4, true), ErrMove)
}

// 所有管口都对上但有一块和水源断开时不算过关。
func TestDisconnectedLoopIsNotSolved(t *testing.T) {
	game := &Game{Size: 3, Source: 4, Tiles: []int{
		Right | Down, Left | Down, Down,
		Up | Right, Left | Up, Up | Down,
		Right, Left | Right, Left | Up,
	}}
	assert.Equal(t, 0, game.Leaks())
	assert.False(t, game.Solved())
}
