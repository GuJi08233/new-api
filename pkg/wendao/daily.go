package wendao

import "slices"

// 每日功课：每天从功课表里抽 DailyTaskCount 件小事(收获灵田、炼丹、领派遣、历练获胜……)，做到了领一份资源；三件都领了再开一个
// 宝箱。连续 DailyStreakBonus 天开了宝箱，那一天的宝箱里多一颗洗髓丹。每天可以换掉一件还没做完的功课(有的功课一时做不成，例如
// 服务器上没有别的掌门可以串门)。功课只发宗门资源、丹药与灵药，不发游戏币。

const (
	// DailyTaskCount 是每天几件功课。
	DailyTaskCount = 3
	// DailyRerolls 是每天能换掉几件功课。
	DailyRerolls = 1
	// DailyStreakBonus 是连续几天开宝箱多给一颗洗髓丹。
	DailyStreakBonus = 7
)

// 功课。
const (
	TaskHarvest = "harvest" // 收获灵田
	TaskWater   = "water"   // 帮道友浇灌
	TaskCraft   = "craft"   // 在丹房炼丹
	TaskClaim   = "claim"   // 领取派遣的收获
	TaskEvent   = "event"   // 处理奇遇
	TaskBuild   = "build"   // 升级建筑
	TaskJourney = "journey" // 历练里打赢
	TaskAbyss   = "abyss"   // 走完一轮九幽秘境
	TaskTrade   = "trade"   // 在万宝阁成交
)

// DailyTaskSpec 是一种功课要做几次。
type DailyTaskSpec struct {
	Key  string `json:"key"`
	Need int    `json:"need"`
}

// DailyTasks 是功课表。
var DailyTasks = []DailyTaskSpec{
	{Key: TaskHarvest, Need: 3},
	{Key: TaskWater, Need: 2},
	{Key: TaskCraft, Need: 2},
	{Key: TaskClaim, Need: 2},
	{Key: TaskEvent, Need: 1},
	{Key: TaskBuild, Need: 1},
	{Key: TaskJourney, Need: 2},
	{Key: TaskAbyss, Need: 1},
	{Key: TaskTrade, Need: 1},
}

// DailyTask 是今天的一件功课：做了几次、奖励领了没有。
type DailyTask struct {
	Key     string `json:"key"`
	Need    int    `json:"need"`
	Have    int    `json:"have"`
	Claimed bool   `json:"claimed"`
}

// Daily 是宗门今天的功课。Chest 表示今天的宝箱开了；Streak 是连续开宝箱的天数(到昨天为止断了就清零)；Rerolls 是今天换了几次。
type Daily struct {
	Day     string      `json:"day"`
	Tasks   []DailyTask `json:"tasks"`
	Chest   bool        `json:"chest"`
	Streak  int         `json:"streak"`
	Rerolls int         `json:"rerolls"`
}

// DailyTaskReward 是领一件功课的奖励：随主殿等级涨。
func DailyTaskReward(hall int) Cost {
	hall = max(hall, 1)
	return Cost{Stone: 60 * hall, Herb: 30 * hall, Rep: 2 + hall/3}
}

// chestRares 是宝箱里那份灵药的权重：朱果、九叶灵芝、七彩莲。
var chestRares = []int{60, 30, 10}

// dailyPool 是今天能抽的功课：还没有主角时不抽历练，主殿不够时不抽万宝阁。
func (s *Sect) dailyPool(hasHero bool, exclude []DailyTask) []DailyTaskSpec {
	var pool []DailyTaskSpec
	for _, spec := range DailyTasks {
		if (spec.Key == TaskJourney && !hasHero) || (spec.Key == TaskTrade && s.CanTrade() != nil) {
			continue
		}
		if slices.ContainsFunc(exclude, func(t DailyTask) bool { return t.Key == spec.Key }) {
			continue
		}
		pool = append(pool, spec)
	}
	return pool
}

// RollDaily 换到 day 这一天的功课：已经是这一天时什么都不做。昨天没开宝箱(或者上一次开宝箱不是昨天)时连续天数清零。
func (s *Sect) RollDaily(day string, yesterday string, hasHero bool) {
	s.hasHero = hasHero
	d := &s.Daily
	if d.Day == day {
		return
	}
	if !(d.Day == yesterday && d.Chest) {
		d.Streak = 0
	}
	d.Day, d.Chest, d.Rerolls, d.Tasks = day, false, 0, nil
	for range DailyTaskCount {
		pool := s.dailyPool(hasHero, d.Tasks)
		if len(pool) == 0 {
			break
		}
		spec := pool[s.Rng.Intn(len(pool))]
		d.Tasks = append(d.Tasks, DailyTask{Key: spec.Key, Need: spec.Need})
	}
}

// RollSaveDaily 按存档里有没有主角换到 day 这一天的功课，还没有宗门时什么都不做。
func (sv *Save) RollSaveDaily(day string, yesterday string) {
	if sv.Sect != nil {
		sv.Sect.RollDaily(day, yesterday, sv.Hero != nil)
	}
}

// Progress 给今天的功课 task 记上 n 次，做满为止。今天没有这件功课时什么都不做。
func (s *Sect) Progress(task string, n int) {
	for i := range s.Daily.Tasks {
		t := &s.Daily.Tasks[i]
		if t.Key == task {
			t.Have = min(t.Have+n, t.Need)
		}
	}
}

// trackDaily 按一次经营操作记功课。
func (s *Sect) trackDaily(action string, evs []Ev) {
	switch action {
	case "upgrade":
		s.Progress(TaskBuild, 1)
	case "craft":
		s.Progress(TaskCraft, 1)
	case "claim":
		s.Progress(TaskClaim, 1)
	case "event":
		s.Progress(TaskEvent, 1)
	case "harvest", "harvest_all":
		n := 0
		for _, ev := range evs {
			if ev.K == "harvest" {
				n++
			}
		}
		s.Progress(TaskHarvest, n)
	}
}

// claimDaily 领第 index 件做完了的功课的奖励。
func (s *Sect) claimDaily(index int) ([]Ev, error) {
	if index < 0 || index >= len(s.Daily.Tasks) {
		return nil, ErrAction
	}
	t := &s.Daily.Tasks[index]
	if t.Claimed || t.Have < t.Need {
		return nil, ErrUnavailable
	}
	t.Claimed = true
	return append([]Ev{{K: "daily", S: t.Key, N: t.Need}}, s.gain(DailyTaskReward(s.Buildings[BuildingHall]))...), nil
}

// openDailyChest 三件功课都领了之后开宝箱：一颗聚气丹、一份灵药，连续第 DailyStreakBonus 天(及其倍数)另有一颗洗髓丹。
func (s *Sect) openDailyChest() ([]Ev, error) {
	d := &s.Daily
	if d.Chest || len(d.Tasks) == 0 || slices.ContainsFunc(d.Tasks, func(t DailyTask) bool { return !t.Claimed }) {
		return nil, ErrUnavailable
	}
	d.Chest = true
	d.Streak++
	rare := RareKeys[pickWeighted(&s.Rng, chestRares)]
	s.Pills[PillQi]++
	s.Rare[rare]++
	evs := []Ev{{K: "daily_chest", N: d.Streak}, {K: "pill", S: PillQi, N: 1}, {K: "rare", S: rare, N: 1}}
	if d.Streak%DailyStreakBonus == 0 {
		s.Pills[PillMarrow]++
		evs = append(evs, Ev{K: "pill", S: PillMarrow, N: 1})
	}
	return evs, nil
}

// rerollDaily 把第 index 件还没做完的功课换成别的一件，每天 DailyRerolls 次。
func (s *Sect) rerollDaily(index int) error {
	d := &s.Daily
	if index < 0 || index >= len(d.Tasks) {
		return ErrAction
	}
	t := d.Tasks[index]
	if d.Rerolls >= DailyRerolls || t.Claimed || t.Have >= t.Need {
		return ErrUnavailable
	}
	pool := s.dailyPool(s.hasHero, d.Tasks)
	if len(pool) == 0 {
		return ErrUnavailable
	}
	spec := pool[s.Rng.Intn(len(pool))]
	d.Tasks[index] = DailyTask{Key: spec.Key, Need: spec.Need}
	d.Rerolls++
	return nil
}

// DailyView 是页面上的功课：今天的三件、每件的奖励、宝箱开了没有与连续天数。
type DailyView struct {
	Daily
	Reward      Cost `json:"reward"`
	StreakBonus int  `json:"streak_bonus"`
	RerollsLeft int  `json:"rerolls_left"`
}

func (s *Sect) dailyView() DailyView {
	tasks := s.Daily.Tasks
	if tasks == nil {
		tasks = []DailyTask{}
	}
	d := s.Daily
	d.Tasks = tasks
	return DailyView{Daily: d, Reward: DailyTaskReward(s.Buildings[BuildingHall]), StreakBonus: DailyStreakBonus,
		RerollsLeft: max(DailyRerolls-s.Daily.Rerolls, 0)}
}
