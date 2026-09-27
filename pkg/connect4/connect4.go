// Package connect4 是游戏中心四子棋的规则与 AI。
//
// 棋盘 7 列 × 6 行竖直立着，双方轮流选一列投子，棋子落到这一列最低的空位；先在横、竖或斜线上连成 4 子的一方获胜，
// 42 格下满还没有人连成 4 子为和棋。一局完全由按顺序投下的列号决定：保存一局只需记下列号，读出时重放(Replay)，
// 重放同时校验了每一步都合规则。
//
// AI 见 ai.go：negamax 形式的 minimax + alpha-beta 搜索，着法中路优先。
package connect4

import (
	"errors"
	"strings"
)

const (
	Cols = 7
	Rows = 6
	// Cells 是棋盘的格数，下满为和。
	Cells = Cols * Rows
)

// Disc 是格子里的棋子。
type Disc int8

const (
	Empty Disc = iota
	// First 是先走的一方(红)，Second 是后走的一方(黄)。
	First
	Second
)

var ErrIllegalMove = errors.New("illegal connect4 move")

// Cell 是棋盘上的一格。Row 从最上面一行数起，与 Board 的顺序一致。
type Cell struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

// lineDirections 是连线的四个方向(列, 高)：横、竖、两条斜线。
var lineDirections = [4][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}}

// Game 是一局四子棋。格子按(列, 高)保存，高从这一列最下面一格数起。
type Game struct {
	cells  [Cols][Rows]Disc
	height [Cols]int
	moves  []int
	winner Disc
	line   []Cell
}

// New 返回空棋盘，先手先走。
func New() *Game {
	return &Game{}
}

// Replay 按顺序投下 moves 里的列号重放一局，有一步不合规则时返回 ErrIllegalMove。
func Replay(moves []int) (*Game, error) {
	g := New()
	for _, col := range moves {
		if err := g.Play(col); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// Over 表示这局已经结束：有人连成 4 子，或者棋盘下满。
func (g *Game) Over() bool {
	return g.winner != Empty || len(g.moves) == Cells
}

// Winner 是连成 4 子的一方，没结束或和棋时为 Empty。
func (g *Game) Winner() Disc {
	return g.winner
}

// Turn 是轮到走的一方，结束后为 Empty。
func (g *Game) Turn() Disc {
	switch {
	case g.Over():
		return Empty
	case len(g.moves)%2 == 0:
		return First
	default:
		return Second
	}
}

// CanPlay 表示这一列还能投子。
func (g *Game) CanPlay(col int) bool {
	return !g.Over() && col >= 0 && col < Cols && g.height[col] < Rows
}

// Play 让轮到走的一方在 col 列投一子。
func (g *Game) Play(col int) error {
	if !g.CanPlay(col) {
		return ErrIllegalMove
	}
	disc := g.Turn()
	h := g.height[col]
	g.cells[col][h] = disc
	g.height[col]++
	g.moves = append(g.moves, col)

	// 只有穿过这一子的线可能刚连成；同时连成几条时都算进获胜连线。
	var line []Cell
	for _, d := range lineDirections {
		var run []Cell
		for _, sign := range [2]int{1, -1} {
			for k := 1; ; k++ {
				c, r := col+sign*k*d[0], h+sign*k*d[1]
				if c < 0 || c >= Cols || r < 0 || r >= Rows || g.cells[c][r] != disc {
					break
				}
				run = append(run, Cell{Col: c, Row: Rows - 1 - r})
			}
		}
		if len(run) >= 3 {
			line = append(line, run...)
		}
	}
	if line != nil {
		g.winner = disc
		g.line = append([]Cell{{Col: col, Row: Rows - 1 - h}}, line...)
	}
	return nil
}

// Moves 返回按顺序投下的列号。
func (g *Game) Moves() []int {
	return append([]int(nil), g.moves...)
}

// Last 是最后投下的一子，还没有人走时 ok 为 false。
func (g *Game) Last() (Cell, bool) {
	if len(g.moves) == 0 {
		return Cell{}, false
	}
	col := g.moves[len(g.moves)-1]
	return Cell{Col: col, Row: Rows - g.height[col]}, true
}

// Line 是获胜的连线(最后一子在最前)，没有人获胜时为空。
func (g *Game) Line() []Cell {
	return append([]Cell(nil), g.line...)
}

// Board 按行从上到下、每行从左到右给出 42 个字符：'.' 空，'r' 先手，'y' 后手。
func (g *Game) Board() string {
	var b strings.Builder
	b.Grow(Cells)
	for h := Rows - 1; h >= 0; h-- {
		for col := 0; col < Cols; col++ {
			b.WriteByte(".ry"[g.cells[col][h]])
		}
	}
	return b.String()
}
