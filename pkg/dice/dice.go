// Package dice 是游戏中心骰子的规则：服务端掷出 0..9999 之间的一个整数，玩家事先选好派彩倍数与方向。
//
// 押 m 倍时赢的概率 = 返还率 / m：获胜阈值 threshold = RtpPercent × 100 / m(整数除法，单位万分之一)。方向 under 掷出
// 小于 threshold 赢，方向 over 掷出不小于 10000 − threshold 赢，两个方向都正好有 threshold 个点数获胜。赢了派彩 = 下注 × m
// (含本金)，输了为 0，所以每下注 1 个游戏币平均派彩 threshold × m / 10000 个：阈值向下取整只会让它低于配置的返还率，
// 返还率只能配置在 50%–99% 之间，玩家长期是输的，骰子不会成为提款机。点数由密码学安全的随机数决定，浏览器只负责播放。
package dice

import (
	"crypto/rand"
	"math/big"
	"slices"
)

// Sides 是点数的个数：掷出 0..Sides-1，阈值与概率都以万分之一为单位。
const Sides = 10000

// 押注方向。
const (
	Under = "under" // 掷出小于阈值赢
	Over  = "over"  // 掷出不小于 Sides − 阈值赢
)

// Multipliers 是可选的派彩倍数，从小到大排列。倍数直接乘进派彩，只能从这几档里选；最高 100 倍在 96% 返还率下还有
// 96 个点数获胜。
var Multipliers = []int{2, 3, 4, 5, 10, 20, 50, 100}

// ValidMultiplier 表示 multiplier 是可选的一档倍数。
func ValidMultiplier(multiplier int) bool {
	return slices.Contains(Multipliers, multiplier)
}

// ValidDirection 表示 direction 是 under 或 over。
func ValidDirection(direction string) bool {
	return direction == Under || direction == Over
}

// Threshold 返回返还率 rtpPercent(百分比)下押 multiplier 倍的获胜阈值：rtpPercent × 100 / multiplier，向下取整。
// multiplier 必须是 Multipliers 里的一档。
func Threshold(rtpPercent int, multiplier int) int {
	return rtpPercent * (Sides / 100) / multiplier
}

// Wins 表示掷出 roll 时，方向 direction、阈值 threshold 的一注是否赢。方向不是 under、over 的一注永远不赢。
func Wins(roll int, threshold int, direction string) bool {
	switch direction {
	case Under:
		return roll < threshold
	case Over:
		return roll >= Sides-threshold
	}
	return false
}

// Roll 用密码学安全的随机数均匀地掷出 0..Sides-1 之间的一个整数。
func Roll() (int, error) {
	roll, err := rand.Int(rand.Reader, big.NewInt(Sides))
	if err != nil {
		return 0, err
	}
	return int(roll.Int64()), nil
}
