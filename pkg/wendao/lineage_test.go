package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLineageSave(t *testing.T) *Save {
	t.Helper()
	save := newHeroSave(t, dungeon.ClassMage)
	s := save.Sect
	s.Buildings[BuildingHall], s.Buildings[BuildingArray] = 8, 0
	s.Res = Cost{Stone: 100000, Herb: 10000, Ore: 10000, Rep: 10000}
	s.RecruitAt = 100 * hour
	s.Recruits = []*Candidate{
		{Name: [3]int{1, 2, -1}, Element: 1, Talent: 3, Trait: TraitSteady, Layer: 1},
		{Name: [3]int{2, 3, -1}, Element: 2, Talent: 3, Trait: TraitSteady, Layer: 1},
		{Name: [3]int{3, 4, -1}, Element: 3, Talent: 3, Trait: TraitSteady, Layer: 1},
	}
	for _, d := range s.Disciples {
		d.Realm, d.Layer, d.Exp, d.Talent, d.Trait = 0, 1, 0, 3, TraitSteady
	}
	s.Disciples[0].Realm = LineageMinRealm
	return save
}

func lineageRecruit(t *testing.T, save *Save, index, teacher int, now int64) *Disciple {
	t.Helper()
	sum, err := RequestSum(save.Sect.Recruits[index])
	require.NoError(t, err)
	_, err = save.LineageApply(LineageAction{Type: "recruit", Index: index, Teacher: teacher, CandidateSum: sum}, now)
	require.NoError(t, err)
	return save.Sect.Disciples[len(save.Sect.Disciples)-1]
}

// 亲传沿用招募价格与全宗容量，记录师长快照和代数；师长两名当前在宗弟子的名额独立于全宗上限。
func TestLineageRecruitUsesCandidateCostCapacityAndGeneration(t *testing.T) {
	save := newLineageSave(t)
	s, teacher := save.Sect, save.Sect.Disciples[0]
	assert.Equal(t, 1, teacher.Generation)
	assert.Nil(t, (&Save{}).LineageView(0))
	assert.Nil(t, looseSave(t).LineageView(1000))
	before := s.Res
	cost := RecruitCost(s.Recruits[0])
	student := lineageRecruit(t, save, 0, teacher.Id, 0)
	assert.Equal(t, teacher.Id, student.TeacherId)
	require.NotNil(t, student.TeacherName)
	assert.Equal(t, teacher.Name, *student.TeacherName)
	assert.Equal(t, 2, student.Generation)
	assert.Nil(t, s.Recruits[0])
	assert.Equal(t, before.Stone-cost.Stone, s.Res.Stone)
	assert.Equal(t, before.Rep-cost.Rep, s.Res.Rep)
	assert.Equal(t, Cost{Herb: before.Herb, Ore: before.Ore}, Cost{Herb: s.Res.Herb, Ore: s.Res.Ore})
	assert.Contains(t, s.Log, Ev{K: "lineage_recruit", Who: &student.Name, N: 2, V: teacher.Id})
	second := lineageRecruit(t, save, 1, teacher.Id, 0)
	view := save.LineageView(0)
	require.Len(t, view.Teachers, 1)
	assert.ElementsMatch(t, []int{student.Id, second.Id}, view.Teachers[0].Students)
	sum, err := RequestSum(s.Recruits[2])
	require.NoError(t, err)
	_, err = save.LineageApply(LineageAction{Type: "recruit", Index: 2, Teacher: teacher.Id, CandidateSum: sum}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.NotNil(t, s.Recruits[2])
	_, err = s.Apply(Action{Type: "dismiss", Disciple: second.Id}, 0)
	require.NoError(t, err)
	assert.Equal(t, []int{student.Id}, save.LineageView(0).Teachers[0].Students)
	student.Realm = LineageMinRealm
	grandchild := lineageRecruit(t, save, 2, student.Id, 0)
	assert.Equal(t, student.Id, grandchild.TeacherId)
	assert.Equal(t, 3, grandchild.Generation)
	assert.Equal(t, student.Name, *grandchild.TeacherName)
}

// 只有金丹、在本宗且无伤的师长可以收徒；满宗门和不足花费同样不能消耗候选。
func TestLineageRecruitRequiresAnAvailableTeacherAndRoom(t *testing.T) {
	for _, state := range []string{"realm", "injured", "mission", "outpost", "companion", "capacity", "cost"} {
		t.Run(state, func(t *testing.T) {
			save := newLineageSave(t)
			s, teacher := save.Sect, save.Sect.Disciples[0]
			switch state {
			case "realm":
				teacher.Realm = 1
			case "injured":
				teacher.Injured = hour
			case "mission":
				teacher.Mission = 1
			case "outpost":
				teacher.Outpost = 1
			case "companion":
				save.Hero.Battle = &HeroBattle{Team: []int{teacher.Id}}
			case "capacity":
				s.Buildings[BuildingHall] = 1
				s.addDisciple(&Candidate{Talent: 3, Trait: TraitSteady, Layer: 1}, 0)
			case "cost":
				s.Res = Cost{}
			}
			sum, err := RequestSum(s.Recruits[0])
			require.NoError(t, err)
			before, count := s.Res, len(s.Disciples)
			_, err = save.LineageApply(LineageAction{Type: "recruit", Index: 0, Teacher: teacher.Id, CandidateSum: sum}, 0)
			if state == "cost" {
				assert.ErrorIs(t, err, ErrCost)
			} else {
				assert.ErrorIs(t, err, ErrUnavailable)
			}
			assert.NotNil(t, s.Recruits[0])
			assert.Equal(t, count, len(s.Disciples))
			assert.Equal(t, before, s.Res)
		})
	}
}

// 候选指纹对应当页所见的人；旧候选被主动刷新或在结算时到期后，整次改写回滚，不收错人或扣费。
func TestLineageRecruitRejectsStaleCandidatesBeforeAndAfterTick(t *testing.T) {
	save := newLineageSave(t)
	s := save.Sect
	teacher := s.Disciples[0].Id
	sum := s.View(0).Recruits[0].Sum
	require.NotEmpty(t, sum)
	_, err := save.LineageApply(LineageAction{Type: "recruit", Index: 0, Teacher: teacher}, 0)
	assert.ErrorIs(t, err, ErrAction)
	s.Recruits[0].Talent++
	_, err = save.LineageApply(LineageAction{Type: "recruit", Index: 0, Teacher: teacher, CandidateSum: sum}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	s.Recruits[0].Talent--
	s.RecruitAt = 1
	encoded, err := save.Encode()
	require.NoError(t, err)
	updated, err := UpdateSave(encoded, func(current *Save) error {
		_, err := current.LineageApply(LineageAction{Type: "recruit", Index: 0, Teacher: teacher, CandidateSum: sum}, 2)
		return err
	})
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Empty(t, updated)
	unchanged, err := ParseSave(encoded)
	require.NoError(t, err)
	assert.Equal(t, s.Res, unchanged.Sect.Res)
	assert.Len(t, unchanged.Sect.Disciples, len(s.Disciples))
	assert.Equal(t, sum, unchanged.Sect.View(0).Recruits[0].Sum)
	preview, err := ParseSave(encoded)
	require.NoError(t, err)
	preview.Sect.Tick(2)
	newSum := preview.Sect.View(2).Recruits[0].Sum
	require.NotEqual(t, sum, newSum)
	_, err = unchanged.LineageApply(LineageAction{Type: "recruit", Index: 0, Teacher: teacher, CandidateSum: newSum}, 2)
	require.NoError(t, err, "a candidate previewed after expiry can be recruited before any separate sync")
	assert.Equal(t, preview.Sect.Recruits[0].Name, unchanged.Sect.Disciples[len(unchanged.Sect.Disciples)-1].Name)
}

// 随行师长战败或退走先结清健康时段，伤势只停掉之后的授业，不能追溯取消此前的修为。
func TestTeachingSettlesBeforeACompanionIsInjured(t *testing.T) {
	for _, ending := range []string{"lost", "flee"} {
		t.Run(ending, func(t *testing.T) {
			save := newLineageSave(t)
			s, teacher := save.Sect, save.Sect.Disciples[0]
			student := lineageRecruit(t, save, 0, teacher.Id, 0)
			save.Hero.Battle = &HeroBattle{Node: "c1_backhill", Team: []int{teacher.Id}, Token: "same-fight", Fight: &Duel{}}
			if ending == "lost" {
				_, err := save.FinishHeroFight(&doufa.Fight{Over: true, Won: false}, 2*hour)
				require.NoError(t, err)
			} else {
				_, err := save.HeroApply(HeroAction{Type: "flee", BattleToken: "same-fight"}, 2*hour, "d1", 0)
				require.NoError(t, err)
			}
			actual := student.Exp
			for layer := 1; layer < student.Layer; layer++ {
				actual += ExpNeed(0, layer)
			}
			assert.InDelta(t, 115, actual, 1e-9)
			require.Equal(t, int64(3*hour), teacher.Injured)
			s.Tick(3 * hour)
			after := student.Exp
			for layer := 1; layer < student.Layer; layer++ {
				after += ExpNeed(0, layer)
			}
			assert.InDelta(t, 50, after-actual, 1e-9, "the injured teacher does not teach during the next hour")
		})
	}
}

// 授业只提高自然闭关，帮工按半速基数提高；即时服丹仍使用基础闭关速度。
func TestTeachingOnlyChangesNaturalExperienceAndItsView(t *testing.T) {
	save := newLineageSave(t)
	s, teacher := save.Sect, save.Sect.Disciples[0]
	student := lineageRecruit(t, save, 0, teacher.Id, 0)
	view := s.View(0).Disciples[len(s.Disciples)-1]
	assert.Equal(t, 15, view.TeachingPct)
	assert.InDelta(t, 57.5, view.Rate, 1e-9)
	s.Tick(hour)
	assert.InDelta(t, 57.5, student.Exp, 1e-9)
	s.Pills[PillQi] = 1
	beforeExp := student.Exp
	_, err := s.Apply(Action{Type: "use_pill", Pill: PillQi, Disciple: student.Id}, hour)
	require.NoError(t, err)
	actual := student.Exp - beforeExp
	for layer := 1; layer < student.Layer; layer++ {
		actual += ExpNeed(0, layer)
	}
	assert.InDelta(t, 150, actual, 1e-9, "a qi pill still grants three hours at base cultivation speed")
	_, err = s.Apply(Action{Type: "assign", Disciple: student.Id, Job: JobField}, hour)
	require.NoError(t, err)
	assert.InDelta(t, 28.75, s.View(hour).Disciples[len(s.Disciples)-1].Rate, 0.051)
	beforeExp, beforeLayer := student.Exp, student.Layer
	s.Tick(2 * hour)
	actual = student.Exp - beforeExp
	for layer := beforeLayer; layer < student.Layer; layer++ {
		actual += ExpNeed(0, layer)
	}
	assert.InDelta(t, 28.75, actual, 1e-9)
}

// 师徒的伤愈时间按健康时段交集计算；一次长结算和按伤愈点分开结算应得到同样修为。
func TestTeachingInjuryOverlapAndOfflineCapAreDeterministic(t *testing.T) {
	makePair := func() (*Save, *Disciple) {
		save := newLineageSave(t)
		teacher := save.Sect.Disciples[0]
		student := lineageRecruit(t, save, 0, teacher.Id, 0)
		teacher.Injured, student.Injured = 2*hour, hour
		return save, student
	}
	whole, wholeStudent := makePair()
	split, splitStudent := makePair()
	whole.Sect.Tick(4 * hour)
	split.Sect.Tick(hour)
	split.Sect.Tick(2 * hour)
	split.Sect.Tick(4 * hour)
	total := func(d *Disciple) float64 {
		exp := d.Exp
		for layer := 1; layer < d.Layer; layer++ {
			exp += ExpNeed(d.Realm, layer)
		}
		return exp
	}
	assert.InDelta(t, 180, total(wholeStudent), 1e-9, "base injured hour + three healthy hours + two hours of teaching")
	assert.InDelta(t, total(wholeStudent), total(splitStudent), 1e-9)
	assert.Equal(t, wholeStudent.Layer, splitStudent.Layer)
	late, lateStudent := makePair()
	late.Sect.Tick(30 * hour)
	assert.InDelta(t, 690, total(lateStudent), 1e-9, "the most recent capped twelve hours are all healthy")
}

// 派遣、驻守、师长离宗或境界追平会停授；关系和姓名快照不丢失，师徒在本宗恢复条件后继续。
func TestTeachingTracksCurrentTeacherAvailabilityWithoutLosingLineage(t *testing.T) {
	save := newLineageSave(t)
	s, teacher := save.Sect, save.Sect.Disciples[0]
	student := lineageRecruit(t, save, 0, teacher.Id, 0)
	name := *student.TeacherName
	assert.Equal(t, 15, s.teachingPct(student, 0))
	teacher.Mission = 1
	assert.Zero(t, s.teachingPct(student, 0))
	teacher.Mission = 0
	student.Outpost = 1
	assert.Zero(t, s.teachingPct(student, 0))
	student.Outpost = 0
	student.Realm = teacher.Realm
	assert.Zero(t, s.teachingPct(student, 0))
	student.Realm = 0
	assert.Equal(t, 15, s.teachingPct(student, 0))
	teacher.Name = [3]int{7, 8, -1}
	assert.Equal(t, name, *student.TeacherName, "the recorded teacher name is a snapshot")
	_, err := s.Apply(Action{Type: "dismiss", Disciple: teacher.Id}, 0)
	require.NoError(t, err)
	assert.Zero(t, s.teachingPct(student, 0))
	assert.Equal(t, teacher.Id, student.TeacherId)
	assert.Equal(t, name, *student.TeacherName)
	assert.Equal(t, 2, student.Generation)
	assert.Empty(t, save.LineageView(0).Teachers)
}

// 旧档原弟子补成一代，普通招募仍是一代；新亲传资料可往返保存。
func TestLineageSaveUpgradeAndOrdinaryRecruitRemainCompatible(t *testing.T) {
	save := newLineageSave(t)
	save.Sect.Disciples[0].Generation, save.Sect.Disciples[1].Generation = 0, 0
	old, err := common.Marshal(map[string]any{"schema": 12, "hero": save.Hero, "sect": save.Sect})
	require.NoError(t, err)
	loaded, err := ParseSave(string(old))
	require.NoError(t, err)
	assert.Equal(t, SaveSchema, loaded.Schema)
	for _, d := range loaded.Sect.Disciples {
		assert.Equal(t, 1, d.Generation)
		assert.Zero(t, d.TeacherId)
	}
	_, err = loaded.Sect.Apply(Action{Type: "recruit", Index: 0}, 0)
	require.NoError(t, err)
	ordinary := loaded.Sect.Disciples[len(loaded.Sect.Disciples)-1]
	assert.Equal(t, 1, ordinary.Generation)
	assert.Zero(t, ordinary.TeacherId)
	student := lineageRecruit(t, loaded, 1, loaded.Sect.Disciples[0].Id, 0)
	encoded, err := loaded.Encode()
	require.NoError(t, err)
	again, err := ParseSave(encoded)
	require.NoError(t, err)
	assert.Equal(t, student, again.Sect.disciple(student.Id))
	assert.Equal(t, loaded.LineageView(0), again.LineageView(0))
	assert.Equal(t, loaded.Sect.View(0).Recruits[2].Sum, again.Sect.View(0).Recruits[2].Sum)
}
