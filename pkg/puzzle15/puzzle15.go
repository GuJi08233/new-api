// Package puzzle15 是游戏中心数字华容道的规则：打乱、按空格的移动重放、判定排好。
//
// 盘面按行优先存成一列数字，0 是空格；排好是 1, 2, …, n²-1 依次排列、空格在右下角。这个游戏没有隐藏信息，开局盘面直接交给
// 浏览器；浏览器记下空格每一步往哪边走，排好后整份上传，服务端从开局盘面重放：有一步走出盘面或者最后没排好都不结算。
//
// 开局时按种子在所有排列里均匀地取一个，不可解时交换两块不是空格的方块(空格不动，可解与不可解的排列由此一一对应，结果在可解的
// 排列里仍是均匀的)，取到排好的排列就重来。
//
// 难度：1 是 3×3、2 是 4×4、3 是 5×5。成绩是排好的难度，奖励按游戏中心的领奖规则发放。
package puzzle15

import (
	"errors"
	"math/rand/v2"
	"slices"
)

const (
	// MaxMoves 是一局最多上传的步数，5×5 慢慢摸索也用不了这么多，重放两万步也不费事。
	MaxMoves = 20000
	// MaxMovesPerSecond 是人每秒最多走几步：上传的步数不能超过开局以来的秒数乘以它，快过这个速度的只可能是脚本。
	MaxMovesPerSecond = 15
)

// 空格的移动方向，是浏览器上传记录里的编码，不能改动。
const (
	MoveUp    = 'U'
	MoveDown  = 'D'
	MoveLeft  = 'L'
	MoveRight = 'R'
)

var (
	ErrInvalidDifficulty = errors.New("invalid puzzle15 difficulty")
	// ErrInvalidMoves 表示上传的步骤不能重放：有一步走出盘面、有不认识的字符，或者超过 MaxMoves 步。
	ErrInvalidMoves = errors.New("invalid puzzle15 moves")
)

// Level 是一个难度的边长。
type Level struct {
	Difficulty int `json:"difficulty"`
	Size       int `json:"size"`
}

// Levels 是三个难度。
var Levels = []Level{
	{Difficulty: 1, Size: 3},
	{Difficulty: 2, Size: 4},
	{Difficulty: 3, Size: 5},
}

// LevelOf 返回难度对应的边长，不是 1、2、3 时 ok 为假。
func LevelOf(difficulty int) (Level, bool) {
	for _, level := range Levels {
		if level.Difficulty == difficulty {
			return level, true
		}
	}
	return Level{}, false
}

// Game 是一局数字华容道的开局：难度、边长与打乱后的盘面，整份存在对局的 State 里。
type Game struct {
	Difficulty int   `json:"difficulty"`
	Size       int   `json:"size"`
	Board      []int `json:"board"`
}

// New 按种子打乱出一局可解、还没排好的盘面，同一个种子打乱出同一个盘面。
func New(difficulty int, seed uint64) (*Game, error) {
	level, ok := LevelOf(difficulty)
	if !ok {
		return nil, ErrInvalidDifficulty
	}
	rng := rand.New(rand.NewPCG(seed, 0))
	board := make([]int, level.Size*level.Size)
	for {
		for index := range board {
			board[index] = index
		}
		rng.Shuffle(len(board), func(i, j int) { board[i], board[j] = board[j], board[i] })
		if !Solvable(level.Size, board) {
			// 交换最前面两块不是空格的方块。最前面三格里至多有一个空格，交换不动空格，同一个盘面总是交换同样的两格。
			first, second := 0, 1
			if board[first] == 0 {
				first = 2
			} else if board[second] == 0 {
				second = 2
			}
			board[first], board[second] = board[second], board[first]
		}
		if !Solved(board) {
			return &Game{Difficulty: difficulty, Size: level.Size, Board: board}, nil
		}
	}
}

// Solvable 判断盘面能否排好。把空格以外的方块按行优先排成一列数逆序对：空格左右走不改变这个顺序；上下走一步，挪动的方块
// 越过 size-1 块方块，逆序数的奇偶随之改变 size-1 次。边长为奇数时逆序数的奇偶始终不变，排好时是 0，所以逆序数为偶数
// 才能排好；边长为偶数时每次上下走都翻转奇偶，逆序数加上空格所在的行(从上往下数)的奇偶才不变，排好时是 size-1。
func Solvable(size int, board []int) bool {
	inversions, blankRow := 0, 0
	for index, tile := range board {
		if tile == 0 {
			blankRow = index / size
			continue
		}
		for _, later := range board[index+1:] {
			if later != 0 && later < tile {
				inversions++
			}
		}
	}
	if size%2 == 1 {
		return inversions%2 == 0
	}
	return (inversions+blankRow)%2 == (size-1)%2
}

// Solved 判断盘面是否已经排好：1, 2, …, n²-1 依次排列，空格在最后。
func Solved(board []int) bool {
	for index, tile := range board[:len(board)-1] {
		if tile != index+1 {
			return false
		}
	}
	return board[len(board)-1] == 0
}

// Replay 从开局盘面按 moves 走一遍，返回走完的盘面。moves 的每个字符是空格往哪边走一格：U 上、D 下、L 左、R 右。
// 有一步走出盘面、有不认识的字符或者超过 MaxMoves 步时返回 ErrInvalidMoves。
func (g *Game) Replay(moves string) ([]int, error) {
	if len(moves) > MaxMoves {
		return nil, ErrInvalidMoves
	}
	board := slices.Clone(g.Board)
	blank := slices.Index(board, 0)
	if blank < 0 {
		return nil, ErrInvalidMoves
	}
	for step := 0; step < len(moves); step++ {
		x, y := blank%g.Size, blank/g.Size
		next := blank
		switch moves[step] {
		case MoveUp:
			if y > 0 {
				next = blank - g.Size
			}
		case MoveDown:
			if y < g.Size-1 {
				next = blank + g.Size
			}
		case MoveLeft:
			if x > 0 {
				next = blank - 1
			}
		case MoveRight:
			if x < g.Size-1 {
				next = blank + 1
			}
		}
		if next == blank {
			return nil, ErrInvalidMoves
		}
		board[blank], board[next] = board[next], board[blank]
		blank = next
	}
	return board, nil
}
