// Package dots 是游戏中心点格棋(Dots and Boxes)的规则与 AI。
//
// 棋盘是 Size × Size 个格子(Size + 1 行、Size + 1 列个点)，双方轮流在相邻两点之间连一条边；谁连上一个格子的第四条边就得到
// 这个格子，并且接着再连一条，没有围成格子则换对方。所有边都连完后格子多的一方获胜，一样多为和棋。一局完全由按顺序连的边
// 决定：保存一局只需记下边的编号，读出时重放(Replay)，重放同时校验了每一步都合规则。
//
// 边的编号：先是横边，第 r 行(0..Size)第 c 条(0..Size-1)是 r*Size + c；再是竖边，第 r 行(0..Size-1)第 c 条(0..Size)是
// HorizontalEdges + r*(Size+1) + c。格子按行编号 r*Size + c。
package dots

import (
	"errors"
	"strings"
)

const (
	Size = 4
	// HorizontalEdges、VerticalEdges 是横边、竖边的条数，Edges 是总边数，Boxes 是格子数。
	HorizontalEdges = (Size + 1) * Size
	VerticalEdges   = Size * (Size + 1)
	Edges           = HorizontalEdges + VerticalEdges
	Boxes           = Size * Size
)

// Player 是连边或占格的一方，First 先走。
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

var ErrIllegalMove = errors.New("illegal dots move")

// boxEdges[b] 是格子 b 的四条边；edgeBoxes[e] 是边 e 两侧的格子(棋盘边上的边只有一侧，另一个为 -1)。
var (
	boxEdges  [Boxes][4]int
	edgeBoxes [Edges][2]int
)

func init() {
	for e := range edgeBoxes {
		edgeBoxes[e] = [2]int{-1, -1}
	}
	for r := 0; r < Size; r++ {
		for c := 0; c < Size; c++ {
			box := r*Size + c
			edges := [4]int{
				r*Size + c,
				(r+1)*Size + c,
				HorizontalEdges + r*(Size+1) + c,
				HorizontalEdges + r*(Size+1) + c + 1,
			}
			boxEdges[box] = edges
			for _, e := range edges {
				if edgeBoxes[e][0] < 0 {
					edgeBoxes[e][0] = box
				} else {
					edgeBoxes[e][1] = box
				}
			}
		}
	}
}

// Game 是一局点格棋。
type Game struct {
	drawn  uint64
	owners [Boxes]Player
	turn   Player
	moves  []int
	// last 是最后一条边，没有时为 -1；claimed 是最后一步围下的格子。
	last    int
	claimed []int
}

// New 返回空棋盘，先手先走。
func New() *Game {
	return &Game{turn: First, last: -1}
}

// Replay 按顺序连 moves 里的边重放一局，有一步不合规则时返回 ErrIllegalMove。
func Replay(moves []int) (*Game, error) {
	g := New()
	for _, edge := range moves {
		if err := g.Play(edge); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// sides 返回格子 box 已经连上的边数。
func sides(drawn uint64, box int) int {
	count := 0
	for _, e := range boxEdges[box] {
		if drawn&(1<<e) != 0 {
			count++
		}
	}
	return count
}

// completes 返回在 drawn 上连 edge 会围下几个格子。
func completes(drawn uint64, edge int) int {
	count := 0
	for _, box := range edgeBoxes[edge] {
		if box >= 0 && sides(drawn, box) == 3 {
			count++
		}
	}
	return count
}

// Play 让轮到的一方连上 edge。围下格子时归他所有、他接着走，否则轮到对方。
func (g *Game) Play(edge int) error {
	if g.Over() || edge < 0 || edge >= Edges || g.drawn&(1<<edge) != 0 {
		return ErrIllegalMove
	}
	g.drawn |= 1 << edge
	g.moves = append(g.moves, edge)
	g.last = edge
	g.claimed = g.claimed[:0]
	for _, box := range edgeBoxes[edge] {
		if box >= 0 && sides(g.drawn, box) == 4 {
			g.owners[box] = g.turn
			g.claimed = append(g.claimed, box)
		}
	}
	if len(g.claimed) == 0 {
		g.turn = g.turn.Opponent()
	}
	return nil
}

// Over 表示所有边都已连完。
func (g *Game) Over() bool {
	return len(g.moves) == Edges
}

// Turn 是轮到走的一方，结束后为 Nobody。
func (g *Game) Turn() Player {
	if g.Over() {
		return Nobody
	}
	return g.turn
}

// Score 返回双方各占了几个格子。
func (g *Game) Score() (first int, second int) {
	for _, owner := range g.owners {
		switch owner {
		case First:
			first++
		case Second:
			second++
		}
	}
	return first, second
}

// Winner 是结束时格子多的一方，没结束或一样多时为 Nobody。
func (g *Game) Winner() Player {
	if !g.Over() {
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

// Moves 返回按顺序连过的边。
func (g *Game) Moves() []int {
	return append([]int(nil), g.moves...)
}

// Last 返回最后一条边，没有时为 -1。
func (g *Game) Last() int {
	return g.last
}

// Claimed 返回最后一步围下的格子。
func (g *Game) Claimed() []int {
	return append([]int{}, g.claimed...)
}

// Edges 返回 Edges 个字符，连上的边为 '1'，没连的为 '0'。
func (g *Game) EdgeString() string {
	var b strings.Builder
	for e := 0; e < Edges; e++ {
		if g.drawn&(1<<e) != 0 {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

// Owners 返回 Boxes 个字符，先手占的格子为 'a'，后手为 'b'，没人占的为 '.'。
func (g *Game) Owners() string {
	var b strings.Builder
	for _, owner := range g.owners {
		switch owner {
		case First:
			b.WriteByte('a')
		case Second:
			b.WriteByte('b')
		default:
			b.WriteByte('.')
		}
	}
	return b.String()
}
