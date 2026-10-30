// Package mancala 是游戏中心播棋(Kalah，六坑四子)的规则与 AI。
//
// 棋盘两侧各有 Pits 个小坑，每坑开局 Seeds 颗子，两端各有一个大仓。轮到的一方从自己一侧挑一个有子的坑，把子全部拿起，
// 逆时针一个坑放一颗(跳过对方的大仓)。最后一颗落进自己的大仓就再走一次；落进自己一侧原本空着的坑、且正对面的坑有子时，
// 把这颗子和对面坑里的子全部收进自己的大仓。任意一方的小坑全空时一局结束，另一方把自己一侧剩下的子收进大仓，大仓里子多的
// 一方获胜，一样多为和棋。
//
// 坑的编号：0..5 是先手的小坑，6 是先手的大仓，7..12 是后手的小坑(12 正对 0)，13 是后手的大仓。一步记为轮到的一方自己的
// 第几个坑(0..5)。一局完全由依次选的坑决定：保存一局只需记下这些坑，读出时重放(Replay)，重放同时校验了每一步都合规则。
package mancala

import (
	"errors"
	"slices"
)

const (
	Pits  = 6
	Seeds = 4
	// Holes 是坑的总数(含两个大仓)，Total 是全盘的子数。
	Holes = 2*Pits + 2
	Total = 2 * Pits * Seeds

	firstStore  = Pits
	secondStore = 2*Pits + 1
)

// Player 是走棋的一方，First 先走。
type Player int8

const (
	Nobody Player = iota
	First
	Second
)

// Opponent 返回另一方。
func (p Player) Opponent() Player {
	if p == First {
		return Second
	}
	return First
}

var ErrIllegalMove = errors.New("illegal mancala move")

// Game 是一局播棋。
type Game struct {
	board [Holes]int
	turn  Player
	moves []int
	over  bool
	// last 是最后一步依次落子的坑(页面据此一颗颗播放)，captured 是最后一步吃掉的对面坑，没有时为 -1。
	last     []int
	captured int
	extra    bool
}

// New 返回开局局面，先手先走。
func New() *Game {
	g := &Game{turn: First, captured: -1}
	for i := 0; i < Pits; i++ {
		g.board[i] = Seeds
		g.board[firstStore+1+i] = Seeds
	}
	return g
}

// Replay 按顺序重放 moves，有一步不合规则时返回 ErrIllegalMove。
func Replay(moves []int) (*Game, error) {
	g := New()
	for _, pit := range moves {
		if err := g.Play(pit); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// storeOf、hole 把一方自己的第几个坑换成全盘编号。
func storeOf(p Player) int {
	if p == First {
		return firstStore
	}
	return secondStore
}

func hole(p Player, pit int) int {
	if p == First {
		return pit
	}
	return firstStore + 1 + pit
}

// owns 表示全盘编号 h 是 p 一侧的小坑。
func owns(p Player, h int) bool {
	if p == First {
		return h >= 0 && h < Pits
	}
	return h > firstStore && h < secondStore
}

// Play 让轮到的一方播自己第 pit 个坑里的子。
func (g *Game) Play(pit int) error {
	if g.over || pit < 0 || pit >= Pits {
		return ErrIllegalMove
	}
	player := g.turn
	h := hole(player, pit)
	seeds := g.board[h]
	if seeds == 0 {
		return ErrIllegalMove
	}
	g.board[h] = 0
	g.last = g.last[:0]
	g.captured = -1
	skip := storeOf(player.Opponent())
	for seeds > 0 {
		h = (h + 1) % Holes
		if h == skip {
			continue
		}
		g.board[h]++
		g.last = append(g.last, h)
		seeds--
	}
	g.moves = append(g.moves, pit)
	g.extra = h == storeOf(player)
	if owns(player, h) && g.board[h] == 1 {
		opposite := 2*Pits - h
		if g.board[opposite] > 0 {
			g.board[storeOf(player)] += g.board[opposite] + 1
			g.board[opposite], g.board[h] = 0, 0
			g.captured = opposite
		}
	}
	if g.sideEmpty(First) || g.sideEmpty(Second) {
		for i := 0; i < Pits; i++ {
			g.board[firstStore] += g.board[i]
			g.board[secondStore] += g.board[firstStore+1+i]
			g.board[i], g.board[firstStore+1+i] = 0, 0
		}
		g.over = true
		return nil
	}
	if !g.extra {
		g.turn = player.Opponent()
	}
	return nil
}

func (g *Game) sideEmpty(p Player) bool {
	for i := 0; i < Pits; i++ {
		if g.board[hole(p, i)] > 0 {
			return false
		}
	}
	return true
}

// Over 表示一局结束。
func (g *Game) Over() bool {
	return g.over
}

// Turn 是轮到走的一方，结束后为 Nobody。
func (g *Game) Turn() Player {
	if g.over {
		return Nobody
	}
	return g.turn
}

// Score 返回双方大仓里的子数。
func (g *Game) Score() (first int, second int) {
	return g.board[firstStore], g.board[secondStore]
}

// Winner 是结束时大仓子多的一方，没结束或一样多时为 Nobody。
func (g *Game) Winner() Player {
	if !g.over {
		return Nobody
	}
	first, second := g.Score()
	switch {
	case first > second:
		return First
	case second > first:
		return Second
	}
	return Nobody
}

// Board 返回全盘 Holes 个坑的子数。
func (g *Game) Board() []int {
	return slices.Clone(g.board[:])
}

// Moves 返回依次选过的坑。
func (g *Game) Moves() []int {
	return slices.Clone(g.moves)
}

// Last 返回最后一步依次落子的坑。
func (g *Game) Last() []int {
	return append([]int{}, g.last...)
}

// Captured 返回最后一步吃掉的对面坑，没有时为 -1。
func (g *Game) Captured() int {
	return g.captured
}

// Extra 表示最后一步落进自己的大仓、同一方接着走。
func (g *Game) Extra() bool {
	return g.extra && !g.over
}

// Legal 返回轮到的一方能选的坑。
func (g *Game) Legal() []int {
	if g.over {
		return nil
	}
	pits := make([]int, 0, Pits)
	for i := 0; i < Pits; i++ {
		if g.board[hole(g.turn, i)] > 0 {
			pits = append(pits, i)
		}
	}
	return pits
}

// clone 复制局面供搜索使用，不复制走过的步。
func (g *Game) clone() *Game {
	c := &Game{board: g.board, turn: g.turn, over: g.over, captured: -1}
	return c
}
