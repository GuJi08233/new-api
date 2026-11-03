package wendao

import (
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// 输掉一场之后的复盘(长期方案 B4)：落败页说明这一场输在哪里，再按主角现在的状态列出几条能去变强的路。复盘跟着这一次
// 操作的事件给页面——hero_review 的 S 是输法，hero_tip 的 S 是一条路、N 是这条路附带的数——不进存档；这一场打了多久、
// 打出多少伤害、敌人还剩多少生命，页面从打完的战斗里自己读。退走不复盘。

// 输法。
const (
	CauseClose = "close" // 差一点：敌人剩下的生命合计不到四分之一
	CauseBurst = "burst" // 被打得太快：十五秒内倒下
	CauseSlow  = "slow"  // 输出跟不上：敌人还很健康，自己被慢慢拖垮
)

// reviewTips 是落败页最多列几条变强的路。
const reviewTips = 3

// tipOrders 是每种输法下各条路的先后：被打得太快先补生命与护体，输出跟不上先补力量与功法，差一点先看下一场马上就能
// 带上的(丹符、灵兽、随行弟子)。丹药的路用丹药的标识(cuiti、jingang、longhu、jiuzhuan)，祭炼按法宝的路数分三条。
var tipOrders = map[string][]string{
	CauseBurst: {"break", ElixirGuard, "refine_" + RefineGuard, "spring", ElixirTemper, "refine_" + RefineVital, "wear",
		"cultivate", "potions", "team", "beast", "upgrade"},
	CauseSlow: {"break", ElixirTiger, ElixirGolden, "refine_" + RefineEdge, "upgrade", "wear", "beast", "team", "potions",
		"cultivate"},
	CauseClose: {"potions", "beast", "team", ElixirGuard, ElixirTiger, "wear", "upgrade", "cultivate", "spring", ElixirTemper,
		"break"},
}

// reviewDefeat 复盘服务端重放出的落败结果。只列现在做得到的路：境界够、屋子建了、还没
// 到顶；空明天象本来就不让带丹符、随行弟子与灵兽，不拿这几条说事。
func (h *Hero) reviewDefeat(s *Sect, battle *HeroBattle, fight *doufa.Fight, now int64) []Ev {
	left, total := 0, 0
	for _, foe := range fight.Foes {
		left += max(foe.HP, 0)
		total += foe.MaxHP
	}
	cause := CauseSlow
	switch {
	case total > 0 && fight.Wave >= len(fight.Waves) && left*4 <= total:
		cause = CauseClose
	case fight.Tick <= 15*doufa.FPS:
		cause = CauseBurst
	}

	open := map[string]int{}
	if h.Bottleneck() && h.Realm < MaxRealm {
		open["break"] = h.Realm + 1
	}
	if h.Layer < MaxLayer {
		open["cultivate"] = heroLayerHP
	}
	if h.Cave[RoomSpring] < RoomCap(h.Realm) {
		open["spring"] = springHP
	}
	for _, recipe := range ElixirRecipes {
		// 丹室还炼不了、手上也没有这种丹的不提。
		if h.Cave[RoomFurnace] < recipe.Furnace && h.Elixirs[recipe.Id] == ([Grades]int{}) {
			continue
		}
		switch recipe.Id {
		case ElixirTemper:
			if room := TemperCap(h.Realm) - h.Tempered; room > 0 {
				open[recipe.Id] = room
			}
		case ElixirGuard, ElixirTiger:
			if h.Buffs[recipe.Id] == nil {
				open[recipe.Id] = recipe.Effect[GradeLow]
			}
		case ElixirGolden:
			if h.Golden < GoldenMax {
				open[recipe.Id] = GoldenMax - h.Golden
			}
		}
	}
	refineGain := map[string]int{RefineVital: RefineHP, RefineGuard: RefineBlock, RefineEdge: refineEdgeStep}
	for _, relic := range h.Worn {
		kind := RefineKind(relic)
		if h.Refine[relic] >= min(h.Cave[RoomForge], MaxRefine) {
			continue
		}
		if kind == RefineEdge && h.refineBonus().Strength >= RefineStrengthMax {
			continue
		}
		open["refine_"+kind] = refineGain[kind]
	}
	if spare := min(h.RelicSlots(), len(h.Relics)) - len(h.Worn); spare > 0 {
		open["wear"] = spare
	}

	upgradable := 0
	for _, card := range h.Cards {
		if dungeon.Upgradable(card) {
			upgradable++
		}
	}
	if upgradable > 0 {
		open["upgrade"] = upgradable
	}

	bare := false
	if w := h.Weekly; battle.Trial > 0 && w != nil && w.Week == battle.Week && battle.Trial <= len(w.Trials) {
		bare = weeklySign(w.Trials[battle.Trial-1].Sign).Bare
	}
	if !bare {
		// 行囊里有丹符会自动带上，没带就是行囊空了；丹房(宗门丹房或洞府丹室)能炼才提。
		if len(battle.Carried) == 0 && h.alchemyLevel(s) >= PotionRecipes[0].Alchemy {
			open["potions"] = 0
		}
		if len(battle.Team) == 0 {
			ready := 0
			for _, d := range s.Disciples {
				if d.home(now) {
					ready++
				}
			}
			if ready > 0 {
				open["team"] = min(ready, CompanionMax)
			}
		}
		if battle.Beast == 0 {
			for _, b := range h.Beasts {
				if b.Hatch == 0 {
					open["beast"] = 0
					break
				}
			}
		}
	}

	evs := []Ev{{K: "hero_review", S: cause}}
	for _, key := range tipOrders[cause] {
		n, ok := open[key]
		if !ok {
			continue
		}
		evs = append(evs, Ev{K: "hero_tip", S: key, N: n})
		if len(evs) > reviewTips {
			break
		}
	}
	return evs
}
