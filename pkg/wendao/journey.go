package wendao

import "github.com/QuantumNous/new-api/pkg/dungeon"

// 历练的数值表：章节、地图上的节点与它们的奖励。节点的名字、位置与剧情都在前端 Games/wendao/journey/text.js，
// 标识与这里一致。世界设定见 world.md。

// 节点种类。剧情看完就算走过；遭遇、强敌与首领是卡牌战斗；采集隔一段时间可以再去一次。
const (
	NodeStory  = "story"
	NodeCombat = dungeon.NodeCombat
	NodeElite  = dungeon.NodeElite
	NodeBoss   = dungeon.NodeBoss
	NodeGather = "gather"
)

// 历练的上限与节奏。
const (
	// Chapters 是现在开放的章节数，第 c 章(从 0 起)对应境界 c，敌人是九幽秘境第 c+1 重的。
	Chapters = 3
	// DeckMin、DeckMax 是牌组的张数范围，MaxCards 是功法收藏最多几张(满了以后赢了也不能再收下功法)。
	DeckMin  = 8
	DeckMax  = 30
	MaxCards = 80
	// MaxPotionStack 是一种丹符最多存几份。
	MaxPotionStack = 9
	// DailyRepeatRewards 是每天前几次重复通过节点(战斗或采集)还有资源；修为不限次数。
	DailyRepeatRewards = 10
	// BattleRewardChoices 是赢下一场之后几选一的功法。
	BattleRewardChoices = 3
	// heroLayerHP、heroRealmHP 是主角每多一层、每高一个境界多出的最大生命；境界还给开战时的力量。
	heroLayerHP = 5
	heroRealmHP = 30
	// heroExpPerHour 是主角闭关每小时的修为(按聚灵阵加快，与弟子闭关相同)。
	heroExpPerHour = baseExpPerHour
)

// nodeScale 是历练敌人的生命与伤害(百分比)：主角每一场都满血开打，还有境界给的生命与力量，所以敌人比九幽秘境里同一重
// 的强；章节每往后一章再加 chapterScaleHP、chapterScaleDmg。按贪心机器人模拟，遭遇战基本必胜，强敌与首领第一次挑战
// 五到九成能赢，打不过的可以回头刷修为、参悟功法再来。
var nodeScale = map[string][2]int{NodeCombat: {150, 130}, NodeElite: {200, 160}, NodeBoss: {130, 115}}

const (
	chapterScaleHP  = 20
	chapterScaleDmg = 10
)

// EnemyScale 返回一个战斗节点敌人的生命与伤害百分比。
func EnemyScale(n *JourneyNode) (hp, dmg int) {
	scale := nodeScale[n.Kind]
	return scale[0] + chapterScaleHP*n.Chapter, scale[1] + chapterScaleDmg*n.Chapter
}

// BreakKeys 是主角从境界 r 突破到 r+1 要用掉的机缘，由第 r+1 章的首领掉落；空字符串表示还没有开放。
var BreakKeys = [...]string{"foundation_dew", "core_fire", "nascent_lotus", ""}

// 宗门与历练互通：随行弟子、丹房炼丹符、聚气丹。
const (
	// CompanionMax 是一场仗最多带几名弟子随行，每人带来一张按灵根的援护牌(dungeon.AllyCards)，弟子到了筑基就是升级后的。
	CompanionMax = 2
	// companionInjury 是输了(或退走)时随行弟子受伤多久。
	companionInjury = 60 * 60
)

// PotionRecipe 是在宗门丹房给主角炼一份丹符：丹房要几级、花多少。
type PotionRecipe struct {
	Potion  string `json:"potion"`
	Alchemy int    `json:"alchemy"`
	Cost    Cost   `json:"cost"`
}

// PotionRecipes 是丹房能炼的丹符，按丹房等级排列。
var PotionRecipes = []PotionRecipe{
	{Potion: dungeon.PotionHeal, Alchemy: 1, Cost: Cost{Stone: 20, Herb: 40}},
	{Potion: dungeon.PotionFire, Alchemy: 1, Cost: Cost{Stone: 40, Ore: 10}},
	{Potion: dungeon.PotionIron, Alchemy: 2, Cost: Cost{Stone: 30, Ore: 30}},
	{Potion: dungeon.PotionWeakness, Alchemy: 2, Cost: Cost{Herb: 40, Ore: 20}},
	{Potion: dungeon.PotionStrength, Alchemy: 2, Cost: Cost{Herb: 60, Ore: 20}},
	{Potion: dungeon.PotionSwift, Alchemy: 3, Cost: Cost{Stone: 50, Herb: 50}},
	{Potion: dungeon.PotionEnergy, Alchemy: 3, Cost: Cost{Stone: 60, Herb: 80}},
}

// UpgradeCost 是参悟一张功法(升级)要花的灵石，按稀有度：初始、普通、罕见、稀有。
var UpgradeCost = [...]int{30, 60, 150, 300}

// JourneyNode 是历练地图上的一个节点。Requires 里的节点都走过才能去；Exp 与 First 是第一次通过的修为与资源，之后每次
// 修为减半、资源按 Repeat(每天前 DailyRepeatRewards 次)。Relic、Potions、Key 只在第一次通过时给。Cooldown 是采集的间隔。
type JourneyNode struct {
	Id       string   `json:"id"`
	Chapter  int      `json:"chapter"`
	Kind     string   `json:"kind"`
	Requires []string `json:"requires"`
	Enemies  []string `json:"enemies,omitempty"`
	Exp      int      `json:"exp"`
	First    Cost     `json:"first"`
	Repeat   Cost     `json:"repeat"`
	Relic    string   `json:"relic,omitempty"`
	Potions  []string `json:"potions,omitempty"`
	Key      string   `json:"key,omitempty"`
	Cooldown int64    `json:"cooldown,omitempty"`
}

// 各种节点第一次通过的修为，乘上章节对应境界的修为倍数 realmExp。
var nodeExp = map[string]int{NodeStory: 0, NodeCombat: 40, NodeElite: 100, NodeBoss: 250, NodeGather: 15}

func node(id string, chapter int, kind string, requires []string, enemies []string, first, repeat Cost) JourneyNode {
	return JourneyNode{Id: id, Chapter: chapter, Kind: kind, Requires: requires, Enemies: enemies,
		Exp: int(float64(nodeExp[kind]) * realmExp[chapter]), First: first, Repeat: repeat}
}

func withRelic(n JourneyNode, relic string) JourneyNode {
	n.Relic = relic
	return n
}

func withPotions(n JourneyNode, potions ...string) JourneyNode {
	n.Potions = potions
	return n
}

func withKey(n JourneyNode, key string) JourneyNode {
	n.Key = key
	return n
}

func gather(id string, chapter int, requires []string, yield Cost, hours int64) JourneyNode {
	n := node(id, chapter, NodeGather, requires, nil, yield, yield)
	n.Cooldown = hours * 60 * 60
	return n
}

func req(ids ...string) []string { return append([]string{}, ids...) }

// JourneyNodes 是所有章节的节点，按章节与剧情顺序排列。
var JourneyNodes = []JourneyNode{
	// 第一章 · 青岚初试(炼气)：青岚山、落枫镇、黑松林、雾隐谷。
	node("c1_oath", 0, NodeStory, req(), nil, Cost{Stone: 50}, Cost{}),
	withPotions(node("c1_backhill", 0, NodeCombat, req("c1_oath"), []string{"slime"}, Cost{Stone: 40}, Cost{Stone: 10}), dungeon.PotionHeal),
	node("c1_town", 0, NodeStory, req("c1_backhill"), nil, Cost{Herb: 40}, Cost{}),
	node("c1_imps", 0, NodeCombat, req("c1_town"), []string{"goblin", "goblin"}, Cost{Stone: 50, Herb: 30}, Cost{Stone: 12, Herb: 8}),
	gather("c1_herbs", 0, req("c1_imps"), Cost{Herb: 60}, 4),
	node("c1_moths", 0, NodeCombat, req("c1_imps"), []string{"moth", "moth"}, Cost{Stone: 50, Herb: 40}, Cost{Stone: 12, Herb: 10}),
	withRelic(node("c1_chief", 0, NodeElite, req("c1_imps"), []string{"goblin_chief"}, Cost{Stone: 120, Ore: 30, Rep: 5}, Cost{Stone: 30, Ore: 8}), dungeon.RelicWhetstone),
	withPotions(node("c1_bones", 0, NodeCombat, req("c1_moths"), []string{"skeleton", "moth"}, Cost{Stone: 60, Ore: 20}, Cost{Stone: 15, Ore: 5}), dungeon.PotionFire),
	node("c1_valley", 0, NodeStory, req("c1_bones", "c1_chief"), nil, Cost{}, Cost{}),
	withRelic(node("c1_gargoyle", 0, NodeElite, req("c1_valley"), []string{"gargoyle"}, Cost{Stone: 150, Ore: 40, Rep: 5}, Cost{Stone: 35, Ore: 10}), dungeon.RelicIronPlate),
	withKey(withRelic(node("c1_boss", 0, NodeBoss, req("c1_gargoyle"), []string{"slime_king"}, Cost{Stone: 300, Herb: 150, Ore: 80, Rep: 20}, Cost{Stone: 60, Herb: 30, Ore: 15}), dungeon.RelicJade), BreakKeys[0]),
	node("c1_rift", 0, NodeStory, req("c1_boss"), nil, Cost{Rep: 10}, Cost{}),

	// 第二章 · 赤霞烽火(筑基)：赤霞原、火云岭。
	node("c2_arrive", 1, NodeStory, req("c1_rift"), nil, Cost{Stone: 150}, Cost{}),
	node("c2_wolves", 1, NodeCombat, req("c2_arrive"), []string{"werewolf"}, Cost{Stone: 150, Herb: 60}, Cost{Stone: 35, Herb: 15}),
	withPotions(node("c2_shade", 1, NodeCombat, req("c2_wolves"), []string{"shade_mage", "beetle"}, Cost{Stone: 160, Ore: 40}, Cost{Stone: 40, Ore: 10}), dungeon.PotionStrength),
	node("c2_lava", 1, NodeCombat, req("c2_wolves"), []string{"lava_elemental"}, Cost{Stone: 160, Herb: 80}, Cost{Stone: 40, Herb: 20}),
	gather("c2_mine", 1, req("c2_lava"), Cost{Ore: 120}, 4),
	node("c2_beetles", 1, NodeCombat, req("c2_shade"), []string{"beetle", "beetle"}, Cost{Stone: 170, Ore: 60}, Cost{Stone: 40, Ore: 15}),
	withRelic(node("c2_troll", 1, NodeElite, req("c2_lava"), []string{"troll"}, Cost{Stone: 400, Ore: 120, Rep: 10}, Cost{Stone: 90, Ore: 30}), dungeon.RelicHawkEye),
	withPotions(node("c2_ambush", 1, NodeCombat, req("c2_beetles", "c2_troll"), []string{"werewolf", "shade_mage"}, Cost{Stone: 200, Herb: 100}, Cost{Stone: 45, Herb: 25}), dungeon.PotionIron),
	withRelic(node("c2_knight", 1, NodeElite, req("c2_ambush"), []string{"death_knight"}, Cost{Stone: 450, Ore: 150, Rep: 10}, Cost{Stone: 100, Ore: 35}), dungeon.RelicRune),
	withKey(withRelic(node("c2_boss", 1, NodeBoss, req("c2_knight"), []string{"flame_lord"}, Cost{Stone: 900, Herb: 400, Ore: 250, Rep: 40}, Cost{Stone: 180, Herb: 80, Ore: 50}), dungeon.RelicFang), BreakKeys[1]),
	node("c2_epilogue", 1, NodeStory, req("c2_boss"), nil, Cost{Rep: 20}, Cost{}),

	// 第三章 · 寒渊(金丹)：北冥寒渊。
	node("c3_arrive", 2, NodeStory, req("c2_epilogue"), nil, Cost{Stone: 400}, Cost{}),
	node("c3_guard", 2, NodeCombat, req("c3_arrive"), []string{"abyss_guard"}, Cost{Stone: 400, Ore: 120}, Cost{Stone: 90, Ore: 30}),
	node("c3_shades", 2, NodeCombat, req("c3_guard"), []string{"void_walker", "void_walker"}, Cost{Stone: 420, Herb: 200}, Cost{Stone: 95, Herb: 45}),
	withPotions(node("c3_eater", 2, NodeCombat, req("c3_guard"), []string{"soul_eater"}, Cost{Stone: 420, Herb: 200}, Cost{Stone: 95, Herb: 45}), dungeon.PotionEnergy),
	gather("c3_spring", 2, req("c3_eater"), Cost{Stone: 150, Herb: 200}, 4),
	node("c3_golem", 2, NodeCombat, req("c3_shades"), []string{"crystal_golem"}, Cost{Stone: 450, Ore: 200}, Cost{Stone: 100, Ore: 45}),
	withRelic(node("c3_lich", 2, NodeElite, req("c3_eater"), []string{"lich"}, Cost{Stone: 1000, Herb: 300, Rep: 20}, Cost{Stone: 220, Herb: 70}), dungeon.RelicMetronome),
	withPotions(node("c3_patrol", 2, NodeCombat, req("c3_golem", "c3_lich"), []string{"abyss_guard", "void_walker"}, Cost{Stone: 500, Ore: 220}, Cost{Stone: 110, Ore: 50}), dungeon.PotionSwift),
	withKey(withRelic(node("c3_drake", 2, NodeBoss, req("c3_patrol"), []string{"abyss_drake"}, Cost{Stone: 2400, Herb: 900, Ore: 700, Rep: 80}, Cost{Stone: 450, Herb: 180, Ore: 140}), dungeon.RelicCrystal), BreakKeys[2]),
	node("c3_epilogue", 2, NodeStory, req("c3_drake"), nil, Cost{Rep: 40}, Cost{}),
}

var journeyNodeIndex = func() map[string]*JourneyNode {
	index := make(map[string]*JourneyNode, len(JourneyNodes))
	for i := range JourneyNodes {
		index[JourneyNodes[i].Id] = &JourneyNodes[i]
	}
	return index
}()

// JourneyNodeOf 返回节点，不认识的标识返回 nil。
func JourneyNodeOf(id string) *JourneyNode { return journeyNodeIndex[id] }
