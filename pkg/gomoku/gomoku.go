// Package gomoku 是游戏中心五子棋的规则与 AI。
//
// 15×15 棋盘，自由规则：黑先，双方轮流在空格落子，先在横、竖或斜线上连成 5 子或以上的一方获胜，没有禁手；225 格下满
// 仍未分出胜负为和棋。一局完全由按顺序的落子决定：保存一局只需记下落子，读出时重放(Replay)，重放同时校验了每一步都合规则。
//
// 坐标 x 是列(从左到右)，y 是行(从上到下)，格子编号 y*15+x。AI 见 ai.go。
package gomoku

import (
	"errors"
	"strings"
)

const (
	Size    = 15
	Squares = Size * Size
)

// Stone 是格子里的棋子，也表示执这种颜色的一方。
type Stone int8

const (
	Empty Stone = iota
	Black
	White
)

var ErrIllegalMove = errors.New("illegal gomoku move")

// Point 是棋盘上的一格。
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// lineDirections 是连线的四个方向(x, y)：横、竖、两条斜线。
var lineDirections = [4][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}

// Game 是一局五子棋。
type Game struct {
	board  [Squares]Stone
	moves  []int
	winner Stone
	line   []Point
}

// New 返回空棋盘，黑先。
func New() *Game {
	return &Game{}
}

// Replay 按顺序重放 moves 里的落子(格子编号 y*15+x)，有一步不合规则时返回 ErrIllegalMove。
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

// Over 表示这局已经结束：有人连成五子，或者棋盘下满。
func (g *Game) Over() bool {
	return g.winner != Empty || len(g.moves) == Squares
}

// Winner 是连成五子的一方，没结束或和棋时为 Empty。
func (g *Game) Winner() Stone {
	return g.winner
}

// Turn 是轮到走的一方，结束后为 Empty。
func (g *Game) Turn() Stone {
	switch {
	case g.Over():
		return Empty
	case len(g.moves)%2 == 0:
		return Black
	default:
		return White
	}
}

// At 返回 (x, y) 上的棋子。
func (g *Game) At(x, y int) Stone {
	return g.board[y*Size+x]
}

// Play 让轮到走的一方在 (x, y) 落子。
func (g *Game) Play(x, y int) error {
	if g.Over() || x < 0 || x >= Size || y < 0 || y >= Size || g.board[y*Size+x] != Empty {
		return ErrIllegalMove
	}
	stone := g.Turn()
	g.board[y*Size+x] = stone
	g.moves = append(g.moves, y*Size+x)

	// 只有穿过这一子的线可能刚连成；同时连成几条时都算进获胜连线。
	var line []Point
	for _, d := range lineDirections {
		var run []Point
		for _, sign := range [2]int{1, -1} {
			for k := 1; ; k++ {
				px, py := x+sign*k*d[0], y+sign*k*d[1]
				if px < 0 || px >= Size || py < 0 || py >= Size || g.board[py*Size+px] != stone {
					break
				}
				run = append(run, Point{X: px, Y: py})
			}
		}
		if len(run) >= 4 {
			line = append(line, run...)
		}
	}
	if line != nil {
		g.winner = stone
		g.line = append([]Point{{X: x, Y: y}}, line...)
	}
	return nil
}

// Moves 返回按顺序的落子(格子编号 y*15+x)。
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

// Line 是获胜的连线(最后一子在最前)，没有人获胜时为空。
func (g *Game) Line() []Point {
	return append([]Point(nil), g.line...)
}

// Board 按行从上到下、每行从左到右给出 225 个字符：'.' 空，'b' 黑，'w' 白。
func (g *Game) Board() string {
	var b strings.Builder
	b.Grow(Squares)
	for _, stone := range g.board {
		b.WriteByte(".bw"[stone])
	}
	return b.String()
}
