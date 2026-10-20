package xiangqi

import (
	"context"
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
// 被将军时多搜一层(将军延伸)。能杀棋时不随机，总是选最快的杀法；要被杀时选撑得最久的。搜索中走回了这局里(最近一次吃子
// 之后)或这条变化里出现过的局面，就当作会一直重复下去，按判局的规则给分：长将的一方输，否则和棋。所以电脑不会长将，占优
// 时也不会来回走成和棋。每次思考最多展开 nodeLimit 个节点，
// 到了就用上一轮迭代加深的结果：节点数只由局面决定，同一局面同样的随机数总是走同样的一步，请求也可以通过上下文取消。

const (
	mateScore = 30000
	// mateBound 以上的分数都是杀棋分：被将死的一方得 -mateScore+步数，离将死越近分数的绝对值越大。
	mateBound = mateScore - 2*maxPly
	// perpetualScore 是重复局面里对方长将、判对方输时的分数：高于任何局面评估，低于杀棋。
	perpetualScore = mateBound - 1
	infinity       = mateScore + 1
	maxPly         = 64
	// maxQuiesce 是吃子静态搜索最多再走几步。
	maxQuiesce = 8
	// nodeLimit 是一次思考最多展开的节点数；实际耗时还取决于局面与服务器速度。
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

// trailEntry 是走过的一个局面：键，以及轮到走的一方是否正被将军(走到这里的一步是不是将军)。
type trailEntry struct {
	hash  uint64
	check bool
}

// searcher 是一次思考的全部状态。quiet[ply] 是走到这一层时连续没有吃子的步数，用来在搜索里也按和棋规则判和。
// trail 是这局里最近一次吃子之后走出的局面，接着是搜索中这条变化上的局面：根局面在 trail[root]，第 ply 层在 trail[root+ply]。
type searcher struct {
	ctx     context.Context
	pos     *Position
	plies   int
	root    int
	trail   []trailEntry
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
	return BestMoveContext(context.Background(), g, level, pick)
}

// BestMoveContext 与 BestMove 一样选棋，请求取消时终止搜索并返回上下文错误，不返回尚未完成的着法。
func BestMoveContext(ctx context.Context, g *Game, level int, pick func(n int) int) (Move, error) {
	if err := ctx.Err(); err != nil {
		return Move{}, err
	}
	if level < 1 || level > AiLevels {
		return Move{}, errUnknownLevel
	}
	found, err := searchRoots(ctx, g, aiLevels[level])
	if err != nil {
		return Move{}, err
	}
	if len(found.completed) == 0 {
		return found.fallback, nil
	}
	params := aiLevels[level]
	best, margin := found.completed[0].score, params.margin
	if best >= mateBound || best <= -mateBound {
		margin = 0
	}
	candidates := 1
	for candidates < len(found.completed) && found.completed[candidates].score >= best-margin {
		candidates++
	}
	choice := pick(candidates)
	if err := ctx.Err(); err != nil {
		return Move{}, err
	}
	if choice < 0 || choice >= candidates {
		choice = 0
	}
	return found.completed[choice].move, nil
}

// Analysis 是一次局面分析的结论，都从轮到走的一方看：推荐的着法、评分(正数表示这一方占优，一个马约 270 分、一个车约
// 600 分)、杀棋步数(正数是自己多少步内将死对方，负数是被将死，0 表示没看到杀棋)，以及按分数排好的前几条着法。
type Analysis struct {
	Move  Move   `json:"move"`
	Score int    `json:"score"`
	Mate  int    `json:"mate,omitempty"`
	Lines []Line `json:"lines,omitempty"`
}

// Line 是分析里的一条推荐着法及其评分与杀棋步数。
type Line struct {
	Move  Move `json:"move"`
	Score int  `json:"score"`
	Mate  int  `json:"mate,omitempty"`
}

const (
	// analyseLines 是分析最多列出几条着法。
	analyseLines = 3
	// analyseMargin 是分析时根节点着法的准确度窗口：比最好的着法差不到这么多分的着法都搜出准确分数，更差的只够知道
	// 它不够好。提示只需要前几条，不必为每一步都花满搜索。
	analyseMargin = 200
)

// Analyse 为轮到走的一方分析局面，用困难难度的搜索给出推荐着法与评分。它不看随机数，同一个局面总是同样的结论；对局
// 已经结束或没有合法着法时返回错误。
func Analyse(g *Game) (Analysis, error) {
	return AnalyseContext(context.Background(), g)
}

// AnalyseContext 与 Analyse 一样分析局面，请求取消时返回上下文错误，不把上一轮结果当作成功提示。
func AnalyseContext(ctx context.Context, g *Game) (Analysis, error) {
	params := aiLevels[AiLevels]
	found, err := searchRoots(ctx, g, aiLevel{depth: params.depth, margin: analyseMargin})
	if err != nil {
		return Analysis{}, err
	}
	if len(found.completed) == 0 {
		line := Line{Move: found.fallback}
		return Analysis{Move: line.Move, Lines: []Line{line}}, nil
	}
	analysis := Analysis{}
	for i := 0; i < len(found.completed) && i < analyseLines; i++ {
		root := found.completed[i]
		analysis.Lines = append(analysis.Lines, Line{Move: root.move, Score: root.score, Mate: matePlies(root.score)})
	}
	analysis.Move, analysis.Score, analysis.Mate = analysis.Lines[0].Move, analysis.Lines[0].Score, analysis.Lines[0].Mate
	return analysis, nil
}

// matePlies 把杀棋分换成离将死的步数：正数是这一方多少步内将死对方，负数是自己多少步内被将死，0 表示不是杀棋分。
// 叶子节点的杀棋分带着层数(-mateScore+ply)，反过来就是还有几步。
func matePlies(score int) int {
	switch {
	case score >= mateBound:
		return mateScore - score
	case score <= -mateBound:
		return -(mateScore + score)
	}
	return 0
}

// rootSearch 是一轮迭代加深之后的根节点着法：completed 是最后一次完整搜完的那一轮的分数(按分数从高到低)，节点数用完的
// 那一轮作废；连第一轮都没搜完时 completed 为空，fallback 是排序最前的一步。
type rootSearch struct {
	completed []rootMove
	fallback  Move
}

// searchRoots 建立一次搜索并逐层加深到 params.depth：根节点的全部合法着法先按排序分数排好，每轮搜完再按分数重排。
// 搜索的节点数上限由参数决定，同一局面同样的参数总是搜出同样的结果。
func searchRoots(ctx context.Context, g *Game, params aiLevel) (rootSearch, error) {
	if err := ctx.Err(); err != nil {
		return rootSearch{}, err
	}
	if g.Over() {
		return rootSearch{}, ErrGameOver
	}
	pos, err := g.Position()
	if err != nil {
		return rootSearch{}, err
	}
	legal := pos.LegalMoves()
	if len(legal) == 0 {
		return rootSearch{}, ErrGameOver
	}
	s := &searcher{ctx: ctx, pos: pos, plies: g.Plies, limit: nodeLimit, tt: make([]ttEntry, ttSize)}
	s.quiet[0] = g.Quiet
	earlier := g.positionsSinceCapture(pos)
	s.root = len(earlier)
	s.trail = make([]trailEntry, s.root+maxPly+2)
	for i := range earlier {
		back := &earlier[i]
		s.trail[s.root-1-i] = trailEntry{hash: back.hash, check: back.InCheck()}
	}
	s.trail[s.root] = trailEntry{hash: pos.hash, check: pos.InCheck()}
	root := make([]rootMove, len(legal))
	for i, m := range legal {
		root[i] = rootMove{move: m, score: int(s.orderScore(0, m, Move{}, false))}
	}
	sort.SliceStable(root, func(i, j int) bool { return root[i].score > root[j].score })
	found := rootSearch{fallback: root[0].move}
	for depth := 1; depth <= params.depth; depth++ {
		if !s.searchRoot(root, depth, params.margin) {
			break
		}
		sort.SliceStable(root, func(i, j int) bool { return root[i].score > root[j].score })
		found.completed = append(found.completed[:0], root...)
	}
	if err := ctx.Err(); err != nil {
		return rootSearch{}, err
	}
	return found, nil
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
	// 每 256 个节点查看一次取消，避免给每个节点增加同步开销；节点预算仍按原来的固定上限计算。
	if s.nodes&255 == 0 && s.ctx.Err() != nil {
		s.aborted = true
		return false
	}
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
	inCheck := pos.InCheck()
	s.trail[s.root+ply] = trailEntry{hash: pos.hash, check: inCheck}
	if score, repeated := s.repetition(ply); repeated {
		return score
	}
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
	if inCheck {
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

// repetition 看第 ply 层的局面是不是走回了最近一次吃子之后出现过的局面(同一方走，所以至少隔 4 步)。重复时从轮到走的一方
// 看过去：这一段里对方每步都在将军、自己不是，对方长将判负；反过来自己判负；否则和棋。
func (s *searcher) repetition(ply int) (int, bool) {
	n := s.root + ply
	oldest := max(n-s.quiet[ply], 0)
	for k := n - 4; k >= oldest; k -= 2 {
		if s.trail[k].hash != s.trail[n].hash {
			continue
		}
		ours, theirs := true, true
		for j := k + 1; j <= n; j++ {
			// 第 j 个局面是走到它的一方走出来的：和第 n 个隔偶数步的是对方走的。
			if (n-j)%2 == 0 {
				theirs = theirs && s.trail[j].check
			} else {
				ours = ours && s.trail[j].check
			}
		}
		switch {
		case theirs && !ours:
			return perpetualScore, true
		case ours && !theirs:
			return -perpetualScore, true
		}
		return 0, true
	}
	return 0, false
}

// quiesce 是吃子静态搜索：没被将军时可以不走(按当前局面评分)或只看吃子，被将军时要搜全部应将，应不了就是被将死。
func (s *searcher) quiesce(ply int, alpha int, beta int, depth int) int {
	if !s.spend() {
		return 0
	}
	pos := s.pos
	inCheck := pos.InCheck()
	// 象棋困毙和将死一样判负，必须先于静态估值与 beta 截断判断；没有吃子着法不代表还能走安静着。
	if !pos.hasLegalMove() {
		return -mateScore + ply
	}
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

	for i := range moves {
		m := nextMove(moves, orders, i)
		us := pos.turn
		captured := pos.makeMove(m)
		if pos.attacked(pos.kings[us], us.Other()) {
			pos.unmakeMove(m, captured)
			continue
		}
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
	return best
}
