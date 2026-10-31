package dungeon

import (
	"slices"
	"strings"
)

// TrialDef 是一关试炼：固定的职业、牌组、遗物与一到几场连续的战斗(生命不回复)。过关得一星；所有战斗一共用的回合数不超过
// Turns 再得一星；结束时生命不少于最大生命的 HPPct% 再得一星。每颗星第一次拿到时奖励 Crystals 颗魂晶。
type TrialDef struct {
	Id       string     `json:"id"`
	Class    string     `json:"class"`
	BonusHP  int        `json:"bonus_hp,omitempty"`
	Deck     []string   `json:"deck"`
	Relics   []string   `json:"relics"`
	Fights   [][]string `json:"fights"`
	Turns    int        `json:"turns"`
	HPPct    int        `json:"hp_pct"`
	Crystals int        `json:"crystals"`
}

// Trials 是所有试炼，按解锁顺序：上一关过关(至少一星)才能挑战下一关。牌名后面带 + 的是升级过的牌。
var Trials = []TrialDef{
	{Id: "t1", Class: ClassWarrior, Deck: starterDecks[ClassWarrior], Relics: []string{RelicWarriorHeart},
		Fights: [][]string{{"slime"}}, Turns: 3, HPPct: 85, Crystals: 5},
	{Id: "t2", Class: ClassWarrior, Deck: []string{"w_strike", "w_strike", "w_strike", "w_defend", "w_defend", "w_defend", "w_sweep", "w_sweep", "w_heavy", "w_warcry"},
		Relics: []string{RelicWarriorHeart}, Fights: [][]string{{"goblin", "goblin", "moth"}}, Turns: 3, HPPct: 80, Crystals: 5},
	{Id: "t3", Class: ClassMage, Deck: []string{"m_bolt", "m_bolt", "m_bolt", "m_ward", "m_ward", "m_ward", "m_ignite", "m_kindle", "m_flamewave", "m_detonate"},
		Relics: []string{RelicEmberStaff}, Fights: [][]string{{"skeleton", "slime"}}, Turns: 4, HPPct: 75, Crystals: 5},
	{Id: "t4", Class: ClassRanger, Deck: []string{"r_shoot", "r_shoot", "r_evade", "r_evade", "r_evade", "r_knives", "r_bladestorm", "r_quick", "r_keen"},
		Relics: []string{RelicQuiver}, Fights: [][]string{{"moth", "moth", "goblin"}}, Turns: 3, HPPct: 85, Crystals: 5},
	{Id: "t5", Class: ClassWarrior, Deck: []string{"w_strike", "w_strike", "w_strike", "w_strike", "w_defend", "w_defend", "w_defend", "w_defend", "w_heavy", "w_rend", "w_fury", "w_stance"},
		Relics: []string{RelicWarriorHeart}, Fights: [][]string{{"goblin_chief", "goblin"}}, Turns: 5, HPPct: 65, Crystals: 8},
	{Id: "t6", Class: ClassRanger, Deck: []string{"r_shoot", "r_shoot", "r_shoot", "r_evade", "r_evade", "r_evade", "r_aimed", "r_snare", "r_roll", "r_smoke"},
		Relics: []string{RelicQuiver}, Fights: [][]string{{"beetle", "beetle"}}, Turns: 6, HPPct: 60, Crystals: 8},
	{Id: "t7", Class: ClassMage, Deck: []string{"m_bolt", "m_bolt", "m_bolt", "m_ward", "m_ward", "m_ward", "m_ignite", "m_fireball", "m_frostnova", "m_arcane", "m_surge", "m_emberheart"},
		Relics: []string{RelicEmberStaff, RelicHerbPouch}, Fights: [][]string{{"werewolf"}, {"shade_mage", "beetle"}}, Turns: 9, HPPct: 60, Crystals: 8},
	{Id: "t8", Class: ClassWarrior, Deck: []string{"w_strike", "w_strike", "w_strike", "w_strike+", "w_defend", "w_defend", "w_defend", "w_heavy+", "w_sweep", "w_rend", "w_ironbody", "w_riposte", "w_execute"},
		Relics: []string{RelicWarriorHeart, RelicIronPlate}, Fights: [][]string{{"slime_king"}}, Turns: 7, HPPct: 60, Crystals: 12},
	{Id: "t9", Class: ClassRanger, BonusHP: 15, Deck: []string{"r_shoot+", "r_shoot+", "r_shoot+", "r_evade", "r_evade", "r_evade+", "r_aimed+", "r_snare", "r_smoke", "r_roll", "r_phantom", "r_bladedance"},
		Relics: []string{RelicQuiver, RelicCrystal, RelicRune}, Fights: [][]string{{"flame_lord"}}, Turns: 10, HPPct: 35, Crystals: 12},
	{Id: "t10", Class: ClassMage, BonusHP: 20, Deck: []string{"m_bolt+", "m_bolt+", "m_ward", "m_ward", "m_ward+", "m_ignite+", "m_fireball+", "m_meteor", "m_emberheart", "m_consume", "m_frostnova", "m_timewarp", "m_wellspring", "m_arcane"},
		Relics: []string{RelicEmberStaff, RelicFlame, RelicPhoenix}, Fights: [][]string{{"abyss_lord"}}, Turns: 12, HPPct: 30, Crystals: 12},
}

// TrialIndex 返回试炼的下标，没有这一关时返回 -1。
func TrialIndex(id string) int {
	return slices.IndexFunc(Trials, func(d TrialDef) bool { return d.Id == id })
}

// parseCard 解析试炼牌组里的牌名，带 + 的是升级过的牌。
func parseCard(spec string) Card {
	id, up := strings.CutSuffix(spec, "+")
	return Card{Id: id, Up: up}
}

// encounterKind 是一场遭遇的类型：有首领就是首领战，有精英就是精英战，否则是普通战。
func encounterKind(encounter []string) string {
	kind := NodeCombat
	for _, enc := range actEncounters {
		for _, id := range encounter {
			if slices.Contains(enc.Boss, id) {
				return NodeBoss
			}
			for _, group := range enc.Elite {
				if slices.Contains(group, id) {
					kind = NodeElite
				}
			}
		}
	}
	return kind
}

// NewTrial 开始一关试炼。
func NewTrial(seed uint64, id string) (*Run, error) {
	index := TrialIndex(id)
	if index < 0 {
		return nil, ErrAction
	}
	def := Trials[index]
	stats := classStats[def.Class]
	r := &Run{
		Version: StateVersion,
		Rng:     Rng{S: seed},
		Mode:    ModeTrial,
		Trial:   id,
		Class:   def.Class,
		Talents: map[string]int{},
		Act:     1,
		MaxHP:   stats.HP + def.BonusHP,
		Relics:  []string{},
		Potions: []string{},
		Path:    []int{},
		Seen:    []string{},
		Map:     []Node{},
	}
	r.HP = r.MaxHP
	for _, spec := range def.Deck {
		r.Deck = append(r.Deck, parseCard(spec))
	}
	for _, relic := range def.Relics {
		r.gainRelic(relic)
	}
	r.Events = nil
	r.Fight = -1
	r.nextTrialFight()
	return r, nil
}

// nextTrialFight 开始试炼的下一场战斗，全部打完时过关。
func (r *Run) nextTrialFight() {
	def := Trials[TrialIndex(r.Trial)]
	r.Fight++
	if r.Fight >= len(def.Fights) {
		r.Fight = len(def.Fights) - 1
		r.Phase = PhaseWon
		r.emit("victory", playerTarget, r.Stars(), "")
		return
	}
	encounter := def.Fights[r.Fight]
	spawns := make([]spawn, 0, len(encounter))
	for _, id := range encounter {
		spawns = append(spawns, spawn{Id: id, HP: 100, Dmg: 100})
	}
	r.startCombat(encounterKind(encounter), spawns)
}

// Stars 是试炼的星级：没过关为 0。
func (r *Run) Stars() int {
	if r.Mode != ModeTrial || !r.Won() {
		return 0
	}
	def := Trials[TrialIndex(r.Trial)]
	stars := 1
	if r.Turns <= def.Turns {
		stars++
	}
	if r.HP*100 >= def.HPPct*r.MaxHP {
		stars++
	}
	return stars
}
