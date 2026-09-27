// Package scratch 是游戏中心刮刮乐的规则：每张票 3×3 共 9 格，每格印着一个金额，刮出 3 格金额相同即中这个金额，
// 一张票最多中一个金额。
//
// 票价 1、5、20 三档共用一张奖品表，奖品是票价的倍数。购买时服务端先用密码学安全的随机数按权重抽出这张票的奖品，再生成
// 相符的票面：中奖金额恰好出现 3 次，其他格子从奖品表里的其他金额中选、每个金额最多出现 2 次；不中奖的票任何金额都不会
// 出现 3 次。结果在购买时就已确定，浏览器只负责刮开的交互。
//
// 权重合计 10000：中奖(含 1 倍回本)的概率 3153/10000 ≈ 31.5%，经常能刮到小奖；平均每花 1 个游戏币派彩 8000/10000 个，
// 理论返还率正好 80%，最高奖 100 倍，玩家长期是输的。返还率由 Payback 算出，改奖品表时要同步更新测试。
package scratch

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// CellCount 是一张票的格数(3 行 3 列，按行排列)。
const CellCount = 9

// TotalWeight 是抽奖的权重合计：每张票在 [0, TotalWeight) 里均匀取一个点，落在哪一行奖品的权重里就中哪一行，
// 落在所有奖品之后就不中奖。
const TotalWeight = 10000

// ErrUnknownPrize 表示要生成的票面奖品不在奖品表里。
var ErrUnknownPrize = errors.New("scratch: prize is not in the prize table")

// Prize 是奖品表的一行：刮出 3 格 Multiple 倍票价的金额即中奖，每张票中这一行的概率是 Weight / TotalWeight。
type Prize struct {
	Multiple int `json:"multiple"`
	Weight   int `json:"weight"`
}

// Prizes 是三档票价共用的奖品表，按倍数从小到大排列。1 倍是回本；10 倍以上的大奖合计 1.53%。
var Prizes = []Prize{
	{Multiple: 1, Weight: 1500},
	{Multiple: 2, Weight: 1000},
	{Multiple: 3, Weight: 300},
	{Multiple: 5, Weight: 200},
	{Multiple: 10, Weight: 100},
	{Multiple: 20, Weight: 40},
	{Multiple: 50, Weight: 10},
	{Multiple: 100, Weight: 3},
}

// Ticket 是一张票：Cells 是 9 格金额各是票价的几倍(按行排列)，Multiple 是中奖金额是票价的几倍，0 表示没有中奖。
type Ticket struct {
	Multiple int
	Cells    [CellCount]int
}

// PrizeAt 把权重合计上的一个点 [0, TotalWeight) 换成奖品倍数：从第一行起每行占去自己权重那么多个点，落在所有奖品之后为 0。
func PrizeAt(point int) int {
	for _, prize := range Prizes {
		if point < prize.Weight {
			return prize.Multiple
		}
		point -= prize.Weight
	}
	return 0
}

// NewTicket 生成奖品为 multiple 倍(0 表示不中奖)的票面：中奖金额放 3 格；奖品表里的其他金额每个准备两张，从中不放回地抽够
// 其余格子，于是每个金额最多出现 2 次、不中奖的票凑不出 3 个相同的金额；最后打乱 9 格的位置，中奖的 3 格可能在任何位置。
// random(n) 返回 [0, n) 之间的一个均匀整数。
func NewTicket(multiple int, random func(n int) (int, error)) (Ticket, error) {
	decoys := make([]int, 0, 2*len(Prizes))
	known := multiple == 0
	for _, prize := range Prizes {
		if prize.Multiple == multiple {
			known = true
			continue
		}
		decoys = append(decoys, prize.Multiple, prize.Multiple)
	}
	if !known {
		return Ticket{}, ErrUnknownPrize
	}
	ticket := Ticket{Multiple: multiple}
	filled := 0
	if multiple > 0 {
		for ; filled < 3; filled++ {
			ticket.Cells[filled] = multiple
		}
	}
	for ; filled < CellCount; filled++ {
		pick, err := random(len(decoys))
		if err != nil {
			return Ticket{}, err
		}
		ticket.Cells[filled] = decoys[pick]
		decoys[pick] = decoys[len(decoys)-1]
		decoys = decoys[:len(decoys)-1]
	}
	for cell := CellCount - 1; cell > 0; cell-- {
		swap, err := random(cell + 1)
		if err != nil {
			return Ticket{}, err
		}
		ticket.Cells[cell], ticket.Cells[swap] = ticket.Cells[swap], ticket.Cells[cell]
	}
	return ticket, nil
}

// Buy 用密码学安全的随机数抽一张票：先在权重合计上均匀取一个点决定奖品，再生成与奖品相符的票面。
func Buy() (Ticket, error) {
	point, err := cryptoRandom(TotalWeight)
	if err != nil {
		return Ticket{}, err
	}
	return NewTicket(PrizeAt(point), cryptoRandom)
}

// Payback 返回奖品倍数按权重加总的派彩与权重合计，两者之比就是理论返还率。
func Payback() (paid int, total int) {
	for _, prize := range Prizes {
		paid += prize.Multiple * prize.Weight
	}
	return paid, TotalWeight
}

// cryptoRandom 用密码学安全的随机数返回 [0, n) 之间的一个均匀整数。
func cryptoRandom(n int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}
