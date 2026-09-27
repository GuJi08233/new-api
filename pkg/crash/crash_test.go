package crash

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 爆点 = floor(RTP × 2^52 / r)，随 r 增大而不增。对每个倍数 m，r = floor(X/m) 时爆点不低于 m、再大 1 就低于 m，
// 所以 r ∈ [1, 2^52] 里爆点 ≥ m 的正好有 floor(X/m) 个，P(爆点 ≥ m) = floor(X/m)/2^52 ≈ RTP/m。r 最小时封顶 1000 倍，
// 最大时低于 1.00×，记为 1.00×。
func TestPointOf(t *testing.T) {
	for _, rtp := range []int{50, 96, 99} {
		x := int64(rtp) << 52
		assert.Equal(t, MaxPoint, PointOf(rtp, 1), "rtp %d r=1", rtp)
		assert.Equal(t, MinPoint, PointOf(rtp, randomSpan), "rtp %d r=2^52 算出 %d，记为 1.00×", rtp, rtp)
		for _, m := range []int{101, 150, 200, 250, 1000, 12345, MaxPoint} {
			boundary := x / int64(m)
			require.GreaterOrEqual(t, PointOf(rtp, boundary), m, "rtp %d m %d", rtp, m)
			require.Less(t, PointOf(rtp, boundary+1), m, "rtp %d m %d", rtp, m)
		}
		// 开局即爆：r 超过 floor(X/101) 时爆点不到 1.01×。
		assert.Equal(t, MinPoint, PointOf(rtp, x/101+1), "rtp %d", rtp)
		assert.Equal(t, 101, PointOf(rtp, x/101), "rtp %d", rtp)
	}
}

// 设自动兑现 a：赢的概率 floor(X/a)/2^52，派彩 floor(下注 × a/100)，期望返还不超过 RTP；下注是 100 的倍数时派彩没有零头，
// 期望只比 RTP 少不到 a/(100 × 2^52)。手动在倍数 a 兑现要求 a 严格低于爆点，赢的概率 floor(X/(a+1))/2^52 更低。
func TestExpectedReturnNeverExceedsRtp(t *testing.T) {
	span := big.NewRat(randomSpan, 1)
	for _, rtp := range []int{50, 96, 99} {
		limit := big.NewRat(int64(rtp), 100)
		x := int64(rtp) << 52
		for _, auto := range []int{MinAutoCashout, 150, 200, 777, 1000, 5000, 99999, MaxPoint} {
			win := new(big.Rat).Quo(big.NewRat(x/int64(auto), 1), span)
			manualWin := new(big.Rat).Quo(big.NewRat(x/int64(auto+1), 1), span)
			for _, bet := range []int{1, 3, 7, 100, 1000} {
				payout := big.NewRat(int64(Payout(bet, auto)), int64(bet))
				expected := new(big.Rat).Mul(win, payout)
				require.LessOrEqual(t, expected.Cmp(limit), 0, "rtp %d auto %d bet %d", rtp, auto, bet)
				if auto < MaxPoint {
					manual := new(big.Rat).Mul(manualWin, payout)
					require.Negative(t, manual.Cmp(expected), "rtp %d auto %d bet %d", rtp, auto, bet)
				}
				if bet%100 == 0 {
					gap := new(big.Rat).Sub(limit, expected)
					require.LessOrEqual(t, gap.Cmp(new(big.Rat).Quo(big.NewRat(int64(auto), 100), span)), 0, "rtp %d auto %d", rtp, auto)
				}
			}
		}
	}
}

// 倍数按 floor(100 × e^(0.00006 t)) 上升：2 倍在 ln2/0.00006 ≈ 11552.45 毫秒，1000 倍之后封顶。
func TestMultiplierAt(t *testing.T) {
	cases := []struct {
		elapsed int64
		want    int
	}{
		{elapsed: -5, want: 100},
		{elapsed: 0, want: 100},
		{elapsed: 11552, want: 199},
		{elapsed: 11553, want: 200},
		{elapsed: 115129, want: 99998},
		{elapsed: 115130, want: MaxPoint},
		{elapsed: 1 << 40, want: MaxPoint},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, MultiplierAt(c.elapsed), "t=%d", c.elapsed)
	}
}

// 倍数第一次达到 m 的时刻与 MultiplierAt 一致：那一刻达到 m，前一毫秒还不到。
func TestTimeOf(t *testing.T) {
	assert.Zero(t, TimeOf(MinPoint))
	assert.Equal(t, int64(11553), TimeOf(200))
	for _, m := range []int{101, 150, 199, 200, 201, 1000, 31415, 99999, MaxPoint} {
		at := TimeOf(m)
		assert.GreaterOrEqual(t, MultiplierAt(at), m, "m %d", m)
		assert.Less(t, MultiplierAt(at-1), m, "m %d", m)
	}
}

// 兑现判定：倍数低于爆点时按当时的倍数兑现，到了爆点就已经爆了；自动兑现不高于爆点时，到点按设定的倍数兑现(与爆点相同也算)，
// 之后再请求兑现或查看也还是这个结果；高于爆点的自动兑现等不到。开局即爆的一局任何时候都是输。
func TestSettle(t *testing.T) {
	crashAt := TimeOf(250)
	cases := []struct {
		name     string
		point    int
		auto     int
		elapsed  int64
		cashout  bool
		wantOver bool
		wantMult int
	}{
		{name: "爆炸前一毫秒兑现", point: 250, elapsed: crashAt - 1, cashout: true, wantOver: true, wantMult: MultiplierAt(crashAt - 1)},
		{name: "爆炸那一刻兑现已经晚了", point: 250, elapsed: crashAt, cashout: true, wantOver: true, wantMult: 0},
		{name: "还在飞", point: 250, elapsed: crashAt - 1, wantOver: false},
		{name: "过了爆点再看就是爆了", point: 250, elapsed: crashAt + 5000, wantOver: true, wantMult: 0},
		{name: "开局立刻兑现拿回 1.00 倍", point: 101, elapsed: 0, cashout: true, wantOver: true, wantMult: 100},
		{name: "自动兑现还没到", point: 250, auto: 200, elapsed: TimeOf(200) - 1, wantOver: false},
		{name: "自动兑现到点", point: 250, auto: 200, elapsed: TimeOf(200), wantOver: true, wantMult: 200},
		{name: "自动兑现之后再请求兑现", point: 250, auto: 200, elapsed: TimeOf(220), cashout: true, wantOver: true, wantMult: 200},
		{name: "自动兑现之后火箭爆了也不影响", point: 250, auto: 200, elapsed: crashAt + 5000, wantOver: true, wantMult: 200},
		{name: "自动兑现等于爆点也算兑现", point: 250, auto: 250, elapsed: crashAt, wantOver: true, wantMult: 250},
		{name: "自动兑现高于爆点等不到", point: 250, auto: 251, elapsed: crashAt, wantOver: true, wantMult: 0},
		{name: "自动兑现之前手动兑现", point: 250, auto: 240, elapsed: TimeOf(230), cashout: true, wantOver: true, wantMult: 230},
		{name: "开局即爆", point: MinPoint, elapsed: 0, cashout: true, wantOver: true, wantMult: 0},
		{name: "开局即爆不用等", point: MinPoint, elapsed: 0, wantOver: true, wantMult: 0},
	}
	for _, c := range cases {
		over, multiplier := Settle(c.point, c.auto, c.elapsed, c.cashout)
		assert.Equal(t, c.wantOver, over, c.name)
		assert.Equal(t, c.wantMult, multiplier, c.name)
	}
}

// 一局自己结束的时刻：自动兑现不高于爆点时是自动兑现的时刻，否则是爆炸的时刻；那一刻之前还在飞，那一刻已经结束。
func TestEndsAt(t *testing.T) {
	for _, c := range []struct{ point, auto int }{{250, 0}, {250, 200}, {250, 250}, {250, 300}, {MinPoint, 0}, {MaxPoint, 0}, {MaxPoint, MaxPoint}} {
		end := EndsAt(c.point, c.auto)
		if end > 0 {
			over, _ := Settle(c.point, c.auto, end-1, false)
			assert.False(t, over, "point %d auto %d", c.point, c.auto)
		}
		over, _ := Settle(c.point, c.auto, end, false)
		assert.True(t, over, "point %d auto %d", c.point, c.auto)
	}
	assert.Equal(t, TimeOf(200), EndsAt(250, 200))
	assert.Equal(t, TimeOf(250), EndsAt(250, 300))
}

// 派彩 = floor(下注 × 倍数 / 100)，爆炸时为 0，最多下注的 1000 倍。
func TestPayout(t *testing.T) {
	assert.Equal(t, 4, Payout(3, 150))
	assert.Equal(t, 19, Payout(10, 199))
	assert.Equal(t, 10, Payout(10, 100))
	assert.Zero(t, Payout(7, 0))
	assert.Equal(t, 1000000, Payout(1000, MaxPoint))
}
