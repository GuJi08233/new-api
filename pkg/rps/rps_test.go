package rps

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 九种出拳组合穷举：石头赢剪刀、剪刀赢布、布赢石头，出得一样是平局。
func TestRoundWinnerForEveryPair(t *testing.T) {
	cases := []struct {
		first, second Choice
		winner        int
	}{
		{Rock, Rock, -1}, {Rock, Paper, 1}, {Rock, Scissors, 0},
		{Paper, Rock, 0}, {Paper, Paper, -1}, {Paper, Scissors, 1},
		{Scissors, Rock, 1}, {Scissors, Paper, 0}, {Scissors, Scissors, -1},
	}
	for _, c := range cases {
		m := New()
		require.NoError(t, m.Play(0, c.first))
		require.NoError(t, m.Play(1, c.second))
		require.Len(t, m.Rounds, 1)
		assert.Equal(t, Round{Choices: [2]Choice{c.first, c.second}, Winner: c.winner}, m.Rounds[0], "%s vs %s", c.first, c.second)
	}
}

// 一场比赛逐回合推进：先出拳的一方等对手(轮到对手)，双方都出了才公开这一回合、下一回合重新同时出拳；平局重来、不计分，
// 先赢两回合的一方拿下比赛。
func TestMatchIsBestOfThreeWithTiesReplayed(t *testing.T) {
	steps := []struct {
		seat       int
		choice     Choice
		wantToMove int
		wantRounds int
		wantWins   [2]int
		wantOver   bool
		wantWinner int
	}{
		{seat: 0, choice: Rock, wantToMove: 1, wantRounds: 0},
		{seat: 1, choice: Scissors, wantToMove: -1, wantRounds: 1, wantWins: [2]int{1, 0}},
		// 这一回合座位 1 先出。
		{seat: 1, choice: Paper, wantToMove: 0, wantRounds: 1, wantWins: [2]int{1, 0}},
		{seat: 0, choice: Paper, wantToMove: -1, wantRounds: 2, wantWins: [2]int{1, 0}},
		{seat: 0, choice: Scissors, wantToMove: 1, wantRounds: 2, wantWins: [2]int{1, 0}},
		{seat: 1, choice: Rock, wantToMove: -1, wantRounds: 3, wantWins: [2]int{1, 1}},
		{seat: 1, choice: Paper, wantToMove: 0, wantRounds: 3, wantWins: [2]int{1, 1}},
		{seat: 0, choice: Scissors, wantToMove: -1, wantRounds: 4, wantWins: [2]int{2, 1}, wantOver: true, wantWinner: 0},
	}
	m := New()
	for i, step := range steps {
		require.NoError(t, m.Play(step.seat, step.choice), "step %d", i)
		assert.Equal(t, step.wantToMove, m.ToMove(), "step %d to move", i)
		assert.Len(t, m.Rounds, step.wantRounds, "step %d rounds", i)
		assert.Equal(t, step.wantWins, m.Wins, "step %d wins", i)
		assert.Equal(t, step.wantOver, m.Over, "step %d over", i)
		if step.wantOver {
			assert.Equal(t, step.wantWinner, m.Winner, "step %d winner", i)
		} else {
			assert.Equal(t, -1, m.Winner, "step %d winner", i)
		}
	}
	assert.ErrorIs(t, m.Play(1, Rock), ErrIllegalMove, "比赛结束后不能再出拳")
}

// 最多打 9 回合，平局的回合也算；打完仍没有人赢满两回合就是和棋，一比零领先也一样。第 8 回合之后还没结束。
func TestMatchIsDrawnAfterNineRounds(t *testing.T) {
	cases := map[string][]Choice{
		"nine ties":         {Rock, Rock, Rock, Rock, Rock, Rock, Rock, Rock, Rock},
		"one win each":      {Paper, Scissors, Rock, Rock, Rock, Rock, Rock, Rock, Rock},
		"one win then ties": {Paper, Rock, Rock, Rock, Rock, Rock, Rock, Rock, Rock},
	}
	for name, seatZero := range cases {
		t.Run(name, func(t *testing.T) {
			m := New()
			for round, choice := range seatZero {
				assert.False(t, m.Over, "round %d", round+1)
				require.NoError(t, m.Play(0, choice))
				// 座位 1 永远出石头：座位 0 出布赢、出剪刀输、出石头平。
				require.NoError(t, m.Play(1, Rock))
			}
			assert.True(t, m.Over)
			assert.Equal(t, -1, m.Winner)
			assert.Len(t, m.Rounds, MaxRounds)
			assert.Equal(t, -1, m.ToMove())
		})
	}
}

// 不合规则的出拳都拒绝，比赛状态不变：同一回合出第二次、不认识的拳、不存在的座位。
func TestPlayRejectsIllegalMoves(t *testing.T) {
	m := New()
	require.NoError(t, m.Play(0, Rock))
	before := m
	for _, c := range []struct {
		seat   int
		choice Choice
	}{
		{seat: 0, choice: Paper},
		{seat: 1, choice: ""},
		{seat: 1, choice: "lizard"},
		{seat: 2, choice: Rock},
		{seat: -1, choice: Rock},
	} {
		assert.ErrorIs(t, m.Play(c.seat, c.choice), ErrIllegalMove, "seat %d %q", c.seat, c.choice)
	}
	assert.Equal(t, before, m)
}

// 超时：这一回合双方都没出判和；一方出了、另一方没出，没出的一方输。
func TestTimeoutWinner(t *testing.T) {
	m := New()
	assert.Equal(t, -1, m.TimeoutWinner(), "双方都没出")
	require.NoError(t, m.Play(1, Paper))
	assert.Equal(t, 1, m.TimeoutWinner(), "座位 0 没出")
	require.NoError(t, m.Play(0, Rock))
	assert.Equal(t, -1, m.TimeoutWinner(), "新的一回合双方都没出")
	require.NoError(t, m.Play(0, Scissors))
	assert.Equal(t, 0, m.TimeoutWinner(), "座位 1 没出")
}

// 已经出了、还没公开的拳只给出拳的人自己看：对手和旁观者只知道他已经出了。双方都出了之后这一回合整个公开。
func TestViewHidesPendingChoice(t *testing.T) {
	m := New()
	require.NoError(t, m.Play(0, Scissors))

	own := m.View(0)
	assert.Equal(t, Scissors, own.Choice)
	assert.Equal(t, [2]bool{true, false}, own.Ready)
	assert.Equal(t, 1, own.Round)
	for _, seat := range []int{1, -1} {
		view := m.View(seat)
		assert.Equal(t, Choice(""), view.Choice, "seat %d", seat)
		assert.Equal(t, [2]bool{true, false}, view.Ready, "seat %d", seat)
		data, err := common.Marshal(view)
		require.NoError(t, err)
		assert.False(t, strings.Contains(string(data), string(Scissors)), "seat %d sees %s", seat, data)
	}

	require.NoError(t, m.Play(1, Rock))
	opponent := m.View(1)
	assert.Equal(t, []Round{{Choices: [2]Choice{Scissors, Rock}, Winner: 1}}, opponent.Rounds)
	assert.Equal(t, [2]int{0, 1}, opponent.Wins)
	assert.Equal(t, 2, opponent.Round)
	assert.Equal(t, [2]bool{false, false}, opponent.Ready)
	assert.Equal(t, Choice(""), opponent.Choice)
}
