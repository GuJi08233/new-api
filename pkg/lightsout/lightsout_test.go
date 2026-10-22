package lightsout

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMoveTogglesCrossWithoutWrappingRows(t *testing.T) {
	g := &Game{Size: 3, Board: []bool{true, true, false, true, false, false, false, false, false}}
	require.NoError(t, g.Move(0))
	assert.True(t, g.Solved())
	assert.Equal(t, 1, g.Moves)
	assert.ErrorIs(t, g.Move(0), ErrMove, "完成后不能追加操作")
}

// 对生成的盘面用 GF(2) 消元求解，验证生成器的可解性；独立于它的随机生成步骤。
func TestGeneratedBoardsCanBeSolved(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		g, err := New(difficulty, 42)
		require.NoError(t, err)
		require.False(t, g.Solved())
		n := len(g.Board)
		rows := make([]uint64, n)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				dx, dy := i%g.Size-j%g.Size, i/g.Size-j/g.Size
				if dx*dx+dy*dy <= 1 {
					rows[i] |= 1 << j
				}
			}
			if g.Board[i] {
				rows[i] |= 1 << n
			}
		}
		pivots := []int{}
		for col := 0; col < n; col++ {
			pivot := -1
			for r := len(pivots); r < n; r++ {
				if rows[r]&(1<<col) != 0 {
					pivot = r
					break
				}
			}
			if pivot < 0 {
				continue
			}
			r := len(pivots)
			rows[r], rows[pivot] = rows[pivot], rows[r]
			for other := 0; other < n; other++ {
				if other != r && rows[other]&(1<<col) != 0 {
					rows[other] ^= rows[r]
				}
			}
			pivots = append(pivots, col)
		}
		for _, row := range rows {
			require.NotEqual(t, uint64(1<<n), row, "盘面应可解")
		}
		for r, col := range pivots {
			if rows[r]&(1<<n) != 0 {
				require.NoError(t, g.Move(col))
			}
		}
		assert.True(t, g.Solved())
	}
}

func TestInvalidMovesLeavePositionUnchanged(t *testing.T) {
	g, err := New(1, 7)
	require.NoError(t, err)
	before := append([]bool(nil), g.Board...)
	for _, cell := range []int{-1, 9} {
		assert.ErrorIs(t, g.Move(cell), ErrMove)
	}
	assert.Equal(t, before, g.Board)
	g.Moves = MaxMoves
	assert.ErrorIs(t, g.Move(0), ErrMove)
	assert.Equal(t, before, g.Board)
	_, err = New(4, 0)
	assert.ErrorIs(t, err, ErrMove)
}
