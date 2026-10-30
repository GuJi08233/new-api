package mancala

import (
	"context"
	"errors"
	"math/rand/v2"
)

// AI 用 alpha-beta 搜索，局面估值是双方大仓之差(结束的局面按最终子数算)。再走一次时轮到的仍是同一方，搜索按轮到谁取
// 最大或最小，不按层数交替。普通、困难分别搜 normalDepth、hardDepth 步；简单难度一半的时候随手选一个坑，另一半只看一步。
const (
	normalDepth = 4
	hardDepth   = 9
)

var ErrInvalidLevel = errors.New("invalid mancala ai level")

// BestMove 为轮到走的一方选一个坑。level 为 1 简单、2 普通、3 困难。
func BestMove(g *Game, level int, rng *rand.Rand) (int, error) {
	return BestMoveContext(context.Background(), g, level, rng)
}

// BestMoveContext 在 ctx 取消时停止搜索并返回 ctx 的错误。
func BestMoveContext(ctx context.Context, g *Game, level int, rng *rand.Rand) (int, error) {
	if level < 1 || level > 3 {
		return -1, ErrInvalidLevel
	}
	legal := g.Legal()
	if len(legal) == 0 {
		return -1, ErrIllegalMove
	}
	rng.Shuffle(len(legal), func(i, j int) { legal[i], legal[j] = legal[j], legal[i] })
	if level == 1 && rng.IntN(2) == 0 {
		return legal[0], nil
	}
	depth := 1
	switch level {
	case 2:
		depth = normalDepth
	case 3:
		depth = hardDepth
	}
	me := g.turn
	s := &searcher{ctx: ctx, me: me}
	best, bestValue := legal[0], -Total-1
	for _, pit := range legal {
		next := g.clone()
		_ = next.Play(pit)
		value := s.search(next, depth-1, -Total-1, Total+1)
		if s.err != nil {
			return -1, s.err
		}
		if value > bestValue {
			best, bestValue = pit, value
		}
	}
	return best, nil
}

type searcher struct {
	ctx   context.Context
	me    Player
	nodes int
	err   error
}

// evaluate 是 me 一方大仓比对方多几颗。
func (s *searcher) evaluate(g *Game) int {
	first, second := g.Score()
	if s.me == First {
		return first - second
	}
	return second - first
}

func (s *searcher) search(g *Game, depth int, alpha, beta int) int {
	s.nodes++
	if s.nodes&1023 == 0 && s.err == nil {
		s.err = s.ctx.Err()
	}
	if s.err != nil || depth <= 0 || g.over {
		return s.evaluate(g)
	}
	maximizing := g.turn == s.me
	for _, pit := range g.Legal() {
		next := g.clone()
		_ = next.Play(pit)
		value := s.search(next, depth-1, alpha, beta)
		if maximizing {
			alpha = max(alpha, value)
		} else {
			beta = min(beta, value)
		}
		if alpha >= beta {
			break
		}
	}
	if maximizing {
		return alpha
	}
	return beta
}
