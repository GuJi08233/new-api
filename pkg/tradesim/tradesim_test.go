package tradesim

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func levels(pairs ...string) []Level {
	out := make([]Level, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Level{Price: d(pairs[i]), Qty: d(pairs[i+1])})
	}
	return out
}

// 1 美元 = 500000 额度单位，手续费 0.1%。
var pricing = Pricing{QuotaPerUsd: decimal.NewFromInt(500000), FeeBps: 10}

func TestWalkTakesOnlyTheQuantityOnTheBook(t *testing.T) {
	asks := levels("100", "0.5", "101", "1", "105", "2")
	tests := []struct {
		name         string
		side         Side
		book         []Level
		qty          string
		limit        string
		step         string
		wantQty      string
		wantNotional string
	}{
		{name: "market buy spans two levels", side: Buy, book: asks, qty: "1.2", limit: "0", step: "0", wantQty: "1.2", wantNotional: "120.7"},
		{name: "depth caps the fill", side: Buy, book: asks, qty: "10", limit: "0", step: "0", wantQty: "3.5", wantNotional: "361"},
		{name: "limit stops at worse levels", side: Buy, book: asks, qty: "10", limit: "101", step: "0", wantQty: "1.5", wantNotional: "151"},
		{name: "step floors the fill from the last level", side: Buy, book: asks, qty: "1.234", limit: "0", step: "0.01", wantQty: "1.23", wantNotional: "123.73"},
		{name: "sell walks bids down to the limit", side: Sell, book: levels("99", "1", "98", "1", "90", "5"), qty: "3", limit: "98", step: "0", wantQty: "2", wantNotional: "197"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fill := Walk(test.book, test.side, d(test.qty), d(test.limit), d(test.step))
			assert.True(t, d(test.wantQty).Equal(fill.Qty), "qty %s", fill.Qty)
			assert.True(t, d(test.wantNotional).Equal(fill.Notional), "notional %s", fill.Notional)
			total := decimal.Zero
			for _, take := range fill.Takes {
				total = total.Add(take)
			}
			assert.True(t, fill.Qty.Equal(total), "takes add up to the fill")
		})
	}
}

func TestDepthCountsOnlyAcceptableLevels(t *testing.T) {
	asks := levels("100", "0.5", "101", "1", "105", "2")
	assert.True(t, d("1.5").Equal(Depth(asks, Buy, d("101"))))
	assert.True(t, d("3.5").Equal(Depth(asks, Buy, decimal.Zero)))
	assert.True(t, decimal.Zero.Equal(Depth(asks, Buy, d("99"))))
}

func TestQuotaRoundingFavorsTheSite(t *testing.T) {
	// 0.0000011 美元 = 0.55 个额度单位：扣款算 1，入账算 0。
	debit, err := pricing.DebitQuota(d("0.0000011"))
	require.NoError(t, err)
	assert.Equal(t, 1, debit)
	credit, err := pricing.CreditQuota(d("0.0000011"))
	require.NoError(t, err)
	assert.Equal(t, 0, credit)

	// 手续费 0.1%：1000 单位收 1，1001 单位向上取整收 2，0 单位不收。
	assert.Equal(t, 1, pricing.FeeQuota(1000))
	assert.Equal(t, 2, pricing.FeeQuota(1001))
	assert.Equal(t, 0, pricing.FeeQuota(0))
	assert.Equal(t, 0, Pricing{QuotaPerUsd: decimal.NewFromInt(500000)}.FeeQuota(1000))
}

func TestQuotaConversionRejectsOutOfRangeAmounts(t *testing.T) {
	_, err := pricing.DebitQuota(d("1e12"))
	assert.Error(t, err)
}

func TestBuyWithBudgetNeverSpendsMoreThanTheBudget(t *testing.T) {
	asks := levels("100", "0.5", "101", "1", "105", "2")
	tests := []struct {
		name    string
		budget  int
		limit   string
		step    string
		wantQty string
	}{
		// 40 美元按 100 的价格加 0.1% 手续费能买 0.399 个(39.9 + 0.0399)；200 美元吃完前两档，在第三档再买 0.464 个。
		{name: "budget inside the first level", budget: 20_000_000, limit: "0", step: "0.001", wantQty: "0.399"},
		{name: "budget spans levels", budget: 100_000_000, limit: "0", step: "0.001", wantQty: "1.964"},
		{name: "limit caps the budget fill", budget: 1_000_000_000, limit: "101", step: "0.001", wantQty: "1.5"},
		{name: "budget too small for one step", budget: 10, limit: "0", step: "0.001", wantQty: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fill, amount, fee, err := pricing.BuyWithBudget(asks, test.budget, d(test.limit), d(test.step))
			require.NoError(t, err)
			assert.True(t, d(test.wantQty).Equal(fill.Qty), "qty %s", fill.Qty)
			assert.LessOrEqual(t, amount+fee, test.budget)
			if fill.Qty.IsPositive() {
				// 再多买一个步长就超出预算。
				more := Walk(asks, Buy, fill.Qty.Add(d(test.step)), d(test.limit), d(test.step))
				moreAmount, moreFee, err := pricing.BuyCost(more)
				require.NoError(t, err)
				assert.True(t, more.Qty.Equal(fill.Qty) || moreAmount+moreFee > test.budget)
			}
		})
	}
}

func TestQtyUnitsRoundTrip(t *testing.T) {
	assert.Equal(t, int64(12345678), QtyUnits(d("0.123456789")))
	assert.True(t, d("0.12345678").Equal(QtyFromUnits(12345678)))
}
