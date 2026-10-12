// Package crash 是游戏中心火箭的规则：单人计时局，开局时服务端决定爆点，倍数随时间上升，玩家要在爆炸之前兑现。
//
// 爆点：取随机整数 r ∈ [1, 2^52]，爆点(百分之一倍) = floor(RTP × 2^52 / r)，小于 MinPoint 记为 MinPoint(开局即爆)，
// 最多 MaxPoint(1000 倍)。记 X = RTP × 2^52。对 101 ≤ m ≤ MaxPoint，floor(X/r) ≥ m 当且仅当 r ≤ floor(X/m)，所以
// P(爆点 ≥ m) = floor(X/m) / 2^52，比 RTP/m 少不到 2^-52，与 RTP/m 的差别在任何实际局数里都看不出来。
// 计算出的爆点低于 1.00× 的概率是 1 − floor(X/100)/2^52 ≈ 1 − RTP/100(返还率 96% 时约 4%)，这一部分记为 1.00×；
// 爆点正好是 1.00×(floor(X/r) = 100)的一局也在开局那一刻就爆了，两者合计开局即爆的概率是 1 − floor(X/101)/2^52 ≈ 1 − RTP/101。
//
// 倍数随时间增长：开局后 t 毫秒的倍数(百分之一倍)是 m(t) = floor(100 × e^(Growth × t))，服务端用 float64 计算，只作为时间表。
// 倍数第一次达到爆点的时刻就是爆炸的时刻：玩家在 t 时刻兑现，m(t) 低于爆点就按 m(t) 派彩，否则已经爆了，输掉下注。
// 这与"t 早于 ln(爆点/100)/Growth 才能兑现"是同一个判定：爆点是整数，100 × e^(Growth × t) < 爆点 与 m(t) < 爆点等价；
// 直接比较 m(t)，时间表与判定用的是同一次浮点计算，不会因为舍入而互相矛盾。
//
// 自动兑现：开局时设一个倍数 a(101 到 MaxPoint)。a 不高于爆点时，倍数到达 a 的那一刻按 a 兑现；否则火箭先爆了，输掉下注。
// 于是设自动兑现 a 的玩法赢的概率是 P(爆点 ≥ a) = floor(X/a)/2^52，派彩 a/100 倍，期望返还不超过 RTP；
// 手动兑现要求倍数严格低于爆点，同样的倍数赢的概率只会更低。派彩 = floor(下注 × 倍数 / 100)，向下取整只会更低。
package crash

import (
	"crypto/rand"
	"math"
	"math/big"
)

const (
	// Growth 是倍数按指数增长的速率(每毫秒)：2 倍约 11.6 秒，10 倍约 38 秒，1000 倍约 115 秒。
	Growth = 0.00006
	// MinPoint、MaxPoint 是爆点的范围(百分之一倍)：1.00× 到 1000×。
	MinPoint = 100
	MaxPoint = 100000
	// MinAutoCashout 是自动兑现最低能设的倍数(百分之一倍)。
	MinAutoCashout = 101
	// randomSpan 是随机数 r 的取值个数 2^52。
	randomSpan = int64(1) << 52
)

// PointOf 返回随机数 r(1 ≤ r ≤ 2^52)、返还率 rtpPercent(不超过 2000，配置只允许 50 到 99)对应的爆点(百分之一倍)：
// floor(rtpPercent × 2^52 / r)，限定在 [MinPoint, MaxPoint]。
func PointOf(rtpPercent int, r int64) int {
	point := (int64(rtpPercent) << 52) / r
	return int(min(max(point, MinPoint), MaxPoint))
}

// NewPoint 用密码学安全的随机数取 r ∈ [1, 2^52]，返回爆点。
func NewPoint(rtpPercent int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(randomSpan))
	if err != nil {
		return 0, err
	}
	return PointOf(rtpPercent, n.Int64()+1), nil
}

// MultiplierAt 返回开局后 elapsedMs 毫秒的倍数(百分之一倍) floor(100 × e^(Growth × t))。开局之前按 1.00×；
// 超过 MaxPoint 时记为 MaxPoint，那时任何一局都已经爆了。
func MultiplierAt(elapsedMs int64) int {
	if elapsedMs <= 0 {
		return MinPoint
	}
	multiplier := math.Floor(100 * math.Exp(Growth*float64(elapsedMs)))
	if multiplier >= MaxPoint {
		return MaxPoint
	}
	return int(multiplier)
}

// TimeOf 返回倍数第一次达到 multiplier(百分之一倍)的时刻(开局后的毫秒数)，与 MultiplierAt 一致：
// MultiplierAt(TimeOf(m)) ≥ m，且更早的时刻都低于 m。
func TimeOf(multiplier int) int64 {
	if multiplier <= MinPoint {
		return 0
	}
	multiplier = min(multiplier, MaxPoint)
	t := int64(math.Ceil(math.Log(float64(multiplier)/100) / Growth))
	for t > 0 && MultiplierAt(t-1) >= multiplier {
		t--
	}
	for MultiplierAt(t) < multiplier {
		t++
	}
	return t
}

// EndsAt 返回一局自己结束的时刻(开局后的毫秒数)：设了自动兑现且不高于爆点时是自动兑现的时刻，否则是爆炸的时刻。
func EndsAt(point int, auto int) int64 {
	if auto > 0 && auto <= point {
		return TimeOf(auto)
	}
	return TimeOf(point)
}

// Settle 判定爆点 point、自动兑现 auto(0 表示不自动)的一局在开局后 elapsedMs 毫秒时的结果。cashout 表示玩家在这一刻请求兑现。
// over 为真表示这一局已经结束，multiplier 是兑现的倍数(百分之一倍)，爆炸时为 0；over 为假表示火箭还在飞(只在不兑现时)。
func Settle(point int, auto int, elapsedMs int64, cashout bool) (over bool, multiplier int) {
	current := MultiplierAt(elapsedMs)
	if auto > 0 && auto <= point && current >= auto {
		return true, auto
	}
	if current >= point {
		return true, 0
	}
	if cashout {
		return true, current
	}
	return false, 0
}

// Payout 返回下注 bet 在倍数 multiplier(百分之一倍，不超过 MaxPoint)兑现的派彩 floor(bet × multiplier / 100)，
// 最多 bet × 1000；爆炸(multiplier 为 0)时为 0。
func Payout(bet int, multiplier int) int {
	return bet * min(multiplier, MaxPoint) / 100
}
