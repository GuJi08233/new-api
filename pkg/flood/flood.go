// Package flood 实现色块漫染（Flood It）解谜规则：从左上角连通区域出发，每次选择一种新颜色将整片区域染色并吞并相邻同色格，在限定步数内染满全盘即获胜。
package flood

import (
	"errors"
	"math/rand/v2"
)

var ErrMove = errors.New("invalid flood move")

type Level struct {
	Size     int `json:"size"`
	Colors   int `json:"colors"`
	MaxMoves int `json:"max_moves"`
}

// Levels 按难度编号(1..3)定义棋盘尺寸、颜色数与步数上限。
var Levels = [4]Level{
	{},
	{Size: 8, Colors: 6, MaxMoves: 16},
	{Size: 10, Colors: 6, MaxMoves: 20},
	{Size: 12, Colors: 6, MaxMoves: 24},
}

type Game struct {
	Difficulty int   `json:"difficulty"`
	Size       int   `json:"size"`
	Colors     int   `json:"colors"`
	MaxMoves   int   `json:"max_moves"`
	Board      []int `json:"board"`
	Moves      int   `json:"moves"`
}

// New 根据难度与种子生成保证在步数上限内可解的初始棋盘。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	level := Levels[difficulty]
	cells := level.Size * level.Size
	rng := rand.New(rand.NewPCG(seed, 73))

	// 只接受贪心求解在步数上限内染满的棋盘；贪心解不出的盘不一定无解，但不能保证有解，宁可退回下面的保底盘。
	var bestBoard []int
	board := make([]int, cells)
	for attempt := 0; attempt < 64; attempt++ {
		for i := range board {
			board[i] = rng.IntN(level.Colors)
		}
		if regionCount(board, level.Size) >= cells {
			continue
		}
		if greedySolve(board, level.Size, level.Colors, level.MaxMoves) <= level.MaxMoves {
			bestBoard = append([]int(nil), board...)
			break
		}
	}
	if bestBoard == nil {
		// 极端情况下退回到确定性可解棋盘（按斜线分色，每步吞并一条斜线，2·Size−2 步染满，不超过各难度的步数上限）。
		bestBoard = make([]int, cells)
		for i := range bestBoard {
			bestBoard[i] = ((i / level.Size) + (i % level.Size)) % level.Colors
		}
	}
	return &Game{
		Difficulty: difficulty,
		Size:       level.Size,
		Colors:     level.Colors,
		MaxMoves:   level.MaxMoves,
		Board:      bestBoard,
	}, nil
}

// Flood 将左上角连通区域染成 color。不能重复选择当前颜色，也不能在结束后继续落子。
func (g *Game) Flood(color int) error {
	if g.Size <= 0 || len(g.Board) != g.Size*g.Size || color < 0 || color >= g.Colors || g.Over() {
		return ErrMove
	}
	current := g.Board[0]
	if color == current {
		return ErrMove
	}
	region := collectRegion(g.Board, g.Size)
	for _, idx := range region {
		g.Board[idx] = color
	}
	g.Moves++
	return nil
}

// Captured 返回当前已归入左上角连通区域的格子数。
func (g *Game) Captured() int {
	if g.Size <= 0 || len(g.Board) != g.Size*g.Size {
		return 0
	}
	return regionCount(g.Board, g.Size)
}

func (g *Game) Solved() bool {
	return len(g.Board) > 0 && g.Captured() == len(g.Board)
}

func (g *Game) Over() bool {
	return g.Solved() || g.Moves >= g.MaxMoves
}

func (g *Game) MovesLeft() int {
	return max(0, g.MaxMoves-g.Moves)
}

func collectRegion(board []int, size int) []int {
	target := board[0]
	visited := make([]bool, len(board))
	queue := make([]int, 0, len(board))
	visited[0] = true
	queue = append(queue, 0)
	for head := 0; head < len(queue); head++ {
		idx := queue[head]
		x, y := idx%size, idx/size
		if x > 0 {
			if n := idx - 1; !visited[n] && board[n] == target {
				visited[n] = true
				queue = append(queue, n)
			}
		}
		if x+1 < size {
			if n := idx + 1; !visited[n] && board[n] == target {
				visited[n] = true
				queue = append(queue, n)
			}
		}
		if y > 0 {
			if n := idx - size; !visited[n] && board[n] == target {
				visited[n] = true
				queue = append(queue, n)
			}
		}
		if y+1 < size {
			if n := idx + size; !visited[n] && board[n] == target {
				visited[n] = true
				queue = append(queue, n)
			}
		}
	}
	return queue
}

func regionCount(board []int, size int) int {
	return len(collectRegion(board, size))
}

// greedySolve 使用贪心策略模拟求解：每步选择能扩张最多领地的颜色。若在 limit 步内求解成功返回实际步数，否则返回 limit+1。
func greedySolve(src []int, size int, colors int, limit int) int {
	board := append([]int(nil), src...)
	total := len(board)
	for step := 1; step <= limit; step++ {
		current := board[0]
		region := collectRegion(board, size)
		if len(region) == total {
			return step - 1
		}
		bestColor := -1
		bestCount := len(region)
		for c := 0; c < colors; c++ {
			if c == current {
				continue
			}
			for _, idx := range region {
				board[idx] = c
			}
			count := regionCount(board, size)
			if count > bestCount {
				bestCount = count
				bestColor = c
			}
		}
		if bestColor < 0 {
			return limit + 1
		}
		for _, idx := range region {
			board[idx] = bestColor
		}
		if bestCount == total {
			return step
		}
	}
	return limit + 1
}
