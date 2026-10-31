package dungeon

import (
	"errors"
	"slices"
)

// 商店道具：花游戏币买进局外存档的背包，在任何一种模式的一轮里用掉一件少一件(练习里用也一样消耗)。道具只能在远征里用，
// 换不回游戏币。为了一轮远征仍然要靠打，每轮最多用 ItemsPerRun 件，护命符每轮最多 1 张。

const (
	ItemElixir  = "elixir"       // 回春药剂：回复 40% 最大生命，战斗内外都能用
	ItemPotion  = "potion_crate" // 药水补给：随机得到一瓶药水，药水栏要有空位
	ItemGold    = "gold_pouch"   // 金币袋：获得 100 金币
	ItemRelic   = "relic_box"    // 遗物匣：随机得到一件还没有的遗物
	ItemWard    = "ward"         // 护命符：这一轮第一次倒下时以 50% 最大生命复活(在不死鸟羽毛之后生效)
	ItemsPerRun = 5
	// MaxItemStack 是背包里每种道具最多存几件。
	MaxItemStack = 99
)

// ShopItem 是商店里的一种道具，Price 是原价(单位为 0.01 游戏币)，实际价格再乘上管理员设的价格比例。
type ShopItem struct {
	Id    string `json:"id"`
	Price int    `json:"price"`
}

// ShopItems 是深渊远征商店的道具，按页面上的排列顺序。护命符最贵：倒下即结束是肉鸽的核心，复活一次相当于多打一轮。
var ShopItems = []ShopItem{
	{Id: ItemElixir, Price: 20},
	{Id: ItemPotion, Price: 10},
	{Id: ItemGold, Price: 20},
	{Id: ItemRelic, Price: 50},
	{Id: ItemWard, Price: 100},
}

var (
	// ErrNoItem 表示背包里没有这件道具。
	ErrNoItem = errors.New("dungeon item is not in the bag")
	// ErrItemLimit 表示这一轮能用的道具已经用完，或者这件道具现在用不了(例如药水栏满了、护命符已经在身上)。
	ErrItemLimit = errors.New("dungeon item cannot be used now")
)

// ShopItemOf 返回一种道具，不在商店里时 ok 为 false。
func ShopItemOf(id string) (ShopItem, bool) {
	index := slices.IndexFunc(ShopItems, func(item ShopItem) bool { return item.Id == id })
	if index < 0 {
		return ShopItem{}, false
	}
	return ShopItems[index], true
}

// AddItem 把 n 件道具放进背包，超过每种道具的上限时返回 ErrItemLimit。
func (m *Meta) AddItem(id string, n int) error {
	if _, ok := ShopItemOf(id); !ok || n < 1 {
		return ErrAction
	}
	if m.Items[id]+n > MaxItemStack {
		return ErrItemLimit
	}
	m.Items[id] += n
	return nil
}

// TakeItem 从背包里拿出一件道具。
func (m *Meta) TakeItem(id string) error {
	if m.Items[id] < 1 {
		return ErrNoItem
	}
	m.Items[id]--
	if m.Items[id] == 0 {
		delete(m.Items, id)
	}
	return nil
}

// UseItem 在这一轮里用掉一件商店道具。用不了时返回 ErrItemLimit，状态不变。
func (r *Run) UseItem(id string) error {
	r.Events = nil
	if r.Over() || r.Phase == PhaseDone {
		return ErrAction
	}
	if _, ok := ShopItemOf(id); !ok {
		return ErrAction
	}
	if r.ItemsUsed >= ItemsPerRun {
		return ErrItemLimit
	}
	switch id {
	case ItemElixir:
		if r.HP >= r.MaxHP {
			return ErrItemLimit
		}
		r.heal(max(r.MaxHP*40/100, 1))
	case ItemPotion:
		if len(r.Potions) >= r.PotionSlots() {
			return ErrItemLimit
		}
		potion := potionList[r.Rng.Intn(len(potionList))]
		r.Potions = append(r.Potions, potion)
	case ItemGold:
		r.Gold += 100
		r.emit("gold", playerTarget, 100, "")
	case ItemRelic:
		relic := r.randomRelic(r.rollRelicRarity())
		if relic == "" {
			return ErrItemLimit
		}
		r.gainRelic(relic)
	case ItemWard:
		if r.Ward || r.WardUsed {
			return ErrItemLimit
		}
		r.Ward = true
	}
	r.ItemsUsed++
	// 道具的效果(回血、金币、遗物)各自有事件，这一条让页面提示用掉了哪件道具。
	r.emit("item", playerTarget, r.ItemsUsed, id)
	return nil
}
