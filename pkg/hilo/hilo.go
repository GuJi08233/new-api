// Package hilo 是游戏中心高低牌的规则：无限副牌，每张牌的点数 1(A) 到 13(K) 等概率，花色随机、只用于显示。
//
// 开局翻开一张牌，玩家猜下一张：点数 r 在 2 到 12 之间时可以猜 higher(不小于 r，命中 14−r 种点数)或 lower(不大于 r，
// 命中 r 种)；A 只能猜 higher(严格大于，12 种)或 same(1 种)，K 只能猜 lower(严格小于，12 种)或 same(1 种)。
// 猜对这一步的倍数是 RTP/100 × 13/命中种数，命中的概率正好是 命中种数/13，所以每一步的期望都恰好是 RTP/100；
// 累计倍数是各步相乘，用有理数精确保存。猜错就输掉下注；也可以 skip 换一张牌、不改变倍数，每局最多 MaxSkips 次。
// 至少猜对一次才能兑现，派彩 = floor(下注 × 累计倍数)，最多下注的 MaxMultiplier 倍，到顶自动兑现。
// 每猜一步，押着的累计倍数期望乘上 RTP/100，换牌不改变期望：猜 n 步再兑现的期望返还是 (RTP/100)^n，怎么猜都一样，
// 最高就是只猜一步就兑现的 RTP；猜得越多返还越低，向下取整与封顶也只会更低。
//
// 下一张牌在玩家猜的那一刻才用密码学安全的随机数翻开，服务端不预先保存牌堆，没有可以泄露的隐藏信息。
package hilo

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

const (
	Ranks = 13
	Suits = 4
	// MaxSkips 是每局最多换几次牌。
	MaxSkips = 5
	// MaxMultiplier 是单局派彩最多为下注的多少倍。
	MaxMultiplier = 1000
)

// 玩家在当前牌上的选择。
const (
	Higher = "higher"
	Lower  = "lower"
	Same   = "same"
	Skip   = "skip"
)

// ErrIllegalGuess 表示当前牌上不能这样猜：猜法不存在或这张牌不能这样猜、换牌次数用完，或者这一局已经输了。
var ErrIllegalGuess = errors.New("illegal hilo guess")

// Card 是一张牌。Rank 是点数 1(A) 到 13(K)；Suit 是花色 0 黑桃、1 红心、2 方块、3 梅花，只用于显示。
type Card struct {
	Rank int `json:"rank"`
	Suit int `json:"suit"`
}

// Draw 用密码学安全的随机数翻开一张牌：52 种点数与花色的组合等概率，点数也就等概率。
func Draw() (Card, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(Ranks*Suits))
	if err != nil {
		return Card{}, err
	}
	value := int(n.Int64())
	return Card{Rank: value%Ranks + 1, Suit: value / Ranks}, nil
}

// Guesses 返回点数 rank 上可以猜的两种猜法。
func Guesses(rank int) [2]string {
	switch rank {
	case 1:
		return [2]string{Higher, Same}
	case Ranks:
		return [2]string{Lower, Same}
	}
	return [2]string{Higher, Lower}
}

// Hits 返回点数 rank 上猜 guess 会命中的点数种数，这张牌上不能这样猜时返回 0。
func Hits(rank int, guess string) int {
	if rank < 1 || rank > Ranks {
		return 0
	}
	edge := rank == 1 || rank == Ranks
	switch {
	case guess == Same && edge:
		return 1
	case guess == Higher && rank == 1, guess == Lower && rank == Ranks:
		return Ranks - 1
	case guess == Higher && !edge:
		return Ranks + 1 - rank
	case guess == Lower && !edge:
		return rank
	}
	return 0
}

// Correct 表示当前点数 rank 上猜 guess 时，下一张的点数 next 算猜对。A 上的 higher 与 K 上的 lower 不含相同点数，
// 其余的 higher、lower 含相同点数。
func Correct(rank int, guess string, next int) bool {
	switch guess {
	case Higher:
		return next > rank || (next == rank && rank != 1)
	case Lower:
		return next < rank || (next == rank && rank != Ranks)
	case Same:
		return next == rank
	}
	return false
}

// StepMultiplier 返回命中 hits 种点数的一步猜对之后的倍数 RTP/100 × 13/hits。
func StepMultiplier(rtpPercent int, hits int) *big.Rat {
	return big.NewRat(int64(rtpPercent)*Ranks, 100*int64(hits))
}

// Payout 返回下注 bet、累计倍数 multiplier 时兑现的派彩：floor(bet × multiplier)，最多 bet × MaxMultiplier。
func Payout(bet int, multiplier *big.Rat) int {
	value := new(big.Rat).Mul(big.NewRat(int64(bet), 1), multiplier)
	// 倍数是正数，截断除法就是向下取整。
	payout := new(big.Int).Quo(value.Num(), value.Denom())
	limit := big.NewInt(int64(bet) * MaxMultiplier)
	if payout.Cmp(limit) > 0 {
		payout = limit
	}
	return int(payout.Int64())
}

// FormatMultiplier 把倍数写成截断到两位小数的字符串，只用于显示。
func FormatMultiplier(multiplier *big.Rat) string {
	hundredths := new(big.Int).Mul(multiplier.Num(), big.NewInt(100))
	hundredths.Quo(hundredths, multiplier.Denom())
	whole, fraction := new(big.Int).QuoRem(hundredths, big.NewInt(100), new(big.Int))
	return fmt.Sprintf("%s.%02d", whole, fraction.Int64())
}

// Round 是一局高低牌在服务端保存的状态。Multiplier 是累计倍数的精确有理数(big.Rat 的字符串)；RtpPercent 是开局时的返还率，
// 管理员中途改了配置也按开局时的算。
type Round struct {
	RtpPercent int    `json:"rtp_percent"`
	Multiplier string `json:"multiplier"`
	// Cards 是按顺序翻开的所有牌，最后一张是当前牌；Moves[i] 是在 Cards[i] 上做的选择，比 Cards 少一个。
	Cards []Card   `json:"cards"`
	Moves []string `json:"moves"`
	Skips int      `json:"skips"`
	// Wins 是猜对的次数，至少猜对一次才能兑现。
	Wins int  `json:"wins"`
	Lost bool `json:"lost,omitempty"`
}

// NewRound 开一局，first 是开局翻开的牌。
func NewRound(rtpPercent int, first Card) Round {
	return Round{RtpPercent: rtpPercent, Multiplier: "1", Cards: []Card{first}, Moves: []string{}}
}

// Current 返回当前牌。
func (r *Round) Current() Card {
	return r.Cards[len(r.Cards)-1]
}

// Value 返回累计倍数。
func (r *Round) Value() (*big.Rat, error) {
	multiplier, ok := new(big.Rat).SetString(r.Multiplier)
	if !ok || multiplier.Sign() <= 0 {
		return nil, fmt.Errorf("invalid hilo multiplier %q", r.Multiplier)
	}
	return multiplier, nil
}

// Allowed 表示现在可以在当前牌上这样选择：这一局没输，猜法是这张牌能猜的，或者还有换牌次数。
func (r *Round) Allowed(guess string) bool {
	if r.Lost {
		return false
	}
	if guess == Skip {
		return r.Skips < MaxSkips
	}
	return Hits(r.Current().Rank, guess) > 0
}

// Play 在当前牌上做一次选择，next 是翻开的下一张牌。返回这一局是否还在继续：猜错时为 false，这一局输了；换牌总是继续。
// 现在不能这样选择(见 Allowed)时返回 ErrIllegalGuess，状态不变。
func (r *Round) Play(guess string, next Card) (bool, error) {
	if !r.Allowed(guess) {
		return false, ErrIllegalGuess
	}
	rank := r.Current().Rank
	if guess == Skip {
		r.Skips++
		r.Cards, r.Moves = append(r.Cards, next), append(r.Moves, guess)
		return true, nil
	}
	hits := Hits(rank, guess)
	multiplier, err := r.Value()
	if err != nil {
		return false, err
	}
	r.Cards, r.Moves = append(r.Cards, next), append(r.Moves, guess)
	if !Correct(rank, guess, next.Rank) {
		r.Lost = true
		return false, nil
	}
	r.Multiplier = multiplier.Mul(multiplier, StepMultiplier(r.RtpPercent, hits)).String()
	r.Wins++
	return true, nil
}
