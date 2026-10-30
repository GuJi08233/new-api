// Package match3 是游戏中心宝石消除的规则。
//
// 8×8 的宝石，交换上下或左右相邻的两颗，交换后连成三颗及以上同色才算数。连成的宝石消掉，上方的落下，空位从顶上补新的；
// 落下后又连成的继续消，每多连锁一轮得分倍数加一。四连留下一颗横向(或纵向)的直线宝石，消掉时清空整行(列)；五连或 L、T
// 形留下一颗炸弹，消掉时炸掉周围 3×3。步数用完之前分数达到目标即过关。
//
// 初盘与补上来的宝石都来自会话种子的同一条随机数序列，所以一局完全由依次交换的位置决定：存档只记交换，读出时从头重放。
// 没有能消的交换时，用同一条序列把宝石重新洗一遍。
package match3

import (
	"errors"
	"math/rand/v2"
	"slices"
)

const (
	Size  = 8
	Cells = Size * Size

	// 宝石的值：低 3 位是颜色(1 起)，再往上是特殊宝石的种类。0 只在消除过程中表示空位。
	colorMask = 7
	kindShift = 3

	KindPlain = 0
	KindRow   = 1
	KindCol   = 2
	KindBomb  = 3

	// PointsPerGem 是每消一颗宝石的基础分，乘以连锁轮数。
	PointsPerGem = 10
)

// Levels 是各难度(1..3)的颜色数、步数与目标分。目标分按模拟定：每步随机交换几乎达不到，每步挑当下得分最高的一步
// 在简单、普通、困难下大约九成、六成、三成能达到，会做特殊宝石的玩家更容易。
var Levels = [4]struct {
	Colors int `json:"colors"`
	Moves  int `json:"moves"`
	Target int `json:"target"`
}{
	{},
	{Colors: 5, Moves: 20, Target: 4000},
	{Colors: 6, Moves: 20, Target: 2500},
	{Colors: 6, Moves: 18, Target: 3000},
}

var ErrMove = errors.New("invalid match3 move")

// Created 是一轮消除后留下的特殊宝石。
type Created struct {
	Cell int `json:"cell"`
	Gem  int `json:"gem"`
}

// Phase 是一轮消除：消掉的格子、留下的特殊宝石、这一轮的得分与连锁轮数，以及落下补齐之后的盘面。页面按轮播放动画。
type Phase struct {
	Cleared []int     `json:"cleared"`
	Created []Created `json:"created"`
	Gained  int       `json:"gained"`
	Chain   int       `json:"chain"`
	Board   []int     `json:"board"`
}

// Game 是一局宝石消除。Phases 是最后一次交换引起的各轮消除，Shuffled 表示最后一次交换之后没有能消的交换、重新洗过。
type Game struct {
	Difficulty int     `json:"difficulty"`
	Board      []int   `json:"board"`
	Score      int     `json:"score"`
	Moves      int     `json:"moves"`
	Phases     []Phase `json:"phases"`
	Shuffled   bool    `json:"shuffled"`
	rng        *rand.Rand
}

func colorOf(gem int) int { return gem & colorMask }

func kindOf(gem int) int { return gem >> kindShift }

// New 由种子生成初盘：没有现成的三连，至少有一步能消。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	g := &Game{Difficulty: difficulty, Board: make([]int, Cells), rng: rand.New(rand.NewPCG(seed, 73))}
	for {
		for cell := range g.Board {
			row, col := cell/Size, cell%Size
			for {
				color := 1 + g.rng.IntN(Levels[difficulty].Colors)
				left := col >= 2 && colorOf(g.Board[cell-1]) == color && colorOf(g.Board[cell-2]) == color
				up := row >= 2 && colorOf(g.Board[cell-Size]) == color && colorOf(g.Board[cell-2*Size]) == color
				if !left && !up {
					g.Board[cell] = color
					break
				}
			}
		}
		if g.HasMove() {
			return g, nil
		}
	}
}

// partner 返回交换的另一颗：dir 0 是右边一颗，1 是下面一颗；越界返回 -1。
func partner(cell, dir int) int {
	switch {
	case dir == 0 && cell%Size < Size-1:
		return cell + 1
	case dir == 1 && cell/Size < Size-1:
		return cell + Size
	}
	return -1
}

// group 是连在一起的一组同色宝石：横向、纵向的三连有公共格子时并成一组。
type group struct {
	cells      []int
	horizontal bool
	vertical   bool
	longest    int
	longestDir int
}

// findGroups 找出盘面上所有三连及以上的组。
func findGroups(board []int) []group {
	parent := make([]int, Cells)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	inRun := make([]bool, Cells)
	type run struct{ start, length, step int }
	runs := make([]run, 0)
	for line := 0; line < Size; line++ {
		for _, step := range []int{1, Size} {
			var first, stride int
			if step == 1 {
				first, stride = line*Size, 1
			} else {
				first, stride = line, Size
			}
			for i := 0; i < Size; {
				cell := first + i*stride
				color := colorOf(board[cell])
				j := i + 1
				for j < Size && color != 0 && colorOf(board[first+j*stride]) == color {
					j++
				}
				if color != 0 && j-i >= 3 {
					runs = append(runs, run{start: cell, length: j - i, step: step})
				}
				i = j
			}
		}
	}
	for _, r := range runs {
		for k := 0; k < r.length; k++ {
			cell := r.start + k*r.step
			inRun[cell] = true
			parent[find(cell)] = find(r.start)
		}
	}
	byRoot := make(map[int]*group)
	order := make([]int, 0)
	for cell := 0; cell < Cells; cell++ {
		if !inRun[cell] {
			continue
		}
		root := find(cell)
		if byRoot[root] == nil {
			byRoot[root] = &group{}
			order = append(order, root)
		}
		byRoot[root].cells = append(byRoot[root].cells, cell)
	}
	for _, r := range runs {
		g := byRoot[find(r.start)]
		if r.step == 1 {
			g.horizontal = true
		} else {
			g.vertical = true
		}
		if r.length > g.longest {
			g.longest, g.longestDir = r.length, r.step
		}
	}
	groups := make([]group, 0, len(order))
	for _, root := range order {
		groups = append(groups, *byRoot[root])
	}
	return groups
}

// special 返回这一组留下的特殊宝石种类：五连或横纵交叉留炸弹，四连留直线宝石(横着连成的清整行，竖着连成的清整列)。
func (g group) special() int {
	switch {
	case g.longest >= 5 || g.horizontal && g.vertical:
		return KindBomb
	case g.longest == 4 && g.longestDir == 1:
		return KindRow
	case g.longest == 4:
		return KindCol
	}
	return KindPlain
}

// blast 返回特殊宝石在 cell 被消掉时波及的格子。
func blast(gem, cell int) []int {
	row, col := cell/Size, cell%Size
	cells := make([]int, 0, Size)
	switch kindOf(gem) {
	case KindRow:
		for c := 0; c < Size; c++ {
			cells = append(cells, row*Size+c)
		}
	case KindCol:
		for r := 0; r < Size; r++ {
			cells = append(cells, r*Size+col)
		}
	case KindBomb:
		for r := max(0, row-1); r <= min(Size-1, row+1); r++ {
			for c := max(0, col-1); c <= min(Size-1, col+1); c++ {
				cells = append(cells, r*Size+c)
			}
		}
	}
	return cells
}

// Swap 交换 cell 与它右边(dir 0)或下面(dir 1)的一颗。交换后连不成三颗时不算数。
func (g *Game) Swap(cell, dir int) error {
	if g.Over() || cell < 0 || cell >= Cells {
		return ErrMove
	}
	other := partner(cell, dir)
	if other < 0 {
		return ErrMove
	}
	g.Board[cell], g.Board[other] = g.Board[other], g.Board[cell]
	if len(findGroups(g.Board)) == 0 {
		g.Board[cell], g.Board[other] = g.Board[other], g.Board[cell]
		return ErrMove
	}
	g.Moves++
	g.Phases = g.resolve([2]int{other, cell})
	g.Shuffled = false
	if !g.Over() && !g.HasMove() {
		g.shuffle()
		g.Shuffled = true
	}
	return nil
}

// resolve 一轮轮消除直到盘面没有三连。swapped 是刚交换的两格，特殊宝石优先留在这里。
func (g *Game) resolve(swapped [2]int) []Phase {
	phases := make([]Phase, 0, 2)
	for chain := 1; ; chain++ {
		groups := findGroups(g.Board)
		if len(groups) == 0 {
			return phases
		}
		clear := make([]bool, Cells)
		created := make([]Created, 0)
		for _, gr := range groups {
			for _, cell := range gr.cells {
				clear[cell] = true
			}
			kind := gr.special()
			if kind == KindPlain {
				continue
			}
			pos := gr.cells[len(gr.cells)/2]
			if chain == 1 {
				for _, cell := range swapped {
					if slices.Contains(gr.cells, cell) {
						pos = cell
						break
					}
				}
			}
			created = append(created, Created{Cell: pos, Gem: kind<<kindShift | colorOf(g.Board[pos])})
		}
		// 被消掉的特殊宝石引爆，波及到的特殊宝石接着引爆。
		queue := make([]int, 0)
		for cell, on := range clear {
			if on {
				queue = append(queue, cell)
			}
		}
		triggered := make([]bool, Cells)
		for len(queue) > 0 {
			cell := queue[0]
			queue = queue[1:]
			if triggered[cell] || kindOf(g.Board[cell]) == KindPlain {
				continue
			}
			triggered[cell] = true
			for _, hit := range blast(g.Board[cell], cell) {
				if !clear[hit] && g.Board[hit] != 0 {
					clear[hit] = true
				}
				queue = append(queue, hit)
			}
		}
		for _, c := range created {
			clear[c.Cell] = false
		}
		cleared := make([]int, 0)
		for cell, on := range clear {
			if on {
				cleared = append(cleared, cell)
				g.Board[cell] = 0
			}
		}
		for _, c := range created {
			g.Board[c.Cell] = c.Gem
		}
		gained := (len(cleared) + len(created)) * PointsPerGem * chain
		g.Score += gained
		g.fall()
		phases = append(phases, Phase{Cleared: cleared, Created: created, Gained: gained, Chain: chain, Board: slices.Clone(g.Board)})
	}
}

// fall 让宝石落到底，空位从顶上补随机颜色的普通宝石。
func (g *Game) fall() {
	colors := Levels[g.Difficulty].Colors
	for col := 0; col < Size; col++ {
		write := Size - 1
		for row := Size - 1; row >= 0; row-- {
			if gem := g.Board[row*Size+col]; gem != 0 {
				g.Board[write*Size+col] = gem
				write--
			}
		}
		for row := write; row >= 0; row-- {
			g.Board[row*Size+col] = 1 + g.rng.IntN(colors)
		}
	}
}

// shuffle 把宝石重新打乱，直到没有现成的三连且有能消的一步。
func (g *Game) shuffle() {
	for {
		g.rng.Shuffle(Cells, func(i, j int) { g.Board[i], g.Board[j] = g.Board[j], g.Board[i] })
		if len(findGroups(g.Board)) == 0 && g.HasMove() {
			return
		}
	}
}

// HasMove 表示有一步交换能连成三颗。
func (g *Game) HasMove() bool {
	for cell := 0; cell < Cells; cell++ {
		for dir := 0; dir < 2; dir++ {
			other := partner(cell, dir)
			if other < 0 {
				continue
			}
			g.Board[cell], g.Board[other] = g.Board[other], g.Board[cell]
			found := len(findGroups(g.Board)) > 0
			g.Board[cell], g.Board[other] = g.Board[other], g.Board[cell]
			if found {
				return true
			}
		}
	}
	return false
}

// Won 表示分数达到了目标。
func (g *Game) Won() bool {
	return g.Score >= Levels[g.Difficulty].Target
}

// Over 表示这一局结束了：过关或步数用完。
func (g *Game) Over() bool {
	return g.Won() || g.Moves >= Levels[g.Difficulty].Moves
}

// MovesLeft 是还剩几步。
func (g *Game) MovesLeft() int {
	return max(0, Levels[g.Difficulty].Moves-g.Moves)
}
