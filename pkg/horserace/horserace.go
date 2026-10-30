// Package horserace 是游戏中心赛马的规则。
//
// 六匹马各有固定的实力(Strength，合计 100)，每场的名次按实力依次抽签：先按实力比例抽出第一名，再在剩下的马里按实力比例
// 抽第二名，依此类推。可以押某匹马"独赢"(跑第一)或"位置"(跑进前二)，一场可以押好几注。赔率按各注的中奖概率定，
// 每一注的理论返还率都在 94% 到 96% 之间(见测试)，玩家长期是输的。名次由密码学安全的随机数决定，浏览器只负责把比赛
// 演出来。
package horserace

import (
	"crypto/rand"
	"errors"
	"math/big"
)

const (
	Horses = 6
	// BetWin 押跑第一，BetPlace 押跑进前二。
	BetWin   = "win"
	BetPlace = "place"
)

// Strength 是各匹马的实力，也是它跑第一的百分比概率。
var Strength = [Horses]int{30, 22, 17, 13, 10, 8}

// Odds 是各匹马独赢、位置的派彩倍数，以百分之一倍计(含本金)。
var Odds = map[string][Horses]int{
	BetWin:   {316, 431, 558, 730, 950, 1187},
	BetPlace: {172, 217, 271, 345, 441, 545},
}

var ErrInvalidBet = errors.New("invalid horse race bet")

// ValidBet 表示押的是存在的马与玩法。
func ValidBet(kind string, horse int) bool {
	_, ok := Odds[kind]
	return ok && horse >= 0 && horse < Horses
}

// Wins 表示押 horse 的这一注按 order(名次，第一名在前)中了。
func Wins(kind string, horse int, order []int) bool {
	switch kind {
	case BetWin:
		return len(order) > 0 && order[0] == horse
	case BetPlace:
		return len(order) > 1 && (order[0] == horse || order[1] == horse)
	}
	return false
}

// Multiplier 返回中奖时的派彩倍数(百分之一倍)，没中为 0。
func Multiplier(kind string, horse int, order []int) int {
	if !ValidBet(kind, horse) || !Wins(kind, horse, order) {
		return 0
	}
	return Odds[kind][horse]
}

// Race 用密码学安全的随机数按实力依次抽出名次。
func Race() ([]int, error) {
	return raceWith(func(n int) (int, error) {
		pick, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if err != nil {
			return 0, err
		}
		return int(pick.Int64()), nil
	})
}

func raceWith(next func(n int) (int, error)) ([]int, error) {
	left := make([]int, Horses)
	for i := range left {
		left[i] = i
	}
	order := make([]int, 0, Horses)
	for len(left) > 0 {
		total := 0
		for _, horse := range left {
			total += Strength[horse]
		}
		roll, err := next(total)
		if err != nil {
			return nil, err
		}
		for i, horse := range left {
			roll -= Strength[horse]
			if roll < 0 {
				order = append(order, horse)
				left = append(left[:i], left[i+1:]...)
				break
			}
		}
	}
	return order, nil
}

// WinChance 返回 horse 跑第一的概率。
func WinChance(horse int) float64 {
	return float64(Strength[horse]) / 100
}

// PlaceChance 返回 horse 跑进前二的概率：自己第一，或者别的马第一、自己在剩下的马里抽到第二。
func PlaceChance(horse int) float64 {
	chance := WinChance(horse)
	for other := 0; other < Horses; other++ {
		if other != horse {
			chance += WinChance(other) * float64(Strength[horse]) / float64(100-Strength[other])
		}
	}
	return chance
}

// Payback 返回押 horse 的这种玩法的理论返还率。
func Payback(kind string, horse int) float64 {
	chance := WinChance(horse)
	if kind == BetPlace {
		chance = PlaceChance(horse)
	}
	return chance * float64(Odds[kind][horse]) / 100
}
