package wendao

import (
	"math"
	"slices"

	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// 炼器：在洞府的器室里祭炼主角的法宝。法宝本身的效果不变，祭炼的每一级按法宝的路数另给一点加成，只有戴在身上的法宝
// 才算：
//   - 命(生命)：每级最大生命 +RefineHP。
//   - 御(护体)：每级开战护体 +RefineBlock。
//   - 锋(力量)：祭炼到 5 级、10 级各多 1 点开战力量；所有法宝加起来最多 RefineStrengthMax 点。
// 法宝最多祭炼到器室的等级(最高 MaxRefine 级)。越往上越难成，失败只损失材料、不掉级，下一次成功率多 refineBless 个
// 百分点(祝福)，成了以后祝福清零。祭炼的加成只在历练(含问心阶)里生效，带不进九幽秘境。

const (
	// MaxRefine 是法宝最高祭炼到几级。
	MaxRefine = 10
	// RefineHP、RefineBlock 是命、御两路法宝每级给的最大生命与开战护体。
	RefineHP    = 3
	RefineBlock = 2
	// RefineStrengthMax 是锋路法宝加起来最多给几点力量；refineEdgeStep 是锋路法宝每几级多 1 点力量。
	RefineStrengthMax = 3
	refineEdgeStep    = 5
	// refineBless 是祭炼失败一次，这件法宝下一次的成功率多几个百分点。
	refineBless = 10
	// refineRareFrom、refineLotusFrom 是从几级起每次祭炼要一份九叶灵芝、一份七彩莲。
	refineRareFrom  = 6
	refineLotusFrom = 9
)

// 法宝的祭炼路数。
const (
	RefineVital = "vital"
	RefineGuard = "guard"
	RefineEdge  = "edge"
)

// relicRefineKinds 是每件法宝的祭炼路数：攻伐类的走锋，护身类的走御，其余走命。没列出来的法宝都走命。
var relicRefineKinds = map[string]string{
	dungeon.RelicWarriorHeart: RefineEdge,
	dungeon.RelicEmberStaff:   RefineEdge,
	dungeon.RelicQuiver:       RefineEdge,
	dungeon.RelicWhetstone:    RefineEdge,
	dungeon.RelicHawkEye:      RefineEdge,
	dungeon.RelicFlame:        RefineEdge,
	dungeon.RelicCrown:        RefineEdge,
	dungeon.RelicIronPlate:    RefineGuard,
	dungeon.RelicSpikedMail:   RefineGuard,
	dungeon.RelicRune:         RefineGuard,
	dungeon.RelicCrystal:      RefineGuard,
	dungeon.RelicMetronome:    RefineGuard,
}

// RefineKind 返回法宝的祭炼路数。
func RefineKind(relic string) string {
	if kind, ok := relicRefineKinds[relic]; ok {
		return kind
	}
	return RefineVital
}

// refineChance 是祭炼到第 L 级的基础成功率(%)。
var refineChance = [...]int{0, 100, 100, 100, 90, 80, 70, 60, 50, 40, 30}

// RefineStep 是把法宝祭炼到某一级：花多少资源、要不要一份灵药、基础成功率。
type RefineStep struct {
	Level  int    `json:"level"`
	Cost   Cost   `json:"cost"`
	Rare   string `json:"rare,omitempty"`
	Chance int    `json:"chance"`
}

// RefineStepOf 返回祭炼到 level 级的花费与成功率：玄铁与灵石每级涨 55%，6 级起要一份九叶灵芝，9 级起换成七彩莲。
func RefineStepOf(level int) RefineStep {
	f := math.Pow(1.55, float64(level-1))
	step := RefineStep{Level: level, Cost: Cost{Stone: 100, Ore: 50}.scale(f), Chance: refineChance[level]}
	switch {
	case level >= refineLotusFrom:
		step.Rare = RareLotus
	case level >= refineRareFrom:
		step.Rare = RareLingzhi
	}
	return step
}

// RefineBonus 是戴着的法宝祭炼给的加成。
type RefineBonus struct {
	HP       int `json:"hp"`
	Block    int `json:"block"`
	Strength int `json:"strength"`
}

// refineBonus 按戴着的法宝算祭炼的加成，力量不超过 RefineStrengthMax。
func (h *Hero) refineBonus() RefineBonus {
	var bonus RefineBonus
	for _, relic := range h.Worn {
		level := h.Refine[relic]
		switch RefineKind(relic) {
		case RefineEdge:
			bonus.Strength += level / refineEdgeStep
		case RefineGuard:
			bonus.Block += RefineBlock * level
		default:
			bonus.HP += RefineHP * level
		}
	}
	bonus.Strength = min(bonus.Strength, RefineStrengthMax)
	return bonus
}

// refine 在器室祭炼一件自己有的法宝：先扣材料，再按成功率(加上祝福)判定。
func (h *Hero) refine(s *Sect, relic string) ([]Ev, error) {
	if !slices.Contains(h.Relics, relic) {
		return nil, ErrAction
	}
	next := h.Refine[relic] + 1
	if next > MaxRefine || next > h.Cave[RoomForge] {
		return nil, ErrUnavailable
	}
	step := RefineStepOf(next)
	if step.Rare != "" && s.Rare[step.Rare] < 1 {
		return nil, ErrCost
	}
	if err := s.pay(step.Cost); err != nil {
		return nil, err
	}
	evs := spentEvs(step.Cost)
	if step.Rare != "" {
		s.Rare[step.Rare]--
		evs = append(evs, Ev{K: "rare", S: step.Rare, N: -1})
	}
	if !s.Rng.Chance(min(step.Chance+h.Bless[relic], 100)) {
		h.Bless[relic] += refineBless
		return append(evs, Ev{K: "hero_refine_fail", S: relic, N: h.Bless[relic]}), nil
	}
	h.Refine[relic] = next
	delete(h.Bless, relic)
	return append(evs, Ev{K: "hero_refine", S: relic, N: next}), nil
}

// RefineRules 是炼器的规则，页面按它显示花费、成功率与加成。
type RefineRules struct {
	Steps       []RefineStep      `json:"steps"`
	Kinds       map[string]string `json:"kinds"`
	HP          int               `json:"hp"`
	Block       int               `json:"block"`
	EdgeStep    int               `json:"edge_step"`
	StrengthMax int               `json:"strength_max"`
	Bless       int               `json:"bless"`
}

// RefineRulesOf 返回炼器的规则：Steps[L-1] 是祭炼到 L 级，Kinds 是每件法宝的路数(没列出来的走命)。
func RefineRulesOf() RefineRules {
	rules := RefineRules{Kinds: relicRefineKinds, HP: RefineHP, Block: RefineBlock, EdgeStep: refineEdgeStep,
		StrengthMax: RefineStrengthMax, Bless: refineBless}
	for level := 1; level <= MaxRefine; level++ {
		rules.Steps = append(rules.Steps, RefineStepOf(level))
	}
	return rules
}
