package xiangqi

import (
	"errors"
	"sort"
)

// 电脑棋手：迭代加深的 alpha-beta 搜索，叶子节点接吃子静态搜索(被将军时搜全部应将)，局面评估是子力加棋子位置表。
// 着法排序依次是置换表里的着法、吃子(先吃大子、用小子吃)、杀手着法、历史表。三个难度只在搜索深度与随机程度上不同：
//
//	1 简单：只看一步(加吃子搜索)，在比最好的着法差不到 100 分(约两个过河兵)的着法里随机选，看不到两步以上的战术；
//	2 普通：3 层，在差不到 15 分的着法里随机选；
//	3 困难：4 层，只在分数完全相同的最好着法里随机选。
//
// 被将军时多搜一层(将军延伸)。能杀棋时不随机，总是选最快的杀法；要被杀时选撑得最久的。每次思考最多展开 nodeLimit 个节点，
// 到了就用上一轮迭代加深的结果：节点数只由局面决定，同一局面同样的随机数总是走同样的一步，任何局面都能在 1 秒内走完。

const (
	mateScore = 30000
	// mateBound 以上的分数都是杀棋分：被将死的一方得 -mateScore+步数，离将死越近分数的绝对值越大。
	mateBound = mateScore - 2*maxPly
	infinity  = mateScore + 1
	maxPly    = 64
	// maxQuiesce 是吃子静态搜索最多再走几步。
	maxQuiesce = 8
	// nodeLimit 是一次思考最多展开的节点数。搜索每秒约展开 190 万个节点，4 层的困难难度实测最多用到 16 万个；
	// 40 万个约 0.2 秒，服务器慢几倍也在 1 秒以内。
	nodeLimit = 400_000
	ttBits    = 16
	ttSize    = 1 << ttBits
)

// aiLevel 是一个难度：搜索深度，以及在比最好的着法差不到 margin 分的着法里随机选一步。
type aiLevel struct {
	depth  int
	margin int
}

// aiLevels 按难度 1、2、3 排列，下标 0 不用。
var aiLevels = [...]aiLevel{{}, {depth: 1, margin: 100}, {depth: 3, margin: 15}, {depth: 4, margin: 0}}

// AiLevels 是电脑棋手的难度数。
const AiLevels = len(aiLevels) - 1

var errUnknownLevel = errors.New("xiangqi: unknown ai level")

// pieceSquareTables 是各种棋子在每个位置上的分值(已含子力价值)，按红方的视角书写：第一行是黑方底线 y=9，
// 最后一行是红方底线 y=0。车约 600、炮约 285、马约 270、仕相约 120，兵没过河 30 左右、过河后 45 到 100，
// 帅的分只表示位置好坏(帅不会被吃，不计子力)。黑方棋子按棋盘旋转 180 度后查同一张表。
var pieceSquareTables = [8][Height][Width]int16{
	King: {
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, -12, -10, -12, 0, 0, 0},
		{0, 0, 0, -6, -4, -6, 0, 0, 0},
		{0, 0, 0, 2, 6, 2, 0, 0, 0},
	},
	Advisor: {
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 118, 0, 118, 0, 0, 0},
		{0, 0, 0, 0, 123, 0, 0, 0, 0},
		{0, 0, 0, 120, 0, 120, 0, 0, 0},
	},
	Elephant: {
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 116, 0, 0, 0, 116, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{114, 0, 0, 0, 124, 0, 0, 0, 114},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 120, 0, 0, 0, 120, 0, 0},
	},
	Horse: {
		{262, 266, 270, 268, 262, 268, 270, 266, 262},
		{266, 276, 282, 280, 276, 280, 282, 276, 266},
		{270, 278, 286, 292, 282, 292, 286, 278, 270},
		{270, 282, 286, 290, 288, 290, 286, 282, 270},
		{266, 276, 282, 286, 286, 286, 282, 276, 266},
		{264, 274, 280, 284, 284, 284, 280, 274, 264},
		{262, 270, 276, 278, 276, 278, 276, 270, 262},
		{258, 266, 272, 272, 266, 272, 272, 266, 258},
		{254, 260, 264, 266, 256, 266, 264, 260, 254},
		{250, 262, 256, 258, 252, 258, 256, 262, 250},
	},
	Rook: {
		{606, 608, 606, 612, 612, 612, 606, 608, 606},
		{606, 612, 610, 616, 620, 616, 610, 612, 606},
		{604, 610, 608, 614, 616, 614, 608, 610, 604},
		{606, 612, 610, 614, 616, 614, 610, 612, 606},
		{608, 614, 612, 614, 616, 614, 612, 614, 608},
		{606, 612, 610, 612, 614, 612, 610, 612, 606},
		{602, 608, 606, 608, 610, 608, 606, 608, 602},
		{598, 606, 602, 606, 606, 606, 602, 606, 598},
		{600, 606, 604, 606, 600, 606, 604, 606, 600},
		{594, 604, 602, 606, 600, 606, 602, 604, 594},
	},
	Cannon: {
		{290, 290, 286, 282, 280, 282, 286, 290, 290},
		{286, 286, 284, 280, 278, 280, 284, 286, 286},
		{284, 284, 284, 286, 290, 286, 284, 284, 284},
		{282, 284, 284, 284, 288, 284, 284, 284, 282},
		{282, 282, 282, 284, 288, 284, 282, 282, 282},
		{282, 284, 284, 284, 288, 284, 284, 284, 282},
		{284, 284, 284, 284, 290, 284, 284, 284, 284},
		{286, 288, 290, 288, 294, 288, 290, 288, 286},
		{286, 286, 288, 288, 290, 288, 288, 286, 286},
		{286, 286, 288, 290, 290, 290, 288, 286, 286},
	},
	Pawn: {
		{35, 40, 45, 50, 55, 50, 45, 40, 35},
		{55, 70, 85, 95, 100, 95, 85, 70, 55},
		{55, 70, 80, 90, 95, 90, 80, 70, 55},
		{50, 60, 70, 78, 82, 78, 70, 60, 50},
		{45, 50, 55, 60, 65, 60, 55, 50, 45},
		{32, 0, 34, 0, 38, 0, 34, 0, 32},
		{30, 0, 31, 0, 33, 0, 31, 0, 30},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
	},
}

// captureOrder 是吃子排序用的棋子大小：先吃大子，同样的子先用小子去吃。
var captureOrder = [8]int32{King: 7, Rook: 6, Cannon: 4, Horse: 4, Advisor: 2, Elephant: 2, Pawn: 1}

// pieceSquare 与 zobristPiece 按棋子(含颜色)与格子查表，在 init 里由上面的表生成。
var (
	pieceSquare  [16][Squares]int16
	zobristPiece [16][Squares]uint64
	zobristTurn  uint64
)

func init() {
	for kind := King; kind <= Pawn; kind++ {
		for sq := 0; sq < Squares; sq++ {
			x, y := SquareXY(sq)
			pieceSquare[makePiece(Red, kind)][sq] = pieceSquareTables[kind][Height-1-y][x]
			pieceSquare[makePiece(Black, kind)][sq] = pieceSquareTables[kind][y][Width-1-x]
		}
	}
	// 置换表的键用 splitmix64 从固定种子生成：只需要分布均匀，不需要不可预测。
	seed := uint64(0x5851F42D4C957F2D)
	keys := make([]uint64, len(zobristPiece)*Squares+1)
	for i := range keys {
		seed += 0x9E3779B97F4A7C15
		z := seed
		z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
		z = (z ^ z>>27) * 0x94D049BB133111EB
		keys[i] = z ^ z>>31
	}
	for piece := range zobristPiece {
		copy(zobristPiece[piece][:], keys[piece*Squares:])
	}
	zobristTurn = keys[len(keys)-1]
}

// evaluate 从轮到走的一方看局面的分数。
func (p *Position) evaluate() int {
	return p.score[p.turn] - p.score[p.turn.Other()]
}

const (
	ttExact uint8 = iota + 1
	ttLower
	ttUpper
)

// ttEntry 是置换表的一项：局面的键、这个局面上找到的最好着法，以及搜到的深度、分数与分数的性质(准确值、下界或上界)。
type ttEntry struct {
	key   uint64
	score int32
	move  uint16
	depth int8
	flag  uint8
}

func packMove(m Move) uint16 {
	return uint16(m.From)<<7 | uint16(m.To)
}

func unpackMove(packed uint16) Move {
	return Move{From: int(packed >> 7), To: int(packed & 0x7F)}
}

// searcher 是一次思考的全部状态。quiet[ply] 是走到这一层时连续没有吃子的步数，用来在搜索里也按和棋规则判和。
type searcher struct {
	pos     *Position
	plies   int
	nodes   int
	limit   int
	aborted bool
	quiet   [maxPly + 2]int
	moves   [maxPly + 2][]Move
	orders  [maxPly + 2][]int32
	killers [maxPly + 2][2]Move
	history [Squares][Squares]int32
	tt      []ttEntry
}

// rootMove 是根节点的一步及其分数。
type rootMove struct {
	move  Move
	score int
}

// BestMove 为轮到走的一方选一步。level 1 简单、2 普通、3 困难；pick(n) 返回 [0,n) 里的随机数，用来在几步差不多好的
// 着法里随机选一步，电脑就不会每局都下得一模一样。
func BestMove(g *Game, level int, pick func(n int) int) (Move, error) {
	if level < 1 || level > AiLevels {
		return Move{}, errUnknownLevel
	}
	if g.Over() {
		return Move{}, ErrGameOver
	}
	pos, err := g.Position()
	if err != nil {
		return Move{}, err
	}
	legal := pos.LegalMoves()
	if len(legal) == 0 {
		return Move{}, ErrGameOver
	}
	s := &searcher{pos: pos, plies: g.Plies, limit: nodeLimit, tt: make([]ttEntry, ttSize)}
	s.quiet[0] = g.Quiet
	root := make([]rootMove, len(legal))
	for i, m := range legal {
		root[i] = rootMove{move: m, score: int(s.orderScore(0, m, Move{}, false))}
	}
	sort.SliceStable(root, func(i, j int) bool { return root[i].score > root[j].score })

	// 迭代加深：每一轮按上一轮的分数排好根节点，节点数用完的那一轮作废。连第一轮都没搜完时走排序最前的一步。
	params := aiLevels[level]
	var completed []rootMove
	for depth := 1; depth <= params.depth; depth++ {
		if !s.searchRoot(root, depth, params.margin) {
			break
		}
		sort.SliceStable(root, func(i, j int) bool { return root[i].score > root[j].score })
		completed = append(completed[:0], root...)
	}
	if len(completed) == 0 {
		return root[0].move, nil
	}
	best, margin := completed[0].score, params.margin
	if best >= mateBound || best <= -mateBound {
		margin = 0
	}
	candidates := 1
	for candidates < len(completed) && completed[candidates].score >= best-margin {
		candidates++
	}
	choice := pick(candidates)
	if choice < 0 || choice >= candidates {
		choice = 0
	}
	return completed[choice].move, nil
}

// searchRoot 把根节点的每一步搜到 depth 层并记下分数。分数不比目前最好的差 margin 分以上的着法要得到准确的分数，
// 更差的只需知道它不够好。节点数用完时返回 false，这一轮的分数作废。
func (s *searcher) searchRoot(root []rootMove, depth int, margin int) bool {
	best := -infinity
	for i := range root {
		lower := -infinity
		if best > -infinity {
			lower = max(best-margin-1, -infinity)
		}
		m := root[i].move
		captured := s.pos.makeMove(m)
		s.quiet[1] = s.quiet[0] + 1
		if captured != Empty {
			s.quiet[1] = 0
		}
		score := -s.alphaBeta(depth-1, 1, -infinity, -lower)
		s.pos.unmakeMove(m, captured)
		if s.aborted {
			return false
		}
		root[i].score = score
		best = max(best, score)
	}
	return true
}

// orderScore 是着法排序的分数：置换表的着法最先，然后是吃子、杀手着法，其余按历史表。
func (s *searcher) orderScore(ply int, m Move, ttMove Move, hasTT bool) int32 {
	victim := s.pos.board[m.To]
	switch {
	case hasTT && m == ttMove:
		return 1 << 30
	case victim != Empty:
		return 1<<20 + captureOrder[victim.Kind()]*16 - captureOrder[s.pos.board[m.From].Kind()]
	case m == s.killers[ply][0]:
		return 1 << 19
	case m == s.killers[ply][1]:
		return 1 << 18
	}
	return s.history[m.From][m.To]
}

// nextMove 把 moves[i:] 里排序分数最高的一步换到第 i 个：剪枝早的节点不必把所有着法排好。
func nextMove(moves []Move, orders []int32, i int) Move {
	best := i
	for j := i + 1; j < len(moves); j++ {
		if orders[j] > orders[best] {
			best = j
		}
	}
	moves[i], moves[best] = moves[best], moves[i]
	orders[i], orders[best] = orders[best], orders[i]
	return moves[i]
}

func (s *searcher) spend() bool {
	if s.nodes >= s.limit {
		s.aborted = true
		return false
	}
	s.nodes++
	return true
}

func (s *searcher) alphaBeta(depth int, ply int, alpha int, beta int) int {
	if !s.spend() {
		return 0
	}
	pos := s.pos
	if s.quiet[ply] >= MaxQuietPlies || s.plies+ply >= MaxPlies {
		// 到了和棋的步数；这时无子可走仍按输棋算，和 Game.Play 的判定一致。
		if !pos.hasLegalMove() {
			return -mateScore + ply
		}
		return 0
	}
	if ply >= maxPly {
		return pos.evaluate()
	}
	if pos.InCheck() {
		depth++
	}
	if depth <= 0 {
		return s.quiesce(ply, alpha, beta, 0)
	}

	entry := &s.tt[pos.hash&(ttSize-1)]
	var ttMove Move
	hasTT := entry.key == pos.hash && entry.flag != 0
	if hasTT {
		ttMove = unpackMove(entry.move)
		if int(entry.depth) >= depth {
			score := int(entry.score)
			if score >= mateBound {
				score -= ply
			} else if score <= -mateBound {
				score += ply
			}
			switch {
			case entry.flag == ttExact,
				entry.flag == ttLower && score >= beta,
				entry.flag == ttUpper && score <= alpha:
				return score
			}
		}
	}

	moves := pos.generate(s.moves[ply][:0], false)
	s.moves[ply] = moves
	orders := s.orders[ply][:0]
	for _, m := range moves {
		orders = append(orders, s.orderScore(ply, m, ttMove, hasTT))
	}
	s.orders[ply] = orders

	originalAlpha := alpha
	best, bestMove, legal := -infinity, Move{}, 0
	for i := range moves {
		m := nextMove(moves, orders, i)
		us := pos.turn
		captured := pos.makeMove(m)
		if pos.attacked(pos.kings[us], us.Other()) {
			pos.unmakeMove(m, captured)
			continue
		}
		legal++
		s.quiet[ply+1] = s.quiet[ply] + 1
		if captured != Empty {
			s.quiet[ply+1] = 0
		}
		score := -s.alphaBeta(depth-1, ply+1, -beta, -alpha)
		pos.unmakeMove(m, captured)
		if s.aborted {
			return 0
		}
		if score <= best {
			continue
		}
		best, bestMove = score, m
		if score <= alpha {
			continue
		}
		alpha = score
		if alpha >= beta {
			if captured == Empty {
				if s.killers[ply][0] != m {
					s.killers[ply][1], s.killers[ply][0] = s.killers[ply][0], m
				}
				s.history[m.From][m.To] = min(s.history[m.From][m.To]+int32(depth*depth), 1<<17)
			}
			break
		}
	}
	if legal == 0 {
		return -mateScore + ply
	}

	stored := best
	if stored >= mateBound {
		stored += ply
	} else if stored <= -mateBound {
		stored -= ply
	}
	flag := ttExact
	if best <= originalAlpha {
		flag = ttUpper
	} else if best >= beta {
		flag = ttLower
	}
	*entry = ttEntry{key: pos.hash, score: int32(stored), move: packMove(bestMove), depth: int8(depth), flag: flag}
	return best
}

// quiesce 是吃子静态搜索：没被将军时可以不走(按当前局面评分)或只看吃子，被将军时要搜全部应将，应不了就是被将死。
func (s *searcher) quiesce(ply int, alpha int, beta int, depth int) int {
	if !s.spend() {
		return 0
	}
	pos := s.pos
	inCheck := pos.InCheck()
	best := -mateScore + ply
	if !inCheck || depth >= maxQuiesce || ply >= maxPly {
		stand := pos.evaluate()
		if !inCheck && stand >= beta {
			return stand
		}
		if depth >= maxQuiesce || ply >= maxPly {
			return stand
		}
		best = stand
		alpha = max(alpha, stand)
	}

	moves := pos.generate(s.moves[ply][:0], !inCheck)
	s.moves[ply] = moves
	orders := s.orders[ply][:0]
	for _, m := range moves {
		orders = append(orders, s.orderScore(ply, m, Move{}, false))
	}
	s.orders[ply] = orders

	legal := 0
	for i := range moves {
		m := nextMove(moves, orders, i)
		us := pos.turn
		captured := pos.makeMove(m)
		if pos.attacked(pos.kings[us], us.Other()) {
			pos.unmakeMove(m, captured)
			continue
		}
		legal++
		score := -s.quiesce(ply+1, -beta, -alpha, depth+1)
		pos.unmakeMove(m, captured)
		if s.aborted {
			return 0
		}
		if score > best {
			best = score
			if score > alpha {
				alpha = score
				if alpha >= beta {
					break
				}
			}
		}
	}
	if inCheck && legal == 0 {
		return -mateScore + ply
	}
	return best
}
