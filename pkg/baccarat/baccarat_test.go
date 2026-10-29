package baccarat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cards 依次发出给定的牌。
func cards(list ...Card) func() Card {
	return func() Card {
		card := list[0]
		list = list[1:]
		return card
	}
}

func c(rank int) Card { return Card{Rank: rank} }

// 补牌规则：天生 8、9 不补；闲家 0–5 补牌；闲家停牌时庄家 0–5 补；闲家补牌后庄家按第三张牌查表。
func TestDealFollowsTheDrawingRules(t *testing.T) {
	cases := map[string]struct {
		deal                   []Card
		player, banker, winner string
		playerCards            int
		bankerCards            int
	}{
		"natural stops both":           {deal: []Card{c(4), c(3), c(5), c(3)}, playerCards: 2, bankerCards: 2, winner: Player},
		"player stands, banker draws":  {deal: []Card{c(3), c(2), c(4), c(2), c(5)}, playerCards: 2, bankerCards: 3, winner: Banker},
		"banker 3 stands on third 8":   {deal: []Card{c(1), c(1), c(2), c(2), c(8)}, playerCards: 3, bankerCards: 2, winner: Banker},
		"banker 6 draws on third 6":    {deal: []Card{c(2), c(3), c(1), c(3), c(6), c(3)}, playerCards: 3, bankerCards: 3, winner: Tie},
		"banker 6 stands on third 5":   {deal: []Card{c(2), c(3), c(1), c(3), c(5)}, playerCards: 3, bankerCards: 2, winner: Player},
		"both six stand, tie":          {deal: []Card{c(3), c(3), c(3), c(3)}, playerCards: 2, bankerCards: 2, winner: Tie},
		"face cards count as zero, 9":  {deal: []Card{c(13), c(10), c(9), c(12)}, playerCards: 2, bankerCards: 2, winner: Player},
		"banker 7 stands after player": {deal: []Card{c(1), c(3), c(1), c(4), c(6)}, playerCards: 3, bankerCards: 2, winner: Player},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := Deal(cards(tc.deal...))
			assert.Len(t, result.Player.Cards, tc.playerCards)
			assert.Len(t, result.Banker.Cards, tc.bankerCards)
			assert.Equal(t, tc.winner, result.Winner)
		})
	}
}

// 派彩含本金：闲 2 倍、庄 1.95 倍(零头舍去)、和 9 倍，和局时闲、庄退回本金，对子 12 倍。
func TestWinPaysTheTable(t *testing.T) {
	player := Result{Winner: Player, PlayerPair: true}
	assert.Equal(t, 200, player.Win(Player, 100))
	assert.Equal(t, 0, player.Win(Banker, 100))
	assert.Equal(t, 0, player.Win(Tie, 100))
	assert.Equal(t, 1200, player.Win(PlayerPair, 100))
	assert.Equal(t, 0, player.Win(BankerPair, 100))
	banker := Result{Winner: Banker}
	assert.Equal(t, 195, banker.Win(Banker, 100))
	assert.Equal(t, 1, banker.Win(Banker, 1), "1.95 个最小单位舍去零头")
	tie := Result{Winner: Tie}
	assert.Equal(t, 100, tie.Win(Player, 100))
	assert.Equal(t, 100, tie.Win(Banker, 100))
	assert.Equal(t, 900, tie.Win(Tie, 100))
	assert.False(t, ValidSpot("dragon"))
}

// 按 8 副牌、不放回地精确计算闲、庄、和的概率与对子的概率，公示的理论返还率与之一致，并且都低于 100%。
func TestPaybackMatchesExactOdds(t *testing.T) {
	// 点数 0 的牌(10、J、Q、K)每副 16 张，1–9 各 4 张。
	var counts [10]int
	counts[0] = 16 * Decks
	for v := 1; v <= 9; v++ {
		counts[v] = 4 * Decks
	}
	var probability = map[string]float64{}
	sequence := make([]int, 6)
	var walk func(depth int, remaining int, weight float64)
	walk = func(depth int, remaining int, weight float64) {
		if depth == len(sequence) {
			i := 0
			result := Deal(func() Card {
				v := sequence[i]
				i++
				if v == 0 {
					return Card{Rank: 10}
				}
				return Card{Rank: v}
			})
			probability[result.Winner] += weight
			return
		}
		for v := 0; v <= 9; v++ {
			if counts[v] == 0 {
				continue
			}
			p := float64(counts[v]) / float64(remaining)
			counts[v]--
			sequence[depth] = v
			walk(depth+1, remaining-1, weight*p)
			counts[v]++
		}
	}
	walk(0, ShoeCards, 1)
	require.InDelta(t, 1, probability[Player]+probability[Banker]+probability[Tie], 1e-9)

	pTie := probability[Tie]
	expected := map[string]float64{
		Player: probability[Player]*2 + pTie,
		Banker: probability[Banker]*1.95 + pTie,
		Tie:    pTie * 9,
		// 头两张同点：第一张之后，同点的还剩 4×8−1 张，在剩下的 ShoeCards−1 张里。
		PlayerPair: 12 * float64(4*Decks-1) / float64(ShoeCards-1),
		BankerPair: 12 * float64(4*Decks-1) / float64(ShoeCards-1),
	}
	for _, spot := range Spots {
		assert.InDelta(t, expected[spot], Payback[spot], 1e-6, spot)
		assert.Less(t, Payback[spot], 1.0, spot)
	}
}

// 随机发出的牌都在一靴牌之内，结果自洽。
func TestPlayDealsFromTheShoe(t *testing.T) {
	for i := 0; i < 50; i++ {
		result, err := Play()
		require.NoError(t, err)
		for _, hand := range []Hand{result.Player, result.Banker} {
			require.GreaterOrEqual(t, len(hand.Cards), 2)
			require.LessOrEqual(t, len(hand.Cards), 3)
			for _, card := range hand.Cards {
				require.True(t, card.Rank >= 1 && card.Rank <= 13 && card.Suit >= 0 && card.Suit <= 3)
			}
			assert.Equal(t, total(hand.Cards), hand.Total)
		}
	}
}
