package xiangqi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pickFirst 与 pickLast 是确定性的"随机数"：总是选分数最高的一步，或候选里的最后一步。
func pickFirst(int) int { return 0 }

func pickLast(n int) int { return n - 1 }

// hasMateInOne 表示轮到走的一方有一步能将死或困毙对方。
func hasMateInOne(t *testing.T, game Game) bool {
	t.Helper()
	pos, err := game.Position()
	require.NoError(t, err)
	for _, m := range pos.LegalMoves() {
		next := game
		require.NoError(t, next.Play(m))
		if next.Over() && next.Winner == game.Turn {
			return true
		}
	}
	return false
}

// 能一步杀棋时每个难度都直接杀，不受随机选择影响：杀棋分比任何子力都大，而且杀棋时不在几步之间随机。这个局面里
// 车 (8,7) 到底线是将死，车 (0,8) 到 (5,8) 是困毙，两步都是一步赢棋。
func TestAiTakesMateInOne(t *testing.T) {
	for level := 1; level <= AiLevels; level++ {
		for _, pick := range []func(int) int{pickFirst, pickLast} {
			game := Game{Board: matedByTwoRooks(t), Turn: Red, Plies: 40, Quiet: 3, Winner: NoColor}
			m, err := BestMove(&game, level, pick)
			require.NoError(t, err, "level %d", level)
			require.NoError(t, game.Play(m), "level %d", level)
			assert.True(t, game.Over(), "level %d played %v", level, m)
			assert.Equal(t, Red, game.Winner, "level %d played %v", level, m)
		}
	}
}

// 黑方车可以白吃红马，但吃了之后红车 (8,7) 走到底线就是杀棋(红仕挡住了黑车将军的路)。普通与困难看得到这一步杀，
// 走完之后红方没有一步杀；简单难度只多看一层吃子，贪吃这匹马而被杀，正是它"看不到两步以上的战术"的样子。
func TestAiSeesMateThreatFromLevelTwo(t *testing.T) {
	trap := diagram(t,
		"....k....",
		"R........",
		"........R",
		".........",
		".........",
		".r.......",
		".........",
		".........",
		".N..A....",
		"....K....",
	)
	greedy := Game{Board: trap, Turn: Black, Plies: 40, Quiet: 3, Winner: NoColor}
	require.NoError(t, greedy.Play(move(1, 4, 1, 1)))
	require.True(t, hasMateInOne(t, greedy), "吃马之后红方有一步杀")

	for level := 2; level <= AiLevels; level++ {
		for _, pick := range []func(int) int{pickFirst, pickLast} {
			game := Game{Board: trap, Turn: Black, Plies: 40, Quiet: 3, Winner: NoColor}
			m, err := BestMove(&game, level, pick)
			require.NoError(t, err)
			require.NoError(t, game.Play(m))
			assert.False(t, hasMateInOne(t, game), "level %d played %v and walked into a mate", level, m)
		}
	}

	game := Game{Board: trap, Turn: Black, Plies: 40, Quiet: 3, Winner: NoColor}
	m, err := BestMove(&game, 1, pickFirst)
	require.NoError(t, err)
	assert.Equal(t, move(1, 4, 1, 1), m)
}

// 从开局起每个难度走出的都是合法的一步，而且同样的局面、同样的随机数总是走同样的一步(搜索按节点数而不是时间截止)。
func TestAiMovesAreLegalAndDeterministic(t *testing.T) {
	for level := 1; level <= AiLevels; level++ {
		game := NewGame()
		for ply := 0; ply < 6; ply++ {
			m, err := BestMove(&game, level, pickLast)
			require.NoError(t, err)
			again, err := BestMove(&game, level, pickLast)
			require.NoError(t, err)
			assert.Equal(t, m, again, "level %d ply %d", level, ply)
			require.NoError(t, game.Play(m), "level %d ply %d", level, ply)
		}
	}
}

// 难度不存在、对局已经结束时不走棋。
func TestAiRejectsUnknownLevelAndFinishedGame(t *testing.T) {
	game := NewGame()
	for _, level := range []int{0, AiLevels + 1} {
		_, err := BestMove(&game, level, pickFirst)
		assert.Error(t, err, "level %d", level)
	}
	mated := Game{Board: matedByTwoRooks(t), Turn: Red, Winner: NoColor}
	require.NoError(t, mated.Play(move(8, 7, 8, 9)))
	_, err := BestMove(&mated, 3, pickFirst)
	assert.ErrorIs(t, err, ErrGameOver)
}
