package match3

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boardFrom 把 8 行字符串写成盘面：'1'..'6' 是普通宝石，'.' 用 6 种颜色里不会连起来的填充色轮换。
func boardFrom(t *testing.T, rows [Size]string) *Game {
	t.Helper()
	g := &Game{Difficulty: 2, Board: make([]int, Cells), rng: rand.New(rand.NewPCG(1, 2))}
	for r, row := range rows {
		require.Len(t, row, Size)
		for c, ch := range row {
			g.Board[r*Size+c] = int(ch - '0')
		}
	}
	return g
}

// 同一种子总是同一盘，初盘没有现成的三连、至少有一步能消。
func TestNewBoardIsPlayable(t *testing.T) {
	for difficulty := 1; difficulty <= 3; difficulty++ {
		for seed := uint64(1); seed <= 30; seed++ {
			a, err := New(difficulty, seed)
			require.NoError(t, err)
			b, err := New(difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, a.Board, b.Board)
			assert.Empty(t, findGroups(a.Board))
			assert.True(t, a.HasMove())
			for _, gem := range a.Board {
				assert.True(t, gem >= 1 && gem <= Levels[difficulty].Colors)
			}
		}
	}
}

// 交换后连不成三颗不算一步，盘面不变；越界的交换被拒绝。
func TestSwapMustMatch(t *testing.T) {
	g := boardFrom(t, [Size]string{
		"12345612",
		"23456123",
		"34561234",
		"45612345",
		"56123456",
		"61234561",
		"12345612",
		"23456123",
	})
	before := append([]int(nil), g.Board...)
	assert.ErrorIs(t, g.Swap(0, 0), ErrMove)
	assert.Equal(t, before, g.Board)
	assert.ErrorIs(t, g.Swap(7, 0), ErrMove, "最右一列不能和右边交换")
	assert.ErrorIs(t, g.Swap(63, 1), ErrMove)
	assert.Equal(t, 0, g.Moves)
}

// 四连留下直线宝石(留在刚换过去的位置)，其余三颗消掉；三连得基础分，留下的特殊宝石也算一颗。五连留下炸弹。
func TestFourInARowLeavesALineGem(t *testing.T) {
	rows := [Size]string{
		"23456123",
		"34561234",
		"45612345",
		"56123456",
		"61234561",
		"23456123",
		"11213456",
		"34163456",
	}
	g := boardFrom(t, rows)
	require.NoError(t, g.Swap(6*Size+2, 1))
	require.NotEmpty(t, g.Phases)
	first := g.Phases[0]
	require.Len(t, first.Created, 1)
	assert.Equal(t, 6*Size+2, first.Created[0].Cell)
	assert.Equal(t, KindRow, kindOf(first.Created[0].Gem))
	assert.Equal(t, 1, colorOf(first.Created[0].Gem))
	assert.Equal(t, []int{6 * Size, 6*Size + 1, 6*Size + 3}, first.Cleared)
	assert.Equal(t, 4*PointsPerGem, first.Gained)
	assert.Equal(t, 1, g.Moves)

	rows[6] = "11211456"
	g = boardFrom(t, rows)
	require.NoError(t, g.Swap(6*Size+2, 1))
	require.Len(t, g.Phases[0].Created, 1)
	assert.Equal(t, KindBomb, kindOf(g.Phases[0].Created[0].Gem))
}

// 直线宝石被消掉时整行一起消掉，波及的炸弹接着炸掉周围 3×3；这一轮按消掉的总数计分。
func TestSpecialGemsChain(t *testing.T) {
	g := boardFrom(t, [Size]string{
		"23456123",
		"34561234",
		"45612345",
		"11345634",
		"61134561",
		"23456123",
		"34561234",
		"45612345",
	})
	g.Board[3*Size] = KindRow<<kindShift | 1
	g.Board[3*Size+5] = KindBomb<<kindShift | 6
	require.NoError(t, g.Swap(3*Size+2, 1))
	first := g.Phases[0]
	expected := []int{}
	for cell := 2 * Size; cell < 5*Size; cell++ {
		row, col := cell/Size, cell%Size
		if row == 3 || col >= 4 && col <= 6 {
			expected = append(expected, cell)
		}
	}
	assert.Equal(t, expected, first.Cleared)
	assert.Empty(t, first.Created)
	assert.Equal(t, len(expected)*PointsPerGem, first.Gained)
}

// 一局完全由交换的序列决定：同一种子、同样的交换重放出同样的分数与盘面；步数用完就结束，不能再交换。
func TestReplayIsDeterministicAndEnds(t *testing.T) {
	play := func() *Game {
		g, err := New(1, 42)
		require.NoError(t, err)
		for !g.Over() {
			moved := false
			for cell := 0; cell < Cells && !moved; cell++ {
				for dir := 0; dir < 2 && !moved; dir++ {
					moved = g.Swap(cell, dir) == nil
				}
			}
			require.True(t, moved, "总有能消的一步")
		}
		return g
	}
	a, b := play(), play()
	assert.Equal(t, a.Board, b.Board)
	assert.Equal(t, a.Score, b.Score)
	assert.True(t, a.Won() || a.MovesLeft() == 0)
	assert.ErrorIs(t, a.Swap(0, 0), ErrMove)
}
