package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loseHeroBattle 把主角压到 1 点生命，一直结束回合到输掉，返回输掉那一下的事件。
func loseHeroBattle(t *testing.T, save *Save, day string) []Ev {
	t.Helper()
	save.Hero.Battle.Run.HP = 1
	for range 10 {
		combat := save.Hero.Battle.Run.Combat
		combat.Block = 0
		delete(combat.St, dungeon.StDodge)
		evs, err := save.HeroApply(HeroAction{Type: "battle", Move: "end", Card: -1, Target: -1, Choice: -1, Potion: -1}, 0, day, 7)
		require.NoError(t, err)
		if save.Hero.Battle == nil {
			return evs
		}
	}
	require.FailNow(t, "the hero should have fallen within ten turns")
	return nil
}

// reviewOf 取出复盘：输法与变强的路(按顺序)。
func reviewOf(evs []Ev) (string, []Ev) {
	cause, tips := "", []Ev{}
	for _, ev := range evs {
		switch ev.K {
		case "hero_review":
			cause = ev.S
		case "hero_tip":
			tips = append(tips, ev)
		}
	}
	return cause, tips
}

func tip(key string, n int) Ev { return Ev{K: "hero_tip", S: key, N: n} }

// 输掉一场跟着输法与最多三条变强的路；新主角一回合就倒下，算被打得太快，能做的是扩建灵泉、闭关升层与带上随行弟子。
// 退走不复盘。
func TestDefeatIsReviewed(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	evs := loseHeroBattle(t, save, "d1")
	assert.True(t, hasEv(evs, "hero_lost"))
	assert.Equal(t, 1, save.Hero.Stats.Losses)
	cause, tips := reviewOf(evs)
	assert.Equal(t, CauseBurst, cause)
	assert.Equal(t, []Ev{tip("spring", springHP), tip("cultivate", heroLayerHP), tip("team", 2)}, tips)

	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	evs = heroAct(t, save, HeroAction{Type: "flee"})
	assert.False(t, hasEv(evs, "hero_review"), "fleeing is not a defeat to review")
}

// 输法：敌人剩下不到四分之一是差一点(哪怕自己被一下打穿)；否则最后一回合受到的伤害达到最大生命一半或撑不过两回合是被打得
// 太快；其余是输出跟不上。打在敌人身上的伤害不算主角受到的。
func TestDefeatCause(t *testing.T) {
	cases := []struct {
		name        string
		enemyHP     int // 敌人剩下的生命占最大生命的百分比
		turn        int
		lostPercent int // 最后一回合受到的伤害占最大生命的百分比
		want        string
	}{
		{"nearly won", 20, 6, 10, CauseClose},
		{"nearly won but crushed", 5, 2, 100, CauseClose},
		{"one heavy turn", 100, 5, 50, CauseBurst},
		{"fell in two turns", 80, 2, 10, CauseBurst},
		{"worn down", 60, 6, 40, CauseSlow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			save := newHeroSave(t, dungeon.ClassWarrior)
			h := save.Hero
			heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
			heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
			run := h.Battle.Run
			for _, e := range run.Combat.Enemies {
				e.HP = e.MaxHP * tc.enemyHP / 100
			}
			run.Combat.Turn = tc.turn
			run.Events = []dungeon.Ev{
				{Kind: "hit", T: 0, N: 999},
				{Kind: "hit", T: -1, N: run.MaxHP * tc.lostPercent / 100},
			}
			cause, _ := reviewOf(h.reviewDefeat(save.Sect, h.Battle, 0))
			assert.Equal(t, tc.want, cause)
		})
	}
}

// 变强的路按输法排先后，只列现在做得到的：瓶颈了才提突破，丹室够级或有存货才提丹药，药力还在的不提，器室等级没到顶的
// 法宝才提祭炼，空着法宝栏且有法宝才提佩戴。
func TestDefeatTipsFollowTheHero(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)
	require.True(t, h.Bottleneck())
	h.Cave = map[string]int{RoomFurnace: 2, RoomForge: 1, RoomSpring: RoomCap(0)}
	h.Relics = []string{dungeon.ClassRelic(dungeon.ClassWarrior), dungeon.RelicIronPlate}
	h.Worn = []string{dungeon.RelicIronPlate}
	for i := range h.Cards {
		h.Cards[i].Up = i >= 2
	}
	for _, d := range s.Disciples {
		d.Mission = 1
	}
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	battle := h.Battle

	_, tips := reviewOf(h.reviewDefeat(s, battle, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip(ElixirGuard, 8), tip("refine_"+RefineGuard, RefineBlock)}, tips,
		"burst: break through, then guard")

	h.Buffs = map[string]*Buff{ElixirGuard: {Power: 8, Left: 2}}
	battle.Run.Combat.Turn = 6
	_, tips = reviewOf(h.reviewDefeat(s, battle, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip("upgrade", 2), tip("wear", 1)}, tips,
		"slow: no tiger elixir below furnace 4, no edge relic worn")

	h.Elixirs = map[string][Grades]int{ElixirTiger: {0, 1, 0}}
	_, tips = reviewOf(h.reviewDefeat(s, battle, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip(ElixirTiger, 1), tip("upgrade", 2)}, tips,
		"a tiger elixir in stock counts even without the furnace level")
}

// 差一点时先提下一场马上能带上的：丹符(行囊空了、丹房能炼)、灵兽、随行弟子；空明天象本来就不让带，换成别的路。
func TestDefeatTipsRespectTheBareSign(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	clearChapters(h, 1)
	s.Buildings[BuildingAlchemy] = 1
	h.Beasts = []*Beast{{Id: 1, Kind: BeastTiger, Aptitude: 1, Level: 1}}
	for i := range h.Cards {
		h.Cards[i].Up = true
	}

	trialAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"}, weekMonday)
	for _, e := range h.Battle.Run.Combat.Enemies {
		e.HP = 1
	}
	cause, tips := reviewOf(loseHeroBattle(t, save, weekMonday))
	assert.Equal(t, CauseClose, cause)
	assert.Equal(t, []Ev{tip("potions", 0), tip("beast", 0), tip("team", 2)}, tips)

	require.Equal(t, SignBare, h.Weekly.Trials[0].Sign)
	trialAct(t, save, HeroAction{Type: "trial", Index: 1}, weekMonday)
	for _, e := range h.Battle.Run.Combat.Enemies {
		e.HP = 1
	}
	cause, tips = reviewOf(loseHeroBattle(t, save, weekMonday))
	assert.Equal(t, CauseClose, cause)
	assert.Equal(t, []Ev{tip("cultivate", heroLayerHP), tip("spring", springHP)}, tips)
}
