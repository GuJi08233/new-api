package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 拆解不在牌组里的功法换灵石：按稀有度给，参悟过的另退一半参悟的灵石；拆掉的功法从收藏里拿走，牌组与预设的下标跟着前移。
// 牌组里的、不存在的拆不了。
func TestSalvageCards(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	h.Cards = append(h.Cards, dungeon.Card{Id: "w_sweep"}, dungeon.Card{Id: "w_fury", Up: true}, dungeon.Card{Id: "w_bash"})
	h.Presets = []*DeckPreset{{Deck: []int{0, 1, 2, 3, 4, 5, 6, 12}}}
	assert.Equal(t, []int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 15, 115, 15}, h.View(s, 0, "d1").Salvage)

	_, err := save.HeroApply(HeroAction{Type: "salvage", Card: 0}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction, "cards in the deck stay")
	_, err = save.HeroApply(HeroAction{Type: "salvage", Card: 13}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)

	stone := s.Res.Stone
	evs := heroAct(t, save, HeroAction{Type: "salvage", Card: 11})
	assert.Equal(t, []Ev{{K: "hero_salvage", S: "w_fury", N: 115, V: 1}, {K: "res", S: "stone", N: 115}}, evs)
	assert.Equal(t, stone+115, s.Res.Stone)
	assert.Equal(t, []dungeon.Card{{Id: "w_sweep"}, {Id: "w_bash"}}, h.Cards[10:])
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 11}, h.Presets[0].Deck, "presets follow the shifted index")
	assert.Equal(t, SalvageDaily-1, h.View(s, 0, "d1").SalvageLeft)
}

// 每天最多拆 SalvageDaily 张，满了以后拆不了(功法与灵石都不动)，第二天重新算；打仗时不能拆。参悟了再拆比不参悟直接拆
// 少拿灵石，没有赚头。
func TestSalvageDailyCap(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	s, h := save.Sect, save.Hero
	for range SalvageDaily + 1 {
		h.Cards = append(h.Cards, dungeon.Card{Id: "w_sweep"})
	}
	for range SalvageDaily {
		heroAct(t, save, HeroAction{Type: "salvage", Card: len(h.Cards) - 1})
	}
	stone, cards := s.Res.Stone, len(h.Cards)
	_, err := save.HeroApply(HeroAction{Type: "salvage", Card: cards - 1}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, stone, s.Res.Stone)
	assert.Len(t, h.Cards, cards)
	assert.Equal(t, 0, h.View(s, 0, "d1").SalvageLeft)
	assert.Equal(t, SalvageDaily, h.View(s, 0, "d2").SalvageLeft, "a new day shows a fresh allowance")

	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	_, err = save.HeroApply(HeroAction{Type: "salvage", Card: cards - 1}, 0, "d2", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "no salvaging mid-battle")
	heroAct(t, save, HeroAction{Type: "flee"})

	s.Res.Stone = 1000
	_, err = save.HeroApply(HeroAction{Type: "upgrade", Card: cards - 1}, 0, "d2", 0)
	require.NoError(t, err)
	_, err = save.HeroApply(HeroAction{Type: "salvage", Card: cards - 1}, 0, "d2", 0)
	require.NoError(t, err)
	assert.Less(t, s.Res.Stone, 1000+SalvageStone[dungeon.RarityCommon], "upgrading before salvaging never pays")
	assert.Equal(t, 1, h.Salvaged)
}
