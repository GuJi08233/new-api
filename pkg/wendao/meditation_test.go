package wendao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 灵脉与游历共用实际到达条件，而不是只有境界够就能直接去最高收益的地域。
func TestMeditationNeedsAReachedRegion(t *testing.T) {
	save := looseSave(t)
	view := save.MeditationView(1000)
	assert.Equal(t, []bool{true, false, false, false, false}, view.Available)
	assert.Equal(t, []int{65, 70, 75, 80, 85}, view.Bonuses)
	assert.InDeltaSlice(t, []float64{82.5, 85, 87.5, 90, 92.5}, view.Rates, 1e-9)
	assert.Nil(t, view.Active)
	assert.ErrorIs(t, save.StartMeditation(-1, 1000), ErrAction)
	assert.ErrorIs(t, save.StartMeditation(5, 1000), ErrAction)
	save.Hero.Realm = 1
	assert.ErrorIs(t, save.StartMeditation(1, 1000), ErrUnavailable)
	save.Hero.Nodes["c2_arrive"] = &NodeRecord{Clears: 1}
	require.NoError(t, save.StartMeditation(1, 1000))
	assert.Equal(t, 1, save.Hero.Meditation.Region)
	assert.Equal(t, 70, save.MeditationView(1000).Active.BonusPct)
	assert.Nil(t, (&Save{}).MeditationView(1000))
	assert.ErrorIs(t, (&Save{}).StartMeditation(0, 1000), ErrUnavailable)
}

// 开始先结普通闭关，离座先结打坐；读取概况不另外发放修为，切换以后不重复计时。
func TestMeditationSettlesEachModeAtItsBoundary(t *testing.T) {
	save := looseSave(t)
	h := save.Hero
	require.NoError(t, save.StartMeditation(0, 2800))
	assert.Equal(t, 25.0, h.Exp)
	assert.Equal(t, int64(2800), h.Last)
	token := h.Meditation.Token
	require.NotEmpty(t, token)
	view := h.View(save.Sect, 2800, "d1")
	assert.Equal(t, 82.5, view.Rate)
	assert.Equal(t, 65, view.Sheet.MeditationPct)
	assert.Equal(t, 50.0, view.Meditation.BaseRate)
	assert.Equal(t, token, view.Meditation.Token)
	assert.Equal(t, 25.0, h.Exp, "the view does not issue another settlement")
	save.TickHero(4600)
	assert.Equal(t, 66.25, h.Exp)
	require.NoError(t, save.LeaveMeditation(token, 6400))
	assert.Nil(t, h.Meditation)
	assert.Equal(t, 2, h.Layer)
	assert.Equal(t, 7.5, h.Exp)
	assert.Equal(t, int64(6400), h.Last)
	view = h.View(save.Sect, 6400, "d1")
	assert.Equal(t, 50.0, view.Rate)
	assert.Zero(t, view.Sheet.MeditationPct)
	assert.Nil(t, view.Meditation)
	save.TickHero(10000)
	assert.Equal(t, 57.5, h.Exp, "one hour after leaving uses the ordinary rate")
}

// 打坐长期持续，但一次离线间隔仍只补十二小时；回来结算后可继续打坐，不是游历那种整趟十二小时。
func TestMeditationCapsEachOfflineIntervalAndContinues(t *testing.T) {
	save := looseSave(t)
	h := save.Hero
	h.Realm = 2
	require.NoError(t, save.StartMeditation(0, 1000))
	save.TickHero(1000 + 24*3600)
	assert.Equal(t, 3, h.Layer)
	assert.Equal(t, 440.0, h.Exp, "twelve hours at 330 per hour, through the first two layers")
	require.NotNil(t, h.Meditation)
	save.TickHero(1000 + 25*3600)
	assert.Equal(t, 770.0, h.Exp, "the next online interval continues at the same spirit vein")
	h.Layer, h.Exp = MaxLayer, ExpNeed(h.Realm, MaxLayer)-1
	save.TickHero(1000 + 26*3600)
	assert.True(t, h.Bottleneck())
	assert.Equal(t, ExpNeed(h.Realm, MaxLayer), h.Exp)
}

// 同一个 token 只能离座一次，旧页面与倒退时钟不能结束后来开始的一次打坐。
func TestMeditationRejectsOldTokensAndBackdatedChanges(t *testing.T) {
	save := looseSave(t)
	assert.ErrorIs(t, save.StartMeditation(0, 999), ErrUnavailable)
	require.NoError(t, save.StartMeditation(0, 1000))
	token := save.Hero.Meditation.Token
	assert.ErrorIs(t, save.StartMeditation(0, 1000), ErrUnavailable)
	assert.ErrorIs(t, save.LeaveMeditation("", 1000), ErrUnavailable)
	assert.ErrorIs(t, save.LeaveMeditation("other", 1000), ErrUnavailable)
	assert.ErrorIs(t, save.LeaveMeditation(token, 999), ErrUnavailable)
	save.TickHero(1100)
	assert.ErrorIs(t, save.LeaveMeditation(token, 1050), ErrUnavailable, "cannot move the cultivation boundary backward")
	require.NoError(t, save.LeaveMeditation(token, 1100))
	assert.ErrorIs(t, save.LeaveMeditation(token, 1100), ErrUnavailable)
	require.NoError(t, save.StartMeditation(0, 1100))
	current := save.Hero.Meditation.Token
	assert.NotEqual(t, token, current)
	assert.ErrorIs(t, save.LeaveMeditation(token, 1200), ErrUnavailable)
	assert.Equal(t, current, save.Hero.Meditation.Token)
}

// 灵脉只提高按时间获得的修为，聚气丹、培元丹、悟道丹的即时修为与游历速率不带临时的入座倍率。
func TestMeditationDoesNotMultiplyPillsOrTravel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action HeroAction
		exp    int
		layer  int
		left   float64
	}{
		{name: "qi", action: HeroAction{Type: "qi", Item: PillQi}, exp: 150, layer: 2, left: 50},
		{name: "peiyuan", action: HeroAction{Type: "qi", Item: PillPeiyuan}, exp: 250, layer: 3, left: 30},
		{name: "insight", action: HeroAction{Type: "elixir", Item: ElixirInsight, Grade: GradeLow}, exp: 300, layer: 3, left: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			save := looseSave(t)
			rate := save.Hero.TravelRate(save.Sect, 0)
			save.Sect.Pills[PillQi], save.Sect.Pills[PillPeiyuan] = 1, 1
			save.Hero.Elixirs[ElixirInsight] = [3]int{1}
			require.NoError(t, save.StartMeditation(0, 1000))
			assert.Equal(t, rate, save.Hero.TravelRate(save.Sect, 0))
			evs, err := save.HeroApply(tc.action, 1000, "d1", 0)
			require.NoError(t, err)
			assert.Contains(t, evs, Ev{K: "hero_exp", N: tc.exp})
			assert.Equal(t, tc.layer, save.Hero.Layer)
			assert.Equal(t, tc.left, save.Hero.Exp)
		})
	}
}

// 升级静室先结清原来速度的打坐，下一段才用新的基础速度；不能把新建的屋子追溯套到离线时间。
func TestMeditationUsesCaveUpgradesOnlyAfterTheyAreBuilt(t *testing.T) {
	save := looseSave(t)
	require.NoError(t, save.StartMeditation(0, 1000))
	_, err := save.HeroApply(HeroAction{Type: "room", Item: RoomQuiet}, 4600, "d1", 0)
	require.NoError(t, err)
	assert.Equal(t, 82.5, save.Hero.Exp)
	assert.InDelta(t, 87.45, save.Hero.IdleRate(save.Sect), 1e-9)
	save.TickHero(8200)
	assert.Equal(t, 2, save.Hero.Layer)
	assert.InDelta(t, 69.95, save.Hero.Exp, 1e-9)
}

// 游历、历练和世界契约都要求先离座；反方向也不能在已有活动时入座。
func TestMeditationExcludesOtherPersonalActivities(t *testing.T) {
	save := looseSave(t)
	require.NoError(t, save.StartMeditation(0, 1000))
	assert.ErrorIs(t, save.StartTravel(0, 1000), ErrUnavailable)
	for _, action := range []HeroAction{{Type: "fight", Node: "c1_backhill"}, {Type: "climb"}, {Type: "trial"}} {
		_, err := save.HeroApply(action, 1000, "d1", 1)
		assert.ErrorIs(t, err, ErrUnavailable)
		assert.Nil(t, save.Hero.Battle)
	}
	_, err := save.StartDuel(DuelTrial, "tianjian", 1, 1000)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Nil(t, save.Duels.Active)
	require.NoError(t, save.LeaveMeditation(save.Hero.Meditation.Token, 1000))
	require.NoError(t, save.StartTravel(0, 1000))
	assert.ErrorIs(t, save.StartMeditation(0, 1000), ErrUnavailable)
	_, err = save.FinishTravel(save.Hero.Travel.Token, 1000)
	require.NoError(t, err)
	save.Hero.Battle = &HeroBattle{}
	assert.ErrorIs(t, save.StartMeditation(0, 1000), ErrUnavailable)
	save.Hero.Battle = nil
	save.Duels.Active = &Duel{Kind: DuelBounty}
	assert.ErrorIs(t, save.StartMeditation(0, 1000), ErrUnavailable)
}

// 第九版存档不会凭空入座，已有主角时间与资源保留；新打坐状态编码往返后可继续结算。
func TestMeditationSaveUpgradeAndRoundTrip(t *testing.T) {
	save, err := ParseSave(`{"schema":9,"hero":{"class":"warrior","layer":1,"last":123},"sect":{"buildings":{},"res":{"stone":42}}}`)
	require.NoError(t, err)
	assert.Equal(t, SaveSchema, save.Schema)
	assert.Nil(t, save.Hero.Meditation)
	assert.Equal(t, int64(123), save.Hero.Last)
	assert.Equal(t, 42, save.Sect.Res.Stone)
	require.NoError(t, save.StartMeditation(0, 123))
	data, err := save.Encode()
	require.NoError(t, err)
	again, err := ParseSave(data)
	require.NoError(t, err)
	assert.Equal(t, save.Hero.Meditation, again.Hero.Meditation)
	assert.Equal(t, save.MeditationView(123), again.MeditationView(123))
	require.NoError(t, again.LeaveMeditation(again.Hero.Meditation.Token, 123+3600))
	assert.Equal(t, 82.5, again.Hero.Exp)
}
