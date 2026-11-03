package doufa

// shotLife 是敌方飞弹最多飞几帧(撞墙就没了，这只是兜底)。
const shotLife = 400

// stepFoe 走一个敌人的一帧：被击退时只往后滑、不出手；否则按行为走位、蓄力、出手。贴着主角时造成接触伤害。
func (f *Fight) stepFoe(e *Foe) {
	spec := content.Foes[e.Kind]
	for _, t := range []*int{&e.Touch, &e.BladeHit, &e.Flash} {
		if *t > 0 {
			*t--
		}
	}
	if e.KX != 0 || e.KY != 0 {
		e.X, e.Y, _ = f.move(e.X, e.Y, e.R, e.KX, e.KY)
		e.KX, e.KY = e.KX*7/10, e.KY*7/10
		if abs(e.KX) < 20 && abs(e.KY) < 20 {
			e.KX, e.KY = 0, 0
		}
		f.touch(e, spec)
		return
	}
	switch spec.AI {
	case "chase":
		f.chaser(e, spec)
	case "shoot":
		f.shooter(e, spec)
	case "charge":
		f.charger(e, spec)
	case "turret":
		f.turret(e, spec)
	case "boss":
		f.boss(e, spec)
	}
	f.touch(e, spec)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (f *Fight) touch(e *Foe, spec FoeSpec) {
	if e.Touch == 0 && spec.Touch > 0 && near(e.X, e.Y, f.P.X, f.P.Y, e.R+f.P.R) && f.hurt(spec.Touch*f.FoeDmg/100) {
		e.Touch = touchCool
	}
}

// chaser 追着主角走，贴上就停；被墙挡住走不动时朝一侧绕 30 帧再接着追(N 记绕哪一侧，每次卡住换一侧)。
func (f *Fight) chaser(e *Foe, spec FoeSpec) {
	p := &f.P
	if near(e.X, e.Y, p.X, p.Y, e.R+p.R) {
		return
	}
	a := angleOf(p.X-e.X, p.Y-e.Y)
	if e.T > 0 {
		e.T--
		a = (a + 16 - 32*e.N) & (Dirs - 1)
	}
	x, y := e.X, e.Y
	sp := pxs(spec.Speed)
	if f.walk(e, a, sp) && abs(e.X-x)+abs(e.Y-y) < sp/2 && e.T == 0 {
		e.T = 30
		e.N ^= 1
	}
}

// walk 让敌人朝方向 a 走一步(sp 是单位每帧)，返回是否撞墙。
func (f *Fight) walk(e *Foe, a, sp int) bool {
	var hit bool
	e.X, e.Y, hit = f.move(e.X, e.Y, e.R, sp*cosOf(a)/trig, sp*sinOf(a)/trig)
	return hit
}

// volley 朝方向 a 放一排飞弹：Count 发，相邻两发差 Spread 个方向。
func (f *Fight) volley(e *Foe, s ShotSpec, a int) {
	for i := 0; i < s.Count; i++ {
		f.shoot(e.X, e.Y, e.R, (a+(2*i-(s.Count-1))*s.Spread/2)&(Dirs-1), s.Speed, s.Radius, s.Damage*f.FoeDmg/100, shotLife, 0, true, e.Kind)
	}
}

// ring 从 offset 起向四周均匀放 count 发飞弹。
func (f *Fight) ring(e *Foe, count, offset, speed, radius, dmg int) {
	for i := 0; i < count; i++ {
		f.shoot(e.X, e.Y, e.R, (offset+i*Dirs/count)&(Dirs-1), speed, radius, dmg*f.FoeDmg/100, shotLife, 0, true, e.Kind)
	}
}

// shooter 保持距离放弹：太远就靠近，太近就后退，否则绕着主角横移(撞墙换边)；冷却好了先蓄力(这时锁定方向、站着不动)，
// 蓄满朝锁定的方向放一排飞弹，所以看到它蓄力就该挪开。
func (f *Fight) shooter(e *Foe, spec FoeSpec) {
	p := &f.P
	if e.State == 1 {
		if e.T--; e.T <= 0 {
			f.volley(e, spec.Shot, e.A)
			e.State, e.Cool = 0, spec.Cooldown+f.rand(40)
		}
		return
	}
	if e.Cool > 0 {
		e.Cool--
	}
	a := angleOf(p.X-e.X, p.Y-e.Y)
	d2 := dist2(p.X, p.Y, e.X, e.Y)
	reach := px(spec.Range)
	far, close := reach+px(30), reach-px(60)
	dir := (a + 16 - 32*e.N) & (Dirs - 1)
	if d2 > far*far {
		dir = a
	} else if d2 < close*close {
		dir = (a + 32) & (Dirs - 1)
	}
	if f.walk(e, dir, pxs(spec.Speed)) && dir != a {
		e.N ^= 1
	}
	if alert := reach + px(120); e.Cool == 0 && d2 < alert*alert {
		e.State, e.T, e.A = 1, spec.Windup, a
	}
}

// charger 蓄力冲撞：慢慢靠近，进了范围先蓄力(锁定方向)，再朝那个方向猛冲，撞墙或冲完喘一会儿，喘气时是反击的时机。
func (f *Fight) charger(e *Foe, spec FoeSpec) {
	p := &f.P
	switch e.State {
	case 0:
		if e.Cool > 0 {
			e.Cool--
		}
		a := angleOf(p.X-e.X, p.Y-e.Y)
		if !near(e.X, e.Y, p.X, p.Y, e.R+p.R) {
			f.walk(e, a, pxs(spec.Speed))
		}
		if e.Cool == 0 && near(e.X, e.Y, p.X, p.Y, px(spec.Range)) {
			e.State, e.T, e.A = 1, spec.Windup, a
		}
	case 1:
		if e.T--; e.T <= 0 {
			e.State, e.T = 2, spec.DashTicks
		}
	case 2:
		hit := f.walk(e, e.A, pxs(spec.Dash))
		if e.T--; hit || e.T <= 0 {
			e.State, e.T = 3, spec.Rest
		}
	default:
		if e.T--; e.T <= 0 {
			e.State, e.Cool = 0, 50
		}
	}
}

// turret 原地放弹环，每次转一个角度，冷却快好时画面会提示。
func (f *Fight) turret(e *Foe, spec FoeSpec) {
	if e.Cool > 0 {
		e.Cool--
		return
	}
	f.ring(e, spec.Shot.Count, e.A, spec.Shot.Speed, spec.Shot.Radius, spec.Shot.Damage)
	e.A = (e.A + spec.Spin) & (Dirs - 1)
	e.Cool = spec.Cooldown
}

// boss 是首领：走动一阵就出一套招，喷射、弹环、召唤、冲撞依次轮换；生命掉到一半进入第二阶段，弹更多、间隔更短。
// State：0 走动，1 喷射，2 弹环，3 冲撞蓄力，4 冲撞。
func (f *Fight) boss(e *Foe, spec FoeSpec) {
	b := spec.Boss
	p := &f.P
	if e.Phase == 0 && e.HP*2 <= e.MaxHP {
		e.Phase = 1
		f.ev("phase", e.X, e.Y, 1)
	}
	gap, spray, ringN := b.Gap, b.Spray.Count, b.Ring.Count
	if e.Phase > 0 {
		gap, spray, ringN = b.Gap2, b.Spray.Count2, b.Ring.Count2
	}
	a := angleOf(p.X-e.X, p.Y-e.Y)
	switch e.State {
	case 0:
		if !near(e.X, e.Y, p.X, p.Y, e.R+p.R+px(40)) {
			f.walk(e, a, pxs(spec.Speed))
		}
		if e.Cool > 0 {
			e.Cool--
			return
		}
		switch e.Pat {
		case 0:
			e.State, e.N, e.T = 1, b.Spray.Volleys, 1
		case 1:
			e.State, e.N, e.T = 2, b.Ring.Volleys, 1
		case 2:
			f.summon(e, b)
			e.Cool = gap
		default:
			e.State, e.T, e.A = 3, b.Charge.Windup, a
		}
		e.Pat = (e.Pat + 1) % 4
	case 1:
		if e.T--; e.T > 0 {
			return
		}
		f.volley(e, ShotSpec{Damage: b.Spray.Damage, Speed: b.Spray.Speed, Radius: b.Spray.Radius, Count: spray, Spread: b.Spray.Spread}, a)
		if e.N--; e.N > 0 {
			e.T = b.Spray.Every
		} else {
			e.State, e.Cool = 0, gap
		}
	case 2:
		if e.T--; e.T > 0 {
			return
		}
		f.ring(e, ringN, e.A, b.Ring.Speed, b.Ring.Radius, b.Ring.Damage)
		e.A = (e.A + b.Ring.Turn) & (Dirs - 1)
		if e.N--; e.N > 0 {
			e.T = b.Ring.Every
		} else {
			e.State, e.Cool = 0, gap
		}
	case 3:
		if e.T--; e.T <= 0 {
			e.State, e.T = 4, b.Charge.Ticks
		}
	default:
		hit := f.walk(e, e.A, pxs(b.Charge.Dash))
		if e.T--; hit || e.T <= 0 {
			e.State, e.Cool = 0, gap
		}
	}
}

// summon 在首领身边放出几只小怪(场上已经满了就不放)，从随机的方向起找不挡路的空地。
func (f *Fight) summon(e *Foe, b *BossSpec) {
	r := px(content.Foes[b.Summon.Kind].Radius)
	for i := 0; i < b.Summon.Count && len(f.Foes) < b.Summon.Cap; i++ {
		start := f.rand(Dirs)
		for k := 0; k < 8; k++ {
			a := (start + k*8) & (Dirs - 1)
			d := e.R + r + px(8)
			x, y := e.X+d*cosOf(a)/trig, e.Y+d*sinOf(a)/trig
			if !f.blocked(x, y, r) {
				f.spawn(b.Summon.Kind, x, y).Cool = 40
				break
			}
		}
	}
}
