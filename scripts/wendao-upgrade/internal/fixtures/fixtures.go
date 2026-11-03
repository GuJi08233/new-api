// Package fixtures 提供固定时间、种子与业务上限的合成演练样本，不读取玩家数据。
package fixtures

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
)

const Now int64 = 1793664000 // 2026-11-03 00:00:00 UTC；不依赖执行机日期或时区。
const Day = "2026-11-03"

type Sample struct {
	Name string
	Kind string
	Data string
}

// Build 的 current_* 是容量较大的合法组合，不声称覆盖没有业务总上限的全部历史记录。
// 各活动单独保存，避免把互斥活动拼进同一份“最大存档”。
func Build() ([]Sample, error) {
	result := []Sample{
		{Name: "empty", Kind: "save", Data: ""},
		{Name: "legacy_sect", Kind: "save", Data: `{"sect":{"version":1,"rng":{"s":731},"created":1793660400,"last":1793660400,"buildings":{"hall":3},"res":{"stone":120,"herb":80,"ore":40,"rep":10},"disciples":[{"id":1,"name":[1,2,3],"realm":1,"layer":4,"talent":3,"job":"cultivate"}]}}`},
		{Name: "legacy_cards", Kind: "save", Data: `{"schema":7,"sect":{"version":1,"buildings":{"hall":3},"res":{"stone":120}},"hero":{"class":"mage","realm":1,"layer":4,"cards":[{"id":"m_bolt","up":true}],"deck":[0],"steps":7,"nodes":{"c1_oath":{"clears":1,"last":1793660400}},"battle":{"node":"c1_backhill","run":{"potions":["fire","heal"]}}},"abyss":{"crystals":30,"earned":200,"runs":9,"classes":["warrior","mage"],"talents":{"vitality":2,"relic":1,"purse":1},"items":{"elixir":2,"potion_crate":3,"gold_pouch":1,"relic_box":4,"ward":1}}}`},
		{Name: "future", Kind: "save", Data: fmt.Sprintf(`{"schema":%d,"sect":{"res":{"stone":123}},"unknown_future_progress":{"keep":true}}`, wendao.SaveSchema+1)},
		{Name: "corrupt", Kind: "save", Data: `{"schema":13,"hero":`},
	}
	sect := wendao.NewSect(731, Now)
	for _, building := range wendao.BuildingOrder {
		sect.Buildings[building] = wendao.MaxBuildingLevel
	}
	sect.Res = wendao.Cost{Stone: 123456789, Herb: 98765432, Ore: 87654321, Rep: 7654321}
	sect.Disciples = nil
	for i := range sect.Cap() {
		d := &wendao.Disciple{Id: i + 1, Name: [3]int{i % wendao.NameSurnames, i, i + 1},
			Element: i % wendao.Elements, Talent: 3, Trait: wendao.Traits[i%len(wendao.Traits)],
			Realm: wendao.MaxRealm, Layer: wendao.MaxLayer, Exp: 123.5, Job: wendao.JobCultivate,
			Joined: Now - 86400, QiReady: Now + 3600, Generation: 1}
		if i >= 1 && i <= 2 {
			name := sect.Disciples[0].Name
			d.TeacherId, d.TeacherName, d.Generation = 1, &name, 2
			d.Realm = wendao.LineageMinRealm
		}
		sect.Disciples = append(sect.Disciples, d)
	}
	sect.NextId = len(sect.Disciples) + 1
	for region := 1; region < len(wendao.Regions); region++ {
		sect.Outposts = append(sect.Outposts, wendao.Outpost{Region: region, Created: Now - 3600})
		d := sect.Disciples[region+2]
		d.Outpost, d.OutpostToken = region, fmt.Sprintf("00000000-0000-4000-8000-%012d", region)
	}
	for i := range sect.MissionSlots() {
		d := sect.Disciples[7+i]
		d.Mission = i + 1
		sect.Missions = append(sect.Missions, &wendao.Mission{Id: i + 1, Key: "herbs", Team: []int{d.Id}, Start: Now, End: Now + 3600})
	}
	for _, pill := range wendao.Pills {
		sect.Pills[pill.Key] = 99
	}
	for _, rare := range []string{wendao.RareFruit, wendao.RareLingzhi, wendao.RareLotus} {
		sect.Rare[rare] = 99
	}
	for i := range wendao.MaxLog {
		name := sect.Disciples[i%len(sect.Disciples)].Name
		sect.Log = append(sect.Log, wendao.Ev{K: "lineage_recruit", S: "cultivate", N: i, Who: &name, At: Now - int64(i)})
	}
	sect.Garden.Plots = nil
	for i := range wendao.PlotCount(wendao.MaxBuildingLevel) {
		crop := wendao.Crops[i%len(wendao.Crops)].Key
		sect.Garden.Plots = append(sect.Garden.Plots, &wendao.Plot{Crop: "qingcao", Planted: Now - 3600,
			Ripe: Now + 3600, Waters: []int{101, 102, 103}})
		sect.Garden.Harvests[crop] = 999
	}
	for range 12 { // garden.go 的来访记录上限；属于样本规模，不新增业务上限。
		sect.Garden.Log = append(sect.Garden.Log, wendao.GardenVisit{Name: "问道演练道友", Kind: "water", Crop: "qingcao", At: Now})
	}
	sect.RollDaily(Day, "", true)
	hero, err := wendao.NewHero(dungeon.ClassMage, Now)
	if err != nil {
		return nil, err
	}
	hero.Realm, hero.Layer, hero.Steps = wendao.MaxRealm, wendao.MaxLayer, wendao.MaxSteps-1
	starter := dungeon.StarterDeck(hero.Class)
	hero.Cards, hero.Deck = nil, nil
	for i := range wendao.MaxCards {
		card := starter[i%len(starter)]
		card.Up = true
		hero.Cards = append(hero.Cards, card)
		if i < wendao.DeckMax {
			hero.Deck = append(hero.Deck, i)
		}
	}
	for range wendao.MaxPresets {
		hero.Presets = append(hero.Presets, &wendao.DeckPreset{Name: strings.Repeat("问", wendao.MaxPresetName), Deck: append([]int(nil), hero.Deck...)})
	}
	for _, room := range wendao.RoomOrder {
		hero.Cave[room] = wendao.MaxRoomLevel
	}
	hero.Relics = dungeon.NewCatalog().Relics
	hero.Worn = []string{dungeon.RelicCrystal, dungeon.RelicIronPlate, dungeon.RelicJade, dungeon.RelicRune, dungeon.RelicMetronome, dungeon.RelicPotionBelt}
	for _, relic := range hero.Relics {
		hero.Refine[relic], hero.Bless[relic] = wendao.MaxRoomLevel, 0
	}
	for _, potion := range dungeon.Potions() {
		hero.Potions[potion] = wendao.MaxPotionStack
	}
	for _, node := range wendao.JourneyNodes {
		hero.Nodes[node.Id] = &wendao.NodeRecord{Clears: 1, Last: Now - 3600}
	}
	for _, key := range wendao.BreakKeys {
		hero.Keys[key] = 1
	}
	for i, recipe := range wendao.ElixirRecipes {
		hero.Elixirs[recipe.Id] = [3]int{99, 99, 99}
		hero.Mastery[recipe.Id] = 10
		if i < wendao.FurnaceSlots(wendao.MaxRoomLevel) {
			hero.Furnaces = append(hero.Furnaces, &wendao.Brew{Recipe: recipe.Id, Fire: wendao.Fires[0].Key, Start: Now, End: Now + int64(recipe.Hours)*3600, Stoked: true})
		}
	}
	for i := range wendao.BeastSlots(wendao.MaxRoomLevel) {
		hero.Beasts = append(hero.Beasts, &wendao.Beast{Id: i + 1, Kind: wendao.BeastKinds[i], Name: strings.Repeat("问", wendao.MaxBeastName),
			Aptitude: 5, Level: wendao.MaxBeastLevel, Stage: 2, Bond: wendao.MaxBond, Exp: 123.5, FedDay: Day, Fed: wendao.BeastFeeds})
	}
	hero.BeastOut, hero.NextBeast = 1, len(hero.Beasts)
	hero.Tempered, hero.Golden = wendao.TemperCap(hero.Realm), wendao.GoldenMax
	hero.Buffs[wendao.ElixirGuard] = &wendao.Buff{Power: 16, Left: 3}
	hero.Buffs[wendao.ElixirTiger] = &wendao.Buff{Power: 1, Left: 5}
	hero.Arms = &wendao.Arms{Weapons: []string{"sword", "flying_sword", "talisman", "spear", "thunder"}, Skills: []string{"quake", "nova", "blades"},
		Loadout: &wendao.Loadout{Weapons: []string{"thunder", "spear"}, Skill: "blades"}}
	save := &wendao.Save{Schema: wendao.SaveSchema, Sect: sect, Hero: hero, Seen: Now,
		Rift: &doufa.Meta{Crystals: 12345, Earned: 54321, Runs: 456, Items: map[string]int{doufa.ItemElixir: 99, doufa.ItemFortune: 99, doufa.ItemWard: 99}}}
	save.Rift.Normalize()
	for _, talent := range doufa.Talents() {
		save.Rift.Talents[talent.Id] = len(talent.Costs)
	}
	save.RollDuels(Day)
	for i := range wendao.ReceiptKeep {
		if err := save.Admit(fmt.Sprintf("00000000-0000-4000-8000-%012d", i), strings.Repeat("a", 32), Now-int64(wendao.ReceiptKeep-i)); err != nil {
			return nil, err
		}
	}
	save.Back = &wendao.ReturnReport{Id: "00000000-0000-4000-8000-000000000999", Since: Now - 86400, At: Now, CapSeconds: wendao.OfflineCap,
		Cultivation: &wendao.ReturnCultivation{Kind: "cave", Seconds: wendao.OfflineCap, Exp: 1234.5, Capped: true},
		Sect:        &wendao.ReturnSect{Seconds: wendao.OfflineCap, Res: wendao.Cost{Stone: 1234, Herb: 567, Ore: 890}, Capped: true}}
	if _, err := save.HeroApply(wendao.HeroAction{Type: "climb", Team: []int{1, 2}}, Now, Day, 731); err != nil {
		return nil, fmt.Errorf("current journey fixture: %w", err)
	}
	hero.Battle.Token = "00000000-0000-4000-8000-000000000731"
	hero.Battle.RequestId = "00000000-0000-4000-8000-000000000732"
	base, err := save.Encode()
	if err != nil {
		return nil, err
	}
	result = append(result, Sample{Name: "current_journey", Kind: "save", Data: base})
	for _, activity := range []string{"travel", "meditation", "contract", "outposts", "hatching"} {
		variant, err := wendao.ParseSave(base)
		if err != nil {
			return nil, err
		}
		variant.Hero.Battle = nil
		switch activity {
		case "travel":
			variant.Hero.Travel = &wendao.Travel{Token: "00000000-0000-4000-8000-000000000733", Region: 4, Started: Now, Rate: variant.Hero.TravelRate(variant.Sect, 4)}
		case "meditation":
			variant.Hero.Meditation = &wendao.Meditation{Token: "00000000-0000-4000-8000-000000000734", Region: 4, Started: Now}
		case "contract":
			if _, err := variant.StartDuel(wendao.DuelBounty, "b1", 731, Now); err != nil {
				return nil, err
			}
		case "outposts":
			variant.Sect.Missions = nil
			for _, d := range variant.Sect.Disciples {
				d.Mission, d.Outpost, d.OutpostToken = 0, 0, ""
			}
			for i, d := range variant.Sect.Disciples[4:] {
				d.Mission, d.Outpost = 0, 1+i/wendao.OutpostSlots
				d.OutpostToken = fmt.Sprintf("00000000-0000-4000-8000-%012d", d.Id)
			}
		case "hatching":
			beast := variant.Hero.Beasts[len(variant.Hero.Beasts)-1]
			beast.Hatch, beast.Level, beast.Stage, beast.Exp, beast.Bond = Now+wendao.BeastHatch, 1, 0, 0, 0
		}
		encoded, err := variant.Encode()
		if err != nil {
			return nil, err
		}
		result = append(result, Sample{Name: "current_" + activity, Kind: "save", Data: encoded})
	}
	rift, err := doufa.NewRun(731, dungeon.ClassMage, nil)
	if err != nil {
		return nil, err
	}
	if err := rift.Enter(0, Now); err != nil {
		return nil, err
	}
	// GameSession.State 使用控制器的真实外层形状；规则对象本身不是可恢复的会话载荷。
	data, err := common.Marshal(struct {
		Run *doufa.Run `json:"run"`
	}{Run: rift})
	if err != nil {
		return nil, err
	}
	result = append(result, Sample{Name: "rift", Kind: "session", Data: string(data)})
	tournament, err := wendao.NewTournament(731, sect, []int{1, 2, 3}, Now)
	if err != nil {
		return nil, err
	}
	data, err = common.Marshal(struct {
		Steps      int                `json:"steps"`
		Tournament *wendao.Tournament `json:"tournament"`
	}{Tournament: tournament})
	if err != nil {
		return nil, err
	}
	return append(result, Sample{Name: "tournament", Kind: "session", Data: string(data)}), nil
}
