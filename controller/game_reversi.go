package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/reversi"
)

// 黑白棋对战：8×8 标准开局，先走的一方执黑；无子可下时自动跳过，双方都无子可下时子多者胜。规则与 AI 见 pkg/reversi，
// AI 的随机数见 boardAiRandom。
const gameReversi = "reversi"

func init() {
	registerPvpGame(gameReversi, "黑白棋", reversiEngine{})
}

// reversiState 是房间或人机对局里保存的一局：执黑(先走)的座位与按顺序的全部落子(格子编号 y*8+x)，跳过不用记。
// 局面每次由规则包重放得出。
type reversiState struct {
	Black int   `json:"black"`
	Moves []int `json:"moves"`
	boardAssist
}

// reversiMove 是一步落子 {"x":0-7,"y":0-7}，不需要 pass：无子可下的一方由引擎自动跳过。
type reversiMove struct {
	X *int `json:"x"`
	Y *int `json:"y"`
}

// reversiView 是给玩家看的局面。board 按行从上到下、每行从左到右 64 个字符('.' 空、'b' 黑、'w' 白)；black 是执黑的座位；
// to_move、winner、passed 是座位，没有时为 -1(winner 在 over 时为 -1 表示和棋)；last 是最后一手，flipped 是它翻转的棋子，
// passed 是这一手之后因为无子可下被跳过的一方；legal 是轮到走的一方能下的格子。
type reversiView struct {
	Size       int             `json:"size"`
	Board      string          `json:"board"`
	Black      int             `json:"black"`
	ToMove     int             `json:"to_move"`
	Moves      int             `json:"moves"`
	Last       *reversi.Point  `json:"last"`
	Flipped    []reversi.Point `json:"flipped"`
	Passed     int             `json:"passed"`
	Legal      []reversi.Point `json:"legal"`
	BlackDiscs int             `json:"black_discs"`
	WhiteDiscs int             `json:"white_discs"`
	Over       bool            `json:"over"`
	Winner     int             `json:"winner"`
	Undo       boardUndoView   `json:"undo"`
}

type reversiEngine struct{}

// loadReversi 读出保存的一局并重放。
func loadReversi(data []byte) (reversiState, *reversi.Game, error) {
	var state reversiState
	if err := common.Unmarshal(data, &state); err != nil {
		return state, nil, err
	}
	if state.Black != 0 && state.Black != 1 {
		return state, nil, fmt.Errorf("invalid reversi black seat %d", state.Black)
	}
	game, err := reversi.Replay(state.Moves)
	if err != nil {
		return state, nil, fmt.Errorf("invalid reversi record: %w", err)
	}
	return state, game, nil
}

// seatOf 返回执 disc 的座位，Empty 返回 -1。
func (s reversiState) seatOf(disc reversi.Disc) int {
	switch disc {
	case reversi.Black:
		return s.Black
	case reversi.White:
		return 1 - s.Black
	}
	return -1
}

func reversiPvpState(state reversiState, game *reversi.Game) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.seatOf(game.Turn()), Over: game.Over(), Winner: state.seatOf(game.Winner())}, nil
}

func (reversiEngine) New(first int) (pvpState, error) {
	if first != 0 && first != 1 {
		return pvpState{}, fmt.Errorf("invalid first seat %d", first)
	}
	return reversiPvpState(reversiState{Black: first, Moves: []int{}}, reversi.New())
}

func (reversiEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return pvpState{}, err
	}
	var m reversiMove
	if err := common.Unmarshal(move, &m); err != nil || m.X == nil || m.Y == nil {
		return pvpState{}, errPvpIllegalMove
	}
	if state.seatOf(game.Turn()) != seat || game.Play(*m.X, *m.Y) != nil {
		return pvpState{}, errPvpIllegalMove
	}
	state.Moves = game.Moves()
	state.played()
	return reversiPvpState(state, game)
}

func (reversiEngine) View(data []byte, seat int) (any, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return nil, err
	}
	black, white := game.Count()
	view := reversiView{
		Size:       reversi.Size,
		Board:      game.Board(),
		Black:      state.Black,
		ToMove:     state.seatOf(game.Turn()),
		Moves:      len(state.Moves),
		Flipped:    game.Flipped(),
		Passed:     state.seatOf(game.Passed()),
		Legal:      game.Legal(),
		BlackDiscs: black,
		WhiteDiscs: white,
		Over:       game.Over(),
		Winner:     state.seatOf(game.Winner()),
		Undo:       state.undoView(state.movers(), seat, game.Over()),
	}
	if last, ok := game.Last(); ok {
		view.Last = &last
	}
	return view, nil
}

// Timeout 判轮到走的一方超时输。
func (reversiEngine) Timeout(data []byte) (int, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return -1, err
	}
	if game.Over() {
		return state.seatOf(game.Winner()), nil
	}
	return 1 - state.seatOf(game.Turn()), nil
}

func (reversiEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	return (reversiEngine{}).AiMoveContext(context.Background(), data, seat, level)
}

func (reversiEngine) AiMoveContext(ctx context.Context, data []byte, seat int, level int) (json.RawMessage, error) {
	state, game, err := loadReversi(data)
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
	point, err := reversi.BestMoveContext(ctx, game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(point)
}

// movers 返回每一步落子的座位。一方无子可下时被跳过，对方会连走几步，所以要从开局重放一遍看每一步轮到谁；保存的棋谱
// 读出时已经重放校验过。
func (s reversiState) movers() []int {
	movers := make([]int, 0, len(s.Moves))
	game := reversi.New()
	for _, square := range s.Moves {
		movers = append(movers, s.seatOf(game.Turn()))
		if game.Play(square%reversi.Size, square/reversi.Size) != nil {
			break
		}
	}
	return movers
}

// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收的游戏币。
func (reversiEngine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	cost, err := state.askUndo(state.movers(), seat, game.Over())
	if err != nil {
		return pvpState{}, 0, err
	}
	next, err := reversiPvpState(state, game)
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回到提出的一方最后一次落子之前。
func (reversiEngine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return pvpUndo{}, err
	}
	asker, steps, cost, err := state.answerUndo(state.movers(), seat, accept, game.Over())
	if err != nil {
		return pvpUndo{}, err
	}
	if steps > 0 {
		state.Moves = state.Moves[:len(state.Moves)-steps]
		if game, err = reversi.Replay(state.Moves); err != nil {
			return pvpUndo{}, err
		}
	} else {
		asker = -1
	}
	next, err := reversiPvpState(state, game)
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (reversiEngine) HintStatus(data []byte, seat int) (pvpHintStatus, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return pvpHintStatus{}, err
	}
	return state.hintStatus(seat, state.seatOf(game.Turn()), game.Over())
}

func (reversiEngine) CachedHint(data []byte, seat int) (json.RawMessage, error) {
	state, game, err := loadReversi(data)
	if err != nil {
		return nil, err
	}
	return state.cachedHint(seat, state.seatOf(game.Turn()), game.Over(), len(state.Moves))
}

func (reversiEngine) Hint(data []byte, seat int) (pvpHint, error) {
	return (reversiEngine{}).HintContext(context.Background(), data, seat)
}

// HintContext 用困难难度的 AI 替 seat 找一步好棋，记下用掉的提示次数。提示不落子。
func (reversiEngine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	state, game, err := loadReversi(data)
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
	point, err := reversi.BestMoveContext(ctx, game, boardHintLevel, rng)
	if err != nil {
		return pvpHint{}, err
	}
	content, cost, err := state.recordHint(boardHint{Moves: len(state.Moves), X: point.X, Y: point.Y})
	if err != nil {
		return pvpHint{}, err
	}
	next, err := reversiPvpState(state, game)
	return pvpHint{Hint: content, State: next, Cost: cost}, err
}
