// Package doufa 是问道的斗法：俯视角的即时战斗，主角在一间由格子组成的房间里走位、挥砍、放飞弹与法术、冲刺闪避，
// 敌人分几波出现。浏览器逐帧记下操作(一帧一个整数，见 Pack)，服务端按同一个种子重放得出结果，不信浏览器报的胜负。
//
// 规则与浏览器的 web/classic/src/pages/Games/wendao/doufa/engine.js 逐行对应：只用整数，60 帧一秒，方向是 64 个角度
// (一张整数余弦表)，随机数是 32 位异或移位；实体每帧按固定顺序更新(主角、敌人按出场先后、飞弹、掉落)。改这里的任何
// 一处都要同样改 engine.js，再用 testdata/generate_golden.mjs 重新生成黄金回放，两边的测试都要过。
package doufa

import (
	"errors"
	"math"
)

const (
	// FPS 是每秒几帧；Scale 是一个像素合多少个单位；Tile 是一个格子多少个单位(32 像素)。
	FPS    = 60
	Scale  = 100
	TilePx = 32
	Tile   = TilePx * Scale
	// MaxTicks 是一间房最多打多久(五分钟)，到时没打完算输。
	MaxTicks = 5 * 60 * FPS
	// Dirs 是一圈分成几个方向；trig 是余弦表的放大倍数。
	Dirs = 64
	trig = 1024
)

// 按键位：攻击、法术、身法、换法宝。输入里记的是这一帧按住了哪些键，刚按下由规则自己比较上一帧得出。
const (
	BtnAttack = 1 << iota
	BtnSkill
	BtnDodge
	BtnSwap
)

// ErrSetup 表示开局的流派、法宝、房间或敌人不认识；ErrReplay 表示操作记录不合法(帧数超限、数值越界、打完以后还有操作)。
var (
	ErrSetup  = errors.New("doufa: invalid setup")
	ErrReplay = errors.New("doufa: invalid replay")
)

// cosTable 是 64 个方向的余弦乘 1024 取整；正弦是同一张表错开四分之一圈(sinOf)。方向 0 朝右，16 朝下(画面的 y 向下)。
var cosTable = [Dirs]int{
	1024, 1019, 1004, 980, 946, 903, 851, 792, 724, 650, 569, 483, 392, 297, 200, 100,
	0, -100, -200, -297, -392, -483, -569, -650, -724, -792, -851, -903, -946, -980, -1004, -1019,
	-1024, -1019, -1004, -980, -946, -903, -851, -792, -724, -650, -569, -483, -392, -297, -200, -100,
	0, 100, 200, 297, 392, 483, 569, 650, 724, 792, 851, 903, 946, 980, 1004, 1019,
}

func cosOf(a int) int { return cosTable[a&(Dirs-1)] }
func sinOf(a int) int { return cosTable[(a+48)&(Dirs-1)] }

// angleOf 返回 (dx, dy) 最接近的方向：点积最大的那个，一样大时取编号小的。不用三角函数，两边算得一样。
func angleOf(dx, dy int) int {
	best, bestDot := 0, math.MinInt
	for i := 0; i < Dirs; i++ {
		if d := dx*cosTable[i] + dy*cosTable[(i+48)&(Dirs-1)]; d > bestDot {
			best, bestDot = i, d
		}
	}
	return best
}

// angleGap 是两个方向差几格(0 到 32)。
func angleGap(a, b int) int {
	d := (a - b) & (Dirs - 1)
	if d > Dirs/2 {
		d = Dirs - d
	}
	return d
}

// px 把像素换成单位，pxs 把像素每秒换成单位每帧。
func px(v int) int  { return v * Scale }
func pxs(v int) int { return v * Scale / FPS }

func fdiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func near(ax, ay, bx, by, d int) bool {
	dx, dy := ax-bx, ay-by
	return dx*dx+dy*dy <= d*d
}

func dist2(ax, ay, bx, by int) int {
	dx, dy := ax-bx, ay-by
	return dx*dx + dy*dy
}

// Pack 把一帧的操作压成一个整数：移动方向(-1 不动)、瞄准方向(-1 自动瞄最近的敌人)与按住的键。
func Pack(move, aim, buttons int) int {
	return (move + 1) | (aim+1)<<7 | (buttons&15)<<14
}

func unpack(in int) (move, aim, buttons int) {
	return (in & 127) - 1, (in>>7)&127 - 1, (in >> 14) & 15
}

// Setup 是开一间房要的东西：流派、两件法宝、房间模板与各波敌人；HP、MP 为 0 时满状态开局(都是点数，不是单位)。
type Setup struct {
	Class   string    `json:"cls"`
	Weapons []string  `json:"weapons"`
	Room    string    `json:"room"`
	Waves   [][]Group `json:"waves"`
	HP      int       `json:"hp"`
	MP      int       `json:"mp"`
}

// Player 是主角。MP 以百分之一点计(灵力 100 点存成 10000)，速度是单位每帧。
type Player struct {
	X, Y      int
	R         int
	HP, MaxHP int
	MP, MaxMP int
	Speed     int
	Face      int
	Class     string
	Weapons   []string
	Slot      int
	Cool      int
	SkillCool int
	DodgeCool int
	Dash      int
	DashDir   int
	Hurt      int
	Swing     int
	Blades    int
	Spin      int
}

// Foe 是一个敌人。State、T、A、N 由各自的行为解释(蓄力、冲撞方向、招式里第几发)；KX、KY 是被击退的速度。
type Foe struct {
	Id, X, Y, R int
	Kind        string
	HP, MaxHP   int
	State, T    int
	A, N        int
	Cool        int
	Touch       int
	BladeHit    int
	KX, KY      int
	Flash       int
	Phase       int
	Pat         int
}

// Shot 是一发飞弹。Hostile 为真是敌人放的；Pierce 是还能再穿过几个敌人，Hit 记已经打中过的敌人。
type Shot struct {
	Id, X, Y, VX, VY, R int
	A                   int
	Dmg                 int
	Hostile             bool
	Life                int
	Pierce              int
	Hit                 []int
	Kind                string
}

// Drop 是地上的掉落：stone 灵石、heal 回血、mana 回灵力。
type Drop struct {
	Id   int
	Kind string
	X, Y int
	N    int
}

// Mark 是下一波敌人出现前地上的标记，MarkT 帧以后变成敌人。
type Mark struct {
	Kind string
	X, Y int
}

// Ev 是这一帧发生的事，只给画面用(打中、倒下、拾取、受伤……)，不参与规则。
type Ev struct {
	K    string
	X, Y int
	N    int
}

// Fight 是一间房的全部状态。
type Fight struct {
	Tick   int
	Rng    uint32
	W, H   int
	Solid  []bool
	Rocks  []bool
	BossX  int
	BossY  int
	P      Player
	Foes   []*Foe
	Shots  []*Shot
	Drops  []*Drop
	Waves  [][]Group
	Wave   int
	Marks  []Mark
	MarkT  int
	NextId int
	Kills  int
	Stones int
	Taken  int
	Dealt  int
	Prev   int
	Over   bool
	Won    bool
	Hash   uint32
	Events []Ev
}

// markTicks 是出怪标记亮多久；autoAimRange 是自动瞄准最远多少像素。
const (
	markTicks    = 50
	autoAimRange = 420
	touchCool    = 30
	hashStart    = 2166136261
	hashPrime    = 16777619
)

// New 开一间房。种子为 0 时按 1 算。
func New(seed uint32, setup Setup) (*Fight, error) {
	class, ok := content.Classes[setup.Class]
	rows, roomOk := content.Rooms[setup.Room]
	if !ok || !roomOk || len(setup.Weapons) < 1 || len(setup.Weapons) > 2 || len(setup.Waves) < 1 {
		return nil, ErrSetup
	}
	for _, w := range setup.Weapons {
		if _, ok := content.Weapons[w]; !ok {
			return nil, ErrSetup
		}
	}
	for _, wave := range setup.Waves {
		if len(wave) < 1 {
			return nil, ErrSetup
		}
		for _, g := range wave {
			if _, ok := content.Foes[g.Foe]; !ok || g.N < 1 || g.N > 12 {
				return nil, ErrSetup
			}
		}
	}
	if seed == 0 {
		seed = 1
	}
	f := &Fight{Rng: seed, H: len(rows), W: len(rows[0]), Waves: setup.Waves, NextId: 1, Hash: hashStart}
	f.Solid = make([]bool, f.W*f.H)
	f.Rocks = make([]bool, f.W*f.H)
	px0, py0 := -1, -1
	for y, row := range rows {
		if len(row) != f.W {
			return nil, ErrSetup
		}
		for x := 0; x < f.W; x++ {
			switch row[x] {
			case '#':
				f.Solid[y*f.W+x] = true
			case 'o':
				f.Solid[y*f.W+x] = true
				f.Rocks[y*f.W+x] = true
			case 'P':
				px0, py0 = x, y
			case 'B':
				f.BossX, f.BossY = x*Tile+Tile/2, y*Tile+Tile/2
			}
		}
	}
	if px0 < 0 {
		return nil, ErrSetup
	}
	f.P = Player{
		X: px0*Tile + Tile/2, Y: py0*Tile + Tile/2, R: px(content.Player.Radius),
		HP: class.HP, MaxHP: class.HP, MP: class.MP * 100, MaxMP: class.MP * 100,
		Speed: pxs(class.Speed), Face: 48, Class: setup.Class, Weapons: append([]string(nil), setup.Weapons...),
	}
	if setup.HP > 0 && setup.HP < class.HP {
		f.P.HP = setup.HP
	}
	if setup.MP > 0 && setup.MP < class.MP {
		f.P.MP = setup.MP * 100
	}
	f.markWave()
	return f, nil
}

func (f *Fight) rand(n int) int {
	s := f.Rng
	s ^= s << 13
	s ^= s >> 17
	s ^= s << 5
	f.Rng = s
	return int(s % uint32(n))
}

func (f *Fight) ev(k string, x, y, n int) {
	f.Events = append(f.Events, Ev{K: k, X: x, Y: y, N: n})
}

func (f *Fight) solidAt(tx, ty int) bool {
	if tx < 0 || ty < 0 || tx >= f.W || ty >= f.H {
		return true
	}
	return f.Solid[ty*f.W+tx]
}

// blocked 判断半径 r 的圆放在 (x, y) 是否碰到墙或石头。
func (f *Fight) blocked(x, y, r int) bool {
	for ty := fdiv(y-r, Tile); ty <= fdiv(y+r, Tile); ty++ {
		for tx := fdiv(x-r, Tile); tx <= fdiv(x+r, Tile); tx++ {
			if !f.solidAt(tx, ty) {
				continue
			}
			cx := min(max(x, tx*Tile), tx*Tile+Tile)
			cy := min(max(y, ty*Tile), ty*Tile+Tile)
			if near(x, y, cx, cy, r-1) {
				return true
			}
		}
	}
	return false
}

// move 让半径 r 的圆走 (dx, dy)，先横后竖；撞墙时试着走一半、四分之一。hit 表示撞到了。
func (f *Fight) move(x, y, r, dx, dy int) (int, int, bool) {
	hit := false
	for i, step := 0, dx; i < 3 && step != 0; i, step = i+1, step/2 {
		if !f.blocked(x+step, y, r) {
			x += step
			break
		}
		hit = true
	}
	for i, step := 0, dy; i < 3 && step != 0; i, step = i+1, step/2 {
		if !f.blocked(x, y+step, r) {
			y += step
			break
		}
		hit = true
	}
	return x, y, hit
}

// markWave 在地上标出下一波敌人的位置：首领站在房间模板的 B 处，其余挑离主角至少五格的空地，彼此不重叠。
func (f *Fight) markWave() {
	if f.Wave >= len(f.Waves) {
		return
	}
	f.Marks = f.Marks[:0]
	for _, g := range f.Waves[f.Wave] {
		for n := 0; n < g.N; n++ {
			if content.Foes[g.Foe].AI == "boss" {
				f.Marks = append(f.Marks, Mark{Kind: g.Foe, X: f.BossX, Y: f.BossY})
				continue
			}
			x, y := f.freeSpot()
			f.Marks = append(f.Marks, Mark{Kind: g.Foe, X: x, Y: y})
		}
	}
	f.MarkT = markTicks
}

// freeSpot 随机挑一块离主角至少五格、离别的标记至少一格半的空地，挑两百次都不行就取最后一次的空地。
func (f *Fight) freeSpot() (int, int) {
	x, y := f.P.X, f.P.Y
	for try := 0; try < 200; try++ {
		tx, ty := 1+f.rand(f.W-2), 1+f.rand(f.H-2)
		if f.solidAt(tx, ty) {
			continue
		}
		x, y = tx*Tile+Tile/2, ty*Tile+Tile/2
		if near(x, y, f.P.X, f.P.Y, 5*Tile) {
			continue
		}
		crowded := false
		for _, m := range f.Marks {
			if near(x, y, m.X, m.Y, Tile*3/2) {
				crowded = true
				break
			}
		}
		if !crowded {
			return x, y
		}
	}
	return x, y
}

func (f *Fight) spawn(kind string, x, y int) *Foe {
	spec := content.Foes[kind]
	e := &Foe{Id: f.NextId, Kind: kind, X: x, Y: y, R: px(spec.Radius), HP: spec.HP, MaxHP: spec.HP}
	f.NextId++
	e.N = e.Id % 2
	e.Cool = 40 + f.rand(60)
	f.Foes = append(f.Foes, e)
	f.ev("spawn", x, y, 0)
	return e
}

// Step 走一帧。打完(赢、输或到时)以后什么都不做。
func (f *Fight) Step(in int) {
	if f.Over {
		return
	}
	f.Events = f.Events[:0]
	f.Tick++
	f.stepPlayer(in)
	n := len(f.Foes)
	for i := 0; i < n; i++ {
		if e := f.Foes[i]; e.HP > 0 {
			f.stepFoe(e)
		}
	}
	f.stepShots()
	f.stepDrops()
	f.reap()
	if f.MarkT > 0 {
		if f.MarkT--; f.MarkT == 0 {
			for _, m := range f.Marks {
				f.spawn(m.Kind, m.X, m.Y)
			}
			f.Marks = f.Marks[:0]
			f.Wave++
		}
	} else if len(f.Foes) == 0 {
		if f.Wave < len(f.Waves) {
			f.markWave()
		} else {
			for _, d := range f.Drops {
				f.pick(d)
			}
			f.Drops = f.Drops[:0]
			f.Won, f.Over = true, true
		}
	}
	if f.P.HP <= 0 || f.Tick >= MaxTicks {
		f.Over = true
	}
	f.mixHash()
}

func (f *Fight) mixHash() {
	h := f.Hash
	mix := func(v int) { h = (h ^ uint32(v)) * hashPrime }
	mix(f.Tick)
	mix(f.P.X)
	mix(f.P.Y)
	mix(f.P.HP)
	mix(f.P.MP)
	for _, e := range f.Foes {
		mix(e.X)
		mix(e.Y)
		mix(e.HP)
	}
	mix(len(f.Shots))
	f.Hash = h
}

func (f *Fight) stepPlayer(in int) {
	p := &f.P
	moveDir, aim, buttons := unpack(in)
	pressed := buttons &^ f.Prev
	f.Prev = buttons
	for _, t := range []*int{&p.Cool, &p.SkillCool, &p.DodgeCool, &p.Hurt, &p.Swing} {
		if *t > 0 {
			*t--
		}
	}
	p.MP = min(p.MaxMP, p.MP+content.Player.MPRegen*100/FPS)
	if aim >= 0 {
		p.Face = aim
	} else if e := f.nearestFoe(); e != nil {
		p.Face = angleOf(e.X-p.X, e.Y-p.Y)
	} else if moveDir >= 0 {
		p.Face = moveDir
	}
	dodge := content.Player.Dodge
	if pressed&BtnDodge != 0 && p.DodgeCool == 0 && p.Dash == 0 {
		p.Dash, p.DashDir, p.DodgeCool = dodge.Ticks, p.Face, dodge.Cooldown
		if moveDir >= 0 {
			p.DashDir = moveDir
		}
		f.ev("dodge", p.X, p.Y, p.DashDir)
	}
	if p.Dash > 0 {
		sp := pxs(dodge.Speed)
		p.X, p.Y, _ = f.move(p.X, p.Y, p.R, sp*cosOf(p.DashDir)/trig, sp*sinOf(p.DashDir)/trig)
		p.Dash--
	} else if moveDir >= 0 {
		p.X, p.Y, _ = f.move(p.X, p.Y, p.R, p.Speed*cosOf(moveDir)/trig, p.Speed*sinOf(moveDir)/trig)
	}
	if pressed&BtnSwap != 0 && len(p.Weapons) > 1 {
		p.Slot = 1 - p.Slot
		p.Cool = max(p.Cool, 8)
		f.ev("swap", p.X, p.Y, p.Slot)
	}
	if buttons&BtnAttack != 0 && p.Cool == 0 && p.Dash == 0 {
		f.fire()
	}
	skill := content.Classes[p.Class].Skill
	if pressed&BtnSkill != 0 && p.SkillCool == 0 && p.Dash == 0 {
		if p.MP >= skill.Cost*100 {
			f.cast(skill)
		} else {
			f.ev("dry", p.X, p.Y, 0)
		}
	}
	if p.Blades > 0 {
		f.stepBlades(skill)
	}
}

// nearestFoe 是自动瞄准的目标：自动瞄准范围内最近的敌人，一样近取先出场的。
func (f *Fight) nearestFoe() *Foe {
	var best *Foe
	bestD := px(autoAimRange) * px(autoAimRange)
	for _, e := range f.Foes {
		if e.HP <= 0 {
			continue
		}
		if d := dist2(e.X, e.Y, f.P.X, f.P.Y); d < bestD {
			best, bestD = e, d
		}
	}
	return best
}

// fire 用手上的法宝出手：近战挥砍扇形，顺手打掉扇形里的敌方飞弹；远程按发数散开放飞弹，灵力不够时只提示。
func (f *Fight) fire() {
	p := &f.P
	w := content.Weapons[p.Weapons[p.Slot]]
	if w.Kind == "melee" {
		p.Cool, p.Swing = w.Cooldown, 10
		f.ev("swing", p.X, p.Y, p.Face)
		reach := px(w.Range)
		for _, e := range f.Foes {
			if e.HP <= 0 || !near(e.X, e.Y, p.X, p.Y, reach+e.R) {
				continue
			}
			a := angleOf(e.X-p.X, e.Y-p.Y)
			if angleGap(a, p.Face) <= w.Arc || near(e.X, e.Y, p.X, p.Y, p.R+e.R) {
				f.hitFoe(e, w.Damage, a, w.Knock)
			}
		}
		kept := f.Shots[:0]
		for _, s := range f.Shots {
			if s.Hostile && near(s.X, s.Y, p.X, p.Y, reach+s.R) && angleGap(angleOf(s.X-p.X, s.Y-p.Y), p.Face) <= w.Arc {
				f.ev("deflect", s.X, s.Y, 0)
				continue
			}
			kept = append(kept, s)
		}
		f.Shots = kept
		return
	}
	if p.MP < w.Cost*100 {
		p.Cool = 10
		f.ev("dry", p.X, p.Y, 0)
		return
	}
	p.MP -= w.Cost * 100
	p.Cool = w.Cooldown
	kind := p.Weapons[p.Slot]
	for i := 0; i < w.Count; i++ {
		a := (p.Face + (2*i-(w.Count-1))*w.Spread/2) & (Dirs - 1)
		f.shoot(p.X, p.Y, p.R, a, w.Speed, w.Radius, w.Damage, w.Life, w.Pierce, false, kind)
	}
	f.ev("shoot", p.X, p.Y, p.Face)
}

// shoot 从半径 r 的身体边上朝方向 a 放一发飞弹。
func (f *Fight) shoot(x, y, r, a, speed, radius, dmg, life, pierce int, hostile bool, kind string) {
	sp := pxs(speed)
	off := r + px(2)
	f.Shots = append(f.Shots, &Shot{
		Id: f.NextId, X: x + off*cosOf(a)/trig, Y: y + off*sinOf(a)/trig, VX: sp * cosOf(a) / trig, VY: sp * sinOf(a) / trig,
		R: px(radius), A: a, Dmg: dmg, Hostile: hostile, Life: life, Pierce: pierce, Kind: kind,
	})
	f.NextId++
}

// cast 放流派法术。
func (f *Fight) cast(s SkillSpec) {
	p := &f.P
	p.MP -= s.Cost * 100
	p.SkillCool = s.Cooldown
	f.ev("cast", p.X, p.Y, 0)
	switch s.Kind {
	case "quake":
		reach := px(s.Radius)
		for _, e := range f.Foes {
			if e.HP > 0 && near(e.X, e.Y, p.X, p.Y, reach+e.R) {
				f.hitFoe(e, s.Damage, angleOf(e.X-p.X, e.Y-p.Y), s.Knock)
			}
		}
		kept := f.Shots[:0]
		for _, sh := range f.Shots {
			if sh.Hostile && near(sh.X, sh.Y, p.X, p.Y, reach) {
				continue
			}
			kept = append(kept, sh)
		}
		f.Shots = kept
		p.Hurt = max(p.Hurt, 20)
	case "nova":
		for i := 0; i < s.Count; i++ {
			f.shoot(p.X, p.Y, p.R, i*Dirs/s.Count, s.Speed, s.Radius, s.Damage, s.Life, 0, false, "fire")
		}
	case "blades":
		p.Blades = s.Ticks
	}
}

// stepBlades 转一格剑阵：每把飞剑碰到的敌人受伤(同一个敌人隔 Rehit 帧才再挨一下)，碰到的敌方飞弹打掉。
func (f *Fight) stepBlades(s SkillSpec) {
	p := &f.P
	p.Blades--
	p.Spin = (p.Spin + 2) & (Dirs - 1)
	orbit, br := px(s.Orbit), px(s.Radius)
	for k := 0; k < s.Count; k++ {
		a := (p.Spin + k*Dirs/s.Count) & (Dirs - 1)
		bx, by := p.X+orbit*cosOf(a)/trig, p.Y+orbit*sinOf(a)/trig
		for _, e := range f.Foes {
			if e.HP > 0 && e.BladeHit == 0 && near(bx, by, e.X, e.Y, br+e.R) {
				f.hitFoe(e, s.Damage, a, 0)
				e.BladeHit = s.Rehit
			}
		}
		kept := f.Shots[:0]
		for _, sh := range f.Shots {
			if sh.Hostile && near(sh.X, sh.Y, bx, by, br+sh.R) {
				continue
			}
			kept = append(kept, sh)
		}
		f.Shots = kept
	}
}

// hitFoe 打中一个敌人；knock 大于 0 时朝方向 a 击退(首领与原地不动的不被击退)。
func (f *Fight) hitFoe(e *Foe, dmg, a, knock int) {
	if e.HP <= 0 {
		return
	}
	e.HP -= dmg
	f.Dealt += dmg
	e.Flash = 6
	f.ev("hit", e.X, e.Y, dmg)
	spec := content.Foes[e.Kind]
	if knock > 0 && spec.AI != "boss" && spec.AI != "turret" {
		e.KX, e.KY = pxs(knock)*cosOf(a)/trig, pxs(knock)*sinOf(a)/trig
	}
}

// hurt 主角受伤；冲刺中或刚受过伤(无敌帧)时不受。
func (f *Fight) hurt(dmg int) bool {
	p := &f.P
	if p.Hurt > 0 || p.Dash > 0 {
		return false
	}
	p.HP = max(p.HP-dmg, 0)
	f.Taken += dmg
	p.Hurt = content.Player.Iframes
	f.ev("hurt", p.X, p.Y, dmg)
	return true
}

func (f *Fight) stepShots() {
	p := &f.P
	kept := f.Shots[:0]
	for _, s := range f.Shots {
		s.X += s.VX
		s.Y += s.VY
		s.Life--
		if s.Life <= 0 || f.solidAt(fdiv(s.X, Tile), fdiv(s.Y, Tile)) {
			f.ev("fizzle", s.X, s.Y, 0)
			continue
		}
		if s.Hostile {
			if near(s.X, s.Y, p.X, p.Y, s.R+p.R-px(2)) && f.hurt(s.Dmg) {
				continue
			}
			kept = append(kept, s)
			continue
		}
		alive := true
		for _, e := range f.Foes {
			if e.HP <= 0 || !near(s.X, s.Y, e.X, e.Y, s.R+e.R) || containsId(s.Hit, e.Id) {
				continue
			}
			f.hitFoe(e, s.Dmg, s.A, 0)
			if s.Pierce <= 0 {
				alive = false
				break
			}
			s.Pierce--
			s.Hit = append(s.Hit, e.Id)
		}
		if alive {
			kept = append(kept, s)
		}
	}
	f.Shots = kept
}

func containsId(ids []int, id int) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func (f *Fight) stepDrops() {
	reach := f.P.R + px(content.Player.Pickup)
	kept := f.Drops[:0]
	for _, d := range f.Drops {
		if near(d.X, d.Y, f.P.X, f.P.Y, reach) {
			f.pick(d)
			continue
		}
		kept = append(kept, d)
	}
	f.Drops = kept
}

func (f *Fight) pick(d *Drop) {
	p := &f.P
	switch d.Kind {
	case "stone":
		f.Stones += d.N
	case "heal":
		p.HP = min(p.MaxHP, p.HP+d.N)
	case "mana":
		p.MP = min(p.MaxMP, p.MP+d.N*100)
	}
	f.ev("pick", d.X, d.Y, d.N)
}

// reap 收走倒下的敌人并掉落：首领掉十块灵石；小怪三成五掉灵石，一成回血，一成三回灵力。
func (f *Fight) reap() {
	kept := f.Foes[:0]
	for _, e := range f.Foes {
		if e.HP > 0 {
			kept = append(kept, e)
			continue
		}
		f.Kills++
		f.ev("die", e.X, e.Y, 0)
		if content.Foes[e.Kind].AI == "boss" {
			f.drop("stone", e.X, e.Y, 10)
			continue
		}
		switch r := f.rand(100); {
		case r < 35:
			f.drop("stone", e.X, e.Y, 1+f.rand(2))
		case r < 45:
			f.drop("heal", e.X, e.Y, 12)
		case r < 58:
			f.drop("mana", e.X, e.Y, 20)
		}
	}
	f.Foes = kept
}

func (f *Fight) drop(kind string, x, y, n int) {
	f.Drops = append(f.Drops, &Drop{Id: f.NextId, Kind: kind, X: x, Y: y, N: n})
	f.NextId++
}

// Summary 是一间房打完(或打到这里)的要点与逐帧校验和，黄金回放按它核对两边的规则一致。
type Summary struct {
	Tick   int    `json:"tick"`
	HP     int    `json:"hp"`
	MP     int    `json:"mp"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Kills  int    `json:"kills"`
	Stones int    `json:"stones"`
	Taken  int    `json:"taken"`
	Dealt  int    `json:"dealt"`
	Wave   int    `json:"wave"`
	Foes   int    `json:"foes"`
	Over   bool   `json:"over"`
	Won    bool   `json:"won"`
	Hash   uint32 `json:"hash"`
}

func (f *Fight) Summary() Summary {
	return Summary{Tick: f.Tick, HP: f.P.HP, MP: f.P.MP, X: f.P.X, Y: f.P.Y, Kills: f.Kills, Stones: f.Stones,
		Taken: f.Taken, Dealt: f.Dealt, Wave: f.Wave, Foes: len(f.Foes), Over: f.Over, Won: f.Won, Hash: f.Hash}
}

// Replay 按操作记录重放一间房。帧数超过上限、输入越界、打完以后还有操作都算不合法。
func Replay(seed uint32, setup Setup, inputs []int) (*Fight, error) {
	if len(inputs) > MaxTicks {
		return nil, ErrReplay
	}
	f, err := New(seed, setup)
	if err != nil {
		return nil, err
	}
	for _, in := range inputs {
		moveDir, aim, _ := unpack(in)
		if in < 0 || in >= 1<<18 || moveDir >= Dirs || aim >= Dirs || f.Over {
			return nil, ErrReplay
		}
		f.Step(in)
	}
	return f, nil
}
