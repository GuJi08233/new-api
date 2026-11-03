package wendao

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/pkg/doufa"
)

// 斗法的法宝与法术：在正道宗门的藏经阁里用贡献换来的，永久属于主角(退出宗门也还在)。主角出战时带哪两件法宝、用哪个法术
// 由自己配置(Loadout)，没配置时是流派起手的两件与流派自己的法术。只在斗法契约(入门试炼、宗门任务、悬赏)里带，带不进发
// 游戏币的九幽秘境。
//
// 藏经阁的东西记在宗门宝库的货品里(SectGoods)：arm_ 前缀是法宝，skill_ 前缀是法术，后面是斗法里的标识。

const (
	goodsArm   = "arm_"
	goodsSkill = "skill_"
)

// Arms 是主角换来的法宝与法术，Loadout 是现在的配置，为空是默认。
type Arms struct {
	Weapons []string `json:"weapons,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Loadout *Loadout `json:"loadout,omitempty"`
}

// Loadout 是出战的配置：一到两件法宝(先列的先拿在手上)与一个法术。
type Loadout struct {
	Weapons []string `json:"weapons"`
	Skill   string   `json:"skill"`
}

// ArmsView 是页面要的：现在能带的法宝与法术(流派起手的加上换来的)和现在的配置。
type ArmsView struct {
	Weapons []string `json:"weapons"`
	Skills  []string `json:"skills"`
	Loadout Loadout  `json:"loadout"`
}

// ownedWeapons 是主角现在能带的法宝：流派起手的两件，加上换来的。
func (h *Hero) ownedWeapons() []string {
	owned := doufa.StartingWeapons(h.Class)
	if h.Arms != nil {
		for _, w := range h.Arms.Weapons {
			if !slices.Contains(owned, w) {
				owned = append(owned, w)
			}
		}
	}
	return owned
}

// ownedSkills 是主角现在能用的法术：流派自己的，加上换来的。
func (h *Hero) ownedSkills() []string {
	owned := []string{doufa.ClassSkill(h.Class)}
	if h.Arms != nil {
		for _, s := range h.Arms.Skills {
			if !slices.Contains(owned, s) {
				owned = append(owned, s)
			}
		}
	}
	return owned
}

// Loadout 是主角出战的配置：配置过而且还都在手上的用它，否则是流派的默认。
func (h *Hero) Loadout() Loadout {
	if h.Arms != nil && h.Arms.Loadout != nil && h.loadoutOK(*h.Arms.Loadout) {
		return *h.Arms.Loadout
	}
	return Loadout{Weapons: doufa.StartingWeapons(h.Class), Skill: doufa.ClassSkill(h.Class)}
}

func (h *Hero) loadoutOK(l Loadout) bool {
	if len(l.Weapons) < 1 || len(l.Weapons) > 2 || (len(l.Weapons) == 2 && l.Weapons[0] == l.Weapons[1]) {
		return false
	}
	owned := h.ownedWeapons()
	for _, w := range l.Weapons {
		if !slices.Contains(owned, w) {
			return false
		}
	}
	return slices.Contains(h.ownedSkills(), l.Skill)
}

// Equip 配置出战：一到两件不同的法宝与一个法术，都得是主角有的。
func (h *Hero) Equip(weapons []string, skill string) error {
	l := Loadout{Weapons: slices.Clone(weapons), Skill: skill}
	if !h.loadoutOK(l) {
		return ErrAction
	}
	if h.Arms == nil {
		h.Arms = &Arms{}
	}
	h.Arms.Loadout = &l
	return nil
}

// ArmsView 是主角现在的法宝、法术与配置。
func (h *Hero) ArmsView() ArmsView {
	return ArmsView{Weapons: h.ownedWeapons(), Skills: h.ownedSkills(), Loadout: h.Loadout()}
}

// unlockArm 在藏经阁换到一样法宝或法术(货品标识 arm_… 或 skill_…)：已经有了就不能再换(调用方不扣贡献)。
func (h *Hero) unlockArm(goods string) error {
	weapon, isWeapon := strings.CutPrefix(goods, goodsArm)
	skill, isSkill := strings.CutPrefix(goods, goodsSkill)
	if !isWeapon && !isSkill {
		return ErrAction
	}
	if (isWeapon && slices.Contains(h.ownedWeapons(), weapon)) || (isSkill && slices.Contains(h.ownedSkills(), skill)) {
		return ErrUnavailable
	}
	if h.Arms == nil {
		h.Arms = &Arms{}
	}
	if isWeapon {
		h.Arms.Weapons = append(h.Arms.Weapons, weapon)
		return nil
	}
	h.Arms.Skills = append(h.Arms.Skills, skill)
	return nil
}
