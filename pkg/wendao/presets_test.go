package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 存进格子、换上、清掉：格子与名字有上限，没存过的格子换不上，末尾空着的格子不留在存档里。
func TestDeckPresets(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassRanger)
	h := save.Hero
	starter := append([]int(nil), h.Deck...)
	heroAct(t, save, HeroAction{Type: "preset_save", Index: 1, Name: " 首领 "})
	require.Len(t, h.Presets, 2)
	assert.Nil(t, h.Presets[0])
	assert.Equal(t, &DeckPreset{Name: "首领", Deck: starter}, h.Presets[1])

	heroAct(t, save, HeroAction{Type: "deck", Deck: starter[:DeckMin]})
	heroAct(t, save, HeroAction{Type: "preset_load", Index: 1})
	assert.Equal(t, starter, h.Deck)
	h.Deck[0] = 9
	assert.Equal(t, starter[0], h.Presets[1].Deck[0], "the preset keeps its own copy")
	h.Deck[0] = starter[0]

	for _, bad := range []HeroAction{
		{Type: "preset_save", Index: MaxPresets},
		{Type: "preset_save", Index: -1},
		{Type: "preset_save", Index: 0, Name: "一二三四五六七八九"},
		{Type: "preset_save", Index: 0, Name: "a\nb"},
	} {
		_, err := save.HeroApply(bad, 0, "d1", 0)
		assert.ErrorIs(t, err, ErrAction, "%+v", bad)
	}
	_, err := save.HeroApply(HeroAction{Type: "preset_load", Index: 0}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "an empty slot")
	_, err = save.HeroApply(HeroAction{Type: "preset_delete", Index: 2}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)

	heroAct(t, save, HeroAction{Type: "preset_delete", Index: 1})
	assert.Empty(t, h.Presets, "trailing empty slots are dropped")
	view := h.View(save.Sect, 0, "d1")
	assert.NotNil(t, view.Presets)
}

// 丢掉一张功法时预设里的这张一起拿掉，后面的下标往前挪，仍指着原来那几张；少于 DeckMin 张的预设换不上。打仗时不能改预设。
func TestForgetUpdatesPresets(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	extra := []dungeon.Card{{Id: "w_sweep"}, {Id: "w_bash", Up: true}, {Id: "w_rend"}}
	h.Cards = append(h.Cards, extra...)
	h.Presets = []*DeckPreset{{Name: "试", Deck: []int{10, 11, 12, 0, 1, 2, 3, 4}}}

	heroAct(t, save, HeroAction{Type: "forget", Card: 11})
	assert.Equal(t, []int{10, 11, 0, 1, 2, 3, 4}, h.Presets[0].Deck)
	assert.Equal(t, extra[2], h.Cards[h.Presets[0].Deck[1]], "the shifted index still points at the same card")
	_, err := save.HeroApply(HeroAction{Type: "preset_load", Index: 0}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "too few cards left in the preset")

	heroAct(t, save, HeroAction{Type: "preset_save", Index: 0})
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	for _, a := range []HeroAction{{Type: "preset_load", Index: 0}, {Type: "preset_save", Index: 1}, {Type: "preset_delete", Index: 0}} {
		_, err := save.HeroApply(a, 0, "d1", 0)
		assert.ErrorIs(t, err, ErrUnavailable, "%s mid-battle", a.Type)
	}
}
