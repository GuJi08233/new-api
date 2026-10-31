package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHeroSave(t *testing.T, class string) *Save {
	t.Helper()
	save, err := ParseSave("")
	require.NoError(t, err)
	save.Sect = NewSect(5, 0)
	_, err = save.HeroApply(HeroAction{Type: "create", Class: class}, 0, "d1", 0)
	require.NoError(t, err)
	return save
}

func heroAct(t *testing.T, save *Save, a HeroAction) []Ev {
	t.Helper()
	evs, err := save.HeroApply(a, 0, "d1", 7)
	require.NoError(t, err)
	return evs
}

// winHeroBattle 把敌人都压到 1 点生命再用初始的攻击功法打倒，赢了以后收下第一张奖励功法。
func winHeroBattle(t *testing.T, save *Save, pick int) []Ev {
	t.Helper()
	run := save.Hero.Battle.Run
	attack := dungeon.StarterDeck(save.Hero.Class)[0]
	for i, e := range run.Combat.Enemies {
		e.HP = 1
		run.Combat.Hand = []dungeon.Card{attack}
		run.Combat.Energy = 3
		heroAct(t, save, HeroAction{Type: "battle", Move: "play", Card: 0, Target: i, Choice: -1, Potion: -1})
	}
	require.Equal(t, dungeon.PhaseReward, run.Phase)
	return heroAct(t, save, HeroAction{Type: "battle", Move: "reward", Card: -1, Target: -1, Choice: pick, Potion: -1})
}

func hasEv(evs []Ev, kind string) bool {
	for _, ev := range evs {
		if ev.K == kind {
			return true
		}
	}
	return false
}

// 创建主角：初始牌组都在牌组里，本命法宝戴在身上；不认识的流派、没有宗门、重复创建都不行。
func TestNewHero(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassMage)
	h := save.Hero
	assert.Len(t, h.Cards, 10)
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, h.Deck)
	assert.Equal(t, []string{dungeon.ClassRelic(dungeon.ClassMage)}, h.Worn)
	assert.Equal(t, dungeon.ClassHP(dungeon.ClassMage), h.MaxHP())

	_, err := save.HeroApply(HeroAction{Type: "create", Class: dungeon.ClassWarrior}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "one hero per save")
	empty, err := ParseSave("")
	require.NoError(t, err)
	_, err = empty.HeroApply(HeroAction{Type: "create", Class: dungeon.ClassWarrior}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "the sect comes first")
	empty.Sect = NewSect(1, 0)
	_, err = empty.HeroApply(HeroAction{Type: "create", Class: "bard"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction)
}

// 剧情节点按顺序解锁、只走一次；第一次通过战斗节点发全部奖励，收下的功法进收藏和牌组，带出去没用的丹符放回行囊。
func TestJourneyFirstClearRewards(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	_, err := save.HeroApply(HeroAction{Type: "fight", Node: "c1_backhill"}, 0, "d1", 1)
	assert.ErrorIs(t, err, ErrUnavailable, "the oath comes first")

	stone := save.Sect.Res.Stone
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	assert.Equal(t, stone+50, save.Sect.Res.Stone)
	_, err = save.HeroApply(HeroAction{Type: "visit", Node: "c1_oath"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "a story is told once")

	h.Potions[dungeon.PotionFire] = 1
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	require.NotNil(t, h.Battle)
	assert.Equal(t, []string{dungeon.PotionFire}, h.Battle.Carried)
	assert.Empty(t, h.Potions, "carried potions leave the bag")
	_, err = save.HeroApply(HeroAction{Type: "deck", Deck: h.Deck}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "no deck changes mid-battle")

	stone = save.Sect.Res.Stone
	evs := winHeroBattle(t, save, 0)
	assert.Nil(t, h.Battle)
	assert.True(t, hasEv(evs, "hero_card"))
	assert.Len(t, h.Cards, 11)
	assert.Len(t, h.Deck, 11, "a picked card joins the deck while it has room")
	assert.Equal(t, stone+40, save.Sect.Res.Stone)
	assert.Equal(t, 1, h.Potions[dungeon.PotionFire], "the unused fire talisman is back")
	assert.Equal(t, 1, h.Potions[dungeon.PotionHeal], "first-clear potion")
	assert.Equal(t, float64(40), h.Exp)
	assert.Equal(t, 1, h.Stats.Wins)
}

// 重复通过：修为减半，资源每天只有前 DailyRepeatRewards 次；输了没有奖励，逃跑算输。
func TestJourneyRepeatsAndDailyCap(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassRanger)
	h := save.Hero
	heroAct(t, save, HeroAction{Type: "visit", Node: "c1_oath"})
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	winHeroBattle(t, save, -1)
	assert.Len(t, h.Cards, 10, "skipping the reward keeps the collection")

	h.DayRepeats = DailyRepeatRewards - 1
	stone, exp := save.Sect.Res.Stone, h.Exp
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	winHeroBattle(t, save, -1)
	assert.Equal(t, stone+10, save.Sect.Res.Stone)
	assert.Equal(t, exp+20, h.Exp, "repeat exp is halved")

	stone = save.Sect.Res.Stone
	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	winHeroBattle(t, save, -1)
	assert.Equal(t, stone, save.Sect.Res.Stone, "no resources past the daily cap")

	_, err := save.HeroApply(HeroAction{Type: "sync"}, 0, "d2", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, h.DayRepeats, "a new day resets the cap")

	heroAct(t, save, HeroAction{Type: "fight", Node: "c1_backhill"})
	heroAct(t, save, HeroAction{Type: "flee"})
	assert.Nil(t, h.Battle)
	assert.Equal(t, 1, h.Stats.Losses)
	assert.Equal(t, 3, h.Nodes["c1_backhill"].Clears)
}

// 采集节点隔 Cooldown 秒才能再去。
func TestJourneyGatherCooldown(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	for _, id := range []string{"c1_oath", "c1_backhill", "c1_town", "c1_imps"} {
		h.Nodes[id] = &NodeRecord{Clears: 1}
	}
	herb := save.Sect.Res.Herb
	_, err := save.HeroApply(HeroAction{Type: "visit", Node: "c1_herbs"}, 100, "d1", 0)
	require.NoError(t, err)
	assert.Equal(t, herb+60, save.Sect.Res.Herb)
	_, err = save.HeroApply(HeroAction{Type: "visit", Node: "c1_herbs"}, 100+3600, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Equal(t, int64(100+4*3600), h.GatherReady(JourneyNodeOf("c1_herbs"), 200))
	_, err = save.HeroApply(HeroAction{Type: "visit", Node: "c1_herbs"}, 100+4*3600, "d1", 0)
	require.NoError(t, err)
}

// 下一章要先突破到对应的境界；突破要第九层圆满与本境界的机缘，成功用掉机缘，失败损失修为、机缘还在；破境丹从宗门扣。
func TestHeroBreakthroughGatesChapters(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	for i := range JourneyNodes {
		if JourneyNodes[i].Chapter == 0 {
			h.Nodes[JourneyNodes[i].Id] = &NodeRecord{Clears: 1}
		}
	}
	assert.False(t, h.Unlocked(JourneyNodeOf("c2_arrive")), "chapter two needs foundation establishment")

	_, err := save.HeroApply(HeroAction{Type: "breakthrough"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "not at the bottleneck yet")
	h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)
	_, err = save.HeroApply(HeroAction{Type: "breakthrough"}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrUnavailable, "needs the foundation dew")

	h.Keys[BreakKeys[0]] = 1
	save.Sect.Pills[PillBreak] = 0
	_, err = save.HeroApply(HeroAction{Type: "breakthrough", UsePill: true}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost)

	save.Sect.Pills[PillBreak] = 1
	for h.Realm == 0 {
		h.Layer, h.Exp = MaxLayer, ExpNeed(0, MaxLayer)
		evs := heroAct(t, save, HeroAction{Type: "breakthrough", UsePill: save.Sect.Pills[PillBreak] > 0})
		if hasEv(evs, "hero_break_fail") {
			assert.Equal(t, 1, h.Keys[BreakKeys[0]], "a failed attempt keeps the key")
			assert.Less(t, h.Exp, ExpNeed(0, MaxLayer))
		}
	}
	assert.Equal(t, 0, save.Sect.Pills[PillBreak])
	assert.Equal(t, 1, h.Layer)
	assert.Empty(t, h.Keys)
	assert.True(t, h.Unlocked(JourneyNodeOf("c2_arrive")))
	assert.Equal(t, dungeon.ClassHP(dungeon.ClassWarrior)+heroRealmHP, h.MaxHP())
	assert.Equal(t, 3, h.RelicSlots())
}

// 牌组的张数与下标要合法；丢掉收藏里的功法时牌组的下标跟着移动，牌组里的功法不能丢；参悟花宗门的灵石。
func TestHeroDeckForgetAndUpgrade(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	h := save.Hero
	for _, deck := range [][]int{{0, 1, 2}, {0, 0, 1, 2, 3, 4, 5, 6}, {0, 1, 2, 3, 4, 5, 6, 99}} {
		_, err := save.HeroApply(HeroAction{Type: "deck", Deck: deck}, 0, "d1", 0)
		assert.ErrorIs(t, err, ErrAction, "%v", deck)
	}
	heroAct(t, save, HeroAction{Type: "deck", Deck: []int{9, 1, 2, 3, 4, 5, 6, 7}})
	_, err := save.HeroApply(HeroAction{Type: "forget", Card: 1}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction, "cards in the deck stay")
	heroAct(t, save, HeroAction{Type: "forget", Card: 0})
	assert.Equal(t, []int{8, 0, 1, 2, 3, 4, 5, 6}, h.Deck)
	assert.Len(t, h.Cards, 9)

	stone := save.Sect.Res.Stone
	heroAct(t, save, HeroAction{Type: "upgrade", Card: 0})
	assert.True(t, h.Cards[0].Up)
	assert.Equal(t, stone-UpgradeCost[dungeon.RarityStarter], save.Sect.Res.Stone)
	_, err = save.HeroApply(HeroAction{Type: "upgrade", Card: 0}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrAction, "already upgraded")
	save.Sect.Res.Stone = 0
	_, err = save.HeroApply(HeroAction{Type: "upgrade", Card: 1}, 0, "d1", 0)
	assert.ErrorIs(t, err, ErrCost)
}

// 闭关修为按聚灵阵加快，离线最多结算 OfflineCap 秒；宗门的操作也会结算主角的修为。
func TestHeroCultivatesOffline(t *testing.T) {
	save := newHeroSave(t, dungeon.ClassWarrior)
	rate := save.Hero.CultivateRate(save.Sect)
	assert.InDelta(t, heroExpPerHour*(1+arrayCultivate), rate, 1e-9)
	save.TickHero(30 * 3600)
	h := save.Hero
	total := h.Exp
	for l := 1; l < h.Layer; l++ {
		total += ExpNeed(0, l)
	}
	assert.InDelta(t, rate*12, total, 1e-6, "capped at twelve hours")
}

// 每个节点的前置都存在且在它前面，敌人都存在，机缘是突破要用的，奖励的法宝与丹符都认识。
func TestJourneyContentIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	keys := map[string]bool{}
	for _, n := range JourneyNodes {
		assert.False(t, seen[n.Id], "duplicate %s", n.Id)
		for _, r := range n.Requires {
			assert.True(t, seen[r], "%s requires %s which comes later or is unknown", n.Id, r)
		}
		seen[n.Id] = true
		assert.Less(t, n.Chapter, Chapters)
		fights := n.Kind == NodeCombat || n.Kind == NodeElite || n.Kind == NodeBoss
		assert.Equal(t, fights, len(n.Enemies) > 0, n.Id)
		if fights {
			_, err := dungeon.NewBattle(1, dungeon.BattleSetup{Class: dungeon.ClassWarrior, Deck: dungeon.StarterDeck(dungeon.ClassWarrior),
				MaxHP: 80, Kind: n.Kind, Enemies: n.Enemies, HP: 100, Dmg: 100})
			assert.NoError(t, err, n.Id)
		}
		if n.Relic != "" {
			assert.True(t, dungeon.IsRelic(n.Relic), n.Id)
		}
		for _, p := range n.Potions {
			assert.True(t, dungeon.IsPotion(p), n.Id)
		}
		if n.Key != "" {
			keys[n.Key] = true
		}
	}
	for realm := 0; realm < Chapters; realm++ {
		assert.True(t, keys[BreakKeys[realm]], "chapter %d drops the key to break realm %d", realm+1, realm)
	}
}
