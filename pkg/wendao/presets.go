package wendao

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// 牌组预设：把现在的牌组存进几个格子，换着用(打首领一套、刷采集一套)。预设和牌组一样记收藏里的下标；丢掉一张功法时所有
// 预设一起改(拿掉这张、后面的下标往前挪一格)，预设因此少于 DeckMin 张时换不上，要重新存。打仗时不能存也不能换，
// 这一场用的是开战时的牌组。

const (
	// MaxPresets 是牌组预设有几格。
	MaxPresets = 3
	// MaxPresetName 是预设的名字最多几个字。
	MaxPresetName = 8
)

// DeckPreset 是存起来的一套牌组，名字可以不起(页面上叫「预设 N」)。
type DeckPreset struct {
	Name string `json:"name,omitempty"`
	Deck []int  `json:"deck"`
}

// preset 是第 slot 格的预设，没有存过时为 nil。
func (h *Hero) preset(slot int) *DeckPreset {
	if slot < 0 || slot >= len(h.Presets) {
		return nil
	}
	return h.Presets[slot]
}

// savePreset 把现在的牌组存进第 slot 格(盖掉原来的)，起名 name。
func (h *Hero) savePreset(slot int, name string) error {
	name = strings.TrimSpace(name)
	if slot < 0 || slot >= MaxPresets || utf8.RuneCountInString(name) > MaxPresetName || strings.ContainsAny(name, "\r\n\t") {
		return ErrAction
	}
	for len(h.Presets) <= slot {
		h.Presets = append(h.Presets, nil)
	}
	h.Presets[slot] = &DeckPreset{Name: name, Deck: slices.Clone(h.Deck)}
	return nil
}

// loadPreset 换上第 slot 格的牌组，规则与自己挑牌组相同。
func (h *Hero) loadPreset(slot int) error {
	p := h.preset(slot)
	if p == nil || h.setDeck(p.Deck) != nil {
		return ErrUnavailable
	}
	return nil
}

// deletePreset 清掉第 slot 格；末尾空着的格子不留在存档里。
func (h *Hero) deletePreset(slot int) error {
	if h.preset(slot) == nil {
		return ErrUnavailable
	}
	h.Presets[slot] = nil
	for len(h.Presets) > 0 && h.Presets[len(h.Presets)-1] == nil {
		h.Presets = h.Presets[:len(h.Presets)-1]
	}
	return nil
}

// dropFromPresets 在收藏里丢掉第 index 张功法以后改所有预设：拿掉这张，后面的下标往前挪一格。
func (h *Hero) dropFromPresets(index int) {
	for _, p := range h.Presets {
		if p == nil {
			continue
		}
		p.Deck = slices.DeleteFunc(p.Deck, func(i int) bool { return i == index })
		for i, d := range p.Deck {
			if d > index {
				p.Deck[i] = d - 1
			}
		}
	}
}
