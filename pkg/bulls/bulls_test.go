package bulls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreCountsBullsAndCows(t *testing.T) {
	cases := []struct {
		secret, guess string
		a, b          int
	}{
		{"1234", "1234", 4, 0},
		{"1234", "4321", 0, 4},
		{"1234", "1243", 2, 2},
		{"1234", "5678", 0, 0},
		{"0123", "0918", 1, 1},
		{"012", "210", 1, 2},
	}
	for _, c := range cases {
		a, b := Score(c.secret, c.guess)
		assert.Equal(t, c.a, a, "%s %s", c.secret, c.guess)
		assert.Equal(t, c.b, b, "%s %s", c.secret, c.guess)
	}
}

// 答案各位互不相同、只由种子决定；猜测必须位数对、各位不同；猜中或用完次数后不能再猜。
func TestRoundRules(t *testing.T) {
	g, err := New(2, 7)
	require.NoError(t, err)
	again, err := New(2, 7)
	require.NoError(t, err)
	assert.Equal(t, g.Secret, again.Secret)
	assert.True(t, ValidGuess(g.Secret, 4))
	assert.Equal(t, 5040, g.Candidates(), "4 位各不相同的数一共 10×9×8×7 个")

	for _, bad := range []string{"123", "12345", "1123", "12a4", ""} {
		_, err := g.Guess(bad)
		assert.ErrorIs(t, err, ErrGuess, bad)
	}
	assert.Empty(t, g.History, "猜错格式不占次数")

	feedback, err := g.Guess(g.Secret)
	require.NoError(t, err)
	assert.Equal(t, 4, feedback.A)
	assert.True(t, g.Won())
	assert.Equal(t, 1, g.Candidates())
	_, err = g.Guess(g.Secret)
	assert.ErrorIs(t, err, ErrFinished)

	hard, err := New(3, 9)
	require.NoError(t, err)
	wrong := "0123"
	if hard.Secret == wrong {
		wrong = "4567"
	}
	for i := 0; i < Levels[3].Guesses; i++ {
		_, err := hard.Guess(wrong)
		require.NoError(t, err)
	}
	assert.True(t, hard.Over())
	assert.False(t, hard.Won())
	_, err = hard.Guess(hard.Secret)
	assert.ErrorIs(t, err, ErrFinished)
	_, err = New(0, 1)
	assert.ErrorIs(t, err, ErrDifficulty)
}
