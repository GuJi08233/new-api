package wendao

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/dungeon"
)

// Travel 是一次尚未领取的游历。收益率在出发时冻结，之后的养成不会追溯改变这次收获。
// 一次行程最多积累 OfflineCap 秒，读取状态或操作其他板块不会延长它。
type Travel struct {
	Token   string     `json:"token"`
	Region  int        `json:"region"`
	Started int64      `json:"started"`
	Rate    TravelRate `json:"rate"`
}

// TravelRate 是每小时带回的修为与材料。
type TravelRate struct {
	Exp float64 `json:"exp"`
	Res Cost    `json:"res"`
}

// TravelProgress 是目前结束这次游历时能领取的收获。Exp 已按主角当前境界的瓶颈截断。
type TravelProgress struct {
	Travel
	Elapsed    int64   `json:"elapsed"`
	CapSeconds int64   `json:"cap_seconds"`
	Exp        float64 `json:"exp"`
	Res        Cost    `json:"res"`
	Capped     bool    `json:"capped"`
}

// TravelLoot 是本次结束游历实际入账的收获；材料不包含宗门自身的离线产出。
type TravelLoot struct {
	Region  int     `json:"region"`
	Seconds int64   `json:"seconds"`
	Exp     float64 `json:"exp"`
	Res     Cost    `json:"res"`
	Capped  bool    `json:"capped"`
}

// TravelView 包含已经到达的地域、当前出发时的每小时收益，以及尚未领取的一次行程。
type TravelView struct {
	Now       int64           `json:"now"`
	Available []bool          `json:"available"`
	Rates     []TravelRate    `json:"rates"`
	Active    *TravelProgress `json:"active,omitempty"`
}

// TravelReached 表示主角真正到过这个地域；青岚山是出生地，其余地域要走过对应章节的至少一个节点。
func (h *Hero) TravelReached(region int) bool {
	if !h.RegionOpen(region) {
		return false
	}
	if region == 0 {
		return true
	}
	for id, record := range h.Nodes {
		if record == nil || record.Clears < 1 {
			continue
		}
		if node := JourneyNodeOf(id); node != nil && node.Chapter == region {
			return true
		}
	}
	return false
}

// TravelRate 按地域与永久养成计算游历的每小时收益。药力、丹符、弟子和灵兽不参与挂机，不消耗战斗补给。
// 强度百分点为层数增量 + 3×力量 + 生命成长/10，上限 50；修为在闭关速度上加 10% + 每地域 5% + 强度，
// 材料基数为每地域档 24 灵石、12 灵草、6 玄铁，再乘 1 + 强度%。
func (h *Hero) TravelRate(s *Sect, region int) TravelRate {
	if region < 0 || region >= len(Regions) {
		return TravelRate{}
	}
	power := min(max(h.Layer-1, 0)+3*max(h.Strength(), 0)+max(h.MaxHP()-dungeon.ClassHP(h.Class), 0)/10, 50)
	size := region + 1
	return TravelRate{
		Exp: h.CultivateRate(s) * float64(110+5*region+power) / 100,
		Res: Cost{Stone: 24 * size * (100 + power) / 100, Herb: 12 * size * (100 + power) / 100, Ore: 6 * size * (100 + power) / 100},
	}
}

// travelProgress 只计算可领取收获，不改存档；闭关与游历分别结算，二者不重复计时。
func (h *Hero) travelProgress(now int64) *TravelProgress {
	if h.Travel == nil {
		return nil
	}
	t := h.Travel
	elapsed := min(max(now-t.Started, 0), int64(OfflineCap))
	hours := float64(elapsed) / 3600
	capacity := -h.Exp
	for layer := h.Layer; layer <= MaxLayer; layer++ {
		capacity += ExpNeed(h.Realm, layer)
	}
	return &TravelProgress{Travel: *t, Elapsed: elapsed, CapSeconds: OfflineCap,
		Exp: min(t.Rate.Exp*hours, max(capacity, 0)),
		Res: Cost{Stone: int(math.Floor(float64(t.Rate.Res.Stone)*hours + 1e-9)),
			Herb: int(math.Floor(float64(t.Rate.Res.Herb)*hours + 1e-9)),
			Ore:  int(math.Floor(float64(t.Rate.Res.Ore)*hours + 1e-9))},
		Capped: now-t.Started >= OfflineCap}
}

// TravelView 返回游历状态；没有主角或储物处时为 nil。
func (sv *Save) TravelView(now int64) *TravelView {
	if sv.Hero == nil || sv.Sect == nil {
		return nil
	}
	h := sv.Hero
	v := &TravelView{Now: now, Available: make([]bool, len(Regions)), Rates: make([]TravelRate, len(Regions)), Active: h.travelProgress(now)}
	for region := range Regions {
		v.Available[region] = h.TravelReached(region)
		v.Rates[region] = h.TravelRate(sv.Sect, region)
	}
	return v
}

// StartTravel 先结清出发前的闭关，再开始一次游历。秘境会话不在 Save 中，由接口在同一事务里核对互斥。
func (sv *Save) StartTravel(region int, now int64) error {
	h := sv.Hero
	if h == nil || sv.Sect == nil || h.Travel != nil || h.Meditation != nil || h.Battle != nil || (sv.Duels != nil && sv.Duels.Active != nil) || now < h.Last {
		return ErrUnavailable
	}
	if region < 0 || region >= len(Regions) {
		return ErrAction
	}
	if !h.TravelReached(region) {
		return ErrUnavailable
	}
	h.tick(sv.Sect, now)
	h.Travel = &Travel{Token: common.GetUUID(), Region: region, Started: now, Rate: h.TravelRate(sv.Sect, region)}
	return nil
}

// FinishTravel 将这次行程的修为与材料各入账一次，随后恢复普通闭关。旧页面的 token 不能结束后来新开的一次游历。
func (sv *Save) FinishTravel(token string, now int64) (*TravelLoot, error) {
	h := sv.Hero
	if h == nil || sv.Sect == nil || h.Travel == nil || token == "" || token != h.Travel.Token || now < h.Travel.Started {
		return nil, ErrUnavailable
	}
	progress := h.travelProgress(now)
	loot := &TravelLoot{Region: progress.Region, Seconds: progress.Elapsed, Exp: progress.Exp, Res: progress.Res, Capped: progress.Capped}
	h.addExp(loot.Exp)
	sv.Sect.gain(loot.Res)
	h.Travel = nil
	h.Last = max(h.Last, now)
	return loot, nil
}
