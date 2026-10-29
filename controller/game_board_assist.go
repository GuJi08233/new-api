package controller

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// boardHintLevel 是提示用的 AI 难度：按困难难度搜索，给出的是 AI 自己认为最好的一步。
const boardHintLevel = 3

// boardAssist 是五子棋、四子棋、黑白棋共用的悔棋与提示进度，嵌在各自保存的状态里。这几种棋保存的是先手座位加按顺序的
// 落子，悔棋就是把落子序列截短再重放；费用与次数和象棋一样：每人每局前 gameUndoFreePlays 次悔棋、前 gameHintFreePlays 次
// 提示免费，之后每次各收 gameUndoCostCoins、gameHintCostCoins 个游戏币。升级前保存的对局没有这些字段，读出来是零值，
// 等于还没悔过棋、没用过提示。
type boardAssist struct {
	Undo boardUndo `json:"undo,omitempty"`
	// Hints 是人机对战里用掉了几个提示；Hint 随局面保存最近一次分析，响应丢失或刷新后仍能取回，落子与悔棋时清除。
	Hints int        `json:"hints,omitempty"`
	Hint  *boardHint `json:"hint,omitempty"`
}

// boardUndo 记下每一方悔过几次、在等对方回应的悔棋请求是哪个座位提的，以及上一次被拒绝的请求。
type boardUndo struct {
	Used    [2]int `json:"used,omitempty"`
	Pending *int   `json:"pending,omitempty"`
	// Declined 是上一次被拒绝时提出悔棋的座位与当时的落子数：这个座位再走一步之前不能再提，免得一直向对方发请求。
	Declined *boardUndoDecline `json:"declined,omitempty"`
}

type boardUndoDecline struct {
	Seat  int `json:"seat"`
	Moves int `json:"moves"`
}

// boardHint 是一次提示：分析时的落子数(局面一变提示就作废)与推荐的落点。四子棋只用 x，表示投进哪一列。
type boardHint struct {
	Moves int `json:"moves"`
	X     int `json:"x"`
	Y     int `json:"y"`
}

// boardUndoView 是看的人此刻的悔棋状态，字段与象棋的悔棋视图一致，页面共用同一套按钮。
type boardUndoView struct {
	FreeLeft  int             `json:"free_left"`
	Cost      model.GameCoins `json:"cost"`
	Available bool            `json:"available"`
	PendingBy int             `json:"pending_by"`
	Declined  bool            `json:"declined"`
	Used      int             `json:"used"`
}

// boardTakeback 返回 seat 悔棋要撤回几步：撤到 seat 最后一次落子之前，那时轮到 seat 走。对方之后的应着(黑白棋里自己
// 无子可下被跳过时可能有好几步)一并撤掉。seat 还没落过子时返回 0。movers 是每一步落子的座位。
func boardTakeback(movers []int, seat int) int {
	for i := len(movers) - 1; i >= 0; i-- {
		if movers[i] == seat {
			return len(movers) - i
		}
	}
	return 0
}

func (a *boardAssist) undoFreeLeft(seat int) int {
	if seat < 0 || seat > 1 {
		return 0
	}
	return max(gameUndoFreePlays-a.Undo.Used[seat], 0)
}

func (a *boardAssist) undoCost(seat int) (model.GameCoins, error) {
	if a.undoFreeLeft(seat) > 0 {
		return 0, nil
	}
	return model.WholeGameCoins(gameUndoCostCoins)
}

func (a *boardAssist) declinedBy(seat int, moves int) bool {
	decline := a.Undo.Declined
	return decline != nil && decline.Seat == seat && decline.Moves == moves
}

// played 在一步新的落子之后清掉在等回应的悔棋请求与旧提示。被拒绝的记录按落子数自然失效，不用清。
func (a *boardAssist) played() {
	a.Undo.Pending = nil
	a.Hint = nil
}

// askUndo 记下 seat 想悔棋，返回对方同意之后要收的游戏币。已经有请求在等、刚被拒绝过、对局已经结束或没有自己落过的子时
// 返回错误，状态不变。
func (a *boardAssist) askUndo(movers []int, seat int, over bool) (model.GameCoins, error) {
	switch {
	case seat < 0 || seat > 1:
		return 0, errPvpIllegalMove
	case a.Undo.Pending != nil:
		return 0, errPvpUndoPending
	case a.declinedBy(seat, len(movers)):
		return 0, errPvpUndoDeclined
	case over || boardTakeback(movers, seat) == 0:
		return 0, errPvpUndoUnavailable
	}
	cost, err := a.undoCost(seat)
	if err != nil {
		return 0, err
	}
	a.Undo.Pending = &seat
	return cost, nil
}

// answerUndo 让 seat 回应对方的悔棋请求，返回提出的座位、要撤回几步与费用；拒绝时 steps 为 0。没有对方提出的请求时返回
// errPvpUndoNoRequest。同意时调用方按 steps 截短落子序列。
func (a *boardAssist) answerUndo(movers []int, seat int, accept bool, over bool) (asker int, steps int, cost model.GameCoins, err error) {
	if seat < 0 || seat > 1 || a.Undo.Pending == nil || *a.Undo.Pending == seat {
		return -1, 0, 0, errPvpUndoNoRequest
	}
	asker = *a.Undo.Pending
	a.Undo.Pending = nil
	if !accept {
		a.Undo.Declined = &boardUndoDecline{Seat: asker, Moves: len(movers)}
		return asker, 0, 0, nil
	}
	steps = boardTakeback(movers, asker)
	if steps == 0 || over {
		return -1, 0, 0, errPvpUndoUnavailable
	}
	if cost, err = a.undoCost(asker); err != nil {
		return -1, 0, 0, err
	}
	a.Undo.Used[asker]++
	a.Undo.Declined = nil
	a.Hint = nil
	return asker, steps, cost, nil
}

func (a *boardAssist) undoView(movers []int, seat int, over bool) boardUndoView {
	view := boardUndoView{PendingBy: -1, FreeLeft: a.undoFreeLeft(seat)}
	if seat >= 0 && seat <= 1 {
		view.Used = a.Undo.Used[seat]
	}
	if a.Undo.Pending != nil {
		view.PendingBy = *a.Undo.Pending
	}
	view.Declined = a.declinedBy(seat, len(movers))
	view.Cost, _ = a.undoCost(seat)
	view.Available = !over && a.Undo.Pending == nil && !view.Declined && boardTakeback(movers, seat) > 0
	return view
}

func (a *boardAssist) hintCost() (model.GameCoins, error) {
	if a.Hints < gameHintFreePlays {
		return 0, nil
	}
	return model.WholeGameCoins(gameHintCostCoins)
}

// hintStatus 汇总 seat 此刻能不能用提示、还剩几次免费、下一次要花多少游戏币：轮到他走、对局还没结束时才能用。
func (a *boardAssist) hintStatus(seat int, toMove int, over bool) (pvpHintStatus, error) {
	cost, err := a.hintCost()
	if err != nil {
		return pvpHintStatus{}, err
	}
	return pvpHintStatus{
		Available: !over && toMove == seat,
		FreeLeft:  max(gameHintFreePlays-a.Hints, 0),
		Used:      a.Hints,
		Cost:      cost,
	}, nil
}

// cachedHint 返回当前局面已经取得的提示，局面变了或轮不到 seat 时为 nil。
func (a *boardAssist) cachedHint(seat int, toMove int, over bool, moves int) (json.RawMessage, error) {
	if over || toMove != seat || a.Hint == nil || a.Hint.Moves != moves {
		return nil, nil
	}
	return common.Marshal(a.Hint)
}

// recordHint 记下一次新的提示并用掉一次次数，返回提示内容与这一次的费用。
func (a *boardAssist) recordHint(hint boardHint) (json.RawMessage, model.GameCoins, error) {
	cost, err := a.hintCost()
	if err != nil {
		return nil, 0, err
	}
	content, err := common.Marshal(hint)
	if err != nil {
		return nil, 0, err
	}
	a.Hints++
	a.Hint = &hint
	return content, cost, nil
}
