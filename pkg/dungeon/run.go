package dungeon

import (
	"errors"
	"slices"
)

// ErrAction 表示这一步在当前局面下不合法。
var ErrAction = errors.New("invalid dungeon action")

// StateVersion 是 Run 序列化格式的版本，规则改动导致旧存档不能继续时加一。
const StateVersion = 1

// 一轮远征的阶段。
const (
	PhaseMap    = "map"
	PhaseCombat = "combat"
	PhaseReward = "reward"
	PhaseShop   = "shop"
	PhaseRest   = "rest"
	PhaseEvent  = "event"
	PhaseWon    = "won"
	PhaseLost   = "lost"
	// PhaseDone 是练习模式一场战斗打完(Result 记胜负)，可以重来。
	PhaseDone = "done"
)

// 三种模式：无尽远征一幕接一幕往下打，按打败的首领数领奖；试炼是固定牌组的关卡，按星级领魂晶；练习只用来测试，没有奖励。
const (
	ModeEndless  = "endless"
	ModeTrial    = "trial"
	ModePractice = "practice"
)

// 路线图：每幕 Rows 层、每层最多 Cols 个节点，最后是首领。前 Acts 幕各有自己的敌人，之后进入无尽深渊。
const (
	Acts     = 3
	Rows     = 8
	Cols     = 4
	MaxAbyss = 10
)

// 节点类型。
const (
	NodeCombat   = "combat"
	NodeElite    = "elite"
	NodeBoss     = "boss"
	NodeShop     = "shop"
	NodeRest     = "rest"
	NodeEvent    = "event"
	NodeTreasure = "treasure"
)

// 天赋(局外成长)，效果在开局时生效，见 meta.go 的价格与等级上限。
const (
	TalentVitality = "vitality" // 每级最大生命 +4
	TalentPurse    = "purse"    // 每级初始金币 +25
	TalentDrill    = "drill"    // 每级随机升级 1 张初始牌
	TalentPotion   = "potion"   // 开局带一瓶随机药水
	TalentHaggle   = "haggle"   // 商店价格 -10%
	TalentMedic    = "medic"    // 休息额外回复 10% 最大生命
	TalentRelic    = "relic"    // 开局获得一件随机普通遗物
)

// Node 是路线图上的一个节点，Next 是下一层可以去的节点。
type Node struct {
	Row  int    `json:"row"`
	Col  int    `json:"col"`
	Type string `json:"type"`
	Next []int  `json:"next"`
}

// Reward 是战斗或宝箱的奖励：金币、遗物与药水已经到手，卡牌三选一(可跳过)。
type Reward struct {
	Gold       int    `json:"gold"`
	Relic      string `json:"relic,omitempty"`
	Potion     string `json:"potion,omitempty"`
	PotionLost bool   `json:"potion_lost,omitempty"` // 药水栏已满，药水没拿到
	Cards      []Card `json:"cards"`
	Boss       bool   `json:"boss,omitempty"`
}

// Offer 是商店里的一件商品。
type Offer struct {
	Kind  string `json:"kind"` // card、relic 或 potion
	Card  Card   `json:"card"`
	Id    string `json:"id"`
	Price int    `json:"price"`
	Sold  bool   `json:"sold"`
}

// Shop 是一家商店：每家店只能移除一张牌。
type Shop struct {
	Offers      []Offer `json:"offers"`
	RemovePrice int     `json:"remove_price"`
	Removed     bool    `json:"removed"`
}

// Options 是开局的选择与局外成长。
type Options struct {
	Class   string
	Abyss   int
	Talents map[string]int
}

// Run 是一轮远征的完整状态，序列化后由服务端保存。
type Run struct {
	Version int            `json:"v"`
	Rng     Rng            `json:"rng"`
	Mode    string         `json:"mode"`
	Class   string         `json:"class"`
	Abyss   int            `json:"abyss"`
	Talents map[string]int `json:"talents"`
	Act     int            `json:"act"`
	Floor   int            `json:"floor"`
	HP      int            `json:"hp"`
	MaxHP   int            `json:"max_hp"`
	Gold    int            `json:"gold"`
	Deck    []Card         `json:"deck"`
	Relics  []string       `json:"relics"`
	Potions []string       `json:"potions"`
	Phase   string         `json:"phase"`
	Map     []Node         `json:"map"`
	Path    []int          `json:"path"`
	Combat  *Combat        `json:"combat,omitempty"`
	Reward  *Reward        `json:"reward,omitempty"`
	Shop    *Shop          `json:"shop,omitempty"`
	Event   string         `json:"event,omitempty"`
	// Seen 是这一轮已经遇到过的事件，同一个事件不会重复出现(全部遇到过之后重新开始)。
	Seen        []string `json:"seen"`
	Kills       int      `json:"kills"`
	Elites      int      `json:"elites"`
	Bosses      int      `json:"bosses"`
	Removals    int      `json:"removals"`
	PhoenixUsed bool     `json:"phoenix_used,omitempty"`
	// ItemsUsed 是这一轮用了几件商店道具；Ward 是身上有一张还没生效的护命符，WardUsed 是护命符已经救过一次。
	ItemsUsed int  `json:"items_used,omitempty"`
	Ward      bool `json:"ward,omitempty"`
	WardUsed  bool `json:"ward_used,omitempty"`
	// Trial 是试炼的标识，Fight 是正在打第几场(从 0 开始)，Turns 是已经打完的几场一共用了几回合。
	Trial string `json:"trial,omitempty"`
	Fight int    `json:"fight,omitempty"`
	Turns int    `json:"turns,omitempty"`
	// Practice 是练习模式的设置，重来时按它重新开战；Result 是练习一场战斗的胜负。
	Practice *PracticeSetup `json:"practice,omitempty"`
	Result   string         `json:"result,omitempty"`
	// Events 是最近一次操作里发生的事，只用于这一次的回应，不保存。
	Events []Ev `json:"-"`
}

// Action 是玩家的一步操作。不用的字段由调用方填 -1。
type Action struct {
	Type   string
	Node   int
	Card   int
	Target int
	Choice int
	Potion int
}

// NewRun 用种子开一轮远征。
func NewRun(seed uint64, o Options) (*Run, error) {
	stats, ok := classStats[o.Class]
	if !ok || o.Abyss < 0 || o.Abyss > MaxAbyss {
		return nil, ErrAction
	}
	talents := map[string]int{}
	for id, level := range o.Talents {
		if level > 0 {
			talents[id] = level
		}
	}
	r := &Run{
		Version: StateVersion,
		Rng:     Rng{S: seed},
		Mode:    ModeEndless,
		Class:   o.Class,
		Abyss:   o.Abyss,
		Talents: talents,
		Act:     1,
		MaxHP:   stats.HP + 4*talents[TalentVitality],
		Gold:    80 + 25*talents[TalentPurse],
		Relics:  []string{},
		Potions: []string{},
		Path:    []int{},
		Seen:    []string{},
	}
	if r.Abyss >= 5 {
		r.MaxHP -= 5
	}
	r.HP = r.MaxHP
	for _, id := range starterDecks[o.Class] {
		r.Deck = append(r.Deck, Card{Id: id})
	}
	for range talents[TalentDrill] {
		r.upgradeRandom()
	}
	if r.Abyss >= 6 {
		r.Deck = append(r.Deck, Card{Id: DoubtCard})
	}
	r.gainRelic(stats.Relic)
	if talents[TalentRelic] > 0 {
		r.gainRelic(r.randomRelic(RarityCommon))
	}
	if talents[TalentPotion] > 0 {
		r.Potions = append(r.Potions, potionList[r.Rng.Intn(len(potionList))])
	}
	r.newAct()
	return r, nil
}

func (r *Run) hasRelic(id string) bool { return slices.Contains(r.Relics, id) }

func (r *Run) gainRelic(id string) {
	if id == "" || r.hasRelic(id) {
		return
	}
	r.Relics = append(r.Relics, id)
	if id == RelicJade {
		r.MaxHP += 10
		r.HP += 10
	}
	r.emit("relic", playerTarget, 0, id)
}

// PotionSlots 是药水栏的格数。
func (r *Run) PotionSlots() int {
	if r.hasRelic(RelicPotionBelt) {
		return 4
	}
	return 3
}

// randomRelic 返回一件还没有的遗物，优先给定稀有度，这一档都有了时往其他档找；全都有了返回空字符串。
func (r *Run) randomRelic(rarity int) string {
	for _, tier := range []int{rarity, RarityCommon, RarityUncommon, RarityRare} {
		var pool []string
		for _, id := range relicTiers[tier] {
			if !r.hasRelic(id) {
				pool = append(pool, id)
			}
		}
		if len(pool) > 0 {
			return pool[r.Rng.Intn(len(pool))]
		}
	}
	return ""
}

func (r *Run) rollRelicRarity() int {
	return []int{RarityCommon, RarityUncommon, RarityRare}[pickWeighted(&r.Rng, []int{50, 35, 15})]
}

// upgradeRandom 随机升级牌组里一张还能升级的牌，返回它的下标，没有可升级的牌时返回 -1。
func (r *Run) upgradeRandom() int {
	var candidates []int
	for i, c := range r.Deck {
		if upgradable(c) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return -1
	}
	i := candidates[r.Rng.Intn(len(candidates))]
	r.Deck[i].Up = true
	return i
}

func upgradable(c Card) bool { return !c.Up && c.def().Type != TypeCurse }

// newAct 生成这一幕的路线图。路线由四条从底到顶的随机路径叠成，相邻两层的连线不会交叉。
func (r *Run) newAct() {
	grid := [Rows][Cols]int{}
	for row := range grid {
		for col := range grid[row] {
			grid[row][col] = -1
		}
	}
	edges := map[[2]int]map[int]bool{} // (row, col) -> 下一层的列
	starts := []int{r.Rng.Intn(Cols)}
	second := r.Rng.Intn(Cols - 1)
	if second >= starts[0] {
		second++
	}
	starts = append(starts, second, r.Rng.Intn(Cols), r.Rng.Intn(Cols))
	for _, col := range starts {
		for row := 0; row < Rows; row++ {
			grid[row][col] = 0
			if row == Rows-1 {
				break
			}
			next := min(max(col+r.Rng.Intn(3)-1, 0), Cols-1)
			// 与已有的连线交叉时改为直走。
			if next == col+1 && edges[[2]int{row, col + 1}][col] {
				next = col
			}
			if next == col-1 && edges[[2]int{row, col - 1}][col] {
				next = col
			}
			key := [2]int{row, col}
			if edges[key] == nil {
				edges[key] = map[int]bool{}
			}
			edges[key][next] = true
			col = next
		}
	}
	r.Map = r.Map[:0]
	for row := range Rows {
		for col := range Cols {
			if grid[row][col] < 0 {
				continue
			}
			grid[row][col] = len(r.Map)
			r.Map = append(r.Map, Node{Row: row, Col: col, Type: r.rollNodeType(row), Next: []int{}})
		}
	}
	boss := len(r.Map)
	r.Map = append(r.Map, Node{Row: Rows, Col: Cols / 2, Type: NodeBoss, Next: []int{}})
	for i := range r.Map[:boss] {
		node := &r.Map[i]
		if node.Row == Rows-1 {
			node.Next = []int{boss}
			continue
		}
		for col := range Cols {
			if edges[[2]int{node.Row, node.Col}][col] {
				node.Next = append(node.Next, grid[node.Row+1][col])
			}
		}
	}
	r.Path = []int{}
	r.Phase = PhaseMap
}

// rollNodeType 决定节点类型：第一层都是普通战，第五层是宝箱，第八层(首领之前)是营火，其余层随机。
func (r *Run) rollNodeType(row int) string {
	switch row {
	case 0:
		return NodeCombat
	case 4:
		return NodeTreasure
	case Rows - 1:
		return NodeRest
	}
	types := []string{NodeCombat, NodeEvent, NodeElite, NodeShop, NodeRest}
	weights := []int{45, 22, 14, 10, 9}
	if row < 2 {
		weights[2], weights[4] = 0, 0
	}
	if row == Rows-2 {
		weights[4] = 0
	}
	return types[pickWeighted(&r.Rng, weights)]
}

// Pos 是当前所在的节点，这一幕还没出发时为 -1。
func (r *Run) Pos() int {
	if len(r.Path) == 0 {
		return -1
	}
	return r.Path[len(r.Path)-1]
}

// Choices 是在路线图上下一步可以去的节点。
func (r *Run) Choices() []int {
	if r.Phase != PhaseMap {
		return []int{}
	}
	pos := r.Pos()
	if pos < 0 {
		var first []int
		for i, node := range r.Map {
			if node.Row == 0 {
				first = append(first, i)
			}
		}
		return first
	}
	return r.Map[pos].Next
}

func (r *Run) enter(node int) {
	r.Path = append(r.Path, node)
	r.Floor++
	switch kind := r.Map[node].Type; kind {
	case NodeCombat, NodeElite, NodeBoss:
		// 前三幕打本幕的敌人；之后的无尽深渊从三幕的敌人里挑，按幕数放大。首领依次轮换。
		source := r.Act
		if r.Act > Acts {
			source = r.Rng.Range(1, Acts)
			if kind == NodeBoss {
				source = (r.Act-1)%Acts + 1
			}
		}
		pool := actEncounters[source-1]
		encounter := pool.Boss
		switch kind {
		case NodeCombat:
			encounter = pool.Normal[r.Rng.Intn(len(pool.Normal))]
		case NodeElite:
			encounter = pool.Elite[r.Rng.Intn(len(pool.Elite))]
		}
		hp, dmg := ActScale(r.Act, source)
		spawns := make([]spawn, 0, len(encounter))
		for _, id := range encounter {
			spawns = append(spawns, spawn{Id: id, HP: hp, Dmg: dmg})
		}
		r.startCombat(kind, spawns)
	case NodeShop:
		r.openShop()
	case NodeRest:
		r.Phase = PhaseRest
	case NodeEvent:
		var pool []string
		for _, id := range eventList {
			if !slices.Contains(r.Seen, id) {
				pool = append(pool, id)
			}
		}
		if len(pool) == 0 {
			r.Seen, pool = []string{}, slices.Clone(eventList)
		}
		r.Event = pool[r.Rng.Intn(len(pool))]
		r.Seen = append(r.Seen, r.Event)
		r.Phase = PhaseEvent
	case NodeTreasure:
		r.Reward = &Reward{Relic: r.randomRelic(r.rollRelicRarity()), Cards: []Card{}}
		r.gainRelic(r.Reward.Relic)
		r.Phase = PhaseReward
	}
}

// ActScale 是第 source 幕的敌人放到第 act 幕时的生命与伤害倍率(百分比)：前三幕各自的强度是设计好的，第四幕起每深入
// 一幕生命再 +25%、伤害再 +12%。敌人不会因为放到更早的幕而变弱。
func ActScale(act, source int) (hp, dmg int) {
	if act <= source {
		return 100, 100
	}
	// 各幕的强度按万分比算，避免连续两次整除丢掉精度。
	hpAt := func(a int) int {
		if a <= Acts {
			return []int{10000, 15000, 22000}[a-1]
		}
		return 22000 * (100 + 25*(a-Acts)) / 100
	}
	dmgAt := func(a int) int {
		if a <= Acts {
			return []int{10000, 13000, 17000}[a-1]
		}
		return 17000 * (100 + 12*(a-Acts)) / 100
	}
	return hpAt(act) * 100 / hpAt(source), dmgAt(act) * 100 / dmgAt(source)
}

// winCombat 在最后一个敌人倒下时结算。无尽远征发奖励(遗物回血、金币、药水、遗物与卡牌)；试炼接着打下一场或者过关；
// 练习只记下胜利。
func (r *Run) winCombat() {
	kind := r.Combat.Kind
	switch r.Mode {
	case ModePractice:
		r.Phase, r.Result = PhaseDone, PhaseWon
		r.emit("win", playerTarget, 0, kind)
		return
	case ModeTrial:
		r.Turns += r.Combat.Turn
		r.Combat = nil
		r.nextTrialFight()
		return
	}
	r.Combat = nil
	if r.hasRelic(RelicWarriorHeart) {
		r.heal(6)
	}
	if r.hasRelic(RelicHerbPouch) {
		r.heal(3)
	}
	reward := &Reward{Cards: []Card{}}
	cardWeights := []int{60, 35, 5}
	potionChance := 35
	switch kind {
	case NodeElite:
		r.Elites++
		reward.Gold = r.Rng.Range(28, 38)
		reward.Relic = r.randomRelic(r.rollRelicRarity())
		cardWeights, potionChance = []int{45, 42, 13}, 50
	case NodeBoss:
		r.Bosses++
		reward.Gold = r.Rng.Range(70, 90)
		reward.Relic = r.randomRelic(RarityRare)
		reward.Boss = true
		cardWeights, potionChance = []int{0, 0, 1}, 0
	default:
		reward.Gold = r.Rng.Range(12, 20)
	}
	if r.hasRelic(RelicLuckyCoin) {
		reward.Gold = reward.Gold * 5 / 4
	}
	r.Gold += reward.Gold
	r.gainRelic(reward.Relic)
	if r.Rng.Chance(potionChance) {
		reward.Potion = potionList[r.Rng.Intn(len(potionList))]
		if len(r.Potions) < r.PotionSlots() {
			r.Potions = append(r.Potions, reward.Potion)
		} else {
			reward.PotionLost = true
		}
	}
	count := 3
	if r.hasRelic(RelicCrown) {
		count = 2
	}
	reward.Cards = r.rollCards(count, cardWeights)
	r.Reward = reward
	r.Phase = PhaseReward
	r.emit("win", playerTarget, reward.Gold, kind)
}

// rollCards 从本职业与中立牌里抽 n 张不重复的牌，稀有度按普通、罕见、稀有的权重；第二幕起有机会已经升级。
func (r *Run) rollCards(n int, weights []int) []Card {
	cards := []Card{}
	for len(cards) < n {
		rarity := RarityCommon + pickWeighted(&r.Rng, weights)
		var pool []string
		for _, d := range cardList {
			if d.Rarity == rarity && (d.Class == r.Class || d.Class == ClassAny) &&
				!slices.ContainsFunc(cards, func(c Card) bool { return c.Id == d.Id }) {
				pool = append(pool, d.Id)
			}
		}
		if len(pool) == 0 {
			continue
		}
		card := Card{Id: pool[r.Rng.Intn(len(pool))]}
		card.Up = r.Rng.Chance(min((r.Act-1)*10, 50))
		cards = append(cards, card)
	}
	return cards
}

// priced 按天赋与深渊等级调整商店价格。
func (r *Run) priced(base int) int {
	pct := 100
	if r.Talents[TalentHaggle] > 0 {
		pct -= 10
	}
	if r.Abyss >= 3 {
		pct += 10
	}
	return base * pct / 100
}

func (r *Run) openShop() {
	shop := &Shop{RemovePrice: r.priced(75 + 25*r.Removals)}
	cardPrices := map[int]int{RarityCommon: 50, RarityUncommon: 75, RarityRare: 140}
	for _, c := range r.rollCards(5, []int{55, 35, 10}) {
		price := r.priced(cardPrices[c.def().Rarity] * r.Rng.Range(90, 110) / 100)
		shop.Offers = append(shop.Offers, Offer{Kind: "card", Card: c, Price: price})
	}
	relicPrices := map[int]int{RarityCommon: 150, RarityUncommon: 220, RarityRare: 300}
	var picked []string
	for range 2 {
		id := r.randomRelic(r.rollRelicRarity())
		if id == "" || slices.Contains(picked, id) {
			continue
		}
		picked = append(picked, id)
		price := r.priced(relicPrices[relicRarity(id)] * r.Rng.Range(90, 110) / 100)
		shop.Offers = append(shop.Offers, Offer{Kind: "relic", Id: id, Price: price})
	}
	for range 2 {
		id := potionList[r.Rng.Intn(len(potionList))]
		shop.Offers = append(shop.Offers, Offer{Kind: "potion", Id: id, Price: r.priced(50 * r.Rng.Range(90, 110) / 100)})
	}
	r.Shop = shop
	r.Phase = PhaseShop
}

func (r *Run) buy(index int) error {
	shop := r.Shop
	if index < 0 || index >= len(shop.Offers) {
		return ErrAction
	}
	offer := &shop.Offers[index]
	if offer.Sold || offer.Price > r.Gold || (offer.Kind == "potion" && len(r.Potions) >= r.PotionSlots()) ||
		(offer.Kind == "relic" && r.hasRelic(offer.Id)) {
		return ErrAction
	}
	r.Gold -= offer.Price
	offer.Sold = true
	switch offer.Kind {
	case "card":
		r.Deck = append(r.Deck, offer.Card)
		r.emit("card", playerTarget, 0, offer.Card.Id)
	case "relic":
		r.gainRelic(offer.Id)
	case "potion":
		r.Potions = append(r.Potions, offer.Id)
	}
	r.emit("gold", playerTarget, -offer.Price, "")
	return nil
}

func (r *Run) removeCard(index int) error {
	if index < 0 || index >= len(r.Deck) || len(r.Deck) <= 1 {
		return ErrAction
	}
	r.emit("remove", playerTarget, index, r.Deck[index].Id)
	r.Deck = slices.Delete(r.Deck, index, index+1)
	return nil
}

// RestHeal 是在营火休息能回复的生命。
func (r *Run) RestHeal() int {
	pct := 30
	if r.Abyss >= 4 {
		pct = 25
	}
	pct += 10 * r.Talents[TalentMedic]
	return r.MaxHP * pct / 100
}

// EventChoice 是事件的一个选项：Card 表示要再选一张牌。
type EventChoice struct {
	Enabled bool `json:"enabled"`
	Card    bool `json:"card"`
	// N 是选项里的数字(花费、回复量等)，页面直接显示。
	N int `json:"n,omitempty"`
}

func (r *Run) altarCost() int { return max(r.MaxHP*15/100, 1) }

// EventChoices 是当前事件的选项。每个事件都有两个选项。
func (r *Run) EventChoices() []EventChoice {
	hasUpgradable := slices.ContainsFunc(r.Deck, upgradable)
	switch r.Event {
	case EventAltar:
		return []EventChoice{{Enabled: r.HP > r.altarCost(), N: r.altarCost()}, {Enabled: true}}
	case EventSpring:
		return []EventChoice{{Enabled: true, N: r.MaxHP * 30 / 100}, {Enabled: len(r.Deck) > 1, Card: true}}
	case EventMerchant:
		return []EventChoice{{Enabled: r.Gold >= 40, N: 40}, {Enabled: true}}
	case EventDojo:
		return []EventChoice{{Enabled: hasUpgradable, N: 2}, {Enabled: true, N: 5}}
	case EventChest:
		return []EventChoice{{Enabled: true}, {Enabled: true}}
	case EventGambler:
		return []EventChoice{{Enabled: r.Gold >= 30, N: 30}, {Enabled: true}}
	case EventMirror:
		return []EventChoice{{Enabled: slices.ContainsFunc(r.Deck, func(c Card) bool { return c.def().Type != TypeCurse }), Card: true}, {Enabled: true}}
	}
	return nil
}

func (r *Run) chooseEvent(choice, cardIndex int) error {
	choices := r.EventChoices()
	if choice < 0 || choice >= len(choices) || !choices[choice].Enabled {
		return ErrAction
	}
	if choices[choice].Card && (cardIndex < 0 || cardIndex >= len(r.Deck)) {
		return ErrAction
	}
	switch r.Event + ":" + string(rune('0'+choice)) {
	case EventAltar + ":0":
		r.loseHP(r.altarCost(), "")
		r.gainRelic(r.randomRelic(RarityUncommon))
	case EventSpring + ":0":
		r.heal(r.MaxHP * 30 / 100)
	case EventSpring + ":1":
		if err := r.removeCard(cardIndex); err != nil {
			return err
		}
	case EventMerchant + ":0":
		r.Gold -= 40
		r.emit("gold", playerTarget, -40, "")
		card := r.rollCards(1, []int{0, 0, 1})[0]
		r.Deck = append(r.Deck, card)
		r.emit("card", playerTarget, 0, card.Id)
	case EventDojo + ":0":
		for range 2 {
			if i := r.upgradeRandom(); i >= 0 {
				r.emit("upgrade", playerTarget, i, r.Deck[i].Id)
			}
		}
	case EventDojo + ":1":
		r.MaxHP += 5
		r.HP += 5
		r.emit("max_hp", playerTarget, 5, "")
	case EventChest + ":0":
		r.gainRelic(r.randomRelic(r.rollRelicRarity()))
		r.Deck = append(r.Deck, Card{Id: DoubtCard})
		r.emit("card", playerTarget, 0, DoubtCard)
	case EventGambler + ":0":
		if r.Rng.Chance(50) {
			r.Gold += 30
			r.emit("gold", playerTarget, 30, "")
		} else {
			r.Gold -= 30
			r.emit("gold", playerTarget, -30, "")
		}
	case EventMirror + ":0":
		card := r.Deck[cardIndex]
		if card.def().Type == TypeCurse {
			return ErrAction
		}
		r.Deck = append(r.Deck, card)
		r.emit("card", playerTarget, 0, card.Id)
	}
	r.Event = ""
	r.Phase = PhaseMap
	return nil
}

// finishReward 离开奖励界面：首领奖励之后进入下一幕，并回复一半已损失的生命(深渊 9 起为四分之一)。
func (r *Run) finishReward() {
	boss := r.Reward.Boss
	r.Reward = nil
	r.Phase = PhaseMap
	if !boss {
		return
	}
	r.Act++
	missing := r.MaxHP - r.HP
	if r.Abyss >= 9 {
		r.heal(missing / 4)
	} else {
		r.heal(missing / 2)
	}
	r.newAct()
}

// Apply 执行一步操作。不合法时返回 ErrAction，状态可能已经部分改变，调用方应丢弃这个 Run。
func (r *Run) Apply(a Action) error {
	r.Events = nil
	if a.Type == "reset" {
		return r.resetPractice()
	}
	if r.Over() || r.Phase == PhaseDone {
		return ErrAction
	}
	switch a.Type {
	case "potion":
		return r.usePotion(a.Potion, a.Target)
	case "toss":
		if a.Potion < 0 || a.Potion >= len(r.Potions) {
			return ErrAction
		}
		r.Potions = slices.Delete(r.Potions, a.Potion, a.Potion+1)
		return nil
	}
	switch r.Phase + ":" + a.Type {
	case PhaseMap + ":path":
		if !slices.Contains(r.Choices(), a.Node) {
			return ErrAction
		}
		r.enter(a.Node)
	case PhaseCombat + ":play":
		return r.playCard(a.Card, a.Target)
	case PhaseCombat + ":end":
		r.endTurn()
	case PhaseReward + ":reward":
		if a.Choice < -1 || a.Choice >= len(r.Reward.Cards) {
			return ErrAction
		}
		if a.Choice >= 0 {
			card := r.Reward.Cards[a.Choice]
			r.Deck = append(r.Deck, card)
			r.emit("card", playerTarget, 0, card.Id)
		}
		r.finishReward()
	case PhaseShop + ":buy":
		return r.buy(a.Choice)
	case PhaseShop + ":remove":
		if r.Shop.Removed || r.Shop.RemovePrice > r.Gold {
			return ErrAction
		}
		if err := r.removeCard(a.Card); err != nil {
			return err
		}
		r.Gold -= r.Shop.RemovePrice
		r.emit("gold", playerTarget, -r.Shop.RemovePrice, "")
		r.Shop.Removed = true
		r.Removals++
	case PhaseShop + ":leave":
		r.Shop = nil
		r.Phase = PhaseMap
	case PhaseRest + ":rest":
		r.heal(r.RestHeal())
		r.Phase = PhaseMap
	case PhaseRest + ":smith":
		if a.Card < 0 || a.Card >= len(r.Deck) || !upgradable(r.Deck[a.Card]) {
			return ErrAction
		}
		r.Deck[a.Card].Up = true
		r.emit("upgrade", playerTarget, a.Card, r.Deck[a.Card].Id)
		r.Phase = PhaseMap
	case PhaseEvent + ":event":
		return r.chooseEvent(a.Choice, a.Card)
	default:
		return ErrAction
	}
	return nil
}

// Over 表示这一轮已经结束(胜利或倒下)。
func (r *Run) Over() bool { return r.Phase == PhaseWon || r.Phase == PhaseLost }

// Won 表示试炼过关。无尽远征没有终点，只会倒下或放弃。
func (r *Run) Won() bool { return r.Phase == PhaseWon }

// Score 是无尽远征的成绩，决定游戏币奖励与最高成绩：打败的首领数，也就是打通了几幕。
func (r *Run) Score() int { return r.Bosses }

// MaxTierScore 是管理端奖励档位能填的最高成绩：打通 30 幕。
const MaxTierScore = 30

// Crystals 是无尽远征这一轮获得的魂晶：每到达一层 1 颗、精英 3 颗、首领 10 颗，按深渊等级每级加 10%。
func (r *Run) Crystals() int {
	return (r.Floor + 3*r.Elites + 10*r.Bosses) * (10 + r.Abyss) / 10
}
