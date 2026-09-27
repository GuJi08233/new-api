// Package minesweeper 是游戏中心扫雷的规则：雷区、布雷、翻开与连锁展开、胜负判定。
//
// 对局由服务端逐步裁判：雷的位置只存在服务端的对局状态里，浏览器每翻一格都要问服务端，拿到的只有翻开的格子和周围的雷数，
// 一局结束后才公开全部雷的位置。旗子只是玩家自己的记号，只在浏览器里。
//
// 雷在第一次翻开时才布置：避开第一下点的格子和它周围的 8 格(格子不够时只避开点的那一格)，第一下一定不会踩雷，
// 而且一定能展开一片。布雷用开局时生成的 256 位随机密钥驱动 ChaCha8，同一个密钥、同一个第一下总是布出同一片雷区，
// 便于测试与事后核查；密钥足够长，玩家没法从翻开的数字穷举反推出整片雷区(32 位的种子就可以)。
//
// 难度：1 初级 9×9/10 雷、2 中级 16×16/40 雷、3 高级 30×16/99 雷(宽 30 高 16)。成绩是赢下的难度，踩雷为 0，
// 奖励按游戏中心的领奖规则发放。
package minesweeper

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
)

// 一局的状态，字符串值会存进对局记录、返回给浏览器，不能改动。
const (
	StatusPlaying = "playing"
	StatusWon     = "won"
	StatusLost    = "lost"
)

// MaxRevealCells 是一次最多翻开的格子数：单击翻开 1 格，双击数字"和弦"翻开它周围没插旗的格子，最多 8 格；
// 留到一个 3×3 的 9 格，前端换一种和弦的做法也够用。
const MaxRevealCells = 9

// 雷区每格一个字符，整片按行优先存成一个字符串。翻开的格子存 '0'..'8'，即周围的雷数。
const (
	cellHidden   = '-' // 没翻开、不是雷；布雷之前所有格子都是它
	cellMine     = '*' // 没翻开的雷
	cellExploded = 'X' // 踩到的那颗雷
)

var (
	ErrInvalidDifficulty = errors.New("invalid minesweeper difficulty")
	// ErrInvalidMove 表示这次翻开不合规则：没有格子、一次翻开太多、格子越界，或者这局已经结束。
	ErrInvalidMove = errors.New("invalid minesweeper move")
)

// Level 是一个难度的雷区大小与雷数。
type Level struct {
	Difficulty int `json:"difficulty"`
	Width      int `json:"width"`
	Height     int `json:"height"`
	Mines      int `json:"mines"`
}

// Levels 是三个难度。每个难度的雷数都小于格子数减 9，第一下周围的 3×3 总能空出来。
var Levels = []Level{
	{Difficulty: 1, Width: 9, Height: 9, Mines: 10},
	{Difficulty: 2, Width: 16, Height: 16, Mines: 40},
	{Difficulty: 3, Width: 30, Height: 16, Mines: 99},
}

// LevelOf 返回难度对应的雷区，不是 1、2、3 时 ok 为假。
func LevelOf(difficulty int) (Level, bool) {
	for _, level := range Levels {
		if level.Difficulty == difficulty {
			return level, true
		}
	}
	return Level{}, false
}

// Game 是一局扫雷的完整状态，整份存在对局的 State 里，只有服务端知道；给玩家看的只有 Revealed 挑出来的部分，
// MineCells 与 Exploded 只能在一局结束后公开。
type Game struct {
	Level
	// Key 是布雷用的随机密钥。
	Key [32]byte `json:"key"`
	// Cells 是雷区，每格一个字符，第 y 行第 x 列是 Cells[y*Width+x]。
	Cells  string `json:"cells"`
	Status string `json:"status"`
}

// New 开一局扫雷，雷要等第一次翻开时才用 key 布置。
func New(difficulty int, key [32]byte) (*Game, error) {
	level, ok := LevelOf(difficulty)
	if !ok {
		return nil, ErrInvalidDifficulty
	}
	return &Game{
		Level:  level,
		Key:    key,
		Cells:  strings.Repeat(string(rune(cellHidden)), level.Width*level.Height),
		Status: StatusPlaying,
	}, nil
}

// Reveal 按顺序翻开 cells 里的格子([x, y])，返回这次新翻开的格子 [x, y, 周围雷数]。翻到周围没有雷的格子时自动翻开它周围的
// 格子，一直连锁下去；已经翻开的格子跳过。踩到雷这局就输了，翻开所有不是雷的格子就赢了，两种情况都不再翻后面的格子：
// 双击和弦时玩家插错了旗，先翻到哪一格由浏览器发来的顺序决定。一局的第一次翻开先以第一个格子为准布雷。
// 没有格子、超过 MaxRevealCells 格、格子越界或这局已经结束时返回 ErrInvalidMove，局面不变。
func (g *Game) Reveal(cells [][2]int) ([][3]int, error) {
	if g.Status != StatusPlaying || len(cells) == 0 || len(cells) > MaxRevealCells {
		return nil, ErrInvalidMove
	}
	for _, cell := range cells {
		if cell[0] < 0 || cell[0] >= g.Width || cell[1] < 0 || cell[1] >= g.Height {
			return nil, ErrInvalidMove
		}
	}
	board := []byte(g.Cells)
	// 进行中的一局只要布过雷，就还有没翻开的雷。
	if bytes.IndexByte(board, cellMine) < 0 {
		g.placeMines(board, cells[0][1]*g.Width+cells[0][0])
	}
	hidden := bytes.Count(board, []byte{cellHidden})
	revealed := [][3]int{}
	for _, cell := range cells {
		index := cell[1]*g.Width + cell[0]
		switch board[index] {
		case cellMine:
			board[index] = cellExploded
			g.Status = StatusLost
		case cellHidden:
			opened := len(revealed)
			revealed = g.open(board, index, revealed)
			hidden -= len(revealed) - opened
			if hidden == 0 {
				g.Status = StatusWon
			}
		}
		if g.Status != StatusPlaying {
			break
		}
	}
	g.Cells = string(board)
	return revealed, nil
}

// placeMines 在第一次翻开时布雷：避开第一下点的格子和它周围的 8 格，格子不够时只避开点的那一格；在其余格子里
// 用密钥驱动的随机数不放回地抽出 Mines 格，每种布法的概率相同。
func (g *Game) placeMines(board []byte, first int) {
	safe := append(g.neighbors(first), first)
	if len(board)-len(safe) < g.Mines {
		safe = []int{first}
	}
	candidates := make([]int, 0, len(board))
	for index := range board {
		if !slices.Contains(safe, index) {
			candidates = append(candidates, index)
		}
	}
	rng := rand.New(rand.NewChaCha8(g.Key))
	// Levels 保证雷数小于格子数，这里的第二个条件只防止雷数配错时越界。
	for i := 0; i < g.Mines && i < len(candidates); i++ {
		j := i + rng.IntN(len(candidates)-i)
		candidates[i], candidates[j] = candidates[j], candidates[i]
		board[candidates[i]] = cellMine
	}
}

// open 翻开一格并连锁展开：周围没有雷的格子把它周围没翻开的格子也翻开，直到碰上周围有雷的格子为止。
// 新翻开的格子依次追加到 revealed 后面。
func (g *Game) open(board []byte, start int, revealed [][3]int) [][3]int {
	board[start] = g.countAround(board, start)
	queue := []int{start}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		revealed = append(revealed, [3]int{index % g.Width, index / g.Width, int(board[index] - '0')})
		if board[index] != '0' {
			continue
		}
		for _, next := range g.neighbors(index) {
			if board[next] == cellHidden {
				board[next] = g.countAround(board, next)
				queue = append(queue, next)
			}
		}
	}
	return revealed
}

// countAround 返回一格周围的雷数，用翻开后显示的字符 '0'..'8' 表示。
func (g *Game) countAround(board []byte, index int) byte {
	count := byte('0')
	for _, next := range g.neighbors(index) {
		if board[next] == cellMine || board[next] == cellExploded {
			count++
		}
	}
	return count
}

// neighbors 返回一格周围在雷区之内的格子，最多 8 格。
func (g *Game) neighbors(index int) []int {
	x, y := index%g.Width, index/g.Width
	around := make([]int, 0, 8)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := x+dx, y+dy
			if (dx != 0 || dy != 0) && nx >= 0 && nx < g.Width && ny >= 0 && ny < g.Height {
				around = append(around, ny*g.Width+nx)
			}
		}
	}
	return around
}

// Revealed 返回所有已经翻开的格子 [x, y, 周围雷数]，按行优先排列，刷新页面后据此恢复雷区。
func (g *Game) Revealed() [][3]int {
	revealed := [][3]int{}
	for index := 0; index < len(g.Cells); index++ {
		if cell := g.Cells[index]; cell >= '0' && cell <= '8' {
			revealed = append(revealed, [3]int{index % g.Width, index / g.Width, int(cell - '0')})
		}
	}
	return revealed
}

// MineCells 返回所有雷的位置 [x, y]，按行优先排列。只能在一局结束之后公开。
func (g *Game) MineCells() [][2]int {
	mines := [][2]int{}
	for index := 0; index < len(g.Cells); index++ {
		if cell := g.Cells[index]; cell == cellMine || cell == cellExploded {
			mines = append(mines, [2]int{index % g.Width, index / g.Width})
		}
	}
	return mines
}

// Exploded 返回踩到的那颗雷 [x, y]，没踩雷时 ok 为假。
func (g *Game) Exploded() ([2]int, bool) {
	index := strings.IndexByte(g.Cells, cellExploded)
	if index < 0 {
		return [2]int{}, false
	}
	return [2]int{index % g.Width, index / g.Width}, true
}
