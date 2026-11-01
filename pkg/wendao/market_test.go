package wendao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 一单的数量要在这种货物的范围里，价格在 0 到上限之间；0 灵石只能指名给道友(赠送)；符箓与不认识的货物不能卖。
func TestValidListing(t *testing.T) {
	cases := []struct {
		goods    string
		count    int
		price    int64
		targeted bool
		ok       bool
	}{
		{"herb", 10, 100, false, true},
		{"herb", 9, 100, false, false},
		{"herb", 100001, 100, false, false},
		{"pill_break", 1, 500, false, true},
		{"pill_break", 100, 500, false, false},
		{"rare_lotus", 1, 0, false, false},
		{"rare_lotus", 1, 0, true, true},
		{"pill_heal", 1, -1, true, false},
		{"pill_heal", 1, MarketMaxPrice + 1, false, false},
		{"spirit_talisman", 1, 10, false, false},
		{"pill_unknown", 1, 10, false, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.ok, ValidListing(c.goods, c.count, c.price, c.targeted), "%+v", c)
	}
}

// 挂单取出货物，不够时什么都不改；撤单放回；丹药、灵药取完后不留空项。
func TestTakeAndPutGoods(t *testing.T) {
	s := NewSect(1, 0)
	s.Res.Herb = 50
	s.Pills[PillBreak] = 2
	assert.ErrorIs(t, s.TakeGoods("herb", 51), ErrCost)
	require.NoError(t, s.TakeGoods("herb", 50))
	assert.Equal(t, 0, s.Res.Herb)
	require.NoError(t, s.TakeGoods("pill_break", 2))
	_, left := s.Pills[PillBreak]
	assert.False(t, left)
	require.NoError(t, s.PutGoods("rare_lotus", 1))
	assert.Equal(t, 1, s.Rare[RareLotus])
	assert.ErrorIs(t, s.TakeGoods("spirit_talisman", 1), ErrAction)
}

// 成交：买家付全价拿货，卖家收到扣掉 5% 抽成的灵石，两边大事记各记一笔；买家灵石不够时什么都不改。
func TestSettleTrade(t *testing.T) {
	seller, buyer := NewSect(1, 0), NewSect(2, 0)
	seller.Res.Stone, buyer.Res.Stone = 0, 1000
	_, err := SettleTrade(seller, buyer, "pill_qi", 3, 1001, 10)
	assert.ErrorIs(t, err, ErrCost)
	assert.Equal(t, 0, buyer.Pills[PillQi])

	tax, err := SettleTrade(seller, buyer, "pill_qi", 3, 999, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(49), tax)
	assert.Equal(t, 1, buyer.Res.Stone)
	assert.Equal(t, 3, buyer.Pills[PillQi])
	assert.Equal(t, 950, seller.Res.Stone)
	assert.Equal(t, Ev{K: "sold", S: "pill_qi", N: 3, V: 950, At: 10}, seller.Log[len(seller.Log)-1])
	assert.Equal(t, Ev{K: "bought", S: "pill_qi", N: 3, V: 999, At: 10}, buyer.Log[len(buyer.Log)-1])

	tax, err = SettleTrade(seller, buyer, "rare_lotus", 1, 0, 20)
	require.NoError(t, err, "a gift costs nothing")
	assert.Equal(t, int64(0), tax)
	assert.Equal(t, 1, buyer.Rare[RareLotus])
}
