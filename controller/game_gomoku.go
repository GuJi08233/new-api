package controller

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gomoku"
)

// 五子棋对战：15×15 自由规则，连成五子或以上获胜，下满为和。先走的一方执黑。规则与 AI 见 pkg/gomoku。
const gameGomoku = "gomoku"

func init() {
	registerPvpGame(gameGomoku, "五子棋", gomokuEngine{})
}

// boardAiRandom 为棋类(五子棋、四子棋、黑白棋)AI 的一步准备随机数：AI 在几步差不多好的棋里随机挑一步，种子来自 crypto/rand，
// 玩家猜不到它会走哪一步，也背不下一条稳赢的棋谱。测试里换成固定种子。
var boardAiRandom = func() (*rand.Rand, error) {
	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return nil, err
	}
	return rand.New(rand.NewChaCha8(seed)), nil
}

// gomokuState 是房间或人机对局里保存的一局：执黑(先走)的座位与按顺序的全部落子(格子编号 y*15+x)。局面每次由规则包重放得出，
// 重放同时校验了保存的棋谱。
type gomokuState struct {
	Black int   `json:"black"`
	Moves []int `json:"moves"`
	boardAssist
}

// gomokuMove 是一步落子 {"x":0-14,"y":0-14}。字段用指针，缺了哪个都不是合法的一步，不会被当成 0。
type gomokuMove struct {
	X *int `json:"x"`
	Y *int `json:"y"`
}

// gomokuView 是给玩家看的局面。board 按行从上到下、每行从左到右 225 个字符('.' 空、'b' 黑、'w' 白)；black 是执黑的座位；
// to_move、winner 是座位，没有时为 -1(winner 在 over 时为 -1 表示和棋)；line 是获胜连线，last 是最后一手。
type gomokuView struct {
	Size   int            `json:"size"`
	Board  string         `json:"board"`
	Black  int            `json:"black"`
	ToMove int            `json:"to_move"`
	Moves  int            `json:"moves"`
	Last   *gomoku.Point  `json:"last"`
	Over   bool           `json:"over"`
	Winner int            `json:"winner"`
	Line   []gomoku.Point `json:"line"`
	Undo   boardUndoView  `json:"undo"`
}

type gomokuEngine struct{}

// loadGomoku 读出保存的一局并重放。
func loadGomoku(data []byte) (gomokuState, *gomoku.Game, error) {
	var state gomokuState
	if err := common.Unmarshal(data, &state); err != nil {
		return state, nil, err
	}
	if state.Black != 0 && state.Black != 1 {
		return state, nil, fmt.Errorf("invalid gomoku black seat %d", state.Black)
	}
	game, err := gomoku.Replay(state.Moves)
	if err != nil {
		return state, nil, fmt.Errorf("invalid gomoku record: %w", err)
	}
	return state, game, nil
}

// seatOf 返回执 stone 的座位，Empty 返回 -1。
func (s gomokuState) seatOf(stone gomoku.Stone) int {
	switch stone {
	case gomoku.Black:
		return s.Black
	case gomoku.White:
		return 1 - s.Black
	}
	return -1
}

func gomokuPvpState(state gomokuState, game *gomoku.Game) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.seatOf(game.Turn()), Over: game.Over(), Winner: state.seatOf(game.Winner())}, nil
}

func (gomokuEngine) New(first int) (pvpState, error) {
	if first != 0 && first != 1 {
		return pvpState{}, fmt.Errorf("invalid first seat %d", first)
	}
	return gomokuPvpState(gomokuState{Black: first, Moves: []int{}}, gomoku.New())
}

func (gomokuEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return pvpState{}, err
	}
	var m gomokuMove
	if err := common.Unmarshal(move, &m); err != nil || m.X == nil || m.Y == nil {
		return pvpState{}, errPvpIllegalMove
	}
	if state.seatOf(game.Turn()) != seat || game.Play(*m.X, *m.Y) != nil {
		return pvpState{}, errPvpIllegalMove
	}
	state.Moves = game.Moves()
	state.played()
	return gomokuPvpState(state, game)
}

func (gomokuEngine) View(data []byte, seat int) (any, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return nil, err
	}
	view := gomokuView{
		Size:   gomoku.Size,
		Board:  game.Board(),
		Black:  state.Black,
		ToMove: state.seatOf(game.Turn()),
		Moves:  len(state.Moves),
		Over:   game.Over(),
		Winner: state.seatOf(game.Winner()),
		Line:   append([]gomoku.Point{}, game.Line()...),
		Undo:   state.undoView(state.movers(), seat, game.Over()),
	}
	if last, ok := game.Last(); ok {
		view.Last = &last
	}
	return view, nil
}

// Timeout 判轮到走的一方超时输。
func (gomokuEngine) Timeout(data []byte) (int, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return -1, err
	}
	if game.Over() {
		return state.seatOf(game.Winner()), nil
	}
	return 1 - state.seatOf(game.Turn()), nil
}

func (gomokuEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	return (gomokuEngine{}).AiMoveContext(context.Background(), data, seat, level)
}

func (gomokuEngine) AiMoveContext(ctx context.Context, data []byte, seat int, level int) (json.RawMessage, error) {
	state, game, err := loadGomoku(data)
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
	point, err := gomoku.BestMoveContext(ctx, game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(point)
}

// movers 返回每一步落子的座位：黑先，双方轮流。
func (s gomokuState) movers() []int {
	movers := make([]int, len(s.Moves))
	for i := range movers {
		movers[i] = s.Black
		if i%2 == 1 {
			movers[i] = 1 - s.Black
		}
	}
	return movers
}

// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收的游戏币。
func (gomokuEngine) UndoAsk(data []byte, seat int) (pvpState, model.GameCoins, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return pvpState{}, 0, err
	}
	cost, err := state.askUndo(state.movers(), seat, game.Over())
	if err != nil {
		return pvpState{}, 0, err
	}
	next, err := gomokuPvpState(state, game)
	return next, cost, err
}

// UndoAnswer 让 seat 回应对方的悔棋请求：同意时撤回到提出的一方最后一次落子之前。
func (gomokuEngine) UndoAnswer(data []byte, seat int, accept bool) (pvpUndo, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return pvpUndo{}, err
	}
	asker, steps, cost, err := state.answerUndo(state.movers(), seat, accept, game.Over())
	if err != nil {
		return pvpUndo{}, err
	}
	if steps > 0 {
		state.Moves = state.Moves[:len(state.Moves)-steps]
		if game, err = gomoku.Replay(state.Moves); err != nil {
			return pvpUndo{}, err
		}
	} else {
		asker = -1
	}
	next, err := gomokuPvpState(state, game)
	return pvpUndo{State: next, Payer: asker, Cost: cost, Steps: steps}, err
}

func (gomokuEngine) HintStatus(data []byte, seat int) (pvpHintStatus, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return pvpHintStatus{}, err
	}
	return state.hintStatus(seat, state.seatOf(game.Turn()), game.Over())
}

func (gomokuEngine) CachedHint(data []byte, seat int) (json.RawMessage, error) {
	state, game, err := loadGomoku(data)
	if err != nil {
		return nil, err
	}
	return state.cachedHint(seat, state.seatOf(game.Turn()), game.Over(), len(state.Moves))
}

func (gomokuEngine) Hint(data []byte, seat int) (pvpHint, error) {
	return (gomokuEngine{}).HintContext(context.Background(), data, seat)
}

// HintContext 用困难难度的 AI 替 seat 找一步好棋，记下用掉的提示次数。提示不落子。
func (gomokuEngine) HintContext(ctx context.Context, data []byte, seat int) (pvpHint, error) {
	state, game, err := loadGomoku(data)
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
	point, err := gomoku.BestMoveContext(ctx, game, boardHintLevel, rng)
	if err != nil {
		return pvpHint{}, err
	}
	content, cost, err := state.recordHint(boardHint{Moves: len(state.Moves), X: point.X, Y: point.Y})
	if err != nil {
		return pvpHint{}, err
	}
	next, err := gomokuPvpState(state, game)
	return pvpHint{Hint: content, State: next, Cost: cost}, err
}
