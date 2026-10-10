package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// landlordTestCards 用点数表达确定的牌，每个点数依次取不同花色；16/17 对应小王/大王。
func landlordTestCards(ranks ...int) []int {
	cards := make([]int, 0, len(ranks))
	var suits [18]int
	for _, rank := range ranks {
		if rank >= 16 {
			cards = append(cards, rank+36)
			continue
		}
		cards = append(cards, (rank-3)*4+suits[rank])
		suits[rank]++
	}
	return cards
}

func landlordTestPlaying(t *testing.T, landlord, turn int, hands [3][]int) pvpState {
	t.Helper()
	state := &landlordState{
		Phase: "playing", Hands: hands, Bottom: []int{49, 50, 51}, Bids: [3]int{-1, -1, -1},
		BidFirst: landlord, BidCount: 1, HighBid: 3, HighBidder: landlord,
		Deals: 1, Landlord: landlord, ToMove: turn, Winner: -1,
	}
	state.Bids[landlord] = 3
	var inHand [54]bool
	for _, hand := range hands {
		for _, card := range hand {
			require.False(t, inHand[card], "fixture duplicated card %d", card)
			inHand[card] = true
		}
	}
	for card, exists := range inHand {
		if !exists {
			state.Played = append(state.Played, card)
		}
	}
	pvp, err := landlordPvpState(state)
	require.NoError(t, err)
	_, err = loadLandlord(pvp.Data)
	require.NoError(t, err)
	return pvp
}

func landlordTestMove(t *testing.T, state pvpState, seat int, move any) pvpState {
	t.Helper()
	data, err := common.Marshal(move)
	require.NoError(t, err)
	next, err := (landlordEngine{}).Play(state.Data, seat, data)
	require.NoError(t, err, "seat %d move %s", seat, data)
	return next
}

func landlordTestView(t *testing.T, state pvpState, seat int) landlordView {
	t.Helper()
	view, err := (landlordEngine{}).View(state.Data, seat)
	require.NoError(t, err)
	return view.(landlordView)
}

func TestLandlordRecognizesAllPlayableCombinations(t *testing.T) {
	cases := []struct {
		name  string
		ranks []int
		want  landlordCombination
	}{
		{"single", []int{17}, landlordCombination{Type: "single", Rank: 17, Chain: 1}},
		{"pair", []int{15, 15}, landlordCombination{Type: "pair", Rank: 15, Chain: 1}},
		{"triple", []int{14, 14, 14}, landlordCombination{Type: "triple", Rank: 14, Chain: 1}},
		{"triple with single joker", []int{3, 3, 3, 16}, landlordCombination{Type: "triple_single", Rank: 3, Chain: 1}},
		{"triple with pair", []int{3, 3, 3, 15, 15}, landlordCombination{Type: "triple_pair", Rank: 3, Chain: 1}},
		{"straight ends at ace", []int{10, 11, 12, 13, 14}, landlordCombination{Type: "straight", Rank: 14, Chain: 5}},
		{"consecutive pairs", []int{11, 11, 12, 12, 13, 13}, landlordCombination{Type: "pair_straight", Rank: 13, Chain: 3}},
		{"airplane", []int{3, 3, 3, 4, 4, 4}, landlordCombination{Type: "airplane", Rank: 4, Chain: 2}},
		{"airplane with single wings", []int{3, 3, 3, 4, 4, 4, 6, 7}, landlordCombination{Type: "airplane_single", Rank: 4, Chain: 2}},
		{"airplane with a pair as single wings", []int{3, 3, 3, 4, 4, 4, 6, 6}, landlordCombination{Type: "airplane_single", Rank: 4, Chain: 2}},
		{"airplane with pair wings", []int{3, 3, 3, 4, 4, 4, 6, 6, 7, 7}, landlordCombination{Type: "airplane_pair", Rank: 4, Chain: 2}},
		{"four with singles", []int{3, 3, 3, 3, 16, 17}, landlordCombination{Type: "four_two", Rank: 3, Chain: 1}},
		{"four with one pair", []int{3, 3, 3, 3, 4, 4}, landlordCombination{Type: "four_two", Rank: 3, Chain: 1}},
		{"four with two pairs", []int{3, 3, 3, 3, 4, 4, 5, 5}, landlordCombination{Type: "four_two_pairs", Rank: 3, Chain: 1}},
		{"bomb", []int{15, 15, 15, 15}, landlordCombination{Type: "bomb", Rank: 15, Chain: 1}},
		{"rocket", []int{16, 17}, landlordCombination{Type: "rocket", Rank: 17, Chain: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cards := landlordTestCards(tc.ranks...)
			// 请求的牌号顺序不影响牌型。
			for left, right := 0, len(cards)-1; left < right; left, right = left+1, right-1 {
				cards[left], cards[right] = cards[right], cards[left]
			}
			got, err := landlordClassify(cards)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLandlordRejectsInvalidCombinations(t *testing.T) {
	cases := []struct {
		name  string
		cards []int
	}{
		{"empty", nil},
		{"negative card", []int{-1}},
		{"unknown card", []int{54}},
		{"duplicate card", []int{0, 0}},
		{"mixed pair", landlordTestCards(3, 4)},
		{"short straight", landlordTestCards(3, 4, 5, 6)},
		{"straight with two", landlordTestCards(11, 12, 13, 14, 15)},
		{"straight with jokers", landlordTestCards(13, 14, 15, 16, 17)},
		{"short consecutive pairs", landlordTestCards(3, 3, 4, 4)},
		{"consecutive pairs with two", landlordTestCards(13, 13, 14, 14, 15, 15)},
		{"airplane with two", landlordTestCards(14, 14, 14, 15, 15, 15)},
		{"nonconsecutive airplane", landlordTestCards(3, 3, 3, 5, 5, 5)},
		{"airplane wing borrows body rank", landlordTestCards(3, 3, 3, 3, 4, 4, 4, 4)},
		{"airplane with triple wing", landlordTestCards(3, 3, 3, 4, 4, 4, 5, 5, 5, 7, 7, 7)},
		{"four with missing wing", landlordTestCards(3, 3, 3, 3, 4)},
		{"four with mixed pair wings", landlordTestCards(3, 3, 3, 3, 4, 4, 5, 6)},
		{"two bombs are not pair wings", landlordTestCards(3, 3, 3, 3, 4, 4, 4, 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := landlordClassify(tc.cards)
			assert.ErrorIs(t, err, errPvpIllegalMove)
		})
	}
}

func TestLandlordComparesTypeLengthAndBombPriority(t *testing.T) {
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
			play, err := landlordClassify(landlordTestCards(tc.play...))
			require.NoError(t, err)
			previous, err := landlordClassify(landlordTestCards(tc.previous...))
			require.NoError(t, err)
			assert.Equal(t, tc.want, play.beats(previous))
		})
	}
}

func TestLandlordDealsAndHidesHandsUntilLandlordIsChosen(t *testing.T) {
	engine := landlordEngine{}
	state, err := engine.New(2)
	require.NoError(t, err)
	assert.Equal(t, 2, state.ToMove)
	assert.False(t, state.Over)
	record, err := loadLandlord(state.Data)
	require.NoError(t, err)
	seen := make(map[int]bool)
	for seat := 0; seat < 3; seat++ {
		view := landlordTestView(t, state, seat)
		assert.Len(t, view.Hand, 17)
		assert.Equal(t, [3]int{17, 17, 17}, view.Counts)
		assert.Empty(t, view.Bottom)
		assert.Nil(t, view.Hands)
		assert.Equal(t, record.Hands[seat], view.Hand)
		assert.IsNonDecreasing(t, view.Hand)
		for _, card := range view.Hand {
			assert.False(t, seen[card])
			seen[card] = true
		}
		encoded, err := common.Marshal(view)
		require.NoError(t, err)
		var response map[string]any
		require.NoError(t, common.Unmarshal(encoded, &response))
		assert.NotContains(t, response, "hands", "other hands must not appear anywhere in the response")
		assert.NotContains(t, response, "played")
	}
	for _, card := range record.Bottom {
		assert.False(t, seen[card])
		seen[card] = true
	}
	assert.Len(t, seen, 54)
	assert.Empty(t, landlordTestView(t, state, -1).Hand, "spectators see no hand")
	assert.Empty(t, landlordTestView(t, state, 3).Hand, "invalid seat cannot see a hand")

	state = landlordTestMove(t, state, 2, map[string]any{"bid": 1})
	state = landlordTestMove(t, state, 0, map[string]any{"bid": 2})
	state = landlordTestMove(t, state, 1, map[string]any{"bid": 0})
	view := landlordTestView(t, state, 0)
	assert.Equal(t, "playing", view.Phase)
	assert.Equal(t, 0, view.Landlord)
	assert.Equal(t, 0, state.ToMove)
	assert.Equal(t, [3]int{20, 17, 17}, view.Counts)
	assert.Equal(t, [3]int{2, 0, 1}, view.Bids)
	assert.Equal(t, record.Bottom, view.Bottom)
	for _, card := range record.Bottom {
		assert.Contains(t, view.Hand, card)
	}
	assert.Nil(t, view.Hands, "only bottom cards are newly public")
}

func TestLandlordThreeBidEndsAuctionImmediately(t *testing.T) {
	state, err := (landlordEngine{}).New(1)
	require.NoError(t, err)
	state = landlordTestMove(t, state, 1, map[string]any{"bid": 3})
	view := landlordTestView(t, state, 1)
	assert.Equal(t, "playing", view.Phase)
	assert.Equal(t, 1, view.Landlord)
	assert.Equal(t, [3]int{-1, 3, -1}, view.Bids)
	assert.Equal(t, [3]int{17, 20, 17}, view.Counts)
}

func TestLandlordNoBidRedealsAndRotatesFirstCaller(t *testing.T) {
	state, err := (landlordEngine{}).New(2)
	require.NoError(t, err)
	for _, seat := range []int{2, 0, 1} {
		state = landlordTestMove(t, state, seat, map[string]any{"bid": 0})
	}
	view := landlordTestView(t, state, 0)
	assert.Equal(t, "bidding", view.Phase)
	assert.Equal(t, 2, view.Deals)
	assert.Equal(t, 0, state.ToMove)
	assert.Equal(t, [3]int{-1, -1, -1}, view.Bids)
	assert.Equal(t, [3]int{17, 17, 17}, view.Counts)
	assert.Equal(t, -1, view.Landlord)
	assert.Empty(t, view.Bottom)
	// 第二次无人叫牌仍可正常重新发牌，不遗留上一次的叫分。
	for _, seat := range []int{0, 1, 2} {
		state = landlordTestMove(t, state, seat, map[string]any{"bid": 0})
	}
	assert.Equal(t, 1, state.ToMove)
	assert.Equal(t, 3, landlordTestView(t, state, 1).Deals)
}

func TestLandlordRejectsMissingMixedAndWrongPhaseMoves(t *testing.T) {
	engine := landlordEngine{}
	state, err := engine.New(0)
	require.NoError(t, err)
	cases := []string{
		`{}`, `null`, `[]`, `{"bid":null}`, `{"bid":-1}`, `{"bid":4}`, `{"bid":1.5}`,
		`{"bid":1,"cards":[]}`, `{"bid":1,"pass":null}`, `{"bid":1,"unknown":true}`,
		`{"pass":true}`, `{"cards":[0]}`,
	}
	for _, move := range cases {
		_, err := engine.Play(state.Data, 0, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove, move)
	}
	_, err = engine.Play(state.Data, 1, []byte(`{"bid":1}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "wrong turn")
	state = landlordTestMove(t, state, 0, map[string]any{"bid": 2})
	for _, move := range []string{`{"bid":1}`, `{"bid":2}`} {
		_, err := engine.Play(state.Data, 1, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove, "must raise or pass")
	}
	state = landlordTestMove(t, state, 1, map[string]any{"bid": 3})
	for _, move := range []string{
		`{"bid":0}`, `{"cards":[]}`, `{"cards":null}`, `{"pass":false}`, `{"pass":null}`,
		`{"cards":[0],"pass":true}`, `{"cards":[0],"bid":null}`, `{"pass":true}`,
	} {
		_, err := engine.Play(state.Data, 1, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove, move)
	}
}

func TestLandlordPlayOwnershipAndPassesResetTrick(t *testing.T) {
	engine := landlordEngine{}
	state := landlordTestPlaying(t, 0, 0, [3][]int{{0, 4, 8}, {1, 5, 9}, {2, 6, 10}})
	before := append([]byte{}, state.Data...)
	for _, move := range []string{`{"cards":[1]}`, `{"cards":[0,0]}`, `{"cards":[0,4]}`} {
		_, err := engine.Play(state.Data, 0, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove)
		assert.Equal(t, before, state.Data, "rejected moves do not mutate the persisted state")
	}
	state = landlordTestMove(t, state, 0, map[string]any{"cards": []int{4}})
	view := landlordTestView(t, state, 0)
	assert.Equal(t, 1, state.ToMove)
	assert.Equal(t, []int{0, 8}, view.Hand)
	assert.Equal(t, [3]int{2, 3, 3}, view.Counts)
	assert.Equal(t, []int{4}, view.LastPlay.Cards)
	assert.Equal(t, "single", view.LastPlay.Type)
	_, err := engine.Play(state.Data, 1, []byte(`{"cards":[1]}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "lower card cannot beat previous single")
	_, err = engine.Play(state.Data, 1, []byte(`{"cards":[5]}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "equal rank cannot beat previous single")
	_, err = engine.Play(state.Data, 2, []byte(`{"cards":[10]}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "cannot skip turn")
	state = landlordTestMove(t, state, 1, map[string]any{"pass": true})
	view = landlordTestView(t, state, 2)
	assert.Equal(t, 2, state.ToMove)
	assert.Equal(t, [3]bool{false, true, false}, view.Passed)
	assert.NotNil(t, view.LastPlay)
	state = landlordTestMove(t, state, 2, map[string]any{"pass": true})
	view = landlordTestView(t, state, 0)
	assert.Equal(t, 0, state.ToMove)
	assert.Nil(t, view.LastPlay, "two passes allow the last player to lead any valid combination")
	assert.Equal(t, [3]bool{}, view.Passed)
	_, err = engine.Play(state.Data, 0, []byte(`{"pass":true}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "leader cannot pass")
	state = landlordTestMove(t, state, 0, map[string]any{"cards": []int{0}})
	assert.Equal(t, 1, state.ToMove)
	state = landlordTestMove(t, state, 1, map[string]any{"cards": []int{9}})
	view = landlordTestView(t, state, 1)
	assert.Equal(t, 1, view.LastPlay.Seat)
	assert.Equal(t, [3]bool{}, view.Passed)
	state = landlordTestMove(t, state, 2, map[string]any{"pass": true})
	state = landlordTestMove(t, state, 0, map[string]any{"pass": true})
	assert.Equal(t, 1, state.ToMove, "new trick returns to the actual last player, not the landlord")
}

func TestLandlordLandlordAndFarmersWinAsTeams(t *testing.T) {
	cases := []struct {
		name     string
		landlord int
		finisher int
		want     []int
	}{
		{"landlord wins", 1, 1, []int{1}},
		{"first farmer wins for both", 1, 0, []int{0, 2}},
		{"second farmer wins for both", 1, 2, []int{0, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := landlordTestPlaying(t, tc.landlord, tc.finisher, [3][]int{{0}, {4}, {8}})
			state = landlordTestMove(t, state, tc.finisher, map[string]any{"cards": []int{tc.finisher * 4}})
			assert.True(t, state.Over)
			assert.Equal(t, -1, state.ToMove)
			assert.Equal(t, tc.finisher, state.Winner)
			winners, err := (landlordEngine{}).Winners(state.Data, -1)
			require.NoError(t, err)
			assert.Equal(t, tc.want, winners)
			for seat := -1; seat < 3; seat++ {
				view := landlordTestView(t, state, seat)
				assert.True(t, view.Over)
				assert.Equal(t, tc.want, view.Winners)
				require.NotNil(t, view.Hands, "remaining hands become public only after normal completion")
				assert.Empty(t, (*view.Hands)[tc.finisher])
			}
			_, err = (landlordEngine{}).Play(state.Data, (tc.finisher+1)%3, []byte(`{"pass":true}`))
			assert.ErrorIs(t, err, errPvpIllegalMove, "finished match accepts no more actions")
		})
	}
}

func TestLandlordTimeoutAndResignUseLosingTeam(t *testing.T) {
	engine := landlordEngine{}
	state := landlordTestPlaying(t, 1, 1, [3][]int{{0}, {4}, {8}})
	for loser, want := range map[int][]int{0: {1}, 1: {0, 2}, 2: {1}} {
		winners, err := engine.Winners(state.Data, loser)
		require.NoError(t, err)
		assert.Equal(t, want, winners)
	}
	winner, err := engine.Timeout(state.Data)
	require.NoError(t, err)
	assert.Equal(t, 0, winner, "landlord timeout yields first farmer as representative")
	_, err = engine.Winners(state.Data, -1)
	assert.Error(t, err, "unfinished state is not a natural result")
	for _, invalid := range []int{-2, 3} {
		_, err := engine.Winners(state.Data, invalid)
		assert.Error(t, err)
	}
	bidding, err := engine.New(2)
	require.NoError(t, err)
	for loser, want := range map[int][]int{0: {1, 2}, 1: {0, 2}, 2: {0, 1}} {
		winners, err := engine.Winners(bidding.Data, loser)
		require.NoError(t, err)
		assert.Equal(t, want, winners, "auction has not formed teams")
	}
}

func TestLandlordRejectsDamagedDeckBeforeViewOrSettlement(t *testing.T) {
	engine := landlordEngine{}
	pvp, err := engine.New(0)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		mutate func(*landlordState)
	}{
		{"duplicate card", func(state *landlordState) { state.Hands[0][0] = state.Hands[1][0] }},
		{"auction already exhausted", func(state *landlordState) {
			state.BidCount, state.Bids = 3, [3]int{0, 0, 0}
		}},
		{"highest bid does not match record", func(state *landlordState) {
			state.HighBid, state.HighBidder = 2, 0
		}},
		{"uncalled player already bid", func(state *landlordState) { state.Bids[1] = 1 }},
		{"bid decreases", func(state *landlordState) {
			state.BidCount, state.Bids, state.HighBid, state.HighBidder, state.ToMove = 2, [3]int{2, 1, -1}, 2, 0, 2
		}},
		{"three bid did not end auction", func(state *landlordState) {
			state.BidCount, state.Bids, state.HighBid, state.HighBidder, state.ToMove = 1, [3]int{3, -1, -1}, 3, 0, 1
		}},
		{"passed without previous play", func(state *landlordState) { state.Passed[1] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := loadLandlord(pvp.Data)
			require.NoError(t, err)
			tc.mutate(state)
			data, err := common.Marshal(state)
			require.NoError(t, err)
			_, err = engine.View(data, 0)
			assert.Error(t, err)
			_, err = engine.Play(data, 0, []byte(`{"bid":1}`))
			assert.Error(t, err)
			_, err = engine.Winners(data, 0)
			assert.Error(t, err)
		})
	}
}
