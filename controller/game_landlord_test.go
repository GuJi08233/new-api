package controller

import (
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/landlord"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// landlordTestEndgame 是一局出牌阶段的牌：三家手牌如 hands，其余的牌都已打出，这一轮刚结束，轮到 turn 领出。
func landlordTestEndgame(t *testing.T, landlordSeat int, turn int, hands [landlord.Players][]int) pvpState {
	t.Helper()
	game := &landlord.Game{
		Phase: landlord.PhasePlaying, Hands: hands, Bids: [landlord.Players]int{-1, -1, -1}, BidFirst: landlordSeat,
		BidCount: 1, HighBid: landlord.MaxBid, HighBidder: landlordSeat, Deals: 1, Landlord: landlordSeat, ToMove: turn, Winner: -1,
	}
	game.Bids[landlordSeat] = landlord.MaxBid
	var held [landlord.DeckSize]bool
	for _, hand := range hands {
		for _, card := range hand {
			held[card] = true
		}
	}
	for card, inHand := range held {
		if !inHand {
			game.Played = append(game.Played, card)
		}
	}
	game.Bottom = slices.Clone(game.Played[:landlord.BottomSize])
	last := game.Played[len(game.Played)-1]
	game.Trick = []landlord.Action{
		{Seat: turn, Type: landlord.Single, Cards: []int{last}},
		{Seat: (turn + 1) % landlord.Players, Pass: true},
		{Seat: (turn + 2) % landlord.Players, Pass: true},
	}
	require.NoError(t, game.Validate())
	state, err := landlordPvpState(game)
	require.NoError(t, err)
	return state
}

// 一次请求只能是叫分、出牌、不出中的一种，哪怕多出来的字段是 null；格式不对的一律当作不合规则。
func TestLandlordMoveCarriesExactlyOneAction(t *testing.T) {
	engine := landlordEngine{}
	bidding, err := engine.New(0)
	require.NoError(t, err)
	for _, move := range []string{
		`{}`, `null`, `[]`, `"bid"`, `{"bid":null}`, `{"bid":-1}`, `{"bid":4}`, `{"bid":1.5}`, `{"bid":"1"}`,
		`{"bid":1,"cards":[]}`, `{"bid":1,"pass":null}`, `{"bid":1,"unknown":true}`, `{"unknown":true}`,
		`{"pass":true}`, `{"cards":[0]}`,
	} {
		_, err := engine.Play(bidding.Data, 0, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove, move)
	}
	_, err = engine.Play(bidding.Data, 1, []byte(`{"bid":1}`))
	assert.ErrorIs(t, err, errPvpIllegalMove, "wrong turn")
	_, err = engine.Play(bidding.Data, 0, []byte(`{"bid":2}`))
	require.NoError(t, err)

	playing := landlordTestEndgame(t, 0, 0, [landlord.Players][]int{{0, 4}, {1}, {2}})
	for _, move := range []string{
		`{"bid":0}`, `{"cards":[]}`, `{"cards":null}`, `{"pass":false}`, `{"pass":null}`, `{"pass":true}`,
		`{"cards":[0],"pass":true}`, `{"cards":[0],"bid":null}`, `{"cards":[1]}`, `{"cards":[0,0]}`,
	} {
		_, err := engine.Play(playing.Data, 0, []byte(move))
		assert.ErrorIs(t, err, errPvpIllegalMove, move)
	}
	next, err := engine.Play(playing.Data, 0, []byte(`{"cards":[4]}`))
	require.NoError(t, err)
	assert.Equal(t, 1, next.ToMove)
}

// 给玩家的局面里只有自己的手牌：别人的牌和底牌不出现在响应的任何地方，正常打完之后才公开剩下的牌。
func TestLandlordViewShowsOnlyTheViewersHand(t *testing.T) {
	engine := landlordEngine{}
	state, err := engine.New(2)
	require.NoError(t, err)
	var game landlord.Game
	require.NoError(t, common.Unmarshal(state.Data, &game))
	for seat := -1; seat < landlord.Players; seat++ {
		view, err := engine.View(state.Data, seat)
		require.NoError(t, err)
		encoded, err := common.Marshal(view)
		require.NoError(t, err)
		var response map[string]any
		require.NoError(t, common.Unmarshal(encoded, &response))
		assert.NotContains(t, response, "hands")
		assert.NotContains(t, response, "played")
		assert.Empty(t, response["bottom"])
		hand := response["hand"].([]any)
		if seat < 0 {
			assert.Empty(t, hand, "spectators see no hand")
			continue
		}
		require.Len(t, hand, landlord.HandSize)
		for i, card := range hand {
			assert.EqualValues(t, game.Hands[seat][i], card)
		}
	}
}

// AI 给出的一步交给 Play 一定能执行：托管与人机对战都直接把 AiMove 的结果当成玩家的一步。三家都由 AI 出牌时每局都能
// 打完，结算的输赢合计为 0。
func TestLandlordAiMovesAreAcceptedUntilTheGameEnds(t *testing.T) {
	engine := landlordEngine{}
	for round := 0; round < 6; round++ {
		state, err := engine.New(round % landlord.Players)
		require.NoError(t, err)
		for steps := 0; !state.Over; steps++ {
			require.Less(t, steps, pvpAiMaxReplies)
			move, err := engine.AiMove(state.Data, state.ToMove, 1+steps%pvpAiLevels)
			require.NoError(t, err)
			state, err = engine.Play(state.Data, state.ToMove, move)
			require.NoError(t, err, "ai move %s", move)
		}
		scores, err := engine.Scores(state.Data)
		require.NoError(t, err)
		sum := 0
		for _, score := range scores {
			sum += score
		}
		assert.Zero(t, sum)
	}
	unfinished, err := engine.New(0)
	require.NoError(t, err)
	_, err = engine.Scores(unfinished.Data)
	assert.Error(t, err, "an unfinished game has no scores")
	_, err = engine.AiMove(unfinished.Data, 1, 2)
	assert.ErrorIs(t, err, errPvpIllegalMove, "the ai only moves for the seat to move")
}
