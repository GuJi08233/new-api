package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 属性来源加起来就是下一场开战用的数：生命、力量(含龙虎丹的药力)、护体(含金刚丹的药力)，超过战斗上限的按上限截断；
// 闭关速度按来源算出来与 CultivateRate 一致。药力的场数是开战前的。
func TestStatSheetMatchesTheNextBattle(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h, s := save.Hero, save.Sect
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	h.Realm, h.Layer = 4, 5
	h.Cave[RoomSpring], h.Cave[RoomQuiet] = 3, 4
	s.Buildings[BuildingArray] = 2
	h.Tempered, h.Golden = 12, 1
	// 锋路两件(力量 1+2，合计不超过 3)，御路三件到 10 级(护体 60)，命路一件(生命 18)。
	h.Relics = []string{dungeon.RelicWarriorHeart, dungeon.RelicWhetstone, dungeon.RelicIronPlate, dungeon.RelicSpikedMail,
		dungeon.RelicRune, dungeon.RelicJade}
	h.Worn = append([]string{}, h.Relics...)
	h.Refine = map[string]int{dungeon.RelicWarriorHeart: 5, dungeon.RelicWhetstone: 10, dungeon.RelicIronPlate: 10,
		dungeon.RelicSpikedMail: 10, dungeon.RelicRune: 10, dungeon.RelicJade: 6}
	h.Buffs = map[string]*Buff{ElixirTiger: {Power: 1, Left: 3}, ElixirGuard: {Power: 16, Left: 2}}

	sheet := h.Sheet(s)
	assert.Equal(t, []StatPart{{Key: "realm", N: 4}, {Key: "refine", N: 3}, {Key: "golden", N: 1},
		{Key: ElixirTiger, N: 1, Left: 3}}, sheet.Strength)
	assert.Equal(t, []StatPart{{Key: "refine", N: 60}, {Key: ElixirGuard, N: 16, Left: 2}}, sheet.Block)
	assert.Equal(t, StatPart{Key: "refine", N: 18}, sheet.HP[4])
	assert.Equal(t, []StatPart{{Key: "base", N: heroExpPerHour}, {Key: "realm", N: 16}, {Key: "array", N: 12},
		{Key: "quiet", N: 24}}, sheet.Rate)
	assert.InDelta(t, h.CultivateRate(s), float64(heroExpPerHour*16)*(1+0.36), 1e-9)

	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	setup := h.Battle.Run.Battle
	require.NotNil(t, setup)
	assert.Equal(t, min(partsSum(sheet.HP), sheet.HPCap), setup.MaxHP)
	assert.Equal(t, min(partsSum(sheet.Strength), sheet.StrengthCap), setup.Strength)
	assert.Equal(t, min(partsSum(sheet.Block), sheet.BlockCap), setup.Block)
	assert.Equal(t, 76, setup.Block)
	assert.Equal(t, 2, h.Buffs[ElixirTiger].Left, "a battle uses up one charge")

	// 御路法宝再祭炼到满，护体超过上限：页面上的来源照实写，开战按上限截断。
	h.Battle = nil
	h.Relics = append(h.Relics, dungeon.RelicCrystal)
	h.Worn = []string{dungeon.RelicIronPlate, dungeon.RelicSpikedMail, dungeon.RelicRune, dungeon.RelicCrystal}
	h.Refine[dungeon.RelicCrystal] = 10
	sheet = h.Sheet(s)
	assert.Equal(t, 80+16, partsSum(sheet.Block))
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	assert.Equal(t, 96, h.Battle.Run.Battle.Block)
	h.Battle = nil
	h.Refine[dungeon.RelicMetronome] = 10
	h.Relics = append(h.Relics, dungeon.RelicMetronome)
	h.Worn = append(h.Worn, dungeon.RelicMetronome)
	sheet = h.Sheet(s)
	require.Greater(t, partsSum(sheet.Block), sheet.BlockCap)
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	assert.Equal(t, dungeon.BattleMaxBlock, h.Battle.Run.Battle.Block)
}
