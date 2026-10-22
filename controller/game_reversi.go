package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
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
