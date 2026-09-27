// Package memory 是游戏中心记忆翻牌的规则：牌面布局、翻牌、配对与步数上限。
//
// 对局由服务端逐步裁判：牌面布局只存在服务端的对局状态里，浏览器每翻一张都要问服务端，拿到的只有这一张的图案，
// 没法一开局就把整副牌看光。一步是翻开两张：图案相同就配上了、一直朝上；不同就由浏览器显示一会儿再翻回去。
// 在步数上限之内配完所有对就赢了，用完步数还没配完就输了。
//
// 难度：1 是 4×4(8 对，20 步)、2 是 4 行 6 列(12 对，32 步)、3 是 6×6(18 对，50 步)。记性完美的玩家平均约 1.6 倍对数的步数
// 就能配完，上限给记错留了余地。成绩是赢下的难度，输了为 0，奖励按游戏中心的领奖规则发放。
//
// 图案编号 0..17，前端映射成图标，小一些的难度从 18 种里随机选几种。洗牌用开局时生成的 256 位随机密钥驱动 ChaCha8：
// 32 位的种子只有 2^32 种布局，玩家翻开几张之后就能穷举种子、推出整副牌。
package memory

import (
	"errors"
	"math/rand/v2"
	"slices"
)

// 一局的状态，字符串值会存进对局记录、返回给浏览器，不能改动。
const (
	StatusPlaying = "playing"
	StatusWon     = "won"
	StatusLost    = "lost"
)

// SymbolCount 是图案的种类数，最大的 6×6 正好用满。
const SymbolCount = 18

var (
	ErrInvalidDifficulty = errors.New("invalid memory difficulty")
	// ErrInvalidMove 表示这张牌不能翻：位置越界、已经配上、正翻开着，或者这局已经结束。
	ErrInvalidMove = errors.New("invalid memory move")
)

// Level 是一个难度的牌面大小、对数与步数上限。
type Level struct {
	Difficulty int `json:"difficulty"`
	Rows       int `json:"rows"`
	Cols       int `json:"cols"`
	Pairs      int `json:"pairs"`
	MoveLimit  int `json:"move_limit"`
}

// Levels 是三个难度。
var Levels = []Level{
	{Difficulty: 1, Rows: 4, Cols: 4, Pairs: 8, MoveLimit: 20},
	{Difficulty: 2, Rows: 4, Cols: 6, Pairs: 12, MoveLimit: 32},
	{Difficulty: 3, Rows: 6, Cols: 6, Pairs: 18, MoveLimit: 50},
}

// LevelOf 返回难度对应的牌面，不是 1、2、3 时 ok 为假。
func LevelOf(difficulty int) (Level, bool) {
	for _, level := range Levels {
		if level.Difficulty == difficulty {
			return level, true
		}
	}
	return Level{}, false
}

// Game 是一局记忆翻牌的完整状态，整份存在对局的 State 里，只有服务端知道；给玩家看的只有翻开过的牌。
type Game struct {
	Level
	// Layout 是每个位置上的图案，按行优先(第 r 行第 c 列是 Layout[r*Cols+c])。
	Layout []int `json:"layout"`
	// Matched 标出已经配上的牌。
	Matched []bool `json:"matched"`
	// First 是这一步已经翻开的第一张，还没翻时为 -1。
	First  int    `json:"first"`
	Moves  int    `json:"moves"`
	Status string `json:"status"`
}

// Flip 是翻开一张牌的结果。
type Flip struct {
	Index  int
	Symbol int
	// First 为真表示这是一步里的第一张；否则这一步完成了，Matched 表示两张配上了，Previous* 是这一步的第一张。
	First          bool
	Matched        bool
	PreviousIndex  int
	PreviousSymbol int
}

// New 开一局记忆翻牌：从 18 种图案里随机选出这个难度的对数，每种两张，用 key 驱动的随机数洗好。
func New(difficulty int, key [32]byte) (*Game, error) {
	level, ok := LevelOf(difficulty)
	if !ok {
		return nil, ErrInvalidDifficulty
	}
	rng := rand.New(rand.NewChaCha8(key))
	layout := make([]int, 0, 2*level.Pairs)
	for _, symbol := range rng.Perm(SymbolCount)[:level.Pairs] {
		layout = append(layout, symbol, symbol)
	}
	rng.Shuffle(len(layout), func(i, j int) { layout[i], layout[j] = layout[j], layout[i] })
	return &Game{
		Level:   level,
		Layout:  layout,
		Matched: make([]bool, len(layout)),
		First:   -1,
		Status:  StatusPlaying,
	}, nil
}

// Flip 翻开一张牌。是一步里的第二张时这一步完成：图案相同就配上；配完所有对就赢了，否则步数用完就输了。
// 位置越界、已经配上或正翻开着的牌、结束的一局返回 ErrInvalidMove，局面不变。
func (g *Game) Flip(index int) (Flip, error) {
	if g.Status != StatusPlaying || index < 0 || index >= len(g.Layout) || g.Matched[index] || index == g.First {
		return Flip{}, ErrInvalidMove
	}
	flip := Flip{Index: index, Symbol: g.Layout[index]}
	if g.First < 0 {
		g.First, flip.First = index, true
		return flip, nil
	}
	flip.PreviousIndex, flip.PreviousSymbol = g.First, g.Layout[g.First]
	flip.Matched = flip.PreviousSymbol == flip.Symbol
	if flip.Matched {
		g.Matched[g.First], g.Matched[index] = true, true
	}
	g.First = -1
	g.Moves++
	switch {
	case !slices.Contains(g.Matched, false):
		g.Status = StatusWon
	case g.Moves >= g.MoveLimit:
		g.Status = StatusLost
	}
	return flip, nil
}

// MatchedCards 返回已经配上的牌 [位置, 图案]，按位置排列，刷新页面后据此恢复牌面。
func (g *Game) MatchedCards() [][2]int {
	cards := [][2]int{}
	for index, matched := range g.Matched {
		if matched {
			cards = append(cards, [2]int{index, g.Layout[index]})
		}
	}
	return cards
}
