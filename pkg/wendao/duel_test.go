package wendao

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	duelWon  = &doufa.Fight{Over: true, Won: true}
	duelLost = &doufa.Fight{Over: true}
)

// 每个地域、每一档难度、每个流派排出来的敌人与房间斗法的规则都认识，入门试炼也一样；难度越高敌人越强，斩首档有首领，
// 讨伐档末一波有精英，地域越远敌人越强。
func TestEveryContractOpensInTheFightRules(t *testing.T) {
	for _, class := range []string{dungeon.ClassWarrior, dungeon.ClassMage, dungeon.ClassRanger} {
		hero, err := NewHero(class, 0)
		require.NoError(t, err)
		hero.Realm = len(Regions) - 1
		book := &DuelBook{Rng: 99}
		for region := range Regions {
			var scales []int
			for tier := 1; tier <= MaxTier; tier++ {
				setup := hero.duelSetup(book.contractSetup(&Contract{Region: region, Tier: tier}))
				_, err := doufa.New(1, setup)
				require.NoError(t, err, "%s region %d tier %d", class, region, tier)
				scales = append(scales, setup.Scale.HP)
				last := setup.Waves[len(setup.Waves)-1]
				switch tier {
				case 1:
					assert.Len(t, setup.Waves, 2)
				case 2:
					assert.Len(t, setup.Waves, 3)
					assert.Equal(t, Regions[region].Elite, last[len(last)-1].Foe)
				case MaxTier:
					assert.Equal(t, []doufa.Group{{Foe: Regions[region].Boss, N: 1}}, last)
				}
			}
			assert.Equal(t, []int{102 + 8*region, 114 + 8*region, 126 + 8*region}, scales)
		}
		for sect := range trialWaves {
			_, err := doufa.New(1, hero.duelSetup(doufa.Setup{Room: Regions[0].Rooms[0], Waves: trialWaves[sect]}))
			require.NoError(t, err, sect)
		}
	}
	for _, sect := range NpcSects {
		assert.Contains(t, trialWaves, sect.Id, "every sect has an entry trial")
	}
}

// 榜单一天一换：悬赏与宗门任务各排好几张，只出在主角够得着的地域；同一天再换不变，换天以后整个换一批、入门试炼的次数清零、
// 昨天没打完的一场作废；存档没写回时再排一遍结果相同(只读的接口可以先在副本上算)。
func TestDuelBoardRollsOncePerDay(t *testing.T) {
	sv := looseSave(t)
	sv.Hero.Realm = 2
	sv.RollDuels("d1")
	require.Len(t, sv.Duels.Bounties, BountySlots)
	require.Len(t, sv.Duels.Tasks, TaskSlots)
	for _, c := range append(append([]*Contract(nil), sv.Duels.Bounties...), sv.Duels.Tasks...) {
		assert.True(t, sv.Hero.RegionOpen(c.Region), "%s is in an open region", c.Id)
		assert.GreaterOrEqual(t, c.Region, 1, "the board favors the farthest regions")
		assert.GreaterOrEqual(t, c.Tier, 1)
		assert.LessOrEqual(t, c.Tier, MaxTier)
		assert.Equal(t, ContractTries, c.Tries)
		size := (c.Region + 1) * c.Tier
		if c.Kind == DuelBounty {
			assert.Equal(t, Cost{Stone: 20 * size, Herb: 8 * size, Ore: 4 * size}, c.Reward)
		} else {
			assert.Equal(t, 30*c.Tier, c.Merit)
		}
	}
	first := append([]*Contract(nil), sv.Duels.Bounties...)
	sv.RollDuels("d1")
	assert.Equal(t, first, sv.Duels.Bounties, "the same day keeps its board")

	other := looseSave(t)
	other.Hero.Realm = 2
	other.RollDuels("d1")
	assert.Equal(t, sv.Duels.Bounties, other.Duels.Bounties, "the roll depends only on what the save already holds")

	sv.Duels.Trials = map[string]int{"jingang": TrialTries}
	sv.Duels.Active = &Duel{Kind: DuelBounty, Id: "b1"}
	sv.RollDuels("d2")
	assert.NotEqual(t, first, sv.Duels.Bounties)
	assert.Empty(t, sv.Duels.Trials)
	assert.Nil(t, sv.Duels.Active)

	fresh := looseSave(t)
	fresh.Hero = nil
	fresh.RollDuels("d1")
	assert.Empty(t, fresh.Duels.Bounties, "no hero, no board")
}

// 接下一张契约就用掉一次挑战，发出种子与开局；一次只能有一场；放弃不退次数；次数用完或做完就不能再接；够不着的地域、
// 不是宗门弟子都接不了宗门任务。
func TestStartingADuelSpendsATry(t *testing.T) {
	sv := looseSave(t)
	sv.RollDuels("d1")
	bounty := sv.Duels.Bounties[0]
	bounty.Region = 0

	duel, err := sv.StartDuel(DuelBounty, bounty.Id, 77, 500)
	require.NoError(t, err)
	assert.Equal(t, uint32(77), duel.Seed)
	assert.Equal(t, int64(500), duel.Issued)
	assert.Equal(t, ContractTries-1, bounty.Tries)
	assert.Equal(t, sv.Hero.Class, duel.Setup.Class)
	assert.Equal(t, doufa.StartingWeapons(sv.Hero.Class), duel.Setup.Weapons)
	_, err = doufa.New(duel.Seed, duel.Setup)
	require.NoError(t, err)
	_, err = sv.StartDuel(DuelBounty, sv.Duels.Bounties[1].Id, 78, 501)
	assert.ErrorIs(t, err, ErrUnavailable, "one fight at a time")

	require.NoError(t, sv.GiveUpDuel())
	assert.Equal(t, ContractTries-1, bounty.Tries, "giving up does not refund the try")
	assert.ErrorIs(t, sv.GiveUpDuel(), ErrUnavailable)
	_, err = sv.StartDuel(DuelBounty, bounty.Id, 79, 502)
	require.NoError(t, err)
	sv.Duels.Active = nil
	_, err = sv.StartDuel(DuelBounty, bounty.Id, 80, 503)
	assert.ErrorIs(t, err, ErrUnavailable, "both tries are used")

	far := sv.Duels.Bounties[1]
	far.Region = 3
	_, err = sv.StartDuel(DuelBounty, far.Id, 81, 504)
	assert.ErrorIs(t, err, ErrUnavailable, "a region the hero cannot reach yet")
	_, err = sv.StartDuel(DuelBounty, "nope", 82, 505)
	assert.ErrorIs(t, err, ErrAction)
	_, err = sv.StartDuel(DuelTask, sv.Duels.Tasks[0].Id, 83, 506)
	assert.ErrorIs(t, err, ErrUnavailable, "sect tasks are for sect disciples")
	_, err = sv.StartDuel("raid", "x", 84, 507)
	assert.ErrorIs(t, err, ErrAction)
}

// 打赢悬赏给资源并算完成，打输什么都没有；任务给贡献与一点灵石，宗门弟子才有；做完的契约不能再接。
func TestFinishingADuelPaysTheContract(t *testing.T) {
	sv := looseSave(t)
	sv.RollDuels("d1")
	bounty := sv.Duels.Bounties[0]
	bounty.Region = 0
	stone := sv.Sect.Res.Stone

	_, err := sv.StartDuel(DuelBounty, bounty.Id, 5, 100)
	require.NoError(t, err)
	evs, err := sv.FinishDuel(duelLost, 200)
	require.NoError(t, err)
	assert.Equal(t, []Ev{{K: "duel_lost", S: DuelBounty}}, evs)
	assert.Equal(t, stone, sv.Sect.Res.Stone)
	assert.False(t, bounty.Done)
	assert.Nil(t, sv.Duels.Active)

	_, err = sv.StartDuel(DuelBounty, bounty.Id, 6, 300)
	require.NoError(t, err)
	_, err = sv.FinishDuel(&doufa.Fight{}, 400)
	assert.ErrorIs(t, err, ErrUnavailable, "a fight that is not over settles nothing")
	evs, err = sv.FinishDuel(duelWon, 400)
	require.NoError(t, err)
	assert.Equal(t, "duel_won", evs[0].K)
	assert.Equal(t, stone+bounty.Reward.Stone, sv.Sect.Res.Stone)
	assert.True(t, bounty.Done)
	_, err = sv.StartDuel(DuelBounty, bounty.Id, 7, 500)
	assert.ErrorIs(t, err, ErrUnavailable, "a finished contract is gone")
	_, err = sv.FinishDuel(duelWon, 600)
	assert.ErrorIs(t, err, ErrUnavailable, "nothing in progress")

	require.NoError(t, sv.JoinSect("jingang", 1000))
	task := sv.Duels.Tasks[0]
	task.Region = 0
	_, err = sv.StartDuel(DuelTask, task.Id, 8, 1100)
	require.NoError(t, err)
	_, err = sv.FinishDuel(duelWon, 1200)
	require.NoError(t, err)
	assert.Equal(t, task.Merit, sv.Hero.Path.Merit)
	assert.Equal(t, task.Reward.Stone, sv.Sect.Res.Stone-stone-bounty.Reward.Stone)
}

// 入门试炼：散修才能打，打赢就拜入那个宗门(退出宗门不满七天还不能)；每个宗门每天最多 TrialTries 次，输了也用掉一次。
func TestEntryTrialGatesJoiningASect(t *testing.T) {
	sv := looseSave(t)
	sv.RollDuels("d1")
	_, err := sv.StartDuel(DuelTrial, "shaolin", 1, 10)
	assert.ErrorIs(t, err, ErrAction)

	for i := 0; i < TrialTries; i++ {
		_, err = sv.StartDuel(DuelTrial, "taixu", uint32(2+i), 20)
		require.NoError(t, err)
		_, err = sv.FinishDuel(duelLost, 30)
		require.NoError(t, err)
	}
	assert.Equal(t, "loose", sv.Identity())
	_, err = sv.StartDuel(DuelTrial, "taixu", 9, 40)
	assert.ErrorIs(t, err, ErrUnavailable, "five tries a day per sect")
	_, err = sv.StartDuel(DuelTrial, "wanhua", 10, 40)
	require.NoError(t, err, "another sect has its own tries")
	evs, err := sv.FinishDuel(duelWon, 50)
	require.NoError(t, err)
	assert.Equal(t, Ev{K: "path_join", S: "wanhua"}, evs[len(evs)-1])
	assert.Equal(t, "disciple", sv.Identity())
	assert.Equal(t, "wanhua", sv.Hero.Path.Sect)
	_, err = sv.StartDuel(DuelTrial, "jingang", 11, 60)
	assert.ErrorIs(t, err, ErrUnavailable, "a disciple does not take another entry trial")

	require.NoError(t, sv.LeaveSect(100))
	sv.RollDuels("d2")
	_, err = sv.StartDuel(DuelTrial, "jingang", 12, 100+PathRejoinWait-1)
	assert.ErrorIs(t, err, ErrUnavailable, "seven days after leaving a sect")
	_, err = sv.StartDuel(DuelTrial, "jingang", 13, 100+PathRejoinWait)
	require.NoError(t, err)
}

// 页面看到的契约：宗门任务只有宗门弟子看得到；进行中的一场只给种类与编号，不带种子与开局；够不着的地域标出来。
func TestDuelViewHidesTheIssuedFight(t *testing.T) {
	sv := looseSave(t)
	sv.RollDuels("d1")
	sv.Duels.Bounties[0].Region = 0
	view := sv.DuelView()
	assert.Empty(t, view.Tasks)
	assert.Equal(t, []bool{true, false, false, false, false}, view.Open)
	assert.Nil(t, view.Active)
	_, err := sv.StartDuel(DuelBounty, sv.Duels.Bounties[0].Id, 31, 10)
	require.NoError(t, err)
	assert.Equal(t, &ActiveDuel{Kind: DuelBounty, Id: "b1", Token: sv.Duels.Active.Token}, sv.DuelView().Active)
	require.NoError(t, sv.JoinSect("jingang", 20))
	assert.Len(t, sv.DuelView().Tasks, TaskSlots)

	empty := &Save{}
	assert.Nil(t, empty.DuelView())
}
