// Package pairlink 实现连连看：相同图案之间最多拐两次弯，连线可以绕过棋盘外沿。
package pairlink

import (
	"errors"
	"math/rand/v2"
)

type Level struct {
	Difficulty int `json:"difficulty"`
	Rows       int `json:"rows"`
	Cols       int `json:"cols"`
	Seconds    int `json:"seconds"`
}

var Levels = []Level{{1, 6, 6, 180}, {2, 6, 8, 240}, {3, 8, 8, 300}}

const Tools = 3

var ErrMove = errors.New("invalid pairlink move")

// Point 使用带一格外沿的坐标，棋盘左上角为 (1,1)。
type Point struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

type Game struct {
	Level
	Board     []int  `json:"board"`
	Remaining int    `json:"remaining"`
	Hints     int    `json:"hints"`
	Shuffles  int    `json:"shuffles"`
	Seed      uint64 `json:"seed"`
	Mixes     uint64 `json:"mixes"`
	Status    string `json:"status"`
}

func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > len(Levels) {
		return nil, ErrMove
	}
	g := &Game{Level: Levels[difficulty-1], Hints: Tools, Shuffles: Tools, Seed: seed, Status: "playing"}
	g.Board = make([]int, g.Rows*g.Cols)
	for i := range g.Board {
		g.Board[i] = i/2%12 + 1
	}
	g.Remaining = len(g.Board) / 2
	g.Shuffle()
	return g, nil
}

// Path 只接受两个尚未消除的相同图案，返回至多三段连线。
func (g *Game) Path(first, second int) []Point {
	if first < 0 || second < 0 || first >= len(g.Board) || second >= len(g.Board) || first == second || g.Board[first] == 0 || g.Board[first] != g.Board[second] {
		return nil
	}
	a, b := Point{first/g.Cols + 1, first%g.Cols + 1}, Point{second/g.Cols + 1, second%g.Cols + 1}
	if g.clearLine(a, b, a, b) {
		return []Point{a, b}
	}
	for _, corner := range []Point{{a.Row, b.Col}, {b.Row, a.Col}} {
		if g.clearLine(a, corner, a, b) && g.clearLine(corner, b, a, b) {
			return []Point{a, corner, b}
		}
	}
	for row := 0; row <= g.Rows+1; row++ {
		c, d := Point{row, a.Col}, Point{row, b.Col}
		if g.clearLine(a, c, a, b) && g.clearLine(c, d, a, b) && g.clearLine(d, b, a, b) {
			return []Point{a, c, d, b}
		}
	}
	for col := 0; col <= g.Cols+1; col++ {
		c, d := Point{a.Row, col}, Point{b.Row, col}
		if g.clearLine(a, c, a, b) && g.clearLine(c, d, a, b) && g.clearLine(d, b, a, b) {
			return []Point{a, c, d, b}
		}
	}
	return nil
}

func (g *Game) clearLine(from, to, first, second Point) bool {
	if from.Row != to.Row && from.Col != to.Col {
		return false
	}
	dr, dc := 0, 0
	if to.Row > from.Row {
		dr = 1
	}
	if to.Row < from.Row {
		dr = -1
	}
	if to.Col > from.Col {
		dc = 1
	}
	if to.Col < from.Col {
		dc = -1
	}
	for p := from; ; p = (Point{p.Row + dr, p.Col + dc}) {
		if p != first && p != second && p.Row > 0 && p.Row <= g.Rows && p.Col > 0 && p.Col <= g.Cols && g.Board[(p.Row-1)*g.Cols+p.Col-1] != 0 {
			return false
		}
		if p == to {
			return true
		}
	}
}

func (g *Game) Hint() []int {
	for a, tile := range g.Board {
		if tile == 0 {
			continue
		}
		for b := a + 1; b < len(g.Board); b++ {
			if g.Board[b] == tile && g.Path(a, b) != nil {
				return []int{a, b}
			}
		}
	}
	return nil
}

// Shuffle 保留剩余图案与数量；没有可走的一对时自动重排，避免只能放弃的死盘。
func (g *Game) Shuffle() {
	if g.Remaining == 0 {
		return
	}
	g.Mixes++
	rng := rand.New(rand.NewPCG(g.Seed, g.Mixes))
	for range 16 {
		rng.Shuffle(len(g.Board), func(i, j int) { g.Board[i], g.Board[j] = g.Board[j], g.Board[i] })
		if g.Hint() != nil {
			return
		}
	}
	// 极端情况下把一对放在相邻位置，其余图案仍完整保留。
	pair := -1
	for i, tile := range g.Board {
		if tile != 0 {
			pair = i
			break
		}
	}
	g.Board[0], g.Board[pair] = g.Board[pair], g.Board[0]
	for i := 1; i < len(g.Board); i++ {
		if g.Board[i] == g.Board[0] {
			g.Board[1], g.Board[i] = g.Board[i], g.Board[1]
			break
		}
	}
}

func (g *Game) Match(first, second int) ([]Point, bool, error) {
	if g.Status != "playing" {
		return nil, false, ErrMove
	}
	path := g.Path(first, second)
	if path == nil {
		return nil, false, ErrMove
	}
	g.Board[first], g.Board[second] = 0, 0
	g.Remaining--
	if g.Remaining == 0 {
		g.Status = "won"
		return path, false, nil
	}
	if g.Hint() == nil {
		g.Shuffle()
		return path, true, nil
	}
	return path, false, nil
}
