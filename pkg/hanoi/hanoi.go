// Package hanoi 实现汉诺塔：一次只能移动塔顶圆盘，大盘不能叠在小盘上，将所有盘移到第三座塔获胜。
package hanoi

import "errors"

const MaxMoves = 1024

var ErrMove = errors.New("invalid hanoi move")

type Game struct {
	Difficulty int      `json:"difficulty"`
	Disks      int      `json:"disks"`
	Towers     [3][]int `json:"towers"`
	Moves      int      `json:"moves"`
}

func New(difficulty int) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	g := &Game{Difficulty: difficulty, Disks: difficulty + 2, Towers: [3][]int{{}, {}, {}}}
	for disk := g.Disks; disk > 0; disk-- {
		g.Towers[0] = append(g.Towers[0], disk)
	}
	return g, nil
}

func (g *Game) Move(from, to int) error {
	if from < 0 || from > 2 || to < 0 || to > 2 || from == to || g.Moves >= MaxMoves || g.Solved() {
		return ErrMove
	}
	source, target := g.Towers[from], g.Towers[to]
	if len(source) == 0 {
		return ErrMove
	}
	disk := source[len(source)-1]
	if len(target) > 0 && target[len(target)-1] < disk {
		return ErrMove
	}
	g.Towers[from] = source[:len(source)-1]
	g.Towers[to] = append(target, disk)
	g.Moves++
	return nil
}

func (g *Game) Solved() bool { return g.Disks > 0 && len(g.Towers[2]) == g.Disks }

func (g *Game) MinimumMoves() int { return (1 << g.Disks) - 1 }
