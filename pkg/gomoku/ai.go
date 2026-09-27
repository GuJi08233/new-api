package gomoku

import (
	"errors"
	"math/rand/v2"
	"slices"
)

// 五子棋 AI 按棋型评分。一格在一个方向上与同色棋子组成的形状叫棋型：活四(两处能成五，对方堵不住)、冲四(只有一处能成五)、
// 活三(再下一子成活四)、眠三(再下一子成冲四)、活二、眠二……棋型只看这个方向上前后各 4 格，所有情形预先算成一张表。
// 一个空格对某一方的分数，是这一方在这里落子后四个方向的棋型合起来的分数：成五最高，活四或双冲四(下一步必成五)其次，
// 冲四加活三、双活三再次，其余按棋型相加。
//
// 三档难度：
//   - 1 简单：能成五就成五，否则在攻守合计分数最高的三格里随机挑一格，常会漏看对方的冲四、活三；
//   - 2 普通：只看一步的棋型评分：能赢必赢，必堵对方的冲四与活三，自己能走出活四、冲四活三就走，否则走攻守合计最高的一格；
//   - 3 困难：先找自己的连续冲四取胜(VCF)，再用 alpha-beta 搜 4 步(候选点限制在已有棋子两格以内、按攻守分数只取前几名；
//     必须堵的冲四不耗深度，搜到底时对方还有活三就把应对走完再估值)，最后排除走完之后对方能连续冲四取胜的着法。
//     叶子局面按双方每颗棋子参与的棋型打分，轮到走的一方再加上它最好一格的分数(马上就能下出来)。
//
// 普通与困难在分数与最好一步相差不超过容差的几步里随机挑一步，同一局面不总走同一步，玩家背不下一条稳赢的棋谱。
// 困难每一步的搜索量有上限(vcfNodes、searchNodes、safetyNodes)，最坏也在一两百毫秒以内。

var ErrInvalidLevel = errors.New("invalid gomoku ai level")

// 棋型，从弱到强。
const (
	shapeNone      uint8 = iota // 这个方向上已经不可能连成五子
	shapeOne                    // 还有连成五子的空间
	shapeTwo                    // 眠二：再下一子成眠三
	shapeOpenTwo                // 活二：再下一子成活三
	shapeThree                  // 眠三：再下一子成冲四
	shapeOpenThree              // 活三：再下一子成活四
	shapeFour                   // 冲四：只有一处能成五
	shapeOpenFour               // 活四：两处能成五，对方堵不住
	shapeFive                   // 五子或以上
)

// 一格的分数。
const (
	scoreFive        = 10_000_000
	scoreOpenFour    = 1_000_000 // 活四或双冲四：下一步必成五
	scoreFourThree   = 100_000   // 冲四加活三：对方堵了冲四，活三就成活四
	scoreDoubleThree = 50_000
	scoreFour        = 5_000
	scoreOpenThree   = 4_000
	scoreThree       = 500
	scoreOpenTwo     = 400
	scoreTwo         = 50
	scoreOne         = 5
)

const (
	// winScore 是赢棋的分数，越快赢分数越高。格子分数的总和远小于它。
	winScore     = 1 << 40
	infinity     = 1 << 41
	decidedScore = winScore - 2*Squares

	easyChoices     = 3
	normalTolerance = 5 // 百分比：攻守合计不低于最高一格的 95% 就算差不多好
	hardDepth       = 4
	hardTolerance   = 150
	// hardMaxPly 限制冲四、活三的延伸，vcfDepth 是连续冲四最多几手。
	hardMaxPly = 16
	vcfDepth   = 7
	// 每一步的搜索量上限(扫描候选点的次数)：找自己的连续冲四、alpha-beta 搜索、每检查一步对方的连续冲四各有一份。
	vcfNodes    = 4_000
	searchNodes = 20_000
	safetyNodes = 1_500
	// rootLimit 与 innerLimits 是根局面与搜索中每一层最多考虑的候选点数，quietLimit 是应对活三时最多考虑的着法数。
	rootLimit  = 12
	quietLimit = 6
	// safetyChecks 是最多检查几步"走完之后对方能不能连续冲四取胜"。
	safetyChecks = 6
)

// innerLimits[depth] 是剩余深度为 depth 时的候选点数。
var innerLimits = [hardDepth]int{0, 6, 8, 10}

// 搜索用的棋盘四周各垫 4 格墙，取一格四个方向上前后各 4 格时不用判断出界。
const (
	pad       = 4
	stride    = Size + 2*pad
	cellCount = stride * stride
	wall      = 3
)

var (
	dirOffsets = [4]int{1, stride, stride + 1, stride - 1}
	// lineSteps 是一个方向上前后各 4 格相对这一格的步数，顺序与棋型表下标的三进制位、线码的四进制位一致。
	lineSteps = [8]int{-4, -3, -2, -1, 1, 2, 3, 4}
	pow3      = [8]int{1, 3, 9, 27, 81, 243, 729, 2187}
	// relative[color][v] 把格子 v(空、黑、白、墙)换成对 color 而言的 0 空、1 己方、2 挡住。
	relative   = [3][4]int{{}, {0, 1, 2, 2}, {0, 2, 1, 2}}
	shapeTable = buildShapeTable()
	// codeShapes[color-1][code] 是线码 code 对 color 而言的棋型，comboTable 是四个方向的棋型合起来的分数。
	codeShapes = buildCodeShapes()
	comboTable = buildComboTable()
	boardCells = buildBoardCells()
	zobrist    = buildZobrist()
)

// buildShapeTable 给出一格(已放上己方棋子)在一个方向上的棋型，下标是前后各 4 格按 lineSteps 顺序的三进制数(0 空、
// 1 己方、2 挡住)。再放一颗己方棋子只会让下标变大，所以从大到小算：算一种情形时，它再放一子之后的各种情形都已算好。
func buildShapeTable() [6561]uint8 {
	var table [6561]uint8
	for index := len(table) - 1; index >= 0; index-- {
		var line [8]int
		rest := index
		for k := range line {
			line[k] = rest % 3
			rest /= 3
		}
		run := 1
		for k := 3; k >= 0 && line[k] == 1; k-- {
			run++
		}
		for k := 4; k < 8 && line[k] == 1; k++ {
			run++
		}
		if run >= 5 {
			table[index] = shapeFive
			continue
		}
		// 窗口里任何连续 5 格都经过中间这一格，所以数一数再放一子能成五的空格就能分出活四与冲四。
		fives, best := 0, shapeNone
		for k := range line {
			if line[k] != 0 {
				continue
			}
			next := table[index+pow3[k]]
			if next == shapeFive {
				fives++
			}
			best = max(best, next)
		}
		switch {
		case fives >= 2:
			table[index] = shapeOpenFour
		case fives == 1:
			table[index] = shapeFour
		case best == shapeOpenFour:
			table[index] = shapeOpenThree
		case best == shapeFour:
			table[index] = shapeThree
		case best == shapeOpenThree:
			table[index] = shapeOpenTwo
		case best == shapeThree:
			table[index] = shapeTwo
		case best != shapeNone:
			table[index] = shapeOne
		}
	}
	return table
}

// buildCodeShapes 把线码换成棋型。一格在一个方向上的线码是前后各 4 格的原始内容(0 空、1 黑、2 白、3 墙)按 lineSteps
// 顺序拼成的四进制数，落子、提子时只改动受影响格子的线码，不用每次重读 8 格。
func buildCodeShapes() *[2][1 << 16]uint8 {
	var table [2][1 << 16]uint8
	for code := range table[0] {
		black, white := 0, 0
		for k := range lineSteps {
			v := code >> (2 * k) & 3
			black += relative[Black][v] * pow3[k]
			white += relative[White][v] * pow3[k]
		}
		table[0][code], table[1][code] = shapeTable[black], shapeTable[white]
	}
	return &table
}

// combo 是一格四个方向的棋型合起来的结果：分数，以及是否至少成冲四。
type combo struct {
	value int
	four  bool
}

// buildComboTable 按四个方向棋型的组合编号(九进制)给出合起来的结果，编号的算法见 comboAt。
func buildComboTable() *[6561]combo {
	var table [6561]combo
	for index := range table {
		shapes := [4]uint8{uint8(index / 729), uint8(index / 81 % 9), uint8(index / 9 % 9), uint8(index % 9)}
		table[index] = combo{value: shapeValue(shapes), four: slices.Max(shapes[:]) >= shapeFour}
	}
	return &table
}

func buildBoardCells() [Squares]int {
	var cells [Squares]int
	for square := range cells {
		cells[square] = toCell(square)
	}
	return cells
}

// buildZobrist 用固定种子生成每一格每种颜色的随机数，局面的哈希是所有棋子对应随机数的异或。
func buildZobrist() [cellCount][3]uint64 {
	var keys [cellCount][3]uint64
	source := rand.New(rand.NewPCG(0x676f6d6f6b75, 0x7a6f62726973))
	for cell := range keys {
		keys[cell][1], keys[cell][2] = source.Uint64(), source.Uint64()
	}
	return keys
}

func toCell(square int) int {
	return (square/Size+pad)*stride + square%Size + pad
}

func toSquare(cell int) int {
	return (cell/stride-pad)*Size + cell%stride - pad
}

// shapeValue 把一格四个方向的棋型合起来打分。
func shapeValue(shapes [4]uint8) int {
	var count [shapeFive + 1]int
	for _, shape := range shapes {
		count[shape]++
	}
	switch {
	case count[shapeFive] > 0:
		return scoreFive
	case count[shapeOpenFour] > 0 || count[shapeFour] >= 2:
		return scoreOpenFour
	case count[shapeFour] > 0 && count[shapeOpenThree] > 0:
		return scoreFourThree
	case count[shapeOpenThree] >= 2:
		return scoreDoubleThree
	}
	return count[shapeFour]*scoreFour + count[shapeOpenThree]*scoreOpenThree + count[shapeThree]*scoreThree +
		count[shapeOpenTwo]*scoreOpenTwo + count[shapeTwo]*scoreTwo + count[shapeOne]*scoreOne
}

// candidate 是一个候选空格：mine、theirs 是轮到走的一方、对方在这里落子的分数，mineFour、theirFour 表示落子后至少成冲四。
type candidate struct {
	cell      int
	mine      int
	theirs    int
	mineFour  bool
	theirFour bool
}

func (c candidate) priority() int {
	return c.mine + c.theirs
}

// scanResult 是扫一遍候选空格的结果，从轮到走的一方看。
type scanResult struct {
	cands []candidate
	// myFive、myOpenFour 是自己能成五、能走出活四(或双冲四)的一格，没有时为 -1。
	myFive     int
	myOpenFour int
	// theirFive 是对方能成五的一格，theirFives 是这样的格子数：一处必须堵，两处就堵不住了。
	theirFive  int
	theirFives int
	// theirOpenFour 表示对方下一步能走出活四或双冲四，也就是对方有活三一类的威胁。
	theirOpenFour bool
}

// searcher 是 AI 用的棋盘：codes 是每一格四个方向的线码，near 记着每一格两格以内有几颗棋子，候选点只在有棋子的地方附近。
type searcher struct {
	board [cellCount]int8
	codes [cellCount][4]uint16
	near  [cellCount]int8
	hash  uint64
	nodes int
	// limit 是这一阶段搜索量的上限，到了就按当前局面估值或放弃寻找连续冲四。
	limit   int
	buffers [hardMaxPly + 1][]candidate
	// failed 记下已经证明在几手之内没有连续冲四取胜的局面(键是哈希异或进攻方)；vcfAborted 表示这次搜索因为搜索量到顶
	// 而中断，结论不可信，不能记下。
	failed     map[uint64]int
	vcfAborted bool
}

func newSearcher(g *Game) *searcher {
	s := &searcher{failed: map[uint64]int{}}
	for cell := range s.board {
		s.board[cell] = wall
	}
	for _, cell := range boardCells {
		s.board[cell] = 0
	}
	for _, cell := range boardCells {
		for d, offset := range dirOffsets {
			for k, step := range lineSteps {
				s.codes[cell][d] |= uint16(s.board[cell+step*offset]) << (2 * k)
			}
		}
	}
	for square, stone := range g.board {
		if stone != Empty {
			s.place(toCell(square), int8(stone))
		}
	}
	for ply := range s.buffers {
		s.buffers[ply] = make([]candidate, 0, Squares)
	}
	return s
}

func (s *searcher) place(cell int, color int8) {
	s.board[cell] = color
	s.hash ^= zobrist[cell][color]
	for d, offset := range dirOffsets {
		for k, step := range lineSteps {
			s.codes[cell-step*offset][d] += uint16(color) << (2 * k)
		}
	}
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			s.near[cell+dy*stride+dx]++
		}
	}
}

func (s *searcher) remove(cell int) {
	color := s.board[cell]
	s.hash ^= zobrist[cell][color]
	s.board[cell] = 0
	for d, offset := range dirOffsets {
		for k, step := range lineSteps {
			s.codes[cell-step*offset][d] -= uint16(color) << (2 * k)
		}
	}
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			s.near[cell+dy*stride+dx]--
		}
	}
}

// comboAt 是 color 在 cell 落子(或 cell 上已有的 color 棋子)四个方向的棋型合起来的结果。
func (s *searcher) comboAt(cell int, color int8) combo {
	shapes, codes := &codeShapes[color-1], &s.codes[cell]
	return comboTable[int(shapes[codes[0]])*729+int(shapes[codes[1]])*81+int(shapes[codes[2]])*9+int(shapes[codes[3]])]
}

// scan 给两格以内有棋子的每个空格算出双方在这里落子的分数，结果放在第 ply 层的缓冲里。
func (s *searcher) scan(color int8, ply int) scanResult {
	r := scanResult{myFive: -1, myOpenFour: -1, theirFive: -1}
	cands := s.buffers[ply][:0]
	for _, cell := range boardCells {
		if s.board[cell] != 0 || s.near[cell] == 0 {
			continue
		}
		mine, theirs := s.comboAt(cell, color), s.comboAt(cell, 3-color)
		c := candidate{cell: cell, mine: mine.value, theirs: theirs.value, mineFour: mine.four, theirFour: theirs.four}
		switch {
		case c.mine >= scoreFive:
			if r.myFive < 0 {
				r.myFive = cell
			}
		case c.mine >= scoreOpenFour:
			if r.myOpenFour < 0 {
				r.myOpenFour = cell
			}
		}
		switch {
		case c.theirs >= scoreFive:
			if r.theirFive < 0 {
				r.theirFive = cell
			}
			r.theirFives++
		case c.theirs >= scoreOpenFour:
			r.theirOpenFour = true
		}
		cands = append(cands, c)
	}
	r.cands = cands
	s.buffers[ply] = cands
	return r
}

// topMoves 从 cands 里挑出攻守合计最高的至多 limit 格，keep 为空时不筛选。
func topMoves(cands []candidate, limit int, keep func(candidate) bool) []candidate {
	if limit <= 0 {
		return nil
	}
	top := make([]candidate, 0, limit+1)
	for _, c := range cands {
		if keep != nil && !keep(c) {
			continue
		}
		i := len(top)
		if i == limit && top[i-1].priority() >= c.priority() {
			continue
		}
		top = append(top, c)
		for i > 0 && top[i-1].priority() < c.priority() {
			top[i] = top[i-1]
			i--
		}
		top[i] = c
		if len(top) > limit {
			top = top[:limit]
		}
	}
	return top
}

// defensive 是对方有活三一类威胁时还值得走的着法：自己冲四反击，或者占住对方能成冲四以上的格子。
func defensive(c candidate) bool {
	return c.mineFour || c.theirFour
}

// selectMoves 是搜索里要试的着法：对方有活三时只试应对它的着法。
func selectMoves(r scanResult, limit int) []candidate {
	if r.theirOpenFour {
		return topMoves(r.cands, limit, defensive)
	}
	return topMoves(r.cands, limit, nil)
}

// bestBlock 在对方能成五的格子里挑一格去堵，同时对自己最有利的优先。
func bestBlock(r scanResult) int {
	best := candidate{cell: -1, mine: -1}
	for _, c := range r.cands {
		if c.theirs >= scoreFive && c.mine > best.mine {
			best = c
		}
	}
	return best.cell
}

// negamax 返回轮到 color 走时在 depth 步以内能保证的分数。
func (s *searcher) negamax(color int8, depth int, alpha int, beta int, ply int) int {
	s.nodes++
	r := s.scan(color, ply)
	if r.myFive >= 0 {
		return winScore - ply - 1
	}
	if r.theirFives >= 2 {
		return -(winScore - ply - 2)
	}
	if ply >= hardMaxPly || s.nodes >= s.limit {
		return s.evaluate(color, r)
	}
	var moves []candidate
	nextDepth := depth - 1
	switch {
	case r.theirFives == 1:
		// 对方冲四：只能堵，不耗深度。
		moves, nextDepth = []candidate{{cell: r.theirFive}}, depth
	case r.myOpenFour >= 0:
		return winScore - ply - 3
	case depth <= 0:
		if !r.theirOpenFour {
			return s.evaluate(color, r)
		}
		// 搜到底时对方还有活三：把应对走完再估值，不然刚走出活三的一方总显得占了大便宜。
		moves, nextDepth = topMoves(r.cands, quietLimit, defensive), 0
	default:
		moves = selectMoves(r, innerLimits[depth])
	}
	best := -infinity
	for _, move := range moves {
		s.place(move.cell, color)
		score := -s.negamax(3-color, nextDepth, -beta, -alpha, ply+1)
		s.remove(move.cell)
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

// evaluate 从轮到走的 color 这一方看局面：每颗棋子按它在四个方向上参与的棋型打分，己方加、对方减；轮到自己走，
// 自己最好的一格马上就能下出来，再加上这一格的分数。
func (s *searcher) evaluate(color int8, r scanResult) int {
	score := 0
	for _, c := range r.cands {
		score = max(score, c.mine)
	}
	for _, cell := range boardCells {
		stone := s.board[cell]
		if stone == 0 {
			continue
		}
		value := s.comboAt(cell, stone).value
		if stone == color {
			score += value
		} else {
			score -= value
		}
	}
	return score
}

// fiveCellsAround 返回 color 在刚下的 cell 周围能成五的空格(只看穿过 cell 的四条线)。
func (s *searcher) fiveCellsAround(cell int, color int8, found []int) []int {
	shapes := &codeShapes[color-1]
	for d, offset := range dirOffsets {
		for _, step := range lineSteps {
			target := cell + step*offset
			if s.board[target] == 0 && shapes[s.codes[target][d]] == shapeFive && !slices.Contains(found, target) {
				found = append(found, target)
			}
		}
	}
	return found
}

// vcf 找 color(轮到它走)的连续冲四取胜：每一步都冲四，对方只能堵在唯一的成五点，直到能成五或走出活四。找到时返回
// 第一步，找不到返回 -1。
func (s *searcher) vcf(color int8, depth int, ply int) int {
	s.nodes++
	key := s.hash ^ uint64(color)
	if tried, ok := s.failed[key]; ok && tried >= depth {
		return -1
	}
	r := s.scan(color, ply)
	switch {
	case r.myFive >= 0:
		return r.myFive
	case r.theirFives > 0:
		return -1
	case r.myOpenFour >= 0:
		return r.myOpenFour
	case depth == 0 || ply+2 > hardMaxPly:
		return -1
	case s.nodes >= s.limit:
		s.vcfAborted = true
		return -1
	}
	fours := topMoves(r.cands, Squares, func(c candidate) bool { return c.mineFour })
	var blocks [4]int
	for _, four := range fours {
		s.place(four.cell, color)
		found := s.fiveCellsAround(four.cell, color, blocks[:0])
		next := -1
		if len(found) == 1 {
			s.place(found[0], 3-color)
			next = s.vcf(color, depth-1, ply+2)
			s.remove(found[0])
		}
		s.remove(four.cell)
		if next >= 0 {
			return four.cell
		}
	}
	if !s.vcfAborted {
		s.failed[key] = depth
	}
	return -1
}

// BestMove 为轮到走的一方选一步。level 为 1 简单、2 普通、3 困难；rng 用来在差不多好的几步里随机挑一步。
func BestMove(g *Game, level int, rng *rand.Rand) (Point, error) {
	if g.Over() {
		return Point{}, ErrIllegalMove
	}
	if level < 1 || level > 3 {
		return Point{}, ErrInvalidLevel
	}
	if len(g.moves) == 0 {
		return Point{X: Size / 2, Y: Size / 2}, nil
	}
	s := newSearcher(g)
	color := int8(g.Turn())
	var cell int
	switch level {
	case 1:
		cell = s.easyMove(color, rng)
	case 2:
		cell = s.normalMove(color, rng)
	default:
		cell = s.hardMove(color, rng)
	}
	square := toSquare(cell)
	return Point{X: square % Size, Y: square / Size}, nil
}

// easyMove 能成五就成五，否则在攻守合计最高的几格里随机挑，越靠前的越常被挑中。
func (s *searcher) easyMove(color int8, rng *rand.Rand) int {
	r := s.scan(color, 0)
	if r.myFive >= 0 {
		return r.myFive
	}
	top := topMoves(r.cands, easyChoices, nil)
	// 第 i 名的权重是 len-i：三格时依次约 1/2、1/3、1/6。
	total := len(top) * (len(top) + 1) / 2
	pick := rng.IntN(total)
	for i := range top {
		weight := len(top) - i
		if pick < weight {
			return top[i].cell
		}
		pick -= weight
	}
	return top[0].cell
}

// normalMove 只看一步：能成五就成五，必堵对方的冲四，能走活四、冲四活三就走，必堵对方的活三，否则走攻守合计最高的一格。
func (s *searcher) normalMove(color int8, rng *rand.Rand) int {
	r := s.scan(color, 0)
	switch {
	case r.myFive >= 0:
		return r.myFive
	case r.theirFives > 0:
		return bestBlock(r)
	case r.myOpenFour >= 0:
		return r.myOpenFour
	}
	var keep func(candidate) bool
	switch {
	case slices.ContainsFunc(r.cands, func(c candidate) bool { return c.mine >= scoreFourThree }):
		keep = func(c candidate) bool { return c.mine >= scoreFourThree }
	case r.theirOpenFour:
		keep = func(c candidate) bool { return c.theirs >= scoreOpenFour }
	}
	top := topMoves(r.cands, Squares, keep)
	threshold := top[0].priority() - top[0].priority()*normalTolerance/100
	similar := 1
	for similar < len(top) && top[similar].priority() >= threshold {
		similar++
	}
	return top[rng.IntN(similar)].cell
}

// hardMove 是困难难度：能赢就赢，必堵冲四，找自己的连续冲四，再搜 hardDepth 步，并排除走完之后对方能连续冲四取胜的着法。
func (s *searcher) hardMove(color int8, rng *rand.Rand) int {
	r := s.scan(color, 0)
	switch {
	case r.myFive >= 0:
		return r.myFive
	case r.theirFives > 0:
		return bestBlock(r)
	case r.myOpenFour >= 0:
		return r.myOpenFour
	}
	s.limit, s.vcfAborted = s.nodes+vcfNodes, false
	if move := s.vcf(color, vcfDepth, 1); move >= 0 {
		return move
	}
	s.limit = s.nodes + searchNodes
	moves := selectMoves(r, rootLimit)
	scores := make([]int, len(moves))
	best := -infinity
	for i, move := range moves {
		// 比最好一步差出容差的着法只需知道它更差，窗口下沿之下的分数不必算准。
		alpha := -infinity
		if best > -infinity {
			alpha = best - hardTolerance - 1
		}
		s.place(move.cell, color)
		scores[i] = -s.negamax(3-color, hardDepth-1, -infinity, -alpha, 1)
		s.remove(move.cell)
		best = max(best, scores[i])
	}
	order := make([]int, len(moves))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return scores[b] - scores[a] })

	// 从分数最高的一步往下，排除走完之后对方能连续冲四取胜的着法；只在不比第一个安全着法差出容差的几步里挑。
	tolerance := hardTolerance
	if scores[order[0]] >= decidedScore || scores[order[0]] <= -decidedScore {
		tolerance = 0
	}
	picks := make([]int, 0, len(order))
	bestSafe := -infinity
	for checked, i := range order {
		if bestSafe > -infinity && scores[i] < bestSafe-tolerance {
			break
		}
		if checked >= safetyChecks {
			break
		}
		s.limit, s.vcfAborted = s.nodes+safetyNodes, false
		s.place(moves[i].cell, color)
		unsafe := s.vcf(3-color, vcfDepth, 1) >= 0
		s.remove(moves[i].cell)
		if unsafe {
			continue
		}
		if bestSafe == -infinity {
			bestSafe = scores[i]
		}
		picks = append(picks, moves[i].cell)
	}
	if len(picks) == 0 {
		return moves[order[0]].cell
	}
	return picks[rng.IntN(len(picks))]
}
