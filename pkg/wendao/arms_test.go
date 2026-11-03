package wendao

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 藏经阁里每一样法宝与法术斗法的规则都认识，换到手以后拿去开局不会出错；每家藏经阁两件法宝一个法术，法术要真传才能换。
func TestEveryLibraryGoodOpensInTheFightRules(t *testing.T) {
	for _, sect := range NpcSects {
		var weapons, skills int
		for _, g := range sect.Goods {
			switch {
			case strings.HasPrefix(g.Id, goodsArm):
				weapons++
				for _, class := range []string{dungeon.ClassWarrior, dungeon.ClassMage, dungeon.ClassRanger} {
					setup := doufa.TrialSetup(class)
					setup.Weapons = []string{g.Id[len(goodsArm):]}
					_, err := doufa.New(1, setup)
					require.NoError(t, err, "%s %s", sect.Id, g.Id)
				}
			case strings.HasPrefix(g.Id, goodsSkill):
				skills++
				assert.Equal(t, RankHeir, g.Rank, "a spell is for true disciples")
				setup := doufa.TrialSetup(dungeon.ClassWarrior)
				setup.Skill = g.Id[len(goodsSkill):]
				_, err := doufa.New(1, setup)
				require.NoError(t, err, "%s %s", sect.Id, g.Id)
			}
		}
		assert.Equal(t, 2, weapons, sect.Id)
		assert.Equal(t, boolInt(sect.Style != ""), skills, sect.Id)
	}
}

// 新主角带流派起手的两件法宝与自己的法术；没配置过的出战配置就是这些，契约开局不写法术。
func TestArmsStartAtTheClassDefaults(t *testing.T) {
	sv := looseSave(t)
	h := sv.Hero
	assert.Equal(t, doufa.StartingWeapons(dungeon.ClassWarrior), h.ownedWeapons())
	assert.Equal(t, []string{"quake"}, h.ownedSkills())
	assert.Equal(t, Loadout{Weapons: doufa.StartingWeapons(dungeon.ClassWarrior), Skill: "quake"}, h.Loadout())
	setup := h.duelSetup(doufa.Setup{})
	assert.Equal(t, doufa.StartingWeapons(dungeon.ClassWarrior), setup.Weapons)
	assert.Empty(t, setup.Skill)
	assert.Equal(t, ArmsView{Weapons: h.ownedWeapons(), Skills: h.ownedSkills(), Loadout: h.Loadout()}, h.ArmsView())
}

// 在藏经阁用贡献换：身份不够的换不了、贡献不够的换不了、已经有的(含流派起手的)换不了而且不扣贡献、货品不在这家的换不了；
// 换到的永久属于主角，退出宗门也在。
func TestLibraryExchangeUnlocksArms(t *testing.T) {
	sv := looseSave(t)
	require.NoError(t, sv.JoinSect("tianjian", 100))
	p := sv.Hero.Path
	p.Merit, p.Earned = 1000, 1000

	_, err := sv.Exchange(goodsArm + "spear")
	assert.ErrorIs(t, err, ErrUnavailable, "inner disciples only")
	_, err = sv.Exchange(goodsSkill + "blades")
	assert.ErrorIs(t, err, ErrUnavailable, "true disciples only")
	_, err = sv.Exchange(goodsArm + "thunder")
	assert.ErrorIs(t, err, ErrAction, "that is Jingang Temple's")
	_, err = sv.Exchange(goodsArm + "sword")
	assert.ErrorIs(t, err, ErrAction)

	evs, err := sv.Exchange(goodsArm + "flying_sword")
	require.NoError(t, err)
	assert.Equal(t, []Ev{{K: "path_exchange", S: goodsArm + "flying_sword", N: 180}}, evs)
	assert.Equal(t, 1000-180, p.Merit)
	assert.Equal(t, []string{"flying_sword"}, sv.Hero.Arms.Weapons)
	_, err = sv.Exchange(goodsArm + "flying_sword")
	assert.ErrorIs(t, err, ErrUnavailable, "already have it")
	assert.Equal(t, 1000-180, p.Merit, "a refused exchange costs nothing")

	p.Rank = RankHeir
	_, err = sv.Exchange(goodsArm + "spear")
	require.NoError(t, err)
	_, err = sv.Exchange(goodsSkill + "blades")
	require.NoError(t, err)
	assert.Equal(t, 1000-180-260-480, p.Merit)
	assert.Equal(t, []string{"flying_sword", "spear"}, sv.Hero.Arms.Weapons)
	assert.Equal(t, []string{"blades"}, sv.Hero.Arms.Skills)

	require.NoError(t, sv.LeaveSect(500))
	assert.Subset(t, sv.Hero.ownedWeapons(), []string{"flying_sword", "spear"}, "leaving the sect keeps what was bought")
	assert.Equal(t, []string{"quake", "blades"}, sv.Hero.ownedSkills())

	// 流派自己的法术(法修的焚天)换不了。
	require.NoError(t, sv.Hero.unlockArm(goodsSkill+"nova"))
	assert.ErrorIs(t, sv.Hero.unlockArm(goodsSkill+"quake"), ErrUnavailable)
	assert.ErrorIs(t, sv.Hero.unlockArm("pill_heal"), ErrAction)
}

// 配置出战：一到两件不同的、手上有的法宝与一个手上有的法术；配置以后契约开局用它(换了法术才写 Skill)；手上没有了的配置
// 自动退回默认。
func TestEquipSetsTheLoadoutContractsUse(t *testing.T) {
	sv := looseSave(t)
	h := sv.Hero
	h.Arms = &Arms{Weapons: []string{"spear"}, Skills: []string{"nova"}}

	assert.ErrorIs(t, h.Equip([]string{"thunder"}, "quake"), ErrAction, "not owned")
	assert.ErrorIs(t, h.Equip([]string{"spear", "spear"}, "quake"), ErrAction, "two of the same")
	assert.ErrorIs(t, h.Equip([]string{"spear", "sword", "talisman"}, "quake"), ErrAction, "three")
	assert.ErrorIs(t, h.Equip(nil, "quake"), ErrAction, "none")
	assert.ErrorIs(t, h.Equip([]string{"spear"}, "blades"), ErrAction, "a spell not owned")
	assert.Nil(t, h.Arms.Loadout, "a refused loadout changes nothing")

	require.NoError(t, h.Equip([]string{"spear", "sword"}, "nova"))
	assert.Equal(t, Loadout{Weapons: []string{"spear", "sword"}, Skill: "nova"}, h.Loadout())
	setup := h.duelSetup(doufa.Setup{})
	assert.Equal(t, []string{"spear", "sword"}, setup.Weapons)
	assert.Equal(t, "nova", setup.Skill)
	_, err := doufa.New(1, h.duelSetup(doufa.TrialSetup(dungeon.ClassWarrior)))
	require.NoError(t, err)

	require.NoError(t, h.Equip([]string{"sword"}, "quake"))
	assert.Empty(t, h.duelSetup(doufa.Setup{}).Skill, "the class's own spell needs no override")

	h.Arms.Loadout = &Loadout{Weapons: []string{"thunder"}, Skill: "quake"}
	assert.Equal(t, Loadout{Weapons: doufa.StartingWeapons(dungeon.ClassWarrior), Skill: "quake"}, h.Loadout(), "a stale loadout falls back")
}
