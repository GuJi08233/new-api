// Package keno 是游戏中心基诺的规则：玩家从 1–40 里选 1 到 10 个号码，服务端开出 10 个不同的号码，按选中的个数查赔付表。
//
// 选 k 个号、中 h 个的概率是超几何分布 C(k,h)·C(40−k,10−h)/C(40,10)。赔付表(Paytable)按选号个数分行，每行的理论返还率
// 都在 92% 到 95% 之间(见测试)，单注最高派彩 1000 倍，与挖宝、高低牌的上限一致：玩家长期是输的。开奖号码由密码学安全的
// 随机数决定，浏览器只负责一个个翻开。
package keno

import (
	"crypto/rand"
	"errors"
	"math/big"
	"slices"
)

const (
	// Numbers 是号码的个数(1..Numbers)，Draws 是每局开出几个，MaxPicks 是最多选几个。
	Numbers  = 40
	Draws    = 10
	MaxPicks = 10
)

var ErrInvalidPicks = errors.New("invalid keno picks")

// Paytable[k][h] 是选 k 个号、中 h 个时的派彩倍数，以百分之一倍计(含本金)，0 表示不中奖。
var Paytable = [MaxPicks + 1][]int{
	1:  {0, 380},
	2:  {0, 100, 970},
	3:  {0, 0, 310, 4200},
	4:  {0, 0, 170, 780, 11500},
	5:  {0, 0, 110, 410, 2000, 31000},
	6:  {0, 0, 0, 330, 1100, 10500, 100000},
	7:  {0, 0, 0, 180, 650, 3700, 28000, 100000},
	8:  {0, 0, 0, 140, 410, 1400, 9300, 52000, 100000},
	9:  {0, 0, 0, 0, 360, 1200, 5500, 36500, 100000, 100000},
	10: {0, 0, 0, 0, 260, 650, 2600, 15500, 78500, 100000, 100000},
}

// ValidPicks 表示 picks 是 1 到 MaxPicks 个互不相同、在 1..Numbers 之内的号码。
func ValidPicks(picks []int) bool {
	if len(picks) < 1 || len(picks) > MaxPicks {
		return false
	}
	seen := make(map[int]bool, len(picks))
	for _, n := range picks {
		if n < 1 || n > Numbers || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// Hits 返回 picks 里有几个在 drawn 里。
func Hits(picks []int, drawn []int) int {
	hits := 0
	for _, n := range picks {
		if slices.Contains(drawn, n) {
			hits++
		}
	}
	return hits
}

// Multiplier 返回选 len(picks) 个号、中 hits 个的派彩倍数(百分之一倍)。picks 必须合法。
func Multiplier(picks int, hits int) int {
	row := Paytable[picks]
	if hits < 0 || hits >= len(row) {
		return 0
	}
	return row[hits]
}

// Draw 用密码学安全的随机数开出 Draws 个互不相同的号码，按开出的先后排列。
func Draw() ([]int, error) {
	pool := make([]int, Numbers)
	for i := range pool {
		pool[i] = i + 1
	}
	drawn := make([]int, 0, Draws)
	for len(drawn) < Draws {
		pick, err := rand.Int(rand.Reader, big.NewInt(int64(len(pool))))
		if err != nil {
			return nil, err
		}
		i := int(pick.Int64())
		drawn = append(drawn, pool[i])
		pool[i] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
	}
	return drawn, nil
}

func choose(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	result := 1.0
	for i := 0; i < k; i++ {
		result = result * float64(n-i) / float64(i+1)
	}
	return result
}

// HitChance 返回选 picks 个号、恰好中 hits 个的概率。
func HitChance(picks int, hits int) float64 {
	return choose(picks, hits) * choose(Numbers-picks, Draws-hits) / choose(Numbers, Draws)
}

// Payback 返回选 picks 个号时的理论返还率。
func Payback(picks int) float64 {
	total := 0.0
	for hits, multiplier := range Paytable[picks] {
		total += HitChance(picks, hits) * float64(multiplier) / 100
	}
	return total
}
