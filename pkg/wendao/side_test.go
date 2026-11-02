package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearMainChapter 把第 chapter 章主线上的节点都记成走过，支线不动。
func clearMainChapter(h *Hero, chapter int) {
	for i := range JourneyNodes {
		if n := &JourneyNodes[i]; n.Side == "" && n.Chapter == chapter {
			h.Nodes[n.Id] = &NodeRecord{Clears: 1}
		}
	}
}

// 支线的抉择：剧情节点要选一个认识的抉择才能走过，选了记在通过记录里并加给那一个的奖励；剧情只走一次，奖励不会再拿。
// 没有抉择的节点不收抉择。
func TestSideStoryChoice(t *testing.T) {
	cases := []struct {
		pick    string
		rep     int
		peiyuan int
	}{
		{pick: "town", rep: 20},
		{pick: "sect", peiyuan: 2},
	}
	for _, c := range cases {
		save := newHeroSave(t, dungeon.ClassWarrior)
		h, s := save.Hero, save.Sect
		clearMainChapter(h, 0)
		heroAct(t, save, HeroAction{Type: "visit", Node: "qh_garden"})
		heroAct(t, save, HeroAction{Type: "fight", Node: "qh_miasma"})
		winHeroBattle(t, save, -1)

		for _, option := range []string{"", "river"} {
			_, err := save.HeroApply(HeroAction{Type: "visit", Node: "qh_herb", Option: option}, 0, "d1", 0)
			assert.ErrorIs(t, err, ErrAction, "option %q", option)
		}
		assert.Zero(t, h.cleared("qh_herb"), "a refused choice leaves the node untouched")

		rep, peiyuan := s.Res.Rep, s.Pills[PillPeiyuan]
		heroAct(t, save, HeroAction{Type: "visit", Node: "qh_herb", Option: c.pick})
		assert.Equal(t, c.pick, h.Nodes["qh_herb"].Pick)
		assert.Equal(t, rep+c.rep, s.Res.Rep, c.pick)
		assert.Equal(t, peiyuan+c.peiyuan, s.Pills[PillPeiyuan], c.pick)

		_, err := save.HeroApply(HeroAction{Type: "visit", Node: "qh_herb", Option: c.pick}, 0, "d1", 0)
		assert.ErrorIs(t, err, ErrUnavailable, "a story is walked once")
		assert.Equal(t, rep+c.rep, s.Res.Rep, "no second reward")
		assert.Equal(t, peiyuan+c.peiyuan, s.Pills[PillPeiyuan], "no second reward")

		heroAct(t, save, HeroAction{Type: "visit", Node: "qh_cure"})
		assert.Empty(t, h.Nodes["qh_cure"].Pick)
	}

	save := newHeroSave(t, dungeon.ClassWarrior)
	_, err := save.HeroApply(HeroAction{Type: "visit", Node: "c1_oath", Option: "town"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)
}

// 抉择存进存档，读回来还在。
func TestSideStoryPickIsSaved(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassMage)
	clearMainChapter(save.Hero, 0)
	save.Hero.Nodes["qh_garden"] = &NodeRecord{Clears: 1}
	save.Hero.Nodes["qh_miasma"] = &NodeRecord{Clears: 1}
	heroAct(t, save, HeroAction{Type: "visit", Node: "qh_herb", Option: "town"})

	data, err := save.Encode()
	require.NoError(t, err)
	loaded, err := ParseSave(data)
	require.NoError(t, err)
	assert.Equal(t, "town", loaded.Hero.Nodes["qh_herb"].Pick)
}

// 支线不是主目标：走完第一章主线以后目标是修炼突破，支线是一件待办，点了去第一个能走的支线节点。林牧的最后一节要等第四章
// 走完、断剑做过抉择才能去。
func TestSideStoriesInTheAgenda(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassRanger)
	h := save.Hero
	clearMainChapter(h, 0)
	agenda := save.Agenda(0, "d1")
	assert.Equal(t, "cultivate", agenda.Goal.Key)
	assert.Contains(t, agenda.Todos, Todo{Key: "side", Part: PartJourney, Tab: "map", S: "qh_garden", N: 1})

	h.Realm = 3
	for chapter := 1; chapter <= 3; chapter++ {
		clearMainChapter(h, chapter)
	}
	assert.Contains(t, save.Agenda(0, "d1").Todos, Todo{Key: "side", Part: PartJourney, Tab: "map", S: "qh_garden", N: 2})
	assert.False(t, h.Unlocked(JourneyNodeOf("lm_vow")), "the sword comes first")

	for _, id := range []string{"qh_garden", "qh_miasma", "qh_herb", "qh_cure", "lm_letter", "lm_cliff", "lm_tomb"} {
		h.Nodes[id] = &NodeRecord{Clears: 1}
	}
	assert.Contains(t, save.Agenda(0, "d1").Todos, Todo{Key: "side", Part: PartJourney, Tab: "map", S: "lm_sword", N: 1})
	heroAct(t, save, HeroAction{Type: "visit", Node: "lm_sword", Option: "carry"})
	assert.True(t, h.Unlocked(JourneyNodeOf("lm_vow")))
}

// 周天试炼只从主线节点里取敌人，支线的强敌不会轮进来。
func TestWeeklyTrialsSkipSideStories(t *testing.T) {
	for week := 0; week < 6; week++ {
		for _, trial := range newWeekly("2026-W45", week, 1).Trials {
			assert.Empty(t, JourneyNodeOf(trial.Node).Side, trial.Node)
		}
	}
}
