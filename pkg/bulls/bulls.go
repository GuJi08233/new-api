// Package bulls 是游戏中心猜数字(1A2B)的规则：服务端想好一个各位数字互不相同的数，玩家每猜一次得到提示：A 是数字和位置都对
// 的个数，B 是数字对但位置不对的个数。在限定次数内猜中(全部是 A)即过关。
//
// 三档难度：简单 3 位数、8 次；普通 4 位数、8 次；困难 4 位数、6 次(最优策略最坏也只要 7 次，6 次要靠推理和一点运气)。
// 答案只由会话种子决定，只保存在服务端；猜完或用完次数后才公开。
package bulls

import (
	"errors"
	"math/rand/v2"
)

var (
	ErrDifficulty = errors.New("invalid bulls difficulty")
	ErrGuess      = errors.New("invalid bulls guess")
	ErrFinished   = errors.New("bulls round finished")
)

// Level 是一档难度：几位数、最多猜几次。
type Level struct {
	Digits  int `json:"digits"`
	Guesses int `json:"guesses"`
}

// Levels[d] 是难度 d 的规则。
var Levels = [4]Level{
	1: {Digits: 3, Guesses: 8},
	2: {Digits: 4, Guesses: 8},
	3: {Digits: 4, Guesses: 6},
}

// Feedback 是一次猜测和它的提示。
type Feedback struct {
	Guess string `json:"guess"`
	A     int    `json:"a"`
	B     int    `json:"b"`
}

// Game 是一局猜数字。
type Game struct {
	Difficulty int
	Secret     string
	History    []Feedback
}

// New 按难度与种子想好答案：从 0–9 里不放回地抽出各位数字，首位可以是 0。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrDifficulty
	}
	rng := rand.New(rand.NewPCG(seed, 0x1a2b))
	digits := rng.Perm(10)[:Levels[difficulty].Digits]
	secret := make([]byte, len(digits))
	for i, d := range digits {
		secret[i] = byte('0' + d)
	}
	return &Game{Difficulty: difficulty, Secret: string(secret)}, nil
}

// ValidGuess 表示 guess 是 digits 位、各位互不相同的数字串。
func ValidGuess(guess string, digits int) bool {
	if len(guess) != digits {
		return false
	}
	seen := [10]bool{}
	for i := 0; i < len(guess); i++ {
		c := guess[i]
		if c < '0' || c > '9' || seen[c-'0'] {
			return false
		}
		seen[c-'0'] = true
	}
	return true
}

// Score 返回 guess 相对 secret 的 A、B。
func Score(secret, guess string) (a int, b int) {
	for i := 0; i < len(guess); i++ {
		for j := 0; j < len(secret); j++ {
			if guess[i] != secret[j] {
				continue
			}
			if i == j {
				a++
			} else {
				b++
			}
		}
	}
	return a, b
}

// Won 表示已经猜中。
func (g *Game) Won() bool {
	n := len(g.History)
	return n > 0 && g.History[n-1].A == len(g.Secret)
}

// Over 表示猜中了或者次数用完了。
func (g *Game) Over() bool {
	return g.Won() || len(g.History) >= Levels[g.Difficulty].Guesses
}

// Guess 猜一次，记下提示。
func (g *Game) Guess(guess string) (Feedback, error) {
	if g.Over() {
		return Feedback{}, ErrFinished
	}
	if !ValidGuess(guess, len(g.Secret)) {
		return Feedback{}, ErrGuess
	}
	a, b := Score(g.Secret, guess)
	feedback := Feedback{Guess: guess, A: a, B: b}
	g.History = append(g.History, feedback)
	return feedback, nil
}

// Candidates 返回与目前所有提示都相符的答案还有几个，页面据此提示玩家推理的进度。
func (g *Game) Candidates() int {
	digits := len(g.Secret)
	count := 0
	var walk func(prefix []byte, used int)
	walk = func(prefix []byte, used int) {
		if len(prefix) == digits {
			candidate := string(prefix)
			for _, f := range g.History {
				if a, b := Score(candidate, f.Guess); a != f.A || b != f.B {
					return
				}
			}
			count++
			return
		}
		for d := 0; d < 10; d++ {
			if used&(1<<d) != 0 {
				continue
			}
			walk(append(prefix, byte('0'+d)), used|1<<d)
		}
	}
	walk(make([]byte, 0, digits), 0)
	return count
}
