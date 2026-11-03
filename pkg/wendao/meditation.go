package wendao

import "github.com/QuantumNous/new-api/common"

// Meditation 是主角正在一处灵脉打坐的状态。自然修为随时间结算，不需要再次领取；离座前一直持续。
type Meditation struct {
	Token   string `json:"token"`
	Region  int    `json:"region"`
	Started int64  `json:"started"`
}

// MeditationProgress 是当前打坐地点与速度；灵脉倍率只作用于按时间结算的修为。
type MeditationProgress struct {
	Meditation
	BonusPct int     `json:"bonus_pct"`
	BaseRate float64 `json:"base_rate"`
	Rate     float64 `json:"rate"`
}

// MeditationView 是世界地图的灵脉状态与当前速度。Rates 按各地域灵脉计算，不改变已经结算的修为。
type MeditationView struct {
	Now       int64               `json:"now"`
	Available []bool              `json:"available"`
	Bonuses   []int               `json:"bonuses"`
	Rates     []float64           `json:"rates"`
	Active    *MeditationProgress `json:"active,omitempty"`
}

// MeditationBonus 是地域灵脉对自然闭关的百分比加成。各地域为 65%、70%、75%、80%、85%，略高于同地域游历的最高修为速度，
// 让打坐侧重修为、游历兼得材料。它在现有闭关来源之外相乘，不影响丹药的即时修为或游历开局速率。
func MeditationBonus(region int) int {
	if region < 0 || region >= len(Regions) {
		return 0
	}
	return 65 + 5*region
}

// IdleRate 是主角按时间自然获得的每小时修为；基础闭关速度仍由 CultivateRate 提供，丹药与游历不带灵脉倍率。
func (h *Hero) IdleRate(s *Sect) float64 {
	if h.Travel != nil {
		return 0
	}
	rate := h.CultivateRate(s)
	if h.Meditation != nil {
		rate *= 1 + float64(MeditationBonus(h.Meditation.Region))/100
	}
	return rate
}

func (h *Hero) meditationProgress(s *Sect) *MeditationProgress {
	if h.Meditation == nil {
		return nil
	}
	return &MeditationProgress{Meditation: *h.Meditation, BonusPct: MeditationBonus(h.Meditation.Region),
		BaseRate: h.CultivateRate(s), Rate: h.IdleRate(s)}
}

// MeditationView 返回可以前往的灵脉与正在打坐的位置；出生地以外要先真正到达对应章节。
func (sv *Save) MeditationView(now int64) *MeditationView {
	if sv.Hero == nil || sv.Sect == nil {
		return nil
	}
	h := sv.Hero
	v := &MeditationView{Now: now, Available: make([]bool, len(Regions)), Bonuses: make([]int, len(Regions)),
		Rates: make([]float64, len(Regions)), Active: h.meditationProgress(sv.Sect)}
	for region := range Regions {
		v.Available[region] = h.TravelReached(region)
		v.Bonuses[region] = MeditationBonus(region)
		v.Rates[region] = h.CultivateRate(sv.Sect) * (1 + float64(v.Bonuses[region])/100)
	}
	return v
}

// StartMeditation 先按原来的普通闭关速度结算，再开始灵脉打坐。游历、历练、世界契约互斥，秘境会话由接口原子核对。
func (sv *Save) StartMeditation(region int, now int64) error {
	h := sv.Hero
	if h == nil || sv.Sect == nil || h.Meditation != nil || h.Travel != nil || h.Battle != nil ||
		(sv.Duels != nil && sv.Duels.Active != nil) || now < h.Last {
		return ErrUnavailable
	}
	if region < 0 || region >= len(Regions) {
		return ErrAction
	}
	if !h.TravelReached(region) {
		return ErrUnavailable
	}
	h.tick(sv.Sect, now)
	h.Meditation = &Meditation{Token: common.GetUUID(), Region: region, Started: now}
	return nil
}

// LeaveMeditation 先结算到离座时刻的打坐修为，再恢复普通闭关；不能用旧行程 token 或倒退的时钟结束当前状态。
func (sv *Save) LeaveMeditation(token string, now int64) error {
	h := sv.Hero
	if h == nil || sv.Sect == nil || h.Meditation == nil || token == "" || token != h.Meditation.Token ||
		now < h.Meditation.Started || now < h.Last {
		return ErrUnavailable
	}
	h.tick(sv.Sect, now)
	h.Meditation = nil
	return nil
}
