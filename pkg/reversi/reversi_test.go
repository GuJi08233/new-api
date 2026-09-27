package reversi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setup 摆出一个局面，rows 是从上到下的 8 行('.' 空，'b' 黑，'w' 白)，轮到 turn 走。
func setup(t *testing.T, turn Disc, rows ...string) *Game {
	t.Helper()
	require.Len(t, rows, Size)
	g := &Game{turn: turn}
	for y, row := range rows {
		require.Len(t, row, Size)
		for x, cell := range row {
			switch cell {
			case 'b':
				g.black |= 1 << (y*Size + x)
			case 'w':
				g.white |= 1 << (y*Size + x)
			}
		}
	}
	return g
}

// 标准开局：中央 d4、e5 白，d5、e4 黑，黑先，黑能走的正好是 d3、c4、f5、e6 四格。
func TestNewGameIsStandardOpening(t *testing.T) {
	g := New()
	want := strings.Repeat(".", 27) + "wb" + strings.Repeat(".", 6) + "bw" + strings.Repeat(".", 27)
	assert.Equal(t, want, g.Board())
	assert.Equal(t, Black, g.Turn())
	assert.ElementsMatch(t, []Point{{X: 3, Y: 2}, {X: 2, Y: 3}, {X: 5, Y: 4}, {X: 4, Y: 5}}, g.Legal())
	black, white := g.Count()
	assert.Equal(t, 2, black)
	assert.Equal(t, 2, white)
}

// 从开局往下每一层的局面数(perft)与公认的数值一致：1 到 6 层依次是 4、12、56、244、1396、8200，着法生成与翻子没有漏也没有多。
func TestPerftMatchesReference(t *testing.T) {
	var perft func(g *Game, depth int) int
	perft = func(g *Game, depth int) int {
		if depth == 0 {
			return 1
		}
		total := 0
		for _, p := range g.Legal() {
			next := *g
			next.moves = g.Moves()
			require.NoError(t, next.Play(p.X, p.Y))
			total += perft(&next, depth-1)
		}
		return total
	}
	for depth, want := range []int{1, 4, 12, 56, 244, 1396, 8200} {
		assert.Equal(t, want, perft(New(), depth), "depth %d", depth)
	}
}

// 一手可以同时在八个方向上夹住对方的棋子，全部翻转；夹不住的方向(隔着空格、到了边上)不翻。
func TestPlayFlipsEveryFlankedDirection(t *testing.T) {
	g := setup(t, Black,
		"b..b..b.",
		".w.w.w..",
		"..www...",
		"bww.wwb.",
		"..www...",
		".w.w.w..",
		"b..b..bw",
		"........",
	)
	require.NoError(t, g.Play(3, 3))
	assert.ElementsMatch(t, []Point{
		{X: 2, Y: 2}, {X: 3, Y: 2}, {X: 4, Y: 2}, {X: 2, Y: 3}, {X: 4, Y: 3}, {X: 2, Y: 4}, {X: 3, Y: 4}, {X: 4, Y: 4},
		{X: 1, Y: 1}, {X: 3, Y: 1}, {X: 5, Y: 1}, {X: 1, Y: 3}, {X: 5, Y: 3}, {X: 1, Y: 5}, {X: 3, Y: 5}, {X: 5, Y: 5},
	}, g.Flipped())
	last, ok := g.Last()
	require.True(t, ok)
	assert.Equal(t, Point{X: 3, Y: 3}, last)
	black, white := g.Count()
	assert.Equal(t, 25, black)
	assert.Equal(t, 1, white, "不在这一手八条线上的白子不受影响")
}

// 右边沿的棋子不会"绕到"下一行的左边沿：只有沿边往左才夹得住。
func TestMovesDoNotWrapAroundEdges(t *testing.T) {
	g := setup(t, Black,
		"......bw",
		"........",
		"........",
		"........",
		"........",
		"........",
		"........",
		"bw......",
	)
	assert.ElementsMatch(t, []Point{{X: 2, Y: 7}}, g.Legal())
	assert.ErrorIs(t, g.Play(0, 1), ErrIllegalMove)
}

// 对方无子可下时跳过，自己接着走；双方都无子可下时结束，子多者胜。
func TestTurnPassesWhenOpponentCannotMove(t *testing.T) {
	g := setup(t, Black,
		"bw......",
		"........",
		"........",
		"........",
		"........",
		"........",
		"......w.",
		".......b",
	)
	require.NoError(t, g.Play(2, 0))
	assert.Equal(t, White, g.Passed(), "白棋剩下的一颗子夹不住任何黑子")
	assert.Equal(t, Black, g.Turn())
	assert.Equal(t, []Point{{X: 5, Y: 5}}, g.Legal())
	assert.False(t, g.Over())

	require.NoError(t, g.Play(5, 5))
	assert.True(t, g.Over())
	assert.Equal(t, Empty, g.Turn())
	assert.Equal(t, Black, g.Winner())
	assert.Empty(t, g.Legal())
	assert.ErrorIs(t, g.Play(3, 3), ErrIllegalMove, "结束后不能再落子")
}

// 双方都无子可下、子数相等是和棋。
func TestGameEndsInDrawWithEqualDiscs(t *testing.T) {
	g := setup(t, Black,
		"bw......",
		"........",
		"........",
		"........",
		"........",
		"........",
		"........",
		".....www",
	)
	require.NoError(t, g.Play(2, 0))
	assert.True(t, g.Over())
	black, white := g.Count()
	assert.Equal(t, 3, black)
	assert.Equal(t, 3, white)
	assert.Equal(t, Empty, g.Winner())
}

// 落在已有棋子的格子、夹不住对方棋子、出界都不合规则；重放时有一步不合规则整局都不接受。
func TestPlayRejectsIllegalMoves(t *testing.T) {
	g := New()
	assert.ErrorIs(t, g.Play(3, 3), ErrIllegalMove, "格子上已经有子")
	assert.ErrorIs(t, g.Play(0, 0), ErrIllegalMove, "夹不住任何白子")
	assert.ErrorIs(t, g.Play(Size, 0), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(2, -1), ErrIllegalMove)
	assert.Equal(t, Black, g.Turn())

	for _, moves := range [][]int{{27}, {0}, {-1}, {Squares}, {19, 19}} {
		_, err := Replay(moves)
		assert.ErrorIs(t, err, ErrIllegalMove, "moves %v", moves)
	}
	g, err := Replay([]int{19, 18})
	require.NoError(t, err)
	assert.Equal(t, []int{19, 18}, g.Moves())
	assert.Equal(t, Black, g.Turn())
}
