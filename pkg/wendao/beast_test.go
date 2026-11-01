package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDenSave(t *testing.T, den int) *Save {
	t.Helper()
	save := newHeroSave(t, dungeon.ClassWarrior)
	save.Hero.Realm = MaxRealm
	save.Hero.Cave[RoomDen] = den
	save.Sect.Res = Cost{Stone: 1000000, Herb: 1000000, Ore: 1000000}
	return save
}

func beastAct(t *testing.T, save *Save, a HeroAction, now int64, day string) []Ev {
	t.Helper()
	evs, err := save.HeroApply(a, now, day, 0)
	require.NoError(t, err)
	return evs
}

// 引兽：要开辟兽苑、有空位、过了冷却；引来的是蛋，到时间孵化成 1 级灵兽。好饵料要灵药。
func TestLureAndHatch(t *testing.T) {
	save := newDenSave(t, 0)
	h := save.Hero
	_, err := save.HeroApply(HeroAction{Type: "lure", Item: "plain"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "no den yet")

	h.Cave[RoomDen] = 1
	_, err = save.HeroApply(HeroAction{Type: "lure", Item: "lotus"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost, "a lotus bait needs a lotus")
	stone := save.Sect.Res.Stone
	evs := beastAct(t, save, HeroAction{Type: "lure", Item: "plain"}, 0, "d1")
	assert.True(t, hasEv(evs, "hero_beast_egg"))
	assert.Equal(t, stone-Baits[0].Cost.Stone, save.Sect.Res.Stone)
	require.Len(t, h.Beasts, 1)
	egg := h.Beasts[0]
	assert.Contains(t, BeastKinds[:Elements], egg.Kind, "a plain bait never brings a fox")
	assert.Equal(t, int64(BeastHatch), egg.Hatch)

	_, err = save.HeroApply(HeroAction{Type: "feed", Beast: egg.Id, Item: "herb"}, 10, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "an egg cannot eat")
	_, err = save.HeroApply(HeroAction{Type: "summon", Beast: egg.Id}, 10, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)

	h.Cave[RoomDen] = 4
	_, err = save.HeroApply(HeroAction{Type: "lure", Item: "plain"}, 100, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "the lure is cooling down")

	evs = beastAct(t, save, HeroAction{Type: "sync"}, BeastHatch, "d1")
	assert.True(t, hasEv(evs, "hero_beast_hatch"))
	assert.Equal(t, 1, egg.Level)
	assert.Zero(t, egg.Hatch)

	beastAct(t, save, HeroAction{Type: "lure", Item: "plain"}, LureCooldown, "d1")
	_, err = save.HeroApply(HeroAction{Type: "lure", Item: "plain"}, 2*LureCooldown, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "den level 4 keeps two beasts")

	save.TickHero(LureCooldown + BeastHatch)
	assert.Equal(t, 1, h.Beasts[1].Level, "reading the save hatches a due egg too")
}

// 七彩莲饵料常引来九尾灵狐，资质也更高。
func TestLotusBaitBringsFoxes(t *testing.T) {
	save := newDenSave(t, 1)
	h := save.Hero
	save.Sect.Rare[RareLotus] = 200
	foxes, aptitude := 0, 0
	for i := range 200 {
		now := int64(i) * LureCooldown
		beastAct(t, save, HeroAction{Type: "lure", Item: "lotus"}, now, "d1")
		b := h.Beasts[0]
		if b.Kind == BeastFox {
			foxes++
		}
		aptitude += b.Aptitude
		assert.GreaterOrEqual(t, b.Aptitude, 2)
		beastAct(t, save, HeroAction{Type: "release", Beast: b.Id}, now, "d1")
	}
	assert.Greater(t, foxes, 20)
	assert.Less(t, foxes, 80)
	assert.Greater(t, aptitude, 200*3)
}

// 喂养：每天每只 BeastFeeds 次，灵草花宗门的灵草、丹药用丹房的；经验够了自动升级，到了本阶上限要进化。
func TestFeedLevelAndEvolve(t *testing.T) {
	save := newDenSave(t, MaxRoomLevel)
	h := save.Hero
	b := &Beast{Id: 1, Kind: BeastTiger, Aptitude: 3, Level: 1}
	h.Beasts = append(h.Beasts, b)
	h.NextBeast = 1

	herb := save.Sect.Res.Herb
	evs := beastAct(t, save, HeroAction{Type: "feed", Beast: 1, Item: "herb"}, 0, "d1")
	assert.True(t, hasEv(evs, "hero_beast_level"), "60 exp passes level 1")
	assert.Equal(t, 2, b.Level)
	assert.Equal(t, herb-100, save.Sect.Res.Herb)
	assert.Equal(t, bondFeed, b.Bond)

	_, err := save.HeroApply(HeroAction{Type: "feed", Beast: 1, Item: PillQi}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost, "no qi pill in the alchemy room")
	save.Sect.Pills[PillQi] = 10
	for range BeastFeeds - 1 {
		beastAct(t, save, HeroAction{Type: "feed", Beast: 1, Item: PillQi}, 0, "d1")
	}
	_, err = save.HeroApply(HeroAction{Type: "feed", Beast: 1, Item: PillQi}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "six feeds a day")
	beastAct(t, save, HeroAction{Type: "feed", Beast: 1, Item: PillQi}, 0, "d2")

	b.Level, b.Exp = 10, 0
	_, err = save.HeroApply(HeroAction{Type: "feed", Beast: 1, Item: "herb"}, 0, "d3", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "level 10 needs an evolution")
	_, err = save.HeroApply(HeroAction{Type: "evolve", Beast: 1}, 0, "d3", 0)
	assert.ErrorIs(t, err, ErrCost, "two vermilion fruits")
	save.Sect.Rare[RareFruit] = 2
	power := b.Power()
	evs = beastAct(t, save, HeroAction{Type: "evolve", Beast: 1}, 0, "d3")
	assert.True(t, hasEv(evs, "hero_beast_evolve"))
	assert.Equal(t, 1, b.Stage)
	assert.Greater(t, b.Power(), power)
	assert.Zero(t, save.Sect.Rare[RareFruit])

	b.Level = 15
	_, err = save.HeroApply(HeroAction{Type: "evolve", Beast: 1}, 0, "d3", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "not at the stage cap yet")
}

// 灵兽的等级不能超过兽苑等级的三倍。
func TestBeastLevelCappedByDen(t *testing.T) {
	save := newDenSave(t, 2)
	h := save.Hero
	b := &Beast{Id: 1, Kind: BeastDeer, Aptitude: 3, Level: 5}
	h.Beasts = append(h.Beasts, b)
	save.Sect.Pills[PillPeiyuan] = 5
	for range 3 {
		beastAct(t, save, HeroAction{Type: "feed", Beast: 1, Item: PillPeiyuan}, 0, "d1")
		if b.Level == 6 {
			break
		}
	}
	assert.Equal(t, 6, b.Level)
	assert.Zero(t, b.Exp, "experience past the cap is not kept")
	_, err := save.HeroApply(HeroAction{Type: "feed", Beast: 1, Item: "herb"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
}

// 嬉戏每天一次涨亲密；起名有字数限制；放生腾出位置，出战的灵兽被放生就不再出战。
func TestPlayRenameRelease(t *testing.T) {
	save := newDenSave(t, 1)
	h := save.Hero
	b := &Beast{Id: 3, Kind: BeastFox, Aptitude: 5, Level: 4}
	h.Beasts = append(h.Beasts, b)
	beastAct(t, save, HeroAction{Type: "play", Beast: 3}, 0, "d1")
	assert.Equal(t, bondPlay, b.Bond)
	_, err := save.HeroApply(HeroAction{Type: "play", Beast: 3}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	beastAct(t, save, HeroAction{Type: "play", Beast: 3}, 0, "d2")
	assert.Equal(t, 2*bondPlay, b.Bond)

	beastAct(t, save, HeroAction{Type: "rename", Beast: 3, Name: "  小白  "}, 0, "d2")
	assert.Equal(t, "小白", b.Name)
	_, err = save.HeroApply(HeroAction{Type: "rename", Beast: 3, Name: "一二三四五六七八九"}, 0, "d2", 0)
	assert.ErrorIs(t, err, ErrAction)

	beastAct(t, save, HeroAction{Type: "summon", Beast: 3}, 0, "d2")
	assert.Equal(t, 3, h.BeastOut)
	rep := save.Sect.Res.Rep
	beastAct(t, save, HeroAction{Type: "release", Beast: 3}, 0, "d2")
	assert.Empty(t, h.Beasts)
	assert.Zero(t, h.BeastOut)
	assert.Equal(t, rep+4, save.Sect.Res.Rep)
}

// 出战的灵兽跟着进战斗，回合结束时出手；打赢了得经验与亲密。
func TestBeastJoinsBattle(t *testing.T) {
	save := newDenSave(t, MaxRoomLevel)
	h := save.Hero
	h.Realm = 0
	b := &Beast{Id: 1, Kind: BeastTurtle, Aptitude: 3, Level: 3}
	h.Beasts = append(h.Beasts, b)
	beastAct(t, save, HeroAction{Type: "summon", Beast: 1}, 0, "d1")

	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	require.NotNil(t, h.Battle)
	assert.Equal(t, &dungeon.BattleBeast{Kind: BeastTurtle, Move: dungeon.BeastGuard, Power: b.Power()}, h.Battle.Run.Battle.Beast)
	assert.Equal(t, 1, h.Battle.Beast)
	view := h.View(save.Sect, 0, "d1")
	require.NotNil(t, view.Battle.Beast)

	evs := winHeroBattle(t, save, 0)
	assert.True(t, hasEv(evs, "hero_clear"))
	assert.Equal(t, bondBattle, b.Bond)
	assert.Greater(t, b.Level*1000+int(b.Exp), 3*1000, "the beast learned from the fight")
}

// 旧存档没有灵兽：读出来是空的，开战不带灵兽。
func TestBeastsMissingInOldSave(t *testing.T) {
	save, err := ParseSave(`{"hero":{"class":"mage","realm":0,"layer":1}}`)
	require.NoError(t, err)
	assert.NotNil(t, save.Hero.Beasts)
	assert.Nil(t, save.Hero.battleBeast())
}
