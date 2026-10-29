// Package twentyfour 是游戏中心 24 点的规则：每局按难度从题库里抽出几组 4 张牌(1–13)，玩家用加减乘除和括号把每组的 4 个
// 数各用一次凑成 24。每组都至少有一种解法；解出全部 HandsPerRound 组即过关，最多可以跳过 MaxSkips 组(跳过的换成下一组)。
//
// 题库按解法多少分三档(见 hands.go)：简单是 1–10 的牌、解法很多；普通解法有几种；困难只有一两种解法，常常要用到分数。
// 一局的题目只由难度和会话种子决定，答案用有理数精确计算，服务端核对每一个算式。
package twentyfour

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
)

const (
	HandsPerRound = 5
	MaxSkips      = 2
	// MaxExpressionLength 是一个算式最多几个字符(去掉空白之前按字节算)。
	MaxExpressionLength = 64
	Target              = 24
)

var (
	ErrDifficulty = errors.New("invalid 24 difficulty")
	ErrFinished   = errors.New("24 round finished")
	ErrNoSkip     = errors.New("no skips left")
	// ErrExpression 是算式写错了：不是合法的四则运算、没有正好用上这 4 个数，或者算出来不是 24。
	ErrExpression = errors.New("invalid 24 expression")
)

// Game 是一局 24 点。
type Game struct {
	Difficulty int      `json:"difficulty"`
	Hands      [][4]int `json:"-"`
	Index      int      `json:"index"`
	Solved     int      `json:"solved"`
	Skips      int      `json:"skips"`
}

// New 按难度与种子抽出这一局的题目：HandsPerRound + MaxSkips 组互不相同的牌，每组 4 张按随机顺序摆放。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrDifficulty
	}
	pool := hands[difficulty]
	count := len(pool) / 4
	rng := rand.New(rand.NewPCG(seed, uint64(difficulty)))
	order := rng.Perm(count)[:HandsPerRound+MaxSkips]
	g := &Game{Difficulty: difficulty}
	for _, i := range order {
		var hand [4]int
		for j := 0; j < 4; j++ {
			hand[j] = strings.IndexByte("0123456789abcd", pool[i*4+j])
		}
		rng.Shuffle(4, func(a, b int) { hand[a], hand[b] = hand[b], hand[a] })
		g.Hands = append(g.Hands, hand)
	}
	return g, nil
}

// Won 表示已经解出全部题目。
func (g *Game) Won() bool { return g.Solved >= HandsPerRound }

// Current 返回现在要解的一组牌。
func (g *Game) Current() [4]int { return g.Hands[g.Index] }

// Submit 核对 expr 是不是这一组牌的解法，是时记一题并换下一组。
func (g *Game) Submit(expr string) error {
	if g.Won() {
		return ErrFinished
	}
	if err := Check(expr, g.Current()); err != nil {
		return err
	}
	g.Solved++
	g.Index++
	return nil
}

// Skip 跳过这一组，换下一组；跳过的次数用完时返回 ErrNoSkip。
func (g *Game) Skip() error {
	if g.Won() {
		return ErrFinished
	}
	if g.Skips >= MaxSkips {
		return ErrNoSkip
	}
	g.Skips++
	g.Index++
	return nil
}

// Check 核对 expr 用 hand 里的 4 个数各一次、只用加减乘除和括号算出 24。可以用 × ÷ 代替 * /。
func Check(expr string, hand [4]int) error {
	if len(expr) == 0 || len(expr) > MaxExpressionLength {
		return ErrExpression
	}
	p := &parser{input: []rune(expr)}
	value, err := p.expression()
	if err != nil {
		return err
	}
	p.skipSpace()
	if p.pos != len(p.input) {
		return ErrExpression
	}
	used := slices.Clone(p.numbers)
	want := hand[:]
	slices.Sort(used)
	sortedHand := slices.Clone(want)
	slices.Sort(sortedHand)
	if !slices.Equal(used, sortedHand) || value.Cmp(big.NewRat(Target, 1)) != 0 {
		return ErrExpression
	}
	return nil
}

// parser 是四则运算的递归下降解析器，边解析边用有理数求值，记下用到的数。
type parser struct {
	input   []rune
	pos     int
	numbers []int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) peek() rune {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *parser) expression() (*big.Rat, error) {
	left, err := p.term()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek() {
		case '+', '-', '−':
			op := p.input[p.pos]
			p.pos++
			right, err := p.term()
			if err != nil {
				return nil, err
			}
			if op == '+' {
				left = new(big.Rat).Add(left, right)
			} else {
				left = new(big.Rat).Sub(left, right)
			}
		default:
			return left, nil
		}
	}
}

func (p *parser) term() (*big.Rat, error) {
	left, err := p.factor()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek() {
		case '*', '×', 'x', 'X':
			p.pos++
			right, err := p.factor()
			if err != nil {
				return nil, err
			}
			left = new(big.Rat).Mul(left, right)
		case '/', '÷':
			p.pos++
			right, err := p.factor()
			if err != nil {
				return nil, err
			}
			if right.Sign() == 0 {
				return nil, ErrExpression
			}
			left = new(big.Rat).Quo(left, right)
		default:
			return left, nil
		}
	}
}

func (p *parser) factor() (*big.Rat, error) {
	switch c := p.peek(); {
	case c == '(' || c == '（':
		p.pos++
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		if c := p.peek(); c != ')' && c != '）' {
			return nil, ErrExpression
		}
		p.pos++
		return value, nil
	case c >= '0' && c <= '9':
		n := 0
		digits := 0
		for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
			n = n*10 + int(p.input[p.pos]-'0')
			p.pos++
			digits++
			if digits > 2 {
				return nil, ErrExpression
			}
		}
		if len(p.numbers) >= 4 {
			return nil, ErrExpression
		}
		p.numbers = append(p.numbers, n)
		return big.NewRat(int64(n), 1), nil
	}
	return nil, ErrExpression
}

// Solve 返回 hand 的一种解法(没有解时为空字符串)，跳过一组时给玩家看答案。
func Solve(hand [4]int) string {
	type item struct {
		value *big.Rat
		expr  string
	}
	var rec func(items []item) string
	rec = func(items []item) string {
		if len(items) == 1 {
			if items[0].value.Cmp(big.NewRat(Target, 1)) == 0 {
				return items[0].expr
			}
			return ""
		}
		for i := 0; i < len(items); i++ {
			for j := 0; j < len(items); j++ {
				if i == j {
					continue
				}
				a, b := items[i], items[j]
				rest := make([]item, 0, len(items)-1)
				for k := range items {
					if k != i && k != j {
						rest = append(rest, items[k])
					}
				}
				candidates := []item{
					{new(big.Rat).Add(a.value, b.value), fmt.Sprintf("(%s+%s)", a.expr, b.expr)},
					{new(big.Rat).Sub(a.value, b.value), fmt.Sprintf("(%s-%s)", a.expr, b.expr)},
					{new(big.Rat).Mul(a.value, b.value), fmt.Sprintf("(%s×%s)", a.expr, b.expr)},
				}
				if b.value.Sign() != 0 {
					candidates = append(candidates, item{new(big.Rat).Quo(a.value, b.value), fmt.Sprintf("(%s÷%s)", a.expr, b.expr)})
				}
				// 每一步都带括号，最后只去掉最外面一层，优先级不会把意思改了。
				for _, next := range candidates {
					if found := rec(append(slices.Clone(rest), next)); found != "" {
						return found
					}
				}
			}
		}
		return ""
	}
	items := make([]item, 4)
	for i, n := range hand {
		items[i] = item{big.NewRat(int64(n), 1), fmt.Sprint(n)}
	}
	answer := rec(items)
	if len(answer) > 2 && answer[0] == '(' && answer[len(answer)-1] == ')' {
		answer = answer[1 : len(answer)-1]
	}
	return answer
}
