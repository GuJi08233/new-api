// Package roulette 是游戏中心轮盘的规则：欧式单零轮盘，号码 0–36，0 是绿色，1–36 按标准轮盘分红黑。
//
// 每次旋转由服务端用密码学安全的随机数均匀地开出一个号码，浏览器只负责把轮盘转到这个号码。一次旋转可以同时下多注，
// 注型与赔率(不含本金)：单号 straight 35:1；红 red / 黑 black、单 odd / 双 even、小 low(1–18) / 大 high(19–36) 1:1；
// 打 dozen(1–12、13–24、25–36) 与列 column(桌面上的三列) 2:1。开出 0 时所有外围注都输，只有押 0 的单号中。
// 覆盖 k 个号码的一注中了派彩 36/k 倍(含本金)，所以不管怎么下注，每一注的理论返还率都恰好是 Payback = 36/37 ≈ 97.3%，
// 玩家长期是输的。返还率由赔率决定、不能配置，由测试穷举 37 个号码锁定。
package roulette

import (
	"crypto/rand"
	"math/big"
	"slices"
)

// Pockets 是号码的个数：0..36。
const Pockets = 37

// MaxBets 是一次旋转最多下几注。
const MaxBets = 50

// Payback 是任何一注的理论返还率：每下注 1 个游戏币，37 个号码的派彩加起来是 36 个。
const Payback = 36.0 / Pockets

// 注型。只有单号、打与列带号码 Value，其他注型的 Value 必须为 0。
const (
	Straight = "straight" // 单号，Value 是号码 0–36
	Red      = "red"
	Black    = "black"
	Odd      = "odd"
	Even     = "even"
	Low      = "low"    // 1–18
	High     = "high"   // 19–36
	Dozen    = "dozen"  // Value 1、2、3 分别是 1–12、13–24、25–36
	Column   = "column" // Value 1、2、3 分别是 1,4,…,34、2,5,…,35、3,6,…,36
)

// Green 是号码 0 的颜色；1–36 的颜色与押红、押黑的注型同名，是 Red 或 Black。
const Green = "green"

// Payouts 是每种注型的赔率(不含本金)：中了派彩 = 下注 × (赔率 + 1)。
var Payouts = map[string]int{
	Straight: 35,
	Red:      1,
	Black:    1,
	Odd:      1,
	Even:     1,
	Low:      1,
	High:     1,
	Dozen:    2,
	Column:   2,
}

// RedNumbers 是红色号码，其余 1–36 是黑色。
var RedNumbers = []int{1, 3, 5, 7, 9, 12, 14, 16, 18, 19, 21, 23, 25, 27, 30, 32, 34, 36}

// Bet 是一注：注型、号码与下注的游戏币。金额的范围由调用方按下注规则校验。
type Bet struct {
	Type   string `json:"type"`
	Value  int    `json:"value"`
	Amount int    `json:"amount"`
}

// Valid 表示注型已知、号码在这种注型的范围内：单号 0–36，打与列 1–3，其他注型不带号码。
func (b Bet) Valid() bool {
	switch b.Type {
	case Straight:
		return b.Value >= 0 && b.Value < Pockets
	case Dozen, Column:
		return b.Value >= 1 && b.Value <= 3
	case Red, Black, Odd, Even, Low, High:
		return b.Value == 0
	}
	return false
}

// Covers 表示开出 number 时这一注中了。外围注都不含 0。
func (b Bet) Covers(number int) bool {
	if b.Type == Straight {
		return number == b.Value
	}
	if number < 1 || number >= Pockets {
		return false
	}
	switch b.Type {
	case Red:
		return slices.Contains(RedNumbers, number)
	case Black:
		return !slices.Contains(RedNumbers, number)
	case Odd:
		return number%2 == 1
	case Even:
		return number%2 == 0
	case Low:
		return number <= 18
	case High:
		return number >= 19
	case Dozen:
		return (number-1)/12+1 == b.Value
	case Column:
		return (number-1)%3+1 == b.Value
	}
	return false
}

// Win 返回开出 number 时这一注的派彩(含本金)，没中为 0。
func (b Bet) Win(number int) int {
	if !b.Covers(number) {
		return 0
	}
	return b.Amount * (Payouts[b.Type] + 1)
}

// Color 返回号码的颜色：0 是 Green，红色号码是 Red，其余是 Black。
func Color(number int) string {
	switch {
	case number == 0:
		return Green
	case slices.Contains(RedNumbers, number):
		return Red
	}
	return Black
}

// Spin 用密码学安全的随机数均匀地开出 0..36 之间的一个号码。
func Spin() (int, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(Pockets))
	if err != nil {
		return 0, err
	}
	return int(number.Int64()), nil
}
