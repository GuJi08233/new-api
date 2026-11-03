// Package wendao 是问道的规则包：宗门经营、主角历练与三个板块共用的存档。秘境(即时斗法肉鸽)的规则在 pkg/doufa，
// 历练里的卡牌战斗在 pkg/dungeon。
// 世界设定、名词对照与发游戏币的规则见同目录的 world.md。
package wendao

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// SaveKey 是问道在 model.GameSave 里的存档标识。宗门、主角与秘境共用这一份存档：灵石、丹药这些资源只有一份，
// 一个板块的产出另一个板块马上能用，跨板块的改动在同一次改写里完成。
const SaveKey = "wendao"

// SaveSchema 是问道存档的格式版本。往存档里加了旧程序不认识的字段(旧程序读的时候会忽略，写回去就把它丢了)，或者改了
// 已有字段的含义，就把它加一，并在 ParseSave 里写好从上一版迁移过来的做法。
//
// 程序读到比自己新的存档时拒绝(ErrNewerSave)：退回旧版本时宁可报错，也不把新版本的进度写丢。v2.16.23 及以前的程序
// 还不认识这个字段，它们写的存档没有 schema(读出来是 0)，缺的字段由 normalize 补成空的。
//
// 各版本：1 是加上 schema 本身；2 是历练节点的通过记录多了支线剧情的抉择(NodeRecord.Pick)；3 是宗门记下离开很久
// 以后回来的那一次结算(Sect.Back)，以及同一版里还没发布时加上的功法拆解次数(Hero.Salvaged)与请求收据(Receipts)；4 是
// 主角的宗门身份(Hero.Path)，以及散修、宗门弟子没有主殿的储物处(Sect 的主殿为 0 级)；5 是秘境换成斗法以后的局外成长
// (Rift：道韵、天赋、战绩与道具背包)，旧版卡牌秘境的 Abyss 在读出时并进去；6 是斗法契约(Duels：悬赏、宗门任务、入门试炼
// 与进行中的一场)。
const SaveSchema = 6

// ErrNewerSave 表示存档是更新版本的程序写的，这个程序不认识其中的一部分，不能读也不能改写。
var ErrNewerSave = errors.New("wendao: the save was written by a newer version")

// Save 是玩家在问道里的全部局外进度。Sect 在开宗立派之前为 nil；Hero 是历练的主角，创建之前为 nil；Rift 是秘境斗法的
// 局外成长(道韵、天赋、战绩、道具背包)。
type Save struct {
	// Schema 是写这份存档的程序的 SaveSchema。
	Schema int   `json:"schema"`
	Sect   *Sect `json:"sect,omitempty"`
	Hero   *Hero `json:"hero,omitempty"`
	// Abyss 是旧版卡牌秘境的局外成长，只在读旧存档时用一次(见 riftFromAbyss)，读出以后就置空，不再写回。
	Abyss *dungeon.Meta `json:"abyss,omitempty"`
	Rift  *doufa.Meta   `json:"rift"`
	// Duels 是主角手上的斗法契约(悬赏、宗门任务、入门试炼)，见 duel.go。
	Duels *DuelBook `json:"duels,omitempty"`
	// AbyssLoot 记今天九幽秘境无尽模式已经往宗门带回几次材料。
	AbyssLoot LootDay `json:"abyss_loot"`
	// Receipts 是最近做过的几次操作的请求收据，重发同一次请求时不做第二遍，见 Admit。
	Receipts []Receipt `json:"receipts,omitempty"`
}

// LootDay 是某一天已经领了几次。
type LootDay struct {
	Day  string `json:"day"`
	Runs int    `json:"runs"`
}

// AbyssLootRuns 是每天前几轮无尽秘境(倒下或放弃都算)会往宗门库房带回材料。
const AbyssLootRuns = 3

// RiftLootOf 是一轮秘境斗法带回宗门的材料：按打通的房间与重数。秘境给的是宗门资源，不影响秘境自己的游戏币奖励，宗门的
// 强弱也带不进秘境。
func RiftLootOf(r *doufa.Run) Cost {
	return Cost{Stone: 12*r.Cleared + 150*r.Floors(), Herb: 5*r.Cleared + 50*r.Floors(), Ore: 3*r.Cleared + 60*r.Floors()}
}

// GrantRiftLoot 把一轮秘境斗法带回的材料放进宗门库房，返回放进去的数量：还没有宗门，或者今天已经带回 AbyssLootRuns 次
// 时什么都不给。
func (s *Save) GrantRiftLoot(r *doufa.Run, day string) Cost {
	if s.Sect == nil {
		return Cost{}
	}
	if s.AbyssLoot.Day != day {
		s.AbyssLoot = LootDay{Day: day}
	}
	loot := RiftLootOf(r)
	if s.AbyssLoot.Runs >= AbyssLootRuns || loot == (Cost{}) {
		return Cost{}
	}
	s.AbyssLoot.Runs++
	s.Sect.gain(loot)
	return loot
}

// ParseSave 读出存档，空字符串是一份新存档；更新版本的程序写的存档返回 ErrNewerSave。
func ParseSave(data string) (*Save, error) {
	save := &Save{}
	if strings.TrimSpace(data) != "" {
		if err := common.UnmarshalJsonStr(data, save); err != nil {
			return nil, err
		}
	}
	if save.Schema > SaveSchema {
		return nil, ErrNewerSave
	}
	// 第 0 版(还没有 schema 的存档)到第 1 版只多了这个字段；第 1 版到第 2 版多的抉择只出现在新加的支线节点上，旧存档
	// 还没走过支线；第 2 版到第 3 版多的回来记录在下一次离开很久以后才有。都没有要迁移的内容。
	save.Schema = SaveSchema
	if save.Sect != nil {
		save.Sect.normalize()
	}
	if save.Hero != nil {
		save.Hero.normalize()
	}
	if save.Rift == nil {
		save.Rift = riftFromAbyss(save.Abyss)
	}
	save.Abyss = nil
	save.Rift.Normalize()
	save.book()
	return save, nil
}

// riftFromAbyss 把旧版卡牌秘境的局外成长并进秘境斗法：道韵连同已经花在旧天赋与解锁流派上的一起原数退回，累计与轮数照旧，
// 商店道具里续命丹、护命符、福缘匣(原法宝匣)接着用，丹符补给与灵珠袋在斗法里没有用处，折成续命丹。
func riftFromAbyss(old *dungeon.Meta) *doufa.Meta {
	meta := &doufa.Meta{}
	if old == nil {
		return meta
	}
	meta.Crystals, meta.Earned, meta.Runs = old.Crystals, old.Earned, old.Runs
	for _, talent := range dungeon.Talents {
		for level := range min(old.Talents[talent.Id], len(talent.Costs)) {
			meta.Crystals += talent.Costs[level]
		}
	}
	for _, class := range old.Classes {
		meta.Crystals += dungeon.ClassCosts[class]
	}
	meta.Items = map[string]int{}
	for id, count := range old.Items {
		switch id {
		case dungeon.ItemElixir, dungeon.ItemPotion, dungeon.ItemGold:
			meta.Items[doufa.ItemElixir] += count
		case dungeon.ItemRelic:
			meta.Items[doufa.ItemFortune] += count
		case dungeon.ItemWard:
			meta.Items[doufa.ItemWard] += count
		}
	}
	for id, count := range meta.Items {
		meta.Items[id] = min(count, doufa.MaxItemStack)
	}
	return meta
}

// UpdateSave 读出存档、交给 edit 改写，再写回 JSON。edit 返回错误时不写回。
func UpdateSave(data string, edit func(save *Save) error) (string, error) {
	save, err := ParseSave(data)
	if err != nil {
		return "", err
	}
	if err := edit(save); err != nil {
		return "", err
	}
	return save.Encode()
}

// TickHero 结算主角到 now 为止的闭关修为，并孵出到时间的灵兽蛋。宗门的操作也调用它，主角不会因为一直只经营宗门而
// 少算离线修为。
func (s *Save) TickHero(now int64) {
	if s.Hero != nil && s.Sect != nil {
		s.Hero.tick(s.Sect, now)
		s.Hero.hatchBeasts(now)
	}
}

// Encode 把存档写成 JSON，记上这个程序的格式版本。
func (s *Save) Encode() (string, error) {
	s.Schema = SaveSchema
	data, err := common.Marshal(s)
	return string(data), err
}
