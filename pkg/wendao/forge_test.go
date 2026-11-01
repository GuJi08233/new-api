package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 祭炼的花费与成功率：越往上越贵越难，6 级起要九叶灵芝，9 级起要七彩莲。
func TestRefineSteps(t *testing.T) {
	prev := RefineStep{}
	for level := 1; level <= MaxRefine; level++ {
		step := RefineStepOf(level)
		assert.Greater(t, step.Cost.Ore, prev.Cost.Ore, "level %d", level)
		if level > 1 {
			assert.LessOrEqual(t, step.Chance, prev.Chance, "level %d", level)
		}
		prev = step
	}
	assert.Equal(t, Cost{Stone: 100, Ore: 50}, RefineStepOf(1).Cost)
	assert.Equal(t, 100, RefineStepOf(3).Chance)
	assert.Equal(t, "", RefineStepOf(5).Rare)
	assert.Equal(t, RareLingzhi, RefineStepOf(6).Rare)
	assert.Equal(t, RareLotus, RefineStepOf(9).Rare)
	assert.Equal(t, 30, RefineStepOf(MaxRefine).Chance)
}

// 祭炼要有这件法宝、器室够高、材料够；失败只扣材料并攒祝福，成功后祝福清零。
func TestRefineRelic(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	s.Res = Cost{Stone: 1000000, Ore: 1000000}
	relic := dungeon.ClassRelic(dungeon.ClassWarrior)

	_, err := save.HeroApply(HeroAction{Type: "refine", Item: relic}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "the artifact chamber is not built")
	_, err = save.HeroApply(HeroAction{Type: "refine", Item: dungeon.RelicCrown}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction, "a treasure the hero does not own")

	h.Realm = MaxRealm
	h.Cave[RoomForge] = MaxRoomLevel
	for level := 1; level <= 3; level++ {
		evs := heroAct(t, save, HeroAction{Type: "refine", Item: relic})
		assert.True(t, hasEv(evs, "hero_refine"), "levels 1-3 always succeed")
		assert.Equal(t, level, h.Refine[relic])
	}

	h.Refine[relic] = 5
	_, err = save.HeroApply(HeroAction{Type: "refine", Item: relic}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost, "level 6 needs a nine-leaf lingzhi")

	s.Rare[RareLingzhi] = 50
	fails, ore := 0, s.Res.Ore
	for h.Refine[relic] == 5 {
		evs := heroAct(t, save, HeroAction{Type: "refine", Item: relic})
		ore -= RefineStepOf(6).Cost.Ore
		if hasEv(evs, "hero_refine_fail") {
			fails++
			assert.Equal(t, fails*refineBless, h.Bless[relic])
		}
	}
	assert.Equal(t, 6, h.Refine[relic])
	assert.Empty(t, h.Bless, "blessing resets after a success")
	assert.Equal(t, ore, s.Res.Ore, "every attempt pays")
	assert.Equal(t, 50-fails-1, s.Rare[RareLingzhi])
	assert.LessOrEqual(t, fails, 3, "blessing guarantees success by the fourth try")

	h.Refine[relic] = MaxRefine
	_, err = save.HeroApply(HeroAction{Type: "refine", Item: relic}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
}

// 祭炼的器室上限：法宝不能高过器室的等级。
func TestRefineCappedByForge(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassMage)
	h := save.Hero
	save.Sect.Res = Cost{Stone: 100000, Ore: 100000}
	relic := dungeon.ClassRelic(dungeon.ClassMage)
	h.Cave[RoomForge] = 2
	heroAct(t, save, HeroAction{Type: "refine", Item: relic})
	heroAct(t, save, HeroAction{Type: "refine", Item: relic})
	_, err := save.HeroApply(HeroAction{Type: "refine", Item: relic}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, 2, h.Refine[relic])
}

// 祭炼的加成只算戴着的法宝：命加生命、御加开战护体、锋每 5 级加 1 点力量(合计最多 3 点)，开战时带进战斗。
func TestRefineBonusFromWornRelics(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	h.Realm = 2
	h.Relics = append(h.Relics, dungeon.RelicJade, dungeon.RelicIronPlate, dungeon.RelicWhetstone, dungeon.RelicFlame,
		dungeon.RelicCrown)
	h.Worn = []string{dungeon.RelicJade, dungeon.RelicIronPlate}
	baseHP := h.MaxHP()
	h.Refine[dungeon.RelicJade] = 4
	h.Refine[dungeon.RelicIronPlate] = 7
	h.Refine[dungeon.RelicSpikedMail] = 10
	assert.Equal(t, baseHP+4*RefineHP, h.MaxHP())
	assert.Equal(t, 7*RefineBlock, h.Block(), "an unworn treasure gives nothing")
	assert.Equal(t, 2, h.Strength())

	h.Worn = []string{dungeon.ClassRelic(dungeon.ClassWarrior), dungeon.RelicWhetstone, dungeon.RelicFlame, dungeon.RelicCrown}
	for _, relic := range h.Worn {
		h.Refine[relic] = MaxRefine
	}
	assert.Equal(t, 2+RefineStrengthMax, h.Strength(), "edge strength is capped")
	assert.Equal(t, 0, h.Block())

	h.Worn = []string{dungeon.RelicIronPlate}
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	require.NotNil(t, h.Battle)
	assert.Equal(t, 8+7*RefineBlock, h.Battle.Run.Combat.Block, "the iron plate's own block plus its refinement")
}
