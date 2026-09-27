package connect4

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 横、竖、两条斜线连成 4 子都算赢：先手在最后一步连成，获胜连线从最后一子开始列出这条线上的所有棋子，结束后不能再投子。
func TestPlayDetectsFourInEveryDirection(t *testing.T) {
	cases := map[string]struct {
		moves    []int
		wantLine []Cell
	}{
		"horizontal": {
			moves:    []int{0, 0, 1, 1, 2, 2, 3},
			wantLine: []Cell{{Col: 3, Row: 5}, {Col: 2, Row: 5}, {Col: 1, Row: 5}, {Col: 0, Row: 5}},
		},
		"vertical": {
			moves:    []int{4, 5, 4, 5, 4, 5, 4},
			wantLine: []Cell{{Col: 4, Row: 2}, {Col: 4, Row: 3}, {Col: 4, Row: 4}, {Col: 4, Row: 5}},
		},
		// 先手 (0,0)(1,1)(2,2)(3,3)，高从下数。
		"rising diagonal": {
			moves:    []int{0, 1, 1, 2, 2, 3, 2, 3, 3, 6, 3},
			wantLine: []Cell{{Col: 3, Row: 2}, {Col: 2, Row: 3}, {Col: 1, Row: 4}, {Col: 0, Row: 5}},
		},
		// 先手 (6,0)(5,1)(4,2)(3,3)。
		"falling diagonal": {
			moves:    []int{6, 5, 5, 4, 4, 3, 4, 3, 3, 0, 3},
			wantLine: []Cell{{Col: 3, Row: 2}, {Col: 4, Row: 3}, {Col: 5, Row: 4}, {Col: 6, Row: 5}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			g, err := Replay(c.moves)
			require.NoError(t, err)
			assert.True(t, g.Over())
			assert.Equal(t, First, g.Winner())
			assert.Equal(t, Empty, g.Turn())
			assert.ElementsMatch(t, c.wantLine, g.Line())
			assert.Equal(t, c.wantLine[0], g.Line()[0], "最后一子排在最前")
			assert.ErrorIs(t, g.Play(6), ErrIllegalMove, "结束后不能再投子")
		})
	}
}

// 棋子落到这一列最低的空位；Board 按行从上到下排列，Last 与 Line 的行号也从上面数起。
func TestPlayDropsToLowestEmptyCell(t *testing.T) {
	g, err := Replay([]int{3, 3, 2})
	require.NoError(t, err)
	board := g.Board()
	require.Len(t, board, Cells)
	assert.Equal(t, strings.Repeat(".", 4*Cols)+"...y..."+"..rr...", board)
	last, ok := g.Last()
	require.True(t, ok)
	assert.Equal(t, Cell{Col: 2, Row: 5}, last)
	assert.Equal(t, Second, g.Turn())
	assert.Empty(t, g.Line())
}

// 列号越界、投进已满的列、对局结束后投子都不合规则；重放时有一步不合规则整局都不接受。
func TestPlayRejectsIllegalColumns(t *testing.T) {
	g := New()
	assert.ErrorIs(t, g.Play(-1), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(Cols), ErrIllegalMove)
	for i := 0; i < Rows; i++ {
		require.NoError(t, g.Play(0))
	}
	assert.False(t, g.CanPlay(0))
	assert.ErrorIs(t, g.Play(0), ErrIllegalMove, "第 0 列已经满了")
	assert.Len(t, g.Moves(), Rows)

	_, err := Replay([]int{0, 0, 0, 0, 0, 0, 0})
	assert.ErrorIs(t, err, ErrIllegalMove)
	_, err = Replay([]int{0, 1, 0, 1, 0, 1, 0, 1})
	assert.ErrorIs(t, err, ErrIllegalMove, "先手已经连成 4 子，后手不能再走")
}

// 42 格下满、谁都没有连成 4 子是和棋。
func TestFullBoardWithoutFourIsDraw(t *testing.T) {
	moves := []int{1, 5, 4, 5, 5, 5, 6, 6, 0, 5, 1, 0, 6, 6, 2, 2, 6, 2, 1, 2, 0, 3, 2, 1, 1, 6, 0, 4, 0, 2, 1, 5, 3, 0, 3, 4, 4, 4, 3, 3, 3, 4}
	g, err := Replay(moves)
	require.NoError(t, err)
	assert.True(t, g.Over())
	assert.Equal(t, Empty, g.Winner())
	assert.Equal(t, Empty, g.Turn())
	assert.NotContains(t, g.Board(), ".")
}
