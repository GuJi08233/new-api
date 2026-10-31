package sect

import "math"

// 问道宗门的数值表。标识与前端 web/classic/src/pages/Games/sect/text.js 一致，名字与说明文字都在前端。

const (
	// StateVersion 是存档格式的版本，改了规则、旧存档不能直接沿用时加一并在 ParseSect 里迁移。
	StateVersion = 1
	// OfflineCap 是两次上线之间最多结算多久的产出与修炼(秒)：一天上线两次就不会浪费。
	OfflineCap = 12 * 60 * 60
	// MaxRealm 是最高境界(化神)，MaxLayer 是每个境界的层数，第九层修满就要突破。
	MaxRealm = 4
	MaxLayer = 9
	// MaxBuildingLevel 是建筑的最高等级；主殿之外的建筑不能高过主殿。
	MaxBuildingLevel = 10
	// RecruitCount 是招募处同时有几个候选，每 RecruitInterval 秒换一批，也可以花 RecruitRefreshCost 灵石马上换。
	RecruitCount       = 3
	RecruitInterval    = 8 * 60 * 60
	RecruitRefreshCost = 50
	// WorkersPerBuilding 是灵田、矿脉、聚灵阵各自最多派几名弟子帮工。
	WorkersPerBuilding = 2
	// MaxCraft 是一次最多炼几颗丹。
	MaxCraft = 10
	// MaxLog 是宗门大事记保留的条数。
	MaxLog = 30
	// 弟子名字由姓与一到两个名字用字的下标拼成，字表在前端。
	NameSurnames = 32
	NameGivens   = 48
	// Elements 是五行灵根：0 金、1 木、2 水、3 火、4 土。
	Elements = 5
	// OpponentSects 是大比对手所属门派的数量，名字在前端。
	OpponentSects = 12
)

// 弟子的性格。
const (
	TraitDiligent = "diligent" // 勤奋：修炼更快
	TraitSteady   = "steady"   // 稳重：突破成功率更高
	TraitBrave    = "brave"    // 好战：战力更高
	TraitClever   = "clever"   // 机敏：帮工产出与历练收获更多
	TraitLucky    = "lucky"    // 福缘：历练更容易大获全胜，奇遇更顺利
)

var Traits = []string{TraitDiligent, TraitSteady, TraitBrave, TraitClever, TraitLucky}

// 弟子的差事：闭关修炼，或者去灵田、矿脉、聚灵阵帮工。
const (
	JobCultivate = "cultivate"
	JobField     = "field"
	JobMine      = "mine"
	JobArray     = "array"
)

var Jobs = []string{JobCultivate, JobField, JobMine, JobArray}

// 建筑。
const (
	BuildingHall    = "hall"    // 主殿：宗门等级、弟子上限、历练队伍数，其他建筑不能高过它
	BuildingField   = "field"   // 灵田：产灵草
	BuildingMine    = "mine"    // 矿脉：产玄铁
	BuildingArray   = "array"   // 聚灵阵：产灵石，加快修炼
	BuildingAlchemy = "alchemy" // 丹房：解锁丹方，有几率多出一颗
	BuildingLibrary = "library" // 藏经阁：提高战力与历练成功率
)

var BuildingOrder = []string{BuildingHall, BuildingField, BuildingMine, BuildingArray, BuildingAlchemy, BuildingLibrary}

// Cost 是一笔资源：灵石、灵草、玄铁与声望。
type Cost struct {
	Stone int `json:"stone,omitempty"`
	Herb  int `json:"herb,omitempty"`
	Ore   int `json:"ore,omitempty"`
	Rep   int `json:"rep,omitempty"`
}

func (c Cost) scale(f float64) Cost {
	round := func(v int) int { return int(math.Round(float64(v) * f)) }
	return Cost{Stone: round(c.Stone), Herb: round(c.Herb), Ore: round(c.Ore), Rep: round(c.Rep)}
}

type buildingSpec struct {
	// Base 是升到 2 级的花费的基数：升到 L 级花 Base × Growth^(L-1)。
	Base   Cost
	Growth float64
}

var buildingSpecs = map[string]buildingSpec{
	BuildingHall:    {Base: Cost{Stone: 150, Ore: 25, Rep: 15}, Growth: 2.25},
	BuildingField:   {Base: Cost{Stone: 60, Ore: 8}, Growth: 1.95},
	BuildingMine:    {Base: Cost{Stone: 120}, Growth: 1.95},
	BuildingArray:   {Base: Cost{Stone: 90, Ore: 15}, Growth: 2},
	BuildingAlchemy: {Base: Cost{Stone: 120, Herb: 60, Ore: 20}, Growth: 2},
	BuildingLibrary: {Base: Cost{Stone: 160, Ore: 40, Rep: 8}, Growth: 2.05},
}

// BuildingCost 返回把建筑升到 level 级要花多少。
func BuildingCost(building string, level int) Cost {
	spec := buildingSpecs[building]
	cost := spec.Base.scale(math.Pow(spec.Growth, float64(level-1)))
	// 主殿 2 级不要玄铁：开局还没有矿脉。
	if building == BuildingHall && level == 2 {
		cost.Ore = 0
	}
	return cost
}

// 每小时产出：主殿每级 20 灵石，聚灵阵每级 30 灵石，灵田每级 25 灵草，矿脉每级 12 玄铁。
const (
	hallStonePerLevel  = 20
	arrayStonePerLevel = 30
	fieldHerbPerLevel  = 25
	mineOrePerLevel    = 12
	// workerBonus 是每名帮工的弟子把建筑产出提高多少：炼气 20%，每高一个境界多 10%。
	workerBonus      = 0.2
	workerRealmBonus = 0.1
	// arrayCultivate 是聚灵阵每级加快修炼多少，libraryPower 是藏经阁每级提高战力多少。
	arrayCultivate = 0.06
	libraryPower   = 0.06
	// libraryMission 是藏经阁每级提高历练成功率几个百分点。
	libraryMission = 2
	// alchemyDouble 是丹房每级让一炉多出一颗的几率(百分点)。
	alchemyDouble = 4
)

// 修炼：闭关每小时 50 修为(按资质、性格、聚灵阵加减)，帮工一半，受伤三成，历练途中不修炼(回来时一起结算)。
const (
	baseExpPerHour = 50
	workExp        = 0.5
	injuredExp     = 0.3
	diligentExp    = 1.25
	// missionExp 是历练回来时按历练时长、以闭关速度的多少倍结算修为。
	missionExp = 1.0
)

// talentMult 是资质(1 凡品、2 良品、3 上品、4 极品、5 天品)对修炼速度与战力的倍数。
var talentMult = [...]float64{0, 0.6, 0.8, 1.0, 1.3, 1.7}

// realmPower 是各境界第一层的基础战力，每多一层多 10%。
var realmPower = [...]float64{10, 28, 80, 230, 660}

// breakBase 是从各境界突破到下一境界的基础成功率(%)。
var breakBase = [...]int{80, 60, 45, 30}

const (
	breakTalent  = 5  // 资质每比上品高一档加几个百分点(低一档减)
	breakSteady  = 10 // 稳重
	breakPill    = 25 // 服破境丹
	breakMax     = 95
	breakFailExp = 0.4 // 失败损失本层所需修为的几成
	breakInjury  = 3 * 60 * 60
	braveCombat  = 1.1
	cleverBonus  = 0.3 // 机敏的弟子帮工多出三成效果；历练收获每人多 15%
	cleverReward = 0.15
)

// realmExp 是各境界每层所需修为的倍数：越往上越慢，元婴要修几周，化神九层是长期目标。
var realmExp = [...]float64{1, 4, 16, 70, 300}

// ExpNeed 是从某境界某层修满所需的修为。
func ExpNeed(realm, layer int) float64 {
	return math.Round(100 * realmExp[realm] * (1 + 0.2*float64(layer-1)))
}

// 丹药。
const (
	PillHeal   = "heal"   // 回春丹：治好伤势；大比里回复三成五生命
	PillQi     = "qi"     // 聚气丹：马上得到三小时闭关的修为
	PillBreak  = "break"  // 破境丹：突破时服下，成功率 +25%
	PillMarrow = "marrow" // 洗髓丹：资质提高一档
)

// PillSpec 是一种丹药的丹方：丹房要几级、一颗花多少。
type PillSpec struct {
	Key     string `json:"key"`
	Alchemy int    `json:"alchemy"`
	Cost    Cost   `json:"cost"`
}

var Pills = []PillSpec{
	{Key: PillHeal, Alchemy: 1, Cost: Cost{Stone: 20, Herb: 40}},
	{Key: PillQi, Alchemy: 2, Cost: Cost{Stone: 40, Herb: 80}},
	{Key: PillBreak, Alchemy: 3, Cost: Cost{Stone: 150, Herb: 200, Ore: 60}},
	{Key: PillMarrow, Alchemy: 5, Cost: Cost{Stone: 600, Herb: 600, Ore: 300}},
}

func pillSpec(key string) (PillSpec, bool) {
	for _, spec := range Pills {
		if spec.Key == key {
			return spec, true
		}
	}
	return PillSpec{}, false
}

const (
	qiPillHours    = 3
	qiPillCooldown = 6 * 60 * 60
	healPillPct    = 35
)

// Drop 是历练成功时有几率带回的丹药。
type Drop struct {
	Pill string `json:"pill"`
	Pct  int    `json:"pct"`
}

// MissionSpec 是一种历练：主殿几级解锁、去多久、最多几人、推荐战力、收获与五行加成(-1 为没有)。
type MissionSpec struct {
	Key     string `json:"key"`
	Hall    int    `json:"hall"`
	Seconds int    `json:"seconds"`
	Size    int    `json:"size"`
	Power   int    `json:"power"`
	Reward  Cost   `json:"reward"`
	Element int    `json:"element"`
	Drops   []Drop `json:"drops"`
}

var Missions = []MissionSpec{
	{Key: "patrol", Hall: 1, Seconds: 30 * 60, Size: 1, Power: 10, Reward: Cost{Stone: 60, Herb: 30, Rep: 5}, Element: -1},
	{Key: "herbs", Hall: 1, Seconds: 60 * 60, Size: 2, Power: 16, Reward: Cost{Herb: 160, Rep: 5}, Element: 1,
		Drops: []Drop{{Pill: PillHeal, Pct: 30}}},
	{Key: "ore", Hall: 2, Seconds: 2 * 60 * 60, Size: 2, Power: 26, Reward: Cost{Stone: 80, Ore: 120, Rep: 8}, Element: 4},
	{Key: "bandits", Hall: 2, Seconds: 2 * 60 * 60, Size: 3, Power: 40, Reward: Cost{Stone: 260, Rep: 20}, Element: 3,
		Drops: []Drop{{Pill: PillHeal, Pct: 40}}},
	{Key: "ruins", Hall: 3, Seconds: 4 * 60 * 60, Size: 3, Power: 70, Reward: Cost{Stone: 320, Herb: 160, Ore: 100, Rep: 25}, Element: 2,
		Drops: []Drop{{Pill: PillQi, Pct: 50}, {Pill: PillBreak, Pct: 20}}},
	{Key: "beasts", Hall: 4, Seconds: 4 * 60 * 60, Size: 3, Power: 120, Reward: Cost{Herb: 260, Ore: 320, Rep: 30}, Element: 0,
		Drops: []Drop{{Pill: PillQi, Pct: 60}, {Pill: PillBreak, Pct: 25}}},
	{Key: "secret", Hall: 5, Seconds: 8 * 60 * 60, Size: 3, Power: 250, Reward: Cost{Stone: 1000, Ore: 400, Rep: 60}, Element: 3,
		Drops: []Drop{{Pill: PillBreak, Pct: 35}, {Pill: PillMarrow, Pct: 8}}},
	{Key: "abyss", Hall: 7, Seconds: 8 * 60 * 60, Size: 3, Power: 600, Reward: Cost{Stone: 2500, Herb: 600, Ore: 800, Rep: 120}, Element: 2,
		Drops: []Drop{{Pill: PillBreak, Pct: 40}, {Pill: PillMarrow, Pct: 15}}},
	{Key: "heaven", Hall: 9, Seconds: 12 * 60 * 60, Size: 3, Power: 1500, Reward: Cost{Stone: 6000, Herb: 1500, Ore: 2000, Rep: 250}, Element: -1,
		Drops: []Drop{{Pill: PillBreak, Pct: 50}, {Pill: PillMarrow, Pct: 25}}},
}

func missionSpec(key string) (MissionSpec, bool) {
	for _, spec := range Missions {
		if spec.Key == key {
			return spec, true
		}
	}
	return MissionSpec{}, false
}

const (
	// missionInjury 是历练失败时一名队员受伤多久。
	missionInjury = 2 * 60 * 60
	missionMin    = 10
	missionMax    = 95
	// missionElement 是每名灵根与历练五行相合的队员加几个百分点。
	missionElement = 10
	missionLucky   = 5
)

// MissionChance 返回一支队伍去某种历练的成功率(%)：队伍总战力达到推荐战力时七成，按比例增减，再加五行与藏经阁的加成。
// 前端按同一个公式预览成功率。
func MissionChance(spec MissionSpec, powers []int, elements []int, library int) int {
	total := 0
	for _, p := range powers {
		total += p
	}
	chance := int(math.Round(70 * float64(total) / float64(spec.Power)))
	for _, element := range elements {
		if element == spec.Element {
			chance += missionElement
		}
	}
	chance += library * libraryMission
	return min(max(chance, missionMin), missionMax)
}

// 招募候选的资质分布与花费：资质越高越稀有、越贵，上品及以上还要声望。
var (
	recruitTalentWeights = []int{30, 35, 22, 10, 3}
	recruitStone         = [...]int{0, 50, 100, 200, 500, 1200}
	recruitRep           = [...]int{0, 0, 0, 10, 40, 100}
)

// HallPower 是主殿某一级对应的「同级门派」战力，奇遇里的敌人和大比对手以它为基准。
func HallPower(hall int) float64 {
	return 10 * math.Pow(1.55, float64(max(hall, 1)-1))
}
