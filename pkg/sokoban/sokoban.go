// Package sokoban 生成每日推箱子关卡，并重放玩家走法。关卡由已完成局面反向拉箱生成，因此一定可解。
package sokoban

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"slices"
	"time"
)

const MaxMoves = 4096
const MaxMovesPerSecond = 12

var ErrMove = errors.New("invalid sokoban move")

type Game struct {
	Day    string `json:"day"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Walls  []int  `json:"walls"`
	Goals  []int  `json:"goals"`
	Boxes  []int  `json:"boxes"`
	Player int    `json:"player"`
	Pushes int    `json:"pushes"`
	// 生成时的可解证据，不序列化到对局或浏览器；测试用正向规则验证。
	solution string
}

func New(day string) (*Game, error) {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte("new-api/sokoban/v1/" + day))
	rng := rand.New(rand.NewPCG(binary.BigEndian.Uint64(hash[:8]), binary.BigEndian.Uint64(hash[8:16])))
	g := &Game{Day: day, Width: 8, Height: 8, Goals: []int{18, 21, 43}, Boxes: []int{18, 21, 43}, Player: 35}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if row == 0 || row == 7 || col == 0 || col == 7 {
				g.Walls = append(g.Walls, row*8+col)
			}
		}
	}
	// 两种通道，所有目标都在可反向拉出的内部地面。
	if hash[16]%2 == 0 {
		g.Walls = append(g.Walls, 27, 28)
	} else {
		g.Walls = append(g.Walls, 34, 37)
	}
	var reverse []byte
	for range 700 {
		direction := "UDLR"[rng.IntN(4)]
		delta := g.delta(direction)
		next := g.Player + delta
		if !g.floor(next) || slices.Contains(g.Boxes, next) {
			continue
		}
		if index := slices.Index(g.Boxes, g.Player-delta); index >= 0 {
			g.Boxes[index] = g.Player
		}
		g.Player = next
		switch direction {
		case 'U':
			reverse = append(reverse, 'D')
		case 'D':
			reverse = append(reverse, 'U')
		case 'L':
			reverse = append(reverse, 'R')
		case 'R':
			reverse = append(reverse, 'L')
		}
	}
	if g.Solved() {
		// 固定的可解备用局面，保证不会发出已经完成的关卡。
		g.Boxes = []int{26, 21, 43}
		g.Player = 34
		reverse = []byte{'U'}
		g.Walls = slices.DeleteFunc(g.Walls, func(v int) bool { return v == 34 })
	}
	slices.Reverse(reverse)
	g.solution = string(reverse)
	return g, nil
}

func (g *Game) delta(direction byte) int {
	switch direction {
	case 'U':
		return -g.Width
	case 'D':
		return g.Width
	case 'L':
		return -1
	case 'R':
		return 1
	}
	return 0
}

func (g *Game) floor(position int) bool {
	return position >= 0 && position < g.Width*g.Height && !slices.Contains(g.Walls, position)
}

func (g *Game) Move(direction byte) error {
	delta := g.delta(direction)
	next := g.Player + delta
	if delta == 0 || !g.floor(next) || (delta == 1 || delta == -1) && next/g.Width != g.Player/g.Width {
		return ErrMove
	}
	if index := slices.Index(g.Boxes, next); index >= 0 {
		after := next + delta
		if !g.floor(after) || slices.Contains(g.Boxes, after) || (delta == 1 || delta == -1) && after/g.Width != next/g.Width {
			return ErrMove
		}
		g.Boxes[index] = after
		g.Pushes++
	}
	g.Player = next
	return nil
}

func (g *Game) Solved() bool {
	if len(g.Boxes) == 0 || len(g.Boxes) != len(g.Goals) {
		return false
	}
	for _, box := range g.Boxes {
		if !slices.Contains(g.Goals, box) {
			return false
		}
	}
	return true
}

func (g *Game) Replay(moves string) (*Game, error) {
	if len(moves) == 0 || len(moves) > MaxMoves {
		return nil, ErrMove
	}
	result := *g
	result.Boxes = slices.Clone(g.Boxes)
	for i := range moves {
		if err := result.Move(moves[i]); err != nil {
			return nil, err
		}
	}
	return &result, nil
}
