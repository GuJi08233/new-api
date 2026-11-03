package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTravelSave(t *testing.T) *Save {
	t.Helper()
	save := newHeroSave(t, dungeon.ClassWarrior)
	save.Sect.Buildings[BuildingArray] = 0
	save.Sect.Res = Cost{}
	return save
}

// 世界地图只按境界开放，游历则必须真正到达；出生地始终可去，未知节点不能伪造到达记录。
func TestTravelRequiresAReachedRegion(t *testing.T) {
	save := newTravelSave(t)
	assert.Equal(t, []bool{true, false, false, false, false}, save.TravelView(0).Available)
	assert.ErrorIs(t, save.StartTravel(-1, 0), ErrAction)
	assert.ErrorIs(t, save.StartTravel(len(Regions), 0), ErrAction)
	save.Hero.Realm = 1
	assert.ErrorIs(t, save.StartTravel(1, 0), ErrUnavailable, "realm alone does not mean a visit")
	save.Hero.Nodes["not-a-node"] = &NodeRecord{Clears: 1}
	save.Hero.Nodes["c2_arrive"] = &NodeRecord{}
	assert.False(t, save.Hero.TravelReached(1))
	save.Hero.Nodes["c2_arrive"].Clears = 1
	assert.True(t, save.Hero.TravelReached(1))
	require.NoError(t, save.StartTravel(1, 0))
	assert.Equal(t, 1, save.Hero.Travel.Region)
	assert.ErrorIs(t, save.StartTravel(0, 0), ErrUnavailable, "only one trip at a time")
	assert.Nil(t, (&Save{}).TravelView(0))
	assert.ErrorIs(t, (&Save{}).StartTravel(0, 0), ErrUnavailable)
}

// 境界、层数和更远地域使挂机收益增长；限场数的药力不应不消耗就永久抬高整次行程的收益。
func TestTravelRatesReflectPermanentGrowthWithoutBattleBuffs(t *testing.T) {
	save := newTravelSave(t)
	h := save.Hero
	initial := h.TravelRate(save.Sect, 0)
	h.Realm, h.Layer = 1, 3
	stronger := h.TravelRate(save.Sect, 1)
	assert.Equal(t, TravelRate{Exp: 124, Res: Cost{Stone: 52, Herb: 26, Ore: 13}}, stronger)
	assert.Greater(t, stronger.Exp, initial.Exp)
	assert.Greater(t, stronger.Res.Stone, initial.Res.Stone)
	h.Buffs[ElixirTiger] = &Buff{Power: 10, Left: 3}
	h.Buffs[ElixirGuard] = &Buff{Power: 10, Left: 3}
	assert.Equal(t, stronger, h.TravelRate(save.Sect, 1))
	assert.Equal(t, 3, h.Buffs[ElixirTiger].Left)
	assert.Equal(t, 3, h.Buffs[ElixirGuard].Left)
}

// 出发前的闭关结清，出发后暂停；培养只影响以后的行程，结束后从领取时刻恢复闭关。
func TestTravelSnapshotsGrowthAndReplacesCultivation(t *testing.T) {
	save := newTravelSave(t)
	require.NoError(t, save.StartTravel(0, 1800))
	h := save.Hero
	assert.Equal(t, 25.0, h.Exp)
	assert.Equal(t, TravelRate{Exp: 55, Res: Cost{Stone: 24, Herb: 12, Ore: 6}}, h.Travel.Rate)
	token := h.Travel.Token
	require.NotEmpty(t, token)
	h.Cave[RoomQuiet] = 10
	save.TickHero(3600)
	assert.Equal(t, 25.0, h.Exp, "ordinary cultivation is paused")
	progress := save.TravelView(3600).Active
	require.NotNil(t, progress)
	assert.Equal(t, 27.5, progress.Exp)
	assert.Equal(t, Cost{Stone: 12, Herb: 6, Ore: 3}, progress.Res)
	assert.Equal(t, 55.0, progress.Rate.Exp, "current cultivation cannot retroactively improve a trip")
	assert.Greater(t, save.TravelView(3600).Rates[0].Exp, progress.Rate.Exp)
	assert.Equal(t, 0.0, h.View(save.Sect, 3600, "d1").Rate)
	assert.NotNil(t, h.View(save.Sect, 3600, "d1").Travel)
	assert.Empty(t, save.Sect.Res, "a preview does not issue rewards")
	assert.Equal(t, token, h.Travel.Token)

	loot, err := save.FinishTravel(token, 5400)
	require.NoError(t, err)
	assert.Equal(t, &TravelLoot{Seconds: 3600, Exp: 55, Res: Cost{Stone: 24, Herb: 12, Ore: 6}}, loot)
	assert.Equal(t, 80.0, h.Exp)
	assert.Equal(t, loot.Res, save.Sect.Res)
	assert.Nil(t, h.Travel)
	assert.Equal(t, int64(5400), h.Last)
	assert.Equal(t, 80.0, h.View(save.Sect, 5400, "d1").Rate)
	save.TickHero(6300)
	assert.Equal(t, 2, h.Layer)
	assert.InDelta(t, 0, h.Exp, 1e-9, "only fifteen minutes after finishing count as ordinary cultivation")
}

// 十二小时是整次行程的上限；刷新、同步、跨日与迟领都不能延长收益，也不会在封顶后补发闭关修为。
func TestTravelCapsTheWholeTripAcrossSyncsAndDays(t *testing.T) {
	save := newTravelSave(t)
	require.NoError(t, save.StartTravel(0, 0))
	token := save.Hero.Travel.Token
	save.TickHero(4 * 3600)
	_, err := save.HeroApply(HeroAction{Type: "sync"}, 8*3600, "d2", 0)
	require.NoError(t, err)
	save.TickHero(16 * 3600)
	assert.Zero(t, save.Hero.Exp)
	first := save.TravelView(12 * 3600).Active
	late := save.TravelView(48 * 3600).Active
	assert.Equal(t, first, late)
	assert.True(t, late.Capped)
	assert.Equal(t, int64(OfflineCap), late.Elapsed)
	loot, err := save.FinishTravel(token, 48*3600)
	require.NoError(t, err)
	assert.Equal(t, 660.0, loot.Exp)
	assert.Equal(t, Cost{Stone: 288, Herb: 144, Ore: 72}, loot.Res)
	assert.Equal(t, 5, save.Hero.Layer)
	assert.Equal(t, 140.0, save.Hero.Exp)
	assert.Equal(t, int64(48*3600), save.Hero.Last)
	_, err = save.FinishTravel(token, 48*3600)
	assert.ErrorIs(t, err, ErrUnavailable, "claiming twice does not pay twice")
	assert.Equal(t, loot.Res, save.Sect.Res)
}

// 修为只能加到当前境界圆满，材料仍正常领取；行程中服丹后显示与实际入账仍按剩余容量计算。
func TestTravelReportsActualExperienceAtTheBottleneck(t *testing.T) {
	save := newTravelSave(t)
	h := save.Hero
	h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)-10
	require.NoError(t, save.StartTravel(0, 0))
	progress := save.TravelView(3600).Active
	assert.Equal(t, 10.0, progress.Exp)
	h.addExp(7)
	assert.Equal(t, 3.0, save.TravelView(3600).Active.Exp)
	loot, err := save.FinishTravel(h.Travel.Token, 3600)
	require.NoError(t, err)
	assert.Equal(t, 3.0, loot.Exp)
	assert.True(t, h.Bottleneck())
	assert.Positive(t, loot.Res.Stone)
	require.NoError(t, save.StartTravel(0, 3600))
	loot, err = save.FinishTravel(h.Travel.Token, 7200)
	require.NoError(t, err)
	assert.Zero(t, loot.Exp)
	assert.Positive(t, loot.Res.Stone)
}

// 任意提前结束都只按已过秒数结算，材料向下取整；旧页面与倒退时钟不能结束或重算另一次行程。
func TestTravelClaimsRequireTheCurrentTrip(t *testing.T) {
	save := newTravelSave(t)
	require.NoError(t, save.StartTravel(0, 100))
	token := save.Hero.Travel.Token
	assert.Zero(t, save.TravelView(99).Active.Elapsed)
	_, err := save.FinishTravel(token, 99)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = save.FinishTravel("", 101)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = save.FinishTravel("another-trip", 101)
	assert.ErrorIs(t, err, ErrUnavailable)
	loot, err := save.FinishTravel(token, 101)
	require.NoError(t, err)
	assert.Equal(t, int64(1), loot.Seconds)
	assert.Equal(t, Cost{}, loot.Res)
	assert.InDelta(t, 55.0/3600, loot.Exp, 1e-9)
	require.NoError(t, save.StartTravel(0, 101))
	current := save.Hero.Travel.Token
	assert.NotEqual(t, token, current)
	_, err = save.FinishTravel(token, 3701)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, current, save.Hero.Travel.Token)
}

// 游历与历练、世界契约双向互斥；不要求先改战斗或消耗挑战次数来发现冲突。
func TestTravelExcludesJourneyAndWorldFights(t *testing.T) {
	save := newTravelSave(t)
	require.NoError(t, save.StartTravel(0, 0))
	for _, action := range []HeroAction{{Type: "fight", Node: "c1_backhill"}, {Type: "climb"}, {Type: "trial"}} {
		_, err := save.HeroApply(action, 0, "d1", 1)
		assert.ErrorIs(t, err, ErrUnavailable)
		assert.Nil(t, save.Hero.Battle)
	}
	_, err := save.StartDuel(DuelTrial, "tianjian", 1, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Nil(t, save.Duels.Active)
	_, err = save.FinishTravel(save.Hero.Travel.Token, 0)
	require.NoError(t, err)
	save.Hero.Battle = &HeroBattle{}
	assert.ErrorIs(t, save.StartTravel(0, 0), ErrUnavailable)
	save.Hero.Battle = nil
	save.Duels.Active = &Duel{Kind: DuelBounty}
	assert.ErrorIs(t, save.StartTravel(0, 0), ErrUnavailable)
}

// 第八版没有行程字段：读升级后不凭空出发；新行程往返编码后保留开局时间、token与收益快照。
func TestTravelSaveUpgradeAndRoundTrip(t *testing.T) {
	save, err := ParseSave(`{"schema":8,"hero":{"class":"warrior","layer":1,"last":123},"sect":{"buildings":{},"res":{"stone":42}}}`)
	require.NoError(t, err)
	assert.Equal(t, 9, save.Schema)
	assert.Nil(t, save.Hero.Travel)
	assert.Equal(t, int64(123), save.Hero.Last)
	assert.Equal(t, 42, save.Sect.Res.Stone)
	require.NoError(t, save.StartTravel(0, 123))
	data, err := save.Encode()
	require.NoError(t, err)
	again, err := ParseSave(data)
	require.NoError(t, err)
	assert.Equal(t, save.Hero.Travel, again.Hero.Travel)
	assert.Equal(t, save.TravelView(3600), again.TravelView(3600))
}
