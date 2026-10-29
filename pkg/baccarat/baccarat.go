// Package baccarat 是游戏中心百家乐的规则：8 副牌，每局从完整的一靴牌里随机发牌，按标准补牌规则比较闲、庄两家的点数。
//
// 点数是牌面之和的个位数：A 记 1，2–9 按牌面，10、J、Q、K 记 0。头两张合计 8 或 9 是天生赢家，双方都不再补牌；否则闲家
// 0–5 补一张、6–7 停牌；闲家停牌时庄家 0–5 补一张，闲家补了牌时庄家按自己的点数和闲家第三张牌查表决定。
//
// 可以同时押闲、庄、和、闲对、庄对。派彩(含本金)：闲赢 2 倍；庄赢 1.95 倍(抽 5% 佣金)；和局 9 倍，和局时押闲、押庄退回本金；
// 闲对、庄对(这一家头两张同点)12 倍。理论返还率见 Payback，全部低于 100%：玩家长期是输的。
package baccarat

import (
	"crypto/rand"
	"math/big"
)

const (
	Decks = 8
	// ShoeCards 是一靴牌的张数。
	ShoeCards = Decks * 52
)

// 押注的种类。
const (
	Player     = "player"
	Banker     = "banker"
	Tie        = "tie"
	PlayerPair = "player_pair"
	BankerPair = "banker_pair"
)

// Spots 是可以押的几处，页面按这个顺序排桌面。
var Spots = []string{Player, Banker, Tie, PlayerPair, BankerPair}

// Payouts 是各处押中时的派彩，以百分之一倍计(含本金)。
var Payouts = map[string]int{
	Player:     200,
	Banker:     195,
	Tie:        900,
	PlayerPair: 1200,
	BankerPair: 1200,
}

// Payback 是 8 副牌下各处的理论返还率(按完整的补牌规则精确计算，见测试)。
var Payback = map[string]float64{
	Player:     0.987649,
	Banker:     0.989421,
	Tie:        0.856404,
	PlayerPair: 0.896386,
	BankerPair: 0.896386,
}

// Card 是一张牌：Rank 1–13(A 到 K)，Suit 0–3。
type Card struct {
	Rank int `json:"rank"`
	Suit int `json:"suit"`
}

// Value 是这张牌的点数：A 记 1，10、J、Q、K 记 0。
func (c Card) Value() int {
	if c.Rank >= 10 {
		return 0
	}
	return c.Rank
}

// Hand 是一家的牌与点数。
type Hand struct {
	Cards []Card `json:"cards"`
	Total int    `json:"total"`
}

func total(cards []Card) int {
	sum := 0
	for _, card := range cards {
		sum += card.Value()
	}
	return sum % 10
}

// Result 是一局的结果。Winner 是 player、banker 或 tie。
type Result struct {
	Player     Hand   `json:"player"`
	Banker     Hand   `json:"banker"`
	Winner     string `json:"winner"`
	PlayerPair bool   `json:"player_pair"`
	BankerPair bool   `json:"banker_pair"`
}

// bankerDraws 表示庄家点数 banker、闲家补的第三张牌点数 third 时庄家要不要补牌。
func bankerDraws(banker int, third int) bool {
	switch banker {
	case 0, 1, 2:
		return true
	case 3:
		return third != 8
	case 4:
		return third >= 2 && third <= 7
	case 5:
		return third >= 4 && third <= 7
	case 6:
		return third == 6 || third == 7
	}
	return false
}

// Deal 按发牌顺序(闲、庄、闲、庄，再按规则补牌)从 next 取牌，算出一局的结果。
func Deal(next func() Card) Result {
	player := []Card{next()}
	banker := []Card{next()}
	player = append(player, next())
	banker = append(banker, next())
	p, b := total(player), total(banker)
	if p < 8 && b < 8 {
		if p <= 5 {
			third := next()
			player = append(player, third)
			if bankerDraws(b, third.Value()) {
				banker = append(banker, next())
			}
		} else if b <= 5 {
			banker = append(banker, next())
		}
	}
	result := Result{
		Player:     Hand{Cards: player, Total: total(player)},
		Banker:     Hand{Cards: banker, Total: total(banker)},
		PlayerPair: player[0].Rank == player[1].Rank,
		BankerPair: banker[0].Rank == banker[1].Rank,
	}
	switch {
	case result.Player.Total > result.Banker.Total:
		result.Winner = Player
	case result.Banker.Total > result.Player.Total:
		result.Winner = Banker
	default:
		result.Winner = Tie
	}
	return result
}

// Win 返回押在 spot 上的 amount(最小单位)这一注的派彩(含本金，没中为 0)，向下取整到最小单位。
func (r Result) Win(spot string, amount int) int {
	hit := false
	switch spot {
	case Player, Banker:
		if r.Winner == Tie {
			return amount
		}
		hit = r.Winner == spot
	case Tie:
		hit = r.Winner == Tie
	case PlayerPair:
		hit = r.PlayerPair
	case BankerPair:
		hit = r.BankerPair
	}
	if !hit {
		return 0
	}
	return amount * Payouts[spot] / 100
}

// ValidSpot 表示 spot 是可以押的一处。
func ValidSpot(spot string) bool {
	_, ok := Payouts[spot]
	return ok
}

// Play 从一靴洗好的新牌里发一局：每张牌用密码学安全的随机数从剩下的牌里抽。
func Play() (Result, error) {
	shoe := make([]Card, 0, ShoeCards)
	for deck := 0; deck < Decks; deck++ {
		for rank := 1; rank <= 13; rank++ {
			for suit := 0; suit < 4; suit++ {
				shoe = append(shoe, Card{Rank: rank, Suit: suit})
			}
		}
	}
	var err error
	result := Deal(func() Card {
		if err != nil {
			return shoe[0]
		}
		var pick *big.Int
		if pick, err = rand.Int(rand.Reader, big.NewInt(int64(len(shoe)))); err != nil {
			return shoe[0]
		}
		i := int(pick.Int64())
		card := shoe[i]
		shoe[i] = shoe[len(shoe)-1]
		shoe = shoe[:len(shoe)-1]
		return card
	})
	return result, err
}
