package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 回流把本次实际入账分项记下：跨层修为不能只拿经验条的前后差；每项最多补算十二小时。
func TestResumeRecordsActualCultivationAndSectIncome(t *testing.T) {
	const left = int64(100)
	save := newHeroSave(t, dungeon.ClassWarrior)
	h, s := save.Hero, save.Sect
	save.Seen, h.Last, s.Last = left, left, left
	h.Exp = ExpNeed(h.Realm, h.Layer) - 5
	rate, beforeRes := h.IdleRate(s), s.Res
	now := left + 30*hour
	save.Resume(now)
	report := save.Back
	require.NotNil(t, report)
	assert.NotEmpty(t, report.Id)
	assert.Equal(t, left, report.Since)
	assert.Equal(t, now, report.At)
	assert.EqualValues(t, OfflineCap, report.CapSeconds)
	require.NotNil(t, report.Cultivation)
	assert.Equal(t, "cave", report.Cultivation.Kind)
	assert.InDelta(t, rate*12, report.Cultivation.Exp, 1e-9)
	assert.Greater(t, h.Layer, 1, "the returned experience includes levels gained during settlement")
	assert.EqualValues(t, OfflineCap, report.Cultivation.Seconds)
	assert.True(t, report.Cultivation.Capped)
	require.NotNil(t, report.Sect)
	assert.Equal(t, Cost{Stone: s.Res.Stone - beforeRes.Stone, Herb: s.Res.Herb - beforeRes.Herb, Ore: s.Res.Ore - beforeRes.Ore}, report.Sect.Res)
	assert.Positive(t, report.Sect.Res.Stone)
	assert.EqualValues(t, OfflineCap, report.Sect.Seconds)
	assert.True(t, report.Sect.Capped)
	assert.Nil(t, report.Travel)
	assert.Equal(t, now, save.Seen)
	assert.Equal(t, now, h.Last)
	assert.Equal(t, now, s.Last)
}

// 灵脉的加速只进自然修为；游历的收获仍待领取，回来本身不把它发进储物处。
func TestResumeSeparatesVeinIncomeAndPendingTravel(t *testing.T) {
	for _, mode := range []string{"cave", "vein", "travel"} {
		t.Run(mode, func(t *testing.T) {
			const left = int64(100)
			save := newHeroSave(t, dungeon.ClassMage)
			save.Sect = NewStorehouse(1, left)
			save.Seen, save.Hero.Last = left, left
			switch mode {
			case "vein":
				require.NoError(t, save.StartMeditation(0, left))
			case "travel":
				require.NoError(t, save.StartTravel(0, left))
			}
			beforeRes, beforeExp, rate := save.Sect.Res, save.Hero.Exp, save.Hero.IdleRate(save.Sect)
			now := left + 2*hour
			pending := save.Hero.travelProgress(now)
			save.Resume(now)
			report := save.Back
			require.NotNil(t, report)
			assert.Nil(t, report.Sect, "a loose cultivator's storehouse has no sect income")
			assert.Equal(t, beforeRes, save.Sect.Res)
			if mode == "travel" {
				assert.Nil(t, report.Cultivation)
				require.NotNil(t, report.Travel)
				assert.Equal(t, pending, report.Travel)
				assert.Positive(t, report.Travel.Res.Stone)
				assert.Equal(t, beforeExp, save.Hero.Exp)
				assert.NotNil(t, save.Hero.Travel, "returning does not finish or claim the trip")
			} else {
				assert.Nil(t, report.Travel)
				require.NotNil(t, report.Cultivation)
				assert.Equal(t, mode, report.Cultivation.Kind)
				assert.InDelta(t, rate*2, report.Cultivation.Exp, 1e-9)
				assert.Equal(t, int64(2*hour), report.Cultivation.Seconds)
				assert.False(t, report.Cultivation.Capped)
			}
		})
	}
}

// 心跳小于门槛时不弹说明；回归记录的标识与时间保持固定，已读跨存档读写保留，旧关闭请求不收起新的一回。
func TestResumeKeepsOneStableReturnUntilTheNextAbsence(t *testing.T) {
	const left = int64(100)
	save := newHeroSave(t, dungeon.ClassRanger)
	save.Seen, save.Hero.Last, save.Sect.Last = left, left, left
	first := left + ReturnAfter - 1
	save.Resume(first)
	assert.Nil(t, save.Back)
	now := first + ReturnAfter
	save.Resume(now)
	require.NotNil(t, save.Back)
	id := save.Back.Id
	assert.Equal(t, first, save.Back.Since)
	save.Resume(now + 60)
	assert.Equal(t, id, save.Back.Id)
	assert.Equal(t, now, save.Back.At, "ordinary polling cannot renew the display window")
	assert.NotNil(t, save.Agenda(now+60, "d1").Back)
	save.AcknowledgeReturn("older-return")
	assert.False(t, save.Back.Seen)
	save.AcknowledgeReturn(id)
	assert.True(t, save.Back.Seen)
	assert.Nil(t, save.Agenda(now+60, "d1").Back)

	encoded, err := save.Encode()
	require.NoError(t, err)
	loaded, err := ParseSave(encoded)
	require.NoError(t, err)
	assert.Equal(t, save.Seen, loaded.Seen)
	assert.Equal(t, save.Back, loaded.Back)
	loaded.Resume(now + 120)
	assert.Equal(t, id, loaded.Back.Id)
	assert.Nil(t, loaded.Agenda(now+120, "d1").Back)

	later := now + 120 + ReturnAfter
	loaded.Resume(later)
	require.NotNil(t, loaded.Back)
	assert.NotEqual(t, id, loaded.Back.Id)
	loaded.AcknowledgeReturn(id)
	assert.False(t, loaded.Back.Seen)
	assert.NotNil(t, loaded.Agenda(later+comebackShown, "d1").Back)
	assert.Nil(t, loaded.Agenda(later+comebackShown+1, "d1").Back)

	beforeSeen, beforeHero, beforeSect := loaded.Seen, loaded.Hero.Last, loaded.Sect.Last
	loaded.Resume(later - 1)
	assert.Equal(t, beforeSeen, loaded.Seen)
	assert.Equal(t, beforeHero, loaded.Hero.Last)
	assert.Equal(t, beforeSect, loaded.Sect.Last)
}

// 旧存档缺少可见心跳时间时从最后一次结算起算；瓶颈只报告实际还能获得的修为。
func TestResumeOldSaveFallbackAndBottleneck(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	save.Hero.Last, save.Sect.Last = 100, 200
	save.Hero.Layer = MaxLayer
	save.Hero.Exp = ExpNeed(save.Hero.Realm, MaxLayer) - 3
	save.Resume(200 + 2*hour)
	require.NotNil(t, save.Back)
	assert.Equal(t, int64(200), save.Back.Since)
	require.NotNil(t, save.Back.Cultivation)
	assert.InDelta(t, 3, save.Back.Cultivation.Exp, 1e-9)
	assert.True(t, save.Hero.Bottleneck())

	empty, err := ParseSave("")
	require.NoError(t, err)
	empty.Resume(100000)
	assert.Nil(t, empty.Back, "a player without idle progress has no income to summarize")
	assert.Equal(t, int64(100000), empty.Seen)
}
