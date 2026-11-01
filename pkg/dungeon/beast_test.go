package dungeon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func beastBattle(t *testing.T, beast *BattleBeast) *Run {
	t.Helper()
	r, err := NewBattle(5, BattleSetup{Class: ClassWarrior, Deck: []Card{{Id: "w_defend"}, {Id: "w_defend"}}, MaxHP: 80,
		Kind: NodeCombat, Enemies: []string{"goblin", "goblin"}, HP: 300, Dmg: 50, Beast: beast})
	require.NoError(t, err)
	return r
}

func endTurn(t *testing.T, r *Run) {
	t.Helper()
	require.NoError(t, r.Apply(Action{Type: "end", Node: -1, Card: -1, Target: -1, Choice: -1, Potion: -1}))
}

// 灵兽在玩家回合结束、敌人行动之前出手：每种招式按力道生效，不吃主角的力量。
func TestBeastMoves(t *testing.T) {
	r := beastBattle(t, &BattleBeast{Kind: "baihu", Move: BeastStrike, Power: 10})
	r.Combat.St[StStrength] = 5
	before := r.Combat.Enemies[0].HP + r.Combat.Enemies[1].HP
	endTurn(t, r)
	assert.Equal(t, before-10, r.Combat.Enemies[0].HP+r.Combat.Enemies[1].HP, "strike hits one enemy for exactly its power")

	r = beastBattle(t, &BattleBeast{Kind: "hu", Move: BeastSweep, Power: 9})
	hp0, hp1 := r.Combat.Enemies[0].HP, r.Combat.Enemies[1].HP
	endTurn(t, r)
	assert.Equal(t, hp0-4, r.Combat.Enemies[0].HP)
	assert.Equal(t, hp1-4, r.Combat.Enemies[1].HP)

	r = beastBattle(t, &BattleBeast{Kind: "que", Move: BeastBurn, Power: 8})
	events := len(r.Events)
	endTurn(t, r)
	assert.Contains(t, r.Events[events:], Ev{Kind: "apply", T: 0, N: 2, S: StBurn})
	assert.Contains(t, r.Events[events:], Ev{Kind: "apply", T: 1, N: 2, S: StBurn})

	r = beastBattle(t, &BattleBeast{Kind: "yuan", Move: BeastMaul, Power: 6})
	events = len(r.Events)
	endTurn(t, r)
	weakened := 0
	for _, ev := range r.Events[events:] {
		if ev.Kind == "apply" && ev.S == StWeak && ev.T >= 0 {
			weakened++
		}
	}
	assert.Equal(t, 1, weakened, "maul weakens one enemy before it acts")

	r = beastBattle(t, &BattleBeast{Kind: "lu", Move: BeastHeal, Power: 10})
	r.HP = 50
	events = len(r.Events)
	endTurn(t, r)
	assert.Contains(t, r.Events[events:], Ev{Kind: "heal", T: playerTarget, N: 5})
}

// 灵兽的一击打倒最后一个敌人就直接赢了这一场，敌人不再行动。
func TestBeastCanFinishTheBattle(t *testing.T) {
	r := beastBattle(t, &BattleBeast{Kind: "hu", Move: BeastSweep, Power: 20})
	for _, e := range r.Combat.Enemies {
		e.HP = 3
	}
	hp := r.HP
	endTurn(t, r)
	assert.Equal(t, PhaseWon, r.Result)
	assert.Equal(t, hp, r.HP)
}

// 灵兽的招式与力道要合法。
func TestBattleBeastValidation(t *testing.T) {
	for _, beast := range []*BattleBeast{{Move: "dance", Power: 5}, {Move: BeastStrike, Power: 0},
		{Move: BeastStrike, Power: BattleMaxBeastPower + 1}} {
		_, err := NewBattle(5, BattleSetup{Class: ClassWarrior, Deck: []Card{{Id: "w_defend"}}, MaxHP: 80, Kind: NodeCombat,
			Enemies: []string{"goblin"}, HP: 100, Dmg: 100, Beast: beast})
		assert.ErrorIs(t, err, ErrAction)
	}
}
