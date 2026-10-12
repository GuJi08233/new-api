package roulette

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allBets 列出每一种合法的注，每注押 amount 个游戏币：37 个单号、6 种一赔一、3 个打与 3 列。
func allBets(amount int) []Bet {
	bets := make([]Bet, 0, Pockets+12)
	for number := 0; number < Pockets; number++ {
		bets = append(bets, Bet{Type: Straight, Value: number, Amount: amount})
	}
	for _, kind := range []string{Red, Black, Odd, Even, Low, High} {
		bets = append(bets, Bet{Type: kind, Amount: amount})
	}
	for value := 1; value <= 3; value++ {
		bets = append(bets, Bet{Type: Dozen, Value: value, Amount: amount}, Bet{Type: Column, Value: value, Amount: amount})
	}
	return bets
}

// 穷举 37 个号码：任何一注押 1 个游戏币，所有号码的派彩加起来都正好是 36 个，理论返还率恰为 36/37，与下注方式无关，
// 也与页面公示的 Payback 一致。改赔率或号码覆盖时这里会失败。
func TestEveryBetPaysBack36Of37(t *testing.T) {
	bets := allBets(1)
	require.Len(t, bets, 49)
	for _, bet := range bets {
		require.True(t, bet.Valid(), "%s %d", bet.Type, bet.Value)
		paid := 0
		for number := 0; number < Pockets; number++ {
			paid += bet.Win(number)
		}
		assert.Equal(t, 36, paid, "%s %d", bet.Type, bet.Value)
		assert.InDelta(t, Payback, float64(paid)/Pockets, 1e-12, "%s %d", bet.Type, bet.Value)
	}
}

// 外围注按号码划分：红黑、单双、大小各把 1–36 分成两份，打与列各分成三份，每个号码正好落在每一组的一注里；0 不属于任何外围注，
// 开出 0 时外围注全输。颜色、打与列按标准轮盘桌面抽查。
func TestOutsideBetsPartitionTheNumbers(t *testing.T) {
	groups := [][]Bet{
		{{Type: Red}, {Type: Black}},
		{{Type: Odd}, {Type: Even}},
		{{Type: Low}, {Type: High}},
		{{Type: Dozen, Value: 1}, {Type: Dozen, Value: 2}, {Type: Dozen, Value: 3}},
		{{Type: Column, Value: 1}, {Type: Column, Value: 2}, {Type: Column, Value: 3}},
	}
	for _, group := range groups {
		for number := 0; number < Pockets; number++ {
			covered := 0
			for _, bet := range group {
				if bet.Covers(number) {
					covered++
				}
			}
			want := 1
			if number == 0 {
				want = 0
			}
			assert.Equal(t, want, covered, "%s group, number %d", group[0].Type, number)
		}
	}

	cases := []struct {
		number int
		color  string
		dozen  int
		column int
	}{
		{number: 1, color: Red, dozen: 1, column: 1},
		{number: 2, color: Black, dozen: 1, column: 2},
		{number: 3, color: Red, dozen: 1, column: 3},
		{number: 10, color: Black, dozen: 1, column: 1},
		{number: 11, color: Black, dozen: 1, column: 2},
		{number: 12, color: Red, dozen: 1, column: 3},
		{number: 13, color: Black, dozen: 2, column: 1},
		{number: 18, color: Red, dozen: 2, column: 3},
		{number: 19, color: Red, dozen: 2, column: 1},
		{number: 24, color: Black, dozen: 2, column: 3},
		{number: 25, color: Red, dozen: 3, column: 1},
		{number: 28, color: Black, dozen: 3, column: 1},
		{number: 29, color: Black, dozen: 3, column: 2},
		{number: 34, color: Red, dozen: 3, column: 1},
		{number: 35, color: Black, dozen: 3, column: 2},
		{number: 36, color: Red, dozen: 3, column: 3},
	}
	for _, c := range cases {
		assert.Equal(t, c.color, Color(c.number), "number %d", c.number)
		assert.True(t, Bet{Type: Dozen, Value: c.dozen}.Covers(c.number), "number %d dozen %d", c.number, c.dozen)
		assert.True(t, Bet{Type: Column, Value: c.column}.Covers(c.number), "number %d column %d", c.number, c.column)
	}
	assert.Equal(t, Green, Color(0))
	assert.Len(t, RedNumbers, 18)
}

// 开出的号码按每一注的赔率派彩(含本金)：单号 36 倍、一赔一 2 倍、打与列 3 倍，没中的注派彩 0。
func TestWinPaysAmountTimesOddsPlusOne(t *testing.T) {
	cases := []struct {
		bet    Bet
		number int
		want   int
	}{
		{bet: Bet{Type: Straight, Value: 0, Amount: 3}, number: 0, want: 108},
		{bet: Bet{Type: Straight, Value: 17, Amount: 5}, number: 17, want: 180},
		{bet: Bet{Type: Straight, Value: 17, Amount: 5}, number: 18, want: 0},
		{bet: Bet{Type: Red, Amount: 7}, number: 1, want: 14},
		{bet: Bet{Type: Red, Amount: 7}, number: 0, want: 0},
		{bet: Bet{Type: Even, Amount: 7}, number: 0, want: 0},
		{bet: Bet{Type: Low, Amount: 4}, number: 18, want: 8},
		{bet: Bet{Type: High, Amount: 4}, number: 18, want: 0},
		{bet: Bet{Type: Dozen, Value: 3, Amount: 10}, number: 25, want: 30},
		{bet: Bet{Type: Column, Value: 1, Amount: 10}, number: 34, want: 30},
		{bet: Bet{Type: Column, Value: 1, Amount: 10}, number: 0, want: 0},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.bet.Win(c.number), "%s %d on %d", c.bet.Type, c.bet.Value, c.number)
	}
}

// 只接受已知的注型和这种注型范围内的号码：单号 0–36，打与列 1–3，其他注型不带号码；注型区分大小写。
func TestBetValid(t *testing.T) {
	cases := []struct {
		bet  Bet
		want bool
	}{
		{bet: Bet{Type: Straight, Value: 0}, want: true},
		{bet: Bet{Type: Straight, Value: 36}, want: true},
		{bet: Bet{Type: Straight, Value: -1}, want: false},
		{bet: Bet{Type: Straight, Value: 37}, want: false},
		{bet: Bet{Type: Dozen, Value: 1}, want: true},
		{bet: Bet{Type: Dozen, Value: 0}, want: false},
		{bet: Bet{Type: Dozen, Value: 4}, want: false},
		{bet: Bet{Type: Column, Value: 3}, want: true},
		{bet: Bet{Type: Column, Value: 0}, want: false},
		{bet: Bet{Type: Column, Value: 4}, want: false},
		{bet: Bet{Type: Red}, want: true},
		{bet: Bet{Type: High}, want: true},
		{bet: Bet{Type: Red, Value: 1}, want: false},
		{bet: Bet{Type: Odd, Value: 3}, want: false},
		{bet: Bet{Type: "Red"}, want: false},
		{bet: Bet{Type: "split", Value: 1}, want: false},
		{bet: Bet{Type: ""}, want: false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.bet.Valid(), "%q %d", c.bet.Type, c.bet.Value)
	}
}
