package wendao

import "math"

// 洞府：宗主本人修行的地方，几间屋子各管一件事，花宗门库房的资源扩建，等级受主角境界限制。
//   - 静室：闭关更快(与宗门聚灵阵的加成相加)。
//   - 灵泉：最大生命更多。
//   - 丹室：开炉炼主角自己服的丹(elixir.go)，等级决定能炼哪些丹方、品质与几座丹炉。
//   - 器室：祭炼法宝(forge.go)，法宝最多祭炼到器室的等级。
//   - 兽苑：养灵兽(beast.go)，等级决定能养几只、灵兽最多练到几级。
// 洞府只管主角，不发游戏币；它给的生命、力量与护体只在历练(含问心阶)里生效，带不进九幽秘境。

const (
	RoomQuiet   = "quiet"
	RoomSpring  = "spring"
	RoomFurnace = "furnace"
	RoomForge   = "forge"
	// MaxRoomLevel 是洞府每间屋子的最高等级。
	MaxRoomLevel = 10
	// quietCultivate 是静室每级让主角闭关快多少，springHP 是灵泉每级多几点最大生命。
	quietCultivate = 0.06
	springHP       = 3
)

// RoomOrder 是洞府的屋子，按页面上的顺序。
var RoomOrder = []string{RoomQuiet, RoomSpring, RoomFurnace, RoomForge, RoomDen}

type roomSpec struct {
	// Base 是建到 1 级的花费：建到 L 级花 Base × Growth^(L-1)。
	Base   Cost
	Growth float64
}

var roomSpecs = map[string]roomSpec{
	RoomQuiet:   {Base: Cost{Stone: 200, Herb: 60}, Growth: 1.8},
	RoomSpring:  {Base: Cost{Stone: 160, Herb: 120}, Growth: 1.8},
	RoomFurnace: {Base: Cost{Stone: 180, Herb: 100, Ore: 40}, Growth: 1.8},
	RoomForge:   {Base: Cost{Stone: 180, Ore: 120}, Growth: 1.8},
	RoomDen:     {Base: Cost{Stone: 220, Herb: 160}, Growth: 1.8},
}

// RoomCost 返回把洞府的一间屋子建到 level 级要花多少。
func RoomCost(room string, level int) Cost {
	spec := roomSpecs[room]
	return spec.Base.scale(math.Pow(spec.Growth, float64(level-1)))
}

// RoomCap 是主角在某个境界能把洞府的屋子建到几级：炼气 2 级，每高一个境界多两级，化神到顶。
func RoomCap(realm int) int { return min(2*(realm+1), MaxRoomLevel) }

// buildRoom 花宗门的资源把洞府的一间屋子扩建一级。
func (h *Hero) buildRoom(s *Sect, room string) ([]Ev, error) {
	if _, ok := roomSpecs[room]; !ok {
		return nil, ErrAction
	}
	next := h.Cave[room] + 1
	if next > RoomCap(h.Realm) {
		return nil, ErrUnavailable
	}
	cost := RoomCost(room, next)
	if err := s.pay(cost); err != nil {
		return nil, err
	}
	h.Cave[room] = next
	s.Progress(TaskBuild, 1)
	return append(spentEvs(cost), Ev{K: "hero_room", S: room, N: next}), nil
}

// spentEvs 是花掉一笔资源对应的事。
func spentEvs(c Cost) []Ev {
	var evs []Ev
	for _, spent := range []struct {
		key string
		n   int
	}{{"stone", c.Stone}, {"herb", c.Herb}, {"ore", c.Ore}, {"rep", c.Rep}} {
		if spent.n > 0 {
			evs = append(evs, Ev{K: "res", S: spent.key, N: -spent.n})
		}
	}
	return evs
}

// CaveRules 是洞府的规则，页面按它显示每一级的花费与效果。
type CaveRules struct {
	Rooms    []string          `json:"rooms"`
	Costs    map[string][]Cost `json:"costs"`
	MaxLevel int               `json:"max_level"`
	QuietPct int               `json:"quiet_pct"`
	SpringHP int               `json:"spring_hp"`
}

// CaveRulesOf 返回洞府的规则：Costs[room][L-1] 是建到 L 级的花费。
func CaveRulesOf() CaveRules {
	rules := CaveRules{Rooms: RoomOrder, Costs: map[string][]Cost{}, MaxLevel: MaxRoomLevel,
		QuietPct: int(math.Round(quietCultivate * 100)), SpringHP: springHP}
	for _, room := range RoomOrder {
		for level := 1; level <= MaxRoomLevel; level++ {
			rules.Costs[room] = append(rules.Costs[room], RoomCost(room, level))
		}
	}
	return rules
}
