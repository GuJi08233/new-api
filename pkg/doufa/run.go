package doufa

import "errors"

// 秘境斗法的一轮：从第一重往下打，每重 FloorSteps 步。每一步从两三间房里挑一间进去：战斗、精英、宝库(换法宝)、
// 静修(回血)、祠堂(祝福)；每重最后一步是首领。战斗房由浏览器打、服务端按房间的种子与开局重放操作记录核对(Clear)，
// 一轮的状态只在服务端。祝福是这一轮里的加成，出了秘境就没有。
//
// 一轮的状态存在对局里(game_sessions)，与浏览器的规则无关，所以只有 Go 这一份。

const (
	FloorSteps = 7
	// RunStateVersion 是一轮状态的格式版本，改了 Run 的字段含义要加一，旧版本的进行中的一轮按作废处理。
	RunStateVersion = 1
	// restHeal、bossHeal 是静修与打倒首领回复的生命(生命上限的百分数)；fightOffer 是普通战斗以后给祝福的机会(百分数)。
	restHeal   = 35
	bossHeal   = 40
	fightOffer = 40
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
// 提交时不能早于"帧数 ÷ 60"秒的八成。
type RunRoom struct {
	Kind   string `json:"kind"`
	Seed   uint32 `json:"seed"`
	Setup  Setup  `json:"setup"`
	Issued int64  `json:"issued"`
}

// Offer 是等着挑的东西：blessing 三选一的祝福，weapon 二选一的法宝。
type Offer struct {
	Kind  string   `json:"kind"`
	Items []string `json:"items"`
}

// Run 是一轮的全部状态。Floor 从 1 起；Step 是这一重第几步(从 0 起)，Choices 是这一步能进的房间。
type Run struct {
	V         int      `json:"v"`
	Rng       uint32   `json:"rng"`
	Class     string   `json:"cls"`
	Floor     int      `json:"floor"`
	Step      int      `json:"step"`
	HP        int      `json:"hp"`
	MP        int      `json:"mp"`
	Weapons   []string `json:"weapons"`
	Blessings []string `json:"blessings"`
	Stones    int      `json:"stones"`
	Kills     int      `json:"kills"`
	Cleared   int      `json:"cleared"`
	Ticks     int      `json:"ticks"`
	Choices   []string `json:"choices,omitempty"`
	Room      *RunRoom `json:"room,omitempty"`
	Offer     *Offer   `json:"offer,omitempty"`
	Over      bool     `json:"over"`
}

// NewRun 开一轮：满状态、流派起手的两件法宝，第一步只有一间战斗房。
func NewRun(seed uint32, class string) (*Run, error) {
	spec, ok := content.Classes[class]
	if !ok {
		return nil, ErrSetup
	}
	if seed == 0 {
		seed = 1
	}
	return &Run{V: RunStateVersion, Rng: seed, Class: class, Floor: 1, HP: spec.HP, MP: spec.MP,
		Weapons: append([]string(nil), spec.Weapons...), Blessings: []string{}, Choices: []string{"fight"}}, nil
}

func (r *Run) rand(n int) int {
	s := r.Rng
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	r.Rng = s
	return int(s % uint32(n))
}

// Mods 是已经拿到的祝福叠起来的加成。
func (r *Run) Mods() Mods {
	var m Mods
	for _, id := range r.Blessings {
		for _, b := range content.Blessings {
			if b.Id != id {
				continue
			}
			m.Damage += b.Mods.Damage
			m.Speed += b.Mods.Speed
			m.Haste += b.Mods.Haste
			m.Pierce += b.Mods.Pierce
			m.Regen += b.Mods.Regen
			m.Vigor += b.Mods.Vigor
			m.Dodge += b.Mods.Dodge
			m.Focus += b.Mods.Focus
			m.Reach += b.Mods.Reach
			m.Ward += b.Mods.Ward
		}
	}
	return m
}

// MaxHP 是现在的生命上限：流派的生命加上祝福。
func (r *Run) MaxHP() int { return content.Classes[r.Class].HP + r.Mods().Vigor }

func (r *Run) pool() floorPool { return floorPools[(r.Floor-1)%len(floorPools)] }

// Score 是这一轮走了多深：每重 FloorSteps 步，打倒首领算走完一重。
func (r *Run) Score() int { return (r.Floor-1)*FloorSteps + max(r.Step, 0) }

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
		r.HP = min(r.MaxHP(), r.HP+r.MaxHP()*restHeal/100)
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

// Clear 记下打完的房间：倒下(或到时)就结束这一轮；赢了按房间给祝福(精英与首领必给，普通战斗有时给)，首领打倒后回血进下一重。
func (r *Run) Clear(f *Fight) error {
	if r.Over || r.Room == nil || !f.Over {
		return ErrRunAction
	}
	kind := r.Room.Kind
	r.Room = nil
	r.Ticks += f.Tick
	r.Kills += f.Kills
	r.Stones += f.Stones
	if !f.Won {
		r.HP = 0
		r.Over = true
		return nil
	}
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
