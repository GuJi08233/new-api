package wendao

import "github.com/QuantumNous/new-api/pkg/dungeon"

// 身份：主角是散修，还是拜入了哪个正道宗门的弟子，还是自己开宗立派的掌门。
//
// 主角的灵石、丹药这些放在 Save.Sect 里：散修和宗门弟子的 Sect 只是储物处(没有主殿、没有弟子，IsFounded 为假，宗门经营的
// 操作都不能做)；开宗立派就是在这个储物处上建起主殿、收下两名弟子，之后与原来的宗门经营一样。拜入的宗门、身份与贡献记在
// 主角身上(Hero.Path)。
//
// 宗门弟子：每天领一次俸禄(按身份)；上交灵石、灵草、玄铁或领每日功课攒贡献；贡献在宗门宝库换丹药与灵药；累计贡献与境界够了
// 自动晋升(外门 → 内门 → 真传 → 长老)。退出宗门身份与贡献清零，七天内不能再拜入。每个宗门另有一样门派加成。

const (
	// RankOuter…RankElder 是宗门弟子的身份。
	RankOuter = 0
	RankInner = 1
	RankHeir  = 2
	RankElder = 3
	// PathRejoinWait 是退出宗门以后多久才能再拜入。
	PathRejoinWait = 7 * 24 * 3600
	// FoundRealm、FoundCost 是开宗立派的条件：主角到金丹，储物处里有这么多灵石(开宗时花掉)。
	FoundRealm = 2
	FoundCost  = 1000
	// PathDailyMerit 是宗门弟子每领一件每日功课多得的贡献。
	PathDailyMerit = 20
	// 门派加成：力量、生命、闭关、炼丹。
	PerkStrength = "strength"
	PerkVitality = "vitality"
	PerkInsight  = "insight"
	PerkAlchemy  = "alchemy"
	// perkHP、perkInsight、perkAlchemy 是生命、闭关、炼丹加成的数值；力量加成是 1。
	perkHP      = 15
	perkInsight = 0.2
	perkAlchemy = 15
)

// SectGoods 是宗门宝库里的一样东西：Id 是丹药(pill_ 前缀)或灵药(rare_ 前缀)，Rank 是要到的身份。
type SectGoods struct {
	Id    string `json:"id"`
	Merit int    `json:"merit"`
	Rank  int    `json:"rank"`
}

// NpcSect 是能拜入的正道宗门：Style 是擅长的流派(空为丹道)，Perk 是门派加成。
type NpcSect struct {
	Id    string      `json:"id"`
	Style string      `json:"style"`
	Perk  string      `json:"perk"`
	Goods []SectGoods `json:"goods"`
}

// commonGoods 是每个宗门宝库都有的；各宗另有一两样拿手的。
var commonGoods = []SectGoods{
	{Id: "pill_" + PillHeal, Merit: 20},
	{Id: "pill_" + PillQi, Merit: 60},
	{Id: "pill_" + PillBreak, Merit: 150, Rank: RankInner},
	{Id: "rare_" + RareFruit, Merit: 120, Rank: RankInner},
}

var NpcSects = []NpcSect{
	{Id: "tianjian", Style: dungeon.ClassRanger, Perk: PerkStrength, Goods: append(append([]SectGoods(nil), commonGoods...),
		SectGoods{Id: "rare_" + RareLingzhi, Merit: 300, Rank: RankHeir})},
	{Id: "jingang", Style: dungeon.ClassWarrior, Perk: PerkVitality, Goods: append(append([]SectGoods(nil), commonGoods...),
		SectGoods{Id: "pill_" + PillMarrow, Merit: 400, Rank: RankHeir})},
	{Id: "taixu", Style: dungeon.ClassMage, Perk: PerkInsight, Goods: append(append([]SectGoods(nil), commonGoods...),
		SectGoods{Id: "pill_" + PillPeiyuan, Merit: 120, Rank: RankInner})},
	{Id: "wanhua", Perk: PerkAlchemy, Goods: append(append([]SectGoods(nil), commonGoods...),
		SectGoods{Id: "rare_" + RareLingzhi, Merit: 260, Rank: RankInner}, SectGoods{Id: "rare_" + RareLotus, Merit: 600, Rank: RankElder})},
}

// RankMerit、RankRealm 是晋升到各身份要的累计贡献与主角境界；RankStipend 是各身份每天的俸禄。
var (
	RankMerit   = [...]int{0, 300, 1200, 4000}
	RankRealm   = [...]int{0, 1, 2, 3}
	RankStipend = [...]Cost{{Stone: 40}, {Stone: 80, Herb: 20}, {Stone: 140, Herb: 40, Ore: 10}, {Stone: 220, Herb: 60, Ore: 30}}
)

// DonateRate 是上交多少换 1 点贡献：灵石 20、灵草 10、玄铁 5。
var DonateRate = Cost{Stone: 20, Herb: 10, Ore: 5}

// PerkValues 是各门派加成的数值，页面拿来写说明：力量、生命加多少，闭关快几成，炼丹品质加多少。
var PerkValues = map[string]float64{PerkStrength: 1, PerkVitality: perkHP, PerkInsight: perkInsight, PerkAlchemy: perkAlchemy}

// Path 是主角的宗门身份：Sect 为空是散修(或自立门户的掌门，看 Save.Identity)；Left 是上一次退出宗门的时间。
type Path struct {
	Sect    string `json:"sect,omitempty"`
	Rank    int    `json:"rank,omitempty"`
	Merit   int    `json:"merit,omitempty"`
	Earned  int    `json:"earned,omitempty"`
	Joined  int64  `json:"joined,omitempty"`
	Left    int64  `json:"left,omitempty"`
	Stipend string `json:"stipend,omitempty"`
}

// PathView 是身份页要的：身份、宗门身份、主角的境界(晋升与开宗都看境界)，以及 day 这天的俸禄还能不能领。
type PathView struct {
	Identity     string `json:"identity"`
	Path         *Path  `json:"path,omitempty"`
	Realm        int    `json:"realm"`
	StipendReady bool   `json:"stipend_ready"`
}

// PathView 是主角现在的身份，还没有主角时身份为空。
func (sv *Save) PathView(day string) PathView {
	v := PathView{Identity: sv.Identity()}
	if sv.Hero != nil {
		v.Path, v.Realm = sv.Hero.Path, sv.Hero.Realm
	}
	v.StipendReady = v.Identity == "disciple" && v.Path.Stipend != day
	return v
}

// NpcSectOf 按标识找宗门。
func NpcSectOf(id string) (*NpcSect, bool) {
	for i := range NpcSects {
		if NpcSects[i].Id == id {
			return &NpcSects[i], true
		}
	}
	return nil, false
}

// IsFounded 表示这是开过宗的宗门(有主殿)，不是散修与宗门弟子的储物处。
func (s *Sect) IsFounded() bool { return s.Buildings[BuildingHall] > 0 }

// NewStorehouse 是新主角的储物处：一些灵石灵草与两颗回春丹，没有主殿、没有弟子、没有灵田。
func NewStorehouse(seed uint64, now int64) *Sect {
	s := &Sect{
		Version: StateVersion, Rng: Rng{S: seed}, Created: now, Last: now, Buildings: map[string]int{},
		Res: Cost{Stone: 200, Herb: 100}, Pills: map[string]int{PillHeal: 2}, NextId: 1, Missions: []*Mission{},
		NextEventAt: now, Log: []Ev{}, Items: map[string]int{}, Rare: map[string]int{},
	}
	s.normalizeGarden()
	return s
}

// Identity 是主角现在的身份：master 掌门(开了宗)、disciple 宗门弟子、loose 散修；还没有主角时为空。
func (sv *Save) Identity() string {
	if sv.Hero == nil || sv.Sect == nil {
		return ""
	}
	return heroIdentity(sv.Hero, sv.Sect)
}

// npcPerk 是主角现在的门派加成，不在宗门里时为空。
func (h *Hero) npcPerk() string {
	if h.Path == nil {
		return ""
	}
	if sect, ok := NpcSectOf(h.Path.Sect); ok {
		return sect.Perk
	}
	return ""
}

// JoinSect 散修拜入一个正道宗门，从外门弟子做起。退出宗门七天内不能再拜入；开了宗的掌门不能拜入别家。
func (sv *Save) JoinSect(id string, now int64) error {
	if _, ok := NpcSectOf(id); !ok {
		return ErrAction
	}
	if sv.Identity() != "loose" {
		return ErrUnavailable
	}
	h := sv.Hero
	if h.Path != nil && h.Path.Left > 0 && now-h.Path.Left < PathRejoinWait {
		return ErrUnavailable
	}
	left := int64(0)
	if h.Path != nil {
		left = h.Path.Left
	}
	h.Path = &Path{Sect: id, Joined: now, Left: left}
	return nil
}

// LeaveSect 退出宗门：身份与贡献清零，记下退出的时间。
func (sv *Save) LeaveSect(now int64) error {
	if sv.Identity() != "disciple" {
		return ErrUnavailable
	}
	sv.Hero.Path = &Path{Left: now}
	return nil
}

// FoundSect 开宗立派：散修(不在任何宗门里)、主角到金丹、储物处里灵石够，花掉 FoundCost 灵石，建起主殿、灵田、聚灵阵，
// 收下两名弟子；之后就是自己的宗门。
func (sv *Save) FoundSect(now int64) ([]Ev, error) {
	if sv.Identity() != "loose" || sv.Hero.Realm < FoundRealm {
		return nil, ErrUnavailable
	}
	s := sv.Sect
	s.Tick(now)
	if err := s.pay(Cost{Stone: FoundCost}); err != nil {
		return nil, err
	}
	for building, level := range startBuildings {
		s.Buildings[building] = level
	}
	s.normalizeGarden()
	for _, talent := range []int{3, 2} {
		c := s.newCandidate()
		c.Talent, c.Layer = talent, 1
		s.addDisciple(c, now)
	}
	s.refreshRecruits(now)
	s.NextEventAt = now + 30*60
	evs := []Ev{{K: "res", S: "stone", N: -FoundCost}, {K: "path_found"}}
	s.record(now, evs)
	return evs, nil
}

// ClaimStipend 宗门弟子每天领一次俸禄，按现在的身份。
func (sv *Save) ClaimStipend(day string) ([]Ev, error) {
	if sv.Identity() != "disciple" {
		return nil, ErrUnavailable
	}
	p := sv.Hero.Path
	if p.Stipend == day {
		return nil, ErrUnavailable
	}
	p.Stipend = day
	return append([]Ev{{K: "path_stipend", N: p.Rank}}, sv.Sect.gain(RankStipend[p.Rank])...), nil
}

// Donate 宗门弟子上交灵石、灵草、玄铁换贡献：按 DonateRate 折算，零头不收。
func (sv *Save) Donate(c Cost) ([]Ev, error) {
	if sv.Identity() != "disciple" || c.Stone < 0 || c.Herb < 0 || c.Ore < 0 {
		return nil, ErrAction
	}
	merit := c.Stone/DonateRate.Stone + c.Herb/DonateRate.Herb + c.Ore/DonateRate.Ore
	if merit <= 0 {
		return nil, ErrAction
	}
	spent := Cost{Stone: c.Stone / DonateRate.Stone * DonateRate.Stone, Herb: c.Herb / DonateRate.Herb * DonateRate.Herb,
		Ore: c.Ore / DonateRate.Ore * DonateRate.Ore}
	if err := sv.Sect.pay(spent); err != nil {
		return nil, err
	}
	return append(costEvents(spent), sv.GrantMerit(merit)...), nil
}

// GrantMerit 给宗门弟子记贡献，顺带按累计贡献与境界晋升(可以连升几级)；不是宗门弟子时什么都不做。
func (sv *Save) GrantMerit(n int) []Ev {
	if sv.Identity() != "disciple" || n <= 0 {
		return nil
	}
	p := sv.Hero.Path
	p.Merit += n
	p.Earned += n
	evs := []Ev{{K: "path_merit", N: n}}
	return append(evs, sv.promote()...)
}

// promote 按累计贡献与境界升到够得上的最高身份。
func (sv *Save) promote() []Ev {
	p := sv.Hero.Path
	var evs []Ev
	for p.Rank+1 < len(RankMerit) && p.Earned >= RankMerit[p.Rank+1] && sv.Hero.Realm >= RankRealm[p.Rank+1] {
		p.Rank++
		evs = append(evs, Ev{K: "path_rank", N: p.Rank})
	}
	return evs
}

// Exchange 用贡献换宗门宝库里的一样东西，身份不够的换不了。
func (sv *Save) Exchange(id string) ([]Ev, error) {
	if sv.Identity() != "disciple" {
		return nil, ErrUnavailable
	}
	p := sv.Hero.Path
	sect, _ := NpcSectOf(p.Sect)
	for _, g := range sect.Goods {
		if g.Id != id {
			continue
		}
		if p.Rank < g.Rank {
			return nil, ErrUnavailable
		}
		if p.Merit < g.Merit {
			return nil, ErrCost
		}
		s := sv.Sect
		switch {
		case len(id) > 5 && id[:5] == "pill_":
			s.Pills[id[5:]]++
		case len(id) > 5 && id[:5] == "rare_":
			s.Rare[id[5:]]++
		default:
			return nil, ErrAction
		}
		p.Merit -= g.Merit
		return []Ev{{K: "path_exchange", S: id, N: g.Merit}}, nil
	}
	return nil, ErrAction
}

// costEvents 是花掉一笔资源的事件(每种一条，负数)。
func costEvents(c Cost) []Ev {
	var evs []Ev
	for _, part := range []struct {
		key string
		n   int
	}{{"stone", c.Stone}, {"herb", c.Herb}, {"ore", c.Ore}} {
		if part.n > 0 {
			evs = append(evs, Ev{K: "res", S: part.key, N: -part.n})
		}
	}
	return evs
}
