// Package plinko 是游戏中心 Plinko 的规则：钉板有 Rows 排钉子，小球从顶端落下，每经过一排等概率地向左或向右偏半格，
// 最后落进底部 Slots 格中的一格。落点从左数起等于向右的次数，落进第 k 格的概率是 Weights[k] / 2^Rows。
//
// 每颗球往哪边偏由服务端用密码学安全的随机数决定，浏览器只照着这条路径播放动画。1、10、100 三档投入共用一张倍率表，
// 两边对称、越往边上越高；游戏币可以有小数，1 币档也按倍率派彩。理论返还率由 Payback 算出，正好 80%，玩家长期是输的。
// 改倍率表时要同步更新测试。
package plinko

import "crypto/rand"

const (
	Rows  = 12
	Slots = Rows + 1
)

// Stakes 是三档投入的挡位(整币)，从小到大排列。每颗球扣挡位那么多游戏币。
var Stakes = []int{1, 10, 100}

// Multipliers 是三档共用的倍率表，单位十分之一倍，从左到右是 13 4 3 1.8 1 0.5 0.2 0.5 1 1.8 3 4 13。
var Multipliers = [Slots]int{130, 40, 30, 18, 10, 5, 2, 5, 10, 18, 30, 40, 130}

// Payout 返回投入 stake(任意记账单位)的一颗球落进第 slot 格的派彩(含扣掉的投入)：stake × 倍率，零头舍去。
func Payout(stake int, slot int) int {
	return stake * Multipliers[slot] / 10
}

// Weights 是落进每一格的路径数 C(Rows, k)，合计 2^Rows。
var Weights = [Slots]int{1, 12, 66, 220, 495, 792, 924, 792, 495, 220, 66, 12, 1}

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

// Payback 返回倍率按落点权重加总与扣币按同样权重加总(都以十分之一倍计)，两者之比就是理论返还率，三档相同。
func Payback() (paid int, cost int) {
	for slot, weight := range Weights {
		paid += weight * Multipliers[slot]
		cost += weight * 10
	}
	return paid, cost
}
