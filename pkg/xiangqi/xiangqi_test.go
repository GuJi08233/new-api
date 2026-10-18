package xiangqi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diagram 把从红方看过去的棋盘图拼成棋盘字符串：第一行是黑方底线 y=9，最后一行是红方底线 y=0，每行 9 个字符。
func diagram(t *testing.T, rows ...string) string {
	t.Helper()
	require.Len(t, rows, Height)
	var board strings.Builder
	for y := 0; y < Height; y++ {
		require.Len(t, rows[Height-1-y], Width)
		board.WriteString(rows[Height-1-y])
	}
	return board.String()
}

func move(fromX, fromY, toX, toY int) Move {
	return Move{From: Square(fromX, fromY), To: Square(toX, toY)}
}

// perft 数出从 pos 走 depth 步的所有合法着法序列。
func perft(pos *Position, depth int) int {
	if depth == 0 {
		return 1
	}
	total := 0
	for _, m := range pos.LegalMoves() {
		captured := pos.makeMove(m)
		total += perft(pos, depth-1)
		pos.unmakeMove(m, captured)
	}
	return total
}

// 开局的着法数与公认的 perft 结果一致：红方第一步 44 种走法，四步之内 3290240 种走法序列。这几层已经走到了炮打马、
// 马踩兵、送将与应将，任何一种棋子的走法错了都对不上。走完退回后局面(含置换表的键与评估分)与原来一模一样。
func TestPerftFromStartPosition(t *testing.T) {
	game := NewGame()
	pos, err := game.Position()
	require.NoError(t, err)
	before := *pos
	for depth, want := range []int{1, 44, 1920, 79666, 3290240} {
		assert.Equal(t, want, perft(pos, depth), "depth %d", depth)
	}
	assert.Equal(t, before, *pos)
	assert.Equal(t, StartBoard, pos.Board())
}

// 每种棋子的走法，包括蹩马腿、塞象眼、炮隔一子吃、仕相帅不出界、兵过河才能横走。
func TestPieceMoves(t *testing.T) {
	cases := []struct {
		name string
		rows []string
		turn Color
		x, y int
		want [][2]int
	}{
		{
			name: "horse in the open reaches all eight points",
			rows: []string{".....k...", ".........", ".........", ".........", ".........", "....N....", ".........", ".........", ".........", "...K....."},
			x:    4, y: 4,
			want: [][2]int{{3, 6}, {5, 6}, {3, 2}, {5, 2}, {6, 5}, {6, 3}, {2, 5}, {2, 3}},
		},
		{
			// 上方与右方的马腿被堵住，左下的落点是自己的仕，左边的落点可以吃对方的马。
			name: "horse legs blocked",
			rows: []string{".....k...", ".........", ".........", ".........", "....P....", "....Np...", "..n......", "...A.....", ".........", "...K....."},
			x:    4, y: 4,
			want: [][2]int{{5, 2}, {2, 5}, {2, 3}},
		},
		{
			// 左上的象眼被塞住，右上的落点可以吃子。
			name: "elephant eye blocked",
			rows: []string{".....k...", ".........", ".........", ".........", ".........", "......n..", "...p.....", "....B....", ".........", "...K....."},
			x:    4, y: 2,
			want: [][2]int{{2, 0}, {6, 0}, {6, 4}},
		},
		{
			name: "elephant does not cross the river",
			rows: []string{".....k...", ".........", ".........", ".........", ".........", "..B......", ".........", ".........", ".........", "...K....."},
			x:    2, y: 4,
			want: [][2]int{{0, 2}, {4, 2}},
		},
		{
			name: "advisor in the palace centre",
			rows: []string{"...k.....", ".........", ".........", ".........", ".........", ".........", ".........", ".........", "....A....", "....K...."},
			x:    4, y: 1,
			want: [][2]int{{3, 0}, {5, 0}, {3, 2}, {5, 2}},
		},
		{
			name: "advisor on the palace corner",
			rows: []string{"...k.....", ".........", ".........", ".........", ".........", ".........", ".........", ".....A...", ".........", "....K...."},
			x:    5, y: 2,
			want: [][2]int{{4, 1}},
		},
		{
			// 走到 x=3 会和黑将在同一列上照面。
			name: "king stays in the palace and never faces the other king",
			rows: []string{"...k.....", ".........", ".........", ".........", ".........", ".........", ".........", ".........", "....K....", "........."},
			x:    4, y: 1,
			want: [][2]int{{4, 0}, {4, 2}, {5, 1}},
		},
		{
			name: "rook slides until the first piece and captures it",
			rows: []string{".....k...", ".........", "....p....", ".........", ".........", "....R....", ".........", ".........", "....A....", "...K....."},
			x:    4, y: 4,
			want: [][2]int{
				{4, 5}, {4, 6}, {4, 7},
				{4, 3}, {4, 2},
				{5, 4}, {6, 4}, {7, 4}, {8, 4},
				{3, 4}, {2, 4}, {1, 4}, {0, 4},
			},
		},
		{
			// 往上隔着自己的兵吃士；往下的炮架后面没有子；右边紧挨着的马不能直接吃，隔着它吃车；左边隔一个兵只能吃紧接着的兵，
			// 吃不到隔两个子的马。
			name: "cannon captures over exactly one screen",
			rows: []string{".....k...", "....a....", ".........", "....P....", ".........", ".nppCn.r.", ".........", ".........", "....A....", "...K....."},
			x:    4, y: 4,
			want: [][2]int{{4, 5}, {4, 8}, {4, 3}, {4, 2}, {7, 4}, {2, 4}},
		},
		{
			name: "pawn before the river only moves forward",
			rows: []string{".....k...", ".........", ".........", ".........", ".........", ".........", "..P......", ".........", ".........", "...K....."},
			x:    2, y: 3,
			want: [][2]int{{2, 4}},
		},
		{
			name: "pawn across the river also moves sideways",
			rows: []string{".....k...", ".........", ".........", ".........", "..P......", ".........", ".........", ".........", ".........", "...K....."},
			x:    2, y: 5,
			want: [][2]int{{2, 6}, {1, 5}, {3, 5}},
		},
		{
			name: "pawn on the last rank only moves sideways",
			rows: []string{"P....k...", ".........", ".........", ".........", ".........", ".........", ".........", ".........", ".........", "...K....."},
			x:    0, y: 9,
			want: [][2]int{{1, 9}},
		},
		{
			name: "black pawn moves down the board",
			rows: []string{".....k...", ".........", ".........", "......p..", ".........", ".........", ".........", ".........", ".........", "...K....."},
			turn: Black,
			x:    6, y: 6,
			want: [][2]int{{6, 5}},
		},
		{
			name: "black pawn across the river",
			rows: []string{".....k...", ".........", ".........", ".........", ".........", "......p..", ".........", ".........", ".........", "...K....."},
			turn: Black,
			x:    6, y: 4,
			want: [][2]int{{6, 3}, {5, 4}, {7, 4}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pos, err := ParsePosition(diagram(t, c.rows...), c.turn)
			require.NoError(t, err)
			targets := [][2]int{}
			for _, m := range pos.LegalMoves() {
				if m.From == Square(c.x, c.y) {
					x, y := SquareXY(m.To)
					targets = append(targets, [2]int{x, y})
				}
			}
			assert.ElementsMatch(t, c.want, targets)
		})
	}
}

// 两个将帅之间只隔着一个子时，这个子不能离开这一列(飞将)；帅自己可以横走离开。
func TestFlyingGeneralPinsTheOnlyPieceBetweenKings(t *testing.T) {
	pos, err := ParsePosition(diagram(t,
		"....k....",
		".........",
		".........",
		".........",
		".........",
		"....N....",
		".........",
		".........",
		".........",
		"....K....",
	), Red)
	require.NoError(t, err)
	assert.ElementsMatch(t, []Move{move(4, 0, 3, 0), move(4, 0, 5, 0), move(4, 0, 4, 1)}, pos.LegalMoves())
}

// 走完之后己方将帅会被攻击的着法不合法：帅走进车的攻击线、走到和黑将照面的一列、被牵制的马离开、被将军时不去应将。
// 不合法的着法不会改变对局。
func TestPlayRejectsMovesThatLeaveTheKingAttacked(t *testing.T) {
	cases := []struct {
		name    string
		rows    []string
		illegal []Move
		legal   Move
	}{
		{
			name:    "king steps into a rook line or faces the other king",
			rows:    []string{".....k...", "...r.....", ".........", ".........", ".........", ".........", ".........", ".........", "....K....", "........."},
			illegal: []Move{move(4, 1, 3, 1), move(4, 1, 5, 1)},
			legal:   move(4, 1, 4, 2),
		},
		{
			name:    "pinned horse cannot leave the file",
			rows:    []string{"...k.....", ".........", ".........", "....r....", ".........", ".........", ".........", "....N....", ".........", "....K...."},
			illegal: []Move{move(4, 2, 5, 4), move(4, 2, 3, 4), move(4, 2, 2, 3)},
			legal:   move(4, 0, 5, 0),
		},
		{
			// 黑车正在将军：挪动别的子不解决问题，用车挡住才行；帅不能躲到和黑将照面的一列。
			name:    "check must be answered",
			rows:    []string{"...k.....", ".........", ".........", ".........", "....r....", ".........", "R.......P", ".........", ".........", "....K...."},
			illegal: []Move{move(8, 3, 8, 4), move(4, 0, 3, 0), move(4, 0, 4, 1)},
			legal:   move(0, 3, 4, 3),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			game := Game{Board: diagram(t, c.rows...), Turn: Red, Winner: NoColor}
			before := game
			for _, m := range c.illegal {
				assert.ErrorIs(t, game.Play(m), ErrIllegalMove, "%v", m)
				assert.Equal(t, before, game)
			}
			require.NoError(t, game.Play(c.legal))
			assert.Equal(t, Black, game.Turn)
			assert.Equal(t, 1, game.Plies)
			assert.Equal(t, &c.legal, game.Last)
			assert.False(t, game.Over())
		})
	}
	// 不在棋盘上的格子也是不合法的着法。
	game := NewGame()
	assert.ErrorIs(t, game.Play(Move{From: -1, To: 0}), ErrIllegalMove)
	assert.ErrorIs(t, game.Play(Move{From: Square(0, 0), To: Squares}), ErrIllegalMove)
	assert.ErrorIs(t, game.Play(move(0, 6, 0, 5)), ErrIllegalMove, "红方先走，不能走黑方的棋子")
}

// matedByTwoRooks 是红方一步杀的局面：车 (8,7) 走到 (8,9) 沿底线将军，另一只车封住 y=8，黑将无处可逃。
func matedByTwoRooks(t *testing.T) string {
	return diagram(t,
		"....k....",
		"R........",
		"........R",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		"...K.....",
	)
}

// 走完之后对方无子可走就是走的一方赢：被将军时是将死，没被将军(困毙)也一样判负。结束之后不能再走。
func TestNoLegalMoveLoses(t *testing.T) {
	cases := []struct {
		name       string
		board      string
		move       Move
		wantResult Result
	}{
		{name: "checkmate", board: matedByTwoRooks(t), move: move(8, 7, 8, 9), wantResult: ResultCheckmate},
		{
			// 黑将走 (4,9) 会和红帅照面，走 (3,8) 在车的攻击线上，它自己没被将军。
			name: "stalemate",
			board: diagram(t,
				"...k.....",
				".........",
				".........",
				".........",
				"........R",
				".........",
				".........",
				".........",
				".........",
				"....K....",
			),
			move:       move(8, 5, 8, 8),
			wantResult: ResultStalemate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			game := Game{Board: c.board, Turn: Red, Winner: NoColor}
			require.NoError(t, game.Play(c.move))
			assert.True(t, game.Over())
			assert.Equal(t, c.wantResult, game.Result)
			assert.Equal(t, Red, game.Winner)
			pos, err := game.Position()
			require.NoError(t, err)
			assert.Empty(t, pos.LegalMoves())
			assert.Equal(t, c.wantResult == ResultCheckmate, pos.InCheck())
			assert.ErrorIs(t, game.Play(move(3, 0, 3, 1)), ErrGameOver)
		})
	}
}

// 连续 120 步没有吃子、或总步数达到 300 判和；吃子让没有吃子的步数清零；同一步既将死对方又到了和棋步数时按将死算。
func TestDrawRules(t *testing.T) {
	quiet := diagram(t,
		".....k...",
		".........",
		".........",
		"p........",
		".........",
		".........",
		".........",
		".........",
		".........",
		"R..K.....",
	)
	cases := []struct {
		name       string
		game       Game
		move       Move
		wantResult Result
		wantWinner Color
		wantQuiet  int
	}{
		{
			name:       "120 quiet plies",
			game:       Game{Board: quiet, Turn: Red, Plies: 150, Quiet: 119, Winner: NoColor},
			move:       move(0, 0, 1, 0),
			wantResult: ResultNoCapture,
			wantWinner: NoColor,
			wantQuiet:  120,
		},
		{
			name:       "a capture resets the quiet count",
			game:       Game{Board: quiet, Turn: Red, Plies: 150, Quiet: 119, Winner: NoColor},
			move:       move(0, 0, 0, 6),
			wantWinner: NoColor,
			wantQuiet:  0,
		},
		{
			name:       "300 plies",
			game:       Game{Board: quiet, Turn: Red, Plies: 299, Quiet: 10, Winner: NoColor},
			move:       move(0, 0, 1, 0),
			wantResult: ResultMoveLimit,
			wantWinner: NoColor,
			wantQuiet:  11,
		},
		{
			name:       "checkmate on the last ply wins",
			game:       Game{Board: matedByTwoRooks(t), Turn: Red, Plies: 299, Quiet: 119, Winner: NoColor},
			move:       move(8, 7, 8, 9),
			wantResult: ResultCheckmate,
			wantWinner: Red,
			wantQuiet:  120,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			game := c.game
			require.NoError(t, game.Play(c.move))
			assert.Equal(t, c.wantResult, game.Result)
			assert.Equal(t, c.wantWinner, game.Winner)
			assert.Equal(t, c.wantQuiet, game.Quiet)
			assert.Equal(t, c.game.Plies+1, game.Plies)
		})
	}
}

// 棋盘字符串必须是 90 个认识的字符、每方正好一个将帅，没轮到走的一方不能正被将军，否则拒绝解析。
func TestParsePositionRejectsInvalidBoards(t *testing.T) {
	// 红车已经在将军，却又轮到红方走。
	blackInCheck := diagram(t,
		"....k...R",
		"R........",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		"...K.....",
	)
	for name, board := range map[string]string{
		"too short":            StartBoard[1:],
		"unknown letter":       strings.Replace(StartBoard, "N", "X", 1),
		"no black king":        strings.Replace(StartBoard, "k", "a", 1),
		"two red kings":        strings.Replace(StartBoard, "A", "K", 1),
		"opponent is in check": blackInCheck,
	} {
		_, err := ParsePosition(board, Red)
		assert.ErrorIs(t, err, ErrInvalidState, name)
	}
	_, err := ParsePosition(StartBoard, NoColor)
	assert.ErrorIs(t, err, ErrInvalidState)
	_, err = ParsePosition(blackInCheck, Black)
	assert.NoError(t, err, "轮到被将军的一方走是正常的局面")
}

// repeatBoard 是重复局面测试用的棋盘：红车 (0,1)、红帅 (5,0)、黑将 (3,9)，红方先走；车平到将所在的一列就是将军。
func repeatBoard(t *testing.T, deadPawn bool) string {
	rows := []string{
		"...k.....",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		".........",
		"R........",
		".....K...",
	}
	if deadPawn {
		rows[1] = ".p......."
	}
	return diagram(t, rows...)
}

// 同一局面出现 RepeatLimit 次时判局：一方每步都在将军、另一方没有时，长将的一方判负(红车在第 3、4 列之间来回将军)；
// 双方都没有一直将军时和棋(车与将各自在两格之间来回走)。
func TestRepetitionEndsTheGame(t *testing.T) {
	perpetual := []Move{move(0, 1, 3, 1)}
	shuffle := []Move{}
	for i := 0; i < 2; i++ {
		perpetual = append(perpetual,
			move(3, 9, 4, 9), move(3, 1, 4, 1), move(4, 9, 3, 9), move(4, 1, 3, 1))
		shuffle = append(shuffle,
			move(0, 1, 1, 1), move(3, 9, 4, 9), move(1, 1, 0, 1), move(4, 9, 3, 9))
	}
	for _, c := range []struct {
		name       string
		moves      []Move
		wantResult Result
		wantWinner Color
	}{
		{name: "perpetual check loses", moves: perpetual, wantResult: ResultPerpetualCheck, wantWinner: Black},
		{name: "shuffling without checks draws", moves: shuffle, wantResult: ResultRepetition, wantWinner: NoColor},
	} {
		t.Run(c.name, func(t *testing.T) {
			game := Game{Board: repeatBoard(t, false), Turn: Red, Winner: NoColor}
			for i, m := range c.moves {
				require.False(t, game.Over(), "第 %d 步之前就结束了", i)
				require.NoError(t, game.Play(m), "第 %d 步", i)
			}
			assert.Equal(t, c.wantResult, game.Result)
			assert.Equal(t, c.wantWinner, game.Winner)
			repeats, _ := game.Repetitions()
			assert.Equal(t, RepeatLimit, repeats)
		})
	}

	t.Run("两次还不判局", func(t *testing.T) {
		game := Game{Board: repeatBoard(t, false), Turn: Red, Winner: NoColor}
		for _, m := range shuffle[:4] {
			require.NoError(t, game.Play(m))
		}
		repeats, span := game.Repetitions()
		assert.Equal(t, 2, repeats)
		assert.Equal(t, 4, span)
		assert.Empty(t, game.Result)
	})
}

// 悔棋把棋盘、轮到的一方、步数、没有吃子的步数与最后一手都放回那时，被吃掉的子回到原处；撤回吃子后没有吃子的步数重新数。
func TestUndoRestoresThePosition(t *testing.T) {
	game := Game{Board: repeatBoard(t, true), Turn: Red, Plies: 10, Quiet: 4, Winner: NoColor}
	require.NoError(t, game.Play(move(0, 1, 1, 1)))
	beforeCapture := game
	require.NoError(t, game.Play(move(3, 9, 4, 9)))
	require.NoError(t, game.Play(move(1, 1, 1, 8)))
	require.Equal(t, 0, game.Quiet)
	require.Equal(t, "p", game.History[len(game.History)-1].Captured)

	undone := game
	require.NoError(t, undone.Undo(2))
	assert.Equal(t, beforeCapture.Board, undone.Board)
	assert.Equal(t, beforeCapture.Turn, undone.Turn)
	assert.Equal(t, beforeCapture.Plies, undone.Plies)
	assert.Equal(t, beforeCapture.Quiet, undone.Quiet)
	assert.Equal(t, beforeCapture.Last, undone.Last)
	assert.Equal(t, beforeCapture.History, undone.History)
	// 撤回之后照样能接着走，原来的对局不受影响。
	require.NoError(t, undone.Play(move(3, 9, 3, 8)))
	assert.Len(t, game.History, 3)

	all := game
	require.NoError(t, all.Undo(3))
	assert.Equal(t, repeatBoard(t, true), all.Board)
	assert.Equal(t, Red, all.Turn)
	assert.Equal(t, 10, all.Plies)
	assert.Nil(t, all.Last)

	assert.ErrorIs(t, game.Undo(4), ErrNothingToUndo, "记录里只有 3 步")
	assert.ErrorIs(t, game.Undo(0), ErrNothingToUndo)

	over := Game{Board: matedByTwoRooks(t), Turn: Red, Winner: NoColor}
	require.NoError(t, over.Play(move(8, 7, 8, 9)))
	require.True(t, over.Over())
	assert.ErrorIs(t, over.Undo(1), ErrGameOver)
}

// 双方同意和棋：对局按和棋结束，棋盘与步数都不变，之后不能再走也不能再撤回。
func TestDrawAgreedEndsTheGameAsADraw(t *testing.T) {
	game := NewGame()
	require.NoError(t, game.Play(move(1, 2, 4, 2)))
	board, plies, turn := game.Board, game.Plies, game.Turn

	require.NoError(t, game.DrawAgreed())
	assert.True(t, game.Over())
	assert.Equal(t, ResultAgreement, game.Result)
	assert.Equal(t, NoColor, game.Winner, "和棋没有赢家")
	assert.Equal(t, board, game.Board)
	assert.Equal(t, plies, game.Plies)
	assert.Equal(t, turn, game.Turn, "求和不改动轮到谁走")

	assert.ErrorIs(t, game.DrawAgreed(), ErrGameOver)
	assert.ErrorIs(t, game.Play(move(1, 9, 1, 8)), ErrGameOver)
	assert.ErrorIs(t, game.Undo(1), ErrGameOver)
}

// 一步将死对方的棋之后同时按和棋结束没有意义：先结束的一方为准，之后的求和被拒绝。
func TestDrawAgreedRefusesAFinishedGame(t *testing.T) {
	game := Game{Board: matedByTwoRooks(t), Turn: Red, Winner: NoColor}
	require.NoError(t, game.Play(move(8, 7, 8, 9)))
	require.Equal(t, ResultCheckmate, game.Result)
	assert.ErrorIs(t, game.DrawAgreed(), ErrGameOver)
	assert.Equal(t, ResultCheckmate, game.Result, "将死的原因不变")
}
