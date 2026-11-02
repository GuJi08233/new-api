package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func todoKeys(todos []Todo) []string {
	keys := make([]string, 0, len(todos))
	for _, todo := range todos {
		keys = append(keys, todo.Key)
	}
	return keys
}

// clearChapters 把前 chapters 章的节点都记成走过。
func clearChapters(h *Hero, chapters int) {
	for i := range JourneyNodes {
		if n := &JourneyNodes[i]; n.Chapter < chapters {
			h.Nodes[n.Id] = &NodeRecord{Clears: 1}
		}
	}
}

// 主目标沿着主线走：先走下一个没走过的节点，本章走完了修到圆满、带机缘突破，五章走完登问心阶，登顶为止。
func TestAgendaGoalFollowsTheJourney(t *testing.T) {
	empty, err := ParseSave("")
	require.NoError(t, err)
	assert.Equal(t, Goal{Key: "create", Part: PartJourney}, empty.Agenda(0, "d1").Goal)

	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	assert.Equal(t, Goal{Key: "node", Part: PartJourney, Tab: "map", S: "c1_oath"}, save.Agenda(0, "d1").Goal)
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	assert.Equal(t, "c1_backhill", save.Agenda(0, "d1").Goal.S)

	clearChapters(h, 1)
	h.Layer, h.Exp = 4, 0
	assert.Equal(t, Goal{Key: "cultivate", Part: PartJourney, N: 0, M: 4}, save.Agenda(0, "d1").Goal)
	h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)
	assert.Equal(t, Goal{Key: "break", Part: PartJourney, S: BreakKeys[0], N: 1}, save.Agenda(0, "d1").Goal)

	h.Realm, h.Layer, h.Exp = 1, 1, 0
	first := save.Agenda(0, "d1").Goal
	assert.Equal(t, "node", first.Key)
	assert.Equal(t, 1, first.N, "the next chapter opens after the breakthrough")

	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	assert.Equal(t, Goal{Key: "battle", Part: PartJourney, S: "c1_backhill"}, save.Agenda(0, "d1").Goal)
	heroAct(t, save, HeroAction{Type: "flee"})

	h.Realm = MaxRealm
	clearChapters(h, Chapters)
	h.Steps = 3
	assert.Equal(t, Goal{Key: "steps", Part: PartJourney, Tab: "steps", N: 4}, save.Agenda(0, "d1").Goal)
	h.Steps = MaxSteps
	assert.Equal(t, Goal{Key: "summit", Part: PartJourney, Tab: "steps"}, save.Agenda(0, "d1").Goal)
}

// 待办按急迫程度排：奇遇、出炉、回山的派遣、熟了的灵田在前，空着的丹炉与田地在后；件数照实算，做不了的(没钱、冷却、
// 正在打仗)不提。
func TestAgendaTodosListWhatCanBeDoneNow(t *testing.T) {
	const now = int64(100000)
	save := newHeroSave(t, dungeon.ClassMage)
	s, h := save.Sect, save.Hero
	s.Res = Cost{Stone: 5000, Herb: 5000, Ore: 5000}
	s.RollDaily("d1", "d0", true)
	assert.Equal(t, []string{"garden_empty"}, todoKeys(save.Agenda(now, "d1").Todos), "a fresh sect only has empty plots")

	s.Event = &Event{Key: "vein", At: now}
	s.Missions = []*Mission{{Id: 1, Key: "patrol", End: now - 1}, {Id: 2, Key: "patrol", End: now + 60}}
	s.Garden.Plots[0] = &Plot{Crop: "qingcao", Planted: now - 7200, Ripe: now - 1}
	s.Garden.Plots[1] = &Plot{Crop: "qingcao", Planted: now, Ripe: now + 3600}
	s.Daily.Tasks[0].Have = s.Daily.Tasks[0].Need
	s.Disciples[0].Layer, s.Disciples[0].Exp = MaxLayer, ExpNeed(0, MaxLayer)
	s.Pills[PillQi] = 1
	h.Cave = map[string]int{RoomFurnace: 5, RoomDen: 1}
	h.Furnaces = []*Brew{
		{Recipe: ElixirTemper, Fire: "mid", Start: now - 3600, End: now - 1},
		{Recipe: ElixirTemper, Fire: "mid", Start: now - 1800, End: now + 1800},
	}
	h.Beasts = []*Beast{{Id: 1, Kind: "lu", Level: 1, Bond: 10}}
	h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)
	h.Keys[BreakKeys[0]] = 1

	agenda := save.Agenda(now, "d1")
	assert.Equal(t, []string{"event", "furnace_done", "missions", "garden", "hero_break", "daily", "furnace_stoke",
		"disciple_break", "beast_play"}, todoKeys(agenda.Todos))
	assert.Equal(t, Todo{Key: "furnace_done", Part: PartJourney, Tab: "cave", View: "furnace", N: 1}, agenda.Todos[1])
	assert.Equal(t, 1, agenda.Todos[2].N, "only the team that is back counts")
	assert.Equal(t, 1, agenda.Todos[3].N, "only the ripe plot counts")
	assert.NotContains(t, todoKeys(agenda.Todos), "qi", "no pill at the bottleneck")
	assert.NotContains(t, todoKeys(agenda.Todos), "lure", "the den is full")

	h.Layer, h.Exp = 5, 0
	h.Furnaces = h.Furnaces[1:]
	h.Furnaces[0].Stoked = true
	h.Beasts[0].Played = "d1"
	s.Event, s.Missions = nil, nil
	s.Garden.Plots[0] = &Plot{}
	agenda = save.Agenda(now, "d1")
	assert.Equal(t, []string{"daily", "qi", "disciple_break", "furnace_idle", "garden_empty"}, todoKeys(agenda.Todos))

	s.Res = Cost{}
	h.QiReady = now + 60
	assert.Equal(t, []string{"daily", "disciple_break"}, todoKeys(save.Agenda(now, "d1").Todos),
		"nothing to brew or plant without resources, and the pill is cooling down")

	s.Res = Cost{Stone: 5000, Herb: 5000}
	h.Furnaces = nil
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	assert.Equal(t, []string{"daily", "disciple_break", "garden_empty"}, todoKeys(save.Agenda(now, "d1").Todos),
		"the journey waits until the battle is over")
}
