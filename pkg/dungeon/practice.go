package dungeon

import "slices"

// 练习模式的上限：牌组最多几张、敌人最多几个、强度最多放大到第几幕。
const (
	PracticeMaxDeck    = 40
	PracticeMaxEnemies = 3
	PracticeMaxStage   = 20
	PracticeMaxHP      = 999
)

// PracticeSetup 是练习模式的设置：任意职业、牌组、遗物与敌人，敌人按第 Stage 幕的强度放大；可以打开无限能量与不会倒下。
type PracticeSetup struct {
	Class          string   `json:"class"`
	Deck           []Card   `json:"deck"`
	Relics         []string `json:"relics"`
	Enemies        []string `json:"enemies"`
	Stage          int      `json:"stage"`
	MaxHP          int      `json:"max_hp"`
	InfiniteEnergy bool     `json:"infinite_energy"`
	Invincible     bool     `json:"invincible"`
}

// spawn 是一个要生成的敌人与它的生命、伤害倍率(百分比)。
type spawn struct {
	Id  string
	HP  int
	Dmg int
}

// NativeAct 是敌人原本出现在第几幕，训练木桩为 0。
func NativeAct(id string) int {
	for act, enc := range actEncounters {
		groups := append(append([][]string{}, enc.Normal...), enc.Elite...)
		groups = append(groups, enc.Boss)
		for _, group := range groups {
			if slices.Contains(group, id) {
				return act + 1
			}
		}
	}
	return 0
}

func validPractice(s PracticeSetup) bool {
	if _, ok := classStats[s.Class]; !ok {
		return false
	}
	if len(s.Deck) == 0 || len(s.Deck) > PracticeMaxDeck || len(s.Enemies) == 0 || len(s.Enemies) > PracticeMaxEnemies ||
		s.Stage < 1 || s.Stage > PracticeMaxStage || s.MaxHP < 0 || s.MaxHP > PracticeMaxHP || len(s.Relics) > len(relicIds()) {
		return false
	}
	for _, c := range s.Deck {
		if cardDefs[c.Id] == nil || (c.Up && c.def().Type == TypeCurse) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, id := range s.Relics {
		if seen[id] || !slices.Contains(relicIds(), id) {
			return false
		}
		seen[id] = true
	}
	for _, id := range s.Enemies {
		if enemyDefs[id] == nil {
			return false
		}
	}
	return true
}

// relicIds 是所有遗物，含各职业的专属遗物。
func relicIds() []string {
	ids := []string{RelicWarriorHeart, RelicEmberStaff, RelicQuiver}
	for _, tier := range relicTiers {
		ids = append(ids, tier...)
	}
	return ids
}

// NewPractice 按设置开始一场练习战斗。
func NewPractice(seed uint64, s PracticeSetup) (*Run, error) {
	if !validPractice(s) {
		return nil, ErrAction
	}
	setup := s
	setup.Deck, setup.Relics, setup.Enemies = slices.Clone(s.Deck), slices.Clone(s.Relics), slices.Clone(s.Enemies)
	r := &Run{
		Version:  StateVersion,
		Rng:      Rng{S: seed},
		Mode:     ModePractice,
		Class:    s.Class,
		Talents:  map[string]int{},
		Act:      setup.Stage,
		Potions:  []string{},
		Path:     []int{},
		Seen:     []string{},
		Map:      []Node{},
		Practice: &setup,
	}
	return r, r.resetPractice()
}

// resetPractice 按设置从头再打一场：生命回满、牌组与遗物复原。
func (r *Run) resetPractice() error {
	if r.Mode != ModePractice || r.Practice == nil {
		return ErrAction
	}
	s := r.Practice
	r.MaxHP = s.MaxHP
	if r.MaxHP == 0 {
		r.MaxHP = classStats[s.Class].HP
	}
	r.HP, r.Result, r.Kills = r.MaxHP, "", 0
	r.Deck = slices.Clone(s.Deck)
	r.Relics = []string{}
	for _, id := range s.Relics {
		r.gainRelic(id)
	}
	r.Events = nil
	spawns := make([]spawn, 0, len(s.Enemies))
	for _, id := range s.Enemies {
		hp, dmg := 100, 100
		if native := NativeAct(id); native > 0 {
			hp, dmg = ActScale(s.Stage, native)
		}
		spawns = append(spawns, spawn{Id: id, HP: hp, Dmg: dmg})
	}
	r.startCombat(encounterKind(s.Enemies), spawns)
	return nil
}
