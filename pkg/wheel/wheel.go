// Package wheel 是游戏中心大转盘的规则：转盘上每一格的倍数与权重都固定在代码里。
//
// 每次旋转由服务端用密码学安全的随机数按权重选一格，浏览器只负责把转盘转到这一格。派彩 = 每次花费 × 这一格的倍数，
// 每天的免费次数也按同样的花费计算奖品。权重合计 1000，每一格被选中的概率就是它的权重的千分之几，与转盘上画得多大无关，
// 页面按权重逐格公示。理论返还率由 Payback 算出，正好 80%，玩家长期是输的，转盘不会成为提款机。改格子时要同步更新测试。
package wheel

import (
	"crypto/rand"
	"math/big"
)

// Segment 是转盘上的一格。Multiplier 是派彩相对每次花费的倍数，包含退回的花费，0 就是"谢谢参与"；
// Weight 是这一格被选中的权重。
type Segment struct {
	Multiplier int `json:"multiplier"`
	Weight     int `json:"weight"`
}

// Segments 是转盘上的格子，从指针正下方起按顺时针排列，每格在转盘上画得一样大。"谢谢参与"分成相对的两格，
// 头奖和 10 倍也相对排开，转盘看起来是均匀的。
var Segments = []Segment{
	{Multiplier: 50, Weight: 1},
	{Multiplier: 1, Weight: 250},
	{Multiplier: 3, Weight: 50},
	{Multiplier: 0, Weight: 287},
	{Multiplier: 10, Weight: 5},
	{Multiplier: 2, Weight: 100},
	{Multiplier: 5, Weight: 20},
	{Multiplier: 0, Weight: 287},
}

// Spin 用密码学安全的随机数按权重选一格，返回它在 Segments 里的下标。
func Spin() (int, error) {
	_, total := Payback()
	point, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, err
	}
	return SegmentAt(int(point.Int64())), nil
}

// SegmentAt 把权重合计上的一个点 [0, 权重合计) 换成它所在的格子：从第一格起，每格占去自己权重那么多个点。
func SegmentAt(point int) int {
	for index, segment := range Segments {
		if point < segment.Weight {
			return index
		}
		point -= segment.Weight
	}
	return len(Segments) - 1
}

// Payback 返回每一格的倍数按权重加总的派彩与权重合计，两者之比就是理论返还率。
func Payback() (paid int, total int) {
	for _, segment := range Segments {
		paid += segment.Multiplier * segment.Weight
		total += segment.Weight
	}
	return paid, total
}
