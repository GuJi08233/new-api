package wendao

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-11-02 是周一(第 45 周)，11-08 是同一周的周日，11-09 起是第 46 周。
const (
	weekMonday = "2026-11-02"
	weekSunday = "2026-11-08"
	nextMonday = "2026-11-09"
	thirdWeek  = "2026-11-16"
)

func trialAct(t *testing.T, save *Save, a HeroAction, day string) []Ev {
	t.Helper()
	evs, err := save.HeroApply(a, 0, day, 7)
	require.NoError(t, err)
	return evs
}

// winTrial 把这一场的敌人都压到 1 点生命打倒，跳过功法奖励。
func winTrial(t *testing.T, save *Save, day string) []Ev {
	t.Helper()
	run := save.Hero.Battle.Run
	attack := dungeon.StarterDeck(save.Hero.Class)[0]
	for i, e := range run.Combat.Enemies {
		e.HP, e.Block = 1, 0
		delete(e.St, dungeon.StDodge)
		run.Combat.Hand = []dungeon.Card{attack}
		run.Combat.Energy = 3
		trialAct(t, save, HeroAction{Type: "battle", Move: "play", Card: 0, Target: i, Choice: -1, Potion: -1}, day)
	}
	require.Equal(t, dungeon.PhaseReward, run.Phase)
	return trialAct(t, save, HeroAction{Type: "battle", Move: "reward", Card: -1, Target: -1, Choice: -1, Potion: -1}, day)
}

// 周按服务器本地日期的 ISO 周算，周一换周；不是日期时没有周。
func TestWeekOf(t *testing.T) {
	week, index := WeekOf(weekMonday)
	assert.Equal(t, "2026-W45", week)
	sunday, same := WeekOf(weekSunday)
	assert.Equal(t, week, sunday)
	assert.Equal(t, index, same)
	next, after := WeekOf(nextMonday)
	assert.Equal(t, "2026-W46", next)
	assert.Equal(t, index+1, after)
	none, _ := WeekOf("d1")
	assert.Empty(t, none)

	nextWeek := time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local).Unix()
	assert.Equal(t, nextWeek, WeekEnd(weekMonday))
	assert.Equal(t, nextWeek, WeekEnd(weekSunday))
	assert.Equal(t, time.Date(2026, 11, 16, 0, 0, 0, 0, time.Local).Unix(), WeekEnd(nextMonday))
}

// 打败第一章首领以后开放；三关是这一章的遭遇、强敌与首领，各带一种不同的天象，敌人强度与奖励照天象和节点算。
func TestWeeklyTrialsOpenAfterTheFirstBoss(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	assert.Nil(t, h.View(save.Sect, 0, weekMonday).Weekly)
	_, err := save.HeroApply(HeroAction{Type: "trial", Index: 1}, 0, weekMonday, 1)
	assert.ErrorIs(t, err, ErrUnavailable)

	clearChapters(h, 1)
	trialAct(t, save, HeroAction{Type: "sync"}, weekMonday)
	w := h.Weekly
	require.NotNil(t, w)
	assert.Equal(t, "2026-W45", w.Week)
	assert.Equal(t, 0, w.Chapter)
	assert.Equal(t, []bool{false, false, false}, w.Cleared)
	assert.Equal(t, WeeklyTrial{Kind: NodeCombat, Node: "c1_backhill", Enemies: []string{"slime"}, HP: 135, Dmg: 117,
		Sign: SignBare, Reward: Cost{Stone: 60}}, w.Trials[0])
	assert.Equal(t, WeeklyTrial{Kind: NodeElite, Node: "c1_chief", Enemies: []string{"goblin_chief"}, HP: 280, Dmg: 128,
		Sign: SignIron, Reward: Cost{Stone: 120, Ore: 30, Rep: 5}, Pill: PillQi}, w.Trials[1])
	assert.Equal(t, WeeklyTrial{Kind: NodeBoss, Node: "c1_boss", Enemies: []string{"slime_king", "slime"}, HP: 91, Dmg: 97,
		Sign: SignHorde, Reward: Cost{Stone: 180, Herb: 90, Ore: 48, Rep: 12}, Pill: PillBreak}, w.Trials[2])

	view := h.View(save.Sect, 0, weekMonday).Weekly
	require.NotNil(t, view)
	assert.Equal(t, WeekEnd(weekMonday), view.Ends)

	clearChapters(h, 3)
	trialAct(t, save, HeroAction{Type: "sync"}, weekSunday)
	assert.Equal(t, 0, h.Weekly.Chapter, "the week keeps the trials it started with")
	next := h.View(save.Sect, 0, nextMonday).Weekly
	assert.Equal(t, 2, next.Chapter, "next week draws from the latest chapter whose boss is beaten")
	assert.Equal(t, "2026-W45", h.Weekly.Week, "viewing does not roll the save")
}

// 每关第一次打赢给奖励，过了的关不能再打；三关全过另给洗髓丹与一份灵药，记一周全过。输了可以再来。
func TestWeeklyTrialRewardsOnce(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassMage)
	s, h := save.Sect, save.Hero
	clearChapters(h, 1)
	trialAct(t, save, HeroAction{Type: "sync"}, weekMonday)

	trialAct(t, save, HeroAction{Type: "trial", Index: 2}, weekMonday)
	require.NotNil(t, h.Battle)
	assert.Equal(t, 2, h.Battle.Trial)
	assert.Equal(t, "2026-W45", h.Battle.Week)
	trialAct(t, save, HeroAction{Type: "flee"}, weekMonday)
	assert.Equal(t, []bool{false, false, false}, h.Weekly.Cleared, "a lost trial can be tried again")

	stone, qi := s.Res.Stone, s.Pills[PillQi]
	trialAct(t, save, HeroAction{Type: "trial", Index: 2}, weekMonday)
	evs := winTrial(t, save, weekMonday)
	assert.True(t, hasEv(evs, "hero_trial"))
	assert.Equal(t, []bool{false, true, false}, h.Weekly.Cleared)
	assert.Equal(t, stone+120, s.Res.Stone)
	assert.Equal(t, qi+1, s.Pills[PillQi])
	_, err := save.HeroApply(HeroAction{Type: "trial", Index: 2}, 0, weekMonday, 3)
	assert.ErrorIs(t, err, ErrUnavailable, "a cleared trial is closed for the week")
	_, err = save.HeroApply(HeroAction{Type: "trial", Index: 4}, 0, weekMonday, 3)
	assert.ErrorIs(t, err, ErrAction)

	marrow := s.Pills[PillMarrow]
	trialAct(t, save, HeroAction{Type: "trial", Index: 1}, weekMonday)
	assert.False(t, hasEv(winTrial(t, save, weekMonday), "hero_trial_week"))
	trialAct(t, save, HeroAction{Type: "trial", Index: 3}, weekMonday)
	evs = winTrial(t, save, weekMonday)
	assert.True(t, hasEv(evs, "hero_trial_week"))
	assert.True(t, hasEv(evs, "rare"))
	assert.Equal(t, marrow+1, s.Pills[PillMarrow])
	assert.Equal(t, 1, h.Weekly.Weeks)
	assert.Equal(t, 0, h.Weekly.left())
}

// 有一场试炼没打完时不换周：周日开战、周一打赢，记在开战那一周；之后的操作才换到新的一周，全过的周数留着。连着换两周也一样。
func TestWeeklyTrialAcrossTheWeekBoundary(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassRanger)
	h := save.Hero
	clearChapters(h, 1)
	trialAct(t, save, HeroAction{Type: "sync"}, weekMonday)
	h.Weekly.Cleared = []bool{true, true, false}

	trialAct(t, save, HeroAction{Type: "trial", Index: 3}, weekSunday)
	trialAct(t, save, HeroAction{Type: "sync"}, nextMonday)
	assert.Equal(t, "2026-W45", h.Weekly.Week, "no rollover while the trial is being fought")
	assert.Equal(t, "2026-W45", h.View(save.Sect, 0, nextMonday).Weekly.Week)
	evs := winTrial(t, save, nextMonday)
	assert.True(t, hasEv(evs, "hero_trial_week"), "the win counts for the week it started in")
	assert.Equal(t, 1, h.Weekly.Weeks)

	trialAct(t, save, HeroAction{Type: "sync"}, nextMonday)
	assert.Equal(t, "2026-W46", h.Weekly.Week)
	assert.Equal(t, []bool{false, false, false}, h.Weekly.Cleared)
	assert.Equal(t, 1, h.Weekly.Weeks, "the count of fully cleared weeks carries over")
	assert.Equal(t, SignBlessed, h.Weekly.Trials[0].Sign, "the signs rotate every week")

	trialAct(t, save, HeroAction{Type: "sync"}, thirdWeek)
	assert.Equal(t, "2026-W47", h.Weekly.Week)
	assert.Equal(t, 1, h.Weekly.Weeks)
	_, err := save.HeroApply(HeroAction{Type: "trial", Index: 1}, 0, "d1", 3)
	assert.NoError(t, err, "a day that is not a date keeps the stored week")
}

// 空明不带丹符、随行弟子与灵兽；天助让主角开战多些力量与护体。
func TestWeeklySignsChangeTheHeroSide(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	clearChapters(h, 1)
	h.Potions[dungeon.PotionFire] = 1
	week, index := WeekOf(weekMonday)
	h.Weekly = newWeekly(week, index, 0)
	require.Equal(t, SignBare, h.Weekly.Trials[0].Sign)

	_, err := save.HeroApply(HeroAction{Type: "trial", Index: 1, Team: []int{s.Disciples[0].Id}}, 0, weekMonday, 3)
	assert.ErrorIs(t, err, ErrAction, "no companions under the bare sign")
	trialAct(t, save, HeroAction{Type: "trial", Index: 1}, weekMonday)
	assert.Empty(t, h.Battle.Carried, "potions stay in the bag")
	assert.Equal(t, 1, h.Potions[dungeon.PotionFire])
	assert.Nil(t, h.Battle.Run.Battle.Beast)
	trialAct(t, save, HeroAction{Type: "flee"}, weekMonday)

	h.Weekly = newWeekly(week, index+1, 0)
	require.Equal(t, SignBlessed, h.Weekly.Trials[0].Sign)
	trialAct(t, save, HeroAction{Type: "trial", Index: 1}, weekMonday)
	assert.Equal(t, h.Strength()+2, h.Battle.Run.Battle.Strength)
	assert.Equal(t, h.Block()+12, h.Battle.Run.Battle.Block)
	assert.Equal(t, []string{dungeon.PotionFire}, h.Battle.Carried, "other signs carry potions as usual")
}
