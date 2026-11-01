package wendao

import (
	"math"
	"slices"
)

// 宗门灵田：灵田建筑之外，掌门亲手打理的几块灵田。种下灵种，过几个小时收获灵草、玄铁、丹药或灵药；灵药是丹房几张
// 新丹方的材料，也能在坊市里卖给别的掌门。道友可以来帮忙浇灌(作物长得更快、更容易丰收)，熟了很久没收的灵田会被
// 顺手摘走一点(只有灵草、玄铁这样的大宗作物，灵药摘不走)。灵田不发游戏币。

const (
	// MaxPlots 是灵田最多几块：开局两块，灵田建筑每两级多一块。
	MaxPlots = 7
	// MaxWaters 是一茬作物最多被几位道友浇灌，每次缩短 waterCut 的生长时间、丰收几率多 waterBumper 个百分点。
	MaxWaters   = 3
	waterCut    = 0.1
	waterBumper = 5
	// bumperBase 是收获时丰收(收成翻倍、灵药多一份)的基础几率(%)。
	bumperBase = 10
	// MaxPicks 是一块熟了的灵田最多被摘几次，每次摘走收成的 pickShare；熟了 PickGrace 秒之后还没收才能摘。
	MaxPicks  = 2
	pickShare = 0.1
	PickGrace = 30 * 60
	// GardenWaterDaily 与 GardenPickDaily 是每位掌门每天最多帮几次浇灌、摘几次。
	GardenWaterDaily = 10
	GardenPickDaily  = 5
	// maxGardenLog 是灵田来访记录保留几条。
	maxGardenLog = 12
	// fieldYieldPerLevel 是灵田建筑每高一级，作物里的灵草、玄铁多几成。
	fieldYieldPerLevel = 0.1
)

// WaterReward 是帮道友浇灌一次得到的谢礼。
var WaterReward = Cost{Stone: 30, Rep: 1}

// 灵药：灵田种出来的稀罕材料，存在宗门的 Rare 里。
const (
	RareFruit   = "zhuguo"  // 朱果：炼培元丹
	RareLingzhi = "lingzhi" // 九叶灵芝：另一种破境丹的丹方
	RareLotus   = "lotus"   // 七彩莲：另一种洗髓丹的丹方
)

// RareKeys 是全部灵药，按页面上的顺序。
var RareKeys = []string{RareFruit, RareLingzhi, RareLotus}

// CropSpec 是一种灵种：灵田建筑几级解锁、长多久、种子花多少，收获什么。Yield 是灵草、玄铁(按灵田等级加成)，Pill 与
// Rare 是收获的丹药或灵药，N 是几份。
type CropSpec struct {
	Key     string `json:"key"`
	Field   int    `json:"field"`
	Seconds int64  `json:"seconds"`
	Seed    Cost   `json:"seed"`
	Yield   Cost   `json:"yield"`
	Pill    string `json:"pill,omitempty"`
	Rare    string `json:"rare,omitempty"`
	N       int    `json:"n,omitempty"`
}

// Crops 是全部灵种，按解锁顺序。
var Crops = []CropSpec{
	{Key: "qingcao", Field: 1, Seconds: 60 * 60, Seed: Cost{Stone: 10}, Yield: Cost{Herb: 60}},
	{Key: "xueshen", Field: 2, Seconds: 2 * 60 * 60, Seed: Cost{Stone: 30, Herb: 20}, Pill: PillHeal, N: 2},
	{Key: "tieteng", Field: 3, Seconds: 3 * 60 * 60, Seed: Cost{Stone: 60}, Yield: Cost{Ore: 90}},
	{Key: RareFruit, Field: 4, Seconds: 6 * 60 * 60, Seed: Cost{Stone: 80, Herb: 40}, Rare: RareFruit, N: 2},
	{Key: RareLingzhi, Field: 5, Seconds: 8 * 60 * 60, Seed: Cost{Stone: 150, Herb: 80, Ore: 20}, Rare: RareLingzhi, N: 1},
	{Key: RareLotus, Field: 7, Seconds: 12 * 60 * 60, Seed: Cost{Stone: 400, Herb: 200, Ore: 80}, Rare: RareLotus, N: 1},
}

// CropOf 返回一种灵种，不认识时 ok 为 false。
func CropOf(key string) (CropSpec, bool) {
	index := slices.IndexFunc(Crops, func(c CropSpec) bool { return c.Key == key })
	if index < 0 {
		return CropSpec{}, false
	}
	return Crops[index], true
}

// Plot 是一块灵田。Crop 为空表示空着；Ripe 是熟的时间(被浇灌会提前)；Waters 与 Picks 是浇灌过、摘过这一茬的掌门；
// Taken 是被摘走了几成(百分点)。
type Plot struct {
	Crop    string `json:"crop,omitempty"`
	Planted int64  `json:"planted,omitempty"`
	Ripe    int64  `json:"ripe,omitempty"`
	Waters  []int  `json:"waters,omitempty"`
	Picks   []int  `json:"picks,omitempty"`
	Taken   int    `json:"taken,omitempty"`
}

// GardenVisit 是一条来访记录：谁(名字记在当时)、浇灌还是摘取、哪种作物、摘走多少。
type GardenVisit struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Crop string `json:"crop"`
	N    int    `json:"n,omitempty"`
	At   int64  `json:"at"`
}

// Garden 是宗门的灵田。Water 与 Pick 记今天去道友那里浇灌、摘取了几次；Harvests 是每种作物收过几茬(图鉴)。
type Garden struct {
	Plots    []*Plot        `json:"plots"`
	Water    LootDay        `json:"water"`
	Pick     LootDay        `json:"pick"`
	Log      []GardenVisit  `json:"log"`
	Harvests map[string]int `json:"harvests"`
}

// PlotCount 是灵田建筑某一级时有几块灵田。
func PlotCount(field int) int { return min(2+field/2, MaxPlots) }

// normalizeGarden 补上空表，灵田建筑升级之后补上新开出来的地。
func (s *Sect) normalizeGarden() {
	g := &s.Garden
	if g.Harvests == nil {
		g.Harvests = map[string]int{}
	}
	if g.Log == nil {
		g.Log = []GardenVisit{}
	}
	for len(g.Plots) < PlotCount(s.Buildings[BuildingField]) {
		g.Plots = append(g.Plots, &Plot{})
	}
}

// ready 表示这块地上的作物到 now 已经熟了。
func (p *Plot) ready(now int64) bool { return p.Crop != "" && now >= p.Ripe }

// cropYield 是一茬作物的收成(不算丰收与被摘)：灵草、玄铁按灵田建筑等级加成。
func (s *Sect) cropYield(spec CropSpec) Cost {
	return spec.Yield.scale(1 + fieldYieldPerLevel*float64(max(s.Buildings[BuildingField], 1)-1))
}

func (s *Sect) plot(index int) (*Plot, error) {
	s.normalizeGarden()
	if index < 0 || index >= len(s.Garden.Plots) {
		return nil, ErrAction
	}
	return s.Garden.Plots[index], nil
}

// plant 在一块空地上种下灵种。
func (s *Sect) plant(index int, crop string, now int64) ([]Ev, error) {
	spec, ok := CropOf(crop)
	if !ok {
		return nil, ErrAction
	}
	p, err := s.plot(index)
	if err != nil {
		return nil, err
	}
	if p.Crop != "" || s.Buildings[BuildingField] < spec.Field {
		return nil, ErrUnavailable
	}
	if err := s.pay(spec.Seed); err != nil {
		return nil, err
	}
	*p = Plot{Crop: crop, Planted: now, Ripe: now + spec.Seconds}
	return s.spent(spec.Seed), nil
}

// plantAll 在每块空地上种同一种灵种，种子钱够几块种几块，一块都种不了时报错。
func (s *Sect) plantAll(crop string, now int64) ([]Ev, error) {
	s.normalizeGarden()
	var evs []Ev
	var firstErr error
	for i, p := range s.Garden.Plots {
		if p.Crop != "" {
			continue
		}
		more, err := s.plant(i, crop, now)
		if err != nil {
			firstErr = err
			break
		}
		evs = append(evs, more...)
	}
	if len(evs) == 0 {
		if firstErr == nil {
			firstErr = ErrUnavailable
		}
		return nil, firstErr
	}
	return evs, nil
}

// harvest 收一块熟了的地：收成减去被摘走的几成，按浇灌次数有几率丰收。
func (s *Sect) harvest(index int, now int64) ([]Ev, error) {
	p, err := s.plot(index)
	if err != nil {
		return nil, err
	}
	if !p.ready(now) {
		return nil, ErrUnavailable
	}
	spec, _ := CropOf(p.Crop)
	bumper := s.Rng.Chance(bumperBase + waterBumper*len(p.Waters))
	n := spec.N
	yield := s.cropYield(spec)
	if bumper {
		yield = yield.scale(2)
		n++
	}
	yield = yield.scale(1 - float64(p.Taken)/100)
	evs := []Ev{{K: "harvest", S: spec.Key, N: boolInt(bumper) + 1}}
	evs = append(evs, s.gain(yield)...)
	if spec.Pill != "" {
		s.Pills[spec.Pill] += n
		evs = append(evs, Ev{K: "pill", S: spec.Pill, N: n})
	}
	if spec.Rare != "" {
		s.Rare[spec.Rare] += n
		evs = append(evs, Ev{K: "rare", S: spec.Rare, N: n})
	}
	s.Garden.Harvests[spec.Key]++
	*p = Plot{}
	return evs, nil
}

// harvestAll 收所有熟了的地，一块都没熟时报错。
func (s *Sect) harvestAll(now int64) ([]Ev, error) {
	s.normalizeGarden()
	var evs []Ev
	for i, p := range s.Garden.Plots {
		if !p.ready(now) {
			continue
		}
		more, err := s.harvest(i, now)
		if err != nil {
			return nil, err
		}
		evs = append(evs, more...)
	}
	if len(evs) == 0 {
		return nil, ErrUnavailable
	}
	return evs, nil
}

// uproot 铲掉一块地上的作物，种子钱不退。
func (s *Sect) uproot(index int) error {
	p, err := s.plot(index)
	if err != nil {
		return err
	}
	if p.Crop == "" {
		return ErrUnavailable
	}
	*p = Plot{}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// countDay 把今天的次数记到 day 上：换了一天从零开始。今天已经满 limit 次时返回 false。
func countDay(d *LootDay, day string, limit int) bool {
	if d.Day != day {
		*d = LootDay{Day: day}
	}
	if d.Runs >= limit {
		return false
	}
	d.Runs++
	return true
}

// CanWater 表示 visitor 现在能给这块地浇灌：在长、还没熟，这一茬没满 MaxWaters 次，自己没浇过。
func (p *Plot) CanWater(visitor int, now int64) bool {
	return p.Crop != "" && now < p.Ripe && len(p.Waters) < MaxWaters && !slices.Contains(p.Waters, visitor)
}

// CanPick 表示 visitor 现在能摘这块地：熟了 PickGrace 秒还没收、是灵草玄铁这样的大宗作物、没满 MaxPicks 次、自己没摘过。
func (p *Plot) CanPick(visitor int, now int64) bool {
	if p.Crop == "" || now < p.Ripe+PickGrace || len(p.Picks) >= MaxPicks || slices.Contains(p.Picks, visitor) {
		return false
	}
	spec, _ := CropOf(p.Crop)
	return spec.Yield != (Cost{})
}

// recordVisit 记一条来访记录，只留最近 maxGardenLog 条。
func (g *Garden) recordVisit(v GardenVisit) {
	g.Log = append(g.Log, v)
	if len(g.Log) > maxGardenLog {
		g.Log = slices.Clone(g.Log[len(g.Log)-maxGardenLog:])
	}
}

// WaterFor 是道友 visitor(名字 name)给这座宗门的第 index 块地浇灌：作物早熟 waterCut 的生长时间。谢礼由调用方用
// GardenThanks 记到道友自己的宗门上(两份存档在同一个事务里改)。
func (s *Sect) WaterFor(visitor int, name string, index int, now int64) error {
	p, err := s.plot(index)
	if err != nil {
		return err
	}
	if !p.CanWater(visitor, now) {
		return ErrUnavailable
	}
	spec, _ := CropOf(p.Crop)
	p.Ripe = max(now, p.Ripe-int64(math.Round(float64(spec.Seconds)*waterCut)))
	p.Waters = append(p.Waters, visitor)
	s.Garden.recordVisit(GardenVisit{Name: name, Kind: "water", Crop: p.Crop, At: now})
	return nil
}

// PickFrom 是道友 visitor 从这座宗门的第 index 块地上摘走收成的 pickShare，返回摘走的数量(放进道友宗门由调用方做)。
func (s *Sect) PickFrom(visitor int, name string, index int, now int64) (Cost, error) {
	p, err := s.plot(index)
	if err != nil {
		return Cost{}, err
	}
	if !p.CanPick(visitor, now) {
		return Cost{}, ErrUnavailable
	}
	spec, _ := CropOf(p.Crop)
	taken := s.cropYield(spec).scale(pickShare)
	p.Picks = append(p.Picks, visitor)
	p.Taken += int(pickShare * 100)
	s.Garden.recordVisit(GardenVisit{Name: name, Kind: "pick", Crop: p.Crop, N: taken.Herb + taken.Ore, At: now})
	return taken, nil
}

// GardenThanks 记下这座宗门的掌门今天又去道友那里浇灌(或摘取)了一次，浇灌给谢礼、摘取给摘到的东西。今天的次数用完时
// 返回 ErrUnavailable，调用方整笔不保存。
func (s *Sect) GardenThanks(kind string, got Cost, day string) ([]Ev, error) {
	s.normalizeGarden()
	switch kind {
	case "water":
		if !countDay(&s.Garden.Water, day, GardenWaterDaily) {
			return nil, ErrUnavailable
		}
		return s.gain(WaterReward), nil
	case "pick":
		if !countDay(&s.Garden.Pick, day, GardenPickDaily) {
			return nil, ErrUnavailable
		}
		return s.gain(got), nil
	}
	return nil, ErrAction
}

// GardenDayLeft 是今天还能去道友那里浇灌、摘取几次。
func (s *Sect) GardenDayLeft(day string) (water, pick int) {
	water, pick = GardenWaterDaily, GardenPickDaily
	if s.Garden.Water.Day == day {
		water -= s.Garden.Water.Runs
	}
	if s.Garden.Pick.Day == day {
		pick -= s.Garden.Pick.Runs
	}
	return max(water, 0), max(pick, 0)
}

// PlotView 是页面上的一块灵田：只给出浇灌、摘取的次数，不露出是哪些掌门。
type PlotView struct {
	Crop    string `json:"crop,omitempty"`
	Planted int64  `json:"planted,omitempty"`
	Ripe    int64  `json:"ripe,omitempty"`
	Waters  int    `json:"waters,omitempty"`
	Picks   int    `json:"picks,omitempty"`
	Taken   int    `json:"taken,omitempty"`
	// CanWater 与 CanPick 只在道友来访时给出：来访的掌门现在能不能浇灌、摘取。
	CanWater bool `json:"can_water,omitempty"`
	CanPick  bool `json:"can_pick,omitempty"`
}

// GardenView 是宗门页里的灵田。
type GardenView struct {
	Plots    []PlotView     `json:"plots"`
	Crops    []CropSpec     `json:"crops"`
	Log      []GardenVisit  `json:"log"`
	Harvests map[string]int `json:"harvests"`
	// Bumper 是不浇灌时丰收的几率，WaterBumper 是每次浇灌多几个百分点。
	Bumper      int `json:"bumper"`
	WaterBumper int `json:"water_bumper"`
}

func (s *Sect) gardenView(now int64) GardenView {
	s.normalizeGarden()
	v := GardenView{Plots: make([]PlotView, 0, len(s.Garden.Plots)), Log: s.Garden.Log, Harvests: s.Garden.Harvests,
		Bumper: bumperBase, WaterBumper: waterBumper}
	for _, p := range s.Garden.Plots {
		v.Plots = append(v.Plots, PlotView{Crop: p.Crop, Planted: p.Planted, Ripe: p.Ripe, Waters: len(p.Waters),
			Picks: len(p.Picks), Taken: p.Taken})
	}
	for _, spec := range Crops {
		spec.Yield = s.cropYield(spec)
		v.Crops = append(v.Crops, spec)
	}
	return v
}

// VisitView 是道友 visitor 来访时看到的灵田。
func (s *Sect) VisitView(visitor int, now int64) []PlotView {
	s.normalizeGarden()
	plots := make([]PlotView, 0, len(s.Garden.Plots))
	for _, p := range s.Garden.Plots {
		plots = append(plots, PlotView{Crop: p.Crop, Planted: p.Planted, Ripe: p.Ripe, Waters: len(p.Waters),
			Picks: len(p.Picks), Taken: p.Taken, CanWater: p.CanWater(visitor, now), CanPick: p.CanPick(visitor, now)})
	}
	return plots
}

// GardenNeeds 是给道友列表用的：这座宗门现在有几块地能让 visitor 浇灌、摘取。
func (s *Sect) GardenNeeds(visitor int, now int64) (water, pick int) {
	s.normalizeGarden()
	for _, p := range s.Garden.Plots {
		if p.CanWater(visitor, now) {
			water++
		}
		if p.CanPick(visitor, now) {
			pick++
		}
	}
	return water, pick
}
