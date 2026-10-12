package controller

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
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
	col, err := connect4.BestMove(game, level, rng)
	if err != nil {
		return nil, err
	}
	return common.Marshal(connect4Move{Col: &col})
}
