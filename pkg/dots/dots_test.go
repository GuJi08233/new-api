package dots

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 左上角格子的四条边：上、下、左、右。
var topLeft = [4]int{0, Size, HorizontalEdges, HorizontalEdges + 1}

// 连上一个格子的第四条边就得到它并接着走；没围下格子时换对方。重复的边、越界的边都不合规则。
func TestClaimingABoxKeepsTheTurn(t *testing.T) {
	g := New()
	require.NoError(t, g.Play(topLeft[0]))
	assert.Equal(t, Second, g.Turn())
	require.NoError(t, g.Play(topLeft[1]))
	require.NoError(t, g.Play(topLeft[2]))
	assert.Equal(t, Second, g.Turn())
	require.NoError(t, g.Play(topLeft[3]))
	assert.Equal(t, Second, g.Turn(), "围下格子的一方接着走")
	assert.Equal(t, []int{0}, g.Claimed())
	first, second := g.Score()
	assert.Equal(t, 0, first)
	assert.Equal(t, 1, second)
	assert.Equal(t, byte('b'), g.Owners()[0])

	assert.ErrorIs(t, g.Play(topLeft[0]), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(Edges), ErrIllegalMove)
	assert.ErrorIs(t, g.Play(-1), ErrIllegalMove)
	_, err := Replay([]int{0, 0})
	assert.ErrorIs(t, err, ErrIllegalMove)
}

// 一条边同时补上两个格子时两个都归他。所有边连完后格子多的一方获胜。
func TestSharedEdgeClaimsTwoBoxesAndTheGameEnds(t *testing.T) {
	g := New()
	// 左上两个格子之间的竖边留到最后。
	shared := HorizontalEdges + 1
	for _, e := range []int{0, 1, Size, Size + 1, HorizontalEdges, HorizontalEdges + 2} {
		require.NoError(t, g.Play(e))
	}
	mover := g.Turn()
	require.NoError(t, g.Play(shared))
	assert.ElementsMatch(t, []int{0, 1}, g.Claimed())
	assert.Equal(t, mover, g.Turn())

	for e := 0; e < Edges; e++ {
		if g.drawn&(1<<e) == 0 {
			require.NoError(t, g.Play(e))
		}
	}
	assert.True(t, g.Over())
	assert.Equal(t, Nobody, g.Turn())
	first, second := g.Score()
	assert.Equal(t, Boxes, first+second)
	if first != second {
		assert.NotEqual(t, Nobody, g.Winner())
	}
}

// 备忘搜索的结果和不带备忘的完整 minimax 一样。
func TestSearchMatchesPlainMinimax(t *testing.T) {
	var plain func(drawn uint64) int
	plain = func(drawn uint64) int {
		best, any := -Boxes-1, false
		for e := 0; e < Edges; e++ {
			if drawn&(1<<e) != 0 {
				continue
			}
			any = true
			gained := completes(drawn, e)
			v := -plain(drawn | 1<<e)
			if gained > 0 {
				v = gained + plain(drawn|1<<e)
			}
			best = max(best, v)
		}
		if !any {
			return 0
		}
		return best
	}
	rng := rand.New(rand.NewPCG(3, 3))
	for round := 0; round < 5; round++ {
		order := rng.Perm(Edges)
		var drawn uint64
		for _, e := range order[:Edges-8] {
			drawn |= 1 << e
		}
		s := &searcher{ctx: context.Background(), memo: map[uint64]int8{}}
		assert.Equal(t, plain(drawn), s.value(drawn), "round %d", round)
	}
}

// 每一档 AI 走的都是合规的边，能把一局下完；有格子可围时普通与困难难度一定去围。
func TestAiPlaysLegalMovesAndTakesFreeBoxes(t *testing.T) {
	for level := 1; level <= 3; level++ {
		rng := rand.New(rand.NewPCG(uint64(level), 9))
		g := New()
		for !g.Over() {
			edge, err := BestMove(g, level, rng)
			require.NoError(t, err)
			require.NoError(t, g.Play(edge), "level %d", level)
		}
	}
	g, err := Replay([]int{topLeft[0], topLeft[1], topLeft[2]})
	require.NoError(t, err)
	for level := 2; level <= 3; level++ {
		edge, err := BestMove(g, level, rand.New(rand.NewPCG(1, 1)))
		require.NoError(t, err)
		assert.Equal(t, topLeft[3], edge, "level %d", level)
	}
	_, err = BestMove(New(), 4, rand.New(rand.NewPCG(1, 1)))
	assert.ErrorIs(t, err, ErrInvalidLevel)
}
