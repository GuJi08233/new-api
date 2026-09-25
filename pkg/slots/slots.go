// Package slots 是游戏中心老虎机的规则：三个转轮、一条中线，转轮和赔付表都固定在代码里。
//
// 每次旋转由服务端用密码学安全的随机数给每个转轮独立、均匀地选一个停点，浏览器只负责把转轮转到这些停点。
// 派彩只看中线上从最左边起连续相同的符号。理论返还率由 Payback 穷举所有停点组合得出，必须低于 100%：
// 老虎机让玩家拿游戏币碰运气，长期来看不能凭空产出游戏币。改转轮或赔付表时要同步更新测试里的返还率。
package slots

import (
	"crypto/rand"
	"math/big"
	"sync"
)

// 符号标识，浏览器按它画出对应的图案。
const (
	Seven   = "seven"
	Diamond = "diamond"
	Bell    = "bell"
	Grape   = "grape"
	Orange  = "orange"
	Lemon   = "lemon"
	Cherry  = "cherry"
)

// Strips 是三个转轮的符号排列，每个转轮 30 个停点：7 一个、钻石两个、铃铛三个、葡萄四个、橙子五个、
// 柠檬七个、樱桃八个。三个转轮的符号数量相同、排列不同，越值钱的符号越少见。
var Strips = [3][]string{
	{Cherry, Lemon, Orange, Cherry, Grape, Lemon, Bell, Cherry, Orange, Lemon, Seven, Cherry, Grape, Lemon, Orange,
		Cherry, Diamond, Lemon, Grape, Cherry, Bell, Orange, Lemon, Cherry, Grape, Diamond, Cherry, Orange, Lemon, Bell},
	{Lemon, Cherry, Bell, Orange, Cherry, Lemon, Grape, Cherry, Diamond, Lemon, Orange, Cherry, Grape, Lemon, Seven,
		Cherry, Orange, Bell, Lemon, Cherry, Grape, Orange, Cherry, Lemon, Diamond, Cherry, Grape, Orange, Lemon, Bell},
	{Orange, Cherry, Lemon, Grape, Cherry, Bell, Lemon, Cherry, Orange, Diamond, Lemon, Cherry, Grape, Orange, Lemon,
		Cherry, Bell, Seven, Lemon, Cherry, Orange, Grape, Cherry, Lemon, Diamond, Cherry, Orange, Grape, Lemon, Bell},
}

// Pay 是一个符号的赔付倍数：中线从最左边起连续三个、只有前两个、只有第一个是这个符号时，
// 派彩分别是下注的多少倍。派彩包含退回的下注，倍数为 1 就是原数退回。
type Pay struct {
	Symbol string `json:"symbol"`
	Three  int    `json:"three"`
	Two    int    `json:"two"`
	One    int    `json:"one"`
}

// Paytable 是赔付表，每个符号一行。
var Paytable = []Pay{
	{Symbol: Seven, Three: 50, Two: 10},
	{Symbol: Diamond, Three: 30, Two: 5},
	{Symbol: Bell, Three: 20, Two: 2},
	{Symbol: Grape, Three: 15, Two: 2},
	{Symbol: Orange, Three: 12, Two: 2},
	{Symbol: Lemon, Three: 10, Two: 2},
	{Symbol: Cherry, Three: 10, Two: 2, One: 1},
}

// Result 是转轮停下后的样子。Matched 是中线从最左边起构成中奖组合的格数，没有中奖时为 0。
type Result struct {
	Stops      [3]int
	Line       [3]string
	Multiplier int
	Matched    int
}

// StopAt 给出三个转轮停在这些停点时的中线与派彩倍数。
func StopAt(stops [3]int) Result {
	result := Result{Stops: stops}
	for reel, stop := range stops {
		result.Line[reel] = Strips[reel][stop]
	}
	line := result.Line
	for _, pay := range Paytable {
		if line[0] != pay.Symbol {
			continue
		}
		switch {
		case line[1] == pay.Symbol && line[2] == pay.Symbol:
			result.Multiplier, result.Matched = pay.Three, 3
		case line[1] == pay.Symbol:
			result.Multiplier, result.Matched = pay.Two, 2
		case pay.One > 0:
			result.Multiplier, result.Matched = pay.One, 1
		}
		return result
	}
	return result
}

// Spin 用密码学安全的随机数给每个转轮独立、均匀地选一个停点。
func Spin() (Result, error) {
	var stops [3]int
	for reel, strip := range Strips {
		stop, err := rand.Int(rand.Reader, big.NewInt(int64(len(strip))))
		if err != nil {
			return Result{}, err
		}
		stops[reel] = int(stop.Int64())
	}
	return StopAt(stops), nil
}

// Payback 穷举三个转轮的所有停点组合，返回派彩倍数之和与组合总数，两者之比就是理论返还率。
// 转轮和赔付表都是常量，只在第一次调用时计算。
var Payback = sync.OnceValues(func() (paid int, combos int) {
	for first := range Strips[0] {
		for second := range Strips[1] {
			for third := range Strips[2] {
				paid += StopAt([3]int{first, second, third}).Multiplier
				combos++
			}
		}
	}
	return paid, combos
})
