package wendao

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// 灵兽：养在洞府兽苑里的灵兽，可以带一只跟主角出战。
//   - 引兽：兽苑开辟以后每 LureCooldown 秒能引一次，付饵料引来一颗灵兽蛋，BeastHatch 秒后孵出来。饵料越好，资质越高，
//     也越容易引来九尾灵狐。
//   - 养成：喂灵草或丹药涨经验，每天喂 BeastFeeds 次；每天能嬉戏一次涨亲密。等级到 10、20 级要进化才能再往上，
//     等级还不能超过兽苑等级的三倍。
//   - 出战：历练(含问心阶)里每个回合结束时灵兽自己出手一次(dungeon.BattleBeast)，力道看种类、等级、资质、进化与亲密。
//     打赢了灵兽也得经验与亲密。灵兽带不进九幽秘境，不发游戏币。

const (
	RoomDen = "den" // 兽苑：洞府里养灵兽的地方

	// LureCooldown 是两次引兽之间要等多久，BeastHatch 是灵兽蛋孵化要多久。
	LureCooldown = 8 * 60 * 60
	BeastHatch   = 4 * 60 * 60
	// BeastFeeds 是每只灵兽每天能喂几次；MaxBond 是亲密的上限，嬉戏一次 bondPlay、喂一次 bondFeed、打赢一场 bondBattle。
	BeastFeeds = 6
	MaxBond    = 100
	bondPlay   = 5
	bondFeed   = 1
	bondBattle = 2
	// MaxBeastLevel 是灵兽的最高等级，beastStageLevels 是每一阶的等级上限(到了要进化)；兽苑每级让等级上限多 denLevelCap。
	MaxBeastLevel = 30
	denLevelCap   = 3
	// MaxBeastName 是灵兽名字最多几个字。
	MaxBeastName = 8
)

// 灵兽的种类，按五行，九尾灵狐稀有。
const (
	BeastTiger  = "baihu" // 白虎(金)：扑击一个敌人
	BeastDeer   = "lu"    // 青鹿(木)：给主角回复生命
	BeastTurtle = "gui"   // 玄龟(水)：给主角护体
	BeastBird   = "que"   // 赤雀(火)：给所有敌人真火
	BeastApe    = "yuan"  // 石猿(土)：重击一个敌人并让它虚弱
	BeastFox    = "hu"    // 九尾灵狐：幻火灼伤所有敌人
)

// BeastKinds 是全部灵兽，按页面上的顺序；前五种按五行(金木水火土)排，下标就是灵根。
var BeastKinds = []string{BeastTiger, BeastDeer, BeastTurtle, BeastBird, BeastApe, BeastFox}

// beastMoves 是每种灵兽在战斗里的招式。
var beastMoves = map[string]string{
	BeastTiger:  dungeon.BeastStrike,
	BeastDeer:   dungeon.BeastHeal,
	BeastTurtle: dungeon.BeastGuard,
	BeastBird:   dungeon.BeastBurn,
	BeastApe:    dungeon.BeastMaul,
	BeastFox:    dungeon.BeastSweep,
}

// beastStages 是每一阶的力道倍数：幼兽、成兽、圣兽。
var beastStages = [...]float64{1, 1.25, 1.6}

// beastStageLevels 是每一阶的等级上限。
var beastStageLevels = [...]int{10, 20, MaxBeastLevel}

// Evolution 是进化到下一阶要花的资源与灵药。
type Evolution struct {
	Cost  Cost   `json:"cost"`
	Rare  string `json:"rare"`
	RareN int    `json:"rare_n"`
}

// Evolutions[s] 是从第 s 阶进化到第 s+1 阶。
var Evolutions = []Evolution{
	{Cost: Cost{Stone: 2000, Herb: 1000}, Rare: RareFruit, RareN: 2},
	{Cost: Cost{Stone: 8000, Herb: 4000, Ore: 2000}, Rare: RareLotus, RareN: 1},
}

// Bait 是一种引兽的饵料：花多少、要不要一份灵药，引来九尾灵狐的几率(%)与资质(1-5)的权重。
type Bait struct {
	Key      string `json:"key"`
	Cost     Cost   `json:"cost"`
	Rare     string `json:"rare,omitempty"`
	FoxPct   int    `json:"fox_pct"`
	Aptitude []int  `json:"aptitude"`
}

// Baits 是全部饵料，按页面上的顺序。
var Baits = []Bait{
	{Key: "plain", Cost: Cost{Stone: 300, Herb: 200}, Aptitude: []int{30, 35, 22, 10, 3}},
	{Key: "fruit", Cost: Cost{Stone: 300, Herb: 200}, Rare: RareFruit, FoxPct: 5, Aptitude: []int{10, 30, 35, 20, 5}},
	{Key: "lotus", Cost: Cost{Stone: 300, Herb: 200}, Rare: RareLotus, FoxPct: 25, Aptitude: []int{0, 10, 40, 35, 15}},
}

// Feed 是一种喂养：花什么、得几点经验。Pill 不为空时喂的是宗门的丹药。
type Feed struct {
	Key  string `json:"key"`
	Cost Cost   `json:"cost"`
	Pill string `json:"pill,omitempty"`
	Exp  int    `json:"exp"`
}

// Feeds 是全部喂养方式。
var Feeds = []Feed{
	{Key: "herb", Cost: Cost{Herb: 100}, Exp: 60},
	{Key: PillQi, Pill: PillQi, Exp: 400},
	{Key: PillPeiyuan, Pill: PillPeiyuan, Exp: 700},
}

// beastBattleExp 是打赢一场灵兽得的经验，按战斗种类，每往后一章多一半。
var beastBattleExp = map[string]int{NodeCombat: 30, NodeElite: 60, NodeBoss: 120}

// Beast 是一只灵兽。Hatch 不为 0 时还是一颗蛋，到时间孵化。FedDay、Fed 记今天喂了几次，Played 是今天嬉戏过的日期。
type Beast struct {
	Id       int     `json:"id"`
	Kind     string  `json:"kind"`
	Name     string  `json:"name,omitempty"`
	Aptitude int     `json:"aptitude"`
	Level    int     `json:"level"`
	Exp      float64 `json:"exp"`
	Stage    int     `json:"stage"`
	Bond     int     `json:"bond"`
	Hatch    int64   `json:"hatch,omitempty"`
	FedDay   string  `json:"fed_day,omitempty"`
	Fed      int     `json:"fed,omitempty"`
	Played   string  `json:"played,omitempty"`
}

// BeastExpNeed 是灵兽从 level 级升到下一级要的经验。
func BeastExpNeed(level int) float64 {
	return math.Round(40 * float64(level) * (1 + float64(level)/10))
}

// BeastSlots 是兽苑某一级能养几只灵兽(蛋也占一个位置)：开辟就有一只，4 级、8 级各多一只。
func BeastSlots(den int) int {
	if den <= 0 {
		return 0
	}
	return 1 + den/4
}

// LevelCap 是这只灵兽现在最多升到几级：本阶的上限与兽苑等级的三倍取小的。
func (b *Beast) LevelCap(den int) int {
	return min(beastStageLevels[b.Stage], denLevelCap*den)
}

// Power 是灵兽出手的力道：种类之外，等级、资质、进化与亲密都让它更强。
func (b *Beast) Power() int {
	if b.Hatch != 0 {
		return 0
	}
	p := (1 + 0.3*float64(b.Level)) * talentMult[b.Aptitude] * beastStages[b.Stage] * (1 + float64(b.Bond)/500)
	return max(int(math.Round(p)), 1)
}

func (h *Hero) beast(id int) *Beast {
	for _, b := range h.Beasts {
		if b.Id == id {
			return b
		}
	}
	return nil
}

// hatchBeasts 孵出到时间的灵兽蛋。
func (h *Hero) hatchBeasts(now int64) []Ev {
	var evs []Ev
	for _, b := range h.Beasts {
		if b.Hatch != 0 && now >= b.Hatch {
			b.Hatch, b.Level = 0, 1
			evs = append(evs, Ev{K: "hero_beast_hatch", S: b.Kind, N: b.Id})
		}
	}
	return evs
}

// lure 付饵料引来一颗灵兽蛋：五行灵兽机会相同，好饵料有几率引来九尾灵狐，资质按饵料的权重。
func (h *Hero) lure(s *Sect, key string, now int64) ([]Ev, error) {
	index := slices.IndexFunc(Baits, func(b Bait) bool { return b.Key == key })
	if index < 0 {
		return nil, ErrAction
	}
	bait := Baits[index]
	if len(h.Beasts) >= BeastSlots(h.Cave[RoomDen]) || h.LureReady > now {
		return nil, ErrUnavailable
	}
	if bait.Rare != "" && s.Rare[bait.Rare] < 1 {
		return nil, ErrCost
	}
	if err := s.pay(bait.Cost); err != nil {
		return nil, err
	}
	evs := spentEvs(bait.Cost)
	if bait.Rare != "" {
		s.Rare[bait.Rare]--
		evs = append(evs, Ev{K: "rare", S: bait.Rare, N: -1})
	}
	kind := BeastKinds[s.Rng.Intn(Elements)]
	if s.Rng.Chance(bait.FoxPct) {
		kind = BeastFox
	}
	h.NextBeast++
	b := &Beast{Id: h.NextBeast, Kind: kind, Aptitude: 1 + pickWeighted(&s.Rng, bait.Aptitude), Hatch: now + BeastHatch}
	h.Beasts = append(h.Beasts, b)
	h.LureReady = now + LureCooldown
	return append(evs, Ev{K: "hero_beast_egg", S: kind, N: b.Aptitude}), nil
}

// beastGainExp 给灵兽加经验并自动升级，到了等级上限就不再长(多出来的经验不留)。
func (h *Hero) beastGainExp(b *Beast, gain float64) []Ev {
	limit := b.LevelCap(h.Cave[RoomDen])
	if gain <= 0 || b.Hatch != 0 || b.Level >= limit {
		return nil
	}
	b.Exp += gain
	levels := 0
	for b.Level < limit && b.Exp >= BeastExpNeed(b.Level) {
		b.Exp -= BeastExpNeed(b.Level)
		b.Level++
		levels++
	}
	if b.Level >= limit {
		b.Exp = 0
	}
	if levels == 0 {
		return nil
	}
	return []Ev{{K: "hero_beast_level", S: b.Kind, N: b.Level, V: b.Id}}
}

// hatched 返回一只已经孵出来的灵兽。
func (h *Hero) hatched(id int) (*Beast, error) {
	b := h.beast(id)
	if b == nil {
		return nil, ErrAction
	}
	if b.Hatch != 0 {
		return nil, ErrUnavailable
	}
	return b, nil
}

// feedBeast 喂一只灵兽：灵草花宗门的灵草，丹药用宗门丹房的。每天每只喂 BeastFeeds 次，到了等级上限喂不进去。
func (h *Hero) feedBeast(s *Sect, id int, key, day string) ([]Ev, error) {
	index := slices.IndexFunc(Feeds, func(f Feed) bool { return f.Key == key })
	if index < 0 {
		return nil, ErrAction
	}
	feed := Feeds[index]
	b, err := h.hatched(id)
	if err != nil {
		return nil, err
	}
	if b.FedDay != day {
		b.FedDay, b.Fed = day, 0
	}
	if b.Fed >= BeastFeeds || b.Level >= b.LevelCap(h.Cave[RoomDen]) {
		return nil, ErrUnavailable
	}
	var evs []Ev
	if feed.Pill != "" {
		if s.Pills[feed.Pill] < 1 {
			return nil, ErrCost
		}
		s.Pills[feed.Pill]--
		evs = append(evs, Ev{K: "pill", S: feed.Pill, N: -1})
	} else {
		if err := s.pay(feed.Cost); err != nil {
			return nil, err
		}
		evs = append(evs, spentEvs(feed.Cost)...)
	}
	b.Fed++
	b.Bond = min(b.Bond+bondFeed, MaxBond)
	evs = append(evs, Ev{K: "hero_beast_fed", S: b.Kind, N: feed.Exp, V: b.Id})
	return append(evs, h.beastGainExp(b, float64(feed.Exp))...), nil
}

// playBeast 和灵兽嬉戏，每天一次，涨亲密。
func (h *Hero) playBeast(id int, day string) ([]Ev, error) {
	b, err := h.hatched(id)
	if err != nil {
		return nil, err
	}
	if b.Played == day || b.Bond >= MaxBond {
		return nil, ErrUnavailable
	}
	b.Played = day
	b.Bond = min(b.Bond+bondPlay, MaxBond)
	return []Ev{{K: "hero_beast_play", S: b.Kind, N: b.Bond, V: b.Id}}, nil
}

// evolveBeast 让到了本阶等级上限的灵兽进化到下一阶。
func (h *Hero) evolveBeast(s *Sect, id int) ([]Ev, error) {
	b, err := h.hatched(id)
	if err != nil {
		return nil, err
	}
	if b.Stage >= len(Evolutions) || b.Level < beastStageLevels[b.Stage] {
		return nil, ErrUnavailable
	}
	evo := Evolutions[b.Stage]
	if s.Rare[evo.Rare] < evo.RareN {
		return nil, ErrCost
	}
	if err := s.pay(evo.Cost); err != nil {
		return nil, err
	}
	s.Rare[evo.Rare] -= evo.RareN
	b.Stage++
	evs := append(spentEvs(evo.Cost), Ev{K: "rare", S: evo.Rare, N: -evo.RareN})
	return append(evs, Ev{K: "hero_beast_evolve", S: b.Kind, N: b.Stage, V: b.Id}), nil
}

// summonBeast 让一只孵出来的灵兽出战(id 为 0 时谁也不带)。
func (h *Hero) summonBeast(id int) error {
	if id == 0 {
		h.BeastOut = 0
		return nil
	}
	if _, err := h.hatched(id); err != nil {
		return err
	}
	h.BeastOut = id
	return nil
}

// renameBeast 给灵兽起名，空名字用回种类的名字。
func (h *Hero) renameBeast(id int, name string) error {
	b := h.beast(id)
	if b == nil {
		return ErrAction
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxBeastName || strings.ContainsAny(name, "\r\n\t") {
		return ErrAction
	}
	b.Name = name
	return nil
}

// releaseBeast 放生一只灵兽(或一颗蛋)，给它腾出位置；宗门得到一点声望。
func (h *Hero) releaseBeast(s *Sect, id int) ([]Ev, error) {
	b := h.beast(id)
	if b == nil {
		return nil, ErrAction
	}
	h.Beasts = slices.DeleteFunc(h.Beasts, func(other *Beast) bool { return other.Id == id })
	if h.BeastOut == id {
		h.BeastOut = 0
	}
	evs := []Ev{{K: "hero_beast_release", S: b.Kind}}
	return append(evs, s.gain(Cost{Rep: b.Level})...), nil
}

// battleBeast 是这一场出战的灵兽，没有时为 nil。
func (h *Hero) battleBeast() *dungeon.BattleBeast {
	b := h.beast(h.BeastOut)
	if b == nil || b.Hatch != 0 {
		return nil
	}
	return &dungeon.BattleBeast{Kind: b.Kind, Move: beastMoves[b.Kind], Power: min(b.Power(), dungeon.BattleMaxBeastPower)}
}

// rewardBeast 打赢以后出战的灵兽得经验与亲密：按战斗种类，每往后一章多一半(问心阶按最后一章)。
func (h *Hero) rewardBeast(id int, kind string, chapter int) []Ev {
	b := h.beast(id)
	if b == nil || b.Hatch != 0 {
		return nil
	}
	b.Bond = min(b.Bond+bondBattle, MaxBond)
	gain := float64(beastBattleExp[kind]) * (1 + 0.5*float64(chapter))
	return h.beastGainExp(b, gain)
}

// BeastRules 是灵兽的规则，页面按它显示饵料、喂养、进化与等级。
type BeastRules struct {
	Kinds        []string    `json:"kinds"`
	Baits        []Bait      `json:"baits"`
	Feeds        []Feed      `json:"feeds"`
	Evolutions   []Evolution `json:"evolutions"`
	StageLevels  []int       `json:"stage_levels"`
	ExpNeed      []float64   `json:"exp_need"`
	Slots        []int       `json:"slots"`
	DenLevelCap  int         `json:"den_level_cap"`
	FeedsPerDay  int         `json:"feeds_per_day"`
	MaxBond      int         `json:"max_bond"`
	BondPlay     int         `json:"bond_play"`
	LureCooldown int         `json:"lure_cooldown"`
	Hatch        int         `json:"hatch"`
	MaxName      int         `json:"max_name"`
}

// BeastRulesOf 返回灵兽的规则：ExpNeed[L] 是从 L 级升一级要的经验，Slots[L] 是兽苑 L 级能养几只。
func BeastRulesOf() BeastRules {
	rules := BeastRules{Kinds: BeastKinds, Baits: Baits, Feeds: Feeds, Evolutions: Evolutions,
		StageLevels: beastStageLevels[:], DenLevelCap: denLevelCap, FeedsPerDay: BeastFeeds, MaxBond: MaxBond,
		BondPlay: bondPlay, LureCooldown: LureCooldown, Hatch: BeastHatch, MaxName: MaxBeastName}
	for level := 0; level <= MaxBeastLevel; level++ {
		rules.ExpNeed = append(rules.ExpNeed, BeastExpNeed(level))
	}
	for level := 0; level <= MaxRoomLevel; level++ {
		rules.Slots = append(rules.Slots, BeastSlots(level))
	}
	return rules
}
