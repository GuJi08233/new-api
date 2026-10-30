package mancala

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 播子逆时针一坑一颗；最后一颗落进自己的大仓再走一次，否则换人；空坑和越界的坑不能选。
func TestSowingAndExtraTurn(t *testing.T) {
	g := New()
	// 先手第 2 个坑 4 颗：落在 3、4、5、6(大仓)，再走一次。
	require.NoError(t, g.Play(2))
	assert.Equal(t, []int{4, 4, 0, 5, 5, 5, 1, 4, 4, 4, 4, 4, 4, 0}, g.Board())
	assert.Equal(t, []int{3, 4, 5, 6}, g.Last())
	assert.True(t, g.Extra())
	assert.Equal(t, First, g.Turn())
	assert.ErrorIs(t, g.Play(2), ErrIllegalMove, "空坑")
	assert.ErrorIs(t, g.Play(6), ErrIllegalMove)
	require.NoError(t, g.Play(0))
	assert.Equal(t, Second, g.Turn())
	assert.False(t, g.Extra())
}

// 最后一颗落进自己一侧的空坑、对面有子时，连同对面的子一起收进大仓；对面是空的则不吃。
func TestCapture(t *testing.T) {
	g := &Game{turn: First, captured: -1}
	g.board = [Holes]int{1, 0, 0, 0, 0, 1, 0, 2, 2, 2, 2, 5, 3, 0}
	require.NoError(t, g.Play(0))
	// 落在 1(空坑)，对面是 11，有 5 颗。
	assert.Equal(t, 11, g.Captured())
	assert.Equal(t, 0, g.Board()[11])
	assert.Equal(t, 6, g.Board()[6])
	assert.Equal(t, Second, g.Turn())

	h := &Game{turn: First, captured: -1}
	h.board = [Holes]int{1, 0, 0, 0, 0, 1, 0, 2, 2, 2, 2, 0, 3, 0}
	require.NoError(t, h.Play(0))
	assert.Equal(t, -1, h.Captured())
	assert.Equal(t, 1, h.Board()[1])
}

// 后手播子跳过先手的大仓。
func TestSecondSkipsFirstStore(t *testing.T) {
	g := &Game{turn: Second, captured: -1}
	g.board = [Holes]int{1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 9, 0}
	require.NoError(t, g.Play(5))
	assert.Equal(t, 0, g.Board()[6], "跳过对方大仓")
	assert.Equal(t, 1, g.Board()[13])
	assert.Equal(t, []int{2, 2, 2, 2, 2, 2, 0, 2, 2, 1, 1, 1, 0, 1}, g.Board())
}

// 一方的小坑全空时结束，另一方收走自己一侧剩下的子，子数守恒，大仓多的赢。
func TestGameEndsWhenASideIsEmpty(t *testing.T) {
	g := &Game{turn: First, captured: -1}
	g.board = [Holes]int{0, 0, 0, 0, 0, 1, 20, 1, 0, 0, 0, 3, 0, 23}
	require.NoError(t, g.Play(5))
	assert.True(t, g.Over())
	assert.Equal(t, Nobody, g.Turn())
	first, second := g.Score()
	assert.Equal(t, 21, first)
	assert.Equal(t, 27, second)
	assert.Equal(t, Second, g.Winner())
	assert.Equal(t, Total, first+second)
	assert.ErrorIs(t, g.Play(0), ErrIllegalMove)
}

// 重放与逐步走得到同样的局面，非法的记录被拒绝。
func TestReplay(t *testing.T) {
	g := New()
	moves := []int{2, 5, 0, 1}
	for _, pit := range moves {
		require.NoError(t, g.Play(pit))
	}
	r, err := Replay(moves)
	require.NoError(t, err)
	assert.Equal(t, g.Board(), r.Board())
	assert.Equal(t, g.Turn(), r.Turn())
	_, err = Replay([]int{2, 2})
	assert.ErrorIs(t, err, ErrIllegalMove)
}

// 普通、困难难度不会错过吃掉一大坑的机会；AI 对下到底都在合理时间内走完，子数守恒。
func TestAiPlaysWholeGames(t *testing.T) {
	for level := 2; level <= 3; level++ {
		g := &Game{turn: First, captured: -1}
		g.board = [Holes]int{1, 0, 0, 0, 0, 2, 0, 1, 1, 1, 1, 10, 1, 0}
		pit, err := BestMove(g, level, rand.New(rand.NewPCG(1, 1)))
		require.NoError(t, err)
		assert.Equal(t, 0, pit, "吃掉对面的 10 颗")
	}

	for level := 1; level <= 3; level++ {
		g := New()
		rng := rand.New(rand.NewPCG(uint64(level), 7))
		started := time.Now()
		for !g.Over() {
			pit, err := BestMove(g, level, rng)
			require.NoError(t, err)
			require.NoError(t, g.Play(pit))
		}
		first, second := g.Score()
		assert.Equal(t, Total, first+second)
		assert.Less(t, time.Since(started), 10*time.Second)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BestMoveContext(ctx, New(), 3, rand.New(rand.NewPCG(1, 1)))
	assert.ErrorIs(t, err, context.Canceled)
}

// 困难难度对普通难度大多能赢。
func TestHardBeatsNormal(t *testing.T) {
	wins := 0
	for game := 0; game < 6; game++ {
		g := New()
		rng := rand.New(rand.NewPCG(uint64(game), 3))
		hard := First
		if game%2 == 1 {
			hard = Second
		}
		for !g.Over() {
			level := 2
			if g.Turn() == hard {
				level = 3
			}
			pit, err := BestMove(g, level, rng)
			require.NoError(t, err)
			require.NoError(t, g.Play(pit))
		}
		if g.Winner() == hard {
			wins++
		}
	}
	assert.GreaterOrEqual(t, wins, 4)
}
