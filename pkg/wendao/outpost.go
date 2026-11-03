package wendao

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
)

const (
	OutpostHallNeed = 3
	OutpostMinRealm = 1
	OutpostSlots    = 2
)

// Outpost 是在其他地域建起的一处下院；驻守名单只从 Disciple.Outpost 读取，避免两份名单不同步。
type Outpost struct {
	Region  int   `json:"region"`
	Created int64 `json:"created"`
}

type OutpostAction struct {
	Type     string `json:"action"`
	Region   int    `json:"region"`
	Disciple int    `json:"disciple"`
	Token    string `json:"token,omitempty"`
}

type OutpostRegionView struct {
	Region  int   `json:"region"`
	Reached bool  `json:"reached"`
	Built   bool  `json:"built"`
	Created int64 `json:"created"`
	Cost    Cost  `json:"cost"`
	Members []int `json:"members"`
	Rate    Rates `json:"rate"`
}

// OutpostView 的 Busy 由控制器根据主动大比与论剑会话补充；历练随行在同一份存档内核对。
type OutpostView struct {
	HallNeed int                 `json:"hall_need"`
	MinRealm int                 `json:"min_realm"`
	Slots    int                 `json:"slots"`
	Regions  []OutpostRegionView `json:"regions"`
	Total    Rates               `json:"total"`
	Busy     []int               `json:"busy"`
}

// OutpostCost 是在地域 region 建院的花费；青岚山是本宗，不建下院。
func OutpostCost(region int) Cost {
	if region < 1 || region >= len(Regions) {
		return Cost{}
	}
	size := region + 1
	return Cost{Stone: 1000 * size, Herb: 300 * size, Ore: 200 * size, Rep: 50 * region}
}

func (s *Sect) outpost(region int) *Outpost {
	for i := range s.Outposts {
		if s.Outposts[i].Region == region {
			return &s.Outposts[i]
		}
	}
	return nil
}

// outpostRate 是下院每小时的实际资源产出，已计仙盟倍率。层数不参与，自动升层不会改变整段离线结算的速率。
func (s *Sect) outpostRate(region int, at int64) Rates {
	if !s.IsFounded() || region < 1 || region >= len(Regions) || s.outpost(region) == nil {
		return Rates{}
	}
	base := [...]Rates{{}, {Stone: 16, Herb: 6, Ore: 12}, {Stone: 20, Herb: 8, Ore: 16},
		{Stone: 24, Herb: 20, Ore: 8}, {Stone: 30, Herb: 10, Ore: 20}}
	var rate Rates
	for _, d := range s.Disciples {
		if d.Outpost != region || d.Mission != 0 || d.Injured > at {
			continue
		}
		bonus := 1 + 0.25*float64(d.Realm)
		if d.Trait == TraitClever {
			bonus *= 1.3
		}
		bonus *= 1 + AllianceBonusPerLevel*float64(s.AllianceLevel)
		rate.Stone += base[region].Stone * bonus
		rate.Herb += base[region].Herb * bonus
		rate.Ore += base[region].Ore * bonus
	}
	return rate
}

func (sv *Save) OutpostView(now int64) *OutpostView {
	if sv.Hero == nil || sv.Sect == nil || !sv.Sect.IsFounded() {
		return nil
	}
	s := sv.Sect
	v := &OutpostView{HallNeed: OutpostHallNeed, MinRealm: OutpostMinRealm, Slots: OutpostSlots,
		Regions: []OutpostRegionView{}, Busy: []int{}}
	for region := 1; region < len(Regions); region++ {
		view := OutpostRegionView{Region: region, Reached: sv.Hero.TravelReached(region), Cost: OutpostCost(region),
			Members: []int{}, Rate: s.outpostRate(region, now)}
		if outpost := s.outpost(region); outpost != nil {
			view.Built, view.Created = true, outpost.Created
		}
		for _, d := range s.Disciples {
			if d.Outpost == region {
				view.Members = append(view.Members, d.Id)
			}
		}
		v.Total.Stone += view.Rate.Stone
		v.Total.Herb += view.Rate.Herb
		v.Total.Ore += view.Rate.Ore
		v.Regions = append(v.Regions, view)
	}
	return v
}

// OutpostApply 在改变驻守与产出来源前结清旧状态。主动大比、论剑的占用由调用方在同一存档锁内检查。
func (sv *Save) OutpostApply(a OutpostAction, now int64) ([]Ev, error) {
	if a.Region < 1 || a.Region >= len(Regions) || (a.Type != "build" && a.Type != "station" && a.Type != "recall") {
		return nil, ErrAction
	}
	s, h := sv.Sect, sv.Hero
	if h == nil || s == nil || !s.IsFounded() || now < s.Last {
		return nil, ErrUnavailable
	}
	evs := s.Tick(now)
	var more []Ev
	switch a.Type {
	case "build":
		if s.Buildings[BuildingHall] < OutpostHallNeed || !h.TravelReached(a.Region) || s.outpost(a.Region) != nil {
			return nil, ErrUnavailable
		}
		cost := OutpostCost(a.Region)
		if err := s.pay(cost); err != nil {
			return nil, err
		}
		s.Outposts = append(s.Outposts, Outpost{Region: a.Region, Created: now})
		more = append(s.spent(cost), Ev{K: "outpost_build", N: a.Region})
	case "station":
		d := s.disciple(a.Disciple)
		if d == nil {
			return nil, ErrAction
		}
		if s.outpost(a.Region) == nil || d.Realm < OutpostMinRealm || !d.home(now) ||
			(h.Battle != nil && slices.Contains(h.Battle.Team, d.Id)) {
			return nil, ErrUnavailable
		}
		members := 0
		for _, other := range s.Disciples {
			if other.Outpost == a.Region {
				members++
			}
		}
		if members >= OutpostSlots {
			return nil, ErrUnavailable
		}
		d.Outpost, d.OutpostToken, d.Job = a.Region, common.GetUUID(), JobCultivate
		more = []Ev{{K: "outpost_station", N: a.Region, Who: &d.Name}}
	case "recall":
		d := s.disciple(a.Disciple)
		if d == nil {
			return nil, ErrAction
		}
		if d.Outpost != a.Region || a.Token == "" || a.Token != d.OutpostToken {
			return nil, ErrUnavailable
		}
		d.Outpost, d.OutpostToken, d.Job = 0, "", JobCultivate
		more = []Ev{{K: "outpost_recall", N: a.Region, Who: &d.Name}}
	}
	s.record(now, more)
	return append(evs, more...), nil
}
