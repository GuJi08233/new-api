package landlord

import (
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardsOf 用点数表示确定的牌，同一点数依次取不同花色；16、17 是小王、大王。
func cardsOf(ranks ...int) []int {
	cards := make([]int, 0, len(ranks))
	var suits [RankBigJoker + 1]int
	for _, rank := range ranks {
		if rank >= RankSmallJoker {
			cards = append(cards, rank-RankSmallJoker+SmallJoker)
			continue
		}
		cards = append(cards, (rank-3)*4+suits[rank])
		suits[rank]++
	}
	return cards
}

// shuffleFrom 让发牌按 source 确定。
func shuffleFrom(t *testing.T, source *rand.Rand) {
	t.Helper()
	original := Shuffle
	t.Cleanup(func() { Shuffle = original })
	Shuffle = func(deck []int) error {
		source.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
		return nil
	}
}

// handsOf 按点数发三家的牌，同一点数在三家之间依次取不同花色，每家从小到大排好。
func handsOf(seats ...[]int) [Players][]int {
	var hands [Players][]int
	var suits [RankBigJoker + 1]int
	for seat, ranks := range seats {
		hands[seat] = []int{}
		for _, rank := range ranks {
			card := rank - RankSmallJoker + SmallJoker
			if rank < RankSmallJoker {
				card = (rank-3)*4 + suits[rank]
				suits[rank]++
			}
			hands[seat] = append(hands[seat], card)
		}
		sort.Ints(hands[seat])
	}
	return hands
}

// pick 从 seat 手里按点数取牌。
func pick(t *testing.T, game *Game, seat int, ranks ...int) []int {
	t.Helper()
	var cards []int
	for _, rank := range ranks {
		found := false
		for _, card := range game.Hands[seat] {
			if Rank(card) == rank && !slices.Contains(cards, card) {
				cards = append(cards, card)
				found = true
				break
			}
		}
		require.True(t, found, "seat %d has no %d left", seat, rank)
	}
	return cards
}

// ranksOf 是一手牌的点数，从小到大。
func ranksOf(cards []int) []int {
	ranks := make([]int, 0, len(cards))
	for _, card := range cards {
		ranks = append(ranks, Rank(card))
	}
	sort.Ints(ranks)
	return ranks
}

// playingGame 造一局出牌阶段的牌：三家手牌如 hands，其余的牌都已打出，最后打出的一张是 turn 领出的单张、另两家都不出，
// 现在轮到 turn 重新领出。底牌取自已经打出的牌。
func playingGame(t *testing.T, landlord int, turn int, hands [Players][]int) *Game {
	t.Helper()
	game := &Game{
		Phase: PhasePlaying, Hands: hands, Bids: [Players]int{-1, -1, -1}, BidFirst: landlord, BidCount: 1,
		HighBid: MaxBid, HighBidder: landlord, Deals: 1, Landlord: landlord, ToMove: turn, Winner: -1,
	}
	game.Bids[landlord] = MaxBid
	var held [DeckSize]bool
	for _, hand := range hands {
		for _, card := range hand {
			require.False(t, held[card], "fixture repeats card %d", card)
			held[card] = true
		}
	}
	for card, inHand := range held {
		if !inHand {
			game.Played = append(game.Played, card)
		}
	}
	require.GreaterOrEqual(t, len(game.Played), BottomSize+1)
	game.Bottom = slices.Clone(game.Played[:BottomSize])
	last := game.Played[len(game.Played)-1]
	game.Trick = []Action{
		{Seat: turn, Type: Single, Cards: []int{last}},
		{Seat: (turn + 1) % Players, Pass: true},
		{Seat: (turn + 2) % Players, Pass: true},
	}
	require.NoError(t, game.Validate())
	return game
}

func TestClassifyRecognizesAllCombinations(t *testing.T) {
	cases := []struct {
		name  string
		ranks []int
		want  Combination
	}{
		{"single", []int{17}, Combination{Single, 17, 1}},
		{"pair", []int{15, 15}, Combination{Pair, 15, 1}},
		{"triple", []int{14, 14, 14}, Combination{Triple, 14, 1}},
		{"triple with single joker", []int{3, 3, 3, 16}, Combination{TripleSingle, 3, 1}},
		{"triple with pair", []int{3, 3, 3, 15, 15}, Combination{TriplePair, 3, 1}},
		{"straight ends at ace", []int{10, 11, 12, 13, 14}, Combination{Straight, 14, 5}},
		{"consecutive pairs", []int{11, 11, 12, 12, 13, 13}, Combination{PairStraight, 13, 3}},
		{"airplane", []int{3, 3, 3, 4, 4, 4}, Combination{Airplane, 4, 2}},
		{"airplane with single wings", []int{3, 3, 3, 4, 4, 4, 6, 7}, Combination{AirplaneSingle, 4, 2}},
		{"airplane with a pair as single wings", []int{3, 3, 3, 4, 4, 4, 6, 6}, Combination{AirplaneSingle, 4, 2}},
		{"airplane with pair wings", []int{3, 3, 3, 4, 4, 4, 6, 6, 7, 7}, Combination{AirplanePair, 4, 2}},
		{"four with singles", []int{3, 3, 3, 3, 16, 17}, Combination{FourTwo, 3, 1}},
		{"four with one pair", []int{3, 3, 3, 3, 4, 4}, Combination{FourTwo, 3, 1}},
		{"four with two pairs", []int{3, 3, 3, 3, 4, 4, 5, 5}, Combination{FourTwoPairs, 3, 1}},
		{"bomb", []int{15, 15, 15, 15}, Combination{Bomb, 15, 1}},
		{"rocket", []int{16, 17}, Combination{Rocket, 17, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cards := cardsOf(tc.ranks...)
			slices.Reverse(cards) // 牌的顺序不影响牌型
			got, err := Classify(cards)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyRejectsInvalidCombinations(t *testing.T) {
	cases := []struct {
		name  string
		cards []int
	}{
		{"empty", nil},
		{"negative card", []int{-1}},
		{"unknown card", []int{54}},
		{"duplicate card", []int{0, 0}},
		{"mixed pair", cardsOf(3, 4)},
		{"short straight", cardsOf(3, 4, 5, 6)},
		{"straight with two", cardsOf(11, 12, 13, 14, 15)},
		{"straight with jokers", cardsOf(13, 14, 15, 16, 17)},
		{"short consecutive pairs", cardsOf(3, 3, 4, 4)},
		{"consecutive pairs with two", cardsOf(13, 13, 14, 14, 15, 15)},
		{"airplane with two", cardsOf(14, 14, 14, 15, 15, 15)},
		{"nonconsecutive airplane", cardsOf(3, 3, 3, 5, 5, 5)},
		{"airplane wing borrows body rank", cardsOf(3, 3, 3, 3, 4, 4, 4, 4)},
		{"airplane with triple wing", cardsOf(3, 3, 3, 4, 4, 4, 5, 5, 5, 7, 7, 7)},
		{"airplane pair wings with a joker", cardsOf(3, 3, 3, 4, 4, 4, 6, 6, 16, 17)},
		{"four with missing wing", cardsOf(3, 3, 3, 3, 4)},
		{"four with mixed pair wings", cardsOf(3, 3, 3, 3, 4, 4, 5, 6)},
		{"two bombs are not pair wings", cardsOf(3, 3, 3, 3, 4, 4, 4, 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Classify(tc.cards)
			assert.ErrorIs(t, err, ErrIllegalMove)
		})
	}
}

func TestBeatsComparesTypeLengthAndBombPriority(t *testing.T) {
	cases := []struct {
		name           string
		play, previous []int
		want           bool
	}{
		{"higher single", []int{4}, []int{3}, true},
		{"same rank different suit", []int{3}, []int{3}, false},
		{"two beats ace", []int{15, 15}, []int{14, 14}, true},
		{"triple single uses triple rank", []int{4, 4, 4, 3}, []int{3, 3, 3, 17}, true},
		{"different type", []int{4, 4, 4}, []int{3, 3, 3, 5}, false},
		{"longer straight", []int{4, 5, 6, 7, 8, 9}, []int{3, 4, 5, 6, 7}, false},
		{"higher equal straight", []int{4, 5, 6, 7, 8}, []int{3, 4, 5, 6, 7}, true},
		{"airplane compares body", []int{4, 4, 4, 5, 5, 5, 3, 3}, []int{3, 3, 3, 4, 4, 4, 16, 17}, true},
		{"bomb beats single joker", []int{3, 3, 3, 3}, []int{17}, true},
		{"higher bomb", []int{4, 4, 4, 4}, []int{3, 3, 3, 3}, true},
		{"lower bomb", []int{3, 3, 3, 3}, []int{4, 4, 4, 4}, false},
		{"rocket beats biggest bomb", []int{16, 17}, []int{15, 15, 15, 15}, true},
		{"nothing beats rocket", []int{15, 15, 15, 15}, []int{16, 17}, false},
		{"four with wings is not bomb", []int{3, 3, 3, 3, 4, 4}, []int{17}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			play, err := Classify(cardsOf(tc.play...))
			require.NoError(t, err)
			previous, err := Classify(cardsOf(tc.previous...))
			require.NoError(t, err)
			assert.Equal(t, tc.want, play.Beats(previous))
		})
	}
}

func TestDealHidesHandsAndBottomUntilLandlordIsChosen(t *testing.T) {
	game, err := New(2)
	require.NoError(t, err)
	require.NoError(t, game.Validate())
	assert.Equal(t, 2, game.ToMove)
	seen := map[int]bool{}
	for seat := 0; seat < Players; seat++ {
		view := game.View(seat)
		assert.Len(t, view.Hand, HandSize)
		assert.Equal(t, [Players]int{17, 17, 17}, view.Counts)
		assert.Empty(t, view.Bottom)
		assert.Nil(t, view.Hands)
		assert.True(t, slices.IsSorted(view.Hand))
		for _, card := range view.Hand {
			assert.False(t, seen[card])
			seen[card] = true
		}
	}
	for _, card := range game.Bottom {
		assert.False(t, seen[card])
		seen[card] = true
	}
	assert.Len(t, seen, DeckSize)
	assert.Empty(t, game.View(-1).Hand, "spectators see no hand")
	assert.Empty(t, game.View(Players).Hand, "an invalid seat sees no hand")

	bottom := slices.Clone(game.Bottom)
	require.NoError(t, game.Bid(2, 1))
	require.NoError(t, game.Bid(0, 2))
	require.NoError(t, game.Bid(1, 0))
	require.NoError(t, game.Validate())
	view := game.View(0)
	assert.Equal(t, PhasePlaying, view.Phase)
	assert.Equal(t, 0, view.Landlord)
	assert.Equal(t, 0, view.ToMove)
	assert.Equal(t, [Players]int{20, 17, 17}, view.Counts)
	assert.Equal(t, [Players]int{2, 0, 1}, view.Bids)
	assert.Equal(t, bottom, view.Bottom)
	for _, card := range bottom {
		assert.Contains(t, view.Hand, card)
	}
	assert.Nil(t, view.Hands, "only the bottom cards become public")
}

func TestThreeBidEndsAuctionImmediately(t *testing.T) {
	game, err := New(1)
	require.NoError(t, err)
	require.NoError(t, game.Bid(1, 3))
	view := game.View(1)
	assert.Equal(t, PhasePlaying, view.Phase)
	assert.Equal(t, 1, view.Landlord)
	assert.Equal(t, [Players]int{-1, 3, -1}, view.Bids)
	assert.Equal(t, [Players]int{17, 20, 17}, view.Counts)
}

func TestBiddingMustRaiseAndFollowTurns(t *testing.T) {
	game, err := New(0)
	require.NoError(t, err)
	assert.ErrorIs(t, game.Bid(1, 1), ErrIllegalMove, "wrong turn")
	for _, bid := range []int{-1, 4} {
		assert.ErrorIs(t, game.Bid(0, bid), ErrIllegalMove)
	}
	assert.ErrorIs(t, game.Play(0, game.Hands[0][:1]), ErrIllegalMove, "cannot play cards while bidding")
	assert.ErrorIs(t, game.Pass(0), ErrIllegalMove)
	require.NoError(t, game.Bid(0, 2))
	for _, bid := range []int{1, 2} {
		assert.ErrorIs(t, game.Bid(1, bid), ErrIllegalMove, "must raise or pass")
	}
	require.NoError(t, game.Bid(1, 3))
	assert.ErrorIs(t, game.Bid(2, 0), ErrIllegalMove, "auction already ended")
}

// 三家都不叫就换下一位首叫重新发牌；第三次发牌时前两位都不叫，最后一位必须叫分，对局一定能开始。
func TestNoBidRedealsAndTheFinalDealForcesTheLastBidder(t *testing.T) {
	shuffleFrom(t, rand.New(rand.NewPCG(7, 7)))
	game, err := New(2)
	require.NoError(t, err)
	first := slices.Clone(game.Hands[0])
	for _, seat := range []int{2, 0, 1} {
		require.NoError(t, game.Bid(seat, 0))
	}
	require.NoError(t, game.Validate())
	assert.Equal(t, PhaseBidding, game.Phase)
	assert.Equal(t, 2, game.Deals)
	assert.Equal(t, 0, game.ToMove, "the next seat calls first after a redeal")
	assert.Equal(t, [Players]int{-1, -1, -1}, game.Bids)
	assert.NotEqual(t, first, game.Hands[0], "a fresh deck is dealt")

	for _, seat := range []int{0, 1, 2} {
		require.NoError(t, game.Bid(seat, 0))
	}
	assert.Equal(t, MaxDeals, game.Deals)
	assert.Equal(t, 1, game.ToMove)
	require.NoError(t, game.Bid(1, 0))
	assert.False(t, game.MustBid())
	require.NoError(t, game.Bid(2, 0))
	assert.True(t, game.MustBid())
	assert.True(t, game.View(1).MustBid)
	assert.ErrorIs(t, game.Bid(0, 0), ErrIllegalMove, "the last bidder of the final deal cannot pass")
	require.NoError(t, game.Bid(0, 1))
	assert.Equal(t, PhasePlaying, game.Phase)
	assert.Equal(t, 0, game.Landlord)
	require.NoError(t, game.Validate())
}

func TestPlayChecksOwnershipAndKeepsTheTrickVisible(t *testing.T) {
	game := playingGame(t, 0, 0, [Players][]int{{0, 4, 8}, {1, 5, 9}, {2, 6, 10}})
	for _, cards := range [][]int{{1}, {0, 0}, {0, 4}} {
		assert.ErrorIs(t, game.Play(0, cards), ErrIllegalMove)
	}
	assert.ErrorIs(t, game.Pass(0), ErrIllegalMove, "the leader cannot pass")
	require.NoError(t, game.Play(0, []int{4}))
	assert.Equal(t, []Action{{Seat: 0, Type: Single, Cards: []int{4}}}, game.Trick, "a new lead starts a new trick")
	assert.Equal(t, 1, game.ToMove)
	assert.Equal(t, []int{0, 8}, game.Hands[0])
	assert.ErrorIs(t, game.Play(1, []int{1}), ErrIllegalMove, "lower card cannot beat the previous single")
	assert.ErrorIs(t, game.Play(1, []int{5}), ErrIllegalMove, "equal rank cannot beat the previous single")
	assert.ErrorIs(t, game.Play(2, []int{10}), ErrIllegalMove, "cannot skip a turn")
	require.NoError(t, game.Pass(1))
	require.NoError(t, game.Pass(2))
	require.NoError(t, game.Validate())
	assert.Equal(t, 0, game.ToMove)
	assert.Nil(t, game.LastPlay, "two passes let the last player lead anything")
	view := game.View(1)
	assert.Equal(t, []Action{
		{Seat: 0, Type: Single, Cards: []int{4}},
		{Seat: 1, Pass: true},
		{Seat: 2, Pass: true},
	}, view.Trick, "the ended trick stays visible until the next lead")

	require.NoError(t, game.Play(0, []int{0}))
	require.NoError(t, game.Play(1, []int{9}))
	require.NoError(t, game.Pass(2))
	require.NoError(t, game.Pass(0))
	require.NoError(t, game.Validate())
	assert.Equal(t, 1, game.ToMove, "the trick returns to its last player, not to the landlord")
	// 每个座位都看得到这一局打出过的全部牌(记牌器用)，最近打出的三张排在最后。
	played := game.View(2).Played
	assert.Equal(t, game.Played, played)
	assert.Equal(t, []int{4, 0, 9}, played[len(played)-3:])
}

func TestLandlordWinsDoubleAndFarmersWinTogether(t *testing.T) {
	cases := []struct {
		name     string
		landlord int
		finisher int
		want     []int
	}{
		{"landlord wins double", 1, 1, []int{-1, 2, -1}},
		{"first farmer wins for both", 1, 0, []int{1, -2, 1}},
		{"second farmer wins for both", 1, 2, []int{1, -2, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := playingGame(t, tc.landlord, tc.finisher, [Players][]int{{0}, {4}, {8}})
			assert.Nil(t, game.Scores(), "an unfinished game has no scores")
			require.NoError(t, game.Play(tc.finisher, []int{tc.finisher * 4}))
			require.NoError(t, game.Validate())
			assert.True(t, game.Over())
			assert.Equal(t, -1, game.ToMove)
			assert.Equal(t, tc.finisher, game.Winner)
			assert.Equal(t, tc.want, game.Scores())
			for seat := -1; seat < Players; seat++ {
				view := game.View(seat)
				assert.True(t, view.Over)
				require.NotNil(t, view.Hands, "remaining hands become public after a normal finish")
				assert.Empty(t, view.Hands[tc.finisher])
			}
			assert.ErrorIs(t, game.Pass((tc.finisher+1)%Players), ErrIllegalMove, "a finished game accepts no more actions")
		})
	}
}

func TestValidateRejectsDamagedState(t *testing.T) {
	bidding := func(t *testing.T) *Game {
		game, err := New(0)
		require.NoError(t, err)
		return game
	}
	trick := func(t *testing.T) *Game {
		game := playingGame(t, 0, 0, [Players][]int{{0, 4, 8}, {1, 5, 9}, {2, 6, 10}})
		require.NoError(t, game.Play(0, []int{4}))
		require.NoError(t, game.Pass(1))
		return game
	}
	cases := []struct {
		name   string
		build  func(*testing.T) *Game
		mutate func(*Game)
	}{
		{"duplicate card", bidding, func(g *Game) { g.Hands[0][0] = g.Hands[1][0] }},
		{"auction already exhausted", bidding, func(g *Game) { g.BidCount, g.Bids = 3, [Players]int{0, 0, 0} }},
		{"highest bid does not match record", bidding, func(g *Game) { g.HighBid, g.HighBidder = 2, 0 }},
		{"uncalled player already bid", bidding, func(g *Game) { g.Bids[1] = 1 }},
		{"bid decreases", bidding, func(g *Game) {
			g.BidCount, g.Bids, g.HighBid, g.HighBidder, g.ToMove = 2, [Players]int{2, 1, -1}, 2, 0, 2
		}},
		{"three bid did not end auction", bidding, func(g *Game) {
			g.BidCount, g.Bids, g.HighBid, g.HighBidder, g.ToMove = 1, [Players]int{3, -1, -1}, 3, 0, 1
		}},
		{"too many deals", bidding, func(g *Game) { g.Deals = MaxDeals + 1 }},
		{"trick skips a seat", trick, func(g *Game) { g.Trick[1].Seat = 2 }},
		{"trick play does not beat", trick, func(g *Game) {
			g.Trick = append(g.Trick, Action{Seat: 2, Type: Single, Cards: []int{2}})
		}},
		{"trick cards were not the last played", trick, func(g *Game) { g.Trick[0].Cards = []int{0} }},
		{"pass recorded with cards", trick, func(g *Game) { g.Trick[1].Cards = []int{1} }},
		{"wrong seat to move", trick, func(g *Game) { g.ToMove = 0 }},
		{"last play differs from trick", trick, func(g *Game) { g.LastPlay = nil }},
		{"bottom card moved to a farmer", trick, func(g *Game) {
			g.Bottom[0], g.Hands[1][0] = g.Hands[1][0], g.Bottom[0]
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := tc.build(t)
			require.NoError(t, game.Validate())
			tc.mutate(game)
			assert.Error(t, game.Validate())
		})
	}
}

// applyMove 按规则执行 AI 选的一步。
func applyMove(game *Game, seat int, move Move) error {
	switch {
	case move.Bid != nil:
		return game.Bid(seat, *move.Bid)
	case move.Pass:
		return game.Pass(seat)
	default:
		return game.Play(seat, move.Cards)
	}
}

// AI 替任何座位、任何难度给出的每一步都必须是合法的一步，三家都由 AI 出牌时每局都能打完：人机对战与托管都依赖这一点。
func TestAiOnlyMakesLegalMovesAndFinishesGames(t *testing.T) {
	deals := rand.New(rand.NewPCG(1, 1))
	shuffleFrom(t, deals)
	for round := 0; round < 24; round++ {
		game, err := New(round % Players)
		require.NoError(t, err)
		rng := rand.New(rand.NewPCG(uint64(round), 99))
		for steps := 0; !game.Over(); steps++ {
			require.Less(t, steps, 400, "round %d did not finish", round)
			seat := game.ToMove
			level := 1 + (seat+round)%3
			move, err := BestMove(game.Knowledge(seat), level, rng)
			require.NoError(t, err)
			require.NoError(t, applyMove(game, seat, move), "round %d seat %d level %d move %+v", round, seat, level, move)
			require.NoError(t, game.Validate())
		}
	}
}

// AI 只看 Knowledge：别人的手牌怎么分、底牌是什么，都不会出现在里面。
func TestKnowledgeRevealsNothingAboutHiddenCards(t *testing.T) {
	mine := []int{3, 5, 5, 9, 12, 14}
	playing := func(t *testing.T, left, right []int) Knowledge {
		return playingGame(t, 0, 0, handsOf(mine, left, right)).Knowledge(0)
	}
	assert.Equal(t,
		playing(t, []int{4, 6, 7, 8}, []int{10, 11, 13, 15}),
		playing(t, []int{4, 10, 13, 15}, []int{6, 7, 8, 11}))

	bidding := func(t *testing.T, rotate int) Knowledge {
		rest := make([]int, 0, DeckSize-HandSize)
		for card := HandSize; card < DeckSize; card++ {
			rest = append(rest, card)
		}
		rest = append(rest[rotate:], rest[:rotate]...)
		game := &Game{
			Phase: PhaseBidding, Bottom: slices.Clone(rest[2*HandSize:]), Bids: [Players]int{-1, -1, -1},
			HighBidder: -1, Deals: 1, Landlord: -1, Trick: []Action{}, Played: []int{}, Winner: -1,
		}
		for card := 0; card < HandSize; card++ {
			game.Hands[0] = append(game.Hands[0], card)
		}
		game.Hands[1] = slices.Sorted(slices.Values(rest[:HandSize]))
		game.Hands[2] = slices.Sorted(slices.Values(rest[HandSize : 2*HandSize]))
		require.NoError(t, game.Validate())
		return game.Knowledge(0)
	}
	assert.Equal(t, bidding(t, 0), bidding(t, 11))
}

func TestAiBiddingFollowsHandStrength(t *testing.T) {
	strong := Knowledge{Phase: PhaseBidding, Hand: cardsOf(3, 4, 5, 6, 7, 9, 9, 10, 11, 12, 13, 14, 15, 15, 15, 16, 17), Bids: [Players]int{-1, -1, -1}}
	weak := Knowledge{Phase: PhaseBidding, Hand: cardsOf(3, 3, 4, 5, 6, 6, 7, 8, 8, 9, 10, 10, 11, 12, 12, 13, 14), Bids: [Players]int{-1, -1, -1}}
	for level := 2; level <= 3; level++ {
		move, err := BestMove(strong, level, rand.New(rand.NewPCG(1, 1)))
		require.NoError(t, err)
		require.NotNil(t, move.Bid)
		assert.Equal(t, 3, *move.Bid, "a rocket with three twos bids 3")

		move, err = BestMove(weak, level, rand.New(rand.NewPCG(1, 1)))
		require.NoError(t, err)
		require.NotNil(t, move.Bid)
		assert.Equal(t, 0, *move.Bid, "no high cards passes")

		forced := weak
		forced.MustBid = true
		move, err = BestMove(forced, level, rand.New(rand.NewPCG(1, 1)))
		require.NoError(t, err)
		require.NotNil(t, move.Bid)
		assert.Equal(t, 1, *move.Bid, "a forced bidder bids the minimum")
	}
}

func TestAiPlayingDecisions(t *testing.T) {
	cases := []struct {
		name  string
		level int
		build func(*testing.T) *Game
		seat  int
		// want 是出的牌的点数，nil 表示不出。
		want []int
	}{
		{
			// 地主 0，农民 1、2。农民 1 出了 5，轮到农民 2，地主还有很多牌：不压队友。
			name: "farmer lets the partner's play stand", level: 2, seat: 2,
			build: func(t *testing.T) *Game {
				game := playingGame(t, 0, 1, handsOf([]int{3, 4, 6, 8, 9, 10, 11}, []int{5, 7}, []int{6, 12, 13}))
				require.NoError(t, game.Play(1, pick(t, game, 1, 5)))
				return game
			},
		},
		{
			// 同样的出牌，但地主只剩一张：农民 2 是地主的上家，用 A 以上的大牌顶住，免得地主出完。
			name: "upstream farmer blocks when the landlord has one card", level: 2, seat: 2,
			build: func(t *testing.T) *Game {
				game := playingGame(t, 0, 1, handsOf([]int{13}, []int{5, 7}, []int{6, 9, 14}))
				require.NoError(t, game.Play(1, pick(t, game, 1, 5)))
				return game
			},
			want: []int{14},
		},
		{
			// 地主 2，农民 0 领出，下家是只剩一张的队友：送最小的单张。
			name: "farmer feeds the partner's last card", level: 2, seat: 0,
			build: func(t *testing.T) *Game {
				return playingGame(t, 2, 0, handsOf([]int{4, 4, 9, 9, 12}, []int{8}, []int{5, 6, 7, 10, 11}))
			},
			want: []int{4},
		},
		{
			name: "plays out the whole hand when it can", level: 2, seat: 1,
			build: func(t *testing.T) *Game {
				game := playingGame(t, 0, 0, handsOf([]int{3, 3, 13}, []int{9, 9}, []int{4, 5, 6}))
				require.NoError(t, game.Play(0, pick(t, game, 0, 3, 3)))
				return game
			},
			want: []int{9, 9},
		},
		{
			// 农民 1 只剩一张、压不住就要出完了：地主没有别的牌压得住，用炸弹。
			name: "bombs when an opponent is about to go out", level: 2, seat: 0,
			build: func(t *testing.T) *Game {
				game := playingGame(t, 0, 1, handsOf([]int{4, 4, 4, 4, 6, 9}, []int{14, 14, 7}, []int{5, 8, 10, 11}))
				require.NoError(t, game.Play(1, pick(t, game, 1, 14, 14)))
				require.NoError(t, game.Pass(2))
				return game
			},
			want: []int{4, 4, 4, 4},
		},
		{
			// 对手牌还多、炸了自己也出不完时，不为压一对小牌用掉炸弹。
			name: "keeps the bomb early in the game", level: 2, seat: 0,
			build: func(t *testing.T) *Game {
				game := playingGame(t, 0, 1, handsOf([]int{4, 4, 4, 4, 6, 9, 11, 13}, []int{5, 5, 7, 8, 9, 10, 11, 12}, []int{3, 8, 10, 11, 12, 13}))
				require.NoError(t, game.Play(1, pick(t, game, 1, 5, 5)))
				require.NoError(t, game.Pass(2))
				return game
			},
		},
		{
			// 困难难度记牌：两张王都已打出，对手也凑不出炸弹，单张 2 没人压得住，残局先出它、再出最后一张。
			name: "hard level leads its unbeatable card first", level: 3, seat: 0,
			build: func(t *testing.T) *Game {
				return playingGame(t, 0, 0, handsOf([]int{3, 15}, []int{5, 6, 7}, []int{8, 9, 10}))
			},
			want: []int{15},
		},
		{
			// 对手只剩一张：不领单张，领对子。
			name: "does not lead a single into an opponent's last card", level: 2, seat: 0,
			build: func(t *testing.T) *Game {
				return playingGame(t, 0, 0, handsOf([]int{4, 4, 5, 9, 9}, []int{13}, []int{6, 7, 8}))
			},
			want: []int{4, 4},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := tc.build(t)
			require.Equal(t, tc.seat, game.ToMove)
			move, err := BestMove(game.Knowledge(tc.seat), tc.level, rand.New(rand.NewPCG(3, 4)))
			require.NoError(t, err)
			if tc.want == nil {
				assert.True(t, move.Pass, "expected a pass, got %v", ranksOf(move.Cards))
			} else {
				assert.False(t, move.Pass)
				assert.Equal(t, tc.want, ranksOf(move.Cards))
			}
			require.NoError(t, applyMove(game, tc.seat, move), "the chosen move must be legal")
		})
	}
}

func TestBestMoveRejectsUnknownLevels(t *testing.T) {
	game, err := New(0)
	require.NoError(t, err)
	for _, level := range []int{0, 4} {
		_, err := BestMove(game.Knowledge(0), level, rand.New(rand.NewPCG(1, 1)))
		assert.ErrorIs(t, err, ErrInvalidLevel)
	}
}
