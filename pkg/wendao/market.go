package wendao

import (
	"errors"
	"slices"
	"strings"
)

// ErrMarketHall 表示主殿还没到 MarketMinHall 级，不能在万宝阁买卖。
var ErrMarketHall = errors.New("sect: main hall is too low for the market")

// CanTrade 表示宗门能在万宝阁买卖，不能时返回 ErrMarketHall。
func (s *Sect) CanTrade() error {
	if s.Buildings[BuildingHall] < MarketMinHall {
		return ErrMarketHall
	}
	return nil
}

// 万宝阁：掌门之间用灵石买卖灵草、玄铁、丹药与灵药。卖家挂单时货物从宗门里取出来押在阁里，有人买下时灵石扣掉一成的二十分之一
// (MarketTaxPct)作为阁里的抽成(直接销毁)，其余记给卖家；挂单可以指定只卖给某位道友，指定了的可以标 0 灵石，就是赠送。
// 挂单与成交记在数据库里(model.WendaoListing)，这里只管货物与价格的规则。万宝阁只流通宗门的东西，不涉及游戏币。

const (
	// MarketMinHall 是主殿几级才能在万宝阁买卖：新开的宗门不能马上进来倒卖。
	MarketMinHall = 2
	// MarketMaxListings 是一位掌门同时最多挂几单(过期还没取回的也算)。
	MarketMaxListings = 6
	// MarketTaxPct 是成交时抽走灵石的百分比。
	MarketTaxPct = 5
	// MarketListingSeconds 是一单挂多久，过期之后买不了，卖家取回货物。
	MarketListingSeconds = 3 * 24 * 60 * 60
	// MarketMaxPrice 是一单最多标多少灵石。
	MarketMaxPrice = 10_000_000
)

// MarketGood 是万宝阁里能卖的一种货物：Min、Max 是一单的数量范围(灵草、玄铁按批卖，丹药、灵药按颗)。
type MarketGood struct {
	Key string `json:"key"`
	Min int    `json:"min"`
	Max int    `json:"max"`
}

// MarketGoods 是能在万宝阁买卖的货物，按页面上的顺序。符箓是用游戏币买的，只能自己用，不能转卖。
var MarketGoods = []MarketGood{
	{Key: "herb", Min: 10, Max: 100000},
	{Key: "ore", Min: 10, Max: 100000},
	{Key: "pill_" + PillHeal, Min: 1, Max: 99},
	{Key: "pill_" + PillQi, Min: 1, Max: 99},
	{Key: "pill_" + PillPeiyuan, Min: 1, Max: 99},
	{Key: "pill_" + PillBreak, Min: 1, Max: 99},
	{Key: "pill_" + PillMarrow, Min: 1, Max: 99},
	{Key: "rare_" + RareFruit, Min: 1, Max: 99},
	{Key: "rare_" + RareLingzhi, Min: 1, Max: 99},
	{Key: "rare_" + RareLotus, Min: 1, Max: 99},
}

// MarketGoodOf 返回一种货物，不能卖的 ok 为 false。
func MarketGoodOf(key string) (MarketGood, bool) {
	index := slices.IndexFunc(MarketGoods, func(g MarketGood) bool { return g.Key == key })
	if index < 0 {
		return MarketGood{}, false
	}
	return MarketGoods[index], true
}

// ValidListing 检查一单的货物、数量与价格：数量在这种货物的范围里，价格在 0 到 MarketMaxPrice 之间，0 灵石只能指定道友(赠送)。
func ValidListing(goods string, count int, price int64, targeted bool) bool {
	good, ok := MarketGoodOf(goods)
	if !ok || count < good.Min || count > good.Max || price < 0 || price > MarketMaxPrice {
		return false
	}
	return price > 0 || targeted
}

// MarketTax 是一单成交时抽走的灵石。
func MarketTax(price int64) int64 { return price * MarketTaxPct / 100 }

// stock 返回宗门里这种货物现在有多少，以及把它改成另一个数的方法；不能卖的货物 ok 为 false。
func (s *Sect) stock(goods string) (have int, set func(int), ok bool) {
	inMap := func(m map[string]int, key string) (int, func(int), bool) {
		return m[key], func(n int) {
			if n == 0 {
				delete(m, key)
				return
			}
			m[key] = n
		}, true
	}
	switch {
	case goods == "herb":
		return s.Res.Herb, func(n int) { s.Res.Herb = n }, true
	case goods == "ore":
		return s.Res.Ore, func(n int) { s.Res.Ore = n }, true
	case strings.HasPrefix(goods, "pill_") && slices.Contains(PillKeys, goods[5:]):
		return inMap(s.Pills, goods[5:])
	case strings.HasPrefix(goods, "rare_") && slices.Contains(RareKeys, goods[5:]):
		return inMap(s.Rare, goods[5:])
	}
	return 0, nil, false
}

// TakeGoods 从宗门里取出 n 份货物押到万宝阁，不够时什么都不改。
func (s *Sect) TakeGoods(goods string, n int) error {
	have, set, ok := s.stock(goods)
	if !ok || n < 1 {
		return ErrAction
	}
	if have < n {
		return ErrCost
	}
	set(have - n)
	return nil
}

// PutGoods 把 n 份货物放进宗门：买到的货，或者撤单、过期取回的货。
func (s *Sect) PutGoods(goods string, n int) error {
	have, set, ok := s.stock(goods)
	if !ok || n < 1 {
		return ErrAction
	}
	set(have + n)
	return nil
}

// SettleTrade 结一单交易：买家付 price 灵石拿到货物，卖家收下扣掉抽成的灵石，两边的大事记各记一笔，返回抽走的灵石。买家灵石
// 不够时什么都不改。卖家的货物在挂单时已经取出。
func SettleTrade(seller *Sect, buyer *Sect, goods string, count int, price int64, now int64) (int64, error) {
	if price < 0 || price > MarketMaxPrice || int64(buyer.Res.Stone) < price {
		return 0, ErrCost
	}
	if err := buyer.PutGoods(goods, count); err != nil {
		return 0, err
	}
	tax := MarketTax(price)
	buyer.Res.Stone -= int(price)
	seller.Res.Stone += int(price - tax)
	seller.record(now, []Ev{{K: "sold", S: goods, N: count, V: int(price - tax)}})
	buyer.record(now, []Ev{{K: "bought", S: goods, N: count, V: int(price)}})
	return tax, nil
}
