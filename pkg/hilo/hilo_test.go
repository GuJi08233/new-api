package hilo

import (
	"math/big"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每张牌上能猜的两种猜法，命中种数就是下一张 13 种点数里算猜对的种数：中间的点数 higher 命中 14−r 种、lower 命中 r 种
// (都含相同点数)；A 只能猜 higher(12 种)或 same(1 种)，K 只能猜 lower(12 种)或 same(1 种)；其余猜法不能猜。
func TestGuessesMatchHits(t *testing.T) {
	for rank := 1; rank <= Ranks; rank++ {
		allowed := Guesses(rank)
		for _, guess := range []string{Higher, Lower, Same} {
			hits := Hits(rank, guess)
			if !slices.Contains(allowed[:], guess) {
				assert.Zero(t, hits, "rank %d %s", rank, guess)
				continue
			}
			correct := 0
			for next := 1; next <= Ranks; next++ {
				if Correct(rank, guess, next) {
					correct++
				}
			}
			assert.Equal(t, correct, hits, "rank %d %s", rank, guess)
		}
	}
	assert.Equal(t, [2]string{Higher, Same}, Guesses(1))
	assert.Equal(t, [2]string{Lower, Same}, Guesses(Ranks))
	assert.Equal(t, 12, Hits(1, Higher))
	assert.Equal(t, 12, Hits(Ranks, Lower))
	assert.Equal(t, 7, Hits(7, Higher))
	assert.Equal(t, 7, Hits(7, Lower))
	assert.Equal(t, 12, Hits(2, Higher))
	assert.Equal(t, 2, Hits(2, Lower))
	assert.Zero(t, Hits(7, Same))
	assert.Zero(t, Hits(0, Higher))
}

// 每种牌点、每种猜法，一步的期望倍数 命中种数/13 × RTP/100 × 13/命中种数 恰好是 RTP/100：怎么猜都一样，理论返还率就是 RTP。
func TestEveryGuessReturnsExactlyRtp(t *testing.T) {
	for _, rtp := range []int{50, 96, 99} {
		want := big.NewRat(int64(rtp), 100)
		for rank := 1; rank <= Ranks; rank++ {
			for _, guess := range Guesses(rank) {
				hits := Hits(rank, guess)
				expected := new(big.Rat).Mul(big.NewRat(int64(hits), Ranks), StepMultiplier(rtp, hits))
				assert.Zero(t, expected.Cmp(want), "rtp %d rank %d %s: %s", rtp, rank, guess, expected)
			}
		}
	}
}

// 兑现派彩 = floor(下注 × 累计倍数)，最多下注的 1000 倍。
func TestPayout(t *testing.T) {
	cases := []struct {
		bet        int
		multiplier *big.Rat
		want       int
	}{
		{bet: 10, multiplier: big.NewRat(26, 25), want: 10},     // 10.4
		{bet: 25, multiplier: big.NewRat(26, 25), want: 26},     // 正好整数
		{bet: 3, multiplier: big.NewRat(156, 25), want: 18},     // 18.72
		{bet: 7, multiplier: big.NewRat(312, 175), want: 12},    // 12.48
		{bet: 2, multiplier: big.NewRat(1500, 1), want: 2000},   // 封顶
		{bet: 1, multiplier: big.NewRat(2000, 2), want: 1000},   // 正好到顶
		{bet: 5, multiplier: big.NewRat(19999, 20), want: 4999}, // 999.95 倍
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Payout(c.bet, c.multiplier), "bet %d × %s", c.bet, c.multiplier)
	}
}

// 显示用的倍数截断到两位小数，不会显示得比实际高。
func TestFormatMultiplier(t *testing.T) {
	assert.Equal(t, "1.00", FormatMultiplier(big.NewRat(1, 1)))
	assert.Equal(t, "1.78", FormatMultiplier(big.NewRat(312, 175)))
	assert.Equal(t, "0.66", FormatMultiplier(big.NewRat(2, 3)))
	assert.Equal(t, "12.48", FormatMultiplier(big.NewRat(1248, 100)))
	assert.Equal(t, "1000.00", FormatMultiplier(big.NewRat(1000, 1)))
}

// 一局里累计倍数逐步相乘、精确保存：7 上猜 higher 翻出 9(0.96 × 13/7)，9 上猜 lower 翻出 9(相同点数也算，0.96 × 13/9)，
// 换牌不改变倍数；猜错这一局就输了，之后什么都不能做。
func TestRoundPlay(t *testing.T) {
	round := NewRound(96, Card{Rank: 7, Suit: 1})
	alive, err := round.Play(Higher, Card{Rank: 9, Suit: 0})
	require.NoError(t, err)
	assert.True(t, alive)
	alive, err = round.Play(Lower, Card{Rank: 9, Suit: 2})
	require.NoError(t, err)
	assert.True(t, alive)
	multiplier, err := round.Value()
	require.NoError(t, err)
	want := new(big.Rat).Mul(StepMultiplier(96, 7), StepMultiplier(96, 9))
	assert.Zero(t, multiplier.Cmp(want), "%s", multiplier)
	assert.Equal(t, 2, round.Wins)

	alive, err = round.Play(Skip, Card{Rank: 1, Suit: 3})
	require.NoError(t, err)
	assert.True(t, alive)
	unchanged, err := round.Value()
	require.NoError(t, err)
	assert.Zero(t, unchanged.Cmp(want), "换牌不改变倍数")
	assert.Equal(t, Card{Rank: 1, Suit: 3}, round.Current())

	for _, guess := range []string{Lower, "", "up"} {
		_, err := round.Play(guess, Card{Rank: 5})
		assert.ErrorIs(t, err, ErrIllegalGuess, "A 上不能猜 %q", guess)
	}
	alive, err = round.Play(Same, Card{Rank: 4, Suit: 0})
	require.NoError(t, err)
	assert.False(t, alive)
	assert.True(t, round.Lost)
	assert.Equal(t, 2, round.Wins)
	assert.Equal(t, []string{Higher, Lower, Skip, Same}, round.Moves)
	assert.Len(t, round.Cards, 5)
	_, err = round.Play(Skip, Card{Rank: 5})
	assert.ErrorIs(t, err, ErrIllegalGuess, "输了之后不能再换牌")
}

// 每局最多换 5 次牌。
func TestSkipLimit(t *testing.T) {
	round := NewRound(96, Card{Rank: 7})
	for i := 0; i < MaxSkips; i++ {
		_, err := round.Play(Skip, Card{Rank: 8})
		require.NoError(t, err)
	}
	_, err := round.Play(Skip, Card{Rank: 8})
	assert.ErrorIs(t, err, ErrIllegalGuess)
	assert.Equal(t, MaxSkips, round.Skips)
	assert.Zero(t, round.Wins)
}
