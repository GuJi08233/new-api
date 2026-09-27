package dice

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 穷举每个可配置的返还率、每一档倍数、两个方向下的全部 10000 个点数：获胜的点数正好是 threshold 个，所以每下注 1 个游戏币
// 平均派彩 threshold × 倍数 / 10000 个。阈值向下取整，平均派彩不超过配置的返还率、少得不到一个点数的派彩，
// 而且总低于 100%：玩家长期是输的。
func TestThresholdKeepsPaybackBelowRtp(t *testing.T) {
	for rtp := operation_setting.GameMinRtpPercent; rtp <= operation_setting.GameMaxRtpPercent; rtp++ {
		for _, multiplier := range Multipliers {
			threshold := Threshold(rtp, multiplier)
			for _, direction := range []string{Under, Over} {
				wins := 0
				for roll := 0; roll < Sides; roll++ {
					if Wins(roll, threshold, direction) {
						wins++
					}
				}
				require.Equal(t, threshold, wins, "rtp %d multiplier %d %s", rtp, multiplier, direction)
			}
			paid := threshold * multiplier
			assert.LessOrEqual(t, paid, rtp*100, "rtp %d multiplier %d", rtp, multiplier)
			assert.Greater(t, paid, rtp*100-multiplier, "rtp %d multiplier %d", rtp, multiplier)
			assert.Less(t, paid, Sides, "rtp %d multiplier %d", rtp, multiplier)
		}
	}
}

// 页面公示的阈值：默认返还率 96% 能被每一档倍数整除，每一档都正好返还 96%；除不尽时向下取整，例如 97% 押 3 倍的阈值是
// 3233，返还率 96.99%。
func TestThreshold(t *testing.T) {
	cases := []struct {
		rtp        int
		multiplier int
		want       int
	}{
		{rtp: 96, multiplier: 2, want: 4800},
		{rtp: 96, multiplier: 3, want: 3200},
		{rtp: 96, multiplier: 4, want: 2400},
		{rtp: 96, multiplier: 5, want: 1920},
		{rtp: 96, multiplier: 10, want: 960},
		{rtp: 96, multiplier: 20, want: 480},
		{rtp: 96, multiplier: 50, want: 192},
		{rtp: 96, multiplier: 100, want: 96},
		{rtp: 97, multiplier: 3, want: 3233},
		{rtp: 97, multiplier: 20, want: 485},
		{rtp: 99, multiplier: 2, want: 4950},
		{rtp: 50, multiplier: 100, want: 50},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Threshold(c.rtp, c.multiplier), "rtp %d multiplier %d", c.rtp, c.multiplier)
	}
}

// 两个方向的边界：under 掷出小于阈值才赢，over 掷出不小于 10000 − 阈值才赢，阈值本身那个点数 under 算输；
// 方向不是 under、over 的一注永远不赢。
func TestWinsBoundaries(t *testing.T) {
	cases := []struct {
		roll      int
		threshold int
		direction string
		want      bool
	}{
		{roll: 0, threshold: 4800, direction: Under, want: true},
		{roll: 4799, threshold: 4800, direction: Under, want: true},
		{roll: 4800, threshold: 4800, direction: Under, want: false},
		{roll: 9999, threshold: 4800, direction: Under, want: false},
		{roll: 0, threshold: 4800, direction: Over, want: false},
		{roll: 5199, threshold: 4800, direction: Over, want: false},
		{roll: 5200, threshold: 4800, direction: Over, want: true},
		{roll: 9999, threshold: 4800, direction: Over, want: true},
		{roll: 95, threshold: 96, direction: Under, want: true},
		{roll: 96, threshold: 96, direction: Under, want: false},
		{roll: 9903, threshold: 96, direction: Over, want: false},
		{roll: 9904, threshold: 96, direction: Over, want: true},
		{roll: 0, threshold: 4800, direction: "", want: false},
		{roll: 9999, threshold: 4800, direction: "OVER", want: false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Wins(c.roll, c.threshold, c.direction), "roll %d threshold %d %q", c.roll, c.threshold, c.direction)
	}
}
