package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loseHeroBattle 验证重放结果进入落败结算后的复盘与进度。
func loseHeroBattle(t *testing.T, save *Save, day string) []Ev {
	t.Helper()
	evs, err := save.FinishHeroFight(&doufa.Fight{Over: true, Tick: 5 * doufa.FPS, Foes: []*doufa.Foe{{HP: 100, MaxHP: 100}}}, 0)
	require.NoError(t, err)
	return evs
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

// 接近胜利必须到最后一波，前几波剩血低不能掩盖后续敌人；短时间倒下归为承伤不足。
func TestDefeatCause(t *testing.T) {
	for _, tc := range []struct {
		name            string
		hp, ticks, wave int
		want            string
	}{
		{"almost won", 20, 30 * doufa.FPS, 2, CauseClose},
		{"later wave remains", 20, 30 * doufa.FPS, 1, CauseSlow},
		{"fell quickly", 80, 5 * doufa.FPS, 1, CauseBurst},
		{"worn down", 60, 30 * doufa.FPS, 2, CauseSlow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			save := newHeroSave(t, dungeon.ClassWarrior)
			fight := &doufa.Fight{Tick: tc.ticks, Wave: tc.wave, Waves: make([][]doufa.Group, 2), Foes: []*doufa.Foe{{HP: tc.hp, MaxHP: 100}}}
			cause, _ := reviewOf(save.Hero.reviewDefeat(save.Sect, &HeroBattle{}, fight, 0))
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
	fight := &doufa.Fight{Tick: 5 * doufa.FPS, Foes: []*doufa.Foe{{HP: 100, MaxHP: 100}}}

	_, tips := reviewOf(h.reviewDefeat(s, battle, fight, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip(ElixirGuard, 8), tip("refine_"+RefineGuard, RefineBlock)}, tips,
		"burst: break through, then guard")

	h.Buffs = map[string]*Buff{ElixirGuard: {Power: 8, Left: 2}}
	fight.Tick = 30 * doufa.FPS
	_, tips = reviewOf(h.reviewDefeat(s, battle, fight, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip("upgrade", 2), tip("wear", 1)}, tips,
		"slow: no tiger elixir below furnace 4, no edge relic worn")

	h.Elixirs = map[string][Grades]int{ElixirTiger: {0, 1, 0}}
	_, tips = reviewOf(h.reviewDefeat(s, battle, fight, 0))
	assert.Equal(t, []Ev{tip("break", 1), tip(ElixirTiger, 1), tip("upgrade", 2)}, tips,
		"a tiger elixir in stock counts even without the furnace level")
}

// 空明天象禁用的丹符、随行与灵兽不会出现在变强建议中。
func TestDefeatTipsRespectTheBareSign(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	clearChapters(h, 1)
	s.Buildings[BuildingAlchemy] = 1
	h.Beasts = []*Beast{{Id: 1, Kind: BeastTiger, Aptitude: 1, Level: 1}}
	for i := range h.Cards {
		h.Cards[i].Up = true
	}
	fight := &doufa.Fight{Tick: 30 * doufa.FPS, Wave: 2, Waves: make([][]doufa.Group, 2), Foes: []*doufa.Foe{{HP: 1, MaxHP: 100}}}
	trialAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"}, weekMonday)
	cause, tips := reviewOf(h.reviewDefeat(s, h.Battle, fight, 0))
	assert.Equal(t, CauseClose, cause)
	assert.Equal(t, []Ev{tip("potions", 0), tip("beast", 0), tip("team", 2)}, tips)
	trialAct(t, save, HeroAction{Type: "flee"}, weekMonday)
	require.Equal(t, SignBare, h.Weekly.Trials[0].Sign)
	trialAct(t, save, HeroAction{Type: "trial", Index: 1}, weekMonday)
	cause, tips = reviewOf(h.reviewDefeat(s, h.Battle, fight, 0))
	assert.Equal(t, CauseClose, cause)
	assert.Equal(t, []Ev{tip("cultivate", heroLayerHP), tip("spring", springHP)}, tips)
}
