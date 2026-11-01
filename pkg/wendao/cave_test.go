package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 洞府的屋子花宗门的资源一级级扩建，等级受主角境界限制；资源不够时什么都不扣。
func TestCaveRoomsFollowRealm(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	s.Res = Cost{Stone: 100000, Herb: 100000, Ore: 100000}

	evs := heroAct(t, save, HeroAction{Type: "room", Item: RoomQuiet})
	assert.Equal(t, 1, h.Cave[RoomQuiet])
	assert.True(t, hasEv(evs, "hero_room"))
	assert.Equal(t, 100000-RoomCost(RoomQuiet, 1).Stone, s.Res.Stone)

	heroAct(t, save, HeroAction{Type: "room", Item: RoomQuiet})
	_, err := save.HeroApply(HeroAction{Type: "room", Item: RoomQuiet}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "炼气只能建到 2 级")

	h.Realm = MaxRealm
	for range MaxRoomLevel - 2 {
		heroAct(t, save, HeroAction{Type: "room", Item: RoomQuiet})
	}
	assert.Equal(t, MaxRoomLevel, h.Cave[RoomQuiet])
	_, err = save.HeroApply(HeroAction{Type: "room", Item: RoomQuiet}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)

	_, err = save.HeroApply(HeroAction{Type: "room", Item: "kitchen"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)

	s.Res = Cost{Stone: RoomCost(RoomSpring, 1).Stone - 1, Herb: 100000}
	_, err = save.HeroApply(HeroAction{Type: "room", Item: RoomSpring}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost)
	assert.Equal(t, 0, h.Cave[RoomSpring])
	assert.Equal(t, RoomCost(RoomSpring, 1).Stone-1, s.Res.Stone)
}

// 静室与聚灵阵的加成相加，灵泉加最大生命，开战时带进战斗。
func TestCaveRoomEffects(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassMage)
	s, h := save.Sect, save.Hero
	s.Buildings[BuildingArray] = 2
	base := h.CultivateRate(s)
	h.Cave[RoomQuiet] = 5
	assert.InDelta(t, base*(1+0.06*2+0.06*5)/(1+0.06*2), h.CultivateRate(s), 1e-9)

	hp := h.MaxHP()
	h.Cave[RoomSpring] = 4
	assert.Equal(t, hp+4*springHP, h.MaxHP())
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	require.NotNil(t, h.Battle)
	assert.Equal(t, hp+4*springHP, h.Battle.Run.MaxHP)
}

// 旧存档没有洞府：读出来是空的，屋子都是 0 级。
func TestCaveMissingInOldSave(t *testing.T) {
	save, err := ParseSave(`{"sect":null,"hero":{"class":"warrior","realm":1,"layer":3}}`)
	require.NoError(t, err)
	assert.NotNil(t, save.Hero.Cave)
	assert.Equal(t, 0, save.Hero.Cave[RoomForge])
}
