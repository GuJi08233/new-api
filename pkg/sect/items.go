package sect

import (
	"errors"
	"slices"
)

// 商店道具：花游戏币买的一次性道具。丹药买了直接放进丹房，和自己炼的一样用；符箓放进背包(Items)，用掉一张少一张。
// 道具只能在宗门里用，换不回游戏币。大比对手跟着出战弟子的战力走，花钱加快的成长换不来大比连胜。

const (
	ItemSpirit  = "spirit_talisman" // 聚灵符：马上得到按现在产量算的两小时灵石、灵草与玄铁
	ItemRecruit = "recruit_order"   // 招贤令：不花灵石马上换一批候选弟子
	ItemSwift   = "swift_talisman"  // 神行符：所有在外的历练队伍马上回到宗门(还要掌门领取)
	// MaxItemStack 是背包里每种符箓最多存几张，也是丹房里买来的丹药一次最多补到几颗。
	MaxItemStack = 99
	// spiritHours 是聚灵符抵几个小时的产出。
	spiritHours = 2
)

// ShopItem 是商店里的一种道具，Price 是原价(单位为 0.01 游戏币)，实际价格再乘上管理员设的价格比例。Pill 不为空时买的是丹药。
type ShopItem struct {
	Id    string `json:"id"`
	Price int    `json:"price"`
	Pill  string `json:"pill,omitempty"`
}

// ShopItems 是宗门商店的道具，按页面上的排列顺序。洗髓丹直接提高资质，不卖。
var ShopItems = []ShopItem{
	{Id: ItemSpirit, Price: 30},
	{Id: ItemRecruit, Price: 10},
	{Id: ItemSwift, Price: 30},
	{Id: "pill_" + PillHeal, Price: 15, Pill: PillHeal},
	{Id: "pill_" + PillQi, Price: 20, Pill: PillQi},
	{Id: "pill_" + PillBreak, Price: 50, Pill: PillBreak},
}

var (
	// ErrNoItem 表示背包里没有这件道具。
	ErrNoItem = errors.New("sect: item is not in the bag")
	// ErrItemLimit 表示背包或丹房里这种道具已经放满，或者现在用了没有效果(例如没有在外的历练队伍)。
	ErrItemLimit = errors.New("sect: item cannot be used now")
)

// ShopItemOf 返回一种道具，不在商店里时 ok 为 false。
func ShopItemOf(id string) (ShopItem, bool) {
	index := slices.IndexFunc(ShopItems, func(item ShopItem) bool { return item.Id == id })
	if index < 0 {
		return ShopItem{}, false
	}
	return ShopItems[index], true
}

// AddItem 把买来的 n 件道具放进宗门：丹药进丹房，符箓进背包，超过上限时返回 ErrItemLimit。
func (s *Sect) AddItem(id string, n int) error {
	item, ok := ShopItemOf(id)
	if !ok || n < 1 {
		return ErrAction
	}
	if item.Pill != "" {
		if s.Pills[item.Pill]+n > MaxItemStack {
			return ErrItemLimit
		}
		s.Pills[item.Pill] += n
		return nil
	}
	if s.Items[id]+n > MaxItemStack {
		return ErrItemLimit
	}
	s.Items[id] += n
	return nil
}

// useItem 用掉背包里的一张符箓。
func (s *Sect) useItem(id string, now int64) ([]Ev, error) {
	if item, ok := ShopItemOf(id); !ok || item.Pill != "" {
		return nil, ErrAction
	}
	if s.Items[id] < 1 {
		return nil, ErrNoItem
	}
	var evs []Ev
	switch id {
	case ItemSpirit:
		rates := s.Rates(now)
		evs = s.gain(Cost{Stone: int(rates.Stone * spiritHours), Herb: int(rates.Herb * spiritHours), Ore: int(rates.Ore * spiritHours)})
		if len(evs) == 0 {
			return nil, ErrItemLimit
		}
	case ItemRecruit:
		s.refreshRecruits(now)
	case ItemSwift:
		hurried := false
		for _, m := range s.Missions {
			if m.End > now {
				m.End, hurried = now, true
			}
		}
		if !hurried {
			return nil, ErrItemLimit
		}
	}
	s.Items[id]--
	if s.Items[id] == 0 {
		delete(s.Items, id)
	}
	return append(evs, Ev{K: "item", S: id}), nil
}
