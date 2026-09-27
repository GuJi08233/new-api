// Package sudoku 是游戏中心数独的出题与判题。
//
// 开局时服务端出题：先随机填出一个完整的解，再按随机顺序挖空，每挖一格都用回溯搜索数一数解的个数(数到 2 个就停)，
// 挖掉之后解不唯一就把这格填回去，直到剩下的给定数达到这一局抽到的目标，所以题目一定只有唯一解。谜底只存在服务端；
// 玩家交卷时判定给出的数字没改、每行每列每宫 1-9 各一次。
//
// 难度按给定数划分：1 简单 38-42 个、2 普通 30-34 个、3 困难 24-28 个。成绩是解出的难度，放弃为 0，奖励按游戏中心的领奖规则发放。
//
// 求解器用位掩码记下每行、每列、每宫用过的数字，每次先填候选最少的空格，一次唯一性检查通常只要几百步。出一道题的所有搜索
// 共用一个尝试次数的上限，耗时因此有上界；实测困难难度最慢的一道也只要几毫秒(见 BenchmarkGenerate)。
package sudoku

import (
	"errors"
	"math/bits"
	"math/rand/v2"
	"strings"
)

const cellCount = 81

// allDigits 是数字 1-9 的位掩码，第 d 位表示数字 d。
const allDigits uint16 = 0x3FE

// searchBudget 是出一道题的所有搜索合计最多尝试填数的次数。实测 5000 个种子里困难难度最多用了约 7.5 万次(约 5ms)，
// 留出六倍多的余量；万一用完就出题失败，玩家重新开局。
const searchBudget = 500000

var (
	ErrInvalidDifficulty = errors.New("invalid sudoku difficulty")
	ErrGenerateFailed    = errors.New("failed to generate a sudoku puzzle")
)

// Level 是一个难度的给定数范围。
type Level struct {
	Difficulty int `json:"difficulty"`
	MinGivens  int `json:"min_givens"`
	MaxGivens  int `json:"max_givens"`
}

// Levels 是三个难度。
var Levels = []Level{
	{Difficulty: 1, MinGivens: 38, MaxGivens: 42},
	{Difficulty: 2, MinGivens: 30, MaxGivens: 34},
	{Difficulty: 3, MinGivens: 24, MaxGivens: 28},
}

// LevelOf 返回难度对应的给定数范围，不是 1、2、3 时 ok 为假。
func LevelOf(difficulty int) (Level, bool) {
	for _, level := range Levels {
		if level.Difficulty == difficulty {
			return level, true
		}
	}
	return Level{}, false
}

// Puzzle 是一道数独，整份存在对局的 State 里。
type Puzzle struct {
	Difficulty int `json:"difficulty"`
	// Puzzle 是题目：81 个字符按行优先，'0' 表示空格。
	Puzzle string `json:"puzzle"`
	// Solution 是唯一的解，只存在服务端。
	Solution string `json:"solution"`
}

// Givens 返回题目给出的数字个数。
func (p *Puzzle) Givens() int {
	return cellCount - strings.Count(p.Puzzle, "0")
}

// Generate 按种子出一道这个难度的题，同一个种子出同一道题。
func Generate(difficulty int, seed uint64) (*Puzzle, error) {
	level, ok := LevelOf(difficulty)
	if !ok {
		return nil, ErrInvalidDifficulty
	}
	rng := rand.New(rand.NewPCG(seed, 0))
	budget := searchBudget
	// 困难难度偶尔挖到底也剩 28 个以上的给定数，就换一个解重新挖。
	for budget > 0 {
		full := &grid{budget: &budget}
		if !full.fill(rng) {
			break
		}
		puzzle := full.cells
		target := level.MinGivens + rng.IntN(level.MaxGivens-level.MinGivens+1)
		givens := cellCount
		for _, index := range rng.Perm(cellCount) {
			if givens == target {
				break
			}
			digit := puzzle[index]
			puzzle[index] = 0
			count, complete := countSolutions(puzzle, 2, &budget)
			if !complete {
				return nil, ErrGenerateFailed
			}
			if count != 1 {
				puzzle[index] = digit
				continue
			}
			givens--
		}
		if givens <= level.MaxGivens {
			return &Puzzle{Difficulty: difficulty, Puzzle: encode(puzzle), Solution: encode(full.cells)}, nil
		}
	}
	return nil, ErrGenerateFailed
}

// Check 判定玩家交的答卷：81 个 '1'-'9'，题目给出的数字没改，每行、每列、每宫 1-9 各出现一次。
func Check(puzzle string, answer string) bool {
	if len(puzzle) != cellCount || len(answer) != cellCount {
		return false
	}
	var rows, cols, boxes [9]uint16
	for index := 0; index < cellCount; index++ {
		digit := answer[index]
		if digit < '1' || digit > '9' || (puzzle[index] != '0' && puzzle[index] != digit) {
			return false
		}
		bit := uint16(1) << (digit - '0')
		row, col, box := index/9, index%9, boxOf(index)
		if (rows[row]|cols[col]|boxes[box])&bit != 0 {
			return false
		}
		rows[row] |= bit
		cols[col] |= bit
		boxes[box] |= bit
	}
	return true
}

func encode(cells [cellCount]uint8) string {
	var text [cellCount]byte
	for index, digit := range cells {
		text[index] = '0' + digit
	}
	return string(text[:])
}

func boxOf(index int) int {
	return index/27*3 + index%9/3
}

// grid 是求解器的盘面：cells 里 0 表示空格，rows、cols、boxes 记下每行、每列、每宫用过的数字；budget 指向还能尝试填数的
// 次数，出一道题的所有搜索共用。
type grid struct {
	cells             [cellCount]uint8
	rows, cols, boxes [9]uint16
	budget            *int
}

func (g *grid) place(index int, digit uint8) {
	bit := uint16(1) << digit
	g.cells[index] = digit
	g.rows[index/9] |= bit
	g.cols[index%9] |= bit
	g.boxes[boxOf(index)] |= bit
}

func (g *grid) clear(index int, digit uint8) {
	bit := uint16(1) << digit
	g.cells[index] = 0
	g.rows[index/9] &^= bit
	g.cols[index%9] &^= bit
	g.boxes[boxOf(index)] &^= bit
}

// choose 返回候选数字最少的空格与它的候选(位掩码)，没有空格时返回 -1。某个空格没有候选时它就是最少的，候选为 0，
// 搜索在这里回头。
func (g *grid) choose() (int, uint16) {
	best, bestCandidates, bestCount := -1, uint16(0), 10
	for index, digit := range g.cells {
		if digit != 0 {
			continue
		}
		candidates := allDigits &^ (g.rows[index/9] | g.cols[index%9] | g.boxes[boxOf(index)])
		if count := bits.OnesCount16(candidates); count < bestCount {
			best, bestCandidates, bestCount = index, candidates, count
			if count <= 1 {
				break
			}
		}
	}
	return best, bestCandidates
}

// fill 按随机顺序试填候选数字，把空盘面填满成一个完整的解；尝试次数用完时返回 false。
func (g *grid) fill(rng *rand.Rand) bool {
	index, candidates := g.choose()
	if index < 0 {
		return true
	}
	digits := make([]uint8, 0, 9)
	for ; candidates != 0; candidates &= candidates - 1 {
		digits = append(digits, uint8(bits.TrailingZeros16(candidates)))
	}
	rng.Shuffle(len(digits), func(i, j int) { digits[i], digits[j] = digits[j], digits[i] })
	for _, digit := range digits {
		if *g.budget <= 0 {
			return false
		}
		*g.budget--
		g.place(index, digit)
		if g.fill(rng) {
			return true
		}
		g.clear(index, digit)
	}
	return false
}

// count 把盘面的解累加到 found 上，数到 limit 个就停；尝试次数用完时返回 false，found 不再可信。
func (g *grid) count(limit int, found *int) bool {
	index, candidates := g.choose()
	if index < 0 {
		*found++
		return true
	}
	for ; candidates != 0; candidates &= candidates - 1 {
		if *g.budget <= 0 {
			return false
		}
		*g.budget--
		digit := uint8(bits.TrailingZeros16(candidates))
		g.place(index, digit)
		complete := g.count(limit, found)
		g.clear(index, digit)
		if !complete {
			return false
		}
		if *found >= limit {
			return true
		}
	}
	return true
}

// countSolutions 数出题目的解，数到 limit 个就停，尝试次数从 budget 里扣；次数用完还没数完时 complete 为假。
// 给定的数字互相冲突时没有解。
func countSolutions(cells [cellCount]uint8, limit int, budget *int) (count int, complete bool) {
	g := &grid{budget: budget}
	for index, digit := range cells {
		if digit == 0 {
			continue
		}
		bit := uint16(1) << digit
		if (g.rows[index/9]|g.cols[index%9]|g.boxes[boxOf(index)])&bit != 0 {
			return 0, true
		}
		g.place(index, digit)
	}
	complete = g.count(limit, &count)
	return count, complete
}
