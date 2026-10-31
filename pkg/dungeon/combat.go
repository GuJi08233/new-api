package dungeon

import "slices"

// 每回合的基础能量、抽牌数与手牌上限。
const (
	baseEnergy = 3
	baseDraw   = 5
	handLimit  = 10
)

// Enemy 是战斗中的一个敌人。Move 是已经公布的下一招(Moves 的下标)，Last 是最近用过的两招，用来避免同一招连用三次。
type Enemy struct {
	Id     string         `json:"id"`
	HP     int            `json:"hp"`
	MaxHP  int            `json:"max_hp"`
	Block  int            `json:"block"`
	St     map[string]int `json:"st"`
	Move   int            `json:"move"`
	Last   []int          `json:"last"`
	Step   int            `json:"step"` // 按 Cycle 出招的敌人走到了第几招
	DmgPct int            `json:"dmg_pct"`
	Dead   bool           `json:"dead"`
}

func (e *Enemy) def() *EnemyDef { return enemyDefs[e.Id] }

// Combat 是一场战斗。玩家的生命在 Run 里，这里只记护甲、状态、能量与牌堆。
type Combat struct {
	Kind    string         `json:"kind"` // combat、elite 或 boss
	Turn    int            `json:"turn"`
	Energy  int            `json:"energy"`
	Block   int            `json:"block"`
	St      map[string]int `json:"st"`
	Draw    []Card         `json:"draw"`
	Hand    []Card         `json:"hand"`
	Discard []Card         `json:"discard"`
	Exhaust []Card         `json:"exhaust"`
	Enemies []*Enemy       `json:"enemies"`
	// Played 是本回合打出的牌数。
	Played int `json:"played"`
	// Dealt、TurnDealt 是这场战斗、本回合敌人实际失去的生命(含灼烧)，MaxHit 是单次攻击的最高伤害(计护甲前)，练习模式据此测试伤害。
	Dealt     int `json:"dealt"`
	TurnDealt int `json:"turn_dealt"`
	MaxHit    int `json:"max_hit"`
}

// Ev 是一次操作里发生的一件事，页面按顺序播放：T 为 -1 指玩家，其余是敌人下标。
type Ev struct {
	Kind string `json:"k"`
	T    int    `json:"t"`
	N    int    `json:"n,omitempty"`
	S    string `json:"s,omitempty"`
}

const playerTarget = -1

func (r *Run) emit(kind string, target, n int, s string) {
	r.Events = append(r.Events, Ev{Kind: kind, T: target, N: n, S: s})
}

// startCombat 生成敌人、洗牌并开始第一回合。每个敌人带着自己的生命与伤害倍率(百分比，100 为原样)，深渊等级的加成
// 再叠加在上面。
func (r *Run) startCombat(kind string, spawns []spawn) {
	c := &Combat{Kind: kind, St: map[string]int{}}
	for _, s := range spawns {
		id := s.Id
		def := enemyDefs[id]
		hp := r.Rng.Range(def.HP[0], def.HP[1])
		pct, dmgPct := s.HP, s.Dmg-100
		switch kind {
		case "elite":
			if r.Abyss >= 1 {
				pct += 20
			}
		case "boss":
			if r.Abyss >= 7 {
				pct += 15
			}
		default:
			if r.Abyss >= 8 {
				pct += 10
			}
			if r.Abyss >= 2 {
				dmgPct += 10
			}
		}
		if r.Abyss >= 10 {
			dmgPct += 10
		}
		hp = hp * pct / 100
		e := &Enemy{Id: id, HP: hp, MaxHP: hp, St: map[string]int{}, Last: []int{}, DmgPct: dmgPct}
		for status, n := range def.Start {
			e.St[status] = n
		}
		e.Block = e.St[StPlate]
		c.Enemies = append(c.Enemies, e)
	}
	c.Draw = slices.Clone(r.Deck)
	shuffle(&r.Rng, c.Draw)
	r.Combat = c
	r.Phase = PhaseCombat
	for _, e := range c.Enemies {
		r.chooseMove(e)
	}
	r.startTurn()
}

// chooseMove 公布敌人的下一招。
func (r *Run) chooseMove(e *Enemy) {
	def := e.def()
	if len(def.Cycle) > 0 {
		e.Move = def.Cycle[e.Step%len(def.Cycle)]
		e.Step++
		return
	}
	weights := slices.Clone(def.Weights)
	if n := len(e.Last); n >= 2 && e.Last[n-1] == e.Last[n-2] {
		weights[e.Last[n-1]] = 0
	}
	e.Move = pickWeighted(&r.Rng, weights)
}

// startTurn 开始玩家的回合：清护甲、结算灼烧、回满能量、抽牌，以及遗物与能力的回合开始效果。
func (r *Run) startTurn() {
	c := r.Combat
	c.Turn++
	c.Played = 0
	c.TurnDealt = 0
	if c.St[StFortify] == 0 {
		c.Block = 0
	}
	if c.Turn == 1 {
		r.combatStartRelics()
	}
	if burn := c.St[StBurn]; burn > 0 {
		r.loseHP(burn, StBurn)
		c.St[StBurn]--
		if r.Phase != PhaseCombat {
			return
		}
	}
	c.Energy = baseEnergy + c.St[StWellspring]
	if r.hasRelic(RelicCrown) {
		c.Energy++
	}
	if c.Turn == 1 && r.hasRelic(RelicCrystal) {
		c.Energy++
	}
	if r.hasRelic(RelicFlame) {
		r.applyAllEnemies(StBurn, 1)
	}
	for i := 0; i < c.St[StBlades]; i++ {
		r.addToHand(Card{Id: KnifeCard})
	}
	draw := baseDraw
	if r.hasRelic(RelicTome) {
		draw++
	}
	if c.Turn == 1 && r.hasRelic(RelicScroll) {
		draw += 2
	}
	r.draw(draw)
	r.emit("turn", playerTarget, c.Turn, "")
}

func (r *Run) combatStartRelics() {
	c := r.Combat
	if r.hasRelic(RelicIronPlate) {
		r.gainBlock(8)
	}
	if r.hasRelic(RelicWhetstone) {
		c.St[StStrength]++
	}
	if r.hasRelic(RelicWarriorHeart) {
		c.St[StStrength]++
	}
	if r.hasRelic(RelicSpikedMail) {
		c.St[StThorns] += 3
	}
	if r.hasRelic(RelicHawkEye) {
		r.applyAllEnemies(StVuln, 1)
	}
	if r.hasRelic(RelicEmberStaff) {
		r.applyAllEnemies(StBurn, 2)
	}
	if r.hasRelic(RelicQuiver) {
		r.addToHand(Card{Id: KnifeCard})
		r.addToHand(Card{Id: KnifeCard})
	}
}

func (r *Run) draw(n int) {
	c := r.Combat
	for range n {
		if len(c.Hand) >= handLimit {
			return
		}
		if len(c.Draw) == 0 {
			if len(c.Discard) == 0 {
				return
			}
			c.Draw, c.Discard = c.Discard, []Card{}
			shuffle(&r.Rng, c.Draw)
			r.emit("shuffle", playerTarget, len(c.Draw), "")
		}
		c.Hand = append(c.Hand, c.Draw[len(c.Draw)-1])
		c.Draw = c.Draw[:len(c.Draw)-1]
	}
}

func (r *Run) addToHand(card Card) {
	c := r.Combat
	if len(c.Hand) >= handLimit {
		c.Discard = append(c.Discard, card)
		return
	}
	c.Hand = append(c.Hand, card)
}

// cardCost 是这张牌现在要花的能量，诅咒为 -1(不能打出)。
func cardCost(card Card) int {
	d := card.def()
	if card.Up {
		return d.Cost[1]
	}
	return d.Cost[0]
}

func cardExhausts(card Card) bool {
	d := card.def()
	if card.Up {
		return d.Exhaust[1]
	}
	return d.Exhaust[0]
}

func (r *Run) aliveEnemy(i int) bool {
	c := r.Combat
	return r.Phase == PhaseCombat && c != nil && i >= 0 && i < len(c.Enemies) && !c.Enemies[i].Dead
}

// playCard 打出手牌里第 index 张，target 是选择的敌人。
func (r *Run) playCard(index, target int) error {
	c := r.Combat
	if index < 0 || index >= len(c.Hand) {
		return ErrAction
	}
	card := c.Hand[index]
	d := card.def()
	cost := cardCost(card)
	if cost < 0 || (cost > c.Energy && !r.infiniteEnergy()) {
		return ErrAction
	}
	if r.infiniteEnergy() {
		cost = 0
	}
	if d.Target == TargetEnemy && !r.aliveEnemy(target) {
		return ErrAction
	}
	if d.Target != TargetEnemy {
		target = playerTarget
	}
	c.Energy -= cost
	c.Hand = slices.Delete(c.Hand, index, index+1)
	r.emit("play", target, index, card.Id)
	played := c.Played
	c.Played++
	for _, f := range d.Effects(card.Up) {
		r.resolve(card, f, target, played)
		if r.Phase != PhaseCombat {
			break
		}
	}
	switch {
	case d.Type == TypePower:
	case cardExhausts(card):
		c.Exhaust = append(c.Exhaust, card)
	default:
		c.Discard = append(c.Discard, card)
	}
	if r.Phase == PhaseCombat && c.Played == 3 && r.hasRelic(RelicMetronome) {
		r.draw(1)
	}
	return nil
}

// resolve 结算一条效果。played 是本回合在这张牌之前打出的牌数。
func (r *Run) resolve(card Card, f Fx, target int, played int) {
	c := r.Combat
	times := max(f.X, 1)
	switch f.Kind {
	case FxDamageBlock, FxPerCard, FxBurst, FxDoubleBurn, FxApply:
		// 同一张牌前面的效果可能已经击败了目标。
		if !r.aliveEnemy(target) {
			return
		}
	}
	switch f.Kind {
	case FxDamage:
		base := f.N
		if card.Id == KnifeCard {
			base += c.St[StKeen]
		}
		for range times {
			if !r.aliveEnemy(target) {
				return
			}
			r.playerAttack(target, base)
		}
	case FxDamageAll:
		for range times {
			for i := range c.Enemies {
				if r.aliveEnemy(i) {
					r.playerAttack(i, f.N)
				}
			}
		}
	case FxDamageRand:
		for range times {
			alive := r.aliveEnemies()
			if len(alive) == 0 {
				return
			}
			r.playerAttack(alive[r.Rng.Intn(len(alive))], f.N)
		}
	case FxDamageBlock:
		r.playerAttack(target, c.Block)
	case FxPerCard:
		r.playerAttack(target, f.N*played)
	case FxBurst:
		e := c.Enemies[target]
		burn := e.St[StBurn]
		delete(e.St, StBurn)
		r.playerAttack(target, burn*f.N)
	case FxDoubleBurn:
		e := c.Enemies[target]
		if burn := e.St[StBurn]; burn > 0 {
			e.St[StBurn] = burn * 2
			r.emit("apply", target, burn, StBurn)
		}
	case FxBlock:
		r.gainBlock(f.N)
	case FxDraw:
		r.draw(f.N)
	case FxEnergy:
		c.Energy += f.N
		r.emit("energy", playerTarget, f.N, "")
	case FxApply:
		r.applyEnemy(target, f.Status, f.N)
	case FxApplyAll:
		r.applyAllEnemies(f.Status, f.N)
	case FxApplySelf, FxPower:
		c.St[f.Status] += f.N
		r.emit("apply", playerTarget, f.N, f.Status)
	case FxAddCard:
		for range f.N {
			r.addToHand(Card{Id: f.Status})
		}
	case FxHeal:
		r.heal(f.N)
	case FxLoseHP:
		r.loseHP(f.N, "")
	}
}

func (r *Run) aliveEnemies() []int {
	var alive []int
	if r.Phase != PhaseCombat {
		return alive
	}
	for i := range r.Combat.Enemies {
		if r.aliveEnemy(i) {
			alive = append(alive, i)
		}
	}
	return alive
}

func (r *Run) applyEnemy(i int, status string, n int) {
	if !r.aliveEnemy(i) || n <= 0 {
		return
	}
	r.Combat.Enemies[i].St[status] += n
	r.emit("apply", i, n, status)
}

func (r *Run) applyAllEnemies(status string, n int) {
	for i := range r.Combat.Enemies {
		r.applyEnemy(i, status, n)
	}
}

func (r *Run) gainBlock(n int) {
	if n <= 0 {
		return
	}
	r.Combat.Block += n
	r.emit("block", playerTarget, n, "")
}

func (r *Run) heal(n int) {
	n = min(n, r.MaxHP-r.HP)
	if n <= 0 {
		return
	}
	r.HP += n
	r.emit("heal", playerTarget, n, "")
}

// AttackDamage 是一次攻击的伤害：基础伤害加力量，攻击者虚弱时减 25%，承受者易伤时加 50%，都向下取整，最少为 0。
func AttackDamage(base int, attacker, defender map[string]int) int {
	damage := max(base+attacker[StStrength], 0)
	if attacker[StWeak] > 0 {
		damage = damage * 3 / 4
	}
	if defender[StVuln] > 0 {
		damage = damage * 3 / 2
	}
	return damage
}

// playerAttack 玩家对一个敌人造成一次攻击伤害：敌人有闪避时躲开，否则先扣护甲；敌人有荆棘时玩家受到反伤。
func (r *Run) playerAttack(i int, base int) {
	c := r.Combat
	e := c.Enemies[i]
	if e.St[StDodge] > 0 {
		e.St[StDodge]--
		r.emit("dodge", i, 0, "")
		return
	}
	damage := AttackDamage(base, c.St, e.St)
	c.MaxHit = max(c.MaxHit, damage)
	blocked := min(damage, e.Block)
	e.Block -= blocked
	r.dealt(damage - blocked)
	e.HP -= damage - blocked
	r.emit("hit", i, damage-blocked, blockedTag(blocked))
	if thorns := e.St[StThorns]; thorns > 0 {
		r.damagePlayer(thorns, StThorns)
		if r.Phase != PhaseCombat {
			return
		}
	}
	if e.HP <= 0 {
		r.killEnemy(i)
	}
}

func blockedTag(blocked int) string {
	if blocked > 0 {
		return "blocked"
	}
	return ""
}

func (r *Run) killEnemy(i int) {
	e := r.Combat.Enemies[i]
	if e.Dead {
		return
	}
	e.HP, e.Dead, e.Block = 0, true, 0
	r.Kills++
	r.emit("die", i, 0, "")
	if r.hasRelic(RelicFang) {
		r.heal(4)
	}
	if len(r.aliveEnemies()) == 0 {
		r.winCombat()
	}
}

// damagePlayer 玩家受到一次可以被护甲抵挡的伤害(敌人的攻击、荆棘)。
func (r *Run) damagePlayer(damage int, source string) int {
	c := r.Combat
	blocked := min(damage, c.Block)
	c.Block -= blocked
	r.loseHP(damage-blocked, source)
	return damage - blocked
}

// loseHP 玩家失去生命(无视护甲)。生命归零时有不死鸟羽毛就复活，否则这一轮远征失败。
func (r *Run) loseHP(n int, source string) {
	if n < 0 {
		n = 0
	}
	r.HP -= n
	r.emit("hit", playerTarget, n, source)
	if r.HP > 0 {
		return
	}
	if r.Practice != nil && r.Practice.Invincible {
		r.HP = 1
		return
	}
	if r.hasRelic(RelicPhoenix) && !r.PhoenixUsed {
		r.PhoenixUsed = true
		r.HP = max(r.MaxHP*30/100, 1)
		r.emit("revive", playerTarget, r.HP, RelicPhoenix)
		return
	}
	r.HP = 0
	r.emit("defeat", playerTarget, 0, "")
	if r.Mode == ModePractice {
		// 练习留着战斗，页面还要显示这一场的伤害统计。
		r.Phase, r.Result = PhaseDone, PhaseLost
		return
	}
	r.Phase = PhaseLost
	r.Combat = nil
}

// dealt 记下敌人实际失去的生命。
func (r *Run) dealt(n int) {
	r.Combat.Dealt += n
	r.Combat.TurnDealt += n
}

func (r *Run) infiniteEnergy() bool { return r.Practice != nil && r.Practice.InfiniteEnergy }

// endTurn 结束玩家回合：回合结束效果、弃掉手牌，然后敌人依次行动，再开始玩家的下一回合。
func (r *Run) endTurn() {
	c := r.Combat
	if c.St[StPlate] > 0 {
		r.gainBlock(c.St[StPlate])
	}
	if c.St[StEmbers] > 0 {
		r.applyAllEnemies(StBurn, c.St[StEmbers])
	}
	if c.St[StRage] > 0 {
		c.St[StStrength] += c.St[StRage]
	}
	if r.hasRelic(RelicRune) && c.Block == 0 {
		r.gainBlock(6)
	}
	c.Discard = append(c.Discard, c.Hand...)
	c.Hand = []Card{}
	decay(c.St)
	for i, e := range c.Enemies {
		if e.Dead {
			continue
		}
		r.enemyTurn(i)
		if r.Phase != PhaseCombat {
			return
		}
	}
	for _, e := range c.Enemies {
		if !e.Dead {
			r.chooseMove(e)
		}
	}
	r.startTurn()
}

// decay 是回合结束时虚弱与易伤各减一层。
func decay(st map[string]int) {
	for _, status := range []string{StWeak, StVuln} {
		if st[status] > 0 {
			st[status]--
		}
		if st[status] == 0 {
			delete(st, status)
		}
	}
}

// enemyTurn 是一个敌人的回合：清护甲、结算灼烧、出招、回合结束的铸甲与状态衰减。
func (r *Run) enemyTurn(i int) {
	c := r.Combat
	e := c.Enemies[i]
	e.Block = 0
	if burn := e.St[StBurn]; burn > 0 {
		e.HP -= burn
		r.dealt(burn)
		r.emit("hit", i, burn, StBurn)
		e.St[StBurn]--
		if e.St[StBurn] == 0 {
			delete(e.St, StBurn)
		}
		if e.HP <= 0 {
			r.killEnemy(i)
			return
		}
	}
	move := e.def().Moves[e.Move]
	e.Last = append(e.Last, e.Move)
	if len(e.Last) > 2 {
		e.Last = e.Last[len(e.Last)-2:]
	}
	r.emit("act", i, e.Move, move.Id)
	if move.Dmg > 0 {
		base := move.Dmg * (100 + e.DmgPct) / 100
		for range max(move.Times, 1) {
			if c.St[StDodge] > 0 {
				c.St[StDodge]--
				r.emit("dodge", playerTarget, 0, "")
				continue
			}
			dealt := r.damagePlayer(AttackDamage(base, e.St, c.St), "")
			if r.Phase != PhaseCombat {
				return
			}
			if move.Lifesteal && dealt > 0 {
				healed := min(dealt, e.MaxHP-e.HP)
				e.HP += healed
				if healed > 0 {
					r.emit("heal", i, healed, "")
				}
			}
			if thorns := c.St[StThorns]; thorns > 0 {
				blocked := min(thorns, e.Block)
				e.Block -= blocked
				r.dealt(thorns - blocked)
				e.HP -= thorns - blocked
				r.emit("hit", i, thorns-blocked, StThorns)
				if e.HP <= 0 {
					r.killEnemy(i)
					return
				}
			}
		}
	}
	if move.Block > 0 {
		e.Block += move.Block
		r.emit("block", i, move.Block, "")
	}
	if move.Heal > 0 {
		healed := min(move.Heal, e.MaxHP-e.HP)
		e.HP += healed
		r.emit("heal", i, healed, "")
	}
	for _, status := range sortedKeys(move.Self) {
		e.St[status] += move.Self[status]
		r.emit("apply", i, move.Self[status], status)
	}
	for _, status := range sortedKeys(move.Foe) {
		c.St[status] += move.Foe[status]
		r.emit("apply", playerTarget, move.Foe[status], status)
	}
	for range move.Wounds {
		c.Discard = append(c.Discard, Card{Id: WoundCard})
	}
	if move.Wounds > 0 {
		r.emit("wound", playerTarget, move.Wounds, "")
	}
	if e.St[StPlate] > 0 {
		e.Block += e.St[StPlate]
	}
	decay(e.St)
}

// usePotion 喝掉药水栏第 slot 瓶。治疗药水战斗外也能喝，其余只能在战斗中用。
func (r *Run) usePotion(slot, target int) error {
	if slot < 0 || slot >= len(r.Potions) {
		return ErrAction
	}
	potion := r.Potions[slot]
	inCombat := r.Phase == PhaseCombat
	if !inCombat && potion != PotionHeal {
		return ErrAction
	}
	if potion == PotionFire && !r.aliveEnemy(target) {
		return ErrAction
	}
	r.Potions = slices.Delete(r.Potions, slot, slot+1)
	r.emit("potion", target, slot, potion)
	switch potion {
	case PotionHeal:
		r.heal(r.MaxHP / 4)
	case PotionFire:
		e := r.Combat.Enemies[target]
		blocked := min(20, e.Block)
		e.Block -= blocked
		r.dealt(20 - blocked)
		e.HP -= 20 - blocked
		r.emit("hit", target, 20-blocked, blockedTag(blocked))
		if e.HP <= 0 {
			r.killEnemy(target)
		}
	case PotionIron:
		r.gainBlock(12)
	case PotionStrength:
		r.Combat.St[StStrength] += 2
		r.emit("apply", playerTarget, 2, StStrength)
	case PotionSwift:
		r.draw(3)
	case PotionEnergy:
		r.Combat.Energy += 2
		r.emit("energy", playerTarget, 2, "")
	case PotionWeakness:
		r.applyAllEnemies(StWeak, 3)
	}
	return nil
}

// IntentDamage 是敌人这一招每次攻击对玩家造成的伤害(计入力量、虚弱、易伤与深渊加成，未计护甲)。
func (r *Run) IntentDamage(e *Enemy) int {
	move := e.def().Moves[e.Move]
	if move.Dmg == 0 {
		return 0
	}
	return AttackDamage(move.Dmg*(100+e.DmgPct)/100, e.St, r.Combat.St)
}
