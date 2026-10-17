package controller

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/xiangqi"
)

const gameXiangqi = "xiangqi"

// 悔棋：每人每局前 gameUndoFreePlays 次免费，之后每次 gameUndoCostCoins 个游戏币。房间对局要对方同意，人机对战由电脑直接
// 同意。
const (
	gameUndoFreePlays = 3
	gameUndoCostCoins = 1
)

func init() {
	registerPvpGame(gameXiangqi, "中国象棋", xiangqiEngine{})
}

// xiangqiEngine 是中国象棋的对战规则引擎：先走的座位执红。规则与电脑棋手在 pkg/xiangqi，这里只负责座位与红黑两方的对应，
// 以及走法、视图在接口里的格式。
type xiangqiEngine struct{}

// xiangqiUndoState 是悔棋的状态：每一方已经悔过几次(前 gameUndoFreePlays 次免费)，以及在等对方回应的悔棋请求是哪个座位
// 提的。走了一步棋请求就作废。
type xiangqiUndoState struct {
	Used    [2]int `json:"used,omitempty"`
	Pending *int   `json:"pending,omitempty"`
	// Declined 是上一次被拒绝时提出悔棋的座位与当时的步数：这个座位再走一步之前不能再提，免得一直向对方发请求。
	Declined *xiangqiUndoDecline `json:"declined,omitempty"`
}

type xiangqiUndoDecline struct {
	Seat  int `json:"seat"`
	Plies int `json:"plies"`
}

// UnmarshalJSON 兼容旧存档：之前的 declined 只记了步数，没有记是谁提的。读出来当作未知座位，双方都不因此被锁住。
func (d *xiangqiUndoDecline) UnmarshalJSON(data []byte) error {
	var plies int
	if err := common.Unmarshal(data, &plies); err == nil {
		*d = xiangqiUndoDecline{Seat: -1, Plies: plies}
		return nil
	}
	type plain xiangqiUndoDecline
	return common.Unmarshal(data, (*plain)(d))
}

// xiangqiState 是房间或人机对局里保存的状态：一局棋、执红的座位与悔棋的进度。
type xiangqiState struct {
	RedSeat int              `json:"red_seat"`
	Game    xiangqi.Game     `json:"game"`
	Undo    xiangqiUndoState `json:"undo,omitempty"`
}

// xiangqiMove 是一步棋在接口里的样子：{"from":[x,y],"to":[x,y]}。x 是列 0-8，y 是行 0-9，红方在 y 0-4 一侧(见 pkg/xiangqi)。
type xiangqiMove struct {
	From []int `json:"from"`
	To   []int `json:"to"`
}

// xiangqiHistoryMove 的步数从 1 开始，棋子沿用棋盘的字母：红方大写、黑方小写。
type xiangqiHistoryMove struct {
	Ply      int    `json:"ply"`
	Color    string `json:"color"`
	Piece    string `json:"piece"`
	Notation string `json:"notation,omitempty"`
	Captured string `json:"captured,omitempty"`
	From     []int  `json:"from"`
	To       []int  `json:"to"`
}

type xiangqiLastMoves struct {
	Red   *xiangqiHistoryMove `json:"red"`
	Black *xiangqiHistoryMove `json:"black"`
}

// declinedBy 表示 seat 上一次的悔棋请求在现在这一步被拒绝了，再走一步之前不能再提。拒绝的一方不受影响。
func (s *xiangqiState) declinedBy(seat int) bool {
	decline := s.Undo.Declined
	return decline != nil && decline.Seat == seat && decline.Plies == s.Game.Plies
}

// undoFreeLeft 返回 seat 这一方还剩几次免费悔棋。
func (s *xiangqiState) undoFreeLeft(seat int) int {
	if seat < 0 || seat > 1 {
		return 0
	}
	return max(gameUndoFreePlays-s.Undo.Used[seat], 0)
}

// takeback 算出 seat 这一方悔棋要撤回几步、收多少游戏币：撤回的棋里要有自己走的，撤完之后轮到自己走。轮到自己时对方的
// 应着一并撤掉，所以一次悔棋撤一步(自己刚走完)或两步(对方已经应了)。没有自己走过的棋时 ok 为假。
func (s *xiangqiState) takeback(seat int) (int, model.GameCoins, bool) {
	mine := s.colorOf(seat)
	if mine < 0 {
		return 0, 0, false
	}
	plies := s.Game.Plies
	if plies == 0 {
		return 0, 0, false
	}
	// 第 plies-1 步是谁走的：红先黑后，步数从 0 开始，奇数步是黑方走的。
	lastMover := (plies - 1) % 2
	steps := 1
	if lastMover != mine {
		// 对方刚应了一步，连自己最后走的那一步一起撤。
		steps = 2
	}
	// 记录走法之前开始的对局，更早的棋步没有记录，撤不回来。
	if steps > plies || steps > len(s.Game.History) || (plies-steps)%2 != mine {
		return 0, 0, false
	}
	cost, err := s.undoCost(seat)
	if err != nil {
		return 0, 0, false
	}
	return steps, cost, true
}

// undoCost 是 seat 这一方下一次悔棋要付的游戏币：免费次数用完之前为 0。
func (s *xiangqiState) undoCost(seat int) (model.GameCoins, error) {
	if s.undoFreeLeft(seat) > 0 {
		return 0, nil
	}
	return model.WholeGameCoins(gameUndoCostCoins)
}

// colorOf 返回 seat 执的一方(红 0、黑 1)，seat 不是这个房间里的座位时返回 -1。
func (s *xiangqiState) colorOf(seat int) int {
	switch seat {
	case s.RedSeat:
		return int(xiangqi.Red)
	case 1 - s.RedSeat:
		return int(xiangqi.Black)
	}
	return -1
}

// xiangqiUndoView 是悔棋的状态：看的人还剩几次免费悔棋、下一次要花多少游戏币、现在能不能悔，以及有没有人在等他回应。
type xiangqiUndoView struct {
	FreeLeft  int             `json:"free_left"`
	Cost      model.GameCoins `json:"cost"`
	Available bool            `json:"available"`
	// PendingBy 是提出了悔棋、在等对方回应的座位，没有请求时为 -1。Declined 表示看的人的悔棋刚被拒绝，再走一步之前不能再提。
	PendingBy int  `json:"pending_by"`
	Declined  bool `json:"declined"`
	// Used 是看的人已经悔过几次，页面据此知道自己的悔棋被执行了(对手同意后由对手的请求执行)。
	Used int `json:"used"`
}

// xiangqiView 是给一个座位看的棋局。象棋没有隐藏信息，只有轮到看的人走时才附上他的全部合法着法，方便页面提示落点。
type xiangqiView struct {
	Undo xiangqiUndoView `json:"undo"`
	// Board 是 10 行棋盘，Board[y] 的第 x 个字符是 (x,y) 上的棋子：红方大写、黑方小写，K 帅/将、A 仕/士、B 相/象、N 马、
	// R 车、C 炮、P 兵/卒，"." 是空位。
	Board   []string `json:"board"`
	RedSeat int      `json:"red_seat"`
	// Turn 是轮到的一方 red 或 black；ToMove 是轮到的座位，结束后为 -1。
	Turn   string `json:"turn"`
	ToMove int    `json:"to_move"`
	// Last 是最后一手，开局时为 null。
	Last *xiangqiMove `json:"last"`
	// History 只包含存档中已有的走法；旧局更早的走法可能缺失，Ply 仍保留实际步数。
	History []xiangqiHistoryMove `json:"history"`
	// LastMoves 是红黑双方各自最近的一手，还没有记录的一方为 null。
	LastMoves xiangqiLastMoves `json:"last_moves"`
	// Check 表示轮到走的一方正被将军(将死时也是 true)。
	Check bool `json:"check"`
	// Legal 是看的人的全部合法着法，只在轮到他走时给出，否则为空数组。
	Legal []xiangqiMove `json:"legal"`
	// Plies 是已经走的步数(双方合计)，达到 300 判和；Quiet 是连续没有吃子的步数，达到 120 判和；Repeat 是当前局面自最近
	// 一次吃子以来出现了几次，到 3 次判局(长将负或和棋)，页面在第 2 次时提醒。
	Plies  int  `json:"plies"`
	Quiet  int  `json:"quiet"`
	Repeat int  `json:"repeat"`
	Over   bool `json:"over"`
	// Winner 是赢家座位，和棋或没结束时为 -1；Result 是结束的原因：checkmate 将死、stalemate 困毙、perpetual_check 长将
	// 判负，repetition、no_capture 与 move_limit 和棋，没结束时为空。
	Winner int    `json:"winner"`
	Result string `json:"result"`
}

func loadXiangqiState(data []byte) (*xiangqiState, error) {
	var state xiangqiState
	if err := common.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// seatOf 返回执 color 一方的座位。
func (s *xiangqiState) seatOf(color xiangqi.Color) int {
	if color == xiangqi.Red {
		return s.RedSeat
	}
	return 1 - s.RedSeat
}

// seats 返回轮到的座位与赢家座位：结束后没有人要走(-1)，没结束或和棋时没有赢家(-1)。
func (s *xiangqiState) seats() (toMove int, winner int) {
	if !s.Game.Over() {
		return s.seatOf(s.Game.Turn), -1
	}
	if s.Game.Winner == xiangqi.NoColor {
		return -1, -1
	}
	return -1, s.seatOf(s.Game.Winner)
}

func (s *xiangqiState) pvp() (pvpState, error) {
	data, err := common.Marshal(s)
	if err != nil {
		return pvpState{}, err
	}
	toMove, winner := s.seats()
	return pvpState{Data: data, ToMove: toMove, Over: s.Game.Over(), Winner: winner}, nil
}

func xiangqiMoveView(m xiangqi.Move) xiangqiMove {
	fromX, fromY := xiangqi.SquareXY(m.From)
	toX, toY := xiangqi.SquareXY(m.To)
	return xiangqiMove{From: []int{fromX, fromY}, To: []int{toX, toY}}
}

// engineMove 把接口里的一步换成引擎的格子编号，坐标不是棋盘上的一点时 ok 为假。
func (m xiangqiMove) engineMove() (xiangqi.Move, bool) {
	points := [2][]int{m.From, m.To}
	var squares [2]int
	for i, point := range points {
		if len(point) != 2 || point[0] < 0 || point[0] >= xiangqi.Width || point[1] < 0 || point[1] >= xiangqi.Height {
			return xiangqi.Move{}, false
		}
		squares[i] = xiangqi.Square(point[0], point[1])
	}
	return xiangqi.Move{From: squares[0], To: squares[1]}, true
}

func (xiangqiEngine) New(first int) (pvpState, error) {
	state := xiangqiState{RedSeat: first, Game: xiangqi.NewGame()}
	return state.pvp()
}

// Play 落子。走了棋之后之前提出、还没人回应的悔棋请求就作废了。
func (xiangqiEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return pvpState{}, err
	}
	var req xiangqiMove
	if err := common.Unmarshal(move, &req); err != nil {
		return pvpState{}, errPvpIllegalMove
	}
	m, ok := req.engineMove()
	if toMove, _ := state.seats(); !ok || toMove != seat {
		return pvpState{}, errPvpIllegalMove
	}
	if err := state.Game.Play(m); err != nil {
		if errors.Is(err, xiangqi.ErrIllegalMove) || errors.Is(err, xiangqi.ErrGameOver) {
			return pvpState{}, errPvpIllegalMove
		}
		return pvpState{}, err
	}
	state.Undo.Pending = nil
	return state.pvp()
}

// UndoAsk 记下 seat 想悔棋，等对方同意，返回同意之后要收的游戏币。没有自己走过的棋、已经有请求在等、刚被拒绝过或棋局已经
// 结束时返回错误，状态不变。
func (xiangqiEngine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	switch {
	case state.Undo.Pending != nil:
		return pvpState{}, 0, errPvpUndoPending
	case state.declinedBy(seat):
		return pvpState{}, 0, errPvpUndoDeclined
	case state.Game.Over():
		return pvpState{}, 0, errPvpUndoUnavailable
	}
	_, cost, ok := state.takeback(seat)
	if !ok {
		return pvpState{}, 0, errPvpUndoUnavailable
	}
	state.Undo.Pending = &seat
	next, err := state.pvp()
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回棋步，记下对方又用掉一次悔棋；拒绝时只清掉请求。没有对方提出的请求时
// 返回 errPvpUndoNoRequest。
func (xiangqiEngine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return pvpUndo{}, err
	}
	if state.Undo.Pending == nil || *state.Undo.Pending == seat || state.colorOf(seat) < 0 {
		return pvpUndo{}, errPvpUndoNoRequest
	}
	asker := *state.Undo.Pending
	state.Undo.Pending = nil
	if !accept {
		state.Undo.Declined = &xiangqiUndoDecline{Seat: asker, Plies: state.Game.Plies}
		next, err := state.pvp()
		return pvpUndo{State: next, Payer: -1}, err
	}
	steps, cost, ok := state.takeback(asker)
	if !ok || state.Game.Over() {
		return pvpUndo{}, errPvpUndoUnavailable
	}
	if err := state.Game.Undo(steps); err != nil {
		return pvpUndo{}, err
	}
	state.Undo.Used[asker]++
	state.Undo.Declined = nil
	next, err := state.pvp()
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (xiangqiEngine) View(data []byte, seat int) (any, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return nil, err
	}
	pos, err := state.Game.Position()
	if err != nil {
		return nil, err
	}
	view := xiangqiView{
		Board:   make([]string, xiangqi.Height),
		RedSeat: state.RedSeat,
		Turn:    "red",
		Check:   pos.InCheck(),
		Legal:   []xiangqiMove{},
		History: make([]xiangqiHistoryMove, len(state.Game.History)),
		Plies:   state.Game.Plies,
		Quiet:   state.Game.Quiet,
		Over:    state.Game.Over(),
		Result:  string(state.Game.Result),
	}
	view.ToMove, view.Winner = state.seats()
	view.Repeat, _ = state.Game.Repetitions()
	for y := range view.Board {
		view.Board[y] = state.Game.Board[y*xiangqi.Width : (y+1)*xiangqi.Width]
	}
	if state.Game.Turn == xiangqi.Black {
		view.Turn = "black"
	}
	if state.Game.Last != nil {
		last := xiangqiMoveView(*state.Game.Last)
		view.Last = &last
		// 旧存档可能只有最后一步；保留它的标记，但不补造棋谱或未知的吃子信息。
		if len(state.Game.History) == 0 {
			m := state.Game.Last
			if m.From < 0 || m.From >= xiangqi.Squares || m.To < 0 || m.To >= xiangqi.Squares || state.Game.Board[m.To] == '.' {
				return nil, xiangqi.ErrInvalidState
			}
			piece := state.Game.Board[m.To]
			entry := &xiangqiHistoryMove{
				Ply: state.Game.Plies, Color: "red", Piece: string(piece), From: last.From, To: last.To,
			}
			if piece >= 'a' && piece <= 'z' {
				entry.Color = "black"
				view.LastMoves.Black = entry
			} else {
				view.LastMoves.Red = entry
			}
		}
	}
	// 引擎为悔棋保存了走法和被吃的子，从当前棋盘倒着还原走前局面，才能正确区分前后子并生成记谱。
	// 视图直接派生自这份持久化记录，刷新或悔棋后不会残留已经撤回的走法。
	board := []byte(state.Game.Board)
	for i := len(state.Game.History) - 1; i >= 0; i-- {
		step := state.Game.History[i]
		if step.From < 0 || step.From >= xiangqi.Squares || step.To < 0 || step.To >= xiangqi.Squares ||
			board[step.To] == '.' || board[step.From] != '.' || len(step.Captured) > 1 {
			return nil, xiangqi.ErrInvalidState
		}
		piece := board[step.To]
		board[step.From], board[step.To] = piece, '.'
		if step.Captured != "" {
			board[step.To] = step.Captured[0]
		}
		notation, err := xiangqi.Notation(string(board), xiangqi.Move{From: step.From, To: step.To})
		if err != nil {
			return nil, err
		}
		move := xiangqiMoveView(xiangqi.Move{From: step.From, To: step.To})
		entry := &view.History[i]
		*entry = xiangqiHistoryMove{
			Ply: state.Game.Plies - len(state.Game.History) + i + 1, Color: "red", Piece: string(piece),
			Notation: notation, Captured: step.Captured, From: move.From, To: move.To,
		}
		if piece >= 'a' && piece <= 'z' {
			entry.Color = "black"
			if view.LastMoves.Black == nil {
				view.LastMoves.Black = entry
			}
		} else if view.LastMoves.Red == nil {
			view.LastMoves.Red = entry
		}
	}
	if seat >= 0 && seat == view.ToMove {
		for _, m := range pos.LegalMoves() {
			view.Legal = append(view.Legal, xiangqiMoveView(m))
		}
	}
	view.Undo = state.undoView(seat)
	return view, nil
}

// undoView 汇总看的人此刻能做的悔棋：还剩几次免费、下一次多少钱、能不能提，以及有没有人在等他回应。
func (s *xiangqiState) undoView(seat int) xiangqiUndoView {
	view := xiangqiUndoView{PendingBy: -1}
	if seat >= 0 && seat <= 1 {
		view.Used = s.Undo.Used[seat]
	}
	if s.Undo.Pending != nil {
		view.PendingBy = *s.Undo.Pending
	}
	view.Declined = s.declinedBy(seat)
	_, _, ok := s.takeback(seat)
	view.FreeLeft = s.undoFreeLeft(seat)
	view.Cost, _ = s.undoCost(seat)
	view.Available = ok && s.Undo.Pending == nil && !view.Declined && !s.Game.Over()
	return view
}

// Timeout 判轮到走的一方超时输。
func (xiangqiEngine) Timeout(data []byte) (int, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return -1, err
	}
	toMove, winner := state.seats()
	if toMove < 0 {
		return winner, nil
	}
	return 1 - toMove, nil
}

// AiMove 让电脑棋手为 seat 选一步。分数差不多的几步之间用密码学安全的随机数挑选。
func (xiangqiEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	state, err := loadXiangqiState(data)
	if err != nil {
		return nil, err
	}
	if toMove, _ := state.seats(); toMove != seat {
		return nil, errPvpIllegalMove
	}
	m, err := xiangqi.BestMove(&state.Game, level, func(n int) int {
		choice, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if err != nil {
			return 0
		}
		return int(choice.Int64())
	})
	if err != nil {
		return nil, err
	}
	return common.Marshal(xiangqiMoveView(m))
}
