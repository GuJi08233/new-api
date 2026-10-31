// Package wendao 是问道的规则包：宗门经营、主角历练与三个板块共用的存档。秘境(卡牌肉鸽)的规则在 pkg/dungeon。
// 世界设定、名词对照与发游戏币的规则见同目录的 world.md。
package wendao

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// SaveKey 是问道在 model.GameSave 里的存档标识。宗门、主角与秘境共用这一份存档：灵石、丹药这些资源只有一份，
// 一个板块的产出另一个板块马上能用，跨板块的改动在同一次改写里完成。
const SaveKey = "wendao"

// Save 是玩家在问道里的全部局外进度。Sect 在开宗立派之前为 nil；Hero 是历练的主角，创建之前为 nil；Abyss 是秘境的
// 局外成长(魂晶、职业、天赋、试炼星级)。
type Save struct {
	Sect  *Sect         `json:"sect,omitempty"`
	Hero  *Hero         `json:"hero,omitempty"`
	Abyss *dungeon.Meta `json:"abyss"`
	// AbyssLoot 记今天九幽秘境无尽模式已经往宗门带回几次材料。
	AbyssLoot LootDay `json:"abyss_loot"`
}

// LootDay 是某一天已经领了几次。
type LootDay struct {
	Day  string `json:"day"`
	Runs int    `json:"runs"`
}

// AbyssLootRuns 是每天前几轮无尽秘境(倒下或放弃都算)会往宗门库房带回材料。
const AbyssLootRuns = 3

// AbyssLootOf 是一轮无尽秘境带回宗门的材料：按到达的层数、打败的强敌与首领，劫数每高一级多一成。秘境给的是宗门资源，
// 不影响秘境自己的游戏币奖励，宗门的强弱也带不进秘境。
func AbyssLootOf(r *dungeon.Run) Cost {
	base := Cost{Stone: 15*r.Floor + 60*r.Bosses, Herb: 6*r.Floor + 20*r.Bosses, Ore: 25*r.Elites + 30*r.Bosses}
	return base.scale(1 + 0.1*float64(r.Abyss))
}

// GrantAbyssLoot 把一轮无尽秘境带回的材料放进宗门库房，返回放进去的数量：还没有宗门、不是无尽模式，或者今天已经带回
// AbyssLootRuns 次时什么都不给。
func (s *Save) GrantAbyssLoot(r *dungeon.Run, day string) Cost {
	if s.Sect == nil || r.Mode != dungeon.ModeEndless {
		return Cost{}
	}
	if s.AbyssLoot.Day != day {
		s.AbyssLoot = LootDay{Day: day}
	}
	loot := AbyssLootOf(r)
	if s.AbyssLoot.Runs >= AbyssLootRuns || loot == (Cost{}) {
		return Cost{}
	}
	s.AbyssLoot.Runs++
	s.Sect.gain(loot)
	return loot
}

// ParseSave 读出存档，空字符串是一份新存档。
func ParseSave(data string) (*Save, error) {
	save := &Save{}
	if strings.TrimSpace(data) != "" {
		if err := common.UnmarshalJsonStr(data, save); err != nil {
			return nil, err
		}
	}
	if save.Sect != nil {
		save.Sect.normalize()
	}
	if save.Hero != nil {
		save.Hero.normalize()
	}
	if save.Abyss == nil {
		save.Abyss = &dungeon.Meta{}
	}
	save.Abyss.Normalize()
	return save, nil
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

// TickHero 结算主角到 now 为止的闭关修为。宗门的操作也调用它，主角不会因为一直只经营宗门而少算离线修为。
func (s *Save) TickHero(now int64) {
	if s.Hero != nil && s.Sect != nil {
		s.Hero.tick(s.Sect, now)
	}
}

// Encode 把存档写成 JSON。
func (s *Save) Encode() (string, error) {
	data, err := common.Marshal(s)
	return string(data), err
}
