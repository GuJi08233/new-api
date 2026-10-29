package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dots"
)

// 点格棋对战：4×4 个格子，双方轮流连边，连上格子的第四条边就占下它并接着走，格子多的一方获胜。规则与 AI 见 pkg/dots。
const gameDots = "dots"

func init() {
	registerPvpGame(gameDots, "点格棋", dotsEngine{})
}

// dotsState 是房间或人机对局里保存的一局：先走的座位与按顺序连过的边。局面每次由规则包重放得出。
type dotsState struct {
	First int   `json:"first"`
	Moves []int `json:"moves"`
	boardAssist
}

// dotsMove 是一步 {"edge":边的编号}。
type dotsMove struct {
	Edge *int `json:"edge"`
}

// dotsView 是给玩家看的局面。edges 是 dots.Edges 个字符('1' 连上、'0' 没连)，owners 是 dots.Boxes 个字符('a' 先手占、
// 'b' 后手占、'.' 没人占)；first 是先走的座位；scores 按座位排列；last 是最后一条边(没有时为 -1)，claimed 是最后一步围下的格子。
type dotsView struct {
	Size    int           `json:"size"`
	Edges   string        `json:"edges"`
	Owners  string        `json:"owners"`
	First   int           `json:"first"`
	ToMove  int           `json:"to_move"`
	Moves   int           `json:"moves"`
	Last    int           `json:"last"`
	Claimed []int         `json:"claimed"`
	Scores  [2]int        `json:"scores"`
	Over    bool          `json:"over"`
	Winner  int           `json:"winner"`
	Undo    boardUndoView `json:"undo"`
}

type dotsEngine struct{}

func loadDots(data []byte) (dotsState, *dots.Game, error) {
	var state dotsState
	if err := common.Unmarshal(data, &state); err != nil {
		return state, nil, err
	}
	if state.First != 0 && state.First != 1 {
		return state, nil, fmt.Errorf("invalid dots first seat %d", state.First)
	}
	game, err := dots.Replay(state.Moves)
	if err != nil {
		return state, nil, fmt.Errorf("invalid dots record: %w", err)
	}
	return state, game, nil
}

// seatOf 返回 player 一方的座位，Nobody 返回 -1。
func (s dotsState) seatOf(player dots.Player) int {
	switch player {
	case dots.First:
		return s.First
	case dots.Second:
		return 1 - s.First
	}
	return -1
}

// movers 返回每一条边是哪个座位连的：围下格子的一方接着走，所以要从开局重放一遍看每一步轮到谁。
func (s dotsState) movers() []int {
	movers := make([]int, 0, len(s.Moves))
	game := dots.New()
	for _, edge := range s.Moves {
		movers = append(movers, s.seatOf(game.Turn()))
		if game.Play(edge) != nil {
			break
		}
	}
	return movers
}

func dotsPvpState(state dotsState, game *dots.Game) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.seatOf(game.Turn()), Over: game.Over(), Winner: state.seatOf(game.Winner())}, nil
}

func (dotsEngine) New(first int) (pvpState, error) {
	if first != 0 && first != 1 {
		return pvpState{}, fmt.Errorf("invalid first seat %d", first)
	}
	return dotsPvpState(dotsState{First: first, Moves: []int{}}, dots.New())
}

func (dotsEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return pvpState{}, err
	}
	var m dotsMove
	if err := common.Unmarshal(move, &m); err != nil || m.Edge == nil {
		return pvpState{}, errPvpIllegalMove
	}
	if state.seatOf(game.Turn()) != seat || game.Play(*m.Edge) != nil {
		return pvpState{}, errPvpIllegalMove
	}
	state.Moves = game.Moves()
	state.played()
	return dotsPvpState(state, game)
}

func (dotsEngine) View(data []byte, seat int) (any, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return nil, err
	}
	first, second := game.Score()
	view := dotsView{
		Size:    dots.Size,
		Edges:   game.EdgeString(),
		Owners:  game.Owners(),
		First:   state.First,
		ToMove:  state.seatOf(game.Turn()),
		Moves:   len(state.Moves),
		Last:    game.Last(),
		Claimed: game.Claimed(),
		Over:    game.Over(),
		Winner:  state.seatOf(game.Winner()),
		Undo:    state.undoView(state.movers(), seat, game.Over()),
	}
	view.Scores[state.First], view.Scores[1-state.First] = first, second
	return view, nil
}

// Timeout 判轮到走的一方超时输。
func (dotsEngine) Timeout(data []byte) (int, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return -1, err
	}
	if game.Over() {
		return state.seatOf(game.Winner()), nil
	}
	return 1 - state.seatOf(game.Turn()), nil
}

func (dotsEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	return (dotsEngine{}).AiMoveContext(context.Background(), data, seat, level)
}

func (dotsEngine) AiMoveContext(ctx context.Context, data []byte, seat int, level int) (json.RawMessage, error) {
	state, game, err := loadDots(data)
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
	edge, err := dots.BestMoveContext(ctx, game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(dotsMove{Edge: &edge})
}

// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收的游戏币。
func (dotsEngine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	cost, err := state.askUndo(state.movers(), seat, game.Over())
	if err != nil {
		return pvpState{}, 0, err
	}
	next, err := dotsPvpState(state, game)
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回到提出的一方最后一次连边之前，期间对方连走的几步一并撤回。
func (dotsEngine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return pvpUndo{}, err
	}
	asker, steps, cost, err := state.answerUndo(state.movers(), seat, accept, game.Over())
	if err != nil {
		return pvpUndo{}, err
	}
	if steps > 0 {
		state.Moves = state.Moves[:len(state.Moves)-steps]
		if game, err = dots.Replay(state.Moves); err != nil {
			return pvpUndo{}, err
		}
	} else {
		asker = -1
	}
	next, err := dotsPvpState(state, game)
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (dotsEngine) HintStatus(data []byte, seat int) (pvpHintStatus, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return pvpHintStatus{}, err
	}
	return state.hintStatus(seat, state.seatOf(game.Turn()), game.Over())
}

func (dotsEngine) CachedHint(data []byte, seat int) (json.RawMessage, error) {
	state, game, err := loadDots(data)
	if err != nil {
		return nil, err
	}
	return state.cachedHint(seat, state.seatOf(game.Turn()), game.Over(), len(state.Moves))
}

func (dotsEngine) Hint(data []byte, seat int) (pvpHint, error) {
	return (dotsEngine{}).HintContext(context.Background(), data, seat)
}

// HintContext 用困难难度的 AI 替 seat 选一条边，记下用掉的提示次数。提示不连边；x 是推荐的边。
func (dotsEngine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	state, game, err := loadDots(data)
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
	edge, err := dots.BestMoveContext(ctx, game, boardHintLevel, rng)
	if err != nil {
		return pvpHint{}, err
	}
	content, cost, err := state.recordHint(boardHint{Moves: len(state.Moves), X: edge})
	if err != nil {
		return pvpHint{}, err
	}
	next, err := dotsPvpState(state, game)
	return pvpHint{Hint: content, State: next, Cost: cost}, err
}
