package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOutpostSave(t *testing.T) *Save {
	t.Helper()
	save := newHeroSave(t, dungeon.ClassWarrior)
	save.Sect.Buildings[BuildingHall] = 3
	save.Sect.Res = Cost{Stone: 20000, Herb: 10000, Ore: 10000, Rep: 1000}
	save.Hero.Realm = 4
	for _, node := range JourneyNodes {
		if node.Kind == NodeStory {
			save.Hero.Nodes[node.Id] = &NodeRecord{Clears: 1}
		}
	}
	for _, d := range save.Sect.Disciples {
		d.Realm, d.Layer, d.Exp, d.Talent, d.Trait = 1, 1, 0, 3, TraitSteady
	}
	return save
}

func outpostAct(t *testing.T, save *Save, action OutpostAction, now int64) []Ev {
	t.Helper()
	evs, err := save.OutpostApply(action, now)
	require.NoError(t, err)
	return evs
}

// 下院只给开宗后的掌门，必须实际到达异地且主殿达标；资源只扣一次，空院没有产出。
func TestOutpostRequiresFoundedSectAndReachedRegion(t *testing.T) {
	assert.Nil(t, (&Save{}).OutpostView(0))
	loose := looseSave(t)
	assert.Nil(t, loose.OutpostView(1000))
	_, err := loose.OutpostApply(OutpostAction{Type: "build", Region: 1}, 1000)
	assert.ErrorIs(t, err, ErrUnavailable)

	save := newOutpostSave(t)
	for _, region := range []int{0, 5, -1} {
		_, err = save.OutpostApply(OutpostAction{Type: "build", Region: region}, 0)
		assert.ErrorIs(t, err, ErrAction)
	}
	save.Sect.Buildings[BuildingHall] = 2
	_, err = save.OutpostApply(OutpostAction{Type: "build", Region: 1}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	save.Sect.Buildings[BuildingHall] = 3
	save.Hero.Nodes = map[string]*NodeRecord{}
	_, err = save.OutpostApply(OutpostAction{Type: "build", Region: 1}, 0)
	assert.ErrorIs(t, err, ErrUnavailable, "realm alone does not mean the hero has visited the region")
	save.Hero.Nodes["c2_arrive"] = &NodeRecord{Clears: 1}
	before := save.Sect.Res
	evs := outpostAct(t, save, OutpostAction{Type: "build", Region: 1}, 0)
	assert.Contains(t, evs, Ev{K: "outpost_build", N: 1})
	assert.Equal(t, Cost{Stone: before.Stone - 2000, Herb: before.Herb - 600, Ore: before.Ore - 400, Rep: before.Rep - 50}, save.Sect.Res)
	after := save.Sect.Res
	_, err = save.OutpostApply(OutpostAction{Type: "build", Region: 1}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, after, save.Sect.Res)
	view := save.OutpostView(0)
	require.Len(t, view.Regions, 4)
	assert.True(t, view.Regions[0].Built)
	assert.True(t, view.Regions[0].Reached)
	assert.Empty(t, view.Regions[0].Members)
	assert.Zero(t, view.Total)
	assert.Empty(t, view.Busy)
	assert.Contains(t, save.Sect.Log, Ev{K: "outpost_build", N: 1})

	poor := newOutpostSave(t)
	poor.Sect.Res = Cost{Stone: 1999, Herb: 600, Ore: 400, Rep: 50}
	_, err = poor.OutpostApply(OutpostAction{Type: "build", Region: 1}, 0)
	assert.ErrorIs(t, err, ErrCost)
	assert.Empty(t, poor.Sect.Outposts)
	assert.Equal(t, 1999, poor.Sect.Res.Stone)
}

// 派驻占用固定名额，召回核对本次 token。召回不恢复旧工位，不能用旧页召回后来重新派驻的弟子。
func TestOutpostStationCapacityAndRecallToken(t *testing.T) {
	save := newOutpostSave(t)
	s := save.Sect
	outpostAct(t, save, OutpostAction{Type: "build", Region: 1}, 0)
	first, second := s.Disciples[0], s.Disciples[1]
	first.Job = JobField
	first.Realm = 0
	_, err := save.OutpostApply(OutpostAction{Type: "station", Region: 1, Disciple: first.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	first.Realm = 1
	first.Injured = 1
	_, err = save.OutpostApply(OutpostAction{Type: "station", Region: 1, Disciple: first.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	first.Injured = 0
	first.Mission = 9
	_, err = save.OutpostApply(OutpostAction{Type: "station", Region: 1, Disciple: first.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	first.Mission = 0
	outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: first.Id}, 0)
	token := first.OutpostToken
	assert.NotEmpty(t, token)
	assert.Equal(t, JobCultivate, first.Job)
	assert.False(t, first.home(0))
	outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: second.Id}, 0)
	third := s.addDisciple(&Candidate{Talent: 3, Trait: TraitSteady, Layer: 1}, 0)
	third.Realm = 1
	_, err = save.OutpostApply(OutpostAction{Type: "station", Region: 1, Disciple: third.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.ElementsMatch(t, []int{first.Id, second.Id}, save.OutpostView(0).Regions[0].Members)
	_, err = save.OutpostApply(OutpostAction{Type: "recall", Region: 2, Disciple: first.Id, Token: token}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = save.OutpostApply(OutpostAction{Type: "recall", Region: 1, Disciple: first.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	outpostAct(t, save, OutpostAction{Type: "recall", Region: 1, Disciple: first.Id, Token: token}, 0)
	assert.Zero(t, first.Outpost)
	assert.Empty(t, first.OutpostToken)
	assert.Equal(t, JobCultivate, first.Job)
	outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: first.Id}, 0)
	assert.NotEqual(t, token, first.OutpostToken)
	_, err = save.OutpostApply(OutpostAction{Type: "recall", Region: 1, Disciple: first.Id, Token: token}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, 1, first.Outpost)
}

// 产出切换先结旧时段；驻守只得半速修为，不再同时给本宗帮工，仙盟倍率也只乘一次。
func TestOutpostProductionUsesOneClockAndOneCultivationRate(t *testing.T) {
	save := newOutpostSave(t)
	s, d := save.Sect, save.Sect.Disciples[0]
	s.Buildings[BuildingArray] = 0
	s.AllianceLevel = 2
	d.Job = JobField
	d.Trait = TraitClever
	outpostAct(t, save, OutpostAction{Type: "build", Region: 1}, 0)
	before := s.Res
	oldRate := s.Rates(0)
	outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: d.Id}, hour)
	assert.Equal(t, before.Stone+int(oldRate.Stone), s.Res.Stone)
	assert.Equal(t, before.Herb+int(oldRate.Herb), s.Res.Herb)
	assert.InDelta(t, 25, d.Exp, 1e-9, "the first hour was ordinary field work at half cultivation speed")
	view := save.OutpostView(hour)
	alliance := 1 + 2*AllianceBonusPerLevel
	assert.InDelta(t, 16*1.25*1.3*alliance, view.Total.Stone, 1e-9)
	assert.InDelta(t, 6*1.25*1.3*alliance, view.Total.Herb, 1e-9)
	assert.InDelta(t, 12*1.25*1.3*alliance, view.Total.Ore, 1e-9)
	assert.InDelta(t, 25*alliance+view.Total.Herb, s.Rates(hour).Herb, 1e-9, "no field worker bonus remains")
	assert.InDelta(t, 25, s.View(hour).Disciples[0].Rate, 1e-9)
	stationedExp := d.Exp
	s.Tick(2 * hour)
	assert.InDelta(t, stationedExp+25, d.Exp, 1e-9)
	beforeRecall := s.Res
	stationedRate := s.Rates(2 * hour)
	outpostAct(t, save, OutpostAction{Type: "recall", Region: 1, Disciple: d.Id, Token: d.OutpostToken}, 3*hour)
	assert.Equal(t, beforeRecall.Stone+int(stationedRate.Stone), s.Res.Stone)
	assert.Zero(t, save.OutpostView(3*hour).Total)
	assert.InDelta(t, 50, s.View(3 * hour).Disciples[0].Rate, 1e-9, "recall resumes full cultivation")
}

// 驻守仍沿用宗门十二小时上限与零头，频繁结算不会因为自动升层改变下院产出。
func TestOutpostOfflineCapAndSplitSettlementAgree(t *testing.T) {
	makeStationed := func() *Save {
		save := newOutpostSave(t)
		d := save.Sect.Disciples[0]
		d.Layer, d.Exp = 1, ExpNeed(1, 1)-1
		outpostAct(t, save, OutpostAction{Type: "build", Region: 1}, 0)
		outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: d.Id}, 0)
		return save
	}
	whole, split, late := makeStationed(), makeStationed(), makeStationed()
	whole.Sect.Tick(12 * hour)
	split.Sect.Tick(5 * hour)
	split.Sect.Tick(12 * hour)
	late.Sect.Tick(30 * hour)
	assert.Equal(t, whole.Sect.Res, split.Sect.Res)
	assert.Equal(t, whole.Sect.Res, late.Sect.Res)
	assert.Equal(t, whole.Sect.Disciples[0].Layer, split.Sect.Disciples[0].Layer)
	assert.InDelta(t, whole.Sect.Disciples[0].Exp, split.Sect.Disciples[0].Exp, 1e-9)
	assert.Greater(t, whole.Sect.Disciples[0].Layer, 1)
	assert.Equal(t, int64(30*hour), late.Sect.Last)
}

// 驻守排除本宗主动活动和悟道分享，被动守擂仍按既有虚影规则可用。
func TestOutpostResidentsCannotUseTheSameDiscipleAtHome(t *testing.T) {
	save := newOutpostSave(t)
	s, d := save.Sect, save.Sect.Disciples[0]
	outpostAct(t, save, OutpostAction{Type: "build", Region: 1}, 0)
	outpostAct(t, save, OutpostAction{Type: "station", Region: 1, Disciple: d.Id}, 0)
	for _, action := range []Action{
		{Type: "assign", Disciple: d.Id, Job: JobCultivate},
		{Type: "assign", Disciple: d.Id, Job: JobField},
		{Type: "dispatch", Mission: "patrol", Team: []int{d.Id}},
		{Type: "dismiss", Disciple: d.Id},
	} {
		_, err := s.Apply(action, 0)
		assert.ErrorIs(t, err, ErrUnavailable, action.Type)
	}
	for _, pill := range []string{PillHeal, PillQi, PillPeiyuan, PillMarrow} {
		s.Pills[pill] = 1
		_, err := s.Apply(Action{Type: "use_pill", Disciple: d.Id, Pill: pill}, 0)
		assert.ErrorIs(t, err, ErrUnavailable, pill)
		assert.Equal(t, 1, s.Pills[pill])
	}
	_, err := NewTournament(1, s, []int{d.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	save.Hero.Nodes["c1_oath"] = &NodeRecord{Clears: 1}
	_, err = save.HeroApply(HeroAction{Type: "fight", Node: "c1_backhill", Team: []int{d.Id}}, 0, "d1", 1)
	assert.ErrorIs(t, err, ErrUnavailable)
	companion := save.Hero.View(s, 0, "d1").Companions[0]
	assert.False(t, companion.Ready)
	assert.Equal(t, 1, companion.Outpost)
	assert.Equal(t, d.OutpostToken, companion.OutpostToken)
	d.Layer, d.Exp = MaxLayer, ExpNeed(d.Realm, MaxLayer)
	_, err = s.Apply(Action{Type: "breakthrough", Disciple: d.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	d.Layer, d.Exp = 1, 0
	s.Event = &Event{Key: "insight", Target: d.Id}
	assert.False(t, s.EventChoices(0)[0])
	_, err = s.Apply(Action{Type: "event", Choice: 1}, 0)
	require.NoError(t, err)
	assert.Zero(t, d.Exp, "outpost residents do not also attend the main sect's shared insight")
	lineup, err := s.ArenaLineup([]int{d.Id})
	require.NoError(t, err)
	assert.Len(t, s.ArenaDefenders(lineup), 1)

	outpostAct(t, save, OutpostAction{Type: "recall", Region: 1, Disciple: d.Id, Token: d.OutpostToken}, 0)
	save.Hero.Battle = &HeroBattle{Team: []int{d.Id}}
	_, err = save.OutpostApply(OutpostAction{Type: "station", Region: 1, Disciple: d.Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable, "already accompanying the hero also occupies the disciple")
}

// 第十一版旧档没有下院，升级不改变资源与养成；新驻守与token可往返保存。
func TestOutpostSaveUpgradeAndRoundTrip(t *testing.T) {
	save := newOutpostSave(t)
	old, err := common.Marshal(map[string]any{"schema": 11, "sect": save.Sect, "hero": save.Hero})
	require.NoError(t, err)
	loaded, err := ParseSave(string(old))
	require.NoError(t, err)
	assert.Equal(t, SaveSchema, loaded.Schema)
	assert.Equal(t, save.Sect.Res, loaded.Sect.Res)
	assert.Empty(t, loaded.Sect.Outposts)
	assert.Zero(t, loaded.Sect.Disciples[0].Outpost)
	outpostAct(t, loaded, OutpostAction{Type: "build", Region: 2}, 0)
	outpostAct(t, loaded, OutpostAction{Type: "station", Region: 2, Disciple: loaded.Sect.Disciples[0].Id}, 0)
	encoded, err := loaded.Encode()
	require.NoError(t, err)
	again, err := ParseSave(encoded)
	require.NoError(t, err)
	assert.Equal(t, loaded.Sect.Outposts, again.Sect.Outposts)
	assert.Equal(t, loaded.Sect.Disciples[0].OutpostToken, again.Sect.Disciples[0].OutpostToken)
	assert.Equal(t, loaded.OutpostView(0), again.OutpostView(0))
}
