// Package xiangqi 是游戏中心中国象棋的规则引擎与电脑棋手(见 ai.go)。
//
// 坐标：x 是列 0-8，y 是行 0-9。红方在 y 0-4 一侧，底线是 y=0；黑方在 y 5-9 一侧，底线是 y=9；河界在 y=4 与 y=5 之间。
// 红方九宫是 x 3-5、y 0-2，黑方九宫是 x 3-5、y 7-9。从红方看过去 x 从左到右增大、y 从下往上增大。
// 棋盘存成 90 个字符的字符串，第 y*9+x 个字符是 (x,y) 上的棋子：红方大写、黑方小写，K 帅/将、A 仕/士、B 相/象、N 马、
// R 车、C 炮、P 兵/卒，"." 是空位。
//
// 走法：帅/将在九宫内直走一格，双方的将帅不能在同一列上直接照面(飞将)；仕/士在九宫内斜走一格；相/象走田字、不能过河，
// 田字中心有子(塞象眼)不能走；马走日字，紧挨着的直线方向有子(蹩马腿)不能走；车直走任意格；炮不吃子时和车一样走，吃子时
// 必须隔着正好一个棋子(炮架)；兵/卒没过河只能前进一格，过河后也可以左右走一格，永远不能后退。走完之后己方的将帅不能
// 被对方攻击(不能送将，飞将也算)。
//
// 胜负：轮到走的一方没有合法着法就输了，被将死和困毙都算输。同一局面(棋盘与轮到的一方)第 RepeatLimit 次出现时：这几次之间
// 一方每步都在将军、另一方不是时，长将的一方判负，否则和棋。另外连续 MaxQuietPlies 步(双方合计)没有吃子，或总步数达到
// MaxPlies 也判和。一步棋同时将死对方又重复了局面或到了和棋步数时，按将死算。长捉等其他循环规则不做。
package xiangqi

import (
	"errors"
	"strings"
)

const (
	Width   = 9
	Height  = 10
	Squares = Width * Height

	// MaxQuietPlies 是连续多少步(双方合计)没有吃子判和。
	MaxQuietPlies = 120
	// MaxPlies 是总步数(双方合计)达到多少判和，也限定了一局最长多久。
	MaxPlies = 300
	// RepeatLimit 是同一局面第几次出现时判长将负或和棋。
	RepeatLimit = 3
)

// StartBoard 是开局的棋盘。
const StartBoard = "RNBAKABNR" +
	"........." +
	".C.....C." +
	"P.P.P.P.P" +
	"........." +
	"........." +
	"p.p.p.p.p" +
	".c.....c." +
	"........." +
	"rnbakabnr"

// Color 是一方：红方先走。
type Color int8

const (
	Red     Color = 0
	Black   Color = 1
	NoColor Color = -1
)

// Other 返回对方，只对红黑两方有意义。
func (c Color) Other() Color {
	return c ^ 1
}

// Kind 是棋子的种类。
type Kind int8

const (
	King     Kind = iota + 1 // 帅/将
	Advisor                  // 仕/士
	Elephant                 // 相/象
	Horse                    // 马
	Rook                     // 车
	Cannon                   // 炮
	Pawn                     // 兵/卒
)

// Piece 是棋盘上的一个棋子，0 是空位：低 3 位是种类，第 4 位是颜色。
type Piece int8

const Empty Piece = 0

// pieceLetters 按 Kind 的顺序列出棋子在棋盘字符串里的字母，红方大写、黑方小写。
const pieceLetters = "KABNRCP"

func makePiece(c Color, k Kind) Piece {
	return Piece(int8(c)<<3 | int8(k))
}

// Color 返回棋子的颜色，空位没有颜色，调用方要先排除空位。
func (p Piece) Color() Color {
	return Color(p >> 3)
}

func (p Piece) Kind() Kind {
	return Kind(p & 7)
}

func (p Piece) letter() byte {
	if p == Empty {
		return '.'
	}
	letter := pieceLetters[p.Kind()-1]
	if p.Color() == Black {
		letter += 'a' - 'A'
	}
	return letter
}

// parsePiece 是 letter 的反面：棋盘字符串里的一个字母对应的棋子，不认识的字母 ok 为假。
func parsePiece(letter byte) (Piece, bool) {
	if index := strings.IndexByte(pieceLetters, letter); index >= 0 {
		return makePiece(Red, Kind(index+1)), true
	}
	if index := strings.IndexByte(pieceLetters, letter-('a'-'A')); index >= 0 {
		return makePiece(Black, Kind(index+1)), true
	}
	return Empty, false
}

// Square 返回 (x,y) 的格子编号 y*9+x。
func Square(x, y int) int {
	return y*Width + x
}

// SquareXY 返回格子编号对应的坐标。
func SquareXY(sq int) (int, int) {
	return sq % Width, sq / Width
}

// Move 是一步棋：从格子 From 走到格子 To。
type Move struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Result 是一局棋怎么结束的，空字符串表示还在进行。
type Result string

const (
	ResultCheckmate      Result = "checkmate"       // 将死
	ResultStalemate      Result = "stalemate"       // 困毙：没被将军但无子可走，同样判负
	ResultNoCapture      Result = "no_capture"      // 连续 MaxQuietPlies 步没有吃子，和棋
	ResultMoveLimit      Result = "move_limit"      // 总步数达到 MaxPlies，和棋
	ResultRepetition     Result = "repetition"      // 同一局面出现 RepeatLimit 次，和棋
	ResultPerpetualCheck Result = "perpetual_check" // 同一局面出现 RepeatLimit 次且一方一直在将军，长将的一方判负
)

var (
	ErrIllegalMove  = errors.New("xiangqi: illegal move")
	ErrGameOver     = errors.New("xiangqi: game is over")
	ErrInvalidState = errors.New("xiangqi: invalid game state")
	// ErrNothingToUndo 表示记录里没有那么多步可以撤回。
	ErrNothingToUndo = errors.New("xiangqi: nothing to undo")
)

// step 是一个有阻挡点的走法：block 是马腿或象眼，to 是落点。
type step struct {
	block int
	to    int
}

// 预先算好每一格的走法，生成着法时只查表。rays 的四个方向依次是 y+1、y-1、x+1、x-1，前两个是竖直方向。
var (
	rays          [Squares][4][]int
	horseSteps    [Squares][]step
	horseAttacks  [Squares][]step // 能跳到这一格的马：block 是马腿，to 是马所在的格子
	elephantSteps [2][Squares][]step
	advisorSteps  [2][Squares][]int
	kingSteps     [2][Squares][]int
	pawnSteps     [2][Squares][]int
)

var orthogonal = [4][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}

var diagonal = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

func onBoard(x, y int) bool {
	return x >= 0 && x < Width && y >= 0 && y < Height
}

func inPalace(c Color, x, y int) bool {
	if x < 3 || x > 5 {
		return false
	}
	if c == Red {
		return y >= 0 && y <= 2
	}
	return y >= 7 && y <= 9
}

// crossedRiver 表示 c 方的棋子在第 y 行已经过了河。
func crossedRiver(c Color, y int) bool {
	if c == Red {
		return y >= 5
	}
	return y <= 4
}

// forward 是 c 方兵卒前进的方向。
func forward(c Color) int {
	if c == Red {
		return 1
	}
	return -1
}

func init() {
	for sq := 0; sq < Squares; sq++ {
		x, y := SquareXY(sq)
		for dir, d := range orthogonal {
			for nx, ny := x+d[0], y+d[1]; onBoard(nx, ny); nx, ny = nx+d[0], ny+d[1] {
				rays[sq][dir] = append(rays[sq][dir], Square(nx, ny))
			}
			// 马先沿这个方向走一格(马腿)，再向两侧斜出一格。
			legX, legY := x+d[0], y+d[1]
			if !onBoard(legX, legY) {
				continue
			}
			for _, side := range [2]int{1, -1} {
				tx, ty := legX+d[0]+side*d[1], legY+d[1]+side*d[0]
				if onBoard(tx, ty) {
					horseSteps[sq] = append(horseSteps[sq], step{block: Square(legX, legY), to: Square(tx, ty)})
				}
			}
		}
		for _, c := range [2]Color{Red, Black} {
			for _, d := range diagonal {
				ex, ey := x+2*d[0], y+2*d[1]
				if onBoard(ex, ey) && !crossedRiver(c, ey) && !crossedRiver(c, y) {
					elephantSteps[c][sq] = append(elephantSteps[c][sq], step{block: Square(x+d[0], y+d[1]), to: Square(ex, ey)})
				}
				if inPalace(c, x, y) && inPalace(c, x+d[0], y+d[1]) {
					advisorSteps[c][sq] = append(advisorSteps[c][sq], Square(x+d[0], y+d[1]))
				}
			}
			for _, d := range orthogonal {
				if inPalace(c, x, y) && inPalace(c, x+d[0], y+d[1]) {
					kingSteps[c][sq] = append(kingSteps[c][sq], Square(x+d[0], y+d[1]))
				}
			}
			if onBoard(x, y+forward(c)) {
				pawnSteps[c][sq] = append(pawnSteps[c][sq], Square(x, y+forward(c)))
			}
			if crossedRiver(c, y) {
				for _, dx := range [2]int{-1, 1} {
					if onBoard(x+dx, y) {
						pawnSteps[c][sq] = append(pawnSteps[c][sq], Square(x+dx, y))
					}
				}
			}
		}
	}
	for from := 0; from < Squares; from++ {
		for _, st := range horseSteps[from] {
			horseAttacks[st.to] = append(horseAttacks[st.to], step{block: st.block, to: from})
		}
	}
}

// Position 是一个局面：棋盘与轮到哪一方。hash 与 score 随着走子增量更新，供电脑棋手使用。
type Position struct {
	board [Squares]Piece
	turn  Color
	kings [2]int
	hash  uint64
	score [2]int
}

// ParsePosition 解析 90 个字符的棋盘。每方必须正好有一个帅/将，没轮到走的一方不能正被将军：那样的局面走不出来，
// 轮到的一方还能直接吃掉对方的将帅。
func ParsePosition(board string, turn Color) (*Position, error) {
	if len(board) != Squares || (turn != Red && turn != Black) {
		return nil, ErrInvalidState
	}
	pos := &Position{kings: [2]int{-1, -1}}
	for sq := 0; sq < Squares; sq++ {
		ch := board[sq]
		if ch == '.' {
			continue
		}
		piece, ok := parsePiece(ch)
		if !ok {
			return nil, ErrInvalidState
		}
		color := piece.Color()
		if piece.Kind() == King {
			if pos.kings[color] >= 0 {
				return nil, ErrInvalidState
			}
			pos.kings[color] = sq
		}
		pos.board[sq] = piece
		pos.hash ^= zobristPiece[piece][sq]
		pos.score[color] += int(pieceSquare[piece][sq])
	}
	if pos.kings[Red] < 0 || pos.kings[Black] < 0 || pos.attacked(pos.kings[turn.Other()], turn) {
		return nil, ErrInvalidState
	}
	if turn == Black {
		pos.turn = Black
		pos.hash ^= zobristTurn
	}
	return pos, nil
}

// Board 返回 90 个字符的棋盘。
func (p *Position) Board() string {
	var board [Squares]byte
	for sq, piece := range p.board {
		board[sq] = piece.letter()
	}
	return string(board[:])
}

// makeMove 走一步(不检查是否合法)并返回被吃掉的棋子，unmakeMove 用它把局面退回去。
func (p *Position) makeMove(m Move) Piece {
	piece, captured := p.board[m.From], p.board[m.To]
	us := piece.Color()
	p.hash ^= zobristPiece[piece][m.From] ^ zobristPiece[piece][m.To] ^ zobristTurn
	p.score[us] += int(pieceSquare[piece][m.To]) - int(pieceSquare[piece][m.From])
	if captured != Empty {
		p.hash ^= zobristPiece[captured][m.To]
		p.score[us.Other()] -= int(pieceSquare[captured][m.To])
	}
	p.board[m.To], p.board[m.From] = piece, Empty
	if piece.Kind() == King {
		p.kings[us] = m.To
	}
	p.turn = us.Other()
	return captured
}

func (p *Position) unmakeMove(m Move, captured Piece) {
	piece := p.board[m.To]
	us := piece.Color()
	p.hash ^= zobristPiece[piece][m.From] ^ zobristPiece[piece][m.To] ^ zobristTurn
	p.score[us] -= int(pieceSquare[piece][m.To]) - int(pieceSquare[piece][m.From])
	if captured != Empty {
		p.hash ^= zobristPiece[captured][m.To]
		p.score[us.Other()] += int(pieceSquare[captured][m.To])
	}
	p.board[m.From], p.board[m.To] = piece, captured
	if piece.Kind() == King {
		p.kings[us] = m.From
	}
	p.turn = us
}

// addTarget 在落点是空位或对方棋子时加上这一步；capturesOnly 时只要吃子的。
func (p *Position) addTarget(moves []Move, from int, to int, capturesOnly bool) []Move {
	target := p.board[to]
	if target == Empty {
		if !capturesOnly {
			moves = append(moves, Move{From: from, To: to})
		}
		return moves
	}
	if target.Color() != p.turn {
		moves = append(moves, Move{From: from, To: to})
	}
	return moves
}

// generate 把轮到走的一方符合棋子走法的着法追加到 moves 后面，还没排除走完被将军的着法。
func (p *Position) generate(moves []Move, capturesOnly bool) []Move {
	us := p.turn
	for sq, piece := range p.board {
		if piece == Empty || piece.Color() != us {
			continue
		}
		switch piece.Kind() {
		case King:
			for _, to := range kingSteps[us][sq] {
				moves = p.addTarget(moves, sq, to, capturesOnly)
			}
		case Advisor:
			for _, to := range advisorSteps[us][sq] {
				moves = p.addTarget(moves, sq, to, capturesOnly)
			}
		case Pawn:
			for _, to := range pawnSteps[us][sq] {
				moves = p.addTarget(moves, sq, to, capturesOnly)
			}
		case Elephant:
			for _, st := range elephantSteps[us][sq] {
				if p.board[st.block] == Empty {
					moves = p.addTarget(moves, sq, st.to, capturesOnly)
				}
			}
		case Horse:
			for _, st := range horseSteps[sq] {
				if p.board[st.block] == Empty {
					moves = p.addTarget(moves, sq, st.to, capturesOnly)
				}
			}
		case Rook:
			for dir := range rays[sq] {
				for _, to := range rays[sq][dir] {
					moves = p.addTarget(moves, sq, to, capturesOnly)
					if p.board[to] != Empty {
						break
					}
				}
			}
		case Cannon:
			for dir := range rays[sq] {
				screened := false
				for _, to := range rays[sq][dir] {
					target := p.board[to]
					if !screened {
						if target != Empty {
							screened = true
						} else if !capturesOnly {
							moves = append(moves, Move{From: sq, To: to})
						}
						continue
					}
					if target != Empty {
						if target.Color() != us {
							moves = append(moves, Move{From: sq, To: to})
						}
						break
					}
				}
			}
		}
	}
	return moves
}

// attacked 表示 by 方能吃到 sq 上的棋子，用来判断将军：车、隔一个炮架的炮、没有蹩腿的马、兵卒，
// 以及同一列上中间没有棋子的对方将帅(飞将)。仕相过不了河、出不了九宫，攻击不到对方的将帅。
func (p *Position) attacked(sq int, by Color) bool {
	for dir := range rays[sq] {
		ray := rays[sq][dir]
		i := 0
		for i < len(ray) && p.board[ray[i]] == Empty {
			i++
		}
		if i == len(ray) {
			continue
		}
		first := p.board[ray[i]]
		if first == makePiece(by, Rook) || (dir < 2 && first == makePiece(by, King)) {
			return true
		}
		for i++; i < len(ray) && p.board[ray[i]] == Empty; i++ {
		}
		if i < len(ray) && p.board[ray[i]] == makePiece(by, Cannon) {
			return true
		}
	}
	horse := makePiece(by, Horse)
	for _, st := range horseAttacks[sq] {
		if p.board[st.to] == horse && p.board[st.block] == Empty {
			return true
		}
	}
	pawn := makePiece(by, Pawn)
	x, y := SquareXY(sq)
	if behind := y - forward(by); behind >= 0 && behind < Height && p.board[Square(x, behind)] == pawn {
		return true
	}
	if crossedRiver(by, y) {
		if x > 0 && p.board[sq-1] == pawn {
			return true
		}
		if x < Width-1 && p.board[sq+1] == pawn {
			return true
		}
	}
	return false
}

// InCheck 表示轮到走的一方正被将军。
func (p *Position) InCheck() bool {
	return p.attacked(p.kings[p.turn], p.turn.Other())
}

// legalAfter 走一步看看己方将帅会不会被攻击，再退回来。
func (p *Position) legalAfter(m Move) bool {
	us := p.turn
	captured := p.makeMove(m)
	legal := !p.attacked(p.kings[us], us.Other())
	p.unmakeMove(m, captured)
	return legal
}

// LegalMoves 返回轮到走的一方的全部合法着法。
func (p *Position) LegalMoves() []Move {
	var buffer [128]Move
	pseudo := p.generate(buffer[:0], false)
	legal := make([]Move, 0, len(pseudo))
	for _, m := range pseudo {
		if p.legalAfter(m) {
			legal = append(legal, m)
		}
	}
	return legal
}

// hasLegalMove 表示轮到走的一方至少有一步合法着法，找到一步就停。
func (p *Position) hasLegalMove() bool {
	var buffer [128]Move
	for _, m := range p.generate(buffer[:0], false) {
		if p.legalAfter(m) {
			return true
		}
	}
	return false
}

// Step 是走过的一步：走法、吃掉的棋子(悔棋时放回去，没吃子时为空)、走完之后对方是否被将军(判长将用)，以及走这一步之前
// 连续没有吃子的步数(悔棋时直接放回去，不必从记录里重新数)。
type Step struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Captured string `json:"captured,omitempty"`
	Check    bool   `json:"check,omitempty"`
	Quiet    int    `json:"quiet,omitempty"`
}

// Game 是一局棋，整份以 JSON 存在对局状态里。Plies 是已经走了几步(双方合计)，Quiet 是最近连续几步没有吃子，
// Winner 在分出胜负时是赢的一方，否则是 NoColor。History 是走过的每一步，悔棋与判重复局面都靠它；记录走法之前开始的对局
// 只有之后的步，更早的步不能悔，也不参与判重复。
type Game struct {
	Board   string `json:"board"`
	Turn    Color  `json:"turn"`
	Plies   int    `json:"plies"`
	Quiet   int    `json:"quiet"`
	Last    *Move  `json:"last,omitempty"`
	Result  Result `json:"result,omitempty"`
	Winner  Color  `json:"winner"`
	History []Step `json:"history,omitempty"`
}

// NewGame 返回开局：红方先走。
func NewGame() Game {
	return Game{Board: StartBoard, Turn: Red, Winner: NoColor}
}

// Over 表示这局已经结束。
func (g *Game) Over() bool {
	return g.Result != ""
}

// Position 解析出当前局面。
func (g *Game) Position() (*Position, error) {
	return ParsePosition(g.Board, g.Turn)
}

// Play 走一步。不合规则时返回 ErrIllegalMove，这局已经结束时返回 ErrGameOver，g 都不会改变。
// 走完之后对方无子可走就是走的一方赢(将死或困毙)，否则再看局面是否重复了 RepeatLimit 次、是否到了和棋的步数。
func (g *Game) Play(m Move) error {
	if g.Over() {
		return ErrGameOver
	}
	pos, err := g.Position()
	if err != nil {
		return err
	}
	if m.From < 0 || m.From >= Squares || m.To < 0 || m.To >= Squares {
		return ErrIllegalMove
	}
	legal := false
	for _, candidate := range pos.LegalMoves() {
		if candidate == m {
			legal = true
			break
		}
	}
	if !legal {
		return ErrIllegalMove
	}
	mover := pos.turn
	quietBefore := g.Quiet
	captured := pos.makeMove(m)
	g.Board, g.Turn = pos.Board(), pos.turn
	g.Plies++
	g.Quiet++
	step := Step{From: m.From, To: m.To, Check: pos.InCheck(), Quiet: quietBefore}
	if captured != Empty {
		g.Quiet = 0
		step.Captured = string(captured.letter())
	}
	// 重新分配一份，不和调用方手里的旧局面共用底层数组。
	g.History = append(g.History[:len(g.History):len(g.History)], step)
	g.Last = &m
	repeats, span := g.Repetitions()
	switch {
	case !pos.hasLegalMove():
		g.Winner, g.Result = mover, ResultStalemate
		if pos.InCheck() {
			g.Result = ResultCheckmate
		}
	case repeats >= RepeatLimit:
		g.Result = ResultRepetition
		switch checker := g.perpetualChecker(span); checker {
		case mover, mover.Other():
			g.Winner, g.Result = checker.Other(), ResultPerpetualCheck
		}
	case g.Quiet >= MaxQuietPlies:
		g.Result = ResultNoCapture
	case g.Plies >= MaxPlies:
		g.Result = ResultMoveLimit
	}
	return nil
}

// Repetitions 返回当前局面(棋盘与轮到的一方)自最近一次吃子以来一共出现了几次(含现在)，以及其中最早的一次之后走了几步。
func (g *Game) Repetitions() (count int, span int) {
	pos, err := g.Position()
	if err != nil {
		return 1, 0
	}
	count = 1
	for i, back := range g.positionsSinceCapture(pos) {
		if back.hash == pos.hash && back.board == pos.board && back.turn == pos.turn {
			count++
			span = i + 1
		}
	}
	return count, span
}

// positionsSinceCapture 返回当前局面 pos 之前、最近一次吃子之后走出过的局面，从近到远：第 i 个是撤回最后 i+1 步的局面。
// 吃子之前的局面子数不同，不会再出现。
func (g *Game) positionsSinceCapture(pos *Position) []Position {
	var positions []Position
	back := *pos
	for i := len(g.History) - 1; i >= 0 && g.History[i].Captured == ""; i-- {
		step := g.History[i]
		if back.board[step.To] == Empty || back.board[step.From] != Empty {
			break
		}
		back.unmakeMove(Move{From: step.From, To: step.To}, Empty)
		positions = append(positions, back)
	}
	return positions
}

// perpetualChecker 看最后 span 步：一方每一步都在将军、另一方不是时返回长将的一方，否则返回 NoColor。span 是偶数，双方各走
// 一半，最后一步是刚走完的一方。
func (g *Game) perpetualChecker(span int) Color {
	if span < 2 || span > len(g.History) {
		return NoColor
	}
	checks := [2]bool{true, true}
	for i, step := range g.History[len(g.History)-span:] {
		// 第 i 步是谁走的：span 是偶数，最后一步(i = span-1)是刚走完的一方，即现在没轮到的一方。
		side := (span - 1 - i) % 2
		checks[side] = checks[side] && step.Check
	}
	mover := g.Turn.Other()
	switch {
	case checks[0] && !checks[1]:
		return mover
	case checks[1] && !checks[0]:
		return mover.Other()
	}
	return NoColor
}

// Undo 撤回最后 n 步，之后轮到撤回的第一步那一方走。对局已经结束时返回 ErrGameOver，记录里没有这么多步时返回
// ErrNothingToUndo，g 都不会改变。
func (g *Game) Undo(n int) error {
	if g.Over() {
		return ErrGameOver
	}
	if n < 1 || n > len(g.History) {
		return ErrNothingToUndo
	}
	pos, err := g.Position()
	if err != nil {
		return err
	}
	keep := len(g.History) - n
	for i := len(g.History) - 1; i >= keep; i-- {
		step := g.History[i]
		captured := Empty
		if step.Captured != "" {
			piece, ok := parsePiece(step.Captured[0])
			if !ok || len(step.Captured) != 1 {
				return ErrInvalidState
			}
			captured = piece
		}
		if step.From < 0 || step.From >= Squares || step.To < 0 || step.To >= Squares ||
			pos.board[step.To] == Empty || pos.board[step.From] != Empty || pos.board[step.To].Color() == pos.turn {
			return ErrInvalidState
		}
		pos.unmakeMove(Move{From: step.From, To: step.To}, captured)
	}
	// 撤回之后没有吃子的步数就是被撤回的第一步之前的那个数。
	quiet := g.History[keep].Quiet
	history := g.History[:keep:keep]
	var last *Move
	if keep > 0 {
		last = &Move{From: history[keep-1].From, To: history[keep-1].To}
	}
	g.Board, g.Turn, g.Plies, g.Quiet, g.Last, g.History = pos.Board(), pos.turn, g.Plies-n, quiet, last, history
	return nil
}
