package reversi

import (
	"context"
	"errors"
	"math/bits"
	"math/rand/v2"
)

// 黑白棋 AI：negamax 形式的 alpha-beta 搜索，无子可下时跳过(不耗搜索深度)，双方都无子可下时按子数定输赢。
//
// 三档难度：
//   - 1 简单：挑翻子最多的一步，翻子数加上随机扰动，不看位置；
//   - 2 普通：位置权重表(角最好，角旁边的格子最差)，搜 3 步；
//   - 3 困难：位置、行动力(双方合法着法数之差)、角与角旁的星位/边位、前沿子(挨着空格、容易被翻的子)一起评估，搜 6 步；
//     剩下 12 个空格以内直接算到终局，按最终子数差下棋。
//
// 普通与困难在得分与最好一步相差不超过容差的几步里随机挑一步(算到终局时只在结果一样的几步里挑)，同一局面不总走同一步，
// 玩家背不下一条稳赢的棋谱。

var ErrInvalidLevel = errors.New("invalid reversi ai level")

const (
	// decidedScore 是终局的分数基数：赢了是 decidedScore + 子数差，输了是 -decidedScore + 子数差，和棋为 0。局面分远小于它。
	decidedScore = 100_000
	infinity     = 1_000_000

	easyNoise       = 3
	normalDepth     = 3
	normalTolerance = 2
	hardDepth       = 6
	hardTolerance   = 4
	// hardExactEmpties 以内的空格，困难难度直接搜到终局。
	hardExactEmpties = 12
)

// nodeLimit 是一次搜索最多展开的节点数。搜索每秒约展开一千万个节点，实测困难难度中局最多用到约 25 万个、算终局最多约
// 21 万个，个别局面可能多得多；超过上限的搜索作废重来(见 searchMove)，一步棋最多想零点几秒。测试里调小它走到重来的路径。
var nodeLimit = 2_000_000

// squareWeights 是普通难度的位置权重：角一旦占住就不会被翻，角旁边的星位与边位会把角送给对方。
var squareWeights = [Squares]int{
	100, -20, 10, 5, 5, 10, -20, 100,
	-20, -50, -2, -2, -2, -2, -50, -20,
	10, -2, 1, 1, 1, 1, -2, 10,
	5, -2, 1, 0, 0, 1, -2, 5,
	5, -2, 1, 0, 0, 1, -2, 5,
	10, -2, 1, 1, 1, 1, -2, 10,
	-20, -50, -2, -2, -2, -2, -50, -20,
	100, -20, 10, 5, 5, 10, -20, 100,
}

// 困难难度的角与角旁格子：xSquares[i] 是角 corners[i] 斜对的星位，cSquares[i] 是与它相邻的两个边位。角空着时占星位、
// 边位要扣分，角已经有子时就不再危险。
var (
	corners    = [4]int{0, 7, 56, 63}
	cornerMask = uint64(1)<<0 | 1<<7 | 1<<56 | 1<<63
	xSquares   = [4]int{9, 14, 49, 54}
	cSquares   = [4]uint64{1<<1 | 1<<8, 1<<6 | 1<<15, 1<<48 | 1<<57, 1<<55 | 1<<62}
)

// weightedSum 按位置权重表给 stones 加总。
func weightedSum(stones uint64) int {
	sum := 0
	for stones != 0 {
		sum += squareWeights[bits.TrailingZeros64(stones)]
		stones &= stones - 1
	}
	return sum
}

// evaluateNormal 是普通难度的局面分：双方位置权重之差。
func evaluateNormal(me, opp uint64) int {
	return weightedSum(me) - weightedSum(opp)
}

// neighbors 是 stones 周围八格(不含 stones 自己)。
func neighbors(stones uint64) uint64 {
	around := stones
	around |= (stones << 1) & notFileA
	around |= (stones >> 1) & notFileH
	around |= stones << 8
	around |= stones >> 8
	around |= (stones << 9) & notFileA
	around |= (stones << 7) & notFileH
	around |= (stones >> 7) & notFileA
	around |= (stones >> 9) & notFileH
	return around &^ stones
}

// evaluateHard 是困难难度的局面分：角最重要；角空着时占它旁边的星位、边位扣分；行动力(自己能走的步数多、对方少)
// 在中局最能决定胜负；前沿子(挨着空格的子)多了容易给对方留下好棋。
func evaluateHard(me, opp uint64) int {
	empty := ^(me | opp)
	score := 0
	score += 80 * (bits.OnesCount64(me&cornerMask) - bits.OnesCount64(opp&cornerMask))
	for i, corner := range corners {
		if empty&(1<<corner) == 0 {
			continue
		}
		x := uint64(1) << xSquares[i]
		switch {
		case me&x != 0:
			score -= 30
		case opp&x != 0:
			score += 30
		}
		score -= 12 * (bits.OnesCount64(me&cSquares[i]) - bits.OnesCount64(opp&cSquares[i]))
	}
	score += 10 * (bits.OnesCount64(legalMoves(me, opp)) - bits.OnesCount64(legalMoves(opp, me)))
	frontier := neighbors(empty)
	score -= 4 * (bits.OnesCount64(me&frontier) - bits.OnesCount64(opp&frontier))
	score += (weightedSum(me) - weightedSum(opp)) / 4
	return score
}

// finalScore 是终局的分数，从 me 这一方看。
func finalScore(me, opp uint64) int {
	diff := bits.OnesCount64(me) - bits.OnesCount64(opp)
	switch {
	case diff > 0:
		return decidedScore + diff
	case diff < 0:
		return -decidedScore + diff
	}
	return 0
}

type searcher struct {
	ctx      context.Context
	evaluate func(me, opp uint64) int
	nodes    int
	// limit 是最多展开的节点数，0 表示不限。展开得更多时 aborted 为真，搜索中止，算出的分数都不能用。
	limit   int
	aborted bool
}

// negamax 返回 me 这一方(轮到它走)在 depth 步以内能保证的分数。depth 为负表示一直搜到终局。
func (s *searcher) negamax(me, opp uint64, depth int, alpha int, beta int) int {
	if s.ctx.Err() != nil {
		return 0
	}
	s.nodes++
	if s.limit > 0 && s.nodes > s.limit {
		s.aborted = true
		return 0
	}
	moves := legalMoves(me, opp)
	if moves == 0 {
		if legalMoves(opp, me) == 0 {
			return finalScore(me, opp)
		}
		return -s.negamax(opp, me, depth, -beta, -alpha)
	}
	if depth == 0 {
		return s.evaluate(me, opp)
	}
	var order [Squares]int
	count := s.orderMoves(me, opp, moves, depth < 0, &order)
	best := -infinity
	for _, square := range order[:count] {
		flipped := flips(me, opp, square)
		score := -s.negamax(opp&^flipped, me|flipped|1<<square, depth-1, -beta, -alpha)
		if score > best {
			best = score
		}
		alpha = max(alpha, score)
		if alpha >= beta {
			break
		}
	}
	return best
}

// orderMoves 排好搜索顺序：好棋先搜，alpha-beta 才剪得多。中局按位置权重，角最先；算终局时先走让对方可走步数最少的一步。
func (s *searcher) orderMoves(me, opp uint64, moves uint64, exact bool, order *[Squares]int) int {
	var keys [Squares]int
	count := 0
	for moves != 0 {
		square := bits.TrailingZeros64(moves)
		moves &= moves - 1
		key := squareWeights[square]
		if exact {
			flipped := flips(me, opp, square)
			key = -bits.OnesCount64(legalMoves(opp&^flipped, me|flipped|1<<square))
		}
		i := count
		for i > 0 && keys[i-1] < key {
			order[i], keys[i] = order[i-1], keys[i-1]
			i--
		}
		order[i], keys[i] = square, key
		count++
	}
	return count
}

// BestMove 为轮到走的一方选一步。level 为 1 简单、2 普通、3 困难；rng 用来在差不多好的几步里随机挑一步。
func BestMove(g *Game, level int, rng *rand.Rand) (Point, error) {
	return BestMoveContext(context.Background(), g, level, rng)
}

// BestMoveContext 保留中局和终局搜索预算，请求取消后不再降深重搜。
func BestMoveContext(ctx context.Context, g *Game, level int, rng *rand.Rand) (Point, error) {
	if err := ctx.Err(); err != nil {
		return Point{}, err
	}
	if g.Over() {
		return Point{}, ErrIllegalMove
	}
	me, opp := g.discs(g.turn)
	var square int
	switch level {
	case 1:
		square = easyMove(me, opp, rng)
	case 2:
		square = searchMove(ctx, evaluateNormal, me, opp, normalDepth, normalTolerance, rng)
	case 3:
		depth := hardDepth
		if bits.OnesCount64(^(me | opp)) <= hardExactEmpties {
			depth = -1
		}
		square = searchMove(ctx, evaluateHard, me, opp, depth, hardTolerance, rng)
	default:
		return Point{}, ErrInvalidLevel
	}
	if err := ctx.Err(); err != nil {
		return Point{}, err
	}
	return Point{X: square % Size, Y: square / Size}, nil
}

// easyMove 挑翻子最多的一步，翻子数加上随机扰动。
func easyMove(me, opp uint64, rng *rand.Rand) int {
	best, bestSquare := -1, -1
	for moves := legalMoves(me, opp); moves != 0; moves &= moves - 1 {
		square := bits.TrailingZeros64(moves)
		score := bits.OnesCount64(flips(me, opp, square)) + rng.IntN(easyNoise)
		if score > best {
			best, bestSquare = score, square
		}
	}
	return bestSquare
}

// searchMove 用 evaluate 搜 depth 步(为负时搜到终局)，给每一步算出分数，在不比最好一步差 tolerance 以上的几步里随机
// 挑一步。已经算出输赢时不留容差。展开的节点超过 nodeLimit 时这次的分数作废：算终局的改搜 hardDepth 步，其余的少搜
// 两步重来；只搜两步时不设上限，展开的节点不过几百个，总能算完。
func searchMove(ctx context.Context, evaluate func(me, opp uint64) int, me, opp uint64, depth int, tolerance int, rng *rand.Rand) int {
	s := &searcher{ctx: ctx, evaluate: evaluate}
	if depth < 0 || depth > 2 {
		s.limit = nodeLimit
	}
	var order [Squares]int
	count := s.orderMoves(me, opp, legalMoves(me, opp), depth < 0, &order)
	squares := make([]int, 0, count)
	scores := make([]int, 0, count)
	best := -infinity
	for _, square := range order[:count] {
		if ctx.Err() != nil {
			return 0
		}
		// 比最好一步差出容差的着法只需知道它更差，窗口下沿之下的分数不必算准。
		alpha := -infinity
		if best > -infinity {
			alpha = best - tolerance - 1
		}
		flipped := flips(me, opp, square)
		score := -s.negamax(opp&^flipped, me|flipped|1<<square, depth-1, -infinity, -alpha)
		squares = append(squares, square)
		scores = append(scores, score)
		best = max(best, score)
	}
	if s.aborted {
		next := depth - 2
		if depth < 0 {
			next = hardDepth
		}
		return searchMove(ctx, evaluate, me, opp, next, tolerance, rng)
	}
	if depth < 0 || best >= decidedScore-Squares || best <= -decidedScore+Squares {
		tolerance = 0
	}
	picks := make([]int, 0, len(squares))
	for i, square := range squares {
		if scores[i] >= best-tolerance {
			picks = append(picks, square)
		}
	}
	return picks[rng.IntN(len(picks))]
}
