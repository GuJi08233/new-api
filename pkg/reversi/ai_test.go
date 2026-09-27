package reversi

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seeded(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x5eed))
}

// 普通与困难难度能一步赢下来就走这一步，不走让对方一步赢回去的那一步。每个局面换几个随机种子，不是碰巧挑中。
func TestBestMoveTakesWinsAndStopsLosses(t *testing.T) {
	cases := map[string]struct {
		rows []string
		want Point
	}{
		// 黑走 c3 同时夹住 d3、b4，白棋剩下的两子谁也夹不住，终局 6:2；黑走 b5 的话白走 f3 就以 5:4 结束。
		"take the win": {
			rows: []string{"...w.w..", "........", ".b.wb...", ".w......", "b.......", "........", "........", "........"},
			want: Point{X: 2, Y: 2},
		},
		// 黑走 e3 的话白走 f3 把黑子全部翻掉；走 a3 之后白无子可下。
		"avoid the loss": {
			rows: []string{"..w.....", "........", ".wbw....", "........", "........", "........", "........", "........"},
			want: Point{X: 0, Y: 2},
		},
	}
	for name, c := range cases {
		for _, level := range []int{2, 3} {
			for seed := uint64(1); seed <= 3; seed++ {
				move, err := BestMove(setup(t, Black, c.rows...), level, seeded(seed))
				require.NoError(t, err)
				assert.Equal(t, c.want, move, "%s level %d seed %d", name, level, seed)
			}
		}
	}
}

// 剩下的空格不多时困难难度直接算到终局：它选的一步能拿到的最终子数差，与穷举双方所有走法得出的最好结果一样。
// 局面取自几局固定种子的随机对局走到只剩 8 个空格时。
func TestHardSolvesEndgames(t *testing.T) {
	// bestDiff 穷举到终局，返回双方都走最好时的最终子数差(黑减白)：黑走时取最大，白走时取最小。
	var bestDiff func(g *Game) int
	bestDiff = func(g *Game) int {
		if g.Over() {
			black, white := g.Count()
			return black - white
		}
		best := 0
		for i, p := range g.Legal() {
			next := *g
			next.moves = g.Moves()
			require.NoError(t, next.Play(p.X, p.Y))
			diff := bestDiff(&next)
			if i == 0 || (g.Turn() == Black && diff > best) || (g.Turn() == White && diff < best) {
				best = diff
			}
		}
		return best
	}
	for seed := uint64(1); seed <= 3; seed++ {
		rng := seeded(seed)
		g := New()
		for !g.Over() && Squares-4-len(g.Moves()) > 8 {
			legal := g.Legal()
			p := legal[rng.IntN(len(legal))]
			require.NoError(t, g.Play(p.X, p.Y))
		}
		require.False(t, g.Over())
		move, err := BestMove(g, 3, rng)
		require.NoError(t, err)
		next := *g
		next.moves = g.Moves()
		require.NoError(t, next.Play(move.X, move.Y))
		assert.Equal(t, bestDiff(g), bestDiff(&next), "seed %d, moves %v", seed, g.Moves())
	}
}

// 困难难度执黑、执白都能赢简单难度：几个固定种子下的整局。
func TestHardBeatsEasy(t *testing.T) {
	for seed := uint64(1); seed <= 3; seed++ {
		for _, hard := range []Disc{Black, White} {
			rng := seeded(seed)
			g := New()
			for !g.Over() {
				level := 1
				if g.Turn() == hard {
					level = 3
				}
				move, err := BestMove(g, level, rng)
				require.NoError(t, err)
				require.NoError(t, g.Play(move.X, move.Y))
			}
			assert.Equal(t, hard, g.Winner(), "seed %d, hard plays %d, moves %v", seed, hard, g.Moves())
		}
	}
}

// 难度只能是 1 到 3，结束的对局没有下一步。
func TestBestMoveRejectsBadLevelAndFinishedGame(t *testing.T) {
	_, err := BestMove(New(), 4, seeded(1))
	assert.ErrorIs(t, err, ErrInvalidLevel)
	over := setup(t, Black, "bbb.....", "........", "........", "........", "........", "........", "........", ".....www")
	over.turn = Empty
	_, err = BestMove(over, 3, seeded(1))
	assert.ErrorIs(t, err, ErrIllegalMove)
}
