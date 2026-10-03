package tradesim

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuturesMarginAndFeeRoundUp(t *testing.T) {
	margin, err := pricing.Margin(d("1000"), 10)
	require.NoError(t, err)
	assert.Equal(t, 50_000_000, margin, "1000 USDT at 10x needs 100 USDT")
	margin, err = pricing.Margin(d("33.333333333"), 3)
	require.NoError(t, err)
	assert.Equal(t, 5_555_556, margin, "11.111111111 USDT rounds up")

	fee, err := pricing.TradeFee(d("1000"), 5)
	require.NoError(t, err)
	assert.Equal(t, 250_000, fee, "0.05% of 1000 USDT")
	fee, err = pricing.TradeFee(d("0.00000001"), 2)
	require.NoError(t, err)
	assert.Equal(t, 1, fee, "any fill pays at least one quota unit")
}

// 盈亏按带符号的值向下取整：赚的少记，亏的多记。
func TestFuturesPnlFloorsTowardsTheSite(t *testing.T) {
	tests := []struct {
		name  string
		side  PositionSide
		entry string
		exit  string
		want  int
	}{
		{name: "long gains when the price rises", side: Long, entry: "1000", exit: "1100", want: 50_000_000},
		{name: "short loses when the price rises", side: Short, entry: "1000", exit: "1100", want: -50_000_000},
		{name: "a long loss of a fraction of a unit costs a whole unit", side: Long, entry: "1000", exit: "999.9999999", want: -1},
		{name: "a short gain of a fraction of a unit is dropped", side: Short, entry: "1000", exit: "999.9999999", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pnl, err := pricing.Pnl(test.side, d(test.entry), d(test.exit))
			require.NoError(t, err)
			assert.Equal(t, test.want, pnl)
		})
	}
}

// 部分平仓结转的开仓价值加起来正好是全部开仓价值。
func TestEntrySharesAddUpToTheEntryValue(t *testing.T) {
	entry := d("100")
	held := int64(3)
	released := decimal.Zero
	for held > 0 {
		share := EntryShare(entry, 1, held)
		released = released.Add(share)
		entry = entry.Sub(share)
		held--
	}
	assert.True(t, d("100").Equal(released), "released %s", released)
	assert.True(t, entry.IsZero())
	assert.True(t, d("33.333333333333333333").Equal(EntryShare(d("100"), 1, 3)))
}

// 强平价与强平判断一致：到了预估强平价就该强平，离强平价还远时不强平。
func TestLiquidationPriceMatchesLiquidatable(t *testing.T) {
	const margin = 50_000_000 // 100 USDT，10 倍开 10 个 @ 100
	tests := []struct {
		name    string
		side    PositionSide
		wantLiq string
		safe    string
	}{
		{name: "long", side: Long, wantLiq: "90.45226131", safe: "90.46"},
		{name: "short", side: Short, wantLiq: "109.45273631", safe: "109.4"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			liq := pricing.LiquidationPrice(test.side, d("1000"), 10_0000_0000, margin, 50)
			assert.True(t, d(test.wantLiq).Equal(liq), "liquidation price %s", liq)
			hit, err := pricing.Liquidatable(test.side, d("1000"), 10_0000_0000, margin, liq, 50)
			require.NoError(t, err)
			assert.True(t, hit, "liquidated at the liquidation price")
			hit, err = pricing.Liquidatable(test.side, d("1000"), 10_0000_0000, margin, d(test.safe), 50)
			require.NoError(t, err)
			assert.False(t, hit, "not liquidated before the liquidation price")
		})
	}
	assert.True(t, pricing.LiquidationPrice(Long, d("1000"), 10_0000_0000, 500_000_000, 50).IsZero(), "a fully funded long is never liquidated")
}

// 资金费率为正时多仓付、空仓收，为负时反过来；按带符号的值向下取整。
func TestFundingSignsAndRounding(t *testing.T) {
	tests := []struct {
		name  string
		side  PositionSide
		value string
		rate  string
		want  int
	}{
		{name: "long pays a positive rate", side: Long, value: "1000", rate: "0.0001", want: -50_000},
		{name: "short receives a positive rate", side: Short, value: "1000", rate: "0.0001", want: 50_000},
		{name: "long receives a negative rate", side: Long, value: "1000", rate: "-0.0001", want: 50_000},
		{name: "short pays a negative rate", side: Short, value: "1000", rate: "-0.0001", want: -50_000},
		{name: "a fraction of a unit paid costs a whole unit", side: Long, value: "1", rate: "0.00000001", want: -1},
		{name: "a fraction of a unit received is dropped", side: Short, value: "1", rate: "0.00000001", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			amount, err := pricing.Funding(test.side, d(test.value), d(test.rate))
			require.NoError(t, err)
			assert.Equal(t, test.want, amount)
		})
	}
}

// 按数量比例分出一份，向上取整；全部卖出或平掉时就是全部，取整不会累计。
func TestShareQuotaRoundsUpAndReleasesAllAtTheEnd(t *testing.T) {
	assert.Equal(t, 4, ShareQuota(10, 1, 3))
	assert.Equal(t, 3, ShareQuota(9, 1, 3))
	assert.Equal(t, 10, ShareQuota(10, 3, 3))
	assert.Equal(t, 1<<53-1, ShareQuota(1<<53-1, 1<<50, 1<<50))
	assert.Equal(t, 1, ShareQuota(1<<53-1, 1, 1<<60))
}
