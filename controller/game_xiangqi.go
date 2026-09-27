package controller

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/xiangqi"
)

const gameXiangqi = "xiangqi"

func init() {
	registerPvpGame(gameXiangqi, "中国象棋", xiangqiEngine{})
}

// xiangqiEngine 是中国象棋的对战规则引擎：先走的座位执红。规则与电脑棋手在 pkg/xiangqi，这里只负责座位与红黑两方的对应，
// 以及走法、视图在接口里的格式。
type xiangqiEngine struct{}

// xiangqiState 是房间或人机对局里保存的状态：一局棋与执红的座位。
type xiangqiState struct {
	RedSeat int          `json:"red_seat"`
	Game    xiangqi.Game `json:"game"`
}

// xiangqiMove 是一步棋在接口里的样子：{"from":[x,y],"to":[x,y]}。x 是列 0-8，y 是行 0-9，红方在 y 0-4 一侧(见 pkg/xiangqi)。
type xiangqiMove struct {
	From []int `json:"from"`
	To   []int `json:"to"`
}

// xiangqiView 是给一个座位看的棋局。象棋没有隐藏信息，只有轮到看的人走时才附上他的全部合法着法，方便页面提示落点。
type xiangqiView struct {
	// Board 是 10 行棋盘，Board[y] 的第 x 个字符是 (x,y) 上的棋子：红方大写、黑方小写，K 帅/将、A 仕/士、B 相/象、N 马、
	// R 车、C 炮、P 兵/卒，"." 是空位。
	Board   []string `json:"board"`
	RedSeat int      `json:"red_seat"`
	// Turn 是轮到的一方 red 或 black；ToMove 是轮到的座位，结束后为 -1。
	Turn   string `json:"turn"`
	ToMove int    `json:"to_move"`
	// Last 是最后一手，开局时为 null。
	Last *xiangqiMove `json:"last"`
	// Check 表示轮到走的一方正被将军(将死时也是 true)。
	Check bool `json:"check"`
	// Legal 是看的人的全部合法着法，只在轮到他走时给出，否则为空数组。
	Legal []xiangqiMove `json:"legal"`
	// Plies 是已经走的步数(双方合计)，达到 300 判和；Quiet 是连续没有吃子的步数，达到 120 判和。
	Plies int  `json:"plies"`
	Quiet int  `json:"quiet"`
	Over  bool `json:"over"`
	// Winner 是赢家座位，和棋或没结束时为 -1；Result 是结束的原因：checkmate 将死、stalemate 困毙、no_capture 与
	// move_limit 和棋，没结束时为空。
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
	return state.pvp()
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
		Plies:   state.Game.Plies,
		Quiet:   state.Game.Quiet,
		Over:    state.Game.Over(),
		Result:  string(state.Game.Result),
	}
	view.ToMove, view.Winner = state.seats()
	for y := range view.Board {
		view.Board[y] = state.Game.Board[y*xiangqi.Width : (y+1)*xiangqi.Width]
	}
	if state.Game.Turn == xiangqi.Black {
		view.Turn = "black"
	}
	if state.Game.Last != nil {
		last := xiangqiMoveView(*state.Game.Last)
		view.Last = &last
	}
	if seat >= 0 && seat == view.ToMove {
		for _, m := range pos.LegalMoves() {
			view.Legal = append(view.Legal, xiangqiMoveView(m))
		}
	}
	return view, nil
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
