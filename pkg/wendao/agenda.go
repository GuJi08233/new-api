package wendao

// 问道页顶上的「当前目标」与「待办」。目标是主角眼下的主线与进行中的活动；待办是四个板块里现在
// 就能处理的事：丹炉出丹、派遣回山、灵田成熟、奇遇等着拿主意……都只是提示，页面点了跳到对应的板块与页签，能不能做仍由
// 各自的操作接口判断。

// 问道页的四个板块。
const (
	PartJourney = "journey"
	PartWorld   = "world"
	PartSect    = "sect"
	PartAbyss   = "abyss"
)

// Goal 是主角眼下的主目标，Part、Tab 是去哪里处理。Key：
//   - create：还没有主角；battle：有一场仗没打完(S 是节点，问心阶时 N 是第几级)
//   - node：去主线的下一个节点 S，N 是第几章(从 0 起)
//   - cultivate：闭关修到第 N 个境界的第九层圆满，M 是现在第几层；break：带着机缘 S 突破到第 N 个境界
//   - steps：登问心阶第 N 级；summit：问心阶已经登顶
//   - travel、meditation：正在 Region 地域游历或打坐；duel：一份契约尚未结束
type Goal struct {
	Key    string `json:"key"`
	Part   string `json:"part"`
	Tab    string `json:"tab,omitempty"`
	S      string `json:"s,omitempty"`
	N      int    `json:"n"`
	M      int    `json:"m,omitempty"`
	Region int    `json:"region,omitempty"`
}

// Todo 是一件现在就能处理的事，N 是件数。View 是页签里的分区(洞府的丹室、灵兽)。Key：
//   - 宗门：event 奇遇、missions 派遣回山、garden 灵田成熟、daily 功课可领(含宝箱)、disciple_break 弟子可突破、
//     garden_empty 灵田空着
//   - 历练：furnace_done 丹炉出丹、hero_break 主角可突破、furnace_stoke 可以调火、qi 可以服聚气丹或培元丹、
//     beast_play 灵兽可以嬉戏、lure 可以引兽、furnace_idle 丹炉空着、gather 采集点可以去、trial 周天试炼还有几关没过、
//     side 有几段支线能接着走(S 是其中第一个能去的节点)
//   - 世界：travel_ready 游历已满十二小时、bounties 可接的悬赏（Region 是第一张所在地域）；sect_tasks 是宗门可接任务
//   - 发游戏币的玩法(由控制器按领奖规则补上)：abyss_reward 秘境、tournament_reward 大比今天的奖励还没领满
type Todo struct {
	Key    string `json:"key"`
	Part   string `json:"part"`
	Tab    string `json:"tab,omitempty"`
	View   string `json:"view,omitempty"`
	S      string `json:"s,omitempty"`
	N      int    `json:"n"`
	Region int    `json:"region,omitempty"`
}

// Agenda 是目标与按急迫程度排好的待办，加上入门指引(Guide)；离开很久刚回来时 Back 是这一次补算的说明。
type Agenda struct {
	Goal  Goal          `json:"goal"`
	Todos []Todo        `json:"todos"`
	Guide []GuideStep   `json:"guide"`
	Back  *ReturnReport `json:"back,omitempty"`
}

// comebackShown 是回来以后多久之内还在问道页顶上说明补算了多少。
const comebackShown = 30 * 60

// Agenda 按 now 时刻的存档算出目标与待办，不改存档。调用方先结算到 now，回流说明只读持久化的 Save.Back，不能在预览时新建。
func (sv *Save) Agenda(now int64, day string) Agenda {
	agenda := Agenda{Goal: sv.heroGoal(), Todos: sv.todos(now, day), Guide: sv.guide()}
	if back := sv.Back; back != nil && !back.Seen && now >= back.At && now-back.At <= comebackShown {
		agenda.Back = back
	}
	return agenda
}

// GuideStep 是入门指引的一步：Done 表示存档里已经做到了，Part、Tab 是去哪里做。Key：hero 创建主角、win 历练里打赢
// 一场、build 升级一座宗门建筑、dispatch 派弟子出去一趟、garden 在灵田种下一茬、break 主角突破筑基、abyss 走完一轮
// 九幽秘境。
type GuideStep struct {
	Key  string `json:"key"`
	Done bool   `json:"done"`
	Part string `json:"part"`
	Tab  string `json:"tab,omitempty"`
}

// guide 是入门指引：七步串起主角、历练、宗门经营与秘境，每一步都按存档里已经做到的事打勾，刷新以后不会从头再来。
func (sv *Save) guide() []GuideStep {
	s, h := sv.Sect, sv.Hero
	if s != nil && !s.IsFounded() {
		// 散修与宗门弟子还没有建筑、派遣与灵田(开宗立派以后才有)，这三步换成"选一条路"：拜入一个正道宗门。
		joined := h != nil && h.Path != nil && h.Path.Sect != ""
		return []GuideStep{
			{Key: "hero", Done: h != nil, Part: PartJourney},
			{Key: "win", Done: h != nil && h.Stats.Wins > 0, Part: PartJourney, Tab: "map"},
			{Key: "path", Done: joined, Part: PartSect},
			{Key: "break", Done: h != nil && h.Realm >= 1, Part: PartJourney},
			{Key: "abyss", Done: sv.Rift.Runs > 0, Part: PartAbyss},
		}
	}
	built, dispatched, planted := false, false, false
	if s != nil {
		for building, level := range s.Buildings {
			built = built || level > startBuildings[building]
		}
		dispatched = s.Stats.Missions > 0 || len(s.Missions) > 0
		planted = len(s.Garden.Harvests) > 0 || s.plotsWhere(func(p *Plot) bool { return p.Crop != "" }) > 0
	}
	return []GuideStep{
		{Key: "hero", Done: h != nil, Part: PartJourney},
		{Key: "win", Done: h != nil && h.Stats.Wins > 0, Part: PartJourney, Tab: "map"},
		{Key: "build", Done: built, Part: PartSect, Tab: "overview"},
		{Key: "dispatch", Done: dispatched, Part: PartSect, Tab: "missions"},
		{Key: "garden", Done: planted, Part: PartSect, Tab: "garden"},
		{Key: "break", Done: h != nil && h.Realm >= 1, Part: PartJourney},
		{Key: "abyss", Done: sv.Rift.Runs > 0, Part: PartAbyss},
	}
}

// heroGoal 是主角的主目标：先走主线上还没走过的节点(采集、支线不算)；本章都走完了就修到圆满、突破进下一章；五章走完登问心阶。
func (sv *Save) heroGoal() Goal {
	h := sv.Hero
	if h == nil {
		return Goal{Key: "create", Part: PartJourney}
	}
	if h.Battle != nil {
		return Goal{Key: "battle", Part: PartJourney, S: h.Battle.Node, N: h.Battle.Step}
	}
	if b := sv.Duels; b != nil && b.Active != nil {
		goal := Goal{Key: "duel", Part: PartSect, Tab: "overview"}
		if b.Active.Kind == DuelBounty {
			goal.Part, goal.Tab = PartWorld, ""
			if contract := b.contract(DuelBounty, b.Active.Id); contract != nil {
				goal.Region = contract.Region
			}
		}
		return goal
	}
	if h.Travel != nil {
		return Goal{Key: "travel", Part: PartWorld, Region: h.Travel.Region}
	}
	if h.Meditation != nil {
		return Goal{Key: "meditation", Part: PartWorld, Region: h.Meditation.Region}
	}
	for i := range JourneyNodes {
		n := &JourneyNodes[i]
		if n.Side == "" && n.Kind != NodeGather && h.cleared(n.Id) == 0 && h.Unlocked(n) {
			return Goal{Key: "node", Part: PartJourney, Tab: "map", S: n.Id, N: n.Chapter}
		}
	}
	if h.Realm < MaxRealm {
		if !h.Bottleneck() {
			return Goal{Key: "cultivate", Part: PartJourney, N: h.Realm, M: h.Layer}
		}
		return Goal{Key: "break", Part: PartJourney, S: BreakKeys[h.Realm], N: h.Realm + 1}
	}
	if h.Steps < MaxSteps {
		return Goal{Key: "steps", Part: PartJourney, Tab: "steps", N: h.Steps + 1}
	}
	return Goal{Key: "summit", Part: PartJourney, Tab: "steps"}
}

// todos 是四个板块现在能处理的事，按急迫程度排：有时限或在等着的在前，空着的丹炉、田地在后(付得起最便宜的丹方、灵种时
// 才提)。主角在打仗时只能打完再说，历练的待办先不提。
func (sv *Save) todos(now int64, day string) []Todo {
	todos := []Todo{}
	add := func(n int, todo Todo) {
		if n > 0 {
			todo.N = n
			todos = append(todos, todo)
		}
	}
	s := sv.Sect
	if s == nil {
		return todos
	}
	h := sv.Hero
	combatReady := h != nil && h.Battle == nil && h.Travel == nil && h.Meditation == nil &&
		(sv.Duels == nil || sv.Duels.Active == nil)
	if h != nil && h.Travel != nil && h.travelProgress(now).Capped {
		add(1, Todo{Key: "travel_ready", Part: PartWorld, View: "travel", Region: h.Travel.Region})
	}
	if h != nil && h.Battle != nil {
		h = nil
	}
	add(boolInt(s.Event != nil), Todo{Key: "event", Part: PartSect, Tab: "overview"})
	if h != nil {
		add(h.brewsWhere(func(b *Brew) bool { return b.End <= now }), Todo{Key: "furnace_done", Part: PartJourney, Tab: "cave", View: "furnace"})
	}
	add(s.readyMissions(now), Todo{Key: "missions", Part: PartSect, Tab: "missions"})
	add(s.plotsWhere(func(p *Plot) bool { return p.ready(now) }), Todo{Key: "garden", Part: PartSect, Tab: "garden"})
	if h != nil {
		add(boolInt(h.canBreak()), Todo{Key: "hero_break", Part: PartJourney})
	}
	add(s.dailyClaimable(), Todo{Key: "daily", Part: PartSect, Tab: "overview"})
	if sv.Identity() == "disciple" {
		add(boolInt(sv.Hero.Path.Stipend != day), Todo{Key: "stipend", Part: PartSect})
	}
	if h != nil {
		add(h.brewsWhere(func(b *Brew) bool {
			from, to := b.StokeWindow()
			return !b.Stoked && now >= from && now <= to
		}), Todo{Key: "furnace_stoke", Part: PartJourney, Tab: "cave", View: "furnace"})
		add(boolInt(!h.Bottleneck() && h.QiReady <= now && s.Pills[PillQi]+s.Pills[PillPeiyuan] > 0),
			Todo{Key: "qi", Part: PartJourney})
	}
	add(s.breakableDisciples(now), Todo{Key: "disciple_break", Part: PartSect, Tab: "disciples"})
	if h != nil {
		add(h.playableBeasts(day), Todo{Key: "beast_play", Part: PartJourney, Tab: "cave", View: "beasts"})
		lure := h.Cave[RoomDen] > 0 && len(h.Beasts) < BeastSlots(h.Cave[RoomDen]) && h.LureReady <= now &&
			s.afford(Baits[0].Cost)
		add(boolInt(lure), Todo{Key: "lure", Part: PartJourney, Tab: "cave", View: "beasts"})
		if s.afford(ElixirRecipes[0].Cost) {
			add(FurnaceSlots(h.Cave[RoomFurnace])-len(h.Furnaces), Todo{Key: "furnace_idle", Part: PartJourney, Tab: "cave", View: "furnace"})
		}
	}
	if s.afford(Crops[0].Seed) {
		add(s.plotsWhere(func(p *Plot) bool { return p.Crop == "" }), Todo{Key: "garden_empty", Part: PartSect, Tab: "garden"})
	}
	if h != nil {
		add(h.gatherReady(now), Todo{Key: "gather", Part: PartJourney, Tab: "map"})
		if w := h.weeklyAt(day); w != nil && combatReady {
			add(w.left(), Todo{Key: "trial", Part: PartJourney, Tab: "trial"})
		}
		stories, first := h.openSideStories()
		add(stories, Todo{Key: "side", Part: PartJourney, Tab: "map", S: first})
	}
	if combatReady && sv.Duels != nil {
		bounties, firstRegion := 0, 0
		for _, contract := range sv.Duels.Bounties {
			if contract == nil || contract.Done || contract.Tries <= 0 || !h.RegionOpen(contract.Region) {
				continue
			}
			if bounties == 0 {
				firstRegion = contract.Region
			}
			bounties++
		}
		add(bounties, Todo{Key: "bounties", Part: PartWorld, View: "bounties", Region: firstRegion})
		if sv.Identity() == "disciple" {
			tasks := 0
			for _, contract := range sv.Duels.Tasks {
				if contract != nil && !contract.Done && contract.Tries > 0 && h.RegionOpen(contract.Region) {
					tasks++
				}
			}
			add(tasks, Todo{Key: "sect_tasks", Part: PartSect, Tab: "overview"})
		}
	}
	return todos
}

// openSideStories 是有节点现在就能去的支线段数，first 是按节点顺序第一个能去的支线节点。
func (h *Hero) openSideStories() (stories int, first string) {
	seen := map[string]bool{}
	for i := range JourneyNodes {
		n := &JourneyNodes[i]
		if n.Side == "" || seen[n.Side] || h.cleared(n.Id) > 0 || !h.Unlocked(n) {
			continue
		}
		seen[n.Side] = true
		if first == "" {
			first = n.Id
		}
	}
	return len(seen), first
}

// canBreak 表示主角修到了圆满、手里有本境界要的机缘，可以冲击下一个境界。
func (h *Hero) canBreak() bool {
	return h.Realm < MaxRealm && h.Bottleneck() && h.Keys[BreakKeys[h.Realm]] > 0
}

// brewsWhere 是丹室里有几炉满足 match。
func (h *Hero) brewsWhere(match func(b *Brew) bool) int {
	n := 0
	for _, b := range h.Furnaces {
		n += boolInt(match(b))
	}
	return n
}

// playableBeasts 是今天还能嬉戏的灵兽(孵出来了、亲密没满、今天还没玩过)。
func (h *Hero) playableBeasts(day string) int {
	n := 0
	for _, b := range h.Beasts {
		n += boolInt(b.Hatch == 0 && b.Played != day && b.Bond < MaxBond)
	}
	return n
}

// gatherReady 是现在就能去的采集节点。
func (h *Hero) gatherReady(now int64) int {
	n := 0
	for i := range JourneyNodes {
		node := &JourneyNodes[i]
		n += boolInt(node.Kind == NodeGather && h.Unlocked(node) && h.GatherReady(node, now) == 0)
	}
	return n
}

// readyMissions 是已经回山、等着领收获的派遣。
func (s *Sect) readyMissions(now int64) int {
	n := 0
	for _, m := range s.Missions {
		n += boolInt(m.End <= now)
	}
	return n
}

// plotsWhere 是灵田里有几块地满足 match。
func (s *Sect) plotsWhere(match func(p *Plot) bool) int {
	n := 0
	for _, p := range s.Garden.Plots {
		n += boolInt(match(p))
	}
	return n
}

// dailyClaimable 是今天能领的功课奖励，三件都领了、宝箱还没开时宝箱也算一份。
func (s *Sect) dailyClaimable() int {
	n, claimed := 0, 0
	for _, t := range s.Daily.Tasks {
		n += boolInt(!t.Claimed && t.Have >= t.Need)
		claimed += boolInt(t.Claimed)
	}
	if len(s.Daily.Tasks) > 0 && claimed == len(s.Daily.Tasks) && !s.Daily.Chest {
		n++
	}
	return n
}

// breakableDisciples 是修到圆满、在宗门里没受伤、能冲击下一个境界的弟子。
func (s *Sect) breakableDisciples(now int64) int {
	n := 0
	for _, d := range s.Disciples {
		n += boolInt(d.Realm < MaxRealm && d.Bottleneck() && d.home(now))
	}
	return n
}
