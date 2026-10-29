package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/connect4"
)

// 四子棋对战：7 列 × 6 行，棋子落到这一列最低的空位，连成 4 子获胜，下满为和。先走的一方执红。规则与 AI 见 pkg/connect4，
// AI 的随机数见 boardAiRandom。
const gameConnect4 = "connect4"

func init() {
	registerPvpGame(gameConnect4, "四子棋", connect4Engine{})
}

// connect4State 是房间或人机对局里保存的一局：先走(执红)的座位与按顺序投下的列号。局面每次由规则包重放得出。
type connect4State struct {
	First int   `json:"first"`
	Moves []int `json:"moves"`
	boardAssist
}

// connect4Move 是一步 {"col":0-6}。
type connect4Move struct {
	Col *int `json:"col"`
}

// connect4View 是给玩家看的局面。board 按行从上到下、每行从左到右 42 个字符('.' 空、'r' 先手的红子、'y' 后手的黄子)；
// first 是执红的座位；to_move、winner 是座位，没有时为 -1(winner 在 over 时为 -1 表示和棋)；last、line 的 row 从最上面一行数起。
type connect4View struct {
	Cols   int             `json:"cols"`
	Rows   int             `json:"rows"`
	Board  string          `json:"board"`
	First  int             `json:"first"`
	ToMove int             `json:"to_move"`
	Moves  int             `json:"moves"`
	Last   *connect4.Cell  `json:"last"`
	Over   bool            `json:"over"`
	Winner int             `json:"winner"`
	Line   []connect4.Cell `json:"line"`
	Undo   boardUndoView   `json:"undo"`
}

type connect4Engine struct{}

// loadConnect4 读出保存的一局并重放。
func loadConnect4(data []byte) (connect4State, *connect4.Game, error) {
	var state connect4State
	if err := common.Unmarshal(data, &state); err != nil {
		return state, nil, err
	}
	if state.First != 0 && state.First != 1 {
		return state, nil, fmt.Errorf("invalid connect4 first seat %d", state.First)
	}
	game, err := connect4.Replay(state.Moves)
	if err != nil {
		return state, nil, fmt.Errorf("invalid connect4 record: %w", err)
	}
	return state, game, nil
}

// seatOf 返回执 disc 的座位，Empty 返回 -1。
func (s connect4State) seatOf(disc connect4.Disc) int {
	switch disc {
	case connect4.First:
		return s.First
	case connect4.Second:
		return 1 - s.First
	}
	return -1
}

func connect4PvpState(state connect4State, game *connect4.Game) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.seatOf(game.Turn()), Over: game.Over(), Winner: state.seatOf(game.Winner())}, nil
}

func (connect4Engine) New(first int) (pvpState, error) {
	if first != 0 && first != 1 {
		return pvpState{}, fmt.Errorf("invalid first seat %d", first)
	}
	return connect4PvpState(connect4State{First: first, Moves: []int{}}, connect4.New())
}

func (connect4Engine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return pvpState{}, err
	}
	var m connect4Move
	if err := common.Unmarshal(move, &m); err != nil || m.Col == nil {
		return pvpState{}, errPvpIllegalMove
	}
	if state.seatOf(game.Turn()) != seat || game.Play(*m.Col) != nil {
		return pvpState{}, errPvpIllegalMove
	}
	state.Moves = game.Moves()
	state.played()
	return connect4PvpState(state, game)
}

func (connect4Engine) View(data []byte, seat int) (any, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return nil, err
	}
	view := connect4View{
		Cols:   connect4.Cols,
		Rows:   connect4.Rows,
		Board:  game.Board(),
		First:  state.First,
		ToMove: state.seatOf(game.Turn()),
		Moves:  len(state.Moves),
		Over:   game.Over(),
		Winner: state.seatOf(game.Winner()),
		Line:   append([]connect4.Cell{}, game.Line()...),
		Undo:   state.undoView(state.movers(), seat, game.Over()),
	}
	if last, ok := game.Last(); ok {
		view.Last = &last
	}
	return view, nil
}

// Timeout 判轮到走的一方超时输。
func (connect4Engine) Timeout(data []byte) (int, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return -1, err
	}
	if game.Over() {
		return state.seatOf(game.Winner()), nil
	}
	return 1 - state.seatOf(game.Turn()), nil
}

func (connect4Engine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	return (connect4Engine{}).AiMoveContext(context.Background(), data, seat, level)
}

func (connect4Engine) AiMoveContext(ctx context.Context, data []byte, seat int, level int) (json.RawMessage, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return nil, err
	}
	if state.seatOf(game.Turn()) != seat {
		return nil, errPvpIllegalMove
	}
	rng, err := boardAiRandom()
	if err != nil {
		return nil, err
	}
	col, err := connect4.BestMoveContext(ctx, game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(connect4Move{Col: &col})
}

// movers 返回每一步落子的座位：先手执红，双方轮流。
func (s connect4State) movers() []int {
	movers := make([]int, len(s.Moves))
	for i := range movers {
		movers[i] = s.First
		if i%2 == 1 {
			movers[i] = 1 - s.First
		}
	}
	return movers
}

// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收的游戏币。
func (connect4Engine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	cost, err := state.askUndo(state.movers(), seat, game.Over())
	if err != nil {
		return pvpState{}, 0, err
	}
	next, err := connect4PvpState(state, game)
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回到提出的一方最后一次落子之前。
func (connect4Engine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return pvpUndo{}, err
	}
	asker, steps, cost, err := state.answerUndo(state.movers(), seat, accept, game.Over())
	if err != nil {
		return pvpUndo{}, err
	}
	if steps > 0 {
		state.Moves = state.Moves[:len(state.Moves)-steps]
		if game, err = connect4.Replay(state.Moves); err != nil {
			return pvpUndo{}, err
		}
	} else {
		asker = -1
	}
	next, err := connect4PvpState(state, game)
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (connect4Engine) HintStatus(data []byte, seat int) (pvpHintStatus, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return pvpHintStatus{}, err
	}
	return state.hintStatus(seat, state.seatOf(game.Turn()), game.Over())
}

func (connect4Engine) CachedHint(data []byte, seat int) (json.RawMessage, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return nil, err
	}
	return state.cachedHint(seat, state.seatOf(game.Turn()), game.Over(), len(state.Moves))
}

func (connect4Engine) Hint(data []byte, seat int) (pvpHint, error) {
	return (connect4Engine{}).HintContext(context.Background(), data, seat)
}

// HintContext 用困难难度的 AI 替 seat 找一步好棋，记下用掉的提示次数。提示不落子。
func (connect4Engine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	state, game, err := loadConnect4(data)
	if err != nil {
		return pvpHint{}, err
	}
	if game.Over() || state.seatOf(game.Turn()) != seat {
		return pvpHint{}, errPvpHintUnavailable
	}
	rng, err := boardAiRandom()
	if err != nil {
		return pvpHint{}, err
	}
	col, err := connect4.BestMoveContext(ctx, game, boardHintLevel, rng)
	if err != nil {
		return pvpHint{}, err
	}
	content, cost, err := state.recordHint(boardHint{Moves: len(state.Moves), X: col})
	if err != nil {
		return pvpHint{}, err
	}
	next, err := connect4PvpState(state, game)
	return pvpHint{Hint: content, State: next, Cost: cost}, err
}
