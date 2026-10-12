// Package reversi 是游戏中心黑白棋的规则与 AI。
//
// 8×8 棋盘，标准开局：中央 d4、e5 为白，d5、e4 为黑，黑先。落子必须夹住并翻转对方至少一子：落子与己方棋子之间，
// 八个方向上连续的对方棋子全部翻转。轮到的一方没有合法着法时自动跳过、由对方继续；双方都无子可下时结束，子多者胜，
// 相等为和。一局完全由按顺序的落子决定，跳过不用记录：保存一局只需记下落子，读出时重放(Replay)，重放同时校验了
// 每一步都合规则。
//
// 坐标 x 是列(从左到右)，y 是行(从上到下)，格子编号 y*8+x。AI 见 ai.go。
package reversi

import (
	"errors"
	"math/bits"
	"strings"
)

const (
	Size    = 8
	Squares = Size * Size
)

// Disc 是格子里的棋子，也表示执这种颜色的一方。
type Disc int8

const (
	Empty Disc = iota
	Black
	White
)

// Opponent 是另一种颜色。
func (d Disc) Opponent() Disc {
	return 3 - d
}

var ErrIllegalMove = errors.New("illegal reversi move")

// Point 是棋盘上的一格。
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// 位棋盘里第 y*8+x 位是 (x, y)。往 x 增大方向平移一格是左移 1 位，这时原来在最右一列的子会绕到下一行最左一列，
// 要用 notFileA 去掉；往 x 减小方向同理用 notFileH。
const (
	fileA    uint64 = 0x0101010101010101
	fileH    uint64 = 0x8080808080808080
	notFileA        = ^fileA
	notFileH        = ^fileH
)

// 八个方向按平移的位数成对：左移 shift 位(x 或 y 增大)与右移 shift 位(减小)，各自带去掉绕行的掩码。
var (
	dirShifts  = [4]uint{1, 8, 9, 7}
	leftMasks  = [4]uint64{notFileA, ^uint64(0), notFileA, notFileH}
	rightMasks = [4]uint64{notFileH, ^uint64(0), notFileH, notFileA}
)

// legalMoves 返回 me 这一方能落子的格子：从 me 的棋子出发，沿一个方向连续是 opp 的棋子，紧接着是空格。
func legalMoves(me, opp uint64) uint64 {
	empty := ^(me | opp)
	var moves uint64
	for i, shift := range dirShifts {
		mask := leftMasks[i] & opp
		run := (me << shift) & mask
		run |= (run << shift) & mask
		run |= (run << shift) & mask
		run |= (run << shift) & mask
		run |= (run << shift) & mask
		run |= (run << shift) & mask
		moves |= (run << shift) & leftMasks[i] & empty

		mask = rightMasks[i] & opp
		run = (me >> shift) & mask
		run |= (run >> shift) & mask
		run |= (run >> shift) & mask
		run |= (run >> shift) & mask
		run |= (run >> shift) & mask
		run |= (run >> shift) & mask
		moves |= (run >> shift) & rightMasks[i] & empty
	}
	return moves
}

// flips 返回 me 在 square 落子时翻转的 opp 棋子，不合法的落子返回 0。
func flips(me, opp uint64, square int) uint64 {
	move := uint64(1) << square
	var flipped uint64
	for i, shift := range dirShifts {
		var line uint64
		cell := (move << shift) & leftMasks[i]
		for cell&opp != 0 {
			line |= cell
			cell = (cell << shift) & leftMasks[i]
		}
		if cell&me != 0 {
			flipped |= line
		}
		line = 0
		cell = (move >> shift) & rightMasks[i]
		for cell&opp != 0 {
			line |= cell
			cell = (cell >> shift) & rightMasks[i]
		}
		if cell&me != 0 {
			flipped |= line
		}
	}
	return flipped
}

func pointsOf(set uint64) []Point {
	points := make([]Point, 0, bits.OnesCount64(set))
	for set != 0 {
		square := bits.TrailingZeros64(set)
		points = append(points, Point{X: square % Size, Y: square / Size})
		set &= set - 1
	}
	return points
}

// Game 是一局黑白棋。
type Game struct {
	black, white uint64
	// turn 是轮到走的一方，结束后为 Empty。
	turn  Disc
	moves []int
	// flipped 是最后一手翻转的棋子；passed 是最后一手之后因为无子可下被跳过的一方，没有时为 Empty。
	flipped uint64
	passed  Disc
}

// New 返回标准开局，黑先。
func New() *Game {
	return &Game{
		black: 1<<(3*Size+4) | 1<<(4*Size+3),
		white: 1<<(3*Size+3) | 1<<(4*Size+4),
		turn:  Black,
	}
}

// Replay 按顺序重放 moves 里的落子(格子编号 y*8+x)，有一步不合规则时返回 ErrIllegalMove。
func Replay(moves []int) (*Game, error) {
	g := New()
	for _, square := range moves {
		if square < 0 || square >= Squares {
			return nil, ErrIllegalMove
		}
		if err := g.Play(square%Size, square/Size); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// discs 返回 color 与对方的棋子。
func (g *Game) discs(color Disc) (uint64, uint64) {
	if color == Black {
		return g.black, g.white
	}
	return g.white, g.black
}

// Turn 是轮到走的一方，结束后为 Empty。
func (g *Game) Turn() Disc {
	return g.turn
}

// Over 表示双方都无子可下，这局结束。
func (g *Game) Over() bool {
	return g.turn == Empty
}

// Count 返回黑白双方的棋子数。
func (g *Game) Count() (black int, white int) {
	return bits.OnesCount64(g.black), bits.OnesCount64(g.white)
}

// Winner 是结束时子多的一方，没结束或子数相等(和棋)时为 Empty。
func (g *Game) Winner() Disc {
	if !g.Over() {
		return Empty
	}
	black, white := g.Count()
	switch {
	case black > white:
		return Black
	case white > black:
		return White
	}
	return Empty
}

// Legal 是轮到走的一方的合法落子，结束后为空。
func (g *Game) Legal() []Point {
	if g.Over() {
		return []Point{}
	}
	return pointsOf(legalMoves(g.discs(g.turn)))
}

// Play 让轮到走的一方在 (x, y) 落子并翻转夹住的棋子，然后决定下一步谁走：对方有子可下就轮到对方；对方无子可下而自己
// 还有就跳过对方；都没有就结束。
func (g *Game) Play(x, y int) error {
	if g.Over() || x < 0 || x >= Size || y < 0 || y >= Size {
		return ErrIllegalMove
	}
	square := y*Size + x
	me, opp := g.discs(g.turn)
	if (me|opp)&(1<<square) != 0 {
		return ErrIllegalMove
	}
	flipped := flips(me, opp, square)
	if flipped == 0 {
		return ErrIllegalMove
	}
	me |= flipped | 1<<square
	opp &^= flipped
	if g.turn == Black {
		g.black, g.white = me, opp
	} else {
		g.white, g.black = me, opp
	}
	g.moves = append(g.moves, square)
	g.flipped, g.passed = flipped, Empty
	switch {
	case legalMoves(opp, me) != 0:
		g.turn = g.turn.Opponent()
	case legalMoves(me, opp) != 0:
		g.passed = g.turn.Opponent()
	default:
		g.turn = Empty
	}
	return nil
}

// Moves 返回按顺序的落子(格子编号 y*8+x)。
func (g *Game) Moves() []int {
	return append([]int(nil), g.moves...)
}

// Last 是最后一手落子，还没有人走时 ok 为 false。
func (g *Game) Last() (Point, bool) {
	if len(g.moves) == 0 {
		return Point{}, false
	}
	square := g.moves[len(g.moves)-1]
	return Point{X: square % Size, Y: square / Size}, true
}

// Flipped 是最后一手翻转的棋子。
func (g *Game) Flipped() []Point {
	return pointsOf(g.flipped)
}

// Passed 是最后一手之后因为无子可下被跳过的一方，没有时为 Empty。
func (g *Game) Passed() Disc {
	return g.passed
}

// Board 按行从上到下、每行从左到右给出 64 个字符：'.' 空，'b' 黑，'w' 白。
func (g *Game) Board() string {
	var b strings.Builder
	b.Grow(Squares)
	for square := 0; square < Squares; square++ {
		switch {
		case g.black&(1<<square) != 0:
			b.WriteByte('b')
		case g.white&(1<<square) != 0:
			b.WriteByte('w')
		default:
			b.WriteByte('.')
		}
	}
	return b.String()
}
