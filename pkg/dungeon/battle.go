package dungeon

import "slices"

// 单场战斗是问道历练里的一场仗：主角带着自己的牌组、法宝与丹符打一场，没有路线图、商店与奇遇。赢了以后可以从几张功法里
// 选一张收下(可以不要)，然后这一场结束；输了也结束，牌组、法宝与生命都不会带到下一场。

// 单场战斗的上限：牌组最多几张、敌人最多几个、敌人强度最多放大到百分之几。
const (
	BattleMaxDeck    = 40
	BattleMaxEnemies = 3
	BattleMaxScale   = 2000
	BattleMaxHP      = 999
)

// BattleSetup 是一场战斗的设置。Kind 是遭遇、强敌或首领(combat、elite、boss)，决定奖励功法的稀有度；敌人的生命与伤害按
// HP、Dmg(百分比，100 为原样)放大；Strength 是开战时就有的力量；Rewards 是赢了以后几选一，0 表示不给功法；Allies 是随行
// 弟子带来的援护牌，只能是 AllyCards 里的，洗进这一场的牌组。
type BattleSetup struct {
	Class    string   `json:"class"`
	Deck     []Card   `json:"deck"`
	Relics   []string `json:"relics"`
	Potions  []string `json:"potions"`
	MaxHP    int      `json:"max_hp"`
	Strength int      `json:"strength,omitempty"`
	Kind     string   `json:"kind"`
	Enemies  []string `json:"enemies"`
	HP       int      `json:"hp"`
	Dmg      int      `json:"dmg"`
	Rewards  int      `json:"rewards,omitempty"`
	Allies   []Card   `json:"allies,omitempty"`
}

func validBattle(s BattleSetup) bool {
	if _, ok := classStats[s.Class]; !ok {
		return false
	}
	if !slices.Contains([]string{NodeCombat, NodeElite, NodeBoss}, s.Kind) {
		return false
	}
	if len(s.Deck) == 0 || len(s.Deck) > BattleMaxDeck || len(s.Enemies) == 0 || len(s.Enemies) > BattleMaxEnemies ||
		s.MaxHP < 1 || s.MaxHP > BattleMaxHP || s.Strength < 0 || s.Strength > 10 || s.Rewards < 0 || s.Rewards > 3 ||
		s.HP < 50 || s.HP > BattleMaxScale || s.Dmg < 50 || s.Dmg > BattleMaxScale || len(s.Potions) > 4 {
		return false
	}
	for _, c := range s.Deck {
		if cardDefs[c.Id] == nil || c.def().Rarity == RaritySpecial {
			return false
		}
	}
	if len(s.Allies) > len(AllyCards) {
		return false
	}
	for _, c := range s.Allies {
		if !slices.Contains(AllyCards[:], c.Id) {
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
	for _, id := range s.Potions {
		if !slices.Contains(potionList, id) {
			return false
		}
	}
	for _, id := range s.Enemies {
		if enemyDefs[id] == nil || id == DummyEnemy {
			return false
		}
	}
	return true
}

// NewBattle 按设置开始一场战斗。
func NewBattle(seed uint64, s BattleSetup) (*Run, error) {
	if !validBattle(s) {
		return nil, ErrAction
	}
	setup := s
	setup.Deck, setup.Relics, setup.Potions, setup.Enemies = slices.Clone(s.Deck), slices.Clone(s.Relics), slices.Clone(s.Potions), slices.Clone(s.Enemies)
	setup.Allies = slices.Clone(s.Allies)
	r := &Run{
		Version: StateVersion,
		Rng:     Rng{S: seed},
		Mode:    ModeBattle,
		Class:   s.Class,
		Talents: map[string]int{},
		Act:     1,
		MaxHP:   s.MaxHP,
		HP:      s.MaxHP,
		Deck:    append(slices.Clone(s.Deck), s.Allies...),
		Relics:  []string{},
		Potions: slices.Clone(s.Potions),
		Path:    []int{},
		Seen:    []string{},
		Map:     []Node{},
		Battle:  &setup,
	}
	for _, id := range s.Relics {
		r.gainRelic(id)
	}
	r.Events = nil
	spawns := make([]spawn, 0, len(s.Enemies))
	for _, id := range s.Enemies {
		spawns = append(spawns, spawn{Id: id, HP: s.HP, Dmg: s.Dmg})
	}
	r.startCombat(s.Kind, spawns)
	if s.Strength > 0 {
		r.Combat.St[StStrength] += s.Strength
	}
	return r, nil
}

// battleRewardWeights 是赢下一场之后功法奖励的稀有度权重(普通、罕见、稀有)。
var battleRewardWeights = map[string][]int{
	NodeCombat: {60, 35, 5},
	NodeElite:  {35, 50, 15},
	NodeBoss:   {0, 40, 60},
}

// winBattle 记下胜利，有功法奖励时进入选牌。
func (r *Run) winBattle(kind string) {
	r.Result = PhaseWon
	r.emit("win", playerTarget, 0, kind)
	r.Combat = nil
	if r.Battle.Rewards == 0 {
		r.Phase = PhaseDone
		return
	}
	r.Reward = &Reward{Cards: r.rollCards(r.Battle.Rewards, battleRewardWeights[kind])}
	r.Phase = PhaseReward
}

// Finished 表示一场战斗已经分出胜负、选完了奖励。
func (r *Run) Finished() bool { return r.Mode == ModeBattle && r.Phase == PhaseDone }

// ClassHP 是流派的初始最大生命。下面几个函数是问道的主角用到的流派与功法数据。
func ClassHP(class string) int { return classStats[class].HP }

// StarterDeck 返回流派的初始牌组，流派不认识时为空。
func StarterDeck(class string) []Card {
	cards := make([]Card, 0, len(starterDecks[class]))
	for _, id := range starterDecks[class] {
		cards = append(cards, Card{Id: id})
	}
	return cards
}

// ClassRelic 返回流派的本命法宝。
func ClassRelic(class string) string { return classStats[class].Relic }

// IsClass 表示 class 是一个流派。
func IsClass(class string) bool {
	_, ok := classStats[class]
	return ok
}

// CardRarity 返回功法的稀有度，不认识的功法返回 -1。
func CardRarity(id string) int {
	if d := cardDefs[id]; d != nil {
		return d.Rarity
	}
	return -1
}

// Upgradable 表示这张功法还能参悟(升级)。
func Upgradable(c Card) bool { return cardDefs[c.Id] != nil && upgradable(c) }

// IsRelic 表示 id 是一件法宝(含各流派的本命法宝)。
func IsRelic(id string) bool { return slices.Contains(relicIds(), id) }

// IsPotion 表示 id 是一种丹符。
func IsPotion(id string) bool { return slices.Contains(potionList, id) }

// Potions 是所有丹符，按固定顺序。
func Potions() []string { return slices.Clone(potionList) }

// PotionSlotsWith 是带着这些法宝时的丹符栏格数。
func PotionSlotsWith(relics []string) int {
	if slices.Contains(relics, RelicPotionBelt) {
		return 4
	}
	return 3
}

// DeckViews 是一组功法在页面上的样子，附带参悟(升级)之后的预览。
func DeckViews(cards []Card) []CardView { return deckViews(cards) }
