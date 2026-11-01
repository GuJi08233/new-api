package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBrewSave(t *testing.T, furnace int) *Save {
	t.Helper()
	save := newHeroSave(t, dungeon.ClassWarrior)
	save.Hero.Realm = MaxRealm
	save.Hero.Cave[RoomFurnace] = furnace
	save.Sect.Res = Cost{Stone: 1000000, Herb: 1000000, Ore: 1000000}
	return save
}

func brewAct(t *testing.T, save *Save, a HeroAction, now int64) []Ev {
	t.Helper()
	evs, err := save.HeroApply(a, now, "d1", 0)
	require.NoError(t, err)
	return evs
}

// 开炉要丹室够级、有空丹炉、材料够；火候决定出炉时间，没到时间收不了。
func TestBrewNeedsFurnaceAndTime(t *testing.T) {
	save := newBrewSave(t, 0)
	h := save.Hero
	_, err := save.HeroApply(HeroAction{Type: "brew", Item: ElixirTemper, Fire: FireSteady}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "no elixir chamber yet")

	h.Cave[RoomFurnace] = 1
	_, err = save.HeroApply(HeroAction{Type: "brew", Item: ElixirTemper, Fire: "lava"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)
	_, err = save.HeroApply(HeroAction{Type: "brew", Item: ElixirGuard, Fire: FireSteady}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "the guard pill needs level 2")

	stone := save.Sect.Res.Stone
	brewAct(t, save, HeroAction{Type: "brew", Item: ElixirTemper, Fire: FireGentle}, 100)
	require.Len(t, h.Furnaces, 1)
	assert.Equal(t, int64(100+3600*3/2), h.Furnaces[0].End, "gentle fire takes half again as long")
	assert.Equal(t, stone-ElixirRecipes[0].Cost.Stone, save.Sect.Res.Stone)
	_, err = save.HeroApply(HeroAction{Type: "brew", Item: ElixirTemper, Fire: FireSteady}, 100, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "one furnace at level 1")

	_, err = save.HeroApply(HeroAction{Type: "collect", Index: 0}, 100+3600, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	evs := brewAct(t, save, HeroAction{Type: "collect", Index: 0}, 100+5400)
	assert.True(t, hasEv(evs, "hero_elixir"))
	assert.Empty(t, h.Furnaces)
	assert.Equal(t, 2, sumGrades(h.Elixirs[ElixirTemper]))
	assert.Equal(t, 1, h.Mastery[ElixirTemper])

	h.Cave[RoomFurnace] = 4
	_, err = save.HeroApply(HeroAction{Type: "brew", Item: ElixirTiger, Fire: FireSteady}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost, "the tiger pill needs a vermilion fruit")
	save.Sect.Rare[RareFruit] = 1
	brewAct(t, save, HeroAction{Type: "brew", Item: ElixirTiger, Fire: FireFierce}, 0)
	assert.Equal(t, int64(3600*3/2), h.Furnaces[0].End, "fierce fire takes half the time")
	assert.Equal(t, 0, save.Sect.Rare[RareFruit])
}

func sumGrades(stock [3]int) int { return stock[0] + stock[1] + stock[2] }

// 调火只能在炼到三分之一到三分之二之间调一次，品质分多 stokeBonus。
func TestStokeWindow(t *testing.T) {
	save := newBrewSave(t, 1)
	h := save.Hero
	brewAct(t, save, HeroAction{Type: "brew", Item: ElixirTemper, Fire: FireSteady}, 0)
	b := h.Furnaces[0]
	before := h.QualityScore(b)
	_, err := save.HeroApply(HeroAction{Type: "stoke", Index: 0}, 1000, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "too early")
	_, err = save.HeroApply(HeroAction{Type: "stoke", Index: 0}, 2500, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "too late")
	brewAct(t, save, HeroAction{Type: "stoke", Index: 0}, 1800)
	assert.Equal(t, before+stokeBonus, h.QualityScore(b))
	_, err = save.HeroApply(HeroAction{Type: "stoke", Index: 0}, 1900, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "only once")
	_, err = save.HeroApply(HeroAction{Type: "stoke", Index: 3}, 1900, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)
}

// 品质分：丹室每级、火候、调火、熟练(最多算 masteryMax 炉)。丹炉数随丹室等级。
func TestQualityScoreAndSlots(t *testing.T) {
	h := &Hero{Cave: map[string]int{RoomFurnace: 6}, Mastery: map[string]int{ElixirTemper: 25}}
	b := &Brew{Recipe: ElixirTemper, Fire: FireGentle, Stoked: true}
	assert.Equal(t, 6*furnaceScore+20+stokeBonus+masteryMax*masteryScore, h.QualityScore(b))
	b = &Brew{Recipe: ElixirGuard, Fire: FireFierce}
	assert.Equal(t, 6*furnaceScore-10, h.QualityScore(b))
	assert.Equal(t, []int{0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3}, ElixirRulesOf().Slots)
}

// 武火有几率炸炉：材料没了、不出丹、不长熟练；没炸的照常出丹。九转金丹不分品级，出炉都是上品。
func TestFierceFireCanExplode(t *testing.T) {
	save := newBrewSave(t, MaxRoomLevel)
	h := save.Hero
	exploded, made := 0, 0
	for i := range 60 {
		now := int64(i) * 100000
		brewAct(t, save, HeroAction{Type: "brew", Item: ElixirTemper, Fire: FireFierce}, now)
		evs := brewAct(t, save, HeroAction{Type: "collect", Index: 0}, now+3600)
		if hasEv(evs, "hero_brew_fail") {
			exploded++
		} else {
			made++
		}
	}
	assert.Positive(t, exploded)
	assert.Positive(t, made)
	assert.Equal(t, made*2, sumGrades(h.Elixirs[ElixirTemper]))
	assert.Equal(t, min(made, masteryMax), h.Mastery[ElixirTemper])

	save.Sect.Rare[RareLotus] = 2
	brewAct(t, save, HeroAction{Type: "brew", Item: ElixirGolden, Fire: FireSteady}, 0)
	brewAct(t, save, HeroAction{Type: "collect", Index: 0}, 8*3600)
	assert.Equal(t, [3]int{0, 0, 1}, h.Elixirs[ElixirGolden])
}

// 丹的效果：淬体丹永久加生命(按境界有上限)，九转金丹永久加力量(最多两颗)，悟道丹给修为并与聚气丹共用冷却。
func TestTakeElixirs(t *testing.T) {
	save := newBrewSave(t, 1)
	h := save.Hero
	h.Realm = 0
	hp := h.MaxHP()
	h.Elixirs[ElixirTemper] = [3]int{0, 0, 5}
	for range 2 {
		brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirTemper, Grade: GradeHigh}, 0)
	}
	brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirTemper, Grade: GradeHigh}, 0)
	assert.Equal(t, TemperCap(0), h.Tempered, "the third pill only fills up to the cap")
	assert.Equal(t, hp+TemperCap(0), h.MaxHP())
	_, err := save.HeroApply(HeroAction{Type: "elixir", Item: ElixirTemper, Grade: GradeHigh}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, [3]int{0, 0, 2}, h.Elixirs[ElixirTemper])
	_, err = save.HeroApply(HeroAction{Type: "elixir", Item: ElixirTemper, Grade: GradeLow}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost)

	h.Elixirs[ElixirGolden] = [3]int{0, 0, 3}
	strength := h.Strength()
	brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirGolden, Grade: GradeHigh}, 0)
	brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirGolden, Grade: GradeHigh}, 0)
	_, err = save.HeroApply(HeroAction{Type: "elixir", Item: ElixirGolden, Grade: GradeHigh}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, strength+GoldenMax, h.Strength())

	h.Elixirs[ElixirInsight] = [3]int{1, 0, 0}
	h.Last = 0
	exp := h.Exp
	evs := brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirInsight, Grade: GradeLow}, 0)
	assert.True(t, hasEv(evs, "hero_exp"))
	assert.True(t, h.Exp > exp || h.Layer > 1)
	assert.Equal(t, int64(qiPillCooldown), h.QiReady)
	_, err = save.HeroApply(HeroAction{Type: "qi"}, 60, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "shares the qi pill cooldown")
	assert.NotContains(t, h.Elixirs, ElixirInsight, "an empty stock is removed")
}

// 金刚丹、龙虎丹的药力管几场：开战时加护体与力量并扣一场，用完就没了；新服的盖过旧的。
func TestBattleBuffsFromElixirs(t *testing.T) {
	save := newBrewSave(t, 1)
	h := save.Hero
	h.Realm = 0
	h.Elixirs[ElixirGuard] = [3]int{1, 0, 0}
	h.Elixirs[ElixirTiger] = [3]int{0, 0, 1}
	brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirGuard, Grade: GradeLow}, 0)
	brewAct(t, save, HeroAction{Type: "elixir", Item: ElixirTiger, Grade: GradeHigh}, 0)
	assert.Equal(t, &Buff{Power: 8, Left: 3}, h.Buffs[ElixirGuard])
	assert.Equal(t, &Buff{Power: 1, Left: 5}, h.Buffs[ElixirTiger])

	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	for battle := 1; battle <= 3; battle++ {
		heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
		require.NotNil(t, h.Battle)
		assert.Equal(t, 8, h.Battle.Run.Combat.Block, "battle %d", battle)
		assert.Equal(t, 1+1, h.Battle.Run.Combat.St[dungeon.StStrength], "the warrior's own treasure plus the tiger pill")
		heroAct(t, save, HeroAction{Type: "flee"})
	}
	assert.NotContains(t, h.Buffs, ElixirGuard, "three battles used it up")
	assert.Equal(t, 2, h.Buffs[ElixirTiger].Left)

	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	assert.Equal(t, 0, h.Battle.Run.Combat.Block)
}
