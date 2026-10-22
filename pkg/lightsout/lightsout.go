// Package lightsout 实现熄灯挑战。点击一格会切换自身及上下左右的灯，初盘由全灭盘反向生成，保证可解。
package lightsout

import (
	"errors"
	"math/rand/v2"
)

const MaxMoves = 256

var ErrMove = errors.New("invalid lights out move")

type Game struct {
	Difficulty int    `json:"difficulty"`
	Size       int    `json:"size"`
	Board      []bool `json:"board"`
	Moves      int    `json:"moves"`
}

func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	g := &Game{Difficulty: difficulty, Size: difficulty + 2}
	g.Board = make([]bool, g.Size*g.Size)
	rng := rand.New(rand.NewPCG(seed, 31))
	for _, cell := range rng.Perm(len(g.Board))[:g.Size*2] {
		g.toggle(cell)
	}
	if g.Solved() {
		g.toggle(0)
	}
	return g, nil
}

func (g *Game) toggle(cell int) {
	x, y := cell%g.Size, cell/g.Size
	g.Board[cell] = !g.Board[cell]
	for _, offset := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		nx, ny := x+offset[0], y+offset[1]
		if nx >= 0 && nx < g.Size && ny >= 0 && ny < g.Size {
			i := ny*g.Size + nx
			g.Board[i] = !g.Board[i]
		}
	}
}

func (g *Game) Move(cell int) error {
	if g.Size < 3 || g.Size > 5 || len(g.Board) != g.Size*g.Size || cell < 0 || cell >= len(g.Board) || g.Moves >= MaxMoves || g.Solved() {
		return ErrMove
	}
	g.toggle(cell)
	g.Moves++
	return nil
}

func (g *Game) Solved() bool {
	for _, on := range g.Board {
		if on {
			return false
		}
	}
	return len(g.Board) > 0
}
