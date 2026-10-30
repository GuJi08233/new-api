// Package watersort 是游戏中心倒水排序的规则。
//
// 若干支试管各装 Capacity 层颜色，另有两支空管。一次把一支试管顶上连续同色的几层倒进另一支：目标管要么是空的，要么顶层同色，
// 且只倒得下还剩的空位。每支试管要么空、要么装满同一种颜色即过关。初盘由会话种子打乱，只发能解开的盘面(用求解器验证)，
// 读出时由种子重新生成，存档里只记倒过的每一步。
package watersort

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
)

const (
	Capacity = 4
	// MaxMoves 是一局最多倒几次，远多于解开任何一盘需要的步数。
	MaxMoves = 400
	// EmptyTubes 是开局时空着的试管数。
	EmptyTubes = 2
	// solverBudget 是验证一盘能否解开时最多展开的局面数，超出算作解不开、换一盘。
	solverBudget = 60000
)

// Colors 是各难度(1..3)的颜色数，试管数是颜色数加 EmptyTubes。
var Colors = [4]int{0, 4, 6, 8}

var ErrMove = errors.New("invalid water sort move")

// Game 是一局倒水排序。Tubes[i] 从管底到管口排列，颜色从 1 开始编号。
type Game struct {
	Difficulty int      `json:"difficulty"`
	Tubes      [][]int8 `json:"tubes"`
	Moves      int      `json:"moves"`
}

// New 由种子生成一盘能解开的初盘。同一种子总是同一盘。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	colors := Colors[difficulty]
	rng := rand.New(rand.NewPCG(seed, 59))
	layers := make([]int8, 0, colors*Capacity)
	for color := 1; color <= colors; color++ {
		for i := 0; i < Capacity; i++ {
			layers = append(layers, int8(color))
		}
	}
	for {
		rng.Shuffle(len(layers), func(i, j int) { layers[i], layers[j] = layers[j], layers[i] })
		tubes := make([][]int8, colors+EmptyTubes)
		for i := 0; i < colors; i++ {
			tubes[i] = slices.Clone(layers[i*Capacity : (i+1)*Capacity])
		}
		for i := colors; i < len(tubes); i++ {
			tubes[i] = []int8{}
		}
		g := &Game{Difficulty: difficulty, Tubes: tubes}
		if !g.anyTubeDone() && solvable(g.Tubes) {
			return g, nil
		}
	}
}

// anyTubeDone 表示有一支试管一开始就是满的同色，这样的盘面太容易，换一盘。
func (g *Game) anyTubeDone() bool {
	for _, tube := range g.Tubes {
		if len(tube) == Capacity && uniform(tube) {
			return true
		}
	}
	return false
}

func uniform(tube []int8) bool {
	for _, layer := range tube {
		if layer != tube[0] {
			return false
		}
	}
	return true
}

// topRun 返回试管顶上连续同色的层数。
func topRun(tube []int8) int {
	n := 0
	for i := len(tube) - 1; i >= 0 && tube[i] == tube[len(tube)-1]; i-- {
		n++
	}
	return n
}

// pourable 返回从 from 倒进 to 能倒几层，倒不了时为 0。
func pourable(tubes [][]int8, from, to int) int {
	if from == to {
		return 0
	}
	source, target := tubes[from], tubes[to]
	if len(source) == 0 || len(target) >= Capacity {
		return 0
	}
	if len(target) > 0 && target[len(target)-1] != source[len(source)-1] {
		return 0
	}
	return min(topRun(source), Capacity-len(target))
}

func pour(tubes [][]int8, from, to, amount int) {
	source := tubes[from]
	tubes[to] = append(tubes[to], source[len(source)-amount:]...)
	tubes[from] = source[:len(source)-amount]
}

// Pour 把 from 顶上的颜色倒进 to，返回倒了几层。
func (g *Game) Pour(from, to int) (int, error) {
	if from < 0 || to < 0 || from >= len(g.Tubes) || to >= len(g.Tubes) || g.Moves >= MaxMoves || g.Solved() {
		return 0, ErrMove
	}
	amount := pourable(g.Tubes, from, to)
	if amount == 0 {
		return 0, ErrMove
	}
	pour(g.Tubes, from, to, amount)
	g.Moves++
	return amount, nil
}

// Solved 表示每支试管要么空、要么装满同一种颜色。
func (g *Game) Solved() bool {
	return solved(g.Tubes)
}

func solved(tubes [][]int8) bool {
	for _, tube := range tubes {
		if len(tube) != 0 && (len(tube) != Capacity || !uniform(tube)) {
			return false
		}
	}
	return true
}

// HasMove 表示还有能倒的一步。没有时只能撤销或重来。
func (g *Game) HasMove() bool {
	for from := range g.Tubes {
		for to := range g.Tubes {
			if pourable(g.Tubes, from, to) > 0 {
				return true
			}
		}
	}
	return false
}

// key 是局面的规范写法：试管顺序无关，排序后拼起来。
func key(tubes [][]int8) string {
	parts := make([]string, len(tubes))
	for i, tube := range tubes {
		var b strings.Builder
		for _, layer := range tube {
			b.WriteByte(byte('a' + layer))
		}
		parts[i] = b.String()
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

// solvable 用深度优先搜索验证盘面能解开，最多展开 solverBudget 个局面。跳过明显无用的倒法：整管同色倒进空管、
// 倒不完的一段倒进别的管子(会拆散同色)。
func solvable(start [][]int8) bool {
	seen := make(map[string]bool)
	budget := solverBudget
	var search func(tubes [][]int8) bool
	search = func(tubes [][]int8) bool {
		if solved(tubes) {
			return true
		}
		k := key(tubes)
		if seen[k] || budget <= 0 {
			return false
		}
		seen[k] = true
		budget--
		for from := range tubes {
			source := tubes[from]
			if len(source) == 0 || (len(source) == Capacity && uniform(source)) {
				continue
			}
			run := topRun(source)
			for to := range tubes {
				amount := pourable(tubes, from, to)
				if amount == 0 || amount < run && len(tubes[to]) > 0 {
					continue
				}
				if len(tubes[to]) == 0 && run == len(source) {
					continue
				}
				next := make([][]int8, len(tubes))
				for i, tube := range tubes {
					next[i] = slices.Clone(tube)
				}
				pour(next, from, to, amount)
				if search(next) {
					return true
				}
			}
		}
		return false
	}
	return search(start)
}
