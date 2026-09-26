// Package plinko 是游戏中心 Plinko 的规则：钉板有 Rows 排钉子，小球从顶端落下，每经过一排等概率地向左或向右偏半格，
// 最后落进底部 Slots 格中的一格。落点从左数起等于向右的次数，落进第 k 格的概率是 Weights[k] / 2^Rows。
//
// 每颗球往哪边偏由服务端用密码学安全的随机数决定，浏览器只照着这条路径播放动画。三档投入的派彩表固定在代码里，
// 两边对称、越往边上越高。游戏币只有整数：1 币档派不出 0.5 倍这样的零头，格子上直接写加减多少个游戏币；
// 10 币档与 100 币档按倍率派彩，倍率只到十分之一，乘出来都是整数。理论返还率由 Payback 算出：倍率两档正好 80%，
// 1 币档平均每颗球输约 0.2 个游戏币，玩家长期是输的。改派彩表时要同步更新测试。
package plinko

import "crypto/rand"

const (
	Rows  = 12
	Slots = Rows + 1
)

// Tier 是一档投入。每颗球先扣 Cost 个游戏币，落进第 k 格派彩 Payouts[k] 个(含扣掉的那部分)。
// Stake 是页面上的挡位：倍率档扣的就是挡位，倍率 = 派彩 ÷ 挡位。1 币档(Delta)的格子直接写加减多少个游戏币，
// 最多扣 2 个，所以先扣 2 个，再派彩 2 加上格子上的数。
type Tier struct {
	Stake   int        `json:"stake"`
	Cost    int        `json:"cost"`
	Delta   bool       `json:"delta"`
	Payouts [Slots]int `json:"payouts"`
}

// Tiers 是三档投入，按挡位从小到大排列。1 币档的格子从左到右是 +10 +8 +4 +2 +1 -1 -2 -1 +1 +2 +4 +8 +10；
// 倍率两档共用一张倍率表 13 4 3 1.8 1 0.5 0.2 0.5 1 1.8 3 4 13。
var Tiers = []Tier{
	{Stake: 1, Cost: 2, Delta: true, Payouts: [Slots]int{12, 10, 6, 4, 3, 1, 0, 1, 3, 4, 6, 10, 12}},
	{Stake: 10, Cost: 10, Payouts: [Slots]int{130, 40, 30, 18, 10, 5, 2, 5, 10, 18, 30, 40, 130}},
	{Stake: 100, Cost: 100, Payouts: [Slots]int{1300, 400, 300, 180, 100, 50, 20, 50, 100, 180, 300, 400, 1300}},
}

// Weights 是落进每一格的路径数 C(Rows, k)，合计 2^Rows。
var Weights = [Slots]int{1, 12, 66, 220, 495, 792, 924, 792, 495, 220, 66, 12, 1}

// TierOf 返回挡位为 stake 的一档，没有这一档时 ok 为 false。
func TierOf(stake int) (Tier, bool) {
	for _, tier := range Tiers {
		if tier.Stake == stake {
			return tier, true
		}
	}
	return Tier{}, false
}

// Drop 用密码学安全的随机数让小球落下一次，返回它在每一排往哪边偏：0 向左，1 向右。
func Drop() ([Rows]int, error) {
	var path [Rows]int
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return path, err
	}
	bits := int(random[0]) | int(random[1])<<8
	for row := range path {
		path[row] = bits >> row & 1
	}
	return path, nil
}

// SlotOf 返回沿 path 落下的小球落进的格子：从左数起，等于向右的次数。
func SlotOf(path [Rows]int) int {
	slot := 0
	for _, right := range path {
		slot += right
	}
	return slot
}

// Payback 返回一档的派彩按落点权重加总与扣币按同样权重加总，两者之比就是理论返还率。
func Payback(tier Tier) (paid int, cost int) {
	for slot, weight := range Weights {
		paid += weight * tier.Payouts[slot]
		cost += weight * tier.Cost
	}
	return paid, cost
}
