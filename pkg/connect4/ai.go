package connect4

import (
	"errors"
	"math/bits"
	"math/rand/v2"
)

// 四子棋 AI：negamax 形式的 minimax + alpha-beta。每个局面先看"一步就赢"(直接取胜)和"对手下一步能赢的格子"(只有一个就
// 必须堵，两个就已经输了；也不能投在对手赢棋格子的正下方，否则对手接着就赢)，再按中路优先、能多造出几个自己的赢棋格子的
// 顺序搜索。叶子局面按每条 4 格线段里只有一方棋子的数量打分，加上中路棋子与赢棋格子所在行的奇偶(先手在奇数行、后手在
// 偶数行的威胁更容易兑现)。
//
// 三档难度：
//   - 1 简单：只看自己这一步(能赢就赢)，局面分加上随机扰动，常会漏看对手的威胁；
//   - 2 普通：搜 5 步；
//   - 3 困难：迭代加深搜 9 步，用置换表排序与剪枝；剩下 20 个空格以内直接搜到终局。每一步在几十毫秒以内。
//
// 普通与困难在得分与最好一步相差不超过容差的几步里随机挑一步(能赢时挑最快赢的)，同一局面不总走同一步，玩家背不下
// 一条稳赢的棋谱。

var ErrInvalidLevel = errors.New("invalid connect4 ai level")

const (
	// winScore 是赢棋的分数，越快赢分数越高：第 ply 步赢得 winScore - ply。局面分远小于它。
	winScore = 1_000_000
	infinity = 2 * winScore
	// decidedScore 以上是已经算出输赢的分数。
	decidedScore = winScore - Cells - 1

	easyNoise       = 24
	normalDepth     = 5
	normalTolerance = 3
	hardDepth       = 9
	hardTolerance   = 1
	// hardExactEmpties 以内的空格，困难难度直接搜到终局。
	hardExactEmpties = 20

	windowWeight1 = 1
	windowWeight2 = 4
	windowWeight3 = 16
	centerWeight  = 3
	parityWeight  = 8

	ttBits = 16
)

// 位棋盘：每列占 7 位(6 行加 1 位哨兵)，第 col 列从下数第 h 格是第 col*7+h 位。
const colStride = Rows + 1

var (
	bottomMask = func() uint64 {
		var mask uint64
		for col := 0; col < Cols; col++ {
			mask |= 1 << (col * colStride)
		}
		return mask
	}()
	boardMask = bottomMask * (1<<Rows - 1)
	// oddRows 是从下数第 1、3、5 行，evenRows 是第 2、4、6 行。
	oddRows  = bottomMask * 0b010101
	evenRows = bottomMask * 0b101010
	// windows 是棋盘上所有 69 条能连成 4 子的线段。
	windows = buildWindows()
	// columnOrder 是中路优先的着法顺序：中间的列参与的连线最多。
	columnOrder = [Cols]int{3, 2, 4, 1, 5, 0, 6}
)

func buildWindows() []uint64 {
	var list []uint64
	for col := 0; col < Cols; col++ {
		for h := 0; h < Rows; h++ {
			for _, d := range lineDirections {
				endCol, endH := col+3*d[0], h+3*d[1]
				if endCol >= Cols || endH < 0 || endH >= Rows {
					continue
				}
				var window uint64
				for k := 0; k < 4; k++ {
					window |= 1 << ((col+k*d[0])*colStride + h + k*d[1])
				}
				list = append(list, window)
			}
		}
	}
	return list
}

func columnMask(col int) uint64 {
	return (1<<Rows - 1) << (col * colStride)
}

// position 是搜索用的局面：cur 是轮到走的一方的棋子，mask 是所有棋子。cur+mask 唯一确定一个局面，用作置换表的键。
type position struct {
	cur, mask uint64
	moves     int
}

func positionOf(g *Game) position {
	p := position{moves: len(g.moves)}
	turn := g.Turn()
	for col := 0; col < Cols; col++ {
		for h := 0; h < g.height[col]; h++ {
			bit := uint64(1) << (col*colStride + h)
			p.mask |= bit
			if g.cells[col][h] == turn {
				p.cur |= bit
			}
		}
	}
	return p
}

// possible 是每一列下一子会落到的格子。
func (p position) possible() uint64 {
	return (p.mask + bottomMask) & boardMask
}

// play 投下 move(一个格子的位)，轮到对方走。
func (p *position) play(move uint64) {
	p.cur ^= p.mask
	p.mask |= move
	p.moves++
}

// winningCells 返回 stones 这一方再下一子就能连成 4 子的空格(不管那一格现在能不能下)。
func winningCells(stones uint64, mask uint64) uint64 {
	cells := (stones << 1) & (stones << 2) & (stones << 3)
	for _, shift := range [3]uint{colStride, colStride - 1, colStride + 1} {
		pair := (stones << shift) & (stones << (2 * shift))
		cells |= pair & (stones << (3 * shift))
		cells |= pair & (stones >> shift)
		pair = (stones >> shift) & (stones >> (2 * shift))
		cells |= pair & (stones << shift)
		cells |= pair & (stones >> (3 * shift))
	}
	return cells & (boardMask ^ mask)
}

// evaluate 从轮到走的一方看局面的好坏：没有对方棋子的线段按自己的子数加分，反之减分；中路的棋子加分；赢棋格子在
// 自己容易兑现的奇偶行上再加分。
func evaluate(p position) int {
	me, opp := p.cur, p.cur^p.mask
	score := 0
	for _, window := range windows {
		mine, theirs := bits.OnesCount64(window&me), bits.OnesCount64(window&opp)
		switch {
		case theirs == 0 && mine > 0:
			score += [4]int{0, windowWeight1, windowWeight2, windowWeight3}[mine]
		case mine == 0 && theirs > 0:
			score -= [4]int{0, windowWeight1, windowWeight2, windowWeight3}[theirs]
		}
	}
	center := columnMask(Cols / 2)
	score += centerWeight * (bits.OnesCount64(me&center) - bits.OnesCount64(opp&center))
	myRows, theirRows := oddRows, evenRows
	if p.moves%2 == 1 {
		myRows, theirRows = evenRows, oddRows
	}
	score += parityWeight * (bits.OnesCount64(winningCells(me, p.mask)&myRows) - bits.OnesCount64(winningCells(opp, p.mask)&theirRows))
	return score
}

const (
	boundExact = iota
	boundLower
	boundUpper
)

type ttEntry struct {
	key   uint64
	score int32
	depth int8
	bound int8
	// move 是这个局面最好一步的列号加一，0 表示没有。
	move int8
}

type searcher struct {
	table []ttEntry
	nodes int
}

func newSearcher() *searcher {
	return &searcher{table: make([]ttEntry, 1<<ttBits)}
}

// negamax 返回轮到走的一方在 depth 步以内能保证的分数。ply 是离搜索起点的步数：同一个局面不论从哪条路走到，棋子数都
// 一样，ply 也一样，所以置换表里的赢棋分数不用换算。
func (s *searcher) negamax(p position, depth int, alpha int, beta int, ply int) int {
	s.nodes++
	possible := p.possible()
	if winningCells(p.cur, p.mask)&possible != 0 {
		return winScore - ply - 1
	}
	if p.moves == Cells {
		return 0
	}
	if depth == 0 {
		return evaluate(p)
	}
	threats := winningCells(p.cur^p.mask, p.mask)
	moves := possible
	if forced := possible & threats; forced != 0 {
		if forced&(forced-1) != 0 {
			return -(winScore - ply - 2)
		}
		moves = forced
	}
	moves &^= threats >> 1
	if moves == 0 {
		return -(winScore - ply - 2)
	}

	key := p.cur + p.mask
	entry := &s.table[(key*0x9E3779B97F4A7C15)>>(64-ttBits)]
	hashMove := -1
	if entry.key == key {
		hashMove = int(entry.move) - 1
		if int(entry.depth) >= depth {
			score := int(entry.score)
			switch entry.bound {
			case boundExact:
				return score
			case boundLower:
				alpha = max(alpha, score)
			case boundUpper:
				beta = min(beta, score)
			}
			if alpha >= beta {
				return score
			}
		}
	}

	var order [Cols]int
	count := orderMoves(p, moves, hashMove, &order)
	original := alpha
	best, bestCol := -infinity, -1
	for _, col := range order[:count] {
		child := p
		child.play(moves & columnMask(col))
		score := -s.negamax(child, depth-1, -beta, -alpha, ply+1)
		if score > best {
			best, bestCol = score, col
		}
		alpha = max(alpha, score)
		if alpha >= beta {
			break
		}
	}
	bound := int8(boundExact)
	switch {
	case best <= original:
		bound = boundUpper
	case best >= beta:
		bound = boundLower
	}
	*entry = ttEntry{key: key, score: int32(best), depth: int8(depth), bound: bound, move: int8(bestCol + 1)}
	return best
}

// orderMoves 排好 moves 里各列的搜索顺序：置换表记下的最好一步最先，其余按走完之后自己的赢棋格子数从多到少，
// 一样多时中路优先。
func orderMoves(p position, moves uint64, hashMove int, order *[Cols]int) int {
	var keys [Cols]int
	count := 0
	for _, col := range columnOrder {
		move := moves & columnMask(col)
		if move == 0 {
			continue
		}
		key := bits.OnesCount64(winningCells(p.cur|move, p.mask|move))
		if col == hashMove {
			key = 1 << 10
		}
		i := count
		for i > 0 && keys[i-1] < key {
			order[i], keys[i] = order[i-1], keys[i-1]
			i--
		}
		order[i], keys[i] = col, key
		count++
	}
	return count
}

// BestMove 为轮到走的一方选一列。level 为 1 简单、2 普通、3 困难；rng 用来在差不多好的几步里随机挑一步。
func BestMove(g *Game, level int, rng *rand.Rand) (int, error) {
	if g.Over() {
		return 0, ErrIllegalMove
	}
	p := positionOf(g)
	if wins := winningCells(p.cur, p.mask) & p.possible(); wins != 0 {
		return bits.TrailingZeros64(wins) / colStride, nil
	}
	switch level {
	case 1:
		return easyMove(p, rng), nil
	case 2:
		return searchMove(p, normalDepth, normalTolerance, rng), nil
	case 3:
		depth := hardDepth
		if Cells-p.moves <= hardExactEmpties {
			depth = Cells - p.moves
		}
		return searchMove(p, depth, hardTolerance, rng), nil
	}
	return 0, ErrInvalidLevel
}

// easyMove 只看自己这一步：走完之后的局面分加上随机扰动，挑最高的一列。
func easyMove(p position, rng *rand.Rand) int {
	possible := p.possible()
	best, bestCol := -infinity, -1
	for _, col := range columnOrder {
		move := possible & columnMask(col)
		if move == 0 {
			continue
		}
		child := p
		child.play(move)
		score := -evaluate(child) + rng.IntN(easyNoise)
		if score > best {
			best, bestCol = score, col
		}
	}
	return bestCol
}

// searchMove 搜 depth 步，给根局面的每一步算出分数，在不比最好一步差 tolerance 以上的几步里随机挑一步。已经算出输赢时
// 不留容差：能赢就挑最快赢的，要输就挑拖得最久的。
func searchMove(p position, depth int, tolerance int, rng *rand.Rand) int {
	s := newSearcher()
	// 迭代加深：浅层的结果存进置换表，给深一层的搜索排好着法顺序。
	for d := 1; d < depth; d++ {
		s.negamax(p, d, -infinity, infinity, 0)
	}
	possible := p.possible()
	cols := make([]int, 0, Cols)
	scores := make([]int, 0, Cols)
	best := -infinity
	for _, col := range columnOrder {
		move := possible & columnMask(col)
		if move == 0 {
			continue
		}
		child := p
		child.play(move)
		// 比最好一步差出容差的着法只需知道它更差，窗口下沿之下的分数不必算准。
		alpha := -infinity
		if best > -infinity {
			alpha = best - tolerance - 1
		}
		score := -s.negamax(child, depth-1, -infinity, -alpha, 1)
		cols = append(cols, col)
		scores = append(scores, score)
		best = max(best, score)
	}
	if best >= decidedScore || best <= -decidedScore {
		tolerance = 0
	}
	picks := make([]int, 0, len(cols))
	for i, col := range cols {
		if scores[i] >= best-tolerance {
			picks = append(picks, col)
		}
	}
	return picks[rng.IntN(len(picks))]
}
