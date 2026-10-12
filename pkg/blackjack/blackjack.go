// Package blackjack 是游戏中心 21 点的规则：6 副牌，每局用密码学安全的随机数重新洗牌，庄家软 17 停牌。
//
// 发牌顺序是玩家、庄家明牌、玩家、庄家暗牌。黑杰克(首两张 21 点)赔 3:2，所以下注必须是偶数。庄家明牌是 A 或 10 点时先偷看暗牌：
// 庄家是黑杰克时这一局立即结束，玩家也是黑杰克就是平局；玩家是黑杰克而庄家不是时同样立即结束，按 2.5 倍派彩。
//
// 玩家可以要牌(hit)、停牌(stand)。首两张可以加倍(double)：追加与这一手相同的下注，只再发一张。首两张点数相同(10、J、Q、K 都算
// 10 点)时可以分牌一次(split)：追加与原下注相同的下注，分成两手、各补一张，依次操作；分牌后的两手也可以加倍，不能再分。
// 分开的 A 各只补一张、不能再操作；分牌后的 21 点不算黑杰克。没有保险、没有投降。一手到了 21 点或爆牌就自动停牌。
// 玩家所有手都结束后庄家翻开暗牌，不到 17 点就要牌，17 点(含软 17)停牌；玩家所有手都爆了时庄家不再要牌。
// 派彩按手相加：赢 2 倍、平 1 倍、黑杰克 2.5 倍、输 0，都含退回的下注。
//
// 这套规则(6 副牌、软 17 停牌、任意首两张可加倍、分牌后可加倍、只分一次、分开的 A 各一张、不能投降、庄家偷看)按基本策略玩，
// 理论返还率约 99.5%(庄家优势约 0.45%)，这是公开的标准结果，不在这里用程序计算。
package blackjack

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
)

const (
	Decks    = 6
	ShoeSize = Decks * 52
	// dealerStand 是庄家停牌的点数：不到 17 点要牌，17 点(含软 17)停牌。
	dealerStand = 17
)

// 玩家的操作。
const (
	ActionHit    = "hit"
	ActionStand  = "stand"
	ActionDouble = "double"
	ActionSplit  = "split"
)

// 一手牌的状态。
const (
	HandPlaying   = "playing"   // 轮到这一手操作
	HandWaiting   = "waiting"   // 分牌后排在后面、还没轮到的一手
	HandStand     = "stand"     // 已停牌：玩家停牌、加倍之后、到了 21 点，或者分开的 A
	HandBust      = "bust"      // 爆牌
	HandBlackjack = "blackjack" // 首两张就是 21 点(分牌后的不算)
)

// 一手牌结束后的结果。
const (
	OutcomeWin       = "win"
	OutcomeLose      = "lose"
	OutcomePush      = "push"
	OutcomeBlackjack = "blackjack"
)

// ErrIllegalAction 表示当前一手不能这样操作，或者这一局已经结束。
var ErrIllegalAction = errors.New("illegal blackjack action")

var errShoeEmpty = errors.New("blackjack shoe is empty")

// 牌用 0 到 51 的编号表示：点数 = 编号 % 13 + 1(1 是 A，11、12、13 是 J、Q、K)，花色 = 编号 / 13(0 黑桃、1 红心、2 方块、3 梅花)。

// Rank 返回牌的点数 1 到 13。
func Rank(card int) int {
	return card%13 + 1
}

// Suit 返回牌的花色 0 到 3。
func Suit(card int) int {
	return card / 13
}

// value 返回牌计点时的值：A 先按 1 算，J、Q、K 按 10。
func value(card int) int {
	return min(Rank(card), 10)
}

// Total 返回一手牌的点数：A 先都按 1 算，有 A 且加 10 不爆时其中一张按 11 算，这时是软点数(soft 为真)。
func Total(cards []int) (total int, soft bool) {
	ace := false
	for _, card := range cards {
		total += value(card)
		ace = ace || Rank(card) == 1
	}
	if ace && total+10 <= 21 {
		return total + 10, true
	}
	return total, false
}

// Shuffle 用密码学安全的随机数洗好一整个牌靴(6 副牌)，返回发牌顺序。
func Shuffle() ([]int, error) {
	shoe := make([]int, ShoeSize)
	for i := range shoe {
		shoe[i] = i % 52
	}
	for i := len(shoe) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		k := int(j.Int64())
		shoe[i], shoe[k] = shoe[k], shoe[i]
	}
	return shoe, nil
}

// Hand 是玩家的一手牌。Bet 是这一手的下注，加倍后翻倍。
type Hand struct {
	Cards   []int `json:"cards"`
	Bet     int   `json:"bet"`
	Doubled bool  `json:"doubled,omitempty"`
	Done    bool  `json:"done,omitempty"`
}

// Game 是一局 21 点在服务端保存的状态。Shoe 是还没发的牌，从前往后发；Dealer 的第二张是暗牌。牌靴与暗牌结束之前只有服务端知道，
// 结束后不再需要牌靴，清空不存。
type Game struct {
	Shoe   []int  `json:"shoe,omitempty"`
	Dealer []int  `json:"dealer"`
	Hands  []Hand `json:"hands"`
	// Current 是轮到操作的那一手。
	Current int  `json:"current"`
	Split   bool `json:"split,omitempty"`
	// Peeked 表示庄家明牌是 A 或 10 点、已经偷看过暗牌：这一局还在继续，说明庄家不是黑杰克。
	Peeked bool `json:"peeked,omitempty"`
	Over   bool `json:"over,omitempty"`
}

// Result 是结束后一手牌的结果，Payout 是这一手的派彩(含退回的下注)。
type Result struct {
	Outcome string
	Payout  int
}

// Deal 用洗好的牌靴开一局、下注 bet(正偶数)。有黑杰克时发完牌这一局就结束了。
func Deal(shoe []int, bet int) (*Game, error) {
	if bet < 2 || bet%2 != 0 {
		return nil, fmt.Errorf("blackjack bet must be a positive even number, got %d", bet)
	}
	game := &Game{Shoe: shoe}
	var cards [4]int
	for i := range cards {
		card, err := game.draw()
		if err != nil {
			return nil, err
		}
		cards[i] = card
	}
	game.Hands = []Hand{{Cards: []int{cards[0], cards[2]}, Bet: bet}}
	game.Dealer = []int{cards[1], cards[3]}
	up := value(cards[1])
	game.Peeked = up == 1 || up == 10
	player, _ := Total(game.Hands[0].Cards)
	dealer, _ := Total(game.Dealer)
	if player == 21 || (game.Peeked && dealer == 21) {
		game.Hands[0].Done = true
		game.finish()
	}
	return game, nil
}

// Actions 返回当前一手可以做的操作，这一局结束后为空。加倍、分牌还要玩家的余额够追加下注，由记账时检查。
func (g *Game) Actions() []string {
	if g.Over {
		return []string{}
	}
	hand := g.Hands[g.Current]
	actions := []string{ActionHit, ActionStand}
	if len(hand.Cards) == 2 {
		actions = append(actions, ActionDouble)
		if !g.Split && value(hand.Cards[0]) == value(hand.Cards[1]) {
			actions = append(actions, ActionSplit)
		}
	}
	return actions
}

// Act 让当前一手做 action，返回要追加的下注(加倍、分牌时为这一手原来的下注，其余为 0)。操作不合规则时返回 ErrIllegalAction，
// 状态不变；返回其他错误时状态可能已被改动，不要保存。
func (g *Game) Act(action string) (int, error) {
	if !slices.Contains(g.Actions(), action) {
		return 0, ErrIllegalAction
	}
	hand := &g.Hands[g.Current]
	extra := 0
	switch action {
	case ActionHit:
		card, err := g.draw()
		if err != nil {
			return 0, err
		}
		hand.Cards = append(hand.Cards, card)
	case ActionStand:
		hand.Done = true
	case ActionDouble:
		card, err := g.draw()
		if err != nil {
			return 0, err
		}
		extra = hand.Bet
		hand.Bet += extra
		hand.Doubled = true
		hand.Cards = append(hand.Cards, card)
		hand.Done = true
	case ActionSplit:
		// 两手依次各补一张。分开的 A 只补这一张，直接停牌。
		first, err := g.draw()
		if err != nil {
			return 0, err
		}
		second, err := g.draw()
		if err != nil {
			return 0, err
		}
		extra = hand.Bet
		aces := Rank(hand.Cards[0]) == 1
		g.Hands = []Hand{
			{Cards: []int{hand.Cards[0], first}, Bet: hand.Bet, Done: aces},
			{Cards: []int{hand.Cards[1], second}, Bet: hand.Bet, Done: aces},
		}
		g.Split = true
	}
	return extra, g.advance()
}

// Status 返回第 i 手的状态(Hand* 常量)。
func (g *Game) Status(i int) string {
	hand := g.Hands[i]
	total, _ := Total(hand.Cards)
	switch {
	case !hand.Done && i == g.Current:
		return HandPlaying
	case !hand.Done:
		return HandWaiting
	case total > 21:
		return HandBust
	case g.natural(i):
		return HandBlackjack
	}
	return HandStand
}

// Results 返回结束的一局每一手的结果与派彩，派彩之和就是这一局的派彩。只能在 Over 之后调用：之前庄家还有暗牌。
func (g *Game) Results() []Result {
	dealer, _ := Total(g.Dealer)
	// 庄家两张 21 点一定是黑杰克：发牌时就偷看出来、结束了这一局，之后庄家要牌总会多于两张。
	dealerBlackjack := len(g.Dealer) == 2 && dealer == 21
	results := make([]Result, len(g.Hands))
	for i, hand := range g.Hands {
		total, _ := Total(hand.Cards)
		natural := g.natural(i)
		switch {
		case total > 21:
			results[i] = Result{Outcome: OutcomeLose}
		case natural && dealerBlackjack:
			results[i] = Result{Outcome: OutcomePush, Payout: hand.Bet}
		case natural:
			results[i] = Result{Outcome: OutcomeBlackjack, Payout: hand.Bet * 5 / 2}
		case dealerBlackjack:
			results[i] = Result{Outcome: OutcomeLose}
		case dealer > 21 || total > dealer:
			results[i] = Result{Outcome: OutcomeWin, Payout: hand.Bet * 2}
		case total == dealer:
			results[i] = Result{Outcome: OutcomePush, Payout: hand.Bet}
		default:
			results[i] = Result{Outcome: OutcomeLose}
		}
	}
	return results
}

// natural 表示第 i 手是黑杰克：没有分牌、首两张就是 21 点。
func (g *Game) natural(i int) bool {
	hand := g.Hands[i]
	total, _ := Total(hand.Cards)
	return !g.Split && len(hand.Cards) == 2 && total == 21
}

func (g *Game) draw() (int, error) {
	if len(g.Shoe) == 0 {
		return 0, errShoeEmpty
	}
	card := g.Shoe[0]
	g.Shoe = g.Shoe[1:]
	return card, nil
}

// advance 从当前一手起跳过已经结束的手，到了 21 点或爆牌的手自动停牌；玩家所有手都结束时庄家补牌，这一局结束。
func (g *Game) advance() error {
	for ; g.Current < len(g.Hands); g.Current++ {
		hand := &g.Hands[g.Current]
		if total, _ := Total(hand.Cards); total >= 21 {
			hand.Done = true
		}
		if !hand.Done {
			return nil
		}
	}
	// 玩家所有手都爆了时庄家不用再要牌。
	standing := slices.ContainsFunc(g.Hands, func(hand Hand) bool {
		total, _ := Total(hand.Cards)
		return total <= 21
	})
	for dealer, _ := Total(g.Dealer); standing && dealer < dealerStand; dealer, _ = Total(g.Dealer) {
		card, err := g.draw()
		if err != nil {
			return err
		}
		g.Dealer = append(g.Dealer, card)
	}
	g.finish()
	return nil
}

// finish 结束这一局：之后不再发牌，清空牌靴。
func (g *Game) finish() {
	g.Over = true
	g.Shoe = nil
}
