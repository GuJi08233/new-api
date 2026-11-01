package wendao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 灵田的块数跟着灵田建筑走：开局两块，每两级多一块，最多 MaxPlots 块，升级之后马上多出空地。
func TestGardenPlotsFollowFieldLevel(t *testing.T) {
	for field, want := range map[int]int{1: 2, 2: 3, 4: 4, 9: 6, 10: 7} {
		assert.Equal(t, want, PlotCount(field), "field %d", field)
	}
	s := NewSect(1, 0)
	require.Len(t, s.Garden.Plots, 2)
	s.Buildings[BuildingField] = 4
	assert.Len(t, s.View(0).Garden.Plots, 4)
}

// 种下要付种子钱、灵田建筑要够级；没熟不能收，熟了收到按灵田等级加成的收成，地空出来。
func TestGardenPlantAndHarvest(t *testing.T) {
	s := NewSect(1, 0)
	s.Res = Cost{Stone: 1000, Herb: 1000}
	_, err := s.Apply(Action{Type: "plant", Index: 0, Crop: "tieteng"}, 0)
	assert.ErrorIs(t, err, ErrUnavailable, "玄铁藤要灵田 3 级")

	_, err = s.Apply(Action{Type: "plant", Index: 0, Crop: "qingcao"}, 0)
	require.NoError(t, err)
	_, err = s.Apply(Action{Type: "plant", Index: 0, Crop: "qingcao"}, 0)
	assert.ErrorIs(t, err, ErrUnavailable, "地里已经种了")
	_, err = s.Apply(Action{Type: "harvest", Index: 0}, 3599)
	assert.ErrorIs(t, err, ErrUnavailable)

	s.Rng = Rng{S: 7}
	s.Buildings[BuildingField] = 3
	herb := s.Res.Herb
	evs, err := s.harvest(0, 3600)
	require.NoError(t, err)
	bumper := evs[0].N == 2
	want := 72 // 60 × (1 + 0.1 × 2)
	if bumper {
		want *= 2
	}
	assert.Equal(t, herb+want, s.Res.Herb)
	assert.Equal(t, Plot{}, *s.Garden.Plots[0])
	assert.Equal(t, 1, s.Garden.Harvests["qingcao"])
}

// 一键播种：种子钱够几块种几块；一块都种不了时报错，什么都不扣。
func TestGardenPlantAllStopsWhenSeedsRunOut(t *testing.T) {
	s := NewSect(1, 0)
	s.Buildings[BuildingField] = 4
	s.normalizeGarden()
	s.Res = Cost{Stone: 25}
	_, err := s.plantAll("qingcao", 0)
	require.NoError(t, err)
	planted := 0
	for _, p := range s.Garden.Plots {
		if p.Crop != "" {
			planted++
		}
	}
	assert.Equal(t, 2, planted)
	assert.Equal(t, 5, s.Res.Stone)

	_, err = s.plantAll("qingcao", 0)
	assert.ErrorIs(t, err, ErrCost)
	assert.Equal(t, 5, s.Res.Stone)
}

// 道友浇灌：每次早熟一成生长时间，一茬最多三位道友、每人一次，熟了就不能再浇；主人自己的宗门不算道友由调用方保证。
func TestGardenWateringSpeedsUpGrowth(t *testing.T) {
	s := NewSect(1, 0)
	s.Res.Stone = 100
	_, err := s.plant(0, "qingcao", 0)
	require.NoError(t, err)
	require.NoError(t, s.WaterFor(11, "a", 0, 100))
	assert.Equal(t, int64(3600-360), s.Garden.Plots[0].Ripe)
	assert.ErrorIs(t, s.WaterFor(11, "a", 0, 200), ErrUnavailable, "同一位道友只能浇一次")
	require.NoError(t, s.WaterFor(12, "b", 0, 200))
	require.NoError(t, s.WaterFor(13, "c", 0, 300))
	assert.ErrorIs(t, s.WaterFor(14, "d", 0, 400), ErrUnavailable, "一茬最多三次")
	assert.Equal(t, int64(3600-3*360), s.Garden.Plots[0].Ripe)
	require.Len(t, s.Garden.Log, 3)
	assert.Equal(t, "water", s.Garden.Log[0].Kind)

	_, err = s.plant(1, "qingcao", 0)
	require.NoError(t, err)
	assert.ErrorIs(t, s.WaterFor(11, "a", 1, 3600), ErrUnavailable, "熟了不能再浇")
}

// 道友摘取：熟了半小时还没收才能摘，每人一次、一块地最多两次，每次拿走一成；主人收获时少掉被摘的部分。灵药摘不走。
func TestGardenPickingTakesAShareOfRipeCrops(t *testing.T) {
	s := NewSect(1, 0)
	s.Buildings[BuildingField] = 4
	s.normalizeGarden()
	s.Res = Cost{Stone: 1000, Herb: 1000}
	_, err := s.plant(0, "qingcao", 0)
	require.NoError(t, err)
	_, err = s.plant(1, RareFruit, 0)
	require.NoError(t, err)

	_, err = s.PickFrom(11, "a", 0, 3600+PickGrace-1)
	assert.ErrorIs(t, err, ErrUnavailable, "刚熟的不能摘")
	got, err := s.PickFrom(11, "a", 0, 3600+PickGrace)
	require.NoError(t, err)
	assert.Equal(t, Cost{Herb: 8}, got) // 60 × 1.3 × 0.1，四舍五入
	_, err = s.PickFrom(11, "a", 0, 3600+PickGrace)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = s.PickFrom(12, "b", 0, 3600+PickGrace)
	require.NoError(t, err)
	_, err = s.PickFrom(13, "c", 0, 3600+PickGrace)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = s.PickFrom(11, "a", 1, 7*hour)
	assert.ErrorIs(t, err, ErrUnavailable, "朱果是灵药，摘不走")

	s.Rng = Rng{S: 3}
	herb := s.Res.Herb
	evs, err := s.harvest(0, 7*hour)
	require.NoError(t, err)
	want := 78
	if evs[0].N == 2 {
		want = 156
	}
	assert.Equal(t, herb+int(float64(want)*0.8+0.5), s.Res.Herb)
}

// 去道友那里浇灌、摘取每天各有次数上限，换一天重新算；浇灌有谢礼。
func TestGardenVisitDailyLimits(t *testing.T) {
	s := NewSect(1, 0)
	stone := s.Res.Stone
	for range GardenWaterDaily {
		_, err := s.GardenThanks("water", Cost{}, "2026-11-01")
		require.NoError(t, err)
	}
	_, err := s.GardenThanks("water", Cost{}, "2026-11-01")
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, stone+GardenWaterDaily*WaterReward.Stone, s.Res.Stone)
	water, pick := s.GardenDayLeft("2026-11-01")
	assert.Equal(t, 0, water)
	assert.Equal(t, GardenPickDaily, pick)

	_, err = s.GardenThanks("water", Cost{}, "2026-11-02")
	require.NoError(t, err)
	for range GardenPickDaily {
		_, err := s.GardenThanks("pick", Cost{Ore: 9}, "2026-11-02")
		require.NoError(t, err)
	}
	_, err = s.GardenThanks("pick", Cost{Ore: 9}, "2026-11-02")
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, 9*GardenPickDaily, s.Res.Ore)
}

// 灵药丹方：每颗要扣灵药，灵药不够时什么都不扣；炼出的是同一种丹药。
func TestCraftWithRareHerbs(t *testing.T) {
	s := NewSect(1, 0)
	s.Buildings[BuildingAlchemy] = 5
	s.Res = Cost{Stone: 10000, Ore: 1000}
	s.Rare[RareLotus] = 1
	_, err := s.craft("marrow_lotus", 2)
	assert.ErrorIs(t, err, ErrCost)
	assert.Equal(t, 10000, s.Res.Stone)

	_, err = s.craft("marrow_lotus", 1)
	require.NoError(t, err)
	assert.Equal(t, 0, s.Rare[RareLotus])
	assert.GreaterOrEqual(t, s.Pills[PillMarrow], 1)
	assert.Equal(t, Cost{Stone: 9700, Ore: 900}, s.Res)
}

// 培元丹与聚气丹共用丹力化开的冷却，培元丹给更多修为。
func TestPeiyuanSharesQiCooldown(t *testing.T) {
	s := NewSect(1, 0)
	d := s.Disciples[0]
	d.Layer, d.Exp = 1, 0
	s.Pills[PillPeiyuan], s.Pills[PillQi] = 1, 1
	rate := s.cultivateRate(d, JobCultivate)
	_, err := s.usePill(PillPeiyuan, d.Id, 0)
	require.NoError(t, err)
	total := d.Exp
	for layer := 1; layer < d.Layer; layer++ {
		total += ExpNeed(0, layer)
	}
	assert.InDelta(t, rate*peiyuanPillHours, total, 1e-6)
	_, err = s.usePill(PillQi, d.Id, 60)
	assert.ErrorIs(t, err, ErrUnavailable)
}
