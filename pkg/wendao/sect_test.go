package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hour = int64(3600)

// 离线收益最多结算 12 小时：隔 30 小时上线与隔 12 小时上线拿到的一样多。
func TestTickCapsOfflineProgress(t *testing.T) {
	capped := NewSect(1, 0)
	capped.Tick(30 * hour)
	exact := NewSect(1, 0)
	exact.Tick(OfflineCap)

	assert.Equal(t, exact.Res, capped.Res)
	assert.Equal(t, exact.Disciples[0].Layer, capped.Disciples[0].Layer)
	assert.InDelta(t, exact.Disciples[0].Exp, capped.Disciples[0].Exp, 1e-9)
	// 开局主殿 1 级、聚灵阵 1 级、灵田 1 级：每小时 50 灵石、25 灵草。
	assert.Equal(t, 200+50*12, capped.Res.Stone)
	assert.Equal(t, 100+25*12, capped.Res.Herb)
	assert.Equal(t, int64(30*hour), capped.Last)
}

// 产出的零头留到下一次，分几次结算与一次结算拿到的一样多。
func TestTickKeepsFractionalProduction(t *testing.T) {
	split := NewSect(1, 0)
	for at := int64(1); at <= 3600; at++ {
		split.Tick(at)
	}
	whole := NewSect(1, 0)
	whole.Tick(3600)
	assert.Equal(t, whole.Res.Stone, split.Res.Stone)
	assert.Equal(t, whole.Res.Herb, split.Res.Herb)
}

// 受伤期间只按三成速度修炼，伤好之后恢复。
func TestInjuryOnlySlowsTheInjuredPart(t *testing.T) {
	s := NewSect(1, 0)
	d := s.Disciples[0]
	d.Exp, d.Layer = 0, 1
	d.Injured = 2 * hour
	rate := s.cultivateRate(d, JobCultivate)
	s.Tick(4 * hour)
	expected := rate*2*injuredExp + rate*2
	total := d.Exp
	for layer := 1; layer < d.Layer; layer++ {
		total += ExpNeed(0, layer)
	}
	assert.InDelta(t, expected, total, 1e-6)
}

// 主殿之外的建筑不能高过主殿；资源不够什么都不扣。
func TestUpgradeIsCappedByHallAndCost(t *testing.T) {
	s := NewSect(1, 0)
	_, err := s.Apply(Action{Type: "upgrade", Building: BuildingField}, 0)
	require.ErrorIs(t, err, ErrUnavailable)

	s.Res = Cost{Stone: 10}
	_, err = s.Apply(Action{Type: "upgrade", Building: BuildingHall}, 0)
	require.ErrorIs(t, err, ErrCost)
	assert.Equal(t, 10, s.Res.Stone)

	s.Res = Cost{Stone: 1000, Rep: 100}
	evs, err := s.Apply(Action{Type: "upgrade", Building: BuildingHall}, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, s.Buildings[BuildingHall])
	assert.Equal(t, Cost{Stone: 1000 - BuildingCost(BuildingHall, 2).Stone, Rep: 100 - BuildingCost(BuildingHall, 2).Rep}, s.Res)
	assert.Equal(t, []Ev{{K: "built", S: BuildingHall, N: 2}}, evs)
}

// 第九层修满之前不能突破；突破成功进入下一境界第一层，失败损失修为并受伤。
func TestBreakthrough(t *testing.T) {
	s := NewSect(7, 0)
	d := s.Disciples[0]
	_, err := s.Apply(Action{Type: "breakthrough", Disciple: d.Id}, 0)
	require.ErrorIs(t, err, ErrUnavailable)

	successes, failures := 0, 0
	for seed := uint64(1); seed <= 200; seed++ {
		s.Rng = Rng{S: seed}
		d.Realm, d.Layer, d.Exp, d.Injured = 0, MaxLayer, ExpNeed(0, MaxLayer), 0
		_, err := s.Apply(Action{Type: "breakthrough", Disciple: d.Id}, 0)
		require.NoError(t, err)
		if d.Realm == 1 {
			successes++
			assert.Equal(t, 1, d.Layer)
			assert.Zero(t, d.Exp)
			continue
		}
		failures++
		assert.Equal(t, MaxLayer, d.Layer)
		assert.InDelta(t, ExpNeed(0, MaxLayer)*(1-breakFailExp), d.Exp, 1e-9)
		assert.Equal(t, int64(breakInjury), d.Injured)
	}
	assert.Positive(t, successes)
	assert.Positive(t, failures)
}

// 破境丹加成功率，稳重加成功率，最高 95%。
func TestBreakChance(t *testing.T) {
	d := &Disciple{Realm: 0, Talent: 3, Trait: TraitBrave}
	assert.Equal(t, 80, BreakChance(d, false))
	assert.Equal(t, 95, BreakChance(d, true))
	d.Realm, d.Talent = 3, 1
	assert.Equal(t, 20, BreakChance(d, false))
	d.Trait = TraitSteady
	assert.Equal(t, 55, BreakChance(d, true))
	d.Realm = MaxRealm
	assert.Zero(t, BreakChance(d, true))
}

// 聚气丹服一颗要等六小时才能服下一颗。
func TestQiPillCooldown(t *testing.T) {
	s := NewSect(1, 0)
	d := s.Disciples[0]
	s.Pills[PillQi] = 2
	_, err := s.Apply(Action{Type: "use_pill", Pill: PillQi, Disciple: d.Id}, 0)
	require.NoError(t, err)
	_, err = s.Apply(Action{Type: "use_pill", Pill: PillQi, Disciple: d.Id}, hour)
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, 1, s.Pills[PillQi])
	_, err = s.Apply(Action{Type: "use_pill", Pill: PillQi, Disciple: d.Id}, qiPillCooldown)
	require.NoError(t, err)
	assert.Zero(t, s.Pills[PillQi])
}

// 历练回来之前不能结算；回来后弟子回到宗门，收获进库，同一种历练同时只能派一队。
func TestMissionRoundTrip(t *testing.T) {
	s := NewSect(3, 0)
	a, b := s.Disciples[0], s.Disciples[1]
	require.NoError(t, s.dispatch("herbs", []int{a.Id}, 0))
	require.ErrorIs(t, s.dispatch("herbs", []int{b.Id}, 0), ErrUnavailable, "同一种历练只能派一队")
	require.ErrorIs(t, s.dispatch("patrol", []int{a.Id}, 0), ErrUnavailable, "弟子已经出发了")
	require.ErrorIs(t, s.dispatch("ore", []int{b.Id}, 0), ErrUnavailable, "主殿 2 级才解锁")
	require.ErrorIs(t, s.dispatch("patrol", []int{b.Id}, 0), ErrUnavailable, "主殿 1 级只能派一队")

	id := s.Missions[0].Id
	_, err := s.Apply(Action{Type: "claim", Id: id}, hour-1)
	require.ErrorIs(t, err, ErrUnavailable)

	herb := s.Res.Herb
	evs, err := s.Apply(Action{Type: "claim", Id: id}, hour)
	require.NoError(t, err)
	assert.Empty(t, s.Missions)
	assert.Zero(t, a.Mission)
	require.NotEmpty(t, evs)
	var outcome *Ev
	for i := range evs {
		if evs[i].K == "mission" {
			outcome = &evs[i]
		}
	}
	require.NotNil(t, outcome)
	assert.Greater(t, s.Res.Herb, herb)
	assert.Equal(t, 1, s.Stats.Missions)
}

// 叫回的队伍没有收获。
func TestRecallReturnsTeamWithoutReward(t *testing.T) {
	s := NewSect(3, 0)
	a := s.Disciples[0]
	require.NoError(t, s.dispatch("patrol", []int{a.Id}, 0))
	before := s.Res
	_, err := s.Apply(Action{Type: "recall", Id: s.Missions[0].Id}, 0)
	require.NoError(t, err)
	assert.Zero(t, a.Mission)
	assert.Empty(t, s.Missions)
	assert.Equal(t, before, s.Res)
}

// 成功率：总战力等于推荐战力时七成，五行相合与藏经阁加成，夹在 10%~95%。
func TestMissionChance(t *testing.T) {
	spec, ok := missionSpec("herbs")
	require.True(t, ok)
	assert.Equal(t, 70, MissionChance(spec, []int{16}, []int{0}, 0))
	assert.Equal(t, 84, MissionChance(spec, []int{16}, []int{1}, 2))
	assert.Equal(t, 95, MissionChance(spec, []int{40, 40}, []int{1, 1}, 0))
	assert.Equal(t, 10, MissionChance(spec, []int{1}, []int{0}, 0))
}

// 招募要有空位、付得起；弟子上限是主殿等级加二。
func TestRecruitNeedsRoomAndCost(t *testing.T) {
	s := NewSect(5, 0)
	s.Recruits[0] = &Candidate{Name: [3]int{1, 2, -1}, Talent: 3, Trait: TraitBrave, Layer: 1}
	s.Res = Cost{Stone: 50}
	_, err := s.Apply(Action{Type: "recruit", Index: 0}, 0)
	require.ErrorIs(t, err, ErrCost)
	s.Res = Cost{Stone: 500, Rep: 10}
	_, err = s.Apply(Action{Type: "recruit", Index: 0}, 0)
	require.NoError(t, err)
	assert.Len(t, s.Disciples, 3)
	assert.Equal(t, Cost{Stone: 300}, s.Res, "良品资质花 200 灵石、10 声望")
	assert.Nil(t, s.Recruits[0])

	s.Recruits[1] = &Candidate{Talent: 1, Layer: 1}
	_, err = s.Apply(Action{Type: "recruit", Index: 1}, 0)
	require.ErrorIs(t, err, ErrUnavailable, "主殿 1 级最多三名弟子")
}

// 帮工的弟子提高建筑产出，每座最多两名，建筑没建时不能去。
func TestWorkersBoostProduction(t *testing.T) {
	s := NewSect(5, 0)
	base := s.Rates(0)
	require.NoError(t, s.assign(s.Disciples[0].Id, JobField, 0))
	assert.InDelta(t, base.Herb*(1+workerBonus), s.Rates(0).Herb, 1e-9)
	require.ErrorIs(t, s.assign(s.Disciples[1].Id, JobMine, 0), ErrUnavailable)

	s.Disciples[0].Injured = hour
	assert.InDelta(t, base.Herb, s.Rates(0).Herb, 1e-9, "受伤的弟子不能帮工")
}

// 奇遇处理之前不会出现新的；花费不够的选项不能选。
func TestEventChoices(t *testing.T) {
	s := NewSect(9, 0)
	s.Tick(s.NextEventAt)
	require.NotNil(t, s.Event)
	s.Event = &Event{Key: "caravan"}
	s.Res = Cost{Stone: 100, Ore: 0}
	assert.Equal(t, [2]bool{true, false}, s.EventChoices(0))
	_, err := s.Apply(Action{Type: "event", Choice: 1}, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	evs, err := s.Apply(Action{Type: "event", Choice: 0}, 0)
	require.NoError(t, err)
	assert.Nil(t, s.Event)
	assert.Equal(t, 0, s.Res.Stone)
	assert.Equal(t, 250, s.Res.Herb)
	assert.Contains(t, evs, Ev{K: "res", S: "herb", N: 250})
	assert.GreaterOrEqual(t, s.NextEventAt, int64(eventMinGap))
}

// 五行相克：金克木、木克土、土克水、水克火、火克金。
func TestElementEdge(t *testing.T) {
	const metal, wood, water, fire, earth = 0, 1, 2, 3, 4
	for _, pair := range [][2]int{{metal, wood}, {wood, earth}, {earth, water}, {water, fire}, {fire, metal}} {
		assert.Equal(t, 1, ElementEdge(pair[0], pair[1]))
		assert.Equal(t, -1, ElementEdge(pair[1], pair[0]))
	}
	assert.Zero(t, ElementEdge(metal, water))
	assert.Zero(t, ElementEdge(fire, fire))
}

func newTestTournament(t *testing.T) (*Sect, *Tournament) {
	s := NewSect(11, 0)
	s.Pills[PillHeal] = 5
	tour, err := NewTournament(21, s, []int{s.Disciples[0].Id, s.Disciples[1].Id}, 0)
	require.NoError(t, err)
	return s, tour
}

// 大比最多带三颗回春丹；受伤或外出的弟子不能报名。
func TestNewTournamentChecksTeam(t *testing.T) {
	s, tour := newTestTournament(t)
	assert.Equal(t, PouchPills, tour.Taken)
	assert.Len(t, tour.Fighters, 2)
	assert.Equal(t, PhasePick, tour.Phase)
	require.NotNil(t, tour.Opponent)

	s.Disciples[0].Injured = hour
	_, err := NewTournament(1, s, []int{s.Disciples[0].Id}, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = NewTournament(1, s, []int{s.Disciples[1].Id, s.Disciples[1].Id}, 0)
	require.ErrorIs(t, err, ErrAction)
	_, err = NewTournament(1, s, nil, 0)
	require.ErrorIs(t, err, ErrAction)
}

// 法术要两点灵力；回春丹满血时不能服；输了一场大比就结束。
func TestDuelRules(t *testing.T) {
	_, tour := newTestTournament(t)
	require.ErrorIs(t, tour.Play(MoveStrike, 0), ErrAction, "先选人上场")
	require.NoError(t, tour.Play("pick", 0))
	require.ErrorIs(t, tour.Play(MoveSpell, 0), ErrAction)
	require.ErrorIs(t, tour.Play(MovePill, 0), ErrAction)

	me := tour.Fighters[0]
	tour.Intent = MoveGuard
	require.NoError(t, tour.Play(MoveMeditate, 0))
	assert.Equal(t, 2, me.Qi)
	assert.Equal(t, 1, tour.Turn)

	// 让对手一击必杀：这一场输掉，大比结束，成绩是之前赢的场数。
	tour.Opponent.Power = me.Hp * 10
	tour.Intent = MoveStrike
	require.NoError(t, tour.Play(MoveSpell, 0))
	assert.Zero(t, me.Hp)
	assert.True(t, tour.Over())
	assert.Zero(t, tour.Score())
	assert.Empty(t, tour.View().Intent, "结束后不再显示对手的下一招")
}

// 赢一场进入下一轮选人，对手变强；五轮全胜就是夺魁。
func TestDuelWinAdvancesRound(t *testing.T) {
	_, tour := newTestTournament(t)
	first := tour.Opponent.Power
	for round := 0; round < TournamentRounds; round++ {
		require.NoError(t, tour.Play("pick", 0))
		tour.Opponent.Hp = 1
		tour.Intent = MoveGuard
		require.NoError(t, tour.Play(MoveStrike, 0))
		assert.Equal(t, round+1, tour.Wins)
	}
	assert.True(t, tour.Over())
	assert.True(t, tour.Champion())
	assert.Greater(t, tour.Opponent.Power, first)
}

// 回合用完按剩余生命比例判，一样多算输。
func TestDuelTimeout(t *testing.T) {
	_, tour := newTestTournament(t)
	require.NoError(t, tour.Play("pick", 0))
	me, o := tour.Fighters[0], tour.Opponent
	tour.Turn = DuelTurns - 1
	o.Power, me.Power = 0, 0
	me.Hp, o.Hp = me.MaxHp/2, o.MaxHp/2
	tour.Intent = MoveGuard
	require.NoError(t, tour.Play(MoveGuard, 0))
	assert.True(t, tour.Over())
	assert.Zero(t, tour.Wins)
}

// 结算放回没用完的回春丹；每天前三次大比给声望与灵石，之后只记战绩。
func TestRecordTournament(t *testing.T) {
	s, tour := newTestTournament(t)
	s.Pills[PillHeal] -= tour.Taken
	tour.Wins, tour.Pills = 3, 1
	s.Res = Cost{}
	s.RecordTournament(tour, "d1", 0)
	assert.Equal(t, 5-PouchPills+1, s.Pills[PillHeal])
	assert.Equal(t, Cost{Stone: 40 * 3, Rep: 8 * 3}, s.Res)
	for range DailyRewardRuns - 1 {
		tour.Pills = 0
		s.RecordTournament(tour, "d1", 0)
	}
	before := s.Res
	s.RecordTournament(tour, "d1", 0)
	assert.Equal(t, before, s.Res, "第四次没有奖励")
	s.RecordTournament(tour, "d2", 0)
	assert.Greater(t, s.Res.Rep, before.Rep, "第二天重新计数")
	assert.Equal(t, DailyRewardRuns+2, s.Stats.Tournaments)
	assert.Equal(t, 3, s.Stats.BestRounds)
}

// 存档读写往返不丢东西；空存档还没有宗门，秘境的局外成长是一份新的。
func TestSaveEncodeRoundTrip(t *testing.T) {
	s := NewSect(13, 100)
	s.Tick(5 * hour)
	save, err := ParseSave("")
	require.NoError(t, err)
	assert.Nil(t, save.Sect)
	assert.Equal(t, []string{dungeon.ClassWarrior}, save.Abyss.Classes)

	save.Sect = s
	save.Abyss.Crystals = 7
	data, err := save.Encode()
	require.NoError(t, err)
	back, err := ParseSave(data)
	require.NoError(t, err)
	assert.Equal(t, save, back)
}

// 聚灵符按现在的产量给两小时的资源；招贤令不花灵石换一批候选；丹药买了直接进丹房，超过上限不收。
func TestShopItems(t *testing.T) {
	s := NewSect(9, 0)
	rates := s.Rates(0)
	res := s.Res
	require.NoError(t, s.AddItem(ItemSpirit, 1))
	require.NoError(t, s.AddItem(ItemRecruit, 1))
	evs, err := s.Apply(Action{Type: "item", Item: ItemSpirit}, 0)
	require.NoError(t, err)
	assert.Equal(t, res.Stone+int(rates.Stone*spiritHours), s.Res.Stone)
	assert.Equal(t, res.Herb+int(rates.Herb*spiritHours), s.Res.Herb)
	assert.Equal(t, Ev{K: "item", S: ItemSpirit}, evs[len(evs)-1])
	_, err = s.Apply(Action{Type: "item", Item: ItemSpirit}, 0)
	assert.ErrorIs(t, err, ErrNoItem)

	recruits := s.Recruits
	_, err = s.Apply(Action{Type: "item", Item: ItemRecruit}, 0)
	require.NoError(t, err)
	assert.NotSame(t, recruits[0], s.Recruits[0])
	assert.Equal(t, res.Stone+int(rates.Stone*spiritHours), s.Res.Stone, "the recruit order costs no spirit stones")
	assert.Empty(t, s.Items)

	_, err = s.Apply(Action{Type: "item", Item: "pill_" + PillHeal}, 0)
	assert.ErrorIs(t, err, ErrAction, "pills go straight to the alchemy room and are used as pills")
	require.NoError(t, s.AddItem("pill_"+PillHeal, MaxItemStack-s.Pills[PillHeal]))
	assert.ErrorIs(t, s.AddItem("pill_"+PillHeal, 1), ErrItemLimit)
	assert.Equal(t, MaxItemStack, s.Pills[PillHeal])
}
