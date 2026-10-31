package sect

import (
	"errors"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

var (
	// ErrAction 表示操作不认识或参数不对。
	ErrAction = errors.New("sect: invalid action")
	// ErrCost 表示资源或丹药不够。
	ErrCost = errors.New("sect: not enough resources")
	// ErrUnavailable 表示现在不能这样做：还没解锁、已满、弟子在外历练或受伤。
	ErrUnavailable = errors.New("sect: not available now")
)

// Disciple 是一名弟子。Mission 是正在进行的历练编号(0 为在宗门里)，Injured 是伤好的时间。
type Disciple struct {
	Id      int     `json:"id"`
	Name    [3]int  `json:"name"`
	Element int     `json:"element"`
	Talent  int     `json:"talent"`
	Trait   string  `json:"trait"`
	Realm   int     `json:"realm"`
	Layer   int     `json:"layer"`
	Exp     float64 `json:"exp"`
	Job     string  `json:"job"`
	Mission int     `json:"mission"`
	Injured int64   `json:"injured"`
	// QiReady 是下一颗聚气丹什么时候能服：服一颗要等丹力化开，免得灵草堆起来就能一口气吃到化神。
	QiReady int64 `json:"qi_ready"`
	Joined  int64 `json:"joined"`
}

// Candidate 是招募处的一个候选弟子。
type Candidate struct {
	Name    [3]int `json:"name"`
	Element int    `json:"element"`
	Talent  int    `json:"talent"`
	Trait   string `json:"trait"`
	Layer   int    `json:"layer"`
}

// Mission 是一支出发了的历练队伍。
type Mission struct {
	Id    int    `json:"id"`
	Key   string `json:"key"`
	Team  []int  `json:"team"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

// Event 是一件等着掌门拿主意的奇遇，Target 是相关弟子(没有为 0)。
type Event struct {
	Key    string `json:"key"`
	At     int64  `json:"at"`
	Target int    `json:"target"`
}

// Ev 是一件发生了的事，回应给页面播放，重要的记进大事记：
//   - res：S 是资源(stone/herb/ore/rep)，N 是增减；pill：S 是丹药，N 是增减
//   - built：S 是建筑，N 是新等级；joined/left：Who 是弟子
//   - layer：Who 升了 N 层；bottleneck：Who 修到第九层圆满；realm：Who 突破到 N 境界；break_fail：Who 突破失败
//   - injured：Who 受伤 N 小时；healed：Who 伤愈；talent：Who 资质升到 N
//   - mission：S 是历练，N 是 0 失败 / 1 成功 / 2 大获全胜；event：S 是奇遇，N 是选项，Win 是比斗胜负(没有比斗为空)
//   - tournament：大比连胜 N 场；arena：论剑 N 为 1 赢 / 0 输；item：用掉符箓 S
type Ev struct {
	K   string  `json:"k"`
	S   string  `json:"s,omitempty"`
	N   int     `json:"n,omitempty"`
	Who *[3]int `json:"who,omitempty"`
	Win *bool   `json:"win,omitempty"`
	At  int64   `json:"at,omitempty"`
}

// logKinds 是要记进大事记的事。
var logKinds = map[string]bool{"built": true, "joined": true, "left": true, "bottleneck": true, "realm": true,
	"break_fail": true, "talent": true, "mission": true, "event": true, "tournament": true, "arena": true}

// Stats 是宗门的战绩。Day 与 DayRuns 记今天参加了几次大比：每天前几次大比才有声望与灵石。
type Stats struct {
	Tournaments   int    `json:"tournaments"`
	Wins          int    `json:"wins"`
	BestRounds    int    `json:"best_rounds"`
	Champions     int    `json:"champions"`
	Day           string `json:"day"`
	DayRuns       int    `json:"day_runs"`
	Missions      int    `json:"missions"`
	Breakthroughs int    `json:"breakthroughs"`
}

type carry struct {
	Stone float64 `json:"stone"`
	Herb  float64 `json:"herb"`
	Ore   float64 `json:"ore"`
}

// Sect 是玩家的宗门，整份存进局外存档。Last 是上次结算产出与修炼的时间；Carry 是产出里还不满一个的零头。
type Sect struct {
	Version     int            `json:"version"`
	Rng         Rng            `json:"rng"`
	Created     int64          `json:"created"`
	Last        int64          `json:"last"`
	Buildings   map[string]int `json:"buildings"`
	Res         Cost           `json:"res"`
	Carry       carry          `json:"carry"`
	Pills       map[string]int `json:"pills"`
	Disciples   []*Disciple    `json:"disciples"`
	NextId      int            `json:"next_id"`
	Missions    []*Mission     `json:"missions"`
	Recruits    []*Candidate   `json:"recruits"`
	RecruitAt   int64          `json:"recruit_at"`
	Event       *Event         `json:"event"`
	NextEventAt int64          `json:"next_event_at"`
	Log         []Ev           `json:"log"`
	Stats       Stats          `json:"stats"`
	// Items 是用游戏币买来、还没用掉的符箓。
	Items map[string]int `json:"items,omitempty"`
	// Arena 是论剑战绩。
	Arena ArenaStats `json:"arena"`
}

// NewSect 开宗立派：主殿、灵田、聚灵阵各一级，两名弟子(上品与良品资质)，一些灵石灵草与两颗回春丹。第一件奇遇半小时后出现。
func NewSect(seed uint64, now int64) *Sect {
	s := &Sect{
		Version:     StateVersion,
		Rng:         Rng{S: seed},
		Created:     now,
		Last:        now,
		Buildings:   map[string]int{BuildingHall: 1, BuildingField: 1, BuildingArray: 1},
		Res:         Cost{Stone: 200, Herb: 100},
		Pills:       map[string]int{PillHeal: 2},
		NextId:      1,
		Missions:    []*Mission{},
		NextEventAt: now + 30*60,
		Log:         []Ev{},
		Items:       map[string]int{},
	}
	for _, talent := range []int{3, 2} {
		c := s.newCandidate()
		c.Talent, c.Layer = talent, 1
		s.addDisciple(c, now)
	}
	s.refreshRecruits(now)
	return s
}

// ParseSect 读出存档，还没有存档时返回 nil。
func ParseSect(data string) (*Sect, error) {
	if data == "" {
		return nil, nil
	}
	s := &Sect{}
	if err := common.UnmarshalJsonStr(data, s); err != nil {
		return nil, err
	}
	if s.Buildings == nil {
		s.Buildings = map[string]int{}
	}
	if s.Pills == nil {
		s.Pills = map[string]int{}
	}
	if s.Items == nil {
		s.Items = map[string]int{}
	}
	return s, nil
}

// Encode 把存档写成 JSON。
func (s *Sect) Encode() (string, error) {
	data, err := common.Marshal(s)
	return string(data), err
}

// Cap 是弟子上限：主殿每级多一名。
func (s *Sect) Cap() int { return 2 + s.Buildings[BuildingHall] }

// MissionSlots 是同时能派出几支历练队伍：主殿每三级多一支。
func (s *Sect) MissionSlots() int { return 1 + s.Buildings[BuildingHall]/3 }

func (s *Sect) disciple(id int) *Disciple {
	for _, d := range s.Disciples {
		if d.Id == id {
			return d
		}
	}
	return nil
}

// home 表示弟子在宗门里且没有受伤，可以派去历练、比斗或突破。
func (d *Disciple) home(now int64) bool { return d.Mission == 0 && d.Injured <= now }

// Power 是弟子的战力：境界与层数，乘资质、藏经阁与好战的加成。
func (s *Sect) Power(d *Disciple) int {
	power := realmPower[d.Realm] * (1 + 0.1*float64(d.Layer-1)) * talentMult[d.Talent] * (1 + libraryPower*float64(s.Buildings[BuildingLibrary]))
	if d.Trait == TraitBrave {
		power *= braveCombat
	}
	return int(math.Round(power))
}

// cultivateRate 是弟子没有受伤时每小时的修为：资质、勤奋、聚灵阵与差事(帮工只有一半)。
func (s *Sect) cultivateRate(d *Disciple, job string) float64 {
	rate := baseExpPerHour * talentMult[d.Talent] * (1 + arrayCultivate*float64(s.Buildings[BuildingArray]))
	if d.Trait == TraitDiligent {
		rate *= diligentExp
	}
	if job != JobCultivate {
		rate *= workExp
	}
	return rate
}

// BreakChance 是弟子现在突破的成功率(%)，pill 表示服破境丹。
func BreakChance(d *Disciple, pill bool) int {
	if d.Realm >= MaxRealm {
		return 0
	}
	chance := breakBase[d.Realm] + (d.Talent-3)*breakTalent
	if d.Trait == TraitSteady {
		chance += breakSteady
	}
	if pill {
		chance += breakPill
	}
	return min(max(chance, 5), breakMax)
}

// Bottleneck 表示弟子修到了本境界第九层圆满，要突破才能继续。
func (d *Disciple) Bottleneck() bool {
	return d.Layer == MaxLayer && d.Exp >= ExpNeed(d.Realm, d.Layer)
}

// addExp 加修为并自动升层；第九层修满后修为不再增加，等突破。
func (s *Sect) addExp(d *Disciple, gain float64) []Ev {
	if gain <= 0 || d.Bottleneck() {
		return nil
	}
	var evs []Ev
	d.Exp += gain
	layers := 0
	for d.Layer < MaxLayer && d.Exp >= ExpNeed(d.Realm, d.Layer) {
		d.Exp -= ExpNeed(d.Realm, d.Layer)
		d.Layer++
		layers++
	}
	if layers > 0 {
		evs = append(evs, Ev{K: "layer", Who: &d.Name, N: layers})
	}
	if d.Layer == MaxLayer && d.Exp >= ExpNeed(d.Realm, d.Layer) {
		d.Exp = ExpNeed(d.Realm, d.Layer)
		if d.Realm < MaxRealm {
			evs = append(evs, Ev{K: "bottleneck", Who: &d.Name})
		}
	}
	return evs
}

// Rates 是每小时的产出。帮工按 at 时刻在宗门里、没受伤的弟子计算。
type Rates struct {
	Stone float64 `json:"stone"`
	Herb  float64 `json:"herb"`
	Ore   float64 `json:"ore"`
}

func (s *Sect) Rates(at int64) Rates {
	bonus := map[string]float64{}
	for _, d := range s.Disciples {
		if d.Job == JobCultivate || !d.home(at) {
			continue
		}
		b := workerBonus + workerRealmBonus*float64(d.Realm)
		if d.Trait == TraitClever {
			b *= 1 + cleverBonus
		}
		bonus[d.Job] += b
	}
	level := func(b string) float64 { return float64(s.Buildings[b]) }
	return Rates{
		Stone: hallStonePerLevel*level(BuildingHall) + arrayStonePerLevel*level(BuildingArray)*(1+bonus[JobArray]),
		Herb:  fieldHerbPerLevel * level(BuildingField) * (1 + bonus[JobField]),
		Ore:   mineOrePerLevel * level(BuildingMine) * (1 + bonus[JobMine]),
	}
}

// Tick 结算从上次到 now 的产出与修炼(最多 OfflineCap 秒)，到点换招募候选、出现新的奇遇。
func (s *Sect) Tick(now int64) []Ev {
	if now <= s.Last {
		return nil
	}
	elapsed := min(now-s.Last, OfflineCap)
	from := now - elapsed
	hours := float64(elapsed) / 3600
	rates := s.Rates(from)
	gain := func(stock *int, frac *float64, rate float64) {
		*frac += rate * hours
		// 留一点余量：每秒结算一次累加几千次，浮点误差会让刚好满一个的零头差一丝。
		whole := math.Floor(*frac + 1e-9)
		*stock += int(whole)
		*frac -= whole
	}
	gain(&s.Res.Stone, &s.Carry.Stone, rates.Stone)
	gain(&s.Res.Herb, &s.Carry.Herb, rates.Herb)
	gain(&s.Res.Ore, &s.Carry.Ore, rates.Ore)

	var evs []Ev
	for _, d := range s.Disciples {
		if d.Mission != 0 {
			continue
		}
		injured := min(max(d.Injured-from, 0), elapsed)
		rate := s.cultivateRate(d, d.Job)
		evs = append(evs, s.addExp(d, rate*(float64(elapsed-injured)+injuredExp*float64(injured))/3600)...)
	}
	if now >= s.RecruitAt {
		s.refreshRecruits(now)
	}
	if s.Event == nil && now >= s.NextEventAt {
		s.spawnEvent(now)
	}
	s.Last = now
	s.record(now, evs)
	return evs
}

// record 把重要的事记进大事记，只留最近 MaxLog 条。
func (s *Sect) record(now int64, evs []Ev) {
	for _, ev := range evs {
		if !logKinds[ev.K] {
			continue
		}
		ev.At = now
		if ev.Who != nil {
			who := *ev.Who
			ev.Who = &who
		}
		s.Log = append(s.Log, ev)
	}
	if len(s.Log) > MaxLog {
		s.Log = slices.Clone(s.Log[len(s.Log)-MaxLog:])
	}
}

func (s *Sect) afford(c Cost) bool {
	return s.Res.Stone >= c.Stone && s.Res.Herb >= c.Herb && s.Res.Ore >= c.Ore && s.Res.Rep >= c.Rep
}

// pay 扣掉一笔花费，不够时什么都不扣。
func (s *Sect) pay(c Cost) error {
	if !s.afford(c) {
		return ErrCost
	}
	s.Res.Stone -= c.Stone
	s.Res.Herb -= c.Herb
	s.Res.Ore -= c.Ore
	s.Res.Rep -= c.Rep
	return nil
}

// gain 加一笔资源(可以为负，扣到 0 为止)，返回实际增减。
func (s *Sect) gain(c Cost) []Ev {
	var evs []Ev
	add := func(stock *int, n int, key string) {
		n = max(n, -*stock)
		if n == 0 {
			return
		}
		*stock += n
		evs = append(evs, Ev{K: "res", S: key, N: n})
	}
	add(&s.Res.Stone, c.Stone, "stone")
	add(&s.Res.Herb, c.Herb, "herb")
	add(&s.Res.Ore, c.Ore, "ore")
	add(&s.Res.Rep, c.Rep, "rep")
	return evs
}

func (s *Sect) newCandidate() *Candidate {
	name := [3]int{s.Rng.Intn(NameSurnames), s.Rng.Intn(NameGivens), -1}
	if s.Rng.Chance(60) {
		name[2] = s.Rng.Intn(NameGivens)
	}
	return &Candidate{
		Name:    name,
		Element: s.Rng.Intn(Elements),
		Talent:  1 + pickWeighted(&s.Rng, recruitTalentWeights),
		Trait:   Traits[s.Rng.Intn(len(Traits))],
		Layer:   s.Rng.Range(1, 3),
	}
}

func (s *Sect) refreshRecruits(now int64) {
	s.Recruits = make([]*Candidate, RecruitCount)
	for i := range s.Recruits {
		s.Recruits[i] = s.newCandidate()
	}
	s.RecruitAt = now + RecruitInterval
}

func (s *Sect) addDisciple(c *Candidate, now int64) *Disciple {
	d := &Disciple{Id: s.NextId, Name: c.Name, Element: c.Element, Talent: c.Talent, Trait: c.Trait,
		Layer: c.Layer, Job: JobCultivate, Joined: now}
	s.NextId++
	s.Disciples = append(s.Disciples, d)
	return d
}

// RecruitCost 是招募一名候选弟子的花费。
func RecruitCost(c *Candidate) Cost {
	return Cost{Stone: recruitStone[c.Talent], Rep: recruitRep[c.Talent]}
}

// Action 是掌门的一次操作。
type Action struct {
	Type     string `json:"action"`
	Building string `json:"building"`
	Disciple int    `json:"disciple"`
	Job      string `json:"job"`
	Pill     string `json:"pill"`
	Count    int    `json:"count"`
	Mission  string `json:"mission"`
	Team     []int  `json:"team"`
	Id       int    `json:"id"`
	Index    int    `json:"index"`
	Choice   int    `json:"choice"`
	UsePill  bool   `json:"use_pill"`
	Item     string `json:"item"`
}

// Apply 先结算到 now，再执行操作，返回这期间发生的事。出错时存档可能已经改了一部分，调用方不要保存。
func (s *Sect) Apply(a Action, now int64) ([]Ev, error) {
	evs := s.Tick(now)
	var more []Ev
	var err error
	switch a.Type {
	case "sync":
	case "upgrade":
		more, err = s.upgrade(a.Building)
	case "assign":
		err = s.assign(a.Disciple, a.Job, now)
	case "craft":
		more, err = s.craft(a.Pill, a.Count)
	case "use_pill":
		more, err = s.usePill(a.Pill, a.Disciple, now)
	case "breakthrough":
		more, err = s.breakthrough(a.Disciple, a.UsePill, now)
	case "dispatch":
		err = s.dispatch(a.Mission, a.Team, now)
	case "claim":
		more, err = s.claim(a.Id, now)
	case "recall":
		err = s.recall(a.Id)
	case "recruit":
		more, err = s.recruit(a.Index, now)
	case "refresh":
		err = s.pay(Cost{Stone: RecruitRefreshCost})
		if err == nil {
			s.refreshRecruits(now)
		}
	case "dismiss":
		more, err = s.dismiss(a.Disciple)
	case "event":
		more, err = s.resolveEvent(a.Choice, now)
	case "item":
		more, err = s.useItem(a.Item, now)
	default:
		err = ErrAction
	}
	if err != nil {
		return nil, err
	}
	s.record(now, more)
	return append(evs, more...), nil
}

func (s *Sect) upgrade(building string) ([]Ev, error) {
	if _, ok := buildingSpecs[building]; !ok {
		return nil, ErrAction
	}
	next := s.Buildings[building] + 1
	if next > MaxBuildingLevel || (building != BuildingHall && next > s.Buildings[BuildingHall]) {
		return nil, ErrUnavailable
	}
	if err := s.pay(BuildingCost(building, next)); err != nil {
		return nil, err
	}
	s.Buildings[building] = next
	return []Ev{{K: "built", S: building, N: next}}, nil
}

// jobBuilding 是帮工的差事对应的建筑。
var jobBuilding = map[string]string{JobField: BuildingField, JobMine: BuildingMine, JobArray: BuildingArray}

func (s *Sect) assign(id int, job string, now int64) error {
	d := s.disciple(id)
	if d == nil || !slices.Contains(Jobs, job) {
		return ErrAction
	}
	if job == d.Job {
		return nil
	}
	if job != JobCultivate {
		if s.Buildings[jobBuilding[job]] == 0 {
			return ErrUnavailable
		}
		workers := 0
		for _, other := range s.Disciples {
			if other.Job == job {
				workers++
			}
		}
		if workers >= WorkersPerBuilding {
			return ErrUnavailable
		}
	}
	d.Job = job
	return nil
}

func (s *Sect) craft(key string, count int) ([]Ev, error) {
	spec, ok := pillSpec(key)
	if !ok || count < 1 || count > MaxCraft {
		return nil, ErrAction
	}
	alchemy := s.Buildings[BuildingAlchemy]
	if alchemy < spec.Alchemy {
		return nil, ErrUnavailable
	}
	if err := s.pay(spec.Cost.scale(float64(count))); err != nil {
		return nil, err
	}
	made := count
	for range count {
		if s.Rng.Chance(alchemy * alchemyDouble) {
			made++
		}
	}
	s.Pills[key] += made
	return []Ev{{K: "pill", S: key, N: made}}, nil
}

func (s *Sect) usePill(key string, id int, now int64) ([]Ev, error) {
	d := s.disciple(id)
	if d == nil {
		return nil, ErrAction
	}
	if s.Pills[key] <= 0 {
		return nil, ErrCost
	}
	var evs []Ev
	switch key {
	case PillHeal:
		if d.Injured <= now {
			return nil, ErrUnavailable
		}
		d.Injured = 0
		evs = append(evs, Ev{K: "healed", Who: &d.Name})
	case PillQi:
		if d.Mission != 0 || d.Bottleneck() || d.QiReady > now {
			return nil, ErrUnavailable
		}
		d.QiReady = now + qiPillCooldown
		evs = append(evs, s.addExp(d, s.cultivateRate(d, JobCultivate)*qiPillHours)...)
	case PillMarrow:
		if d.Talent >= len(talentMult)-1 {
			return nil, ErrUnavailable
		}
		d.Talent++
		evs = append(evs, Ev{K: "talent", Who: &d.Name, N: d.Talent})
	default:
		return nil, ErrAction
	}
	s.Pills[key]--
	return append(evs, Ev{K: "pill", S: key, N: -1}), nil
}

func (s *Sect) breakthrough(id int, pill bool, now int64) ([]Ev, error) {
	d := s.disciple(id)
	if d == nil {
		return nil, ErrAction
	}
	if !d.Bottleneck() || d.Realm >= MaxRealm || !d.home(now) {
		return nil, ErrUnavailable
	}
	if pill && s.Pills[PillBreak] <= 0 {
		return nil, ErrCost
	}
	var evs []Ev
	if pill {
		s.Pills[PillBreak]--
		evs = append(evs, Ev{K: "pill", S: PillBreak, N: -1})
	}
	if s.Rng.Chance(BreakChance(d, pill)) {
		d.Realm++
		d.Layer, d.Exp = 1, 0
		s.Stats.Breakthroughs++
		return append(evs, Ev{K: "realm", Who: &d.Name, N: d.Realm}), nil
	}
	d.Exp -= ExpNeed(d.Realm, d.Layer) * breakFailExp
	d.Injured = now + breakInjury
	return append(evs, Ev{K: "break_fail", Who: &d.Name}, Ev{K: "injured", Who: &d.Name, N: breakInjury / 3600}), nil
}

func (s *Sect) dispatch(key string, team []int, now int64) error {
	spec, ok := missionSpec(key)
	if !ok || len(team) < 1 || len(team) > spec.Size {
		return ErrAction
	}
	if s.Buildings[BuildingHall] < spec.Hall || len(s.Missions) >= s.MissionSlots() {
		return ErrUnavailable
	}
	for _, m := range s.Missions {
		if m.Key == key {
			return ErrUnavailable
		}
	}
	members := make([]*Disciple, 0, len(team))
	for i, id := range team {
		d := s.disciple(id)
		if d == nil || slices.Contains(team[:i], id) {
			return ErrAction
		}
		if !d.home(now) {
			return ErrUnavailable
		}
		members = append(members, d)
	}
	m := &Mission{Id: s.NextId, Key: key, Team: slices.Clone(team), Start: now, End: now + int64(spec.Seconds)}
	s.NextId++
	for _, d := range members {
		d.Mission = m.Id
	}
	s.Missions = append(s.Missions, m)
	return nil
}

func (s *Sect) takeMission(id int) *Mission {
	for i, m := range s.Missions {
		if m.Id == id {
			s.Missions = slices.Delete(s.Missions, i, i+1)
			for _, d := range s.Disciples {
				if d.Mission == id {
					d.Mission = 0
				}
			}
			return m
		}
	}
	return nil
}

// recall 把还没回来的队伍叫回来，没有收获。
func (s *Sect) recall(id int) error {
	if s.takeMission(id) == nil {
		return ErrAction
	}
	return nil
}

// claim 结算一支已经回来的历练队伍：按成功率判定成败，大获全胜收获翻一半，失败只有三成收获、一名队员受伤。
func (s *Sect) claim(id int, now int64) ([]Ev, error) {
	var mission *Mission
	for _, m := range s.Missions {
		if m.Id == id {
			mission = m
		}
	}
	if mission == nil {
		return nil, ErrAction
	}
	if now < mission.End {
		return nil, ErrUnavailable
	}
	spec, _ := missionSpec(mission.Key)
	s.takeMission(id)
	var team []*Disciple
	for _, memberId := range mission.Team {
		if d := s.disciple(memberId); d != nil {
			team = append(team, d)
		}
	}
	powers, elements := make([]int, len(team)), make([]int, len(team))
	bonus, lucky := 1.0, 0
	for i, d := range team {
		powers[i], elements[i] = s.Power(d), d.Element
		if d.Trait == TraitClever {
			bonus += cleverReward
		}
		if d.Trait == TraitLucky {
			lucky++
		}
	}
	chance := MissionChance(spec, powers, elements, s.Buildings[BuildingLibrary])
	roll := s.Rng.Intn(100)
	outcome := 0
	switch {
	case roll < chance/3+lucky*missionLucky:
		outcome, bonus = 2, bonus*1.5
	case roll < chance:
		outcome = 1
	default:
		bonus *= 0.3
	}
	evs := []Ev{{K: "mission", S: spec.Key, N: outcome}}
	evs = append(evs, s.gain(spec.Reward.scale(bonus))...)
	if outcome > 0 {
		for _, drop := range spec.Drops {
			pct := drop.Pct
			if outcome == 2 {
				pct = pct * 3 / 2
			}
			if s.Rng.Chance(pct) {
				s.Pills[drop.Pill]++
				evs = append(evs, Ev{K: "pill", S: drop.Pill, N: 1})
			}
		}
	}
	hours := float64(spec.Seconds) / 3600
	for _, d := range team {
		evs = append(evs, s.addExp(d, s.cultivateRate(d, JobCultivate)*hours*missionExp)...)
	}
	if outcome == 0 && len(team) > 0 {
		hurt := team[s.Rng.Intn(len(team))]
		hurt.Injured = max(hurt.Injured, now) + missionInjury
		evs = append(evs, Ev{K: "injured", Who: &hurt.Name, N: missionInjury / 3600})
	}
	s.Stats.Missions++
	return evs, nil
}

func (s *Sect) recruit(index int, now int64) ([]Ev, error) {
	if index < 0 || index >= len(s.Recruits) || s.Recruits[index] == nil {
		return nil, ErrAction
	}
	if len(s.Disciples) >= s.Cap() {
		return nil, ErrUnavailable
	}
	c := s.Recruits[index]
	if err := s.pay(RecruitCost(c)); err != nil {
		return nil, err
	}
	s.Recruits[index] = nil
	d := s.addDisciple(c, now)
	return []Ev{{K: "joined", Who: &d.Name}}, nil
}

// dismiss 让一名在宗门里的弟子下山，至少留一名。
func (s *Sect) dismiss(id int) ([]Ev, error) {
	d := s.disciple(id)
	if d == nil {
		return nil, ErrAction
	}
	if d.Mission != 0 || len(s.Disciples) <= 1 {
		return nil, ErrUnavailable
	}
	s.Disciples = slices.DeleteFunc(s.Disciples, func(other *Disciple) bool { return other.Id == id })
	if s.Event != nil && s.Event.Target == id {
		s.Event.Target = 0
	}
	return []Ev{{K: "left", Who: &d.Name}}, nil
}

// bestHome 是宗门里没受伤的弟子中战力最高的一名，没有时返回 nil。
func (s *Sect) bestHome(now int64) *Disciple {
	var best *Disciple
	for _, d := range s.Disciples {
		if d.home(now) && (best == nil || s.Power(d) > s.Power(best)) {
			best = d
		}
	}
	return best
}
