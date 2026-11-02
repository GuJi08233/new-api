package wendao

import (
	"fmt"
	"slices"
	"time"
)

// 周天试炼：打败第一章的首领以后开放，每周三关，第一关遭遇、第二关强敌、第三关首领。敌人取自主角打败了首领的最后一章的
// 节点，每关带一种「天象」改一改规则：敌人更硬、更凶、更多，或者不许带丹符、弟子与灵兽，或者主角开战多些力量与护体、
// 敌人也更强。三关在这一周第一次操作时按当时打到的章节定下，整周不变(之后规则改了也不影响这一周)；每关第一次打赢有
// 宗门资源与丹药，三关全过另有一颗洗髓丹与一份灵药。可以一直重试，过了的关不能再打。周天试炼不发游戏币，不算进九幽秘境
// 的成绩，也不影响问心阶。
//
// 周按服务器本地日期的 ISO 周算，在这一周第一次操作时换；有一场试炼没打完时不换，这一场打完照旧算开战的那一周。

// WeeklyTrials 是每周几关。
const WeeklyTrials = 3

// 天象。
const (
	SignIron    = "iron"    // 金石：敌人皮糙肉厚、出手轻
	SignFury    = "fury"    // 血月：敌人出手重、身子脆
	SignHorde   = "horde"   // 百鬼：多一个敌人，各自弱一些
	SignBare    = "bare"    // 空明：不带丹符、随行弟子与灵兽
	SignBlessed = "blessed" // 天助：主角开战有力量与护体，敌人也更强
)

// WeeklySign 是一种天象：敌人的生命、伤害再乘的百分比；Extra 多一个敌人(这一周遭遇那一关的第一个敌人)；Bare 不带丹符、
// 随行弟子与灵兽；Strength、Block 是主角开战多的力量与护体。
type WeeklySign struct {
	Key      string `json:"key"`
	HP       int    `json:"hp"`
	Dmg      int    `json:"dmg"`
	Extra    bool   `json:"extra,omitempty"`
	Bare     bool   `json:"bare,omitempty"`
	Strength int    `json:"strength,omitempty"`
	Block    int    `json:"block,omitempty"`
}

// WeeklySigns 是全部天象，每周按周数轮流取三种(一关一种，不重复)。数值按贪心机器人模拟定：刚打完一章的主角(不带丹符、
// 弟子、灵兽与洞府加成)第一次就赢的比例与这一章原样的节点相近，多在三成到全胜之间，各流派各有吃亏和占便宜的天象。
var WeeklySigns = []WeeklySign{
	{Key: SignIron, HP: 140, Dmg: 80},
	{Key: SignFury, HP: 70, Dmg: 120},
	{Key: SignHorde, HP: 70, Dmg: 85, Extra: true},
	{Key: SignBare, HP: 90, Dmg: 90, Bare: true},
	{Key: SignBlessed, HP: 110, Dmg: 100, Strength: 2, Block: 12},
}

// trialKinds 是三关的种类，trialReward 是每关第一次打赢的资源：敌人所在节点第一次通过的资源乘这个倍数；trialPills 是
// 每关另给的丹药。
var (
	trialKinds  = [WeeklyTrials]string{NodeCombat, NodeElite, NodeBoss}
	trialReward = map[string]float64{NodeCombat: 1.5, NodeElite: 1, NodeBoss: 0.6}
	trialPills  = map[string]string{NodeElite: PillQi, NodeBoss: PillBreak}
)

// WeeklyTrial 是一关：种类、敌人取自的节点、敌人与强度(已经乘上天象)、天象，以及第一次打赢的资源与丹药。
type WeeklyTrial struct {
	Kind    string   `json:"kind"`
	Node    string   `json:"node"`
	Enemies []string `json:"enemies"`
	HP      int      `json:"hp"`
	Dmg     int      `json:"dmg"`
	Sign    string   `json:"sign"`
	Reward  Cost     `json:"reward"`
	Pill    string   `json:"pill,omitempty"`
}

// Weekly 是主角的周天试炼。Week 是哪一周(如 2026-W45)，Chapter 是这一周的敌人取自第几章(从 0 起)，Trials 是这一周
// 的三关，Cleared 是哪几关打赢过；Weeks 是一共有几周三关全过(换周时留下)。
type Weekly struct {
	Week    string        `json:"week"`
	Chapter int           `json:"chapter"`
	Trials  []WeeklyTrial `json:"trials"`
	Cleared []bool        `json:"cleared"`
	Weeks   int           `json:"weeks,omitempty"`
}

// weekEpoch 是推算第几周用的一个周一。
var weekEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// WeekOf 返回 day(服务器本地日期，2006-01-02)所在的 ISO 周(如 2026-W45)与从 weekEpoch 起的第几周，day 不是日期时周为空。
func WeekOf(day string) (string, int) {
	date, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return "", 0
	}
	year, week := date.ISOWeek()
	days := int(date.Sub(weekEpoch).Hours()) / 24
	index := days / 7
	if days%7 < 0 {
		index--
	}
	return fmt.Sprintf("%d-W%02d", year, week), index
}

// WeekEnd 是 day 所在的这一周在服务器本地时间什么时候结束(下周一零点)，day 不是日期时为 0。
func WeekEnd(day string) int64 {
	date, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return 0
	}
	ahead := (8 - int(date.Weekday())) % 7
	if ahead == 0 {
		ahead = 7
	}
	return time.Date(date.Year(), date.Month(), date.Day()+ahead, 0, 0, 0, 0, time.Local).Unix()
}

// weeklySign 是标识为 key 的天象。
func weeklySign(key string) WeeklySign {
	index := slices.IndexFunc(WeeklySigns, func(s WeeklySign) bool { return s.Key == key })
	if index < 0 {
		return WeeklySign{Key: key, HP: 100, Dmg: 100}
	}
	return WeeklySigns[index]
}

// trialChapter 是周天试炼取敌人的章节：打败了最后一位首领的最后一章，一章都没有打完时为 -1(还没开放)。
func (h *Hero) trialChapter() int {
	for c := Chapters - 1; c >= 0; c-- {
		last := ""
		for i := range JourneyNodes {
			if n := &JourneyNodes[i]; n.Chapter == c && n.Kind == NodeBoss {
				last = n.Id
			}
		}
		if last != "" && h.cleared(last) > 0 {
			return c
		}
	}
	return -1
}

// newWeekly 按第 index 周与章节 chapter 定下这一周的三关：每关取这一章同种节点里轮到的那一个，天象按周数轮流取。
func newWeekly(week string, index int, chapter int) *Weekly {
	w := &Weekly{Week: week, Chapter: chapter, Cleared: make([]bool, WeeklyTrials)}
	var first string
	for k, kind := range trialKinds {
		var nodes []*JourneyNode
		for i := range JourneyNodes {
			if n := &JourneyNodes[i]; n.Chapter == chapter && n.Kind == kind {
				nodes = append(nodes, n)
			}
		}
		n := nodes[mod(index, len(nodes))]
		sign := WeeklySigns[mod(index+2*k, len(WeeklySigns))]
		hp, dmg := EnemyScale(n)
		enemies := slices.Clone(n.Enemies)
		if k == 0 {
			first = enemies[0]
		}
		if sign.Extra && len(enemies) < maxTrialEnemies {
			enemies = append(enemies, first)
		}
		w.Trials = append(w.Trials, WeeklyTrial{
			Kind: kind, Node: n.Id, Enemies: enemies, Sign: sign.Key,
			HP: min(max(hp*sign.HP/100, 50), 2000), Dmg: min(max(dmg*sign.Dmg/100, 50), 2000),
			Reward: n.First.scale(trialReward[kind]), Pill: trialPills[kind],
		})
	}
	return w
}

// maxTrialEnemies 是一关最多几个敌人(dungeon.BattleMaxEnemies)。
const maxTrialEnemies = 3

func mod(a, n int) int { return ((a % n) + n) % n }

// weeklyAt 是 day 这一周该看到的周天试炼，不改存档：存档里已经是这一周的、或者有一场试炼没打完(跨周的仗照旧算开战
// 那一周)时就是存档里的；否则按现在打到的章节新定三关(下一次操作时 rollWeekly 写进存档)。还没开放时为 nil。
func (h *Hero) weeklyAt(day string) *Weekly {
	week, index := WeekOf(day)
	if week == "" {
		return nil
	}
	if h.Weekly != nil && (h.Weekly.Week == week || (h.Battle != nil && h.Battle.Trial > 0)) {
		return h.Weekly
	}
	chapter := h.trialChapter()
	if chapter < 0 {
		return nil
	}
	w := newWeekly(week, index, chapter)
	if h.Weekly != nil {
		w.Weeks = h.Weekly.Weeks
	}
	return w
}

// left 是这一周还有几关没打赢。
func (w *Weekly) left() int {
	n := 0
	for _, cleared := range w.Cleared {
		n += boolInt(!cleared)
	}
	return n
}

// rollWeekly 把存档里的周天试炼换到 day 这一周(还没开放时什么都不做)。
func (h *Hero) rollWeekly(day string) {
	if w := h.weeklyAt(day); w != nil {
		h.Weekly = w
	}
}

// trial 挑战这一周的第 index 关(从 1 起)：打赢过的关不能再打；空明天象不带随行弟子。
func (h *Hero) trial(s *Sect, index int, team []int, seed uint64, now int64) error {
	w := h.Weekly
	if w == nil {
		return ErrUnavailable
	}
	if index < 1 || index > len(w.Trials) {
		return ErrAction
	}
	if w.Cleared[index-1] {
		return ErrUnavailable
	}
	t := w.Trials[index-1]
	sign := weeklySign(t.Sign)
	if sign.Bare && len(team) > 0 {
		return ErrAction
	}
	rules := battleRules{Bare: sign.Bare, Strength: sign.Strength, Block: sign.Block}
	return h.startBattle(s, &HeroBattle{Trial: index, Week: w.Week}, t.Kind, t.Enemies, t.HP, t.Dmg, team, rules, seed, now)
}

// clearTrial 记下打赢周天试炼的一关并发奖励，三关都过了另开宝箱。返回这一关的种类与章节(给出战的灵兽算经验)。只认开战
// 那一周、还没打赢过的关，别的情况(不会发生：有试炼没打完时不换周)什么都不给。
func (h *Hero) clearTrial(s *Sect, battle *HeroBattle) ([]Ev, string, int) {
	w := h.Weekly
	if w == nil || w.Week != battle.Week || battle.Trial < 1 || battle.Trial > len(w.Trials) || w.Cleared[battle.Trial-1] {
		return nil, NodeCombat, 0
	}
	t := w.Trials[battle.Trial-1]
	w.Cleared[battle.Trial-1] = true
	evs := append([]Ev{{K: "hero_trial", S: t.Sign, N: battle.Trial}}, s.gain(t.Reward)...)
	if t.Pill != "" {
		s.Pills[t.Pill]++
		evs = append(evs, Ev{K: "pill", S: t.Pill, N: 1})
	}
	if !slices.Contains(w.Cleared, false) {
		w.Weeks++
		rare := RareKeys[pickWeighted(&s.Rng, chestRares)]
		s.Pills[PillMarrow]++
		s.Rare[rare]++
		evs = append(evs, Ev{K: "hero_trial_week", N: w.Weeks}, Ev{K: "pill", S: PillMarrow, N: 1}, Ev{K: "rare", S: rare, N: 1})
	}
	return evs, t.Kind, w.Chapter
}

// WeeklyView 是页面上的周天试炼：Ends 是这一周什么时候结束。
type WeeklyView struct {
	*Weekly
	Ends int64 `json:"ends"`
}

// WeeklyRules 是页面要的周天试炼规则：天象表，以及三关全过另给的丹药。
type WeeklyRules struct {
	Signs []WeeklySign `json:"signs"`
	Chest string       `json:"chest"`
}

// WeeklyRulesOf 返回周天试炼的规则。
func WeeklyRulesOf() WeeklyRules { return WeeklyRules{Signs: WeeklySigns, Chest: PillMarrow} }
