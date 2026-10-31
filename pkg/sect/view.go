package sect

import "math"

// 页面上看到的宗门：随机数状态、产出零头不下发，数值(花费、产出、成功率)都由服务端算好。

type BuildingView struct {
	Key   string `json:"key"`
	Level int    `json:"level"`
	// Cost 是升到下一级的花费，已满级为空；Blocked 表示要先升主殿。
	Cost    *Cost `json:"cost"`
	Blocked bool  `json:"blocked"`
}

type DiscipleView struct {
	Id      int     `json:"id"`
	Name    [3]int  `json:"name"`
	Element int     `json:"element"`
	Talent  int     `json:"talent"`
	Trait   string  `json:"trait"`
	Realm   int     `json:"realm"`
	Layer   int     `json:"layer"`
	Exp     int     `json:"exp"`
	Need    int     `json:"need"`
	Rate    float64 `json:"rate"`
	Power   int     `json:"power"`
	Job     string  `json:"job"`
	Mission int     `json:"mission"`
	Injured int64   `json:"injured"`
	QiReady int64   `json:"qi_ready"`
	// BreakChance 是现在突破(不服丹)的成功率，没到瓶颈或已是最高境界为 0。
	BreakChance int `json:"break_chance"`
}

type CandidateView struct {
	*Candidate
	Power int  `json:"power"`
	Cost  Cost `json:"cost"`
}

type EventView struct {
	*Event
	Choices [2]bool `json:"choices"`
	Costs   [2]Cost `json:"costs"`
	Gains   [2]Cost `json:"gains"`
	// Threat 是需要比斗的奇遇里对手的战力，Fighter 是会出战的弟子。
	Threat  int `json:"threat,omitempty"`
	Fighter int `json:"fighter,omitempty"`
}

type MissionView struct {
	MissionSpec
	Unlocked bool `json:"unlocked"`
}

type View struct {
	Now          int64            `json:"now"`
	Created      int64            `json:"created"`
	Buildings    []BuildingView   `json:"buildings"`
	Res          Cost             `json:"res"`
	Rates        Rates            `json:"rates"`
	Pills        map[string]int   `json:"pills"`
	Items        map[string]int   `json:"items"`
	Recipes      []PillSpec       `json:"recipes"`
	Disciples    []DiscipleView   `json:"disciples"`
	Cap          int              `json:"cap"`
	Missions     []*Mission       `json:"missions"`
	MissionSlots int              `json:"mission_slots"`
	Catalog      []MissionView    `json:"catalog"`
	Recruits     []*CandidateView `json:"recruits"`
	RecruitAt    int64            `json:"recruit_at"`
	Event        *EventView       `json:"event"`
	NextEventAt  int64            `json:"next_event_at"`
	Log          []Ev             `json:"log"`
	Stats        Stats            `json:"stats"`
	Power        int              `json:"power"`
	HallPower    int              `json:"hall_power"`
}

// View 返回 now 时刻(已经 Tick 过)的宗门。
func (s *Sect) View(now int64) View {
	v := View{
		Now: now, Created: s.Created, Res: s.Res, Rates: s.Rates(now), Pills: s.Pills, Items: s.Items, Recipes: Pills,
		Cap: s.Cap(), Missions: s.Missions, MissionSlots: s.MissionSlots(), RecruitAt: s.RecruitAt,
		NextEventAt: s.NextEventAt, Log: s.Log, Stats: s.Stats, HallPower: int(math.Round(HallPower(s.Buildings[BuildingHall]))),
	}
	hall := s.Buildings[BuildingHall]
	for _, key := range BuildingOrder {
		level := s.Buildings[key]
		b := BuildingView{Key: key, Level: level}
		if level < MaxBuildingLevel {
			cost := BuildingCost(key, level+1)
			b.Cost = &cost
			b.Blocked = key != BuildingHall && level+1 > hall
		}
		v.Buildings = append(v.Buildings, b)
	}
	for _, d := range s.Disciples {
		rate := s.cultivateRate(d, d.Job)
		if d.Injured > now {
			rate *= injuredExp
		}
		if d.Mission != 0 || d.Bottleneck() {
			rate = 0
		}
		view := DiscipleView{Id: d.Id, Name: d.Name, Element: d.Element, Talent: d.Talent, Trait: d.Trait, Realm: d.Realm,
			Layer: d.Layer, Exp: int(d.Exp), Need: int(ExpNeed(d.Realm, d.Layer)), Rate: math.Round(rate*10) / 10,
			Power: s.Power(d), Job: d.Job, Mission: d.Mission, Injured: d.Injured, QiReady: d.QiReady}
		if d.Bottleneck() {
			view.BreakChance = BreakChance(d, false)
		}
		v.Power += view.Power
		v.Disciples = append(v.Disciples, view)
	}
	for _, spec := range Missions {
		v.Catalog = append(v.Catalog, MissionView{MissionSpec: spec, Unlocked: hall >= spec.Hall})
	}
	for _, c := range s.Recruits {
		if c == nil {
			v.Recruits = append(v.Recruits, nil)
			continue
		}
		d := &Disciple{Talent: c.Talent, Trait: c.Trait, Layer: c.Layer}
		v.Recruits = append(v.Recruits, &CandidateView{Candidate: c, Power: s.Power(d), Cost: RecruitCost(c)})
	}
	if s.Event != nil {
		e := &EventView{Event: s.Event, Choices: s.EventChoices(now)}
		for choice := range e.Costs {
			e.Costs[choice] = s.eventCost(s.Event.Key, choice)
			e.Gains[choice] = s.eventGain(s.Event.Key, choice)
		}
		if fightEvent(s.Event.Key, 0) {
			e.Threat = s.EventThreat(s.Event.Key)
			if best := s.bestHome(now); best != nil {
				e.Fighter = best.Id
			}
		}
		v.Event = e
	}
	return v
}

// TournamentView 是大比在页面上的样子：对手只露出下一招，随机数状态不下发。Edges 是每名弟子打当前对手的五行关系。
type TournamentView struct {
	Round    int        `json:"round"`
	Rounds   int        `json:"rounds"`
	Wins     int        `json:"wins"`
	Phase    string     `json:"phase"`
	Fighters []*Fighter `json:"fighters"`
	Edges    []int      `json:"edges"`
	Current  int        `json:"current"`
	Opponent *Fighter   `json:"opponent"`
	Intent   string     `json:"intent"`
	Pills    int        `json:"pills"`
	Turn     int        `json:"turn"`
	Turns    int        `json:"turns"`
	Last     *Turn      `json:"last"`
}

func (t *Tournament) View() TournamentView {
	v := TournamentView{Round: t.Round, Rounds: TournamentRounds, Wins: t.Wins, Phase: t.Phase, Fighters: t.Fighters,
		Current: t.Current, Opponent: t.Opponent, Pills: t.Pills, Turn: t.Turn, Turns: DuelTurns, Last: t.Last}
	if t.Phase != PhaseOver {
		v.Intent = t.Intent
	}
	for _, f := range t.Fighters {
		v.Edges = append(v.Edges, ElementEdge(f.Element, t.Opponent.Element))
	}
	return v
}
