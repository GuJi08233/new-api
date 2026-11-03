package wendao

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/doufa"
)

// 斗法契约：世界上要打的斗法都是契约——入门试炼(拜入正道宗门要先过的一场)、宗门任务(宗门弟子每天的差事，给贡献)、悬赏
// (悬赏榜上的活儿，给资源)。契约由服务端按地域与难度排好敌人，发给浏览器去打，打完交操作记录，服务端按种子与开局重放核对，
// 只认重放出来的输赢(规则见 pkg/doufa)。发的是宗门资源与贡献，不是游戏币，所以不进对局表，状态记在问道存档的 Duels 里。
//
// 一次只能有一场进行中(Duels.Active)。接下契约的那一刻就算用掉一次挑战：打赢、打输、放弃(刷新、换设备后回来只能放弃，
// 因为种子只在接下的回应里给一次)都用掉它，所以这里没有"重来"，刷新换不来第二次机会。每张悬赏与任务每天两次，每个宗门的
// 入门试炼每天五次；第二天榜单整个换一批。

const (
	DuelTrial  = "trial"
	DuelTask   = "task"
	DuelBounty = "bounty"
	// BountySlots、TaskSlots 是悬赏榜与宗门每天发几张；ContractTries 是每张最多挑战几次，TrialTries 是每个宗门的入门试炼
	// 每天最多几次。
	BountySlots   = 3
	TaskSlots     = 3
	ContractTries = 2
	TrialTries    = 5
	// MaxTier 是契约难度的最高档：一清剿、二讨伐、三斩首。
	MaxTier = 3
)

// Region 是世界地图上的一个地域：第 i 个地域在主角到第 i 个境界时开放，对应第 i+1 章。Sect 是山门在这里的正道宗门，
// Rooms、Foes、Elite、Boss 是这里的契约用的房间模板与敌人(都是 pkg/doufa 里有的，后面的地域暂时沿用前面的敌人，换上自己的
// 敌人表以后只改这里)。
type Region struct {
	Id    string   `json:"id"`
	Sect  string   `json:"sect,omitempty"`
	Rooms []string `json:"-"`
	Foes  []string `json:"-"`
	Elite string   `json:"-"`
	Boss  string   `json:"-"`
}

// Regions 是五个地域：青岚山、赤霞原、北冥寒渊、万妖林、九幽，与剧情的五章一一对应。
var Regions = [...]Region{
	{Id: "qinglan", Sect: "tianjian", Rooms: []string{"glade", "ruins"}, Foes: []string{"slime", "skeleton", "moth", "goblin", "gargoyle"},
		Elite: "goblin_chief", Boss: "slime_king"},
	{Id: "chixia", Sect: "jingang", Rooms: []string{"cavern", "ruins"}, Foes: []string{"werewolf", "shade_mage", "lava_elemental", "beetle", "goblin"},
		Elite: "troll", Boss: "flame_lord"},
	{Id: "beiming", Sect: "taixu", Rooms: []string{"ruins", "glade"}, Foes: []string{"skeleton", "shade_mage", "gargoyle", "beetle", "werewolf"},
		Elite: "troll", Boss: "slime_king"},
	{Id: "wanyao", Sect: "wanhua", Rooms: []string{"glade", "cavern"}, Foes: []string{"werewolf", "moth", "goblin", "slime", "beetle"},
		Elite: "goblin_chief", Boss: "flame_lord"},
	{Id: "jiuyou", Rooms: []string{"ruins", "cavern"}, Foes: []string{"shade_mage", "skeleton", "lava_elemental", "gargoyle", "werewolf"},
		Elite: "troll", Boss: "flame_lord"},
}

// trialWaves 是各宗门的入门试炼：一间房、两波敌人，按各宗的路数挑的敌人。敌人强度按第一个地域的一档算。
var trialWaves = map[string][][]doufa.Group{
	"tianjian": {{{Foe: "moth", N: 3}}, {{Foe: "goblin", N: 3}, {Foe: "moth", N: 2}}},
	"jingang":  {{{Foe: "slime", N: 4}}, {{Foe: "gargoyle", N: 2}, {Foe: "skeleton", N: 2}}},
	"taixu":    {{{Foe: "skeleton", N: 3}}, {{Foe: "moth", N: 3}, {Foe: "skeleton", N: 2}}},
	"wanhua":   {{{Foe: "slime", N: 2}, {Foe: "moth", N: 2}}, {{Foe: "goblin_chief", N: 1}, {Foe: "goblin", N: 2}}},
}

// Contract 是榜上的一张悬赏或宗门任务。Region 是地域的下标，Tier 是难度档(一清剿、二讨伐、三斩首)，Tries 是还剩几次挑战，
// Done 为真是已经完成。Reward 是宗门资源，Merit 是宗门任务给的贡献。
type Contract struct {
	Id     string `json:"id"`
	Kind   string `json:"kind"`
	Region int    `json:"region"`
	Tier   int    `json:"tier"`
	Tries  int    `json:"tries"`
	Done   bool   `json:"done,omitempty"`
	Reward Cost   `json:"reward"`
	Merit  int    `json:"merit,omitempty"`
}

// Duel 是进行中的一场：Kind、Id 指哪张契约(入门试炼的 Id 是宗门)，Seed 与 Setup 是发给浏览器的种子与开局，Issued 是发出的
// 时间(秒)，提交时不能早于"帧数 ÷ 60"秒的八成。
type Duel struct {
	Kind   string      `json:"kind"`
	Id     string      `json:"id"`
	Seed   uint32      `json:"seed"`
	Setup  doufa.Setup `json:"setup"`
	Issued int64       `json:"issued"`
}

// DuelBook 是主角手上的斗法契约：今天的悬赏榜与宗门任务、每个宗门的入门试炼今天用了几次、进行中的一场。Rng 是榜单的随机数。
type DuelBook struct {
	Day      string         `json:"day"`
	Rng      uint32         `json:"rng"`
	Bounties []*Contract    `json:"bounties,omitempty"`
	Tasks    []*Contract    `json:"tasks,omitempty"`
	Trials   map[string]int `json:"trials,omitempty"`
	Active   *Duel          `json:"active,omitempty"`
}

func (b *DuelBook) rand(n int) int {
	s := b.Rng
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	b.Rng = s
	return int(s % uint32(n))
}

// book 是主角手上的契约，没有的话先建一份空的。
func (sv *Save) book() *DuelBook {
	if sv.Duels == nil {
		sv.Duels = &DuelBook{}
	}
	return sv.Duels
}

// RegionOpen 表示主角的境界够得着第 i 个地域。
func (h *Hero) RegionOpen(i int) bool { return i >= 0 && i < len(Regions) && h.Realm >= i }

// RollDuels 把契约换到 day 这一天：整个榜单换一批，入门试炼的次数清零，昨天没打完的一场作废。没有主角、已经是今天时什么都不做。
// 榜单由存档里的随机数决定，没写回存档时再算一遍结果相同，所以只读的接口可以在一份副本上先算。
func (sv *Save) RollDuels(day string) {
	b := sv.book()
	if sv.Hero == nil || b.Day == day {
		return
	}
	if b.Rng == 0 {
		b.Rng = 2463534242
		if sv.Sect != nil {
			b.Rng ^= uint32(sv.Sect.Created)
		}
		b.Rng = max(b.Rng, 1)
		// 种子里只有几位是 1 的时候，异或移位的头几个数相差不大，先空转几下。
		for range 16 {
			b.rand(1)
		}
	}
	b.Day, b.Trials, b.Active = day, nil, nil
	b.Bounties, b.Tasks = nil, nil
	for i := 1; i <= BountySlots; i++ {
		b.Bounties = append(b.Bounties, sv.newContract(DuelBounty, fmt.Sprintf("b%d", i)))
	}
	for i := 1; i <= TaskSlots; i++ {
		b.Tasks = append(b.Tasks, sv.newContract(DuelTask, fmt.Sprintf("t%d", i)))
	}
}

// newContract 排一张契约：地域取最远开放的那个或再近一个，难度随机；报酬按地域与难度涨。
func (sv *Save) newContract(kind, id string) *Contract {
	b := sv.book()
	realm := sv.Hero.Realm
	region := realm - b.rand(min(realm+1, 2))
	tier := 1 + b.rand(MaxTier)
	c := &Contract{Id: id, Kind: kind, Region: region, Tier: tier, Tries: ContractTries}
	size := (region + 1) * tier
	if kind == DuelBounty {
		c.Reward = Cost{Stone: 20 * size, Herb: 8 * size, Ore: 4 * size}
	} else {
		c.Merit = 30 * tier
		c.Reward = Cost{Stone: 10 * size}
	}
	return c
}

// contract 找 kind 这一类里编号为 id 的契约。
func (b *DuelBook) contract(kind, id string) *Contract {
	list := b.Bounties
	if kind == DuelTask {
		list = b.Tasks
	}
	for _, c := range list {
		if c.Id == id {
			return c
		}
	}
	return nil
}

// StartDuel 接下一张契约：用掉一次挑战，排好敌人，发出种子与开局。接下之前要没有进行中的一场；入门试炼要是散修、没在退出
// 宗门以后的等待里；宗门任务要是宗门弟子；悬赏要主角够得着那个地域。seed 是这一场的随机数种子，由调用方取安全随机数。
func (sv *Save) StartDuel(kind, id string, seed uint32, now int64) (*Duel, error) {
	b := sv.book()
	if sv.Hero == nil || b.Active != nil {
		return nil, ErrUnavailable
	}
	var contract *Contract
	var setup doufa.Setup
	switch kind {
	case DuelTrial:
		if _, ok := NpcSectOf(id); !ok {
			return nil, ErrAction
		}
		h := sv.Hero
		if sv.Identity() != "loose" || (h.Path != nil && h.Path.Left > 0 && now-h.Path.Left < PathRejoinWait) || b.Trials[id] >= TrialTries {
			return nil, ErrUnavailable
		}
		setup = sv.Hero.duelSetup(doufa.Setup{Room: Regions[0].Rooms[0], Waves: trialWaves[id], Scale: doufa.FoeScale{HP: 100, Dmg: 100}})
	case DuelTask, DuelBounty:
		if contract = b.contract(kind, id); contract == nil {
			return nil, ErrAction
		}
		if contract.Done || contract.Tries <= 0 || !sv.Hero.RegionOpen(contract.Region) || (kind == DuelTask && sv.Identity() != "disciple") {
			return nil, ErrUnavailable
		}
		setup = sv.Hero.duelSetup(b.contractSetup(contract))
	default:
		return nil, ErrAction
	}
	if contract != nil {
		contract.Tries--
	} else {
		if b.Trials == nil {
			b.Trials = map[string]int{}
		}
		b.Trials[id]++
	}
	b.Active = &Duel{Kind: kind, Id: id, Seed: seed, Setup: setup, Issued: now}
	return b.Active, nil
}

// duelSetup 补上主角这一边：流派、配置的法宝与法术(没换过法术时不写，用流派自己的)，以及境界给的生命与伤害；敌人与房间是
// base 里排好的。
func (h *Hero) duelSetup(base doufa.Setup) doufa.Setup {
	base.Class = h.Class
	loadout := h.Loadout()
	base.Weapons = loadout.Weapons
	if loadout.Skill != doufa.ClassSkill(h.Class) {
		base.Skill = loadout.Skill
	}
	base.Mods = doufa.Mods{Vigor: duelRealmVigor * h.Realm, Damage: duelRealmDamage * h.Realm}
	return base
}

// 境界在斗法里给的加成：每个境界生命上限与伤害各加这么多(百分点)，只在契约里生效，带不进发游戏币的九幽秘境。
const (
	duelRealmVigor  = 12
	duelRealmDamage = 4
)

// contractSetup 排一张悬赏或任务的敌人与房间：一档清剿(两波)、二档讨伐(三波，末一波多一只精英)、三档斩首(两波加首领)，
// 每波三四只，敌人的生命与伤害随地域与难度涨。
func (b *DuelBook) contractSetup(c *Contract) doufa.Setup {
	region := &Regions[c.Region]
	waves := 2
	if c.Tier == 2 {
		waves = 3
	}
	var out [][]doufa.Group
	for w := 0; w < waves; w++ {
		counts := make([]int, len(region.Foes))
		for k := 3 + b.rand(2); k > 0; k-- {
			counts[b.rand(len(region.Foes))]++
		}
		var wave []doufa.Group
		for i, foe := range region.Foes {
			if counts[i] > 0 {
				wave = append(wave, doufa.Group{Foe: foe, N: counts[i]})
			}
		}
		if c.Tier == 2 && w == waves-1 {
			wave = append(wave, doufa.Group{Foe: region.Elite, N: 1})
		}
		out = append(out, wave)
	}
	room := region.Rooms[b.rand(len(region.Rooms))]
	if c.Tier == MaxTier {
		out = append(out, []doufa.Group{{Foe: region.Boss, N: 1}})
		room = region.Rooms[0]
	}
	return doufa.Setup{Room: room, Waves: out, Scale: doufa.FoeScale{HP: 90 + 12*c.Tier + 8*c.Region, Dmg: 85 + 8*c.Tier + 5*c.Region}}
}

// FinishDuel 记下打完的这一场(f 是服务端重放出来的结果)：这一场不管输赢都结束；赢了按种类给东西——入门试炼拜入那个宗门，
// 宗门任务记贡献并给一点灵石，悬赏给宗门资源，那张契约算完成。
func (sv *Save) FinishDuel(f *doufa.Fight, now int64) ([]Ev, error) {
	b := sv.book()
	duel := b.Active
	if duel == nil || !f.Over {
		return nil, ErrUnavailable
	}
	b.Active = nil
	if !f.Won {
		return []Ev{{K: "duel_lost", S: duel.Kind}}, nil
	}
	evs := []Ev{{K: "duel_won", S: duel.Kind}}
	if duel.Kind == DuelTrial {
		if err := sv.JoinSect(duel.Id, now); err != nil {
			return nil, err
		}
		return append(evs, Ev{K: "path_join", S: duel.Id}), nil
	}
	c := b.contract(duel.Kind, duel.Id)
	if c == nil || c.Done {
		return nil, ErrUnavailable
	}
	c.Done = true
	evs = append(evs, sv.Sect.gain(c.Reward)...)
	return append(evs, sv.GrantMerit(c.Merit)...), nil
}

// GiveUpDuel 放弃进行中的一场：用掉的那次挑战不退。
func (sv *Save) GiveUpDuel() error {
	b := sv.book()
	if b.Active == nil {
		return ErrUnavailable
	}
	b.Active = nil
	return nil
}

// DuelView 是契约在页面上的样子：今天的悬赏、宗门任务(宗门弟子才有)、每个宗门的入门试炼用了几次、进行中的是哪一张(不带
// 种子与开局：只有接下契约的那次回应里有)，每个地域主角够不够得着，以及出战的法宝与法术。
type DuelView struct {
	Day      string         `json:"day"`
	Bounties []*Contract    `json:"bounties"`
	Tasks    []*Contract    `json:"tasks"`
	Trials   map[string]int `json:"trials"`
	Active   *ActiveDuel    `json:"active,omitempty"`
	Open     []bool         `json:"open"`
	Arms     ArmsView       `json:"arms"`
}

// ActiveDuel 是进行中那一场的概况。
type ActiveDuel struct {
	Kind string `json:"kind"`
	Id   string `json:"id"`
}

// DuelView 是主角现在的契约；没有主角时为 nil。
func (sv *Save) DuelView() *DuelView {
	if sv.Hero == nil {
		return nil
	}
	b := sv.book()
	v := &DuelView{Day: b.Day, Bounties: b.Bounties, Tasks: []*Contract{}, Trials: b.Trials, Open: make([]bool, len(Regions)), Arms: sv.Hero.ArmsView()}
	if sv.Identity() == "disciple" {
		v.Tasks = b.Tasks
	}
	if b.Active != nil {
		v.Active = &ActiveDuel{Kind: b.Active.Kind, Id: b.Active.Id}
	}
	for i := range Regions {
		v.Open[i] = sv.Hero.RegionOpen(i)
	}
	if v.Trials == nil {
		v.Trials = map[string]int{}
	}
	if v.Bounties == nil {
		v.Bounties = []*Contract{}
	}
	return v
}
