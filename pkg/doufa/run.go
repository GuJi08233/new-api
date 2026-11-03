package doufa

import "errors"

// 秘境斗法的一轮：从第一重往下打，每重 FloorSteps 步。每一步从两三间房里挑一间进去：战斗、精英、宝库(换法宝)、
// 静修(回血)、祠堂(祝福)；每重最后一步是首领。战斗房由浏览器打、服务端按房间的种子与开局重放操作记录核对(Clear)，
// 一轮的状态只在服务端。祝福是这一轮里的加成，出了秘境就没有；道韵天赋是开局时带进来的加成(见 Talent)。
//
// 一间战斗房发出以后只能打完它：交上操作记录(赢了往下走，倒下这一轮结束)，或者用一次退路重新开始(Retreat)。页面刷新、
// 换设备、半路退出都不会让房间回到没发出的样子，所以"打不过就刷新重来"换不来第二次机会——重来的次数就是退路。
// 一轮的状态存在对局里(game_sessions)，与浏览器的规则无关，所以只有 Go 这一份。

const (
	FloorSteps = 7
	// RunStateVersion 是一轮状态的格式版本，改了 Run 的字段含义要加一，旧版本的进行中的一轮按作废处理。
	RunStateVersion = 2
	// restHeal、bossHeal 是静修与打倒首领回复的生命(生命上限的百分数)；fightOffer 是普通战斗以后给祝福的机会(百分数)。
	restHeal   = 35
	bossHeal   = 40
	fightOffer = 40
	// BaseRetreats 是一轮开局有几次退路：从没打完的房间里退出来(撤退、刷新、换设备)，同一间房换个种子重新开始；
	// 用完以后再退出就算倒下。道韵天赋「定魂」可以多几次。
	BaseRetreats = 2
	// elixirHeal、wardHeal 是续命丹回复、护命符复活时的生命(生命上限的百分数)。
	elixirHeal = 40
	wardHeal   = 50
)

// ErrRunAction 表示这一步现在不能做：一轮已经结束、房间没打完、还没挑祝福、选项不存在……
var ErrRunAction = errors.New("doufa: run action not available")

// floorPool 是一重的敌人与房间：第一重迷雾林，第二重赤炎窟；第三重起两份轮流用，强度按重数往上加。
type floorPool struct {
	Rooms    []string
	Foes     []string
	Elite    string
	Boss     string
	BossRoom string
}

var floorPools = []floorPool{
	{Rooms: []string{"glade", "ruins"}, Foes: []string{"slime", "skeleton", "moth", "goblin", "gargoyle"}, Elite: "goblin_chief", Boss: "slime_king", BossRoom: "glade"},
	{Rooms: []string{"cavern", "ruins"}, Foes: []string{"werewolf", "shade_mage", "lava_elemental", "beetle", "goblin"}, Elite: "troll", Boss: "flame_lord", BossRoom: "cavern"},
}

// RunRoom 是正在打的一间战斗房：浏览器按 Seed 与 Setup 开局，打完提交操作记录；Issued 是发出房间的时间(秒)，
// 提交时不能早于"帧数 ÷ 60"秒的八成。Hidden 的房间是给页面看的样子(见 Public)，不带种子和开局。
type RunRoom struct {
	Kind   string `json:"kind"`
	Seed   uint32 `json:"seed"`
	Setup  Setup  `json:"setup"`
	Issued int64  `json:"issued"`
	Hidden bool   `json:"hidden,omitempty"`
}

// Offer 是等着挑的东西：blessing 三选一的祝福，weapon 二选一的法宝。
type Offer struct {
	Kind  string   `json:"kind"`
	Items []string `json:"items"`
}

// Run 是一轮的全部状态。Floor 从 1 起；Step 是这一重第几步(从 0 起)，Choices 是这一步能进的房间。Talents 是开局时带进来
// 的天赋等级(这一轮里不再变)；Retreats 是还剩几次退路；Ward 为真是身上挂着护命符；Used 是用过几件商店道具。
type Run struct {
	V         int            `json:"v"`
	Rng       uint32         `json:"rng"`
	Class     string         `json:"cls"`
	Floor     int            `json:"floor"`
	Step      int            `json:"step"`
	HP        int            `json:"hp"`
	MP        int            `json:"mp"`
	Weapons   []string       `json:"weapons"`
	Blessings []string       `json:"blessings"`
	Talents   map[string]int `json:"tal,omitempty"`
	Retreats  int            `json:"retreats"`
	Ward      bool           `json:"ward,omitempty"`
	Used      int            `json:"used,omitempty"`
	Stones    int            `json:"stones"`
	Kills     int            `json:"kills"`
	Cleared   int            `json:"cleared"`
	Ticks     int            `json:"ticks"`
	Choices   []string       `json:"choices,omitempty"`
	Room      *RunRoom       `json:"room,omitempty"`
	Offer     *Offer         `json:"offer,omitempty"`
	Over      bool           `json:"over"`
}

// NewRun 开一轮：带上道韵天赋(talents 是天赋标识到等级)，满状态、流派起手的两件法宝，第一步只有一间战斗房。
func NewRun(seed uint32, class string, talents map[string]int) (*Run, error) {
	spec, ok := content.Classes[class]
	if !ok {
		return nil, ErrSetup
	}
	if seed == 0 {
		seed = 1
	}
	r := &Run{V: RunStateVersion, Rng: seed, Class: class, Floor: 1, Weapons: append([]string(nil), spec.Weapons...),
		Blessings: []string{}, Choices: []string{"fight"}, Retreats: BaseRetreats}
	for _, t := range content.Talents {
		if level := min(talents[t.Id], len(t.Costs)); level > 0 {
			if r.Talents == nil {
				r.Talents = map[string]int{}
			}
			r.Talents[t.Id] = level
		}
	}
	r.Retreats += r.talentTotal(func(t Talent) int { return t.Retreats })
	for range r.talentTotal(func(t Talent) int { return t.Blessings }) {
		r.Blessings = append(r.Blessings, r.draw(r.missingBlessings(), 1)...)
	}
	r.HP, r.MP = r.MaxHP(), spec.MP
	return r, nil
}

// talentTotal 把带进来的每种天赋按等级折算，加起来：pick 取每一级给的那一项。
func (r *Run) talentTotal(pick func(Talent) int) int {
	total := 0
	for _, t := range content.Talents {
		total += pick(t) * r.Talents[t.Id]
	}
	return total
}

func (r *Run) rand(n int) int {
	s := r.Rng
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	r.Rng = s
	return int(s % uint32(n))
}

// add 把 o 的每一项乘上 times 加进 m。
func (m *Mods) add(o Mods, times int) {
	m.Damage += o.Damage * times
	m.Speed += o.Speed * times
	m.Haste += o.Haste * times
	m.Pierce += o.Pierce * times
	m.Regen += o.Regen * times
	m.Vigor += o.Vigor * times
	m.Dodge += o.Dodge * times
	m.Focus += o.Focus * times
	m.Reach += o.Reach * times
	m.Ward += o.Ward * times
}

// Mods 是已经拿到的祝福与带进来的天赋叠起来的加成。
func (r *Run) Mods() Mods {
	var m Mods
	for _, id := range r.Blessings {
		for _, b := range content.Blessings {
			if b.Id == id {
				m.add(b.Mods, 1)
			}
		}
	}
	for _, t := range content.Talents {
		m.add(t.Mods, r.Talents[t.Id])
	}
	return m
}

// MaxHP 是现在的生命上限：流派的生命加上祝福与天赋。
func (r *Run) MaxHP() int { return content.Classes[r.Class].HP + r.Mods().Vigor }

func (r *Run) pool() floorPool { return floorPools[(r.Floor-1)%len(floorPools)] }

// Score 是这一轮走了多深：每重 FloorSteps 步，打倒首领算走完一重。
func (r *Run) Score() int { return max(0, (r.Floor-1)*FloorSteps+max(r.Step, 0)) }

// Floors 是打通了几重(打倒了几位首领)，游戏币的奖励按它取档。
func (r *Run) Floors() int { return max(0, r.Floor-1) }

// Crystals 是这一轮结束时得到的道韵：打通的房间和打通的重数。
func (r *Run) Crystals() int { return 2*r.Cleared + 12*r.Floors() }

// Public 是给页面看的一轮。deliver 为真(刚进房或刚重新开始)时带着房间的种子与开局，由浏览器去打；其余时候已经发出
// 的房间只留下种类，种子和开局要用一次退路才能再拿到。
func (r *Run) Public(deliver bool) *Run {
	if deliver || r.Room == nil {
		return r
	}
	view := *r
	view.Room = &RunRoom{Kind: r.Room.Kind, Hidden: true}
	return &view
}

// Enter 进入这一步的第 choice 间房。战斗房发出房间等浏览器来打；宝库与祠堂给出要挑的东西；静修直接回血往下走。
func (r *Run) Enter(choice int, now int64) error {
	if r.Over || r.Room != nil || r.Offer != nil || choice < 0 || choice >= len(r.Choices) {
		return ErrRunAction
	}
	kind := r.Choices[choice]
	r.Choices = nil
	switch kind {
	case "fight", "elite", "boss":
		seed := uint32(1 + r.rand(1<<30))
		r.Room = &RunRoom{Kind: kind, Seed: seed, Setup: r.setup(kind), Issued: now}
	case "treasure":
		r.Offer = &Offer{Kind: "weapon", Items: r.draw(r.missingWeapons(), 2)}
	case "shrine":
		r.Offer = &Offer{Kind: "blessing", Items: r.draw(r.missingBlessings(), 3)}
	case "rest":
		r.HP = min(r.MaxHP(), r.HP+r.MaxHP()*(restHeal+r.talentTotal(func(t Talent) int { return t.Rest }))/100)
		r.next()
	}
	return nil
}

// setup 是一间战斗房的开局：这一重的房间与敌人，带上现在的生命、灵力、法宝与祝福。普通战斗两波(第四步起三波)，
// 每波三到四只(重数越深越多)；精英房最后一波多一只精英；首领房只有首领。敌人的生命与伤害按重数加强。
func (r *Run) setup(kind string) Setup {
	pool := r.pool()
	room := pool.Rooms[r.rand(len(pool.Rooms))]
	var waves [][]Group
	if kind == "boss" {
		room = pool.BossRoom
		waves = [][]Group{{{Foe: pool.Boss, N: 1}}}
	} else {
		count := 2
		if r.Step >= 3 {
			count = 3
		}
		for w := 0; w < count; w++ {
			n := min(3+r.rand(2)+(r.Floor-1)/2, 7)
			counts := make([]int, len(pool.Foes))
			for k := 0; k < n; k++ {
				counts[r.rand(len(pool.Foes))]++
			}
			var wave []Group
			for i, foe := range pool.Foes {
				if counts[i] > 0 {
					wave = append(wave, Group{Foe: foe, N: counts[i]})
				}
			}
			if kind == "elite" && w == count-1 {
				wave = append(wave, Group{Foe: pool.Elite, N: 1})
			}
			waves = append(waves, wave)
		}
	}
	return Setup{Class: r.Class, Weapons: append([]string(nil), r.Weapons...), Room: room, Waves: waves, HP: r.HP, MP: r.MP,
		Mods: r.Mods(), Scale: FoeScale{HP: min(100+30*(r.Floor-1), 1000), Dmg: min(100+12*(r.Floor-1), 1000)}}
}

// Clear 记下打完的房间(now 是提交的时间)：倒下(或到时)就结束这一轮，身上有护命符的话以半条命复活、同一间房重新打；
// 赢了按房间给祝福(精英与首领必给，普通战斗有时给)，首领打倒后回血进下一重。
func (r *Run) Clear(f *Fight, now int64) error {
	if r.Over || r.Room == nil || !f.Over {
		return ErrRunAction
	}
	r.Ticks += f.Tick
	if !f.Won {
		if !r.Ward {
			r.Kills += f.Kills
			r.Stones += f.Stones
		}
		r.fall(now)
		return nil
	}
	kind := r.Room.Kind
	r.Room = nil
	r.Kills += f.Kills
	r.Stones += f.Stones
	r.Cleared++
	r.HP = f.P.HP
	r.MP = f.P.MP / 100
	offer := kind != "fight" || r.rand(100) < fightOffer
	if kind == "boss" {
		r.HP = min(r.MaxHP(), r.HP+r.MaxHP()*bossHeal/100)
		r.Floor++
		r.Step = -1
	}
	if items := r.draw(r.missingBlessings(), 3); offer && len(items) > 0 {
		r.Offer = &Offer{Kind: "blessing", Items: items}
		return nil
	}
	r.next()
	return nil
}

// Retreat 从没打完的战斗房里退出来：用掉一次退路，同一间房换个种子重新开始，生命与灵力和进房时一样；没有退路了，退出
// 就算倒下。撤退、刷新页面、换设备后重新进房都是它。
func (r *Run) Retreat(now int64) error {
	if r.Over || r.Room == nil {
		return ErrRunAction
	}
	if r.Retreats > 0 {
		r.Retreats--
		r.reissue(now)
		return nil
	}
	r.fall(now)
	return nil
}

// fall 是在房间里倒下(或退无可退)：身上有护命符就以半条命复活、同一间房重新打，否则这一轮结束。
func (r *Run) fall(now int64) {
	if r.Ward {
		r.Ward = false
		r.HP = max(1, r.MaxHP()*wardHeal/100)
		r.Room.Setup.HP = r.HP
		r.reissue(now)
		return
	}
	r.HP, r.Room, r.Over = 0, nil, true
}

// reissue 把这间房换个种子重新发出：敌人不变，随机数和发出时间重新算。新种子由旧种子走一步异或移位得到，不动一轮的随机数，
// 退路换不来别的随机结果。
func (r *Run) reissue(now int64) {
	seed := r.Room.Seed
	seed ^= seed << 13
	seed ^= seed >> 17
	seed ^= seed << 5
	r.Room.Seed = seed
	r.Room.Issued = now
}

// UseItem 在两间房之间用掉一件商店道具(背包里的数量由调用方扣)：续命丹回复生命，福缘匣送一样还没有的祝福，护命符挂在身上。
// 每轮最多用 ItemsPerRun 件；这件道具现在用不了(生命是满的、祝福已经齐了、护命符已经挂着)返回 ErrItemLimit，状态不变。
func (r *Run) UseItem(id string) error {
	if r.Over || r.Room != nil || r.Offer != nil {
		return ErrRunAction
	}
	if r.Used >= ItemsPerRun {
		return ErrItemLimit
	}
	switch id {
	case ItemElixir:
		if r.HP >= r.MaxHP() {
			return ErrItemLimit
		}
		r.HP = min(r.MaxHP(), r.HP+r.MaxHP()*elixirHeal/100)
	case ItemFortune:
		items := r.draw(r.missingBlessings(), 1)
		if len(items) == 0 {
			return ErrItemLimit
		}
		r.Blessings = append(r.Blessings, items...)
	case ItemWard:
		if r.Ward {
			return ErrItemLimit
		}
		r.Ward = true
	default:
		return ErrRunAction
	}
	r.Used++
	return nil
}

// Pick 挑一样：祝福收下；法宝换掉第 slot 件(只有一件时加在后面)。挑完往下一步走。choice 为 -1 是都不要。
func (r *Run) Pick(choice, slot int) error {
	if r.Over || r.Offer == nil || choice < -1 || choice >= len(r.Offer.Items) {
		return ErrRunAction
	}
	if choice >= 0 {
		item := r.Offer.Items[choice]
		switch {
		case r.Offer.Kind == "blessing":
			r.Blessings = append(r.Blessings, item)
		case len(r.Weapons) < 2:
			r.Weapons = append(r.Weapons, item)
		case slot == 0 || slot == 1:
			r.Weapons[slot] = item
		default:
			return ErrRunAction
		}
	}
	r.Offer = nil
	r.next()
	return nil
}

// Abandon 放弃这一轮。
func (r *Run) Abandon() error {
	if r.Over {
		return ErrRunAction
	}
	r.Room, r.Offer, r.Choices, r.Over = nil, nil, nil, true
	return nil
}

// next 走到下一步并给出能进的房间：最后一步只有首领；其余从战斗、精英(第三步起)、宝库、静修(没满血时)、祠堂里
// 按权重挑两到三间不重样的，至少有一间要打的。
func (r *Run) next() {
	r.Step++
	if r.Step >= FloorSteps-1 {
		r.Choices = []string{"boss"}
		return
	}
	type option struct {
		kind   string
		weight int
	}
	options := []option{{"fight", 40}, {"treasure", 12}, {"shrine", 18}}
	if r.Step >= 2 {
		options = append(options, option{"elite", 16})
	}
	if r.HP < r.MaxHP() {
		options = append(options, option{"rest", 14})
	}
	want := 2 + r.rand(2)
	var picked []string
	for len(picked) < want && len(options) > 0 {
		total := 0
		for _, o := range options {
			total += o.weight
		}
		roll := r.rand(total)
		for i, o := range options {
			if roll < o.weight {
				picked = append(picked, o.kind)
				options = append(options[:i], options[i+1:]...)
				break
			}
			roll -= o.weight
		}
	}
	fights := false
	for _, k := range picked {
		fights = fights || k == "fight" || k == "elite"
	}
	if !fights {
		picked[0] = "fight"
	}
	r.Choices = picked
}

// draw 从候选里不放回地随机挑 n 个(不够就全给)。
func (r *Run) draw(from []string, n int) []string {
	pool := append([]string(nil), from...)
	var out []string
	for len(out) < n && len(pool) > 0 {
		i := r.rand(len(pool))
		out = append(out, pool[i])
		pool = append(pool[:i], pool[i+1:]...)
	}
	return out
}

func (r *Run) missingBlessings() []string {
	var out []string
	for _, b := range content.Blessings {
		if !containsString(r.Blessings, b.Id) {
			out = append(out, b.Id)
		}
	}
	return out
}

func (r *Run) missingWeapons() []string {
	var out []string
	for _, w := range content.WeaponPool {
		if !containsString(r.Weapons, w) {
			out = append(out, w)
		}
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
