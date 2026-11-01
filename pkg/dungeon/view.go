package dungeon

import (
	"slices"
	"strings"
)

// CardView 是页面上的一张牌。手牌里的攻击伤害已经计入力量与虚弱(不针对具体目标)，Fx 的 B 是改动前的基础值。
type CardView struct {
	Id      string   `json:"id"`
	Up      bool     `json:"up,omitempty"`
	Type    string   `json:"type"`
	Rarity  int      `json:"rarity"`
	Class   string   `json:"class"`
	Cost    int      `json:"cost"`
	Target  int      `json:"target"`
	Exhaust bool     `json:"exhaust,omitempty"`
	Fx      []FxView `json:"fx"`
	// Upgrade 是牌组里这张牌升级之后的样子，营火锻造与事件升级前给玩家预览；已经升级或不能升级时为空。
	Upgrade *CardView `json:"upgrade,omitempty"`
}

// FxView 是牌面上的一条效果。
type FxView struct {
	Fx
	B int `json:"b,omitempty"`
}

// Intent 是敌人公布的下一招。
type Intent struct {
	Move   string `json:"move"`
	Dmg    int    `json:"dmg,omitempty"`
	Times  int    `json:"times,omitempty"`
	Block  bool   `json:"block,omitempty"`
	Buff   bool   `json:"buff,omitempty"`
	Debuff bool   `json:"debuff,omitempty"`
	Heal   bool   `json:"heal,omitempty"`
}

// EnemyView 是页面上的一个敌人。
type EnemyView struct {
	Id     string         `json:"id"`
	HP     int            `json:"hp"`
	MaxHP  int            `json:"max_hp"`
	Block  int            `json:"block"`
	St     map[string]int `json:"st"`
	Dead   bool           `json:"dead"`
	Intent *Intent        `json:"intent,omitempty"`
}

// CombatView 是战斗中玩家能看到的一切。抽牌堆只给内容、按牌名排序，不泄露顺序。
type CombatView struct {
	Kind    string         `json:"kind"`
	Turn    int            `json:"turn"`
	Energy  int            `json:"energy"`
	Block   int            `json:"block"`
	St      map[string]int `json:"st"`
	Hand    []CardView     `json:"hand"`
	Draw    []CardView     `json:"draw"`
	Discard []CardView     `json:"discard"`
	Exhaust []CardView     `json:"exhaust"`
	Enemies []EnemyView    `json:"enemies"`
	// 伤害统计：这场战斗、本回合敌人失去的生命，单次攻击的最高伤害。
	Dealt     int `json:"dealt"`
	TurnDealt int `json:"turn_dealt"`
	MaxHit    int `json:"max_hit"`
}

// OfferView 是商店里的一件商品。
type OfferView struct {
	Kind  string    `json:"kind"`
	Card  *CardView `json:"card,omitempty"`
	Id    string    `json:"id,omitempty"`
	Price int       `json:"price"`
	Sold  bool      `json:"sold"`
}

// View 是一轮远征在页面上的样子，不含随机数状态与抽牌顺序。
type View struct {
	Mode        string      `json:"mode"`
	Class       string      `json:"class"`
	Abyss       int         `json:"abyss"`
	Act         int         `json:"act"`
	Floor       int         `json:"floor"`
	HP          int         `json:"hp"`
	MaxHP       int         `json:"max_hp"`
	Gold        int         `json:"gold"`
	Phase       string      `json:"phase"`
	Deck        []CardView  `json:"deck"`
	Relics      []string    `json:"relics"`
	Potions     []string    `json:"potions"`
	PotionSlots int         `json:"potion_slots"`
	Map         []Node      `json:"map"`
	Path        []int       `json:"path"`
	Choices     []int       `json:"choices"`
	Combat      *CombatView `json:"combat,omitempty"`
	Reward      *struct {
		Reward
		Cards []CardView `json:"cards"`
	} `json:"reward,omitempty"`
	Shop *struct {
		Offers      []OfferView `json:"offers"`
		RemovePrice int         `json:"remove_price"`
		Removed     bool        `json:"removed"`
	} `json:"shop,omitempty"`
	RestHeal     int           `json:"rest_heal,omitempty"`
	Event        string        `json:"event,omitempty"`
	EventChoices []EventChoice `json:"event_choices,omitempty"`
	Events       []Ev          `json:"events"`
	Kills        int           `json:"kills"`
	Elites       int           `json:"elites"`
	Bosses       int           `json:"bosses"`
	Score        int           `json:"score"`
	Crystals     int           `json:"crystals"`
	PhoenixUsed  bool          `json:"phoenix_used,omitempty"`
	ItemsUsed    int           `json:"items_used"`
	Ward         bool          `json:"ward,omitempty"`
	WardUsed     bool          `json:"ward_used,omitempty"`
	// 试炼：哪一关、正在打第几场、一共几场、已经用了几回合、星级(过关后)。
	Trial  string `json:"trial,omitempty"`
	Fight  int    `json:"fight"`
	Fights int    `json:"fights,omitempty"`
	Turns  int    `json:"turns"`
	Stars  int    `json:"stars"`
	// 练习：设置与这一场的胜负。
	Practice *PracticeSetup `json:"practice,omitempty"`
	Result   string         `json:"result,omitempty"`
	// 单场战斗：赢了以后收下的功法。
	Picked *Card `json:"picked,omitempty"`
}

func cardView(c Card) CardView {
	d := c.def()
	v := CardView{Id: c.Id, Up: c.Up, Type: d.Type, Rarity: d.Rarity, Class: d.Class, Cost: cardCost(c), Target: d.Target, Exhaust: cardExhausts(c)}
	for _, f := range d.Effects(c.Up) {
		v.Fx = append(v.Fx, FxView{Fx: f})
	}
	if v.Fx == nil {
		v.Fx = []FxView{}
	}
	return v
}

func cardViews(cards []Card) []CardView {
	views := make([]CardView, 0, len(cards))
	for _, c := range cards {
		views = append(views, cardView(c))
	}
	return views
}

// deckViews 是牌组里的牌，附带升级预览。
func deckViews(cards []Card) []CardView {
	views := cardViews(cards)
	for i, c := range cards {
		if upgradable(c) {
			up := cardView(Card{Id: c.Id, Up: true})
			views[i].Upgrade = &up
		}
	}
	return views
}

// handView 把力量、虚弱与锋锐计入手牌的攻击伤害。
func (r *Run) handView(c Card) CardView {
	v := cardView(c)
	for i := range v.Fx {
		f := &v.Fx[i]
		switch f.Kind {
		case FxDamage, FxDamageAll, FxDamageRand:
			base := f.N
			if c.Id == KnifeCard {
				base += r.Combat.St[StKeen]
			}
			if n := AttackDamage(base, r.Combat.St, nil); n != f.N {
				f.B, f.N = f.N, n
			}
		}
	}
	return v
}

// View 返回页面上的样子，附带最近一次操作里发生的事。
func (r *Run) View() View {
	v := View{
		Class: r.Class, Abyss: r.Abyss, Act: r.Act, Floor: r.Floor, HP: r.HP, MaxHP: r.MaxHP, Gold: r.Gold, Phase: r.Phase,
		Deck: deckViews(r.Deck), Relics: r.Relics, Potions: r.Potions, PotionSlots: r.PotionSlots(),
		Map: r.Map, Path: r.Path, Choices: r.Choices(), Events: r.Events,
		Kills: r.Kills, Elites: r.Elites, Bosses: r.Bosses, Score: r.Score(), Crystals: r.Crystals(), PhoenixUsed: r.PhoenixUsed,
		ItemsUsed: r.ItemsUsed, Ward: r.Ward, WardUsed: r.WardUsed,
		Mode: r.Mode, Trial: r.Trial, Fight: r.Fight, Turns: r.Turns, Stars: r.Stars(), Practice: r.Practice, Result: r.Result,
		Picked: r.Picked,
	}
	if index := TrialIndex(r.Trial); index >= 0 {
		v.Fights = len(Trials[index].Fights)
	}
	if v.Events == nil {
		v.Events = []Ev{}
	}
	if c := r.Combat; c != nil {
		cv := &CombatView{Kind: c.Kind, Turn: c.Turn, Energy: c.Energy, Block: c.Block, St: c.St,
			Discard: cardViews(c.Discard), Exhaust: cardViews(c.Exhaust), Dealt: c.Dealt, TurnDealt: c.TurnDealt, MaxHit: c.MaxHit}
		for _, card := range c.Hand {
			cv.Hand = append(cv.Hand, r.handView(card))
		}
		if cv.Hand == nil {
			cv.Hand = []CardView{}
		}
		draw := slices.Clone(c.Draw)
		slices.SortFunc(draw, func(a, b Card) int {
			if n := strings.Compare(a.Id, b.Id); n != 0 {
				return n
			}
			if a.Up == b.Up {
				return 0
			}
			if a.Up {
				return 1
			}
			return -1
		})
		cv.Draw = cardViews(draw)
		for _, e := range c.Enemies {
			ev := EnemyView{Id: e.Id, HP: e.HP, MaxHP: e.MaxHP, Block: e.Block, St: e.St, Dead: e.Dead}
			if !e.Dead {
				move := e.def().Moves[e.Move]
				ev.Intent = &Intent{Move: move.Id, Dmg: r.IntentDamage(e), Times: max(move.Times, 1),
					Block: move.Block > 0, Buff: len(move.Self) > 0, Debuff: len(move.Foe) > 0 || move.Wounds > 0, Heal: move.Heal > 0}
				if move.Dmg == 0 {
					ev.Intent.Times = 0
				}
			}
			cv.Enemies = append(cv.Enemies, ev)
		}
		v.Combat = cv
	}
	if r.Reward != nil {
		v.Reward = &struct {
			Reward
			Cards []CardView `json:"cards"`
		}{Reward: *r.Reward, Cards: cardViews(r.Reward.Cards)}
	}
	if r.Shop != nil {
		shop := &struct {
			Offers      []OfferView `json:"offers"`
			RemovePrice int         `json:"remove_price"`
			Removed     bool        `json:"removed"`
		}{RemovePrice: r.Shop.RemovePrice, Removed: r.Shop.Removed}
		for _, o := range r.Shop.Offers {
			ov := OfferView{Kind: o.Kind, Id: o.Id, Price: o.Price, Sold: o.Sold}
			if o.Kind == "card" {
				card := cardView(o.Card)
				ov.Card = &card
			}
			shop.Offers = append(shop.Offers, ov)
		}
		v.Shop = shop
	}
	if r.Phase == PhaseRest {
		v.RestHeal = min(r.RestHeal(), r.MaxHP-r.HP)
	}
	if r.Phase == PhaseEvent {
		v.Event, v.EventChoices = r.Event, r.EventChoices()
	}
	return v
}

// CatalogEnemy 是练习模式里能选的一个敌人。Act 是出身的幕(训练木桩为 0)；历练专有的敌人没有幕，Chapter 是它出自
// 历练的第几章。
type CatalogEnemy struct {
	Id      string `json:"id"`
	Act     int    `json:"act"`
	Chapter int    `json:"chapter,omitempty"`
	Kind    string `json:"kind"`
}

// Catalog 是练习模式与试炼页面要用的全部内容：能放进牌组的牌(附升级预览)、遗物、敌人与试炼。
type Catalog struct {
	Cards   []CardView          `json:"cards"`
	Relics  []string            `json:"relics"`
	Enemies []CatalogEnemy      `json:"enemies"`
	Trials  []TrialDef          `json:"trials"`
	Starter map[string][]string `json:"starter"`
}

// NewCatalog 返回练习与试炼用的内容表。
func NewCatalog() Catalog {
	c := Catalog{Relics: relicIds(), Trials: Trials, Starter: starterDecks}
	for _, d := range cardList {
		if d.Id == WoundCard {
			continue
		}
		c.Cards = append(c.Cards, deckViews([]Card{{Id: d.Id}})[0])
	}
	for _, d := range enemyList {
		c.Enemies = append(c.Enemies, CatalogEnemy{Id: d.Id, Act: NativeAct(d.Id), Kind: encounterKind([]string{d.Id})})
	}
	for _, d := range journeyEnemyList {
		origin := journeyEnemyOrigin[d.Id]
		c.Enemies = append(c.Enemies, CatalogEnemy{Id: d.Id, Chapter: origin.Chapter, Kind: origin.Kind})
	}
	return c
}
