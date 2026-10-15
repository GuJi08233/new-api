package landlord

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHintMovesAreLegalDeterministicAndReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hand     []int
		previous []int
		first    []int
	}{
		{name: "lead a planned pair", hand: []int{3, 3, 4, 4, 5}, first: []int{3, 3}},
		{name: "lead all cards with four and a pair", hand: []int{4, 4, 4, 4, 5, 5}, first: []int{4, 4, 4, 4, 5, 5}},
		{name: "follow a single before bombs", hand: []int{4, 5, 6, 7, 7, 7, 7, 16, 17}, previous: []int{3}, first: []int{4}},
		{name: "follow a pair", hand: []int{4, 4, 5, 5, 6}, previous: []int{3, 3}, first: []int{4, 4}},
		{name: "follow a straight", hand: []int{4, 5, 6, 7, 8, 12}, previous: []int{3, 4, 5, 6, 7}, first: []int{4, 5, 6, 7, 8}},
		{name: "airplane can use a pair as single wings", hand: []int{4, 4, 4, 5, 5, 5, 6, 6}, previous: []int{3, 3, 3, 4, 4, 4, 7, 8}, first: []int{4, 4, 4, 5, 5, 5, 6, 6}},
		{name: "four with a pair finishes before using a bomb", hand: []int{4, 4, 4, 4, 5, 5}, previous: []int{3, 3, 3, 3, 6, 7}, first: []int{4, 4, 4, 4, 5, 5}},
		{name: "cannot beat the big joker", hand: []int{3, 4, 14, 16}, previous: []int{17}},
		{name: "cannot beat a rocket", hand: []int{3, 3, 3, 3}, previous: []int{16, 17}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := Knowledge{Seat: 0, Phase: PhasePlaying, Hand: cardsOf(tc.hand...), Landlord: 0, Counts: [Players]int{len(tc.hand), 5, 5}}
			if tc.previous != nil {
				combo, err := Classify(cardsOf(tc.previous...))
				require.NoError(t, err)
				k.LastPlay = &Play{Combination: combo, Seat: 2, Cards: cardsOf(tc.previous...)}
			}
			handBefore := slices.Clone(k.Hand)
			hints := HintMoves(k)
			assert.Equal(t, handBefore, k.Hand)
			assert.Equal(t, hints, HintMoves(k), "repeated requests keep cycling order stable")
			require.LessOrEqual(t, len(hints), 12)
			if tc.first == nil {
				assert.Equal(t, [][]int{}, hints)
				return
			}
			require.NotEmpty(t, hints)
			assert.Equal(t, tc.first, ranksOf(hints[0]))
			seenBomb := false
			for i, cards := range hints {
				combo, err := Classify(cards)
				require.NoError(t, err)
				for _, card := range cards {
					assert.Contains(t, k.Hand, card)
				}
				if k.LastPlay != nil {
					assert.True(t, combo.Beats(k.LastPlay.Combination))
				}
				for _, earlier := range hints[:i] {
					assert.NotEqual(t, earlier, cards, "equivalent selections should not repeat")
				}
				if len(cards) == len(k.Hand) {
					continue
				}
				if combo.Type == Bomb || combo.Type == Rocket {
					seenBomb = true
				} else {
					assert.False(t, seenBomb, "ordinary replies precede bombs")
				}
			}
		})
	}
}

func TestHintMovesBoundCandidatesAndIgnoreOtherPhases(t *testing.T) {
	k := Knowledge{
		Seat: 0, Phase: PhasePlaying, Landlord: 0, Counts: [Players]int{20, 17, 17},
		Hand: cardsOf(3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 12, 13, 14),
	}
	hints := HintMoves(k)
	require.NotEmpty(t, hints)
	assert.LessOrEqual(t, len(hints), 12)
	for _, phase := range []string{PhaseBidding, PhaseFinished} {
		k.Phase = phase
		assert.Equal(t, [][]int{}, HintMoves(k))
	}
}
