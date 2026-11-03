package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func looseSave(t *testing.T) *Save {
	t.Helper()
	hero, err := NewHero(dungeon.ClassWarrior, 1000)
	require.NoError(t, err)
	return &Save{Sect: NewStorehouse(7, 1000), Hero: hero, Rift: &doufa.Meta{}}
}

// 新主角是散修：储物处里有些灵石灵草，没有主殿、弟子与灵田，宗门经营的操作都做不了，每日功课只抽不用宗门的。
func TestNewHeroStartsLoose(t *testing.T) {
	sv := looseSave(t)
	assert.Equal(t, "loose", sv.Identity())
	assert.False(t, sv.Sect.IsFounded())
	assert.Empty(t, sv.Sect.Disciples)
	assert.Empty(t, sv.Sect.Garden.Plots)
	assert.Equal(t, Cost{Stone: 200, Herb: 100}, sv.Sect.Res)
	_, err := sv.Sect.Apply(Action{Type: "upgrade", Building: BuildingHall}, 2000)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = sv.Sect.Apply(Action{Type: "sync"}, 2000)
	require.NoError(t, err)
	// 储物处比主角先有时抽不到历练，只有两件；同一天有了主角再补上历练。三种都抽到了就没有别的可换。
	sv.Sect.RollDaily("2026-11-03", "2026-11-02", false)
	assert.Len(t, sv.Sect.Daily.Tasks, 2)
	sv.Sect.RollDaily("2026-11-03", "2026-11-02", true)
	keys := []string{}
	for _, task := range sv.Sect.Daily.Tasks {
		keys = append(keys, task.Key)
	}
	assert.ElementsMatch(t, []string{TaskJourney, TaskAbyss, TaskWater}, keys)
	assert.Zero(t, sv.Sect.View(2000).Daily.RerollsLeft)
	steps := []string{}
	for _, g := range sv.guide() {
		steps = append(steps, g.Key)
	}
	assert.Equal(t, []string{"hero", "win", "path", "break", "abyss"}, steps)
}

// 拜入宗门以后有门派加成；俸禄一天一次；上交资源按比例换贡献，零头不收；贡献与境界够了自动晋升；宝库按身份换东西。
func TestDiscipleLife(t *testing.T) {
	sv := looseSave(t)
	baseHP, baseStrength := sv.Hero.MaxHP(), sv.Hero.Strength()
	assert.ErrorIs(t, sv.JoinSect("shaolin", 1000), ErrAction)
	require.NoError(t, sv.JoinSect("jingang", 1000))
	assert.Equal(t, "disciple", sv.Identity())
	assert.Equal(t, baseHP+perkHP, sv.Hero.MaxHP(), "Jingang Temple adds HP")
	assert.Equal(t, baseStrength, sv.Hero.Strength())
	assert.ErrorIs(t, sv.JoinSect("taixu", 1000), ErrUnavailable, "one sect at a time")

	evs, err := sv.ClaimStipend("2026-11-03")
	require.NoError(t, err)
	assert.Equal(t, "path_stipend", evs[0].K)
	assert.Equal(t, 200+RankStipend[RankOuter].Stone, sv.Sect.Res.Stone)
	_, err = sv.ClaimStipend("2026-11-03")
	assert.ErrorIs(t, err, ErrUnavailable, "once a day")

	_, err = sv.Donate(Cost{Stone: 59, Herb: 9})
	require.NoError(t, err)
	assert.Equal(t, 2, sv.Hero.Path.Merit, "59 stones make 2 merit; 9 herbs make none")
	assert.Equal(t, 200+RankStipend[RankOuter].Stone-40, sv.Sect.Res.Stone, "the leftover stones stay")
	assert.Equal(t, 100, sv.Sect.Res.Herb)
	_, err = sv.Donate(Cost{Herb: 5})
	assert.ErrorIs(t, err, ErrAction)

	sv.GrantMerit(RankMerit[RankInner])
	assert.Equal(t, RankOuter, sv.Hero.Path.Rank, "the inner rank also needs Foundation")
	sv.Hero.Realm = 1
	sv.GrantMerit(1)
	assert.Equal(t, RankInner, sv.Hero.Path.Rank)

	merit := sv.Hero.Path.Merit
	_, err = sv.Exchange("pill_" + PillMarrow)
	assert.ErrorIs(t, err, ErrUnavailable, "the marrow pill needs the heir rank")
	_, err = sv.Exchange("pill_" + PillBreak)
	require.NoError(t, err)
	assert.Equal(t, 1, sv.Sect.Pills[PillBreak])
	assert.Equal(t, merit-150, sv.Hero.Path.Merit)
	assert.Equal(t, RankInner, sv.Hero.Path.Rank, "spending merit keeps the rank")
	_, err = sv.Exchange("pill_unknown")
	assert.ErrorIs(t, err, ErrAction)
}

// 门派加成：天剑宗力量 +1，太虚观闭关更快，万花谷炼丹品质更高。
func TestSectPerks(t *testing.T) {
	sv := looseSave(t)
	strength, rate := sv.Hero.Strength(), sv.Hero.CultivateRate(sv.Sect)
	brew := &Brew{Recipe: ElixirTemper, Fire: "medium"}
	quality := sv.Hero.QualityScore(brew)
	require.NoError(t, sv.JoinSect("tianjian", 1000))
	assert.Equal(t, strength+1, sv.Hero.Strength())
	sv.Hero.Path.Sect = "taixu"
	assert.InDelta(t, rate*(1+perkInsight), sv.Hero.CultivateRate(sv.Sect), 1e-9)
	assert.Contains(t, sv.Hero.Sheet(sv.Sect).Rate, StatPart{Key: "sect", N: 20}, "the stat sheet explains the faster pace")
	sv.Hero.Path.Sect = "wanhua"
	assert.Equal(t, quality+perkAlchemy, sv.Hero.QualityScore(brew))
}

// 退出宗门身份与贡献清零，七天内不能再拜入；过了七天可以拜入别家。
func TestLeaveSectWaitsAWeek(t *testing.T) {
	sv := looseSave(t)
	require.NoError(t, sv.JoinSect("taixu", 1000))
	sv.GrantMerit(500)
	require.NoError(t, sv.LeaveSect(5000))
	assert.Equal(t, "loose", sv.Identity())
	assert.Zero(t, sv.Hero.Path.Merit)
	assert.ErrorIs(t, sv.LeaveSect(5000), ErrUnavailable)
	assert.ErrorIs(t, sv.JoinSect("wanhua", 5000+PathRejoinWait-1), ErrUnavailable)
	require.NoError(t, sv.JoinSect("wanhua", 5000+PathRejoinWait))
	assert.Equal(t, int64(5000), sv.Hero.Path.Left)
}

// 开宗立派：要散修、金丹、灵石够；开宗以后有主殿、灵田、聚灵阵与两名弟子，宗门经营都能做了，身份是掌门。
func TestFoundSect(t *testing.T) {
	sv := looseSave(t)
	_, err := sv.FoundSect(2000)
	assert.ErrorIs(t, err, ErrUnavailable, "needs the Golden Core realm")
	sv.Hero.Realm = FoundRealm
	_, err = sv.FoundSect(2000)
	assert.ErrorIs(t, err, ErrCost, "needs the stones")
	sv.Sect.Res.Stone = FoundCost + 50
	require.NoError(t, sv.JoinSect("tianjian", 2000))
	_, err = sv.FoundSect(2000)
	assert.ErrorIs(t, err, ErrUnavailable, "a disciple cannot found a sect")
	require.NoError(t, sv.LeaveSect(2000))

	evs, err := sv.FoundSect(3000)
	require.NoError(t, err)
	assert.Equal(t, "path_found", evs[len(evs)-1].K)
	assert.Equal(t, []Ev{{K: "path_found", At: 3000}}, sv.Sect.Log, "the chronicle starts with the founding")
	assert.Equal(t, "master", sv.Identity())
	assert.True(t, sv.Sect.IsFounded())
	assert.Equal(t, 50, sv.Sect.Res.Stone)
	assert.Len(t, sv.Sect.Disciples, 2)
	assert.Len(t, sv.Sect.Garden.Plots, PlotCount(1))
	_, err = sv.Sect.Apply(Action{Type: "sync"}, 3100)
	require.NoError(t, err)
	_, err = sv.FoundSect(3200)
	assert.ErrorIs(t, err, ErrUnavailable, "only once")
	assert.ErrorIs(t, sv.JoinSect("taixu", 3200+PathRejoinWait), ErrUnavailable, "a master does not join another sect")
}

// 散修没有宗门丹房，洞府的丹室也能炼战斗用的丹符：丹室没开辟时炼不了，开辟以后按丹室的等级解锁丹方。
func TestLooseCultivatorCraftsPotionsInTheCave(t *testing.T) {
	sv := looseSave(t)
	_, err := sv.Hero.craft(sv.Sect, dungeon.PotionHeal, 1)
	assert.ErrorIs(t, err, ErrUnavailable)
	sv.Hero.Cave[RoomFurnace] = 1
	_, err = sv.Hero.craft(sv.Sect, dungeon.PotionHeal, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, sv.Hero.Potions[dungeon.PotionHeal])
	_, err = sv.Hero.craft(sv.Sect, dungeon.PotionIron, 1)
	assert.ErrorIs(t, err, ErrUnavailable, "the iron potion needs a level-2 furnace")
}

// 拜入和退出太虚观在身份变化前结清自然修为，入门试炼直接调用规则时也不能追溯获得门派加成。
func TestSectIdentityChangesSettleCultivationBeforeThePerkChanges(t *testing.T) {
	for _, meditate := range []bool{false, true} {
		name := "ordinary"
		if meditate {
			name = "meditating"
		}
		t.Run(name, func(t *testing.T) {
			save := looseSave(t)
			if meditate {
				require.NoError(t, save.StartMeditation(0, 1000))
			}
			require.NoError(t, save.JoinSect("taixu", 2800))
			beforeJoin, beforeLeave, afterLeave := 25.0, 55.0, 80.0
			if meditate {
				beforeJoin, beforeLeave, afterLeave = 41.25, 90.75, 132
			}
			assert.InDelta(t, beforeJoin, save.Hero.Exp, 1e-9)
			require.NoError(t, save.LeaveSect(4600))
			assert.InDelta(t, beforeLeave, save.Hero.Exp, 1e-9)
			save.TickHero(6400)
			if meditate {
				assert.Equal(t, 2, save.Hero.Layer)
				assert.InDelta(t, afterLeave-100, save.Hero.Exp, 1e-9)
			} else {
				assert.Equal(t, 1, save.Hero.Layer)
				assert.InDelta(t, afterLeave, save.Hero.Exp, 1e-9)
			}
		})
	}
}

// 开宗会建聚灵阵，开宗前的闭关或灵脉打坐不能带新建筑的加成。
func TestFoundingSettlesCultivationBeforeBuildingTheArray(t *testing.T) {
	for _, meditate := range []bool{false, true} {
		name := "ordinary"
		if meditate {
			name = "meditating"
		}
		t.Run(name, func(t *testing.T) {
			save := looseSave(t)
			save.Hero.Realm = FoundRealm
			save.Sect.Res.Stone = FoundCost
			if meditate {
				require.NoError(t, save.StartMeditation(0, 1000))
			}
			_, err := save.FoundSect(2800)
			require.NoError(t, err)
			before, after := 100.0, 206.0
			if meditate {
				before, after = 165, 339.9
			}
			assert.InDelta(t, before, save.Hero.Exp, 1e-9)
			save.TickHero(4600)
			assert.InDelta(t, after, save.Hero.Exp, 1e-9)
		})
	}
}
