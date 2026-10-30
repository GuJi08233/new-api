// Package onestroke 是游戏中心一笔画的规则。
//
// Size × Size 的格子里有几块石头，玩家从给定的起点出发，每次走到上下左右相邻、还没走过的空格，一笔走遍所有空格即过关。
// 初盘由会话种子决定：先随机放石头，再用回溯搜索找一条走遍其余空格的路线，找到才发这一盘，所以每一盘都有解。路线可以
// 不止一条，玩家走出任意一条都算过关。存档里只记依次走到的格子，读出时重放校验。
package onestroke

import (
	"errors"
	"math/rand/v2"
	"slices"
)

const (
	// MaxSize 是最大的边长，格子编号 row*Size + col 不超过 MaxSize*MaxSize。
	MaxSize = 7
	// searchBudget 是给一种石头摆法找路线时最多展开的步数，超出就换一种摆法。
	searchBudget = 500
)

// Levels 是各难度(1..3)的边长与石头数范围。
var Levels = [4]struct {
	Size      int
	MinStones int
	MaxStones int
}{
	{},
	{Size: 5, MinStones: 2, MaxStones: 4},
	{Size: 6, MinStones: 3, MaxStones: 6},
	{Size: 7, MinStones: 4, MaxStones: 8},
}

var ErrMove = errors.New("invalid one stroke move")

// Game 是一局一笔画。Stones[i] 为真表示格子 i 是石头；Path 是依次走过的格子，第一个是起点。
type Game struct {
	Difficulty int    `json:"difficulty"`
	Size       int    `json:"size"`
	Stones     []bool `json:"stones"`
	Path       []int  `json:"path"`
	// route 是生成时找到的一条路线，只给测试核对。
	route []int
}

// New 由种子生成一盘有解的初盘，Path 里只有起点。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	level := Levels[difficulty]
	size := level.Size
	rng := rand.New(rand.NewPCG(seed, 67))
	for {
		stones := make([]bool, size*size)
		count := level.MinStones + rng.IntN(level.MaxStones-level.MinStones+1)
		for _, cell := range rng.Perm(size * size)[:count] {
			stones[cell] = true
		}
		starts := routeStarts(size, stones)
		if len(starts) == 0 {
			continue
		}
		start := starts[rng.IntN(len(starts))]
		if route := findRoute(size, stones, start, rng); route != nil {
			return &Game{Difficulty: difficulty, Size: size, Stones: stones, Path: []int{start}, route: route}, nil
		}
	}
}

// routeStarts 用两条必要条件筛掉一定无解的摆法，返回可以试着出发的格子；一定无解时为空。
//   - 棋盘按国际象棋染色，路线黑白相间：两色空格数最多差一，差一时起点必须在多的那一色上。
//   - 只有一个空邻居的格子只能是路线的两端：这样的格子最多两个，有两个时起点只能是其中之一；孤立的空格无解。
func routeStarts(size int, stones []bool) []int {
	var colors [2]int
	var ends []int
	open := make([]int, 0, len(stones))
	for cell, stone := range stones {
		if stone {
			continue
		}
		open = append(open, cell)
		colors[(cell/size+cell%size)%2]++
		degree := 0
		for _, next := range neighbors(size, cell) {
			if !stones[next] {
				degree++
			}
		}
		if degree == 0 {
			return nil
		}
		if degree == 1 {
			ends = append(ends, cell)
		}
	}
	if len(ends) > 2 || colors[0]-colors[1] > 1 || colors[1]-colors[0] > 1 {
		return nil
	}
	candidates := open
	if len(ends) == 2 {
		candidates = ends
	}
	starts := make([]int, 0, len(candidates))
	for _, cell := range candidates {
		color := (cell/size + cell%size) % 2
		if colors[color] >= colors[1-color] {
			starts = append(starts, cell)
		}
	}
	return starts
}

func neighbors(size, cell int) []int {
	row, col := cell/size, cell%size
	result := make([]int, 0, 4)
	if row > 0 {
		result = append(result, cell-size)
	}
	if row < size-1 {
		result = append(result, cell+size)
	}
	if col > 0 {
		result = append(result, cell-1)
	}
	if col < size-1 {
		result = append(result, cell+1)
	}
	return result
}

// findRoute 从 start 出发找一条走遍所有空格的路线：每步优先走出路最少的格子(Warnsdorff 规则)，出路一样多时随机，
// 不行就回溯，最多展开 searchBudget 步。找不到返回 nil。
func findRoute(size int, stones []bool, start int, rng *rand.Rand) []int {
	visited := slices.Clone(stones)
	total := 0
	for _, stone := range stones {
		if !stone {
			total++
		}
	}
	route := []int{start}
	visited[start] = true
	budget := searchBudget
	degree := func(cell int) int {
		n := 0
		for _, next := range neighbors(size, cell) {
			if !visited[next] {
				n++
			}
		}
		return n
	}
	var search func(cell int) bool
	search = func(cell int) bool {
		if len(route) == total {
			return true
		}
		if budget <= 0 {
			return false
		}
		budget--
		candidates := make([]int, 0, 4)
		for _, next := range neighbors(size, cell) {
			if !visited[next] {
				candidates = append(candidates, next)
			}
		}
		rng.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		slices.SortStableFunc(candidates, func(a, b int) int { return degree(a) - degree(b) })
		for _, next := range candidates {
			visited[next] = true
			route = append(route, next)
			if search(next) {
				return true
			}
			route = route[:len(route)-1]
			visited[next] = false
		}
		return false
	}
	if search(start) {
		return route
	}
	return nil
}

// Step 从路线末端走到 cell：必须与末端相邻、是空格、还没走过。
func (g *Game) Step(cell int) error {
	if cell < 0 || cell >= len(g.Stones) || g.Stones[cell] || slices.Contains(g.Path, cell) || g.Solved() {
		return ErrMove
	}
	if !slices.Contains(neighbors(g.Size, g.Path[len(g.Path)-1]), cell) {
		return ErrMove
	}
	g.Path = append(g.Path, cell)
	return nil
}

// Open 是空格的个数。
func (g *Game) Open() int {
	n := 0
	for _, stone := range g.Stones {
		if !stone {
			n++
		}
	}
	return n
}

// Solved 表示已经走遍所有空格。
func (g *Game) Solved() bool {
	return len(g.Path) == g.Open()
}

// Stuck 表示还没走完，末端却已经无路可走，只能撤回。
func (g *Game) Stuck() bool {
	if g.Solved() {
		return false
	}
	for _, next := range neighbors(g.Size, g.Path[len(g.Path)-1]) {
		if !g.Stones[next] && !slices.Contains(g.Path, next) {
			return false
		}
	}
	return true
}
