// Package rps 是游戏中心石头剪刀布(对战)的规则。
//
// 三局两胜：每回合双方同时出拳，石头赢剪刀、剪刀赢布、布赢石头；两人出得一样是平局，这一回合重来。先赢 WinsNeeded 回合的
// 一方获胜；最多打 MaxRounds 回合(平局也算一回合)，打完仍没有人赢满 WinsNeeded 回合判和，哪怕有一方领先。
//
// 一方先出拳后轮到还没出的一方(ToMove)，房间给它重新计时；双方都出了就公开这一回合，下一回合双方同时出拳(ToMove 为 -1)。
// 超时：双方都没出判和，否则还没出的一方输。已经出了、还没公开的拳只存在服务端的状态里，View 绝不给对手看。
package rps

import "errors"

// Choice 是一次出拳。
type Choice string

const (
	Rock     Choice = "rock"
	Paper    Choice = "paper"
	Scissors Choice = "scissors"
)

const (
	// WinsNeeded 是赢下比赛要赢的回合数。
	WinsNeeded = 2
	// MaxRounds 是一场最多打几回合，平局的回合也算。
	MaxRounds = 9
)

var ErrIllegalMove = errors.New("rps: illegal move")

// Valid 表示这是石头、剪刀、布之一。
func (c Choice) Valid() bool {
	return c == Rock || c == Paper || c == Scissors
}

// Beats 表示 c 赢 other。
func (c Choice) Beats(other Choice) bool {
	return (c == Rock && other == Scissors) || (c == Scissors && other == Paper) || (c == Paper && other == Rock)
}

// Round 是公开了的一回合：两个座位各出了什么，Winner 是赢的座位，平局为 -1。
type Round struct {
	Choices [2]Choice `json:"choices"`
	Winner  int       `json:"winner"`
}

// Match 是一场比赛，整份以 JSON 存在房间里。Pending 是这一回合已经出了、还没公开的拳，空字符串表示还没出。
// Winner 在比赛结束时是赢的座位，和棋或还没结束时为 -1。
type Match struct {
	Rounds  []Round   `json:"rounds"`
	Pending [2]Choice `json:"pending"`
	Wins    [2]int    `json:"wins"`
	Over    bool      `json:"over"`
	Winner  int       `json:"winner"`
}

// New 返回一场新比赛：第一回合，双方都还没出拳。
func New() Match {
	return Match{Rounds: []Round{}, Winner: -1}
}

// Play 让 seat 在这一回合出拳。比赛已结束、座位或拳不对、这一回合已经出过时返回 ErrIllegalMove，m 不会改变。
// 双方都出了就公开这一回合并记下胜负。
func (m *Match) Play(seat int, choice Choice) error {
	if m.Over || (seat != 0 && seat != 1) || !choice.Valid() || m.Pending[seat] != "" {
		return ErrIllegalMove
	}
	m.Pending[seat] = choice
	if m.Pending[1-seat] == "" {
		return nil
	}
	round := Round{Choices: m.Pending, Winner: -1}
	switch {
	case round.Choices[0].Beats(round.Choices[1]):
		round.Winner = 0
	case round.Choices[1].Beats(round.Choices[0]):
		round.Winner = 1
	}
	m.Rounds = append(m.Rounds, round)
	m.Pending = [2]Choice{}
	if round.Winner >= 0 {
		m.Wins[round.Winner]++
		if m.Wins[round.Winner] >= WinsNeeded {
			m.Over, m.Winner = true, round.Winner
			return nil
		}
	}
	if len(m.Rounds) >= MaxRounds {
		m.Over = true
	}
	return nil
}

// ToMove 是轮到出拳的座位：这一回合一方出了就是另一方；双方都还没出(或比赛已结束)为 -1。
func (m *Match) ToMove() int {
	switch {
	case m.Over:
		return -1
	case m.Pending[0] != "" && m.Pending[1] == "":
		return 1
	case m.Pending[1] != "" && m.Pending[0] == "":
		return 0
	}
	return -1
}

// TimeoutWinner 是这一回合超时之后的赢家座位：双方都没出判和(-1)，否则还没出的一方输。
func (m *Match) TimeoutWinner() int {
	if m.Over {
		return m.Winner
	}
	if waiting := m.ToMove(); waiting >= 0 {
		return 1 - waiting
	}
	return -1
}

// View 是给一个座位看的比赛：公开了的回合、比分、这一回合谁已经出了，以及看的人自己这一回合出的拳。
// 对手这一回合出的拳不在里面。
type View struct {
	Rounds []Round `json:"rounds"`
	Wins   [2]int  `json:"wins"`
	// Round 是这一回合是第几回合(从 1 开始)；比赛结束后是最后一回合。
	Round int `json:"round"`
	// Ready 表示两个座位这一回合是否已经出拳。
	Ready [2]bool `json:"ready"`
	// Choice 是看的人自己这一回合出的拳，还没出或旁观时为空。
	Choice     Choice `json:"choice"`
	WinsNeeded int    `json:"wins_needed"`
	MaxRounds  int    `json:"max_rounds"`
	Over       bool   `json:"over"`
	Winner     int    `json:"winner"`
}

// View 返回给 seat 看的比赛，seat 为 -1 表示旁观者。
func (m *Match) View(seat int) View {
	view := View{
		Rounds:     append([]Round{}, m.Rounds...),
		Wins:       m.Wins,
		Round:      len(m.Rounds) + 1,
		Ready:      [2]bool{m.Pending[0] != "", m.Pending[1] != ""},
		WinsNeeded: WinsNeeded,
		MaxRounds:  MaxRounds,
		Over:       m.Over,
		Winner:     m.Winner,
	}
	if m.Over {
		view.Round = len(m.Rounds)
	}
	if seat == 0 || seat == 1 {
		view.Choice = m.Pending[seat]
	}
	return view
}
