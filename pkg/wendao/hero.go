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

// HeroBattle 是主角正在打的一场仗：在哪个节点、这一场的局面，以及从行囊里带出来的丹符(打完把没用掉的放回去)。
type HeroBattle struct {
	Node    string       `json:"node"`
	Run     *dungeon.Run `json:"run"`
	Carried []string     `json:"carried"`
}

// HeroStats 是主角的战绩。
type HeroStats struct {
	Wins          int `json:"wins"`
	Losses        int `json:"losses"`
	Breakthroughs int `json:"breakthroughs"`
}

// HeroAction 是历练里的一次操作。Move、Card、Target、Choice、Potion 是战斗里的一步(与九幽秘境相同)。
type HeroAction struct {
	Type    string   `json:"action"`
	Class   string   `json:"class"`
	Node    string   `json:"node"`
	Deck    []int    `json:"deck"`
	Relics  []string `json:"relics"`
	Card    int      `json:"card"`
	UsePill bool     `json:"use_pill"`
	Move    string   `json:"move"`
	Target  int      `json:"target"`
	Choice  int      `json:"choice"`
	Potion  int      `json:"potion"`
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

// MaxHP 是主角的最大生命：流派的初始生命，每多一层、每高一个境界都更多。
func (h *Hero) MaxHP() int {
	return dungeon.ClassHP(h.Class) + heroLayerHP*(h.Layer-1) + heroRealmHP*h.Realm
}

// Strength 是主角开战时的力量：每高一个境界多一点。
func (h *Hero) Strength() int { return h.Realm }

// RelicSlots 是能同时佩戴几件法宝。
func (h *Hero) RelicSlots() int { return 2 + h.Realm }

// Bottleneck 表示修到了本境界第九层圆满，要突破才能继续。
func (h *Hero) Bottleneck() bool {
	return h.Layer == MaxLayer && h.Exp >= ExpNeed(h.Realm, h.Layer)
}

// CultivateRate 是主角闭关每小时的修为：聚灵阵每级加快，与弟子相同。
func (h *Hero) CultivateRate(s *Sect) float64 {
	return heroExpPerHour * (1 + arrayCultivate*float64(s.Buildings[BuildingArray]))
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
	evs := h.tick(s, now)
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
		err = h.fight(a.Node, seed)
	case "battle":
		more, err = h.battleStep(s, a, now)
	case "flee":
		if h.Battle == nil {
			return nil, ErrUnavailable
		}
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
	return evs
}

// fight 在一个战斗节点开战：带上牌组、佩戴的法宝与行囊里的丹符(按丹符栏格数，从行囊里拿出来)。
func (h *Hero) fight(id string, seed uint64) error {
	n := JourneyNodeOf(id)
	if n == nil || (n.Kind != NodeCombat && n.Kind != NodeElite && n.Kind != NodeBoss) {
		return ErrAction
	}
	if !h.Unlocked(n) {
		return ErrUnavailable
	}
	if len(h.Deck) < DeckMin {
		return ErrUnavailable
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
	hp, dmg := EnemyScale(n)
	run, err := dungeon.NewBattle(seed, dungeon.BattleSetup{
		Class: h.Class, Deck: h.deckCards(), Relics: h.Worn, Potions: carried, MaxHP: h.MaxHP(), Strength: h.Strength(),
		Kind: n.Kind, Enemies: n.Enemies, HP: hp, Dmg: dmg, Rewards: rewards,
	})
	if err != nil {
		return err
	}
	h.Battle = &HeroBattle{Node: id, Run: run, Carried: carried}
	return nil
}

// endBattle 结束这一场：没用掉的丹符放回行囊。
func (h *Hero) endBattle() {
	for _, p := range h.Battle.Run.Potions {
		h.Potions[p] = min(h.Potions[p]+1, MaxPotionStack)
	}
	h.Battle = nil
}

// battleStep 在战斗里走一步；分出胜负并选完奖励后结束这一场，赢了按节点发奖励，收下的功法进收藏(牌组没满时也放进牌组)。
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
	n := JourneyNodeOf(h.Battle.Node)
	won := run.Result == dungeon.PhaseWon
	h.endBattle()
	if !won {
		h.Stats.Losses++
		return []Ev{{K: "hero_lost", S: n.Id}}, nil
	}
	h.Stats.Wins++
	evs := h.clear(s, n, now)
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
	Battle      *HeroBattleView        `json:"battle,omitempty"`
	Now         int64                  `json:"now"`
}

// HeroBattleView 是正在打的一场仗。
type HeroBattleView struct {
	Node string       `json:"node"`
	Run  dungeon.View `json:"run"`
}

// View 返回主角在 now 时刻的样子，s 是宗门(闭关速度看聚灵阵)。
func (h *Hero) View(s *Sect, now int64, day string) HeroView {
	v := HeroView{
		Class: h.Class, Realm: h.Realm, Layer: h.Layer, Exp: h.Exp, Need: ExpNeed(h.Realm, h.Layer), Rate: h.CultivateRate(s),
		Bottleneck: h.Bottleneck(), BreakChance: [2]int{h.BreakChance(false), h.BreakChance(true)},
		MaxHP: h.MaxHP(), Strength: h.Strength(), RelicSlots: h.RelicSlots(), PotionSlots: dungeon.PotionSlotsWith(h.Worn),
		Cards: dungeon.DeckViews(h.Cards), Deck: h.Deck, Relics: h.Relics, Worn: h.Worn, Potions: h.Potions, Keys: h.Keys,
		Nodes: h.Nodes, Open: []string{}, Ready: map[string]int64{}, Stats: h.Stats, Now: now,
		RepeatsLeft: DailyRepeatRewards,
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
		v.Battle = &HeroBattleView{Node: h.Battle.Node, Run: h.Battle.Run.View()}
	}
	return v
}
