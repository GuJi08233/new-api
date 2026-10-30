package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mancala"
)

// 播棋对战：两侧各六个坑，轮流播子，落进自己的大仓再走一次，落进自己的空坑吃掉对面，大仓子多的一方获胜。规则与 AI 见
// pkg/mancala。
const gameMancala = "mancala"

func init() {
	registerPvpGame(gameMancala, "播棋", mancalaEngine{})
}

// mancalaState 是房间或人机对局里保存的一局：先走的座位与依次选的坑。局面每次由规则包重放得出。
type mancalaState struct {
	First int   `json:"first"`
	Moves []int `json:"moves"`
	boardAssist
}

// mancalaMove 是一步 {"pit":自己一侧的第几个坑(0..5)}。
type mancalaMove struct {
	Pit *int `json:"pit"`
}

// mancalaView 是给玩家看的局面。board 是 mancala.Holes 个坑的子数(0..5 先手小坑、6 先手大仓、7..12 后手小坑、13 后手大仓)；
// first 是先走的座位；scores 按座位排列；last_pit 是最后一步选的坑(没有时为 -1)，last 是这一步依次落子的坑，captured 是
// 这一步吃掉的对面坑(没有时为 -1)，extra 表示这一步落进自己的大仓、同一方接着走。
type mancalaView struct {
	Board    []int         `json:"board"`
	First    int           `json:"first"`
	ToMove   int           `json:"to_move"`
	Moves    int           `json:"moves"`
	LastPit  int           `json:"last_pit"`
	Last     []int         `json:"last"`
	Captured int           `json:"captured"`
	Extra    bool          `json:"extra"`
	Scores   [2]int        `json:"scores"`
	Over     bool          `json:"over"`
	Winner   int           `json:"winner"`
	Undo     boardUndoView `json:"undo"`
}

type mancalaEngine struct{}

func loadMancala(data []byte) (mancalaState, *mancala.Game, error) {
	var state mancalaState
	if err := common.Unmarshal(data, &state); err != nil {
		return state, nil, err
	}
	if state.First != 0 && state.First != 1 {
		return state, nil, fmt.Errorf("invalid mancala first seat %d", state.First)
	}
	game, err := mancala.Replay(state.Moves)
	if err != nil {
		return state, nil, fmt.Errorf("invalid mancala record: %w", err)
	}
	return state, game, nil
}

// seatOf 返回 player 一方的座位，Nobody 返回 -1。
func (s mancalaState) seatOf(player mancala.Player) int {
	switch player {
	case mancala.First:
		return s.First
	case mancala.Second:
		return 1 - s.First
	}
	return -1
}

// movers 返回每一步是哪个座位走的：落进自己大仓的一方接着走，所以要从开局重放一遍看每一步轮到谁。
func (s mancalaState) movers() []int {
	movers := make([]int, 0, len(s.Moves))
	game := mancala.New()
	for _, pit := range s.Moves {
		movers = append(movers, s.seatOf(game.Turn()))
		if game.Play(pit) != nil {
			break
		}
	}
	return movers
}

func mancalaPvpState(state mancalaState, game *mancala.Game) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.seatOf(game.Turn()), Over: game.Over(), Winner: state.seatOf(game.Winner())}, nil
}

func (mancalaEngine) New(first int) (pvpState, error) {
	if first != 0 && first != 1 {
		return pvpState{}, fmt.Errorf("invalid first seat %d", first)
	}
	return mancalaPvpState(mancalaState{First: first, Moves: []int{}}, mancala.New())
}

func (mancalaEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return pvpState{}, err
	}
	var m mancalaMove
	if err := common.Unmarshal(move, &m); err != nil || m.Pit == nil {
		return pvpState{}, errPvpIllegalMove
	}
	if state.seatOf(game.Turn()) != seat || game.Play(*m.Pit) != nil {
		return pvpState{}, errPvpIllegalMove
	}
	state.Moves = game.Moves()
	state.played()
	return mancalaPvpState(state, game)
}

func (mancalaEngine) View(data []byte, seat int) (any, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return nil, err
	}
	first, second := game.Score()
	lastPit := -1
	if n := len(state.Moves); n > 0 {
		lastPit = state.Moves[n-1]
	}
	view := mancalaView{
		Board:    game.Board(),
		First:    state.First,
		ToMove:   state.seatOf(game.Turn()),
		Moves:    len(state.Moves),
		LastPit:  lastPit,
		Last:     game.Last(),
		Captured: game.Captured(),
		Extra:    game.Extra(),
		Over:     game.Over(),
		Winner:   state.seatOf(game.Winner()),
		Undo:     state.undoView(state.movers(), seat, game.Over()),
	}
	view.Scores[state.First], view.Scores[1-state.First] = first, second
	return view, nil
}

// Timeout 判轮到走的一方超时输。
func (mancalaEngine) Timeout(data []byte) (int, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return -1, err
	}
	if game.Over() {
		return state.seatOf(game.Winner()), nil
	}
	return 1 - state.seatOf(game.Turn()), nil
}

func (mancalaEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	return (mancalaEngine{}).AiMoveContext(context.Background(), data, seat, level)
}

func (mancalaEngine) AiMoveContext(ctx context.Context, data []byte, seat int, level int) (json.RawMessage, error) {
	state, game, err := loadMancala(data)
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
	pit, err := mancala.BestMoveContext(ctx, game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(mancalaMove{Pit: &pit})
}

// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收的游戏币。
func (mancalaEngine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	cost, err := state.askUndo(state.movers(), seat, game.Over())
	if err != nil {
		return pvpState{}, 0, err
	}
	next, err := mancalaPvpState(state, game)
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回到提出的一方最后一次走子之前，期间对方连走的几步一并撤回。
func (mancalaEngine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return pvpUndo{}, err
	}
	asker, steps, cost, err := state.answerUndo(state.movers(), seat, accept, game.Over())
	if err != nil {
		return pvpUndo{}, err
	}
	if steps > 0 {
		state.Moves = state.Moves[:len(state.Moves)-steps]
		if game, err = mancala.Replay(state.Moves); err != nil {
			return pvpUndo{}, err
		}
	} else {
		asker = -1
	}
	next, err := mancalaPvpState(state, game)
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (mancalaEngine) HintStatus(data []byte, seat int) (pvpHintStatus, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return pvpHintStatus{}, err
	}
	return state.hintStatus(seat, state.seatOf(game.Turn()), game.Over())
}

func (mancalaEngine) CachedHint(data []byte, seat int) (json.RawMessage, error) {
	state, game, err := loadMancala(data)
	if err != nil {
		return nil, err
	}
	return state.cachedHint(seat, state.seatOf(game.Turn()), game.Over(), len(state.Moves))
}

func (mancalaEngine) Hint(data []byte, seat int) (pvpHint, error) {
	return (mancalaEngine{}).HintContext(context.Background(), data, seat)
}

// HintContext 用困难难度的 AI 替 seat 选一个坑，记下用掉的提示次数。提示不走子；x 是推荐的坑。
func (mancalaEngine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	state, game, err := loadMancala(data)
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
	pit, err := mancala.BestMoveContext(ctx, game, boardHintLevel, rng)
	if err != nil {
		return pvpHint{}, err
	}
	content, cost, err := state.recordHint(boardHint{Moves: len(state.Moves), X: pit})
	if err != nil {
		return pvpHint{}, err
	}
	next, err := mancalaPvpState(state, game)
	return pvpHint{Hint: content, State: next, Cost: cost}, err
}
