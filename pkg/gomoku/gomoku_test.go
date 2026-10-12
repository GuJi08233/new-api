package gomoku

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// squares 把 (x, y) 坐标对换成格子编号，按落子顺序排列。
func squares(points ...[2]int) []int {
	moves := make([]int, 0, len(points))
	for _, p := range points {
		moves = append(moves, p[1]*Size+p[0])
	}
	return moves
}

// 横、竖、两条斜线连成五子都算赢；自由规则下连成六子(长连)也算赢。获胜连线从最后一子开始列出这条线上的所有棋子，
// 结束后不能再落子。
func TestPlayWinsWithFiveOrMore(t *testing.T) {
	cases := map[string]struct {
		moves      [][2]int
		wantWinner Stone
		wantLine   []Point
	}{
		"horizontal": {
			moves:      [][2]int{{3, 7}, {3, 9}, {4, 7}, {5, 9}, {5, 7}, {7, 9}, {6, 7}, {9, 9}, {7, 7}},
			wantWinner: Black,
			wantLine:   []Point{{X: 7, Y: 7}, {X: 6, Y: 7}, {X: 5, Y: 7}, {X: 4, Y: 7}, {X: 3, Y: 7}},
		},
		"vertical at the edge": {
			moves:      [][2]int{{0, 10}, {14, 0}, {0, 14}, {14, 2}, {0, 11}, {14, 4}, {0, 13}, {14, 6}, {0, 12}},
			wantWinner: Black,
			wantLine:   []Point{{X: 0, Y: 12}, {X: 0, Y: 13}, {X: 0, Y: 14}, {X: 0, Y: 11}, {X: 0, Y: 10}},
		},
		"diagonal": {
			moves:      [][2]int{{0, 0}, {5, 0}, {1, 1}, {6, 0}, {2, 2}, {7, 0}, {3, 3}, {9, 0}, {4, 4}},
			wantWinner: Black,
			wantLine:   []Point{{X: 4, Y: 4}, {X: 3, Y: 3}, {X: 2, Y: 2}, {X: 1, Y: 1}, {X: 0, Y: 0}},
		},
		"anti-diagonal by white": {
			moves:      [][2]int{{7, 7}, {10, 14}, {0, 0}, {11, 13}, {2, 0}, {12, 12}, {4, 0}, {13, 11}, {6, 0}, {14, 10}},
			wantWinner: White,
			wantLine:   []Point{{X: 14, Y: 10}, {X: 13, Y: 11}, {X: 12, Y: 12}, {X: 11, Y: 13}, {X: 10, Y: 14}},
		},
		"overline of six": {
			moves:      [][2]int{{0, 0}, {0, 2}, {1, 0}, {2, 2}, {2, 0}, {4, 2}, {4, 0}, {6, 2}, {5, 0}, {8, 2}, {3, 0}},
			wantWinner: Black,
			wantLine:   []Point{{X: 3, Y: 0}, {X: 4, Y: 0}, {X: 5, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 0}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			g, err := Replay(squares(c.moves...))
			require.NoError(t, err)
			assert.True(t, g.Over())
			assert.Equal(t, c.wantWinner, g.Winner())
			assert.Equal(t, Empty, g.Turn())
			assert.ElementsMatch(t, c.wantLine, g.Line())
			assert.Equal(t, c.wantLine[0], g.Line()[0], "最后一子排在最前")
			assert.ErrorIs(t, g.Play(14, 14), ErrIllegalMove, "结束后不能再落子")
		})
	}
}

// 四子连着不算赢；Board 按行从上到下排列，Last 是最后一手。
func TestFourInARowIsNotAWin(t *testing.T) {
	g, err := Replay(squares([2]int{3, 7}, [2]int{3, 9}, [2]int{4, 7}, [2]int{5, 9}, [2]int{5, 7}, [2]int{7, 9}, [2]int{6, 7}, [2]int{8, 8}))
	require.NoError(t, err)
	assert.False(t, g.Over())
	assert.Equal(t, Black, g.Turn())
	assert.Empty(t, g.Line())
	board := g.Board()
	require.Len(t, board, Squares)
	assert.Equal(t, "...bbbb........", board[7*Size:8*Size])
	assert.Equal(t, byte('w'), board[8*Size+8])
	last, ok := g.Last()
	require.True(t, ok)
	assert.Equal(t, Point{X: 8, Y: 8}, last)
	assert.Equal(t, White, g.At(8, 8))
}

// 落在已有棋子的格子、出界都不合规则；重放时有一步不合规则整局都不接受。
func TestPlayRejectsIllegalMoves(t *testing.T) {
	g := New()
	require.NoError(t, g.Play(7, 7))
	assert.ErrorIs(t, g.Play(7, 7), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(Size, 0), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(0, -1), ErrIllegalMove)
	assert.Equal(t, White, g.Turn(), "不合规则的落子不改变轮到谁")

	for _, moves := range [][]int{{112, 112}, {-1}, {Squares}} {
		_, err := Replay(moves)
		assert.ErrorIs(t, err, ErrIllegalMove, "moves %v", moves)
	}
}

// 225 格下满、谁都没有连成五子是和棋。这里黑白按 (x+2y) mod 4 分成两两相间的斜纹，任何方向上同色最多连两子。
func TestFullBoardWithoutFiveIsDraw(t *testing.T) {
	var black, white []int
	for square := 0; square < Squares; square++ {
		if (square%Size+2*(square/Size))%4 < 2 {
			black = append(black, square)
		} else {
			white = append(white, square)
		}
	}
	require.Len(t, black, 113)
	require.Len(t, white, 112)
	moves := make([]int, 0, Squares)
	for i := range black {
		moves = append(moves, black[i])
		if i < len(white) {
			moves = append(moves, white[i])
		}
	}
	g, err := Replay(moves)
	require.NoError(t, err)
	assert.True(t, g.Over())
	assert.Equal(t, Empty, g.Winner())
	assert.Equal(t, Empty, g.Turn())
	assert.NotContains(t, g.Board(), ".")
}
