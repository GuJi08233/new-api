package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 旧卡牌战斗在升级时退出，归还未使用的丹符；已获得的章节、问心阶、功法与养成不丢，也不凭迁移发胜利奖励。
func TestOldJourneyBattleBecomesAvailableForDuels(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	h.Nodes["c1_oath"] = &NodeRecord{Clears: 1, Pick: "kept"}
	h.Steps, h.Tempered = 12, 15
	h.Cards[0].Up = true
	h.Battle = &HeroBattle{Node: "c1_backhill", Run: &dungeon.Run{Potions: []string{dungeon.PotionFire, dungeon.PotionHeal}}}
	h.Weekly = &Weekly{Week: "2026-W45", Chapter: 0, Trials: []WeeklyTrial{{Node: "c1_backhill", Sign: SignBare, HP: 135, Dmg: 117}}, Cleared: []bool{true}, Weeks: 3}
	save.Schema = 7
	data, err := common.Marshal(save)
	require.NoError(t, err)
	loaded, err := ParseSave(string(data))
	require.NoError(t, err)
	assert.Nil(t, loaded.Hero.Battle)
	assert.Equal(t, map[string]int{dungeon.PotionFire: 1, dungeon.PotionHeal: 1}, loaded.Hero.Potions)
	assert.Equal(t, h.Nodes, loaded.Hero.Nodes)
	assert.Equal(t, h.Cards, loaded.Hero.Cards)
	assert.Equal(t, 12, loaded.Hero.Steps)
	assert.Equal(t, 15, loaded.Hero.Tempered)
	assert.Equal(t, h.Weekly.Cleared, loaded.Hero.Weekly.Cleared)
	assert.Equal(t, "2026-W45", loaded.Hero.Weekly.Week)
	assert.Equal(t, 3, loaded.Hero.Weekly.Weeks)
	hp, dmg := EnemyScale(JourneyNodeOf("c1_backhill"))
	assert.Equal(t, hp*90/100, loaded.Hero.Weekly.Trials[0].HP)
	assert.Equal(t, dmg*90/100, loaded.Hero.Weekly.Trials[0].Dmg)
	assert.Zero(t, loaded.Hero.Stats.Wins)
	assert.Equal(t, SaveSchema, loaded.Schema)
}

// 斗法开局是养成与武器的快照。旧牌组不再限制开战，参悟影响全收藏；读取状态不泄露种子和敌人开局。
func TestJourneyFightUsesCultivationAndTheEquippedArms(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	h.Deck = nil
	h.Cards[0].Up = true
	h.Arms = &Arms{Weapons: []string{"spear"}, Skills: []string{"nova"}, Loadout: &Loadout{Weapons: []string{"spear"}, Skill: "nova"}}
	h.Potions[dungeon.PotionFire] = 1
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	running := h.Battle.Fight
	require.NotNil(t, running)
	assert.Equal(t, []string{"spear"}, running.Setup.Weapons)
	assert.Equal(t, "nova", running.Setup.Skill)
	assert.Equal(t, 11, running.Setup.Mods.Damage, "one studied manual plus the opening fire talisman")
	assert.Zero(t, h.Potions[dungeon.PotionFire])
	view := h.View(save.Sect, 0, "d1")
	encoded, err := (&Save{Hero: h}).Encode()
	require.NoError(t, err)
	assert.Contains(t, encoded, `"setup"`, "the server keeps the replay snapshot")
	assert.Equal(t, "c1_backhill", view.Battle.Node)
	public, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(public), `"seed"`)
	assert.NotContains(t, string(public), `"setup"`)
	fight, err := doufa.New(running.Seed, running.Setup)
	require.NoError(t, err)
	assert.Equal(t, view.FightMaxHP, fight.P.MaxHP)
	assert.Equal(t, 1, view.FightMods.Damage, "the public base stats exclude already spent talismans")
	heroAct(t, save, HeroAction{Type: "flee"})
	assert.Zero(t, h.Potions[dungeon.PotionFire], "leaving cannot recover a talisman that already took effect")
}

// 一名主角只能参加一场个人斗法，契约与历练相互排斥，胜利只能结算一次。
func TestPersonalFightsAreExclusiveAndSettleOnce(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	save.RollDuels(weekMonday)
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	_, err := save.StartDuel(DuelTrial, "tianjian", 1, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = save.FinishHeroFight(&doufa.Fight{Over: true, Won: true}, 0)
	require.NoError(t, err)
	stone := save.Sect.Res.Stone
	_, err = save.FinishHeroFight(&doufa.Fight{Over: true, Won: true}, 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, stone, save.Sect.Res.Stone)
	assert.Equal(t, 1, save.Hero.Nodes["c1_backhill"].Clears)
	// 夹具是掌门，入门试炼不开放；悬赏在任意身份均可接。
	_, err = save.StartDuel(DuelBounty, save.Duels.Bounties[0].Id, 1, 0)
	require.NoError(t, err)
	_, err = save.HeroApply(HeroAction{Type: "fight", Node: "c1_backhill"}, 0, "d1", 1)
	assert.ErrorIs(t, err, ErrUnavailable)
}

// 拆掉参悟过的功法立即移除其斗法加成，旧牌组不会把这张功法锁在收藏里。
func TestStudiedManualCanBeSalvagedAndItsBonusIsRemoved(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	heroAct(t, save, HeroAction{Type: "upgrade", Card: 0})
	assert.Equal(t, 1, save.Hero.View(save.Sect, 0, "d1").FightMods.Damage)
	heroAct(t, save, HeroAction{Type: "salvage", Card: 0})
	assert.Zero(t, save.Hero.View(save.Sect, 0, "d1").FightMods.Damage)
	assert.Len(t, save.Hero.Cards, 9)
}

// 即使在同一秒、同一节点重开，旧页面也不能退掉新场；无令牌的旧请求同样拒绝。
func TestFleeRequiresTheCurrentFightToken(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	old := save.Hero.Battle.Token
	require.NotEmpty(t, old)
	heroAct(t, save, HeroAction{Type: "flee"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	current := save.Hero.Battle
	assert.NotEqual(t, old, current.Token)
	for _, token := range []string{old, ""} {
		_, err := save.HeroApply(HeroAction{Type: "flee", BattleToken: token}, 0, "d1", 7)
		assert.ErrorIs(t, err, ErrBattleMoved)
		assert.Same(t, current, save.Hero.Battle)
	}
	heroAct(t, save, HeroAction{Type: "flee"})
	assert.Nil(t, save.Hero.Battle)
}
