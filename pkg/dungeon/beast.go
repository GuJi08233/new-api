package dungeon

import "slices"

// 灵兽：问道历练里跟着主角出战的灵兽。每个玩家回合结束时(敌人行动之前)自己出手一次，招式由 Move 决定，力道是 Power。
// 灵兽不吃主角的力量与虚弱，也不会被敌人打；敌人的护甲、身法与易伤照常算。只有单场战斗(ModeBattle)带灵兽。

// 灵兽的招式。
const (
	BeastStrike = "strike" // 扑击一个随机敌人，Power 点伤害
	BeastHeal   = "heal"   // 给主角回复 Power/2 点生命(至少 1 点)
	BeastGuard  = "guard"  // 给主角 Power 点护甲
	BeastBurn   = "burn"   // 给所有敌人 Power/4 层灼烧(至少 1 层)
	BeastMaul   = "maul"   // 重击一个随机敌人 Power/2 点伤害(至少 1 点)，再给它 1 层虚弱
	BeastSweep  = "sweep"  // 对所有敌人各 Power/2 点伤害(至少 1 点)
	// BattleMaxBeastPower 是灵兽力道的上限。
	BattleMaxBeastPower = 99
)

// BeastMoves 是全部招式。
var BeastMoves = []string{BeastStrike, BeastHeal, BeastGuard, BeastBurn, BeastMaul, BeastSweep}

// BattleBeast 是一场战斗里出战的灵兽：Kind 是种类(页面显示用)，Move 是招式，Power 是力道。
type BattleBeast struct {
	Kind  string `json:"kind"`
	Move  string `json:"move"`
	Power int    `json:"power"`
}

func (b *BattleBeast) valid() bool {
	return slices.Contains(BeastMoves, b.Move) && b.Power >= 1 && b.Power <= BattleMaxBeastPower
}

// beastTurn 让出战的灵兽出手一次。打倒最后一个敌人时战斗就结束了，调用方要看 Phase。
func (r *Run) beastTurn(b *BattleBeast) {
	half := max(b.Power/2, 1)
	r.emit("beast", playerTarget, b.Power, b.Kind)
	switch b.Move {
	case BeastStrike, BeastMaul:
		alive := r.aliveEnemies()
		if len(alive) == 0 {
			return
		}
		target := alive[r.Rng.Intn(len(alive))]
		if b.Move == BeastStrike {
			r.beastStrike(target, b.Power)
			return
		}
		r.beastStrike(target, half)
		r.applyEnemy(target, StWeak, 1)
	case BeastSweep:
		for _, i := range r.aliveEnemies() {
			r.beastStrike(i, half)
		}
	case BeastHeal:
		r.heal(half)
	case BeastGuard:
		r.gainBlock(b.Power)
	case BeastBurn:
		r.applyAllEnemies(StBurn, max(b.Power/4, 1))
	}
}

// beastStrike 灵兽打一个敌人 n 点：易伤多一半，身法躲得开，护甲先挡。
func (r *Run) beastStrike(i, n int) {
	if !r.aliveEnemy(i) {
		return
	}
	e := r.Combat.Enemies[i]
	if e.St[StDodge] > 0 {
		e.St[StDodge]--
		r.emit("dodge", i, 0, "")
		return
	}
	if e.St[StVuln] > 0 {
		n = n * 3 / 2
	}
	blocked := min(n, e.Block)
	e.Block -= blocked
	r.dealt(n - blocked)
	e.HP -= n - blocked
	r.emit("hit", i, n-blocked, blockedTag(blocked))
	if e.HP <= 0 {
		r.killEnemy(i)
	}
}
