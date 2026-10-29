package twentyfour

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 题库里每一组牌都有解，Solve 给出的答案能通过 Check；简单档只有 1–10 的牌；三档互不重复。
func TestEveryHandIsSolvable(t *testing.T) {
	seen := map[string]int{}
	for difficulty := 1; difficulty <= 3; difficulty++ {
		pool := hands[difficulty]
		require.Zero(t, len(pool)%4)
		require.GreaterOrEqual(t, len(pool)/4, HandsPerRound+MaxSkips)
		for i := 0; i < len(pool); i += 4 {
			key := pool[i : i+4]
			var hand [4]int
			for j := range hand {
				hand[j] = strings.IndexByte("0123456789abcd", key[j])
				require.True(t, hand[j] >= 1 && hand[j] <= 13, key)
				if difficulty == 1 {
					require.LessOrEqual(t, hand[j], 10, key)
				}
			}
			answer := Solve(hand)
			require.NotEmpty(t, answer, key)
			require.NoError(t, Check(answer, hand), "%s: %s", key, answer)
			_, dup := seen[key]
			require.False(t, dup, key)
			seen[key] = difficulty
		}
	}
}

// 算式要正好用上这 4 个数各一次，按四则运算的优先级用有理数精确计算。
func TestCheckValidatesExpressions(t *testing.T) {
	hand := [4]int{3, 3, 8, 8}
	assert.NoError(t, Check("8/(3-8/3)", hand))
	assert.NoError(t, Check("8 ÷ ( 3 − 8 ÷ 3 )", hand))
	assert.NoError(t, Check("（8÷(3-8÷3)）", hand))
	for _, bad := range []string{
		"", "8/(3-8/3", "8/3-8/3", "8*3*1", "8/(3-8/3)+0", "24", "8/(3-8/3)3",
		"-8/(3-8/3)", "8/(3-8/3))", "008/(3-8/3)", "(3-3)/(8-8)", strings.Repeat("(", 70),
	} {
		assert.ErrorIs(t, Check(bad, hand), ErrExpression, bad)
	}
	assert.NoError(t, Check("(1+2+3)*4", [4]int{4, 3, 2, 1}))
	assert.NoError(t, Check("12×2×1×1", [4]int{1, 1, 2, 12}))
}

// 一局按种子抽题，解出 HandsPerRound 组过关；跳过最多 MaxSkips 次，换成下一组。
func TestRoundProgress(t *testing.T) {
	g, err := New(2, 42)
	require.NoError(t, err)
	again, err := New(2, 42)
	require.NoError(t, err)
	assert.Equal(t, g.Hands, again.Hands, "同一个种子抽到同样的题")
	require.Len(t, g.Hands, HandsPerRound+MaxSkips)

	assert.ErrorIs(t, g.Submit("1+2"), ErrExpression)
	assert.Equal(t, 0, g.Index)
	for i := 0; i < MaxSkips; i++ {
		require.NoError(t, g.Skip())
	}
	assert.ErrorIs(t, g.Skip(), ErrNoSkip)
	for !g.Won() {
		require.NoError(t, g.Submit(Solve(g.Current())))
	}
	assert.Equal(t, HandsPerRound, g.Solved)
	assert.ErrorIs(t, g.Submit("1"), ErrFinished)
	_, err = New(4, 1)
	assert.ErrorIs(t, err, ErrDifficulty)
}
