package doufa

import (
	"errors"
	"slices"
)

// 秘境斗法的局外成长：道韵(每一轮结束时按打得多深得到)、用道韵升级的天赋、各流派的战绩，以及用游戏币买来的道具背包。
// 它存在问道存档里(wendao.Save.Rift)，一轮开始时把天赋带进去(NewRun)。

var (
	// ErrCrystals 表示道韵不够。
	ErrCrystals = errors.New("doufa: not enough crystals")
	// ErrMaxed 表示这种天赋已经满级。
	ErrMaxed = errors.New("doufa: talent is maxed")
	// ErrUnknown 表示没有这种天赋或道具。
	ErrUnknown = errors.New("doufa: unknown talent or item")
	// ErrNoItem 表示背包里没有这件道具。
	ErrNoItem = errors.New("doufa: item is not in the bag")
	// ErrItemLimit 表示背包放不下，或者这件道具现在用不了(每轮用完了、生命是满的……)。
	ErrItemLimit = errors.New("doufa: item cannot be used now")
)

// 商店道具：花游戏币买进局外的背包，在一轮的两间房之间用掉一件少一件，换不回游戏币。
const (
	ItemElixir  = "elixir"    // 续命丹：回复四成生命上限
	ItemFortune = "relic_box" // 福缘匣：随机得到一样还没有的祝福(标识沿用旧的，已经买下的数量接着用)
	ItemWard    = "ward"      // 护命符：这一轮里第一次倒下时以半条命复活，同一间房重新打
	// ItemsPerRun 是一轮最多用几件商店道具，倒下即结束是肉鸽的核心，道具不能把它抹掉。
	ItemsPerRun = 3
	// MaxItemStack 是背包里每种道具最多存几件。
	MaxItemStack = 99
)

// ShopItem 是商店里的一种道具，Price 是原价(单位为 0.01 游戏币)，实际价格再乘上管理员设的价格比例。
type ShopItem struct {
	Id    string `json:"id"`
	Price int    `json:"price"`
}

// ShopItems 是秘境商店的道具，按页面上的排列顺序。护命符最贵：复活一次相当于多打一轮。
var ShopItems = []ShopItem{
	{Id: ItemElixir, Price: 20},
	{Id: ItemFortune, Price: 50},
	{Id: ItemWard, Price: 100},
}

// ShopItemOf 返回一种道具，不在商店里时 ok 为 false。
func ShopItemOf(id string) (ShopItem, bool) {
	index := slices.IndexFunc(ShopItems, func(item ShopItem) bool { return item.Id == id })
	if index < 0 {
		return ShopItem{}, false
	}
	return ShopItems[index], true
}

// Record 是一个流派的战绩：打过几轮，一轮最深走到哪里(Run.Score)。
type Record struct {
	Runs int `json:"runs"`
	Best int `json:"best"`
}

// Meta 是玩家在秘境斗法里的局外存档。
type Meta struct {
	// Crystals 是现有的道韵，Earned 是累计得到的。
	Crystals int `json:"crystals"`
	Earned   int `json:"earned"`
	// Runs 是打完(倒下或放弃)的轮数。
	Runs    int                `json:"runs"`
	Talents map[string]int     `json:"talents"`
	Records map[string]*Record `json:"records"`
	// Items 是用游戏币买来、还没用掉的商店道具。
	Items map[string]int `json:"items"`
}

// Normalize 补上存档里省略的空表和各流派的战绩，读出存档之后调用。
func (m *Meta) Normalize() {
	if m.Talents == nil {
		m.Talents = map[string]int{}
	}
	if m.Records == nil {
		m.Records = map[string]*Record{}
	}
	if m.Items == nil {
		m.Items = map[string]int{}
	}
	for class := range content.Classes {
		if m.Records[class] == nil {
			m.Records[class] = &Record{}
		}
	}
}

// Buy 花道韵把一种天赋升一级。
func (m *Meta) Buy(id string) error {
	index := slices.IndexFunc(content.Talents, func(t Talent) bool { return t.Id == id })
	if index < 0 {
		return ErrUnknown
	}
	level := m.Talents[id]
	costs := content.Talents[index].Costs
	if level >= len(costs) {
		return ErrMaxed
	}
	if m.Crystals < costs[level] {
		return ErrCrystals
	}
	m.Crystals -= costs[level]
	m.Talents[id] = level + 1
	return nil
}

// Settle 把结束(倒下或放弃)的一轮记进存档：发道韵、记战绩，返回这一轮得到的道韵。
func (m *Meta) Settle(r *Run) int {
	gained := r.Crystals()
	m.Crystals += gained
	m.Earned += gained
	m.Runs++
	record := m.Records[r.Class]
	if record == nil {
		record = &Record{}
		m.Records[r.Class] = record
	}
	record.Runs++
	record.Best = max(record.Best, r.Score())
	return gained
}

// AddItem 把 n 件道具放进背包，超过每种道具的上限时返回 ErrItemLimit。
func (m *Meta) AddItem(id string, n int) error {
	if _, ok := ShopItemOf(id); !ok || n < 1 {
		return ErrUnknown
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

// Talents 是所有天赋(按页面上的排列顺序)，页面按它列出天赋和价格。
func Talents() []Talent { return content.Talents }
