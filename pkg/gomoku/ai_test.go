package gomoku

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seeded(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x5eed))
}

// 能成五就成五(三档都是)；普通与困难必堵对方的冲四与活三。每个局面换几个随机种子，不是碰巧挑中。
func TestBestMoveTakesWinsAndStopsThreats(t *testing.T) {
	cases := map[string]struct {
		moves   [][2]int
		levels  []int
		allowed []Point
	}{
		// 黑棋第 7 行 3-6 连着四子、左边被白堵住；白棋第 9 行有活三。黑直接在右边成五，不去堵白。
		"complete five": {
			moves:   [][2]int{{3, 7}, {2, 7}, {4, 7}, {3, 9}, {5, 7}, {4, 9}, {6, 7}, {5, 9}},
			levels:  []int{1, 2, 3},
			allowed: []Point{{X: 7, Y: 7}},
		},
		// 黑棋冲四，白只能堵唯一的成五点。
		"block four": {
			moves:   [][2]int{{3, 7}, {2, 7}, {4, 7}, {3, 10}, {5, 7}, {7, 11}, {6, 7}},
			levels:  []int{2, 3},
			allowed: []Point{{X: 7, Y: 7}},
		},
		// 黑棋第 7 行 5-7 是两头都空的活三，白没有自己的冲四可走，只能堵在紧挨着的两头之一，堵远一格黑就成活四。
		"block open three": {
			moves:   [][2]int{{5, 7}, {5, 10}, {6, 7}, {9, 11}, {7, 7}},
			levels:  []int{2, 3},
			allowed: []Point{{X: 4, Y: 7}, {X: 8, Y: 7}},
		},
	}
	for name, c := range cases {
		for _, level := range c.levels {
			for seed := uint64(1); seed <= 3; seed++ {
				g, err := Replay(squares(c.moves...))
				require.NoError(t, err)
				move, err := BestMove(g, level, seeded(seed))
				require.NoError(t, err)
				assert.Contains(t, c.allowed, move, "%s level %d seed %d", name, level, seed)
			}
		}
	}
}

// 空棋盘上 AI 执黑先走天元。
func TestBestMoveOpensAtCenter(t *testing.T) {
	for level := 1; level <= 3; level++ {
		move, err := BestMove(New(), level, seeded(1))
		require.NoError(t, err)
		assert.Equal(t, Point{X: 7, Y: 7}, move)
	}
}

// playOut 让 black、white 两档难度下完一局，返回赢家。
func playOut(t *testing.T, black int, white int, seed uint64) (Stone, []int) {
	t.Helper()
	rng := seeded(seed)
	g := New()
	for !g.Over() {
		level := black
		if g.Turn() == White {
			level = white
		}
		move, err := BestMove(g, level, rng)
		require.NoError(t, err)
		require.NoError(t, g.Play(move.X, move.Y))
	}
	return g.Winner(), g.Moves()
}

// 困难难度执黑、执白都能赢简单与普通难度：几个固定种子下的整局。
func TestHardBeatsEasyAndNormal(t *testing.T) {
	for _, weaker := range []int{1, 2} {
		for seed := uint64(1); seed <= 3; seed++ {
			winner, moves := playOut(t, 3, weaker, seed)
			assert.Equal(t, Black, winner, "hard black vs level %d, seed %d, moves %v", weaker, seed, moves)
			winner, moves = playOut(t, weaker, 3, seed)
			assert.Equal(t, White, winner, "level %d black vs hard white, seed %d, moves %v", weaker, seed, moves)
		}
	}
}

// 难度只能是 1 到 3，结束的对局没有下一步。
func TestBestMoveRejectsBadLevelAndFinishedGame(t *testing.T) {
	_, err := BestMove(New(), 4, seeded(1))
	assert.ErrorIs(t, err, ErrInvalidLevel)
	_, err = BestMove(New(), 0, seeded(1))
	assert.ErrorIs(t, err, ErrInvalidLevel)
	won, err := Replay(squares([2]int{3, 7}, [2]int{3, 9}, [2]int{4, 7}, [2]int{5, 9}, [2]int{5, 7}, [2]int{7, 9}, [2]int{6, 7}, [2]int{9, 9}, [2]int{7, 7}))
	require.NoError(t, err)
	_, err = BestMove(won, 3, seeded(1))
	assert.ErrorIs(t, err, ErrIllegalMove)
}
