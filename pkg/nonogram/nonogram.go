// Package nonogram 是游戏中心数织(Nonogram)的规则：方格旁边的数字是这一行(列)里连续涂黑的格子各有几个、按顺序排列，
// 玩家按提示涂格子，所有行、列都与提示相符即过关(不要求和出题的图一模一样)。
//
// 三档难度：5×5、8×8、10×10。题目由会话种子生成：随机涂一张图，只留下仅靠逐行逐列推理(不用猜)就能唯一解出的图，
// 玩家总能一步步推出来。
package nonogram

import (
	"errors"
	"math/rand/v2"
)

const MaxMoves = 2000

var (
	ErrDifficulty = errors.New("invalid nonogram difficulty")
	ErrMove       = errors.New("invalid nonogram move")
)

// Sizes[d] 是难度 d 的边长。
var Sizes = [4]int{1: 5, 2: 8, 3: 10}

// Puzzle 是一道题：边长与每行、每列的提示。
type Puzzle struct {
	Size int     `json:"size"`
	Rows [][]int `json:"rows"`
	Cols [][]int `json:"cols"`
}

// Game 是一局数织：题目与玩家涂黑的格子。
type Game struct {
	Puzzle
	Filled []bool `json:"filled"`
	Moves  int    `json:"moves"`
}

// runs 返回一行格子里连续涂黑的各段长度，没有涂黑的格子时为空。
func runs(line []bool) []int {
	result := []int{}
	count := 0
	for _, on := range line {
		if on {
			count++
		} else if count > 0 {
			result = append(result, count)
			count = 0
		}
	}
	if count > 0 {
		result = append(result, count)
	}
	return result
}

func row(grid []bool, size, r int) []bool { return grid[r*size : (r+1)*size] }

func col(grid []bool, size, c int) []bool {
	line := make([]bool, size)
	for r := 0; r < size; r++ {
		line[r] = grid[r*size+c]
	}
	return line
}

func cluesOf(grid []bool, size int) Puzzle {
	p := Puzzle{Size: size, Rows: make([][]int, size), Cols: make([][]int, size)}
	for i := 0; i < size; i++ {
		p.Rows[i] = runs(row(grid, size, i))
		p.Cols[i] = runs(col(grid, size, i))
	}
	return p
}

// New 按难度与种子出题。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrDifficulty
	}
	size := Sizes[difficulty]
	rng := rand.New(rand.NewPCG(seed, uint64(difficulty)))
	for {
		grid := make([]bool, size*size)
		for i := range grid {
			grid[i] = rng.IntN(100) < 60
		}
		puzzle := cluesOf(grid, size)
		if LineSolvable(puzzle) {
			return &Game{Puzzle: puzzle, Filled: make([]bool, size*size)}, nil
		}
	}
}

// Toggle 涂黑或擦掉一格。
func (g *Game) Toggle(cell int) error {
	if cell < 0 || cell >= len(g.Filled) || g.Moves >= MaxMoves || g.Solved() {
		return ErrMove
	}
	g.Filled[cell] = !g.Filled[cell]
	g.Moves++
	return nil
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Solved 表示所有行、列都与提示相符。
func (g *Game) Solved() bool {
	for i := 0; i < g.Size; i++ {
		if !equal(runs(row(g.Filled, g.Size, i)), g.Rows[i]) || !equal(runs(col(g.Filled, g.Size, i)), g.Cols[i]) {
			return false
		}
	}
	return true
}

// 推理时每格的状态。
const (
	unknown int8 = iota
	filled
	empty
)

// placements 枚举长度为 n、提示为 clue、与 known 不矛盾的所有涂法，对每种涂法调用 visit。
func placements(clue []int, known []int8, visit func(line []bool)) {
	n := len(known)
	line := make([]bool, n)
	var place func(index, start int)
	place = func(index, start int) {
		if index == len(clue) {
			for i := start; i < n; i++ {
				if known[i] == filled {
					return
				}
			}
			visit(line)
			return
		}
		remaining := 0
		for _, length := range clue[index+1:] {
			remaining += length + 1
		}
		for pos := start; pos+clue[index]+remaining <= n; pos++ {
			// pos 之前到 start 的格子都留白。
			if pos > start && known[pos-1] == filled {
				return
			}
			ok := true
			for i := pos; i < pos+clue[index]; i++ {
				if known[i] == empty {
					ok = false
					break
				}
			}
			end := pos + clue[index]
			if ok && end < n && known[end] == filled {
				ok = false
			}
			if !ok {
				continue
			}
			for i := pos; i < end; i++ {
				line[i] = true
			}
			next := end + 1
			place(index+1, min(next, n))
			for i := pos; i < end; i++ {
				line[i] = false
			}
		}
	}
	place(0, 0)
}

// solveLine 在 known 的基础上推出这一行(列)必然的格子，返回有没有新推出的格子；没有任何涂法时 ok 为假。
func solveLine(clue []int, known []int8) (changed bool, ok bool) {
	n := len(known)
	alwaysOn := make([]bool, n)
	alwaysOff := make([]bool, n)
	for i := range alwaysOn {
		alwaysOn[i], alwaysOff[i] = true, true
	}
	count := 0
	placements(clue, known, func(line []bool) {
		count++
		for i, on := range line {
			if on {
				alwaysOff[i] = false
			} else {
				alwaysOn[i] = false
			}
		}
	})
	if count == 0 {
		return false, false
	}
	for i := range known {
		if known[i] != unknown {
			continue
		}
		if alwaysOn[i] {
			known[i], changed = filled, true
		} else if alwaysOff[i] {
			known[i], changed = empty, true
		}
	}
	return changed, true
}

// LineSolvable 表示只靠逐行逐列推理就能把每一格都确定下来(答案因此唯一)。
func LineSolvable(p Puzzle) bool {
	_, ok := lineSolve(p)
	return ok
}

// lineSolve 逐行逐列推理，能把每一格都确定下来时返回推出的答案。
func lineSolve(p Puzzle) ([]bool, bool) {
	size := p.Size
	known := make([]int8, size*size)
	for {
		progress := false
		for r := 0; r < size; r++ {
			line := known[r*size : (r+1)*size]
			changed, ok := solveLine(p.Rows[r], line)
			if !ok {
				return nil, false
			}
			progress = progress || changed
		}
		for c := 0; c < size; c++ {
			line := make([]int8, size)
			for r := 0; r < size; r++ {
				line[r] = known[r*size+c]
			}
			changed, ok := solveLine(p.Cols[c], line)
			if !ok {
				return nil, false
			}
			for r := 0; r < size; r++ {
				known[r*size+c] = line[r]
			}
			progress = progress || changed
		}
		if !progress {
			break
		}
	}
	solution := make([]bool, len(known))
	for i, state := range known {
		if state == unknown {
			return nil, false
		}
		solution[i] = state == filled
	}
	return solution, true
}
