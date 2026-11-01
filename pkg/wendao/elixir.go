package wendao

import (
	"math"
	"slices"
)

// 炼丹：宗主在洞府的丹室亲自开炉，炼主角自己服的丹。和宗门丹房不同，开一炉要等几个小时，炼出来的丹分下品、中品、上品：
//   - 火候：文火慢一半、品质更好；中火照常；武火快一半、品质差些，还有几率炸炉(材料全没了)。
//   - 调火：炼到三分之一到三分之二之间回来调一次火，品质更好；错过了也能照常出丹。
//   - 品质按丹室等级、火候、调火与这张丹方炼成过几次(熟练)加一个随机数定，出炉时才揭晓。
// 丹室每到 5 级、9 级多一座丹炉。丹的效果见 ElixirRecipes；只在历练(含问心阶)里生效，带不进九幽秘境，不发游戏币。

// 丹室的丹。
const (
	ElixirTemper  = "cuiti"    // 淬体丹：永久增加最大生命，每个境界有上限
	ElixirGuard   = "jingang"  // 金刚丹：接下来几场开战多一层护体
	ElixirTiger   = "longhu"   // 龙虎丹：接下来几场开战多 1 点力量
	ElixirInsight = "wudao"    // 悟道丹：马上得到几个小时闭关的修为，与聚气丹共用冷却
	ElixirGolden  = "jiuzhuan" // 九转金丹：永久多 1 点力量，一共只能服 GoldenMax 颗
)

// 火候。
const (
	FireGentle = "gentle"
	FireSteady = "steady"
	FireFierce = "fierce"
)

// 品质：下品、中品、上品，是 Elixirs 里数组的下标。
const (
	GradeLow  = 0
	GradeMid  = 1
	GradeHigh = 2
	Grades    = 3
)

const (
	// GoldenMax 是九转金丹一共能服几颗。
	GoldenMax = 2
	// MaxElixirStack 是每种丹每个品质最多存几颗。
	MaxElixirStack = 99
	// stokeBonus 是调过火的一炉品质分多几分；furnaceScore 是丹室每级多几分；masteryScore 是每炼成一炉多几分，最多算
	// masteryMax 炉。
	stokeBonus   = 15
	furnaceScore = 4
	masteryScore = 2
	masteryMax   = 10
	// gradeMid、gradeHigh 是中品、上品要的品质分(随机数 0-99 加上各项加分)。
	gradeMid  = 70
	gradeHigh = 110
	// temperBase、temperRealm 是淬体丹的上限：炼气 10 点生命，每高一个境界多 10 点。
	temperBase  = 10
	temperRealm = 10
)

// FireSpec 是一种火候：炼的时间(百分比)、品质分的加减与炸炉几率(%)。
type FireSpec struct {
	Key     string `json:"key"`
	TimePct int    `json:"time_pct"`
	Score   int    `json:"score"`
	Explode int    `json:"explode"`
}

// Fires 是全部火候，按页面上的顺序。
var Fires = []FireSpec{
	{Key: FireGentle, TimePct: 150, Score: 20},
	{Key: FireSteady, TimePct: 100},
	{Key: FireFierce, TimePct: 50, Score: -10, Explode: 15},
}

func fireSpec(key string) (FireSpec, bool) {
	index := slices.IndexFunc(Fires, func(f FireSpec) bool { return f.Key == key })
	if index < 0 {
		return FireSpec{}, false
	}
	return Fires[index], true
}

// ElixirRecipe 是丹室的一张丹方：丹室几级、中火炼几个小时、花多少、要几份什么灵药、一炉出几颗。Effect 是下品、中品、
// 上品的药力(淬体丹是生命、金刚丹是护体、龙虎丹是力量、悟道丹是几个小时的修为、九转金丹是力量)，Battles 是药力管
// 几场；Graded 为假的丹不分品级，出炉都是上品。
type ElixirRecipe struct {
	Id      string `json:"id"`
	Furnace int    `json:"furnace"`
	Hours   int    `json:"hours"`
	Cost    Cost   `json:"cost"`
	Rare    string `json:"rare,omitempty"`
	RareN   int    `json:"rare_n,omitempty"`
	Yield   int    `json:"yield"`
	Effect  [3]int `json:"effect"`
	Battles [3]int `json:"battles,omitempty"`
	Graded  bool   `json:"graded"`
}

// ElixirRecipes 是丹室的丹方，按丹室等级排列。
var ElixirRecipes = []ElixirRecipe{
	{Id: ElixirTemper, Furnace: 1, Hours: 1, Cost: Cost{Stone: 80, Herb: 160}, Yield: 2, Effect: [3]int{2, 3, 4}, Graded: true},
	{Id: ElixirGuard, Furnace: 2, Hours: 2, Cost: Cost{Stone: 120, Herb: 100, Ore: 160}, Yield: 2, Effect: [3]int{8, 12, 16},
		Battles: [3]int{3, 3, 3}, Graded: true},
	{Id: ElixirTiger, Furnace: 4, Hours: 3, Cost: Cost{Stone: 400, Herb: 300}, Rare: RareFruit, RareN: 1, Yield: 2,
		Effect: [3]int{1, 1, 1}, Battles: [3]int{3, 4, 5}, Graded: true},
	{Id: ElixirInsight, Furnace: 6, Hours: 4, Cost: Cost{Stone: 800, Herb: 500}, Rare: RareLingzhi, RareN: 1, Yield: 1,
		Effect: [3]int{6, 8, 10}, Graded: true},
	{Id: ElixirGolden, Furnace: 8, Hours: 8, Cost: Cost{Stone: 4000, Herb: 2000, Ore: 1500}, Rare: RareLotus, RareN: 2, Yield: 1,
		Effect: [3]int{1, 1, 1}},
}

func elixirRecipe(id string) (ElixirRecipe, bool) {
	index := slices.IndexFunc(ElixirRecipes, func(r ElixirRecipe) bool { return r.Id == id })
	if index < 0 {
		return ElixirRecipe{}, false
	}
	return ElixirRecipes[index], true
}

// Brew 是丹室里正在炼的一炉。
type Brew struct {
	Recipe string `json:"recipe"`
	Fire   string `json:"fire"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Stoked bool   `json:"stoked,omitempty"`
}

// StokeWindow 返回这一炉能调火的时间段：炼到三分之一到三分之二之间。
func (b *Brew) StokeWindow() (from, to int64) {
	span := b.End - b.Start
	return b.Start + span/3, b.Start + span*2/3
}

// Buff 是服下的金刚丹、龙虎丹还剩几场药力，Power 是每场给的护体或力量。
type Buff struct {
	Power int `json:"power"`
	Left  int `json:"left"`
}

// FurnaceSlots 是丹室某一级有几座丹炉：开辟丹室就有一座，5 级、9 级各多一座。
func FurnaceSlots(level int) int {
	switch {
	case level >= 9:
		return 3
	case level >= 5:
		return 2
	case level >= 1:
		return 1
	}
	return 0
}

// TemperCap 是某个境界淬体丹最多加到多少生命。
func TemperCap(realm int) int { return temperBase + temperRealm*realm }

// brew 在一座空丹炉里开炉：花资源与灵药，按火候定出炉时间。
func (h *Hero) brew(s *Sect, id, fire string, now int64) ([]Ev, error) {
	recipe, ok := elixirRecipe(id)
	spec, okFire := fireSpec(fire)
	if !ok || !okFire {
		return nil, ErrAction
	}
	level := h.Cave[RoomFurnace]
	if level < recipe.Furnace || len(h.Furnaces) >= FurnaceSlots(level) {
		return nil, ErrUnavailable
	}
	if recipe.Rare != "" && s.Rare[recipe.Rare] < recipe.RareN {
		return nil, ErrCost
	}
	if err := s.pay(recipe.Cost); err != nil {
		return nil, err
	}
	evs := spentEvs(recipe.Cost)
	if recipe.Rare != "" {
		s.Rare[recipe.Rare] -= recipe.RareN
		evs = append(evs, Ev{K: "rare", S: recipe.Rare, N: -recipe.RareN})
	}
	seconds := int64(recipe.Hours) * 3600 * int64(spec.TimePct) / 100
	h.Furnaces = append(h.Furnaces, &Brew{Recipe: id, Fire: fire, Start: now, End: now + seconds})
	s.Progress(TaskCraft, 1)
	return append(evs, Ev{K: "hero_brew", S: id}), nil
}

// stoke 给一炉调火：只能在调火的时间段里调一次。
func (h *Hero) stoke(index int, now int64) ([]Ev, error) {
	if index < 0 || index >= len(h.Furnaces) {
		return nil, ErrAction
	}
	b := h.Furnaces[index]
	from, to := b.StokeWindow()
	if b.Stoked || now < from || now > to {
		return nil, ErrUnavailable
	}
	b.Stoked = true
	return []Ev{{K: "hero_stoke", S: b.Recipe}}, nil
}

// QualityScore 是一炉的品质分(不含随机数)：丹室等级、火候、调火与熟练。
func (h *Hero) QualityScore(b *Brew) int {
	spec, _ := fireSpec(b.Fire)
	score := furnaceScore*h.Cave[RoomFurnace] + spec.Score + masteryScore*min(h.Mastery[b.Recipe], masteryMax)
	if b.Stoked {
		score += stokeBonus
	}
	return score
}

// collect 收一炉炼好的丹：武火先看炸没炸炉，再按品质分加一个 0-99 的随机数定品级。
func (h *Hero) collect(s *Sect, index int, now int64) ([]Ev, error) {
	if index < 0 || index >= len(h.Furnaces) {
		return nil, ErrAction
	}
	b := h.Furnaces[index]
	if now < b.End {
		return nil, ErrUnavailable
	}
	recipe, _ := elixirRecipe(b.Recipe)
	spec, _ := fireSpec(b.Fire)
	h.Furnaces = slices.Delete(h.Furnaces, index, index+1)
	if spec.Explode > 0 && s.Rng.Chance(spec.Explode) {
		return []Ev{{K: "hero_brew_fail", S: b.Recipe}}, nil
	}
	grade := GradeHigh
	if recipe.Graded {
		score := h.QualityScore(b) + s.Rng.Intn(100)
		switch {
		case score >= gradeHigh:
			grade = GradeHigh
		case score >= gradeMid:
			grade = GradeMid
		default:
			grade = GradeLow
		}
	}
	stock := h.Elixirs[b.Recipe]
	made := min(recipe.Yield, MaxElixirStack-stock[grade])
	stock[grade] += made
	h.Elixirs[b.Recipe] = stock
	h.Mastery[b.Recipe] = min(h.Mastery[b.Recipe]+1, masteryMax)
	return []Ev{{K: "hero_elixir", S: b.Recipe, N: made, V: grade}}, nil
}

// takeElixir 服一颗丹室炼的丹。
func (h *Hero) takeElixir(s *Sect, id string, grade int, now int64) ([]Ev, error) {
	recipe, ok := elixirRecipe(id)
	if !ok || grade < 0 || grade >= Grades {
		return nil, ErrAction
	}
	stock := h.Elixirs[id]
	if stock[grade] < 1 {
		return nil, ErrCost
	}
	power := recipe.Effect[grade]
	var evs []Ev
	switch id {
	case ElixirTemper:
		room := TemperCap(h.Realm) - h.Tempered
		if room <= 0 {
			return nil, ErrUnavailable
		}
		gain := min(power, room)
		h.Tempered += gain
		evs = append(evs, Ev{K: "hero_temper", N: gain})
	case ElixirGuard, ElixirTiger:
		h.Buffs[id] = &Buff{Power: power, Left: recipe.Battles[grade]}
		evs = append(evs, Ev{K: "hero_buff", S: id, N: recipe.Battles[grade]})
	case ElixirInsight:
		if h.Bottleneck() || h.QiReady > now {
			return nil, ErrUnavailable
		}
		h.QiReady = now + qiPillCooldown
		gain := h.CultivateRate(s) * float64(power)
		evs = append(evs, Ev{K: "hero_exp", N: int(math.Round(gain))})
		evs = append(evs, h.addExp(gain)...)
	case ElixirGolden:
		if h.Golden >= GoldenMax {
			return nil, ErrUnavailable
		}
		h.Golden++
		evs = append(evs, Ev{K: "hero_golden", N: h.Golden})
	}
	stock[grade]--
	if stock == ([3]int{}) {
		delete(h.Elixirs, id)
	} else {
		h.Elixirs[id] = stock
	}
	return append([]Ev{{K: "hero_take", S: id, V: grade}}, evs...), nil
}

// buffPower 是身上的金刚丹、龙虎丹现在给的开战护体与力量。
func (h *Hero) buffPower() (block, strength int) {
	if buff := h.Buffs[ElixirGuard]; buff != nil {
		block = buff.Power
	}
	if buff := h.Buffs[ElixirTiger]; buff != nil {
		strength = buff.Power
	}
	return block, strength
}

// spendBuffs 开战时给身上每种药力扣掉一场，用完的去掉。
func (h *Hero) spendBuffs() {
	for id, buff := range h.Buffs {
		buff.Left--
		if buff.Left <= 0 {
			delete(h.Buffs, id)
		}
	}
}

// ElixirRules 是炼丹的规则，页面按它显示丹方、火候与品质。
type ElixirRules struct {
	Recipes      []ElixirRecipe `json:"recipes"`
	Fires        []FireSpec     `json:"fires"`
	StokeBonus   int            `json:"stoke_bonus"`
	FurnaceScore int            `json:"furnace_score"`
	MasteryScore int            `json:"mastery_score"`
	MasteryMax   int            `json:"mastery_max"`
	GradeMid     int            `json:"grade_mid"`
	GradeHigh    int            `json:"grade_high"`
	GoldenMax    int            `json:"golden_max"`
	Slots        []int          `json:"slots"`
}

// ElixirRulesOf 返回炼丹的规则；Slots[L] 是丹室 L 级有几座丹炉。
func ElixirRulesOf() ElixirRules {
	rules := ElixirRules{Recipes: ElixirRecipes, Fires: Fires, StokeBonus: stokeBonus, FurnaceScore: furnaceScore,
		MasteryScore: masteryScore, MasteryMax: masteryMax, GradeMid: gradeMid, GradeHigh: gradeHigh, GoldenMax: GoldenMax}
	for level := 0; level <= MaxRoomLevel; level++ {
		rules.Slots = append(rules.Slots, FurnaceSlots(level))
	}
	return rules
}
