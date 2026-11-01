package wendao

import (
	"math"
	"slices"

	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// Hero 是玩家在历练里的主角，也就是宗主本人。主角的境界与弟子用同一张表；功法(卡牌)攒在收藏里，从中挑出牌组带进战斗；
// 法宝要佩戴才生效，佩戴数随境界增加；丹符与机缘放在行囊里。资源与丹药用宗门的(Save.Sect)。
type Hero struct {
	Class string  `json:"class"`
	Realm int     `json:"realm"`
	Layer int     `json:"layer"`
	Exp   float64 `json:"exp"`
	// Last 是上次结算闭关修为的时间。
	Last    int64          `json:"last"`
	Cards   []dungeon.Card `json:"cards"`
	Deck    []int          `json:"deck"`
	Relics  []string       `json:"relics"`
	Worn    []string       `json:"worn"`
	Potions map[string]int `json:"potions"`
	Keys    map[string]int `json:"keys"`
	// Nodes 是走过的节点：通过次数与上次通过的时间(采集按它算间隔)。
	Nodes map[string]*NodeRecord `json:"nodes"`
	// Steps 是问心阶已经登到第几级。
	Steps int `json:"steps,omitempty"`
	// QiReady 是下一颗聚气丹什么时候能服(与弟子相同，服一颗要等丹力化开)。
	QiReady int64 `json:"qi_ready,omitempty"`
	// Cave 是洞府里每间屋子的等级(cave.go)。
	Cave map[string]int `json:"cave,omitempty"`
	// Refine 是每件法宝祭炼到几级，Bless 是祭炼失败攒下的祝福(下一次成功率多几个百分点)，见 forge.go。
	Refine map[string]int `json:"refine,omitempty"`
	Bless  map[string]int `json:"bless,omitempty"`
	// Furnaces 是丹室里正在炼的几炉，Elixirs 是炼好的丹(按下品、中品、上品)，Buffs 是服下还有药力的金刚丹、龙虎丹，
	// Mastery 是每张丹方炼成过几炉；Tempered 是淬体丹加了多少生命，Golden 是服了几颗九转金丹。见 elixir.go。
	Furnaces []*Brew           `json:"furnaces,omitempty"`
	Elixirs  map[string][3]int `json:"elixirs,omitempty"`
	Buffs    map[string]*Buff  `json:"buffs,omitempty"`
	Mastery  map[string]int    `json:"mastery,omitempty"`
	Tempered int               `json:"tempered,omitempty"`
	Golden   int               `json:"golden,omitempty"`
	// Beasts 是兽苑里的灵兽(含还没孵出来的蛋)，BeastOut 是出战的那只(0 为不带)，NextBeast 是上一只的编号，LureReady
	// 是下一次能引兽的时间。见 beast.go。
	Beasts    []*Beast `json:"beasts,omitempty"`
	BeastOut  int      `json:"beast_out,omitempty"`
	NextBeast int      `json:"next_beast,omitempty"`
	LureReady int64    `json:"lure_ready,omitempty"`
	// Day 与 DayRepeats 记今天重复通过节点拿了几次资源。
	Day        string      `json:"day"`
	DayRepeats int         `json:"day_repeats"`
	Battle     *HeroBattle `json:"battle,omitempty"`
	Stats      HeroStats   `json:"stats"`
}

// NodeRecord 是一个节点的通过记录。
type NodeRecord struct {
	Clears int   `json:"clears"`
	Last   int64 `json:"last"`
}

// HeroBattle 是主角正在打的一场仗：在哪个节点(或问心阶的第几级)、这一场的局面，以及从行囊里带出来的丹符(打完把没用掉的
// 放回去)。
type HeroBattle struct {
	Node    string       `json:"node"`
	Step    int          `json:"step,omitempty"`
	Run     *dungeon.Run `json:"run"`
	Carried []string     `json:"carried"`
	// Team 是随行的弟子编号，Beast 是出战的灵兽编号(没有为 0)。
	Team  []int `json:"team,omitempty"`
	Beast int   `json:"beast,omitempty"`
}

// HeroStats 是主角的战绩。
type HeroStats struct {
	Wins          int `json:"wins"`
	Losses        int `json:"losses"`
	Breakthroughs int `json:"breakthroughs"`
}

// HeroAction 是历练里的一次操作。Move、Card、Target、Choice、Potion 是战斗里的一步(与九幽秘境相同)；Team 是开战(或登
// 问心阶)时随行的弟子；Item、Count 是在丹房炼的丹符与份数(洞府的操作里 Item 是屋子、法宝或丹方)；Fire 是开炉的火候，
// Index 是第几座丹炉，Grade 是服哪个品质的丹；Beast 是灵兽的编号，Name 是给灵兽起的名字。
type HeroAction struct {
	Type    string   `json:"action"`
	Class   string   `json:"class"`
	Node    string   `json:"node"`
	Team    []int    `json:"team"`
	Item    string   `json:"item"`
	Count   int      `json:"count"`
	Deck    []int    `json:"deck"`
	Relics  []string `json:"relics"`
	Card    int      `json:"card"`
	UsePill bool     `json:"use_pill"`
	Move    string   `json:"move"`
	Target  int      `json:"target"`
	Choice  int      `json:"choice"`
	Potion  int      `json:"potion"`
	Fire    string   `json:"fire"`
	Index   int      `json:"index"`
	Grade   int      `json:"grade"`
	Beast   int      `json:"beast"`
	Name    string   `json:"name"`
}

// normalize 补上存档里省略的空表。
func (h *Hero) normalize() {
	if h.Cards == nil {
		h.Cards = []dungeon.Card{}
	}
	if h.Deck == nil {
		h.Deck = []int{}
	}
	if h.Relics == nil {
		h.Relics = []string{}
	}
	if h.Worn == nil {
		h.Worn = []string{}
	}
	if h.Potions == nil {
		h.Potions = map[string]int{}
	}
	if h.Keys == nil {
		h.Keys = map[string]int{}
	}
	if h.Nodes == nil {
		h.Nodes = map[string]*NodeRecord{}
	}
	if h.Cave == nil {
		h.Cave = map[string]int{}
	}
	if h.Refine == nil {
		h.Refine = map[string]int{}
	}
	if h.Bless == nil {
		h.Bless = map[string]int{}
	}
	if h.Furnaces == nil {
		h.Furnaces = []*Brew{}
	}
	if h.Elixirs == nil {
		h.Elixirs = map[string][3]int{}
	}
	if h.Buffs == nil {
		h.Buffs = map[string]*Buff{}
	}
	if h.Mastery == nil {
		h.Mastery = map[string]int{}
	}
	if h.Beasts == nil {
		h.Beasts = []*Beast{}
	}
}

// NewHero 按流派创建主角：初始牌组全部放进牌组，本命法宝戴在身上，炼气一层。
func NewHero(class string, now int64) (*Hero, error) {
	if !dungeon.IsClass(class) {
		return nil, ErrAction
	}
	h := &Hero{Class: class, Layer: 1, Last: now, Cards: dungeon.StarterDeck(class)}
	h.normalize()
	for i := range h.Cards {
		h.Deck = append(h.Deck, i)
	}
	relic := dungeon.ClassRelic(class)
	h.Relics = append(h.Relics, relic)
	h.Worn = append(h.Worn, relic)
	return h, nil
}

// MaxHP 是主角的最大生命：流派的初始生命，每多一层、每高一个境界都更多，洞府的灵泉、祭炼过的法宝与淬体丹再加。
func (h *Hero) MaxHP() int {
	hp := dungeon.ClassHP(h.Class) + heroLayerHP*(h.Layer-1) + heroRealmHP*h.Realm + springHP*h.Cave[RoomSpring] +
		h.refineBonus().HP + h.Tempered
	return min(hp, dungeon.BattleMaxHP)
}

// Strength 是主角开战时的力量：每高一个境界多一点，祭炼过的锋路法宝与九转金丹再加(金刚丹、龙虎丹的药力另算)。
func (h *Hero) Strength() int {
	return min(h.Realm+h.refineBonus().Strength+h.Golden, dungeon.BattleMaxStrength)
}

// Block 是主角开战时的护体：祭炼过的御路法宝给的。
func (h *Hero) Block() int { return min(h.refineBonus().Block, dungeon.BattleMaxBlock) }

// RelicSlots 是能同时佩戴几件法宝。
func (h *Hero) RelicSlots() int { return 2 + h.Realm }

// Bottleneck 表示修到了本境界第九层圆满，要突破才能继续。
func (h *Hero) Bottleneck() bool {
	return h.Layer == MaxLayer && h.Exp >= ExpNeed(h.Realm, h.Layer)
}

// CultivateRate 是主角闭关每小时的修为：每高一个大境界快一倍(境界越高每层要的修为越多，主角靠它跟上章节)，聚灵阵每级
// 再加快(与弟子相同)，洞府的静室也加快，两者相加。
func (h *Hero) CultivateRate(s *Sect) float64 {
	bonus := arrayCultivate*float64(s.Buildings[BuildingArray]) + quietCultivate*float64(h.Cave[RoomQuiet])
	return heroExpPerHour * float64(int(1)<<h.Realm) * (1 + bonus)
}

// addExp 加修为并自动升层；第九层修满后修为不再增加，等突破。
func (h *Hero) addExp(gain float64) []Ev {
	if gain <= 0 || h.Bottleneck() {
		return nil
	}
	h.Exp += gain
	layers := 0
	for h.Layer < MaxLayer && h.Exp >= ExpNeed(h.Realm, h.Layer) {
		h.Exp -= ExpNeed(h.Realm, h.Layer)
		h.Layer++
		layers++
	}
	var evs []Ev
	if layers > 0 {
		evs = append(evs, Ev{K: "hero_layer", N: layers})
	}
	if h.Layer == MaxLayer && h.Exp >= ExpNeed(h.Realm, h.Layer) {
		h.Exp = ExpNeed(h.Realm, h.Layer)
		evs = append(evs, Ev{K: "hero_bottleneck"})
	}
	return evs
}

// tick 结算从上次到 now 的闭关修为(最多 OfflineCap 秒)，打仗时也在修炼。
func (h *Hero) tick(s *Sect, now int64) []Ev {
	if now <= h.Last {
		return nil
	}
	elapsed := min(now-h.Last, OfflineCap)
	h.Last = now
	return h.addExp(h.CultivateRate(s) * float64(elapsed) / 3600)
}

// BreakChance 是主角现在突破的成功率(%)，pill 表示服破境丹。
func (h *Hero) BreakChance(pill bool) int {
	if h.Realm >= MaxRealm {
		return 0
	}
	chance := breakBase[h.Realm]
	if pill {
		chance += breakPill
	}
	return min(chance, breakMax)
}

// Unlocked 表示这个节点现在能去：章节对应的境界已经到了，前置节点都走过了。
func (h *Hero) Unlocked(n *JourneyNode) bool {
	if n.Chapter > h.Realm {
		return false
	}
	for _, id := range n.Requires {
		if h.cleared(id) == 0 {
			return false
		}
	}
	return true
}

func (h *Hero) cleared(id string) int {
	if r := h.Nodes[id]; r != nil {
		return r.Clears
	}
	return 0
}

// GatherReady 返回采集节点下一次能去的时间，现在就能去时为 0。
func (h *Hero) GatherReady(n *JourneyNode, now int64) int64 {
	r := h.Nodes[n.Id]
	if n.Kind != NodeGather || r == nil || r.Clears == 0 || r.Last+n.Cooldown <= now {
		return 0
	}
	return r.Last + n.Cooldown
}

// deckCards 是牌组里的功法。
func (h *Hero) deckCards() []dungeon.Card {
	cards := make([]dungeon.Card, 0, len(h.Deck))
	for _, i := range h.Deck {
		cards = append(cards, h.Cards[i])
	}
	return cards
}

// HeroApply 执行一次历练操作：先结算闭关修为，再执行操作，返回这期间发生的事。还没有主角时只能创建；资源与丹药用宗门的，
// 宗门要已经建好。seed 只在开战时用到。出错时存档可能已经改了一部分，调用方不要保存。
func (sv *Save) HeroApply(a HeroAction, now int64, day string, seed uint64) ([]Ev, error) {
	s := sv.Sect
	if s == nil {
		return nil, ErrUnavailable
	}
	if a.Type == "create" {
		if sv.Hero != nil {
			return nil, ErrUnavailable
		}
		hero, err := NewHero(a.Class, now)
		if err != nil {
			return nil, err
		}
		sv.Hero = hero
		return []Ev{}, nil
	}
	h := sv.Hero
	if h == nil {
		return nil, ErrUnavailable
	}
	if h.Day != day {
		h.Day, h.DayRepeats = day, 0
	}
	evs := append(h.tick(s, now), h.hatchBeasts(now)...)
	if h.Battle != nil && a.Type != "sync" && a.Type != "battle" && a.Type != "flee" {
		return nil, ErrUnavailable
	}
	var more []Ev
	var err error
	switch a.Type {
	case "sync":
	case "deck":
		err = h.setDeck(a.Deck)
	case "wear":
		err = h.wear(a.Relics)
	case "upgrade":
		more, err = h.upgrade(s, a.Card)
	case "forget":
		err = h.forget(a.Card)
	case "breakthrough":
		more, err = h.breakthrough(s, a.UsePill)
	case "visit":
		more, err = h.visit(s, a.Node, now)
	case "fight":
		err = h.fight(s, a.Node, a.Team, seed, now)
	case "climb":
		err = h.climb(s, a.Team, seed, now)
	case "craft":
		more, err = h.craft(s, a.Item, a.Count)
	case "qi":
		more, err = h.takeQi(s, a.Item, now)
	case "room":
		more, err = h.buildRoom(s, a.Item)
	case "refine":
		more, err = h.refine(s, a.Item)
	case "brew":
		more, err = h.brew(s, a.Item, a.Fire, now)
	case "stoke":
		more, err = h.stoke(a.Index, now)
	case "collect":
		more, err = h.collect(s, a.Index, now)
	case "elixir":
		more, err = h.takeElixir(s, a.Item, a.Grade, now)
	case "lure":
		more, err = h.lure(s, a.Item, now)
	case "feed":
		more, err = h.feedBeast(s, a.Beast, a.Item, day)
	case "play":
		more, err = h.playBeast(a.Beast, day)
	case "evolve":
		more, err = h.evolveBeast(s, a.Beast)
	case "summon":
		err = h.summonBeast(a.Beast)
	case "rename":
		err = h.renameBeast(a.Beast, a.Name)
	case "release":
		more, err = h.releaseBeast(s, a.Beast)
	case "battle":
		more, err = h.battleStep(s, a, now)
	case "flee":
		if h.Battle == nil {
			return nil, ErrUnavailable
		}
		more = h.injureTeam(s, now)
		h.endBattle()
		h.Stats.Losses++
	default:
		err = ErrAction
	}
	if err != nil {
		return nil, err
	}
	return append(evs, more...), nil
}

// setDeck 按收藏里的下标重新挑选牌组，同一张不能挑两次。
func (h *Hero) setDeck(deck []int) error {
	if len(deck) < DeckMin || len(deck) > DeckMax {
		return ErrAction
	}
	seen := map[int]bool{}
	for _, i := range deck {
		if i < 0 || i >= len(h.Cards) || seen[i] {
			return ErrAction
		}
		seen[i] = true
	}
	h.Deck = slices.Clone(deck)
	return nil
}

// wear 换上一组法宝：只能戴自己有的，数量不超过佩戴上限。
func (h *Hero) wear(relics []string) error {
	if len(relics) > h.RelicSlots() {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, id := range relics {
		if seen[id] || !slices.Contains(h.Relics, id) {
			return ErrAction
		}
		seen[id] = true
	}
	h.Worn = slices.Clone(relics)
	return nil
}

// upgrade 花灵石参悟收藏里的一张功法。
func (h *Hero) upgrade(s *Sect, index int) ([]Ev, error) {
	if index < 0 || index >= len(h.Cards) || !dungeon.Upgradable(h.Cards[index]) {
		return nil, ErrAction
	}
	cost := Cost{Stone: UpgradeCost[dungeon.CardRarity(h.Cards[index].Id)]}
	if err := s.pay(cost); err != nil {
		return nil, err
	}
	h.Cards[index].Up = true
	return []Ev{{K: "res", S: "stone", N: -cost.Stone}, {K: "hero_upgrade", S: h.Cards[index].Id}}, nil
}

// forget 从收藏里丢掉一张不在牌组里的功法，给新的功法腾地方。
func (h *Hero) forget(index int) error {
	if index < 0 || index >= len(h.Cards) || slices.Contains(h.Deck, index) {
		return ErrAction
	}
	h.Cards = slices.Delete(h.Cards, index, index+1)
	for i, d := range h.Deck {
		if d > index {
			h.Deck[i] = d - 1
		}
	}
	return nil
}

// breakthrough 第九层圆满时冲击下一个境界，要用掉本境界的机缘；可以服宗门的破境丹。失败损失一部分修为，机缘不会丢。
func (h *Hero) breakthrough(s *Sect, pill bool) ([]Ev, error) {
	if h.Realm >= MaxRealm || !h.Bottleneck() {
		return nil, ErrUnavailable
	}
	key := BreakKeys[h.Realm]
	if key == "" || h.Keys[key] < 1 {
		return nil, ErrUnavailable
	}
	if pill {
		if s.Pills[PillBreak] < 1 {
			return nil, ErrCost
		}
		s.Pills[PillBreak]--
	}
	var evs []Ev
	if pill {
		evs = append(evs, Ev{K: "pill", S: PillBreak, N: -1})
	}
	if !s.Rng.Chance(h.BreakChance(pill)) {
		h.Exp = math.Max(0, h.Exp-breakFailExp*ExpNeed(h.Realm, h.Layer))
		return append(evs, Ev{K: "hero_break_fail"}), nil
	}
	h.Keys[key]--
	if h.Keys[key] == 0 {
		delete(h.Keys, key)
	}
	h.Realm++
	h.Layer, h.Exp = 1, 0
	h.Stats.Breakthroughs++
	return append(evs, Ev{K: "hero_realm", N: h.Realm}), nil
}

// visit 走过一个剧情或采集节点。剧情只走一次；采集隔 Cooldown 秒可以再去。
func (h *Hero) visit(s *Sect, id string, now int64) ([]Ev, error) {
	n := JourneyNodeOf(id)
	if n == nil || (n.Kind != NodeStory && n.Kind != NodeGather) {
		return nil, ErrAction
	}
	if !h.Unlocked(n) {
		return nil, ErrUnavailable
	}
	if n.Kind == NodeStory && h.cleared(id) > 0 {
		return nil, ErrUnavailable
	}
	if h.GatherReady(n, now) > 0 {
		return nil, ErrUnavailable
	}
	return h.clear(s, n, now), nil
}

// clear 记下通过一个节点并发奖励：第一次给全部修为、资源与法宝丹符机缘，之后修为减半、资源按每天的次数。
func (h *Hero) clear(s *Sect, n *JourneyNode, now int64) []Ev {
	r := h.Nodes[n.Id]
	if r == nil {
		r = &NodeRecord{}
		h.Nodes[n.Id] = r
	}
	first := r.Clears == 0
	r.Clears++
	r.Last = now
	evs := []Ev{{K: "hero_clear", S: n.Id, N: r.Clears}}
	exp := float64(n.Exp)
	reward := n.First
	if !first {
		exp /= 2
		reward = Cost{}
		if h.DayRepeats < DailyRepeatRewards && n.Repeat != (Cost{}) {
			h.DayRepeats++
			reward = n.Repeat
		}
	}
	if exp > 0 {
		evs = append(evs, Ev{K: "hero_exp", N: int(math.Round(exp))})
		evs = append(evs, h.addExp(exp)...)
	}
	evs = append(evs, s.gain(reward)...)
	if !first {
		return evs
	}
	if n.Relic != "" && !slices.Contains(h.Relics, n.Relic) {
		h.Relics = append(h.Relics, n.Relic)
		evs = append(evs, Ev{K: "hero_relic", S: n.Relic})
		if len(h.Worn) < h.RelicSlots() {
			h.Worn = append(h.Worn, n.Relic)
		}
	}
	for _, p := range n.Potions {
		if h.Potions[p] < MaxPotionStack {
			h.Potions[p]++
			evs = append(evs, Ev{K: "hero_potion", S: p, N: 1})
		}
	}
	if n.Key != "" {
		h.Keys[n.Key]++
		evs = append(evs, Ev{K: "hero_key", S: n.Key})
	}
	if n.Id == StepsUnlock {
		evs = append(evs, Ev{K: "hero_steps_open"})
	}
	return evs
}

// clearStep 记下登上问心阶的一级并发奖励：修为、宗门资源，强敌与首领那一级另给丹药。
func (h *Hero) clearStep(s *Sect, st Step) []Ev {
	h.Steps = st.Step
	evs := []Ev{{K: "hero_step", N: st.Step}, {K: "hero_exp", N: st.Exp}}
	evs = append(evs, h.addExp(float64(st.Exp))...)
	evs = append(evs, s.gain(st.Reward)...)
	if st.Pill != "" {
		s.Pills[st.Pill]++
		evs = append(evs, Ev{K: "pill", S: st.Pill, N: 1})
	}
	return evs
}

// fight 在一个战斗节点开战。
func (h *Hero) fight(s *Sect, id string, team []int, seed uint64, now int64) error {
	n := JourneyNodeOf(id)
	if n == nil || (n.Kind != NodeCombat && n.Kind != NodeElite && n.Kind != NodeBoss) {
		return ErrAction
	}
	if !h.Unlocked(n) {
		return ErrUnavailable
	}
	hp, dmg := EnemyScale(n)
	return h.startBattle(s, &HeroBattle{Node: id}, n.Kind, n.Enemies, hp, dmg, team, seed, now)
}

// climb 挑战问心阶的下一级。
func (h *Hero) climb(s *Sect, team []int, seed uint64, now int64) error {
	if !h.StepsOpen() || h.Steps >= MaxSteps {
		return ErrUnavailable
	}
	st := StepOf(h.Steps + 1)
	return h.startBattle(s, &HeroBattle{Step: st.Step}, st.Kind, st.Enemies, st.HP, st.Dmg, team, seed, now)
}

// startBattle 开始一场仗：带上牌组、佩戴的法宝、行囊里的丹符(按丹符栏格数，从行囊里拿出来)与随行弟子的援护牌。battle
// 记着这一场打的是哪个节点或问心阶的哪一级。
func (h *Hero) startBattle(s *Sect, battle *HeroBattle, kind string, enemies []string, hp, dmg int, team []int, seed uint64,
	now int64) error {
	if len(h.Deck) < DeckMin {
		return ErrUnavailable
	}
	if len(team) > CompanionMax {
		return ErrAction
	}
	allies := []dungeon.Card{}
	for i, id := range team {
		d := s.disciple(id)
		if d == nil || slices.Contains(team[:i], id) {
			return ErrAction
		}
		if !d.home(now) {
			return ErrUnavailable
		}
		allies = append(allies, dungeon.Card{Id: dungeon.AllyCards[d.Element], Up: d.Realm >= 1})
	}
	carried := []string{}
	slots := dungeon.PotionSlotsWith(h.Worn)
	for _, p := range dungeon.Potions() {
		for h.Potions[p] > 0 && len(carried) < slots {
			h.Potions[p]--
			carried = append(carried, p)
		}
		if h.Potions[p] == 0 {
			delete(h.Potions, p)
		}
	}
	rewards := BattleRewardChoices
	if len(h.Cards) >= MaxCards {
		rewards = 0
	}
	buffBlock, buffStrength := h.buffPower()
	run, err := dungeon.NewBattle(seed, dungeon.BattleSetup{
		Class: h.Class, Deck: h.deckCards(), Relics: h.Worn, Potions: carried, MaxHP: h.MaxHP(),
		Strength: min(h.Strength()+buffStrength, dungeon.BattleMaxStrength),
		Block:    min(h.Block()+buffBlock, dungeon.BattleMaxBlock),
		Kind:     kind, Enemies: enemies, HP: hp, Dmg: dmg, Rewards: rewards, Allies: allies, Beast: h.battleBeast(),
	})
	if err != nil {
		return err
	}
	h.spendBuffs()
	battle.Run, battle.Carried, battle.Team = run, carried, slices.Clone(team)
	if run.Battle.Beast != nil {
		battle.Beast = h.BeastOut
	}
	h.Battle = battle
	return nil
}

// injureTeam 让随行的弟子受伤(输了或退走)。已经离开宗门的弟子不管。
func (h *Hero) injureTeam(s *Sect, now int64) []Ev {
	var evs []Ev
	for _, id := range h.Battle.Team {
		if d := s.disciple(id); d != nil {
			d.Injured = max(d.Injured, now+companionInjury)
			evs = append(evs, Ev{K: "injured", Who: &d.Name, N: companionInjury / 3600})
		}
	}
	return evs
}

// craft 在宗门丹房给主角炼丹符，花宗门的资源；每种丹符最多存 MaxPotionStack 份。
func (h *Hero) craft(s *Sect, potion string, count int) ([]Ev, error) {
	index := slices.IndexFunc(PotionRecipes, func(r PotionRecipe) bool { return r.Potion == potion })
	if index < 0 || count < 1 || count > MaxPotionStack {
		return nil, ErrAction
	}
	recipe := PotionRecipes[index]
	if s.Buildings[BuildingAlchemy] < recipe.Alchemy || h.Potions[potion]+count > MaxPotionStack {
		return nil, ErrUnavailable
	}
	cost := recipe.Cost.scale(float64(count))
	if err := s.pay(cost); err != nil {
		return nil, err
	}
	h.Potions[potion] += count
	return append(spentEvs(cost), Ev{K: "hero_potion", S: potion, N: count}), nil
}

// takeQi 服一颗宗门的聚气丹(pill 为空或 qi)或培元丹：马上得到几个小时闭关的修为，要等丹力化开才能服下一颗。
func (h *Hero) takeQi(s *Sect, pill string, now int64) ([]Ev, error) {
	if pill == "" {
		pill = PillQi
	}
	if pill != PillQi && pill != PillPeiyuan {
		return nil, ErrAction
	}
	if h.Bottleneck() || h.QiReady > now {
		return nil, ErrUnavailable
	}
	if s.Pills[pill] < 1 {
		return nil, ErrCost
	}
	s.Pills[pill]--
	h.QiReady = now + qiPillCooldown
	gain := h.CultivateRate(s) * float64(QiPillHours(pill))
	evs := []Ev{{K: "pill", S: pill, N: -1}, {K: "hero_exp", N: int(math.Round(gain))}}
	return append(evs, h.addExp(gain)...), nil
}

// endBattle 结束这一场：没用掉的丹符放回行囊。
func (h *Hero) endBattle() {
	for _, p := range h.Battle.Run.Potions {
		h.Potions[p] = min(h.Potions[p]+1, MaxPotionStack)
	}
	h.Battle = nil
}

// battleStep 在战斗里走一步；分出胜负并选完奖励后结束这一场，赢了按节点(或问心阶的那一级)发奖励，收下的功法进收藏
// (牌组没满时也放进牌组)。
func (h *Hero) battleStep(s *Sect, a HeroAction, now int64) ([]Ev, error) {
	if h.Battle == nil {
		return nil, ErrUnavailable
	}
	run := h.Battle.Run
	if err := run.Apply(dungeon.Action{Type: a.Move, Node: -1, Card: a.Card, Target: a.Target, Choice: a.Choice, Potion: a.Potion}); err != nil {
		return nil, ErrAction
	}
	if !run.Finished() {
		return nil, nil
	}
	battle := h.Battle
	if run.Result != dungeon.PhaseWon {
		evs := append([]Ev{{K: "hero_lost", S: battle.Node, N: battle.Step}}, h.injureTeam(s, now)...)
		h.endBattle()
		h.Stats.Losses++
		return evs, nil
	}
	h.endBattle()
	h.Stats.Wins++
	s.Progress(TaskJourney, 1)
	var evs []Ev
	var exp int
	if battle.Step > 0 {
		st := StepOf(battle.Step)
		evs, exp = h.clearStep(s, st), st.Exp
		evs = append(evs, h.rewardBeast(battle.Beast, st.Kind, Chapters-1)...)
	} else {
		n := JourneyNodeOf(battle.Node)
		evs, exp = h.clear(s, n, now), n.Exp
		evs = append(evs, h.rewardBeast(battle.Beast, n.Kind, n.Chapter)...)
	}
	// 随行的弟子也有所得：这一场修为的一半。
	for _, id := range battle.Team {
		if d := s.disciple(id); d != nil {
			evs = append(evs, s.addExp(d, float64(exp)/2)...)
		}
	}
	if run.Picked != nil && len(h.Cards) < MaxCards {
		h.Cards = append(h.Cards, *run.Picked)
		if len(h.Deck) < DeckMax {
			h.Deck = append(h.Deck, len(h.Cards)-1)
		}
		evs = append(evs, Ev{K: "hero_card", S: run.Picked.Id})
	}
	return evs, nil
}

// HeroView 是主角在页面上的样子。Open 是现在能去的节点；Ready 是采集节点下一次能去的时间。
type HeroView struct {
	Class       string                 `json:"class"`
	Realm       int                    `json:"realm"`
	Layer       int                    `json:"layer"`
	Exp         float64                `json:"exp"`
	Need        float64                `json:"need"`
	Rate        float64                `json:"rate"`
	Bottleneck  bool                   `json:"bottleneck"`
	BreakKey    string                 `json:"break_key"`
	BreakChance [2]int                 `json:"break_chance"`
	MaxHP       int                    `json:"max_hp"`
	Strength    int                    `json:"strength"`
	RelicSlots  int                    `json:"relic_slots"`
	PotionSlots int                    `json:"potion_slots"`
	Cards       []dungeon.CardView     `json:"cards"`
	Deck        []int                  `json:"deck"`
	Relics      []string               `json:"relics"`
	Worn        []string               `json:"worn"`
	Potions     map[string]int         `json:"potions"`
	Keys        map[string]int         `json:"keys"`
	Nodes       map[string]*NodeRecord `json:"nodes"`
	Open        []string               `json:"open"`
	Ready       map[string]int64       `json:"ready"`
	RepeatsLeft int                    `json:"repeats_left"`
	Stats       HeroStats              `json:"stats"`
	// StepsOpen 表示问心阶已经开放，Steps 是登到了第几级。
	StepsOpen bool            `json:"steps_open"`
	Steps     int             `json:"steps"`
	Battle    *HeroBattleView `json:"battle,omitempty"`
	Now       int64           `json:"now"`
	QiReady   int64           `json:"qi_ready"`
	// Alchemy 是宗门丹房的等级，决定能炼哪些丹符。
	Alchemy int `json:"alchemy"`
	// Companions 是宗门里的弟子，Ready 表示现在能随行(在宗门里、没受伤)。
	Companions []CompanionView `json:"companions"`
	// Cave 是洞府每间屋子的等级，RoomCap 是现在的境界能建到几级。
	Cave    map[string]int `json:"cave"`
	RoomCap int            `json:"room_cap"`
	// Block 是开战时的护体；Refine、Bless 是法宝的祭炼等级与祝福，RefineBonus 是戴着的法宝祭炼给的加成。
	Block       int            `json:"block"`
	Refine      map[string]int `json:"refine"`
	Bless       map[string]int `json:"bless"`
	RefineBonus RefineBonus    `json:"refine_bonus"`
	// 丹室：正在炼的几炉(Score 是不含随机数的品质分)、丹炉数、炼好的丹、身上的药力、熟练、淬体与九转金丹。
	Furnaces     []BrewView        `json:"furnaces"`
	FurnaceSlots int               `json:"furnace_slots"`
	Elixirs      map[string][3]int `json:"elixirs"`
	Buffs        map[string]*Buff  `json:"buffs"`
	Mastery      map[string]int    `json:"mastery"`
	Tempered     int               `json:"tempered"`
	TemperCap    int               `json:"temper_cap"`
	Golden       int               `json:"golden"`
	// 兽苑：灵兽、出战的那只、下一次能引兽的时间与能养几只。
	Beasts     []BeastView `json:"beasts"`
	BeastOut   int         `json:"beast_out"`
	LureReady  int64       `json:"lure_ready"`
	BeastSlots int         `json:"beast_slots"`
}

// BeastView 是一只灵兽：Power 是出手的力道，Cap 是现在的等级上限，FedToday、PlayedToday 是今天喂了几次、嬉戏过没有。
type BeastView struct {
	Beast
	Power       int  `json:"power"`
	Cap         int  `json:"cap"`
	FedToday    int  `json:"fed_today"`
	PlayedToday bool `json:"played_today"`
}

// BrewView 是丹室里正在炼的一炉：StokeFrom、StokeTo 是能调火的时间段。
type BrewView struct {
	Brew
	StokeFrom int64 `json:"stoke_from"`
	StokeTo   int64 `json:"stoke_to"`
	Score     int   `json:"score"`
}

// CompanionView 是一名可以随行的弟子。
type CompanionView struct {
	Id      int    `json:"id"`
	Name    [3]int `json:"name"`
	Element int    `json:"element"`
	Realm   int    `json:"realm"`
	Layer   int    `json:"layer"`
	Ready   bool   `json:"ready"`
}

// HeroBattleView 是正在打的一场仗。
type HeroBattleView struct {
	Node string       `json:"node"`
	Step int          `json:"step,omitempty"`
	Run  dungeon.View `json:"run"`
	Team []int        `json:"team"`
	// Beast 是这一场出战的灵兽(没有为空)。
	Beast *dungeon.BattleBeast `json:"beast,omitempty"`
}

// View 返回主角在 now 时刻的样子，s 是宗门(闭关速度看聚灵阵)。
func (h *Hero) View(s *Sect, now int64, day string) HeroView {
	v := HeroView{
		Class: h.Class, Realm: h.Realm, Layer: h.Layer, Exp: h.Exp, Need: ExpNeed(h.Realm, h.Layer), Rate: h.CultivateRate(s),
		Bottleneck: h.Bottleneck(), BreakChance: [2]int{h.BreakChance(false), h.BreakChance(true)},
		MaxHP: h.MaxHP(), Strength: h.Strength(), RelicSlots: h.RelicSlots(), PotionSlots: dungeon.PotionSlotsWith(h.Worn),
		Cards: dungeon.DeckViews(h.Cards), Deck: h.Deck, Relics: h.Relics, Worn: h.Worn, Potions: h.Potions, Keys: h.Keys,
		Nodes: h.Nodes, Open: []string{}, Ready: map[string]int64{}, Stats: h.Stats, Now: now,
		RepeatsLeft: DailyRepeatRewards, QiReady: h.QiReady, Companions: []CompanionView{},
		Alchemy: s.Buildings[BuildingAlchemy], StepsOpen: h.StepsOpen(), Steps: h.Steps,
		Cave: h.Cave, RoomCap: RoomCap(h.Realm), Block: h.Block(), Refine: h.Refine, Bless: h.Bless,
		RefineBonus: h.refineBonus(), Beasts: []BeastView{}, BeastOut: h.BeastOut, LureReady: h.LureReady,
		BeastSlots: BeastSlots(h.Cave[RoomDen]), Furnaces: []BrewView{}, FurnaceSlots: FurnaceSlots(h.Cave[RoomFurnace]),
		Elixirs: h.Elixirs, Buffs: h.Buffs, Mastery: h.Mastery, Tempered: h.Tempered, TemperCap: TemperCap(h.Realm),
		Golden: h.Golden,
	}
	for _, b := range h.Beasts {
		v.Beasts = append(v.Beasts, BeastView{Beast: *b, Power: b.Power(), Cap: b.LevelCap(h.Cave[RoomDen]),
			FedToday: b.Fed * boolInt(b.FedDay == day), PlayedToday: b.Played == day})
	}
	for _, b := range h.Furnaces {
		from, to := b.StokeWindow()
		v.Furnaces = append(v.Furnaces, BrewView{Brew: *b, StokeFrom: from, StokeTo: to, Score: h.QualityScore(b)})
	}
	for _, d := range s.Disciples {
		v.Companions = append(v.Companions, CompanionView{Id: d.Id, Name: d.Name, Element: d.Element, Realm: d.Realm,
			Layer: d.Layer, Ready: d.home(now)})
	}
	if h.Realm < MaxRealm {
		v.BreakKey = BreakKeys[h.Realm]
	}
	if h.Day == day {
		v.RepeatsLeft = max(DailyRepeatRewards-h.DayRepeats, 0)
	}
	for i := range JourneyNodes {
		n := &JourneyNodes[i]
		if !h.Unlocked(n) || (n.Kind == NodeStory && h.cleared(n.Id) > 0) {
			continue
		}
		v.Open = append(v.Open, n.Id)
		if ready := h.GatherReady(n, now); ready > 0 {
			v.Ready[n.Id] = ready
		}
	}
	if h.Battle != nil {
		v.Battle = &HeroBattleView{Node: h.Battle.Node, Step: h.Battle.Step, Run: h.Battle.Run.View(), Team: h.Battle.Team,
			Beast: h.Battle.Run.Battle.Beast}
	}
	return v
}
