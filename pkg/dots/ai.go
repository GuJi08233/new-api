package dots

import (
	"context"
	"errors"
	"math/bits"
	"math/rand/v2"
)

// AI：
//   - 剩下的边不多时精确搜索：局面只由连过的边决定，"从这里往后轮到的一方还能比对方多拿几个格子"是这些边的函数，带备忘的
//     negamax 把每个局面只算一次。普通难度剩 normalSearchEdges 条以内、困难难度剩 hardSearchEdges 条以内时搜索，
//     最优解里自然包含"留两个格子给对方、保住先手"这类长链技巧。
//   - 边还多时按经验走：能围格子就围；否则连不会让任何格子变成三条边的"安全边"；没有安全边时挑送给对方格子最少的一条。
//   - 简单难度一半的时候随手连一条边，另一半按经验走，不搜索。
const (
	normalSearchEdges = 10
	hardSearchEdges   = 18
)

var ErrInvalidLevel = errors.New("invalid dots ai level")

// BestMove 为轮到走的一方选一条边。level 为 1 简单、2 普通、3 困难。
func BestMove(g *Game, level int, rng *rand.Rand) (int, error) {
	return BestMoveContext(context.Background(), g, level, rng)
}

// BestMoveContext 在 ctx 取消时停止搜索并返回 ctx 的错误。
func BestMoveContext(ctx context.Context, g *Game, level int, rng *rand.Rand) (int, error) {
	if level < 1 || level > 3 {
		return -1, ErrInvalidLevel
	}
	if g.Over() {
		return -1, ErrIllegalMove
	}
	free := freeEdges(g.drawn)
	if level == 1 && rng.IntN(2) == 0 {
		return free[rng.IntN(len(free))], nil
	}
	limit := 0
	switch level {
	case 2:
		limit = normalSearchEdges
	case 3:
		limit = hardSearchEdges
	}
	if len(free) <= limit {
		s := &searcher{ctx: ctx, memo: make(map[uint64]int8)}
		best, bestValue := -1, -Boxes-1
		for _, edge := range shuffled(free, rng) {
			value := s.after(g.drawn, edge)
			if s.err != nil {
				return -1, s.err
			}
			if value > bestValue {
				best, bestValue = edge, value
			}
		}
		return best, nil
	}
	return heuristicMove(g.drawn, free, rng), nil
}

// freeEdges 返回还没连的边。
func freeEdges(drawn uint64) []int {
	free := make([]int, 0, Edges)
	for e := 0; e < Edges; e++ {
		if drawn&(1<<e) == 0 {
			free = append(free, e)
		}
	}
	return free
}

func shuffled(edges []int, rng *rand.Rand) []int {
	out := append([]int(nil), edges...)
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// heuristicMove：能围格子就围，否则连安全边，都没有时送出最少的格子。
func heuristicMove(drawn uint64, free []int, rng *rand.Rand) int {
	free = shuffled(free, rng)
	for _, edge := range free {
		if completes(drawn, edge) > 0 {
			return edge
		}
	}
	for _, edge := range free {
		if safe(drawn, edge) {
			return edge
		}
	}
	best, fewest := free[0], Boxes+1
	for _, edge := range free {
		if given := giveaway(drawn | 1<<edge); given < fewest {
			best, fewest = edge, given
		}
	}
	return best
}

// safe 表示连上 edge 之后两侧的格子都不会只差一条边。
func safe(drawn uint64, edge int) bool {
	for _, box := range edgeBoxes[edge] {
		if box >= 0 && sides(drawn, box) == 2 {
			return false
		}
	}
	return true
}

// giveaway 返回对方在 drawn 上一路贪心围格子能拿走几个。
func giveaway(drawn uint64) int {
	taken := 0
	for {
		found := false
		for e := 0; e < Edges; e++ {
			if drawn&(1<<e) != 0 {
				continue
			}
			if n := completes(drawn, e); n > 0 {
				drawn |= 1 << e
				taken += n
				found = true
			}
		}
		if !found {
			return taken
		}
	}
}

type searcher struct {
	ctx   context.Context
	memo  map[uint64]int8
	nodes int
	err   error
}

// value 返回在 drawn 上轮到走的一方从此往后最多能比对方多拿几个格子。
func (s *searcher) value(drawn uint64) int {
	if bits.OnesCount64(drawn) == Edges {
		return 0
	}
	if v, ok := s.memo[drawn]; ok {
		return int(v)
	}
	s.nodes++
	if s.nodes&0xfff == 0 && s.ctx.Err() != nil {
		s.err = s.ctx.Err()
	}
	if s.err != nil {
		return 0
	}
	best := -Boxes - 1
	for e := 0; e < Edges; e++ {
		if drawn&(1<<e) != 0 {
			continue
		}
		if v := s.after(drawn, e); v > best {
			best = v
		}
	}
	s.memo[drawn] = int8(best)
	return best
}

// after 返回轮到走的一方连上 edge 之后的净得格子数：围下格子时接着走，否则换对方。
func (s *searcher) after(drawn uint64, edge int) int {
	gained := completes(drawn, edge)
	next := drawn | 1<<edge
	if gained > 0 {
		return gained + s.value(next)
	}
	return -s.value(next)
}
