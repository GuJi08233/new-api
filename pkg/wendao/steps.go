package wendao

// 问心阶是历练通关(重铸封印)以后的终局挑战：问道宗山门后的五十级石阶，每一级守着一个心魔，化作宗主一路打过的敌人。
// 只能挑战上一次登到的下一级，赢了才能往上走；每一级第一次登上给修为与宗门资源，强敌那一级另给破境丹，首领那一级
// 另给洗髓丹。输了没有损失(随行弟子照样受伤)，可以一直再来。问心阶不发游戏币。

const (
	// MaxSteps 是问心阶的级数。
	MaxSteps = 50
	// StepsUnlock 是问心阶开放要走过的节点：第五章的尾声。
	StepsUnlock = "c5_epilogue"
)

// 心魔化作的敌人。普通的一级是两只，按 stepFoes 依次轮换；第 5、15、25…级是强敌，第 10、20…级是首领，都按顺序各出现一次。
var (
	stepFoes = [][]string{
		{"treant", "viper"}, {"nether_hound", "nether_hound"}, {"fox_guard", "hungry_ghost"}, {"abyss_guard", "void_walker"},
		{"soul_eater", "viper"}, {"crystal_golem"}, {"treant", "fox_guard"}, {"hungry_ghost", "soul_eater"},
	}
	stepElites = [][]string{{"lich"}, {"bear"}, {"spider"}, {"judge"}, {"soul_lord"}}
	stepBosses = [][]string{{"flame_lord"}, {"abyss_drake"}, {"fox_emperor"}, {"taixu"}, {"patriarch"}}
)

// stepScale 是心魔的生命与伤害(百分比)：第 s 级是 基数 + 每级的增量 × s。第一级与第五章的遭遇差不多，按贪心机器人模拟
// (化神、通关第五章时的牌组，每级最多试十次)，三种流派的中位数能登到第 34~49 级；最后一级的首领一次只有一到三成胜算。
var stepScale = map[string][4]int{
	NodeCombat: {236, 159, 14, 5},
	NodeElite:  {256, 169, 14, 5},
	NodeBoss:   {217, 129, 7, 3},
}

// stepRewardKind 是各种一级的资源倍数与声望。
var stepRewardKind = map[string]struct {
	Mult float64
	Rep  int
}{
	NodeCombat: {1, 0},
	NodeElite:  {2, 40},
	NodeBoss:   {3, 100},
}

// Step 是问心阶的一级：心魔的样子与强度，第一次登上的修为、资源与丹药。
type Step struct {
	Step    int      `json:"step"`
	Kind    string   `json:"kind"`
	Enemies []string `json:"enemies"`
	HP      int      `json:"hp"`
	Dmg     int      `json:"dmg"`
	Exp     int      `json:"exp"`
	Reward  Cost     `json:"reward"`
	Pill    string   `json:"pill,omitempty"`
}

// StepOf 返回第 step 级(1 到 MaxSteps)。修为是化神那一章同种节点的一半。
func StepOf(step int) Step {
	kind, enemies := NodeCombat, stepFoes[(step-1)%len(stepFoes)]
	pill := ""
	switch {
	case step%10 == 0:
		kind, enemies, pill = NodeBoss, stepBosses[step/10-1], PillMarrow
	case step%5 == 0:
		kind, enemies, pill = NodeElite, stepElites[step/10], PillBreak
	}
	scale := stepScale[kind]
	reward := stepRewardKind[kind]
	base := Cost{Stone: 2000 + 100*step, Herb: 600 + 40*step, Ore: 600 + 40*step}.scale(reward.Mult)
	base.Rep = reward.Rep
	return Step{
		Step: step, Kind: kind, Enemies: enemies, HP: scale[0] + scale[2]*step, Dmg: scale[1] + scale[3]*step,
		Exp: int(float64(nodeExp[kind]) * realmExp[MaxRealm] / 2), Reward: base, Pill: pill,
	}
}

// Steps 是问心阶的全部级数，页面按它画出石阶。
func Steps() []Step {
	steps := make([]Step, 0, MaxSteps)
	for s := 1; s <= MaxSteps; s++ {
		steps = append(steps, StepOf(s))
	}
	return steps
}

// StepsOpen 表示问心阶已经开放：第五章的尾声已经走过。
func (h *Hero) StepsOpen() bool { return h.cleared(StepsUnlock) > 0 }
