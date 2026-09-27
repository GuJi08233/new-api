// Package mines 是游戏中心挖宝的规则：5×5 共 25 格，玩家开局时选 1 到 24 个雷，服务端用密码学安全的随机数布雷。
//
// 玩家一格一格地翻：翻到雷就输掉下注，揭示全部雷；翻到安全格可以继续翻，也可以兑现。有 m 个雷时，翻开 k 个安全格之后
// 兑现的倍数是 RTP/100 × C(25,k) / C(25−m,k)。前 k 格都是安全格的概率正好是 C(25−m,k) / C(25,k)，两者相乘就是 RTP/100：
// 翻到第几格兑现都一样，玩家不论怎么选格子、翻几格再兑现，理论返还率都恰好是 RTP(格子的位置对称，玩家能看到的只有
// 已经翻开的格子都是安全格，任何玩法都等于"翻开 k 格就兑现"的混合)。派彩向下取整到整数游戏币，只会更低。
// 单局派彩最多是下注的 MaxMultiplier 倍，到顶自动兑现；翻开全部安全格也自动兑现。
package mines

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
)

const (
	Cells    = 25
	MinMines = 1
	MaxMines = Cells - 1
	// MaxMultiplier 是单局派彩最多为下注的多少倍。
	MaxMultiplier = 1000
)

// ErrIllegalPick 表示这一格不能翻：越界、已经翻开，或者这一局已经结束。
var ErrIllegalPick = errors.New("illegal mines pick")

// Place 用密码学安全的随机数从 25 格里均匀地选出 mines 个雷，返回升序排列的格子编号(行优先，0 到 24)。
func Place(mines int) ([]int, error) {
	if mines < MinMines || mines > MaxMines {
		return nil, fmt.Errorf("mines must be %d to %d, got %d", MinMines, MaxMines, mines)
	}
	cells := make([]int, Cells)
	for i := range cells {
		cells[i] = i
	}
	// 只洗前 mines 个位置的 Fisher-Yates：每个 mines 元子集被选中的概率相同。
	for i := 0; i < mines; i++ {
		offset, err := rand.Int(rand.Reader, big.NewInt(int64(Cells-i)))
		if err != nil {
			return nil, err
		}
		j := i + int(offset.Int64())
		cells[i], cells[j] = cells[j], cells[i]
	}
	layout := slices.Clone(cells[:mines])
	slices.Sort(layout)
	return layout, nil
}

// Payout 返回下注 bet、有 mines 个雷、翻开 safe 个安全格之后兑现的派彩(含退回的下注)：
// floor(bet × rtpPercent × C(25,safe) / (100 × C(25−mines,safe)))，最多 bet × MaxMultiplier。
// 组合数与乘积用大整数计算，不会溢出；safe 超过安全格总数时没有这样的局面，返回 0。
func Payout(bet int, rtpPercent int, mines int, safe int) int {
	if bet < 1 || safe < 0 || safe > Cells-mines {
		return 0
	}
	numerator := new(big.Int).Binomial(Cells, int64(safe))
	numerator.Mul(numerator, big.NewInt(int64(bet)))
	numerator.Mul(numerator, big.NewInt(int64(rtpPercent)))
	denominator := new(big.Int).Binomial(int64(Cells-mines), int64(safe))
	denominator.Mul(denominator, big.NewInt(100))
	// 两者都是正数，截断除法就是向下取整。
	payout := numerator.Quo(numerator, denominator)
	limit := big.NewInt(int64(bet) * MaxMultiplier)
	if payout.Cmp(limit) > 0 {
		payout = limit
	}
	return int(payout.Int64())
}

// Round 是一局挖宝在服务端保存的状态。Layout 是雷所在的格子，结束之前只有服务端知道；RtpPercent 是开局时的返还率，
// 管理员中途改了配置也按开局时的算。
type Round struct {
	RtpPercent int   `json:"rtp_percent"`
	Mines      int   `json:"mines"`
	Layout     []int `json:"layout"`
	// Revealed 是按翻开顺序排列的安全格。
	Revealed []int `json:"revealed"`
	// Exploded 是踩到的雷，没有踩到时为 -1。
	Exploded int `json:"exploded"`
}

// NewRound 开一局：layout 是 Place 布好的雷。
func NewRound(rtpPercent int, layout []int) Round {
	return Round{RtpPercent: rtpPercent, Mines: len(layout), Layout: layout, Revealed: []int{}, Exploded: -1}
}

// Lost 表示踩到了雷。
func (r *Round) Lost() bool {
	return r.Exploded >= 0
}

// Cleared 表示所有安全格都翻开了。
func (r *Round) Cleared() bool {
	return len(r.Revealed) == Cells-r.Mines
}

// Pick 翻开 index 这一格，返回它是不是雷。越界、已经翻开的格子，以及已经踩雷或翻完的一局返回 ErrIllegalPick。
func (r *Round) Pick(index int) (bool, error) {
	if r.Lost() || r.Cleared() || index < 0 || index >= Cells || slices.Contains(r.Revealed, index) {
		return false, ErrIllegalPick
	}
	if slices.Contains(r.Layout, index) {
		r.Exploded = index
		return true, nil
	}
	r.Revealed = append(r.Revealed, index)
	return false, nil
}

// Cashout 返回现在兑现的派彩。还没翻开安全格时不能兑现，由调用方检查。
func (r *Round) Cashout(bet int) int {
	return Payout(bet, r.RtpPercent, r.Mines, len(r.Revealed))
}

// NextCashout 返回再翻开一个安全格之后兑现的派彩。
func (r *Round) NextCashout(bet int) int {
	return Payout(bet, r.RtpPercent, r.Mines, len(r.Revealed)+1)
}

// AutoCashout 表示这一局要自动兑现：安全格都翻开了，或者派彩已经到了封顶。
func (r *Round) AutoCashout(bet int) bool {
	return r.Cleared() || r.Cashout(bet) >= bet*MaxMultiplier
}
