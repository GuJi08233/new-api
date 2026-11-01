// Package dungeon 是深渊远征(卡牌肉鸽)的规则引擎：一轮远征分三幕，每幕在路线图上走八层再挑战首领，回合制卡牌战斗。
// 整轮的状态(含随机数状态)由服务端保存，玩家每一步操作都在这里校验；局外成长(魂晶、职业、天赋、深渊等级)见 meta.go。
package dungeon

// 卡牌类型。
const (
	TypeAttack = "attack"
	TypeSkill  = "skill"
	TypePower  = "power"
	TypeCurse  = "curse"
)

// 卡牌的目标：不需要目标、选一个敌人、全体敌人(不需要选择)。
const (
	TargetNone  = 0
	TargetEnemy = 1
	TargetAll   = 2
)

// 稀有度：初始牌、普通、罕见、稀有；Special 是飞刀、诅咒这类不会出现在奖励里的牌。
const (
	RarityStarter = iota
	RarityCommon
	RarityUncommon
	RarityRare
	RaritySpecial
)

// 职业。中立牌的 Class 为 ClassAny。
const (
	ClassWarrior = "warrior"
	ClassMage    = "mage"
	ClassRanger  = "ranger"
	ClassAny     = "any"
)

// Classes 是所有职业，按解锁顺序排列。
var Classes = []string{ClassWarrior, ClassMage, ClassRanger}

// 效果种类。数值 N 与次数 X 都按 [未升级, 升级] 给出。
const (
	FxDamage      = "dmg"        // 对目标造成 N 点伤害 X 次
	FxDamageAll   = "dmg_all"    // 对所有敌人造成 N 点伤害 X 次
	FxDamageRand  = "dmg_rand"   // 随机对一个敌人造成 N 点伤害，重复 X 次
	FxBlock       = "block"      // 获得 N 点护甲
	FxDraw        = "draw"       // 抽 N 张牌
	FxEnergy      = "energy"     // 获得 N 点能量
	FxApply       = "apply"      // 给予目标 N 层状态 S
	FxApplyAll    = "apply_all"  // 给予所有敌人 N 层状态 S
	FxApplySelf   = "apply_self" // 自己获得 N 层状态 S
	FxPower       = "power"      // 能力：自己获得 N 层持续整场战斗的状态 S
	FxAddCard     = "add"        // 把 N 张 S 加入手牌
	FxHeal        = "heal"       // 回复 N 点生命
	FxLoseHP      = "lose_hp"    // 失去 N 点生命
	FxDamageBlock = "dmg_block"  // 造成等同于自己护甲的伤害
	FxDoubleBurn  = "double_burn"
	FxBurst       = "burst"        // 造成目标灼烧层数 N 倍的伤害，然后清除灼烧
	FxPerCard     = "dmg_per_card" // 本回合每打出过一张牌(不含这张)，造成 N 点伤害
)

// 状态。力量、狂怒、铸甲、荆棘、闪避、余烬、刀舞、锋锐、铁壁、涌泉不会自然衰减；虚弱、易伤在拥有者的回合结束时减一层；
// 灼烧在拥有者的回合开始时造成等同层数的伤害(无视护甲)，然后减一层。
const (
	StStrength   = "str"        // 攻击伤害 +N
	StWeak       = "weak"       // 攻击伤害 -25%
	StVuln       = "vuln"       // 受到的攻击伤害 +50%
	StBurn       = "burn"       // 回合开始受到 N 点伤害
	StThorns     = "thorns"     // 被攻击时反弹 N 点伤害
	StDodge      = "dodge"      // 完全躲开接下来 N 次攻击
	StRage       = "rage"       // 回合结束获得 N 点力量
	StPlate      = "plate"      // 回合结束获得 N 点护甲
	StEmbers     = "embers"     // 回合结束给予所有敌人 N 层灼烧
	StBlades     = "blades"     // 回合开始把 N 张飞刀加入手牌
	StKeen       = "keen"       // 飞刀伤害 +N
	StFortify    = "fortify"    // 护甲不会在回合开始时清除
	StWellspring = "wellspring" // 回合开始额外获得 N 点能量
)

// Fx 是一条效果，Card 与视图里都用它。
type Fx struct {
	Kind   string `json:"k"`
	N      int    `json:"n,omitempty"`
	X      int    `json:"x,omitempty"`
	Status string `json:"s,omitempty"`
}

type fxDef struct {
	kind   string
	n      [2]int
	x      [2]int
	status string
}

// CardDef 是一种卡牌。
type CardDef struct {
	Id      string
	Class   string
	Rarity  int
	Type    string
	Cost    [2]int
	Target  int
	Exhaust [2]bool
	fx      []fxDef
}

// Effects 返回卡牌(未升级或升级后)的效果。
func (d *CardDef) Effects(up bool) []Fx {
	i := 0
	if up {
		i = 1
	}
	out := make([]Fx, 0, len(d.fx))
	for _, f := range d.fx {
		out = append(out, Fx{Kind: f.kind, N: f.n[i], X: f.x[i], Status: f.status})
	}
	return out
}

func fx(kind string, n0, n1 int) fxDef { return fxDef{kind: kind, n: [2]int{n0, n1}} }
func fxTimes(kind string, n0, n1, x0, x1 int) fxDef {
	return fxDef{kind: kind, n: [2]int{n0, n1}, x: [2]int{x0, x1}}
}
func fxStatus(kind, status string, n0, n1 int) fxDef {
	return fxDef{kind: kind, n: [2]int{n0, n1}, status: status}
}

func card(id, class string, rarity int, typ string, cost, costUp int, target int, effects ...fxDef) *CardDef {
	return &CardDef{Id: id, Class: class, Rarity: rarity, Type: typ, Cost: [2]int{cost, costUp}, Target: target, fx: effects}
}

func exhaust(d *CardDef, base, up bool) *CardDef {
	d.Exhaust = [2]bool{base, up}
	return d
}

// AllyCards 是五行灵根(金、木、水、火、土)的援护牌，下标与灵根编号一致。
var AllyCards = [...]string{"ally_metal", "ally_wood", "ally_water", "ally_fire", "ally_earth"}

// KnifeCard 与诅咒牌在战斗中生成或由事件加入，不会出现在奖励里。
const (
	KnifeCard = "knife"
	DoubtCard = "doubt"
	WoundCard = "wound"
)

var cardList = []*CardDef{
	// 剑士
	card("w_strike", ClassWarrior, RarityStarter, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 6, 9)),
	card("w_defend", ClassWarrior, RarityStarter, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 5, 8)),
	card("w_heavy", ClassWarrior, RarityStarter, TypeAttack, 2, 2, TargetEnemy, fx(FxDamage, 11, 15), fxStatus(FxApply, StVuln, 2, 3)),
	card("w_sweep", ClassWarrior, RarityCommon, TypeAttack, 1, 1, TargetAll, fx(FxDamageAll, 8, 11)),
	card("w_bash", ClassWarrior, RarityCommon, TypeAttack, 1, 0, TargetEnemy, fx(FxDamageBlock, 0, 0)),
	card("w_warcry", ClassWarrior, RarityCommon, TypeSkill, 0, 0, TargetNone, fx(FxBlock, 3, 5), fx(FxDraw, 1, 1)),
	card("w_twin", ClassWarrior, RarityCommon, TypeAttack, 1, 1, TargetEnemy, fxTimes(FxDamage, 5, 7, 2, 2)),
	card("w_stance", ClassWarrior, RarityCommon, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 9, 12)),
	card("w_rend", ClassWarrior, RarityCommon, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 6, 8), fxStatus(FxApply, StVuln, 2, 3)),
	card("w_fury", ClassWarrior, RarityUncommon, TypePower, 1, 1, TargetNone, fxStatus(FxPower, StStrength, 2, 3)),
	card("w_ironbody", ClassWarrior, RarityUncommon, TypePower, 2, 2, TargetNone, fxStatus(FxPower, StPlate, 4, 6)),
	card("w_riposte", ClassWarrior, RarityUncommon, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 5, 8), fxStatus(FxApplySelf, StThorns, 3, 4)),
	card("w_bloodblade", ClassWarrior, RarityUncommon, TypeSkill, 0, 0, TargetNone, fx(FxLoseHP, 3, 2), fx(FxEnergy, 2, 2)),
	card("w_quake", ClassWarrior, RarityUncommon, TypeAttack, 2, 2, TargetAll, fx(FxDamageAll, 12, 16), fxStatus(FxApplyAll, StWeak, 1, 2)),
	card("w_execute", ClassWarrior, RarityUncommon, TypeAttack, 2, 2, TargetEnemy, fx(FxDamage, 18, 24)),
	card("w_unyielding", ClassWarrior, RarityRare, TypePower, 2, 1, TargetNone, fxStatus(FxPower, StFortify, 1, 1)),
	card("w_giant", ClassWarrior, RarityRare, TypePower, 3, 2, TargetNone, fxStatus(FxPower, StRage, 2, 2)),
	card("w_whirlwind", ClassWarrior, RarityRare, TypeAttack, 3, 3, TargetAll, fxTimes(FxDamageAll, 6, 8, 3, 3)),

	// 术士
	card("m_bolt", ClassMage, RarityStarter, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 6, 9)),
	card("m_ward", ClassMage, RarityStarter, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 5, 8)),
	card("m_ignite", ClassMage, RarityStarter, TypeSkill, 1, 1, TargetEnemy, fxStatus(FxApply, StBurn, 5, 8)),
	exhaust(card("m_meditate", ClassMage, RarityStarter, TypeSkill, 0, 0, TargetNone, fx(FxEnergy, 1, 2)), true, true),
	card("m_fireball", ClassMage, RarityCommon, TypeAttack, 2, 2, TargetEnemy, fx(FxDamage, 14, 18), fxStatus(FxApply, StBurn, 3, 4)),
	card("m_flamewave", ClassMage, RarityCommon, TypeSkill, 1, 1, TargetAll, fxStatus(FxApplyAll, StBurn, 3, 5)),
	card("m_iceshard", ClassMage, RarityCommon, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 7, 10), fxStatus(FxApply, StWeak, 1, 2)),
	card("m_arcane", ClassMage, RarityCommon, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 7, 10), fx(FxDraw, 1, 1)),
	card("m_kindle", ClassMage, RarityCommon, TypeSkill, 0, 0, TargetEnemy, fxStatus(FxApply, StBurn, 3, 5)),
	card("m_surge", ClassMage, RarityCommon, TypeSkill, 1, 1, TargetNone, fx(FxDraw, 2, 3)),
	card("m_detonate", ClassMage, RarityUncommon, TypeSkill, 1, 0, TargetEnemy, fx(FxDoubleBurn, 0, 0)),
	card("m_emberheart", ClassMage, RarityUncommon, TypePower, 1, 1, TargetNone, fxStatus(FxPower, StEmbers, 2, 3)),
	card("m_firewall", ClassMage, RarityUncommon, TypeSkill, 2, 2, TargetAll, fx(FxBlock, 12, 15), fxStatus(FxApplyAll, StBurn, 2, 3)),
	card("m_meteor", ClassMage, RarityUncommon, TypeAttack, 3, 3, TargetAll, fx(FxDamageAll, 20, 26)),
	card("m_frostnova", ClassMage, RarityUncommon, TypeSkill, 1, 1, TargetAll, fxStatus(FxApplyAll, StWeak, 2, 3), fx(FxBlock, 4, 6)),
	exhaust(card("m_siphon", ClassMage, RarityUncommon, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 8, 11), fx(FxHeal, 3, 4)), true, true),
	card("m_wellspring", ClassMage, RarityRare, TypePower, 2, 1, TargetNone, fxStatus(FxPower, StWellspring, 1, 1)),
	card("m_consume", ClassMage, RarityRare, TypeAttack, 2, 2, TargetEnemy, fx(FxBurst, 2, 3)),
	exhaust(card("m_timewarp", ClassMage, RarityRare, TypeSkill, 0, 0, TargetNone, fx(FxDraw, 3, 4), fx(FxEnergy, 1, 1)), true, true),

	// 游侠
	card("r_shoot", ClassRanger, RarityStarter, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 6, 9)),
	card("r_evade", ClassRanger, RarityStarter, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 5, 8)),
	card("r_knives", ClassRanger, RarityStarter, TypeSkill, 0, 0, TargetNone, fxStatus(FxAddCard, KnifeCard, 2, 3)),
	card("r_snare", ClassRanger, RarityStarter, TypeSkill, 1, 1, TargetEnemy, fxStatus(FxApply, StWeak, 1, 2), fxStatus(FxApply, StVuln, 1, 2)),
	card("r_volley", ClassRanger, RarityCommon, TypeAttack, 1, 1, TargetEnemy, fxTimes(FxDamage, 3, 4, 3, 3)),
	card("r_quick", ClassRanger, RarityCommon, TypeAttack, 0, 0, TargetEnemy, fx(FxDamage, 4, 6), fx(FxDraw, 1, 1)),
	card("r_roll", ClassRanger, RarityCommon, TypeSkill, 1, 1, TargetNone, fxStatus(FxApplySelf, StDodge, 1, 2), fx(FxDraw, 1, 1)),
	card("r_bladestorm", ClassRanger, RarityCommon, TypeSkill, 1, 1, TargetNone, fxStatus(FxAddCard, KnifeCard, 3, 4)),
	card("r_aimed", ClassRanger, RarityCommon, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 9, 12), fxStatus(FxApply, StVuln, 1, 2)),
	card("r_scatter", ClassRanger, RarityCommon, TypeAttack, 1, 1, TargetAll, fxTimes(FxDamageRand, 4, 5, 3, 3)),
	card("r_bladedance", ClassRanger, RarityUncommon, TypePower, 1, 1, TargetNone, fxStatus(FxPower, StBlades, 1, 2)),
	card("r_rhythm", ClassRanger, RarityUncommon, TypeAttack, 1, 1, TargetEnemy, fx(FxPerCard, 3, 4)),
	card("r_smoke", ClassRanger, RarityUncommon, TypeSkill, 1, 1, TargetAll, fxStatus(FxApplyAll, StWeak, 2, 3), fx(FxBlock, 4, 6)),
	exhaust(card("r_prepare", ClassRanger, RarityUncommon, TypeSkill, 0, 0, TargetNone, fx(FxDraw, 2, 3)), true, true),
	card("r_pierce", ClassRanger, RarityUncommon, TypeAttack, 2, 2, TargetAll, fx(FxDamageAll, 12, 16)),
	card("r_mark", ClassRanger, RarityUncommon, TypeSkill, 0, 0, TargetEnemy, fxStatus(FxApply, StVuln, 2, 3)),
	card("r_keen", ClassRanger, RarityRare, TypePower, 1, 1, TargetNone, fxStatus(FxPower, StKeen, 3, 5)),
	card("r_phantom", ClassRanger, RarityRare, TypeSkill, 2, 2, TargetNone, fxStatus(FxApplySelf, StDodge, 2, 3), fx(FxBlock, 6, 10)),
	card("r_arrowrain", ClassRanger, RarityRare, TypeAttack, 2, 2, TargetAll, fxTimes(FxDamageRand, 3, 3, 6, 8)),

	// 中立
	exhaust(card("n_firstaid", ClassAny, RarityUncommon, TypeSkill, 1, 1, TargetNone, fx(FxHeal, 5, 8)), true, true),
	card("n_allout", ClassAny, RarityCommon, TypeAttack, 2, 2, TargetEnemy, fx(FxDamage, 15, 20)),
	card("n_brace", ClassAny, RarityCommon, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 6, 9), fx(FxDraw, 1, 1)),
	exhaust(card("n_inspire", ClassAny, RarityUncommon, TypeSkill, 0, 0, TargetNone, fx(FxEnergy, 1, 2)), true, true),
	card("n_tactics", ClassAny, RarityUncommon, TypeSkill, 1, 1, TargetNone, fx(FxDraw, 2, 3)),
	exhaust(card("n_flash", ClassAny, RarityCommon, TypeSkill, 0, 0, TargetAll, fxStatus(FxApplyAll, StWeak, 1, 2)), true, true),

	// 援护牌：问道历练里随行弟子带来的一张牌，按弟子的灵根(金木水火土)；弟子到了筑基就是升级后的样子。
	card(AllyCards[0], ClassAny, RaritySpecial, TypeAttack, 1, 1, TargetEnemy, fx(FxDamage, 9, 13)),
	exhaust(card(AllyCards[1], ClassAny, RaritySpecial, TypeSkill, 1, 1, TargetNone, fx(FxHeal, 6, 9)), true, true),
	card(AllyCards[2], ClassAny, RaritySpecial, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 7, 10), fx(FxDraw, 1, 1)),
	card(AllyCards[3], ClassAny, RaritySpecial, TypeSkill, 1, 1, TargetAll, fxStatus(FxApplyAll, StBurn, 3, 5)),
	card(AllyCards[4], ClassAny, RaritySpecial, TypeSkill, 1, 1, TargetNone, fx(FxBlock, 10, 14)),

	// 生成牌与诅咒
	exhaust(card(KnifeCard, ClassAny, RaritySpecial, TypeAttack, 0, 0, TargetEnemy, fx(FxDamage, 4, 6)), true, true),
	card(DoubtCard, ClassAny, RaritySpecial, TypeCurse, -1, -1, TargetNone),
	card(WoundCard, ClassAny, RaritySpecial, TypeCurse, -1, -1, TargetNone),
}

var cardDefs = func() map[string]*CardDef {
	defs := make(map[string]*CardDef, len(cardList))
	for _, d := range cardList {
		defs[d.Id] = d
	}
	return defs
}()

// Card 是牌组里的一张牌。
type Card struct {
	Id string `json:"id"`
	Up bool   `json:"up,omitempty"`
}

func (c Card) def() *CardDef { return cardDefs[c.Id] }

// starterDecks 是各职业的初始牌组。
var starterDecks = map[string][]string{
	ClassWarrior: {"w_strike", "w_strike", "w_strike", "w_strike", "w_strike", "w_defend", "w_defend", "w_defend", "w_defend", "w_heavy"},
	ClassMage:    {"m_bolt", "m_bolt", "m_bolt", "m_bolt", "m_ward", "m_ward", "m_ward", "m_ward", "m_ignite", "m_meditate"},
	ClassRanger:  {"r_shoot", "r_shoot", "r_shoot", "r_shoot", "r_evade", "r_evade", "r_evade", "r_evade", "r_knives", "r_snare"},
}

// classStats 是各职业的初始最大生命与专属遗物。
var classStats = map[string]struct {
	HP    int
	Relic string
}{
	ClassWarrior: {HP: 84, Relic: "warrior_heart"},
	ClassMage:    {HP: 70, Relic: "ember_staff"},
	ClassRanger:  {HP: 72, Relic: "quiver"},
}

// 遗物。
const (
	RelicWarriorHeart = "warrior_heart" // 战斗开始获得 1 点力量，战斗结束回复 6 点生命
	RelicEmberStaff   = "ember_staff"   // 战斗开始给予所有敌人 2 层灼烧
	RelicQuiver       = "quiver"        // 战斗开始把 2 张飞刀加入手牌
	RelicIronPlate    = "iron_plate"    // 战斗开始获得 8 点护甲
	RelicWhetstone    = "whetstone"     // 战斗开始获得 1 点力量
	RelicLuckyCoin    = "lucky_coin"    // 战斗获得的金币 +25%
	RelicHerbPouch    = "herb_pouch"    // 战斗结束回复 3 点生命
	RelicSpikedMail   = "spiked_mail"   // 战斗开始获得 3 层荆棘
	RelicPotionBelt   = "potion_belt"   // 药水栏 +1
	RelicJade         = "jade"          // 拾取时最大生命 +10
	RelicScroll       = "scroll"        // 每场战斗第一回合多抽 2 张牌
	RelicCrystal      = "crystal"       // 每场战斗第一回合额外获得 1 点能量
	RelicHawkEye      = "hawk_eye"      // 战斗开始给予所有敌人 1 层易伤
	RelicMetronome    = "metronome"     // 每回合打出第 3 张牌时抽 1 张牌
	RelicFang         = "vampire_fang"  // 每击败一个敌人回复 4 点生命
	RelicFlame        = "eternal_flame" // 回合开始给予所有敌人 1 层灼烧
	RelicRune         = "guard_rune"    // 回合结束时如果没有护甲，获得 6 点护甲
	RelicPhoenix      = "phoenix"       // 第一次死亡时以 30% 最大生命复活
	RelicTome         = "sage_tome"     // 每回合多抽 1 张牌
	RelicCrown        = "crown"         // 每回合额外获得 1 点能量，但卡牌奖励只有 2 张可选
)

// relicTiers 是会在奖励、宝箱与商店里出现的遗物，按稀有度分组；职业遗物只在开局获得。
var relicTiers = [][]string{
	RarityCommon:   {RelicIronPlate, RelicWhetstone, RelicLuckyCoin, RelicHerbPouch, RelicSpikedMail, RelicPotionBelt, RelicJade, RelicScroll},
	RarityUncommon: {RelicCrystal, RelicHawkEye, RelicMetronome, RelicFang, RelicFlame, RelicRune},
	RarityRare:     {RelicPhoenix, RelicTome, RelicCrown},
}

func relicRarity(id string) int {
	for rarity, ids := range relicTiers {
		for _, relic := range ids {
			if relic == id {
				return rarity
			}
		}
	}
	return RarityStarter
}

// 药水。
const (
	PotionHeal     = "heal"     // 回复 25% 最大生命，战斗外也能用
	PotionFire     = "fire"     // 对一个敌人造成 20 点伤害
	PotionIron     = "iron"     // 获得 12 点护甲
	PotionStrength = "strength" // 获得 2 点力量
	PotionSwift    = "swift"    // 抽 3 张牌
	PotionEnergy   = "energy"   // 获得 2 点能量
	PotionWeakness = "weakness" // 给予所有敌人 3 层虚弱
)

var potionList = []string{PotionHeal, PotionFire, PotionIron, PotionStrength, PotionSwift, PotionEnergy, PotionWeakness}

// Move 是敌人的一种行动。Dmg 是每次攻击的基础伤害(未计力量、虚弱与深渊加成)，Times 为 0 表示一次。
type Move struct {
	Id        string
	Dmg       int
	Times     int
	Block     int
	Heal      int
	Wounds    int
	Lifesteal bool
	Self      map[string]int
	Foe       map[string]int
}

// EnemyDef 是一种敌人。Cycle 不为空时按顺序循环出招，否则按 Weights 随机，同一招不会连用三次。
type EnemyDef struct {
	Id      string
	HP      [2]int
	Moves   []Move
	Cycle   []int
	Weights []int
	Start   map[string]int
}

var enemyList = []*EnemyDef{
	// 第一幕
	{Id: "slime", HP: [2]int{28, 32}, Moves: []Move{{Id: "slam", Dmg: 8}, {Id: "ooze", Dmg: 5, Foe: map[string]int{StWeak: 1}}}, Weights: []int{1, 1}},
	{Id: "goblin", HP: [2]int{18, 22}, Moves: []Move{{Id: "stab", Dmg: 6}, {Id: "flurry", Dmg: 3, Times: 2}}, Weights: []int{1, 1}},
	{Id: "skeleton", HP: [2]int{30, 34}, Moves: []Move{{Id: "cleave", Dmg: 9}, {Id: "guard", Dmg: 4, Block: 8}}, Weights: []int{1, 1}},
	{Id: "moth", HP: [2]int{13, 17}, Moves: []Move{{Id: "dust", Dmg: 3, Foe: map[string]int{StVuln: 1}}, {Id: "flap", Dmg: 5}}, Weights: []int{1, 1}},
	{Id: "goblin_chief", HP: [2]int{62, 66}, Moves: []Move{{Id: "roar", Dmg: 6, Foe: map[string]int{StVuln: 2}}, {Id: "smash", Dmg: 14}, {Id: "rally", Block: 6, Self: map[string]int{StStrength: 2}}}, Cycle: []int{0, 1, 2, 1}},
	{Id: "gargoyle", HP: [2]int{50, 54}, Start: map[string]int{StPlate: 4}, Moves: []Move{{Id: "claw", Dmg: 10}, {Id: "rake", Dmg: 4, Times: 2}, {Id: "harden", Self: map[string]int{StPlate: 3}}}, Weights: []int{2, 2, 1}},
	{Id: "slime_king", HP: [2]int{140, 140}, Moves: []Move{{Id: "spew", Dmg: 8, Wounds: 2}, {Id: "harden", Block: 15, Self: map[string]int{StStrength: 2}}, {Id: "crush", Dmg: 22}}, Cycle: []int{0, 1, 2}},

	// 第二幕
	{Id: "werewolf", HP: [2]int{40, 44}, Moves: []Move{{Id: "bite", Dmg: 6, Times: 2}, {Id: "howl", Self: map[string]int{StStrength: 2}}}, Weights: []int{7, 3}},
	{Id: "shade_mage", HP: [2]int{34, 38}, Moves: []Move{{Id: "bolt", Dmg: 10}, {Id: "curse", Foe: map[string]int{StWeak: 2, StVuln: 1}}}, Weights: []int{3, 2}},
	{Id: "lava_elemental", HP: [2]int{42, 46}, Moves: []Move{{Id: "fist", Dmg: 10}, {Id: "erupt", Dmg: 5, Foe: map[string]int{StBurn: 3}}}, Weights: []int{1, 1}},
	{Id: "beetle", HP: [2]int{30, 34}, Start: map[string]int{StThorns: 2}, Moves: []Move{{Id: "ram", Dmg: 9}, {Id: "shell", Block: 10}}, Weights: []int{3, 2}},
	{Id: "troll", HP: [2]int{100, 106}, Moves: []Move{{Id: "slam", Dmg: 16}, {Id: "roar", Self: map[string]int{StStrength: 2}}, {Id: "regen", Heal: 10, Block: 8}}, Cycle: []int{0, 1, 0, 2}},
	{Id: "death_knight", HP: [2]int{86, 92}, Moves: []Move{{Id: "slash", Dmg: 13, Foe: map[string]int{StWeak: 1}}, {Id: "double", Dmg: 7, Times: 2}, {Id: "shield", Block: 12, Self: map[string]int{StStrength: 2}}}, Weights: []int{2, 2, 1}},
	{Id: "flame_lord", HP: [2]int{180, 180}, Moves: []Move{{Id: "breath", Dmg: 8, Foe: map[string]int{StBurn: 4}}, {Id: "fist", Dmg: 22}, {Id: "aegis", Block: 16, Self: map[string]int{StThorns: 2}}}, Cycle: []int{0, 1, 2}},

	// 第三幕
	{Id: "abyss_guard", HP: [2]int{56, 60}, Moves: []Move{{Id: "halberd", Dmg: 15}, {Id: "stance", Block: 15, Self: map[string]int{StStrength: 2}}}, Weights: []int{3, 2}},
	{Id: "void_walker", HP: [2]int{48, 52}, Moves: []Move{{Id: "rend", Dmg: 5, Times: 3}, {Id: "phase", Self: map[string]int{StDodge: 1}, Foe: map[string]int{StWeak: 2}}}, Weights: []int{3, 2}},
	{Id: "soul_eater", HP: [2]int{54, 58}, Moves: []Move{{Id: "drain", Dmg: 13, Lifesteal: true}, {Id: "dread", Foe: map[string]int{StVuln: 2, StWeak: 1}}}, Weights: []int{3, 2}},
	{Id: "crystal_golem", HP: [2]int{64, 68}, Start: map[string]int{StPlate: 5}, Moves: []Move{{Id: "smash", Dmg: 18}, {Id: "shards", Dmg: 6, Times: 2}}, Weights: []int{2, 3}},
	{Id: "abyss_drake", HP: [2]int{165, 175}, Moves: []Move{{Id: "claws", Dmg: 9, Times: 2}, {Id: "breath", Dmg: 22}, {Id: "roar", Block: 15, Self: map[string]int{StStrength: 3}}}, Cycle: []int{0, 1, 2}},
	{Id: "lich", HP: [2]int{145, 155}, Moves: []Move{{Id: "plague", Wounds: 2, Foe: map[string]int{StWeak: 2, StVuln: 2}}, {Id: "drain", Dmg: 14, Lifesteal: true}, {Id: "bones", Dmg: 8, Block: 20}}, Cycle: []int{0, 1, 2, 1}},
	// 练习模式的训练木桩：生命很多、从不行动，用来测试伤害。
	{Id: DummyEnemy, HP: [2]int{999, 999}, Moves: []Move{{Id: "idle"}}, Weights: []int{1}},
	{Id: "abyss_lord", HP: [2]int{320, 320}, Moves: []Move{{Id: "corrode", Wounds: 2, Foe: map[string]int{StWeak: 2, StVuln: 2}}, {Id: "lash", Dmg: 8, Times: 3}, {Id: "bulwark", Block: 25, Self: map[string]int{StStrength: 3}}, {Id: "annihilate", Dmg: 36}}, Cycle: []int{0, 1, 2, 3}},
}

// DummyEnemy 是训练木桩，只在练习模式出现。
const DummyEnemy = "dummy"

// journeyEnemyList 是问道历练第四、五章专有的敌人(万妖林与九幽深处)。它们不在九幽秘境的幕里出现，也不进试炼，由历练的
// 节点与问心阶调用；练习的敌人表里也有它们，按 journeyEnemyOrigin 标出自历练第几章。数值是未放大的基础值，历练再按节点
// 种类与章节放大(见 pkg/wendao 的 EnemyScale)。
var journeyEnemyList = []*EnemyDef{
	// 第四章 · 万妖林
	{Id: "treant", HP: [2]int{50, 56}, Start: map[string]int{StThorns: 2}, Moves: []Move{{Id: "vine", Dmg: 9, Foe: map[string]int{StWeak: 1}}, {Id: "bloom", Block: 10, Heal: 6}}, Weights: []int{3, 2}},
	{Id: "viper", HP: [2]int{40, 44}, Moves: []Move{{Id: "fang", Dmg: 6, Times: 2, Foe: map[string]int{StVuln: 1}}, {Id: "venom", Dmg: 5, Wounds: 1, Foe: map[string]int{StWeak: 1}}}, Weights: []int{3, 2}},
	{Id: "fox_guard", HP: [2]int{50, 54}, Moves: []Move{{Id: "claw", Dmg: 6, Times: 2}, {Id: "charm", Self: map[string]int{StDodge: 1}, Foe: map[string]int{StWeak: 2, StVuln: 1}}}, Weights: []int{3, 2}},
	{Id: "bear", HP: [2]int{135, 145}, Moves: []Move{{Id: "maul", Dmg: 19}, {Id: "roar", Block: 14, Self: map[string]int{StStrength: 2}}, {Id: "stomp", Dmg: 8, Times: 2, Foe: map[string]int{StWeak: 1}}}, Cycle: []int{2, 0, 1, 0}},
	{Id: "spider", HP: [2]int{150, 160}, Moves: []Move{{Id: "web", Block: 12, Self: map[string]int{StStrength: 1}, Foe: map[string]int{StWeak: 2, StVuln: 2}}, {Id: "venom", Dmg: 15, Wounds: 2}, {Id: "bite", Dmg: 10, Times: 2}}, Cycle: []int{0, 1, 2, 1}},
	{Id: "fox_emperor", HP: [2]int{250, 260}, Moves: []Move{{Id: "tails", Dmg: 9, Times: 3}, {Id: "illusion", Block: 20, Self: map[string]int{StDodge: 2}, Foe: map[string]int{StWeak: 2}}, {Id: "soulfire", Dmg: 15, Foe: map[string]int{StBurn: 5}}, {Id: "frenzy", Wounds: 2, Self: map[string]int{StStrength: 3}}}, Cycle: []int{0, 2, 3, 1}},

	// 第五章 · 九幽深处
	{Id: "nether_hound", HP: [2]int{48, 52}, Moves: []Move{{Id: "pounce", Dmg: 5, Times: 3}, {Id: "howl", Self: map[string]int{StStrength: 2}, Foe: map[string]int{StWeak: 1}}}, Weights: []int{7, 3}},
	{Id: "hungry_ghost", HP: [2]int{54, 60}, Moves: []Move{{Id: "gnaw", Dmg: 8, Lifesteal: true}, {Id: "famine", Block: 6, Self: map[string]int{StStrength: 2}}}, Weights: []int{3, 2}},
	{Id: "judge", HP: [2]int{125, 133}, Moves: []Move{{Id: "verdict", Dmg: 25}, {Id: "ledger", Block: 18, Foe: map[string]int{StWeak: 1, StVuln: 1}}, {Id: "shackle", Dmg: 9, Times: 2, Foe: map[string]int{StVuln: 2}}}, Weights: []int{2, 2, 3}},
	{Id: "soul_lord", HP: [2]int{160, 170}, Moves: []Move{{Id: "devour", Dmg: 20, Lifesteal: true}, {Id: "dread", Wounds: 2, Foe: map[string]int{StWeak: 2, StVuln: 2}}, {Id: "ward", Block: 24, Self: map[string]int{StStrength: 2}}}, Cycle: []int{0, 1, 0, 2}},
	{Id: "taixu", HP: [2]int{270, 280}, Moves: []Move{{Id: "sword_rain", Dmg: 7, Times: 4}, {Id: "array", Block: 26, Self: map[string]int{StStrength: 2}}, {Id: "thunder", Dmg: 30}, {Id: "siphon", Dmg: 13, Lifesteal: true, Foe: map[string]int{StWeak: 2}}}, Cycle: []int{0, 3, 1, 2}},
	{Id: "patriarch", HP: [2]int{230, 240}, Moves: []Move{{Id: "corrode", Wounds: 3, Foe: map[string]int{StWeak: 2, StVuln: 2}}, {Id: "lash", Dmg: 8, Times: 4}, {Id: "bulwark", Block: 30, Self: map[string]int{StStrength: 2}}, {Id: "annihilate", Dmg: 40}}, Cycle: []int{0, 1, 2, 3}},
}

// journeyEnemyOrigin 是历练专有的敌人出自历练第几章、在那里是哪种对手。
var journeyEnemyOrigin = map[string]struct {
	Chapter int
	Kind    string
}{
	"treant": {4, NodeCombat}, "viper": {4, NodeCombat}, "fox_guard": {4, NodeCombat}, "bear": {4, NodeElite},
	"spider": {4, NodeElite}, "fox_emperor": {4, NodeBoss},
	"nether_hound": {5, NodeCombat}, "hungry_ghost": {5, NodeCombat}, "judge": {5, NodeElite}, "soul_lord": {5, NodeElite},
	"taixu": {5, NodeBoss}, "patriarch": {5, NodeBoss},
}

var enemyDefs = func() map[string]*EnemyDef {
	defs := make(map[string]*EnemyDef, len(enemyList)+len(journeyEnemyList))
	for _, d := range enemyList {
		defs[d.Id] = d
	}
	for _, d := range journeyEnemyList {
		defs[d.Id] = d
	}
	return defs
}()

// actEncounters 是每一幕的普通战、精英战与首领战，下标 0 是第一幕。
var actEncounters = [3]struct {
	Normal [][]string
	Elite  [][]string
	Boss   []string
}{
	{
		Normal: [][]string{{"slime"}, {"goblin", "goblin"}, {"skeleton"}, {"moth", "moth"}, {"goblin", "moth"}, {"slime", "moth"}},
		Elite:  [][]string{{"goblin_chief"}, {"gargoyle"}},
		Boss:   []string{"slime_king"},
	},
	{
		Normal: [][]string{{"werewolf"}, {"shade_mage", "beetle"}, {"lava_elemental"}, {"beetle", "beetle"}, {"werewolf", "shade_mage"}},
		Elite:  [][]string{{"troll"}, {"death_knight"}},
		Boss:   []string{"flame_lord"},
	},
	{
		Normal: [][]string{{"abyss_guard"}, {"void_walker", "void_walker"}, {"soul_eater"}, {"crystal_golem"}, {"abyss_guard", "void_walker"}},
		Elite:  [][]string{{"abyss_drake"}, {"lich"}},
		Boss:   []string{"abyss_lord"},
	},
}

// 随机事件。
const (
	EventAltar    = "altar"    // 古老祭坛：献血换遗物
	EventSpring   = "spring"   // 治愈之泉：回复生命或移除一张牌
	EventMerchant = "merchant" // 迷途商人：花金币买一张随机稀有牌
	EventDojo     = "dojo"     // 训练场：随机升级两张牌或最大生命 +5
	EventChest    = "chest"    // 诅咒宝箱：遗物与诅咒
	EventGambler  = "gambler"  // 赌徒：押金币赌一把
	EventMirror   = "mirror"   // 镜子：复制一张牌
)

var eventList = []string{EventAltar, EventSpring, EventMerchant, EventDojo, EventChest, EventGambler, EventMirror}
