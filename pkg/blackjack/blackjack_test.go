package blackjack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试里的点数简写。
const (
	A = 1
	J = 11
	Q = 12
	K = 13
)

// cards 把点数写成牌(花色都是黑桃)。作为牌靴时发牌顺序是玩家、庄家明牌、玩家、庄家暗牌，之后按需要依次发出。
func cards(ranks ...int) []int {
	codes := make([]int, len(ranks))
	for i, rank := range ranks {
		codes[i] = rank - 1
	}
	return codes
}

func deal(t *testing.T, bet int, ranks ...int) *Game {
	t.Helper()
	game, err := Deal(cards(ranks...), bet)
	require.NoError(t, err)
	return game
}

func act(t *testing.T, game *Game, action string) int {
	t.Helper()
	extra, err := game.Act(action)
	require.NoError(t, err, action)
	return extra
}

// A 先按 1 算，加 10 不爆时其中一张按 11 算，这时是软点数；J、Q、K 都是 10 点。
func TestTotal(t *testing.T) {
	cases := []struct {
		ranks []int
		total int
		soft  bool
	}{
		{ranks: []int{A, 6}, total: 17, soft: true},
		{ranks: []int{A, 6, 10}, total: 17},
		{ranks: []int{A, A}, total: 12, soft: true},
		{ranks: []int{A, A, 9}, total: 21, soft: true},
		{ranks: []int{A, 5, A}, total: 17, soft: true},
		{ranks: []int{K, Q}, total: 20},
		{ranks: []int{J, A}, total: 21, soft: true},
		{ranks: []int{10, 6, 5}, total: 21},
		{ranks: []int{9, 7, 8}, total: 24},
		{ranks: []int{A, A, A, A, A, A, A, A, A, A, A}, total: 21, soft: true},
	}
	for _, c := range cases {
		total, soft := Total(cards(c.ranks...))
		assert.Equal(t, c.total, total, "%v", c.ranks)
		assert.Equal(t, c.soft, soft, "%v", c.ranks)
	}
}

// 洗好的牌靴是完整的 6 副牌：52 种牌各 6 张。
func TestShuffleIsSixFullDecks(t *testing.T) {
	shoe, err := Shuffle()
	require.NoError(t, err)
	require.Len(t, shoe, ShoeSize)
	counts := make([]int, 52)
	for _, card := range shoe {
		require.GreaterOrEqual(t, card, 0)
		require.Less(t, card, 52)
		counts[card]++
	}
	for card, count := range counts {
		assert.Equal(t, Decks, count, "card %d", card)
	}
}

// 有黑杰克时发完牌就结束：明牌是 A 或 10 点时庄家先偷看，庄家黑杰克玩家直接输(都是黑杰克则平)；玩家黑杰克赔 2.5 倍。
// 偷看之后庄家不是黑杰克、或者明牌根本不可能是黑杰克时照常继续。
func TestDealSettlesNaturals(t *testing.T) {
	cases := []struct {
		name        string
		ranks       []int // 玩家、明牌、玩家、暗牌
		wantOver    bool
		wantPeeked  bool
		wantOutcome string
		wantPayout  int
	}{
		{name: "明牌 A 偷看出黑杰克", ranks: []int{10, A, 9, K}, wantOver: true, wantPeeked: true, wantOutcome: OutcomeLose},
		{name: "明牌 Q 偷看出黑杰克", ranks: []int{10, Q, 9, A}, wantOver: true, wantPeeked: true, wantOutcome: OutcomeLose},
		{name: "双方都是黑杰克", ranks: []int{A, K, Q, A}, wantOver: true, wantPeeked: true, wantOutcome: OutcomePush, wantPayout: 10},
		{name: "玩家黑杰克", ranks: []int{A, 5, K, 6}, wantOver: true, wantOutcome: OutcomeBlackjack, wantPayout: 25},
		{name: "偷看后庄家不是黑杰克、玩家黑杰克", ranks: []int{A, 10, J, 9}, wantOver: true, wantPeeked: true, wantOutcome: OutcomeBlackjack, wantPayout: 25},
		{name: "偷看后庄家不是黑杰克，继续", ranks: []int{10, A, 6, 9}, wantPeeked: true},
		{name: "明牌 9 不用偷看", ranks: []int{10, 9, 6, A}},
	}
	for _, c := range cases {
		game := deal(t, 10, c.ranks...)
		assert.Equal(t, c.wantOver, game.Over, c.name)
		assert.Equal(t, c.wantPeeked, game.Peeked, c.name)
		if !c.wantOver {
			assert.Equal(t, []string{ActionHit, ActionStand, ActionDouble}, game.Actions(), c.name)
			continue
		}
		assert.Empty(t, game.Actions(), c.name)
		assert.Empty(t, game.Shoe, c.name)
		results := game.Results()
		require.Len(t, results, 1, c.name)
		assert.Equal(t, Result{Outcome: c.wantOutcome, Payout: c.wantPayout}, results[0], c.name)
	}
	// 下注按记账单位计，可以是奇数：黑杰克 2.5 倍派彩的零头舍去。
	game, err := Deal(cards(1, 9, 13, 7), 3)
	require.NoError(t, err)
	require.True(t, game.Over)
	assert.Equal(t, []Result{{Outcome: OutcomeBlackjack, Payout: 7}}, game.Results())
	_, err = Deal(cards(10, 9, 6, 7), 0)
	assert.Error(t, err, "下注必须为正")
}

// 庄家不到 17 点要牌，17 点停牌，软 17 也停；玩家停牌后按点数比大小，庄家爆牌玩家赢。
func TestDealerPlay(t *testing.T) {
	cases := []struct {
		name        string
		ranks       []int
		wantDealer  []int
		wantOutcome string
		wantPayout  int
	}{
		{name: "软 17 停牌", ranks: []int{10, A, 9, 6}, wantDealer: []int{A, 6}, wantOutcome: OutcomeWin, wantPayout: 20},
		{name: "软 16 要牌", ranks: []int{10, A, 9, 5, 5}, wantDealer: []int{A, 5, 5}, wantOutcome: OutcomeLose},
		{name: "硬 16 要牌", ranks: []int{10, 10, 9, 6, 2}, wantDealer: []int{10, 6, 2}, wantOutcome: OutcomeWin, wantPayout: 20},
		{name: "庄家爆牌", ranks: []int{10, 10, 8, 6, 9}, wantDealer: []int{10, 6, 9}, wantOutcome: OutcomeWin, wantPayout: 20},
		{name: "点数相同是平局", ranks: []int{10, 10, 9, 9}, wantDealer: []int{10, 9}, wantOutcome: OutcomePush, wantPayout: 10},
	}
	for _, c := range cases {
		game := deal(t, 10, c.ranks...)
		require.False(t, game.Over, c.name)
		assert.Zero(t, act(t, game, ActionStand), c.name)
		require.True(t, game.Over, c.name)
		assert.Equal(t, cards(c.wantDealer...), game.Dealer, c.name)
		assert.Equal(t, HandStand, game.Status(0), c.name)
		assert.Equal(t, []Result{{Outcome: c.wantOutcome, Payout: c.wantPayout}}, game.Results(), c.name)
	}
}

// 要牌爆了就输，庄家不再要牌；要到 21 点自动停牌。
func TestHit(t *testing.T) {
	bust := deal(t, 10, 10, 10, 6, 5, K)
	act(t, bust, ActionHit)
	assert.True(t, bust.Over)
	assert.Equal(t, HandBust, bust.Status(0))
	assert.Equal(t, cards(10, 5), bust.Dealer, "玩家爆了，庄家不再要牌")
	assert.Equal(t, []Result{{Outcome: OutcomeLose}}, bust.Results())

	twentyOne := deal(t, 10, 5, 10, 6, 7, 10)
	act(t, twentyOne, ActionHit)
	assert.True(t, twentyOne.Over, "21 点自动停牌")
	assert.Equal(t, []Result{{Outcome: OutcomeWin, Payout: 20}}, twentyOne.Results())

	three := deal(t, 10, 2, 10, 3, 7, 4)
	act(t, three, ActionHit)
	assert.False(t, three.Over)
	assert.Equal(t, []string{ActionHit, ActionStand}, three.Actions(), "三张牌不能加倍")
	_, err := three.Act(ActionDouble)
	assert.ErrorIs(t, err, ErrIllegalAction)
}

// 加倍追加与这一手相同的下注，只再发一张就停牌。
func TestDouble(t *testing.T) {
	game := deal(t, 10, 5, 10, 6, 7, 10)
	assert.Equal(t, 10, act(t, game, ActionDouble))
	require.True(t, game.Over)
	assert.True(t, game.Hands[0].Doubled)
	assert.Equal(t, 20, game.Hands[0].Bet)
	assert.Len(t, game.Hands[0].Cards, 3)
	assert.Equal(t, []Result{{Outcome: OutcomeWin, Payout: 40}}, game.Results())
}

// 分开的 A 各只补一张就停牌，庄家接着补牌；分牌后的 21 点不算黑杰克，只赔 2 倍。
func TestSplitAces(t *testing.T) {
	game := deal(t, 10, A, 7, A, 10, K, 5)
	assert.Equal(t, []string{ActionHit, ActionStand, ActionDouble, ActionSplit}, game.Actions())
	assert.Equal(t, 10, act(t, game, ActionSplit))
	require.True(t, game.Over)
	assert.Equal(t, cards(A, K), game.Hands[0].Cards)
	assert.Equal(t, cards(A, 5), game.Hands[1].Cards)
	assert.Equal(t, HandStand, game.Status(0), "分牌后的 21 点不是黑杰克")
	assert.Equal(t, []Result{{Outcome: OutcomeWin, Payout: 20}, {Outcome: OutcomeLose}}, game.Results())
}

// 10、J、Q、K 都算 10 点，可以互相分牌；点数不同不能分。分牌后两手依次操作，都可以加倍，不能再分。
func TestSplitHandsPlayInOrder(t *testing.T) {
	pair := deal(t, 10, K, 9, 10, 7)
	assert.Contains(t, pair.Actions(), ActionSplit)
	noPair := deal(t, 10, K, 9, 9, 7)
	_, err := noPair.Act(ActionSplit)
	assert.ErrorIs(t, err, ErrIllegalAction)

	// 8、8 对庄家 6、10：分成 8、3 与 8、10；第一手加倍补到 9 共 20 点，第二手 18 点停牌；庄家 16 点补 7 爆牌。
	game := deal(t, 10, 8, 6, 8, 10, 3, 10, 9, 7)
	assert.Equal(t, 10, act(t, game, ActionSplit))
	assert.Equal(t, HandPlaying, game.Status(0))
	assert.Equal(t, HandWaiting, game.Status(1))
	assert.Equal(t, []string{ActionHit, ActionStand, ActionDouble}, game.Actions(), "只能分一次")
	assert.Equal(t, 10, act(t, game, ActionDouble))
	assert.False(t, game.Over)
	assert.Equal(t, 1, game.Current)
	assert.Equal(t, HandStand, game.Status(0))
	assert.Equal(t, HandPlaying, game.Status(1))
	assert.Zero(t, act(t, game, ActionStand))
	require.True(t, game.Over)
	assert.Equal(t, cards(6, 10, 7), game.Dealer)
	assert.Equal(t, []Result{{Outcome: OutcomeWin, Payout: 40}, {Outcome: OutcomeWin, Payout: 20}}, game.Results())

	_, err = game.Act(ActionHit)
	assert.ErrorIs(t, err, ErrIllegalAction, "结束之后不能再操作")
	_, err = deal(t, 10, 10, 9, 6, 7).Act("surrender")
	assert.ErrorIs(t, err, ErrIllegalAction, "没有投降")
}
