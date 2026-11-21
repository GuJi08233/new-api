package wendao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 仙盟规则：人数上限与升级盟资随等级增加，满级不能再升；玄铁一份折三点盟资，声望不能捐；周 Boss 血量随等级与人数增长。
func TestAllianceRules(t *testing.T) {
	assert.Equal(t, 10, AllianceCap(1))
	assert.Equal(t, 30, AllianceCap(AllianceMaxLevel))
	assert.Equal(t, int64(3000), AllianceUpgradeFunds(1))
	assert.Zero(t, AllianceUpgradeFunds(AllianceMaxLevel))
	assert.Equal(t, int64(100+3*10), DonationFunds(Cost{Stone: 100, Ore: 10}))
	assert.Equal(t, int64(-1), DonationFunds(Cost{Rep: 1}))
	assert.Equal(t, int64(-1), DonationFunds(Cost{Stone: -5}))
	assert.Equal(t, int64(1200), AllianceBossHp(1, 1))
	assert.Equal(t, int64(1920*3), AllianceBossHp(2, 3))

	s := NewSect(21, 0)
	assert.ErrorIs(t, s.FoundAlliance(), ErrUnavailable, "main hall level 2 is required")
	s.Buildings[BuildingHall] = AllianceMinHall
	assert.ErrorIs(t, s.FoundAlliance(), ErrCost)
	s.Res = Cost{Stone: 1000, Rep: 50}
	require.NoError(t, s.FoundAlliance())
	assert.Equal(t, Cost{}, s.Res)

	s.Res = Cost{Stone: 10}
	_, err := s.Donate(Cost{Stone: 11})
	assert.ErrorIs(t, err, ErrCost)
	_, err = s.Donate(Cost{})
	assert.ErrorIs(t, err, ErrAction)

	damage, err := s.AllianceAttack([]int{s.Disciples[0].Id}, 0)
	require.NoError(t, err)
	hp := s.Power(s.Disciples[0]) * hpPerPower
	assert.InDelta(t, hp, damage, float64(hp)/10+1)
	s.Disciples[0].Injured = hour
	_, err = s.AllianceAttack([]int{s.Disciples[0].Id}, 0)
	assert.ErrorIs(t, err, ErrUnavailable, "injured disciples cannot fight the boss")
}

// 周首领三套机制轮换、阵容/血量/同盟人数增伤、全盟三档与个人三档目标，以及跨盟周资格同步。
func TestAllianceBossMechanicsAndGoals(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range []string{"2026-W44", "2026-W45", "2026-W46"} {
		seen[AllianceBossKindForWeek(w)] = true
	}
	assert.Len(t, seen, 3, "three consecutive weeks rotate through all three boss mechanics")
	assert.Equal(t, AllianceBossIronShell, AllianceBossKindForWeek("bad-week"))

	assert.Equal(t, 0, AllianceBossBonusPct(AllianceBossIronShell, 2, 2, 1000, 1000, 0))
	assert.Equal(t, 20, AllianceBossBonusPct(AllianceBossIronShell, 3, 1, 1000, 1000, 0))
	assert.Equal(t, 0, AllianceBossBonusPct(AllianceBossBloodFury, 3, 2, 500, 1000, 0))
	assert.Equal(t, 25, AllianceBossBonusPct(AllianceBossBloodFury, 1, 1, 499, 1000, 0))
	assert.Equal(t, 10, AllianceBossBonusPct(AllianceBossSpiritTide, 2, 2, 1000, 1000, 0))
	assert.Equal(t, 40, AllianceBossBonusPct(AllianceBossSpiritTide, 3, 2, 1000, 1000, 5), "peer bonus caps at 3 peers plus element diversity")

	assert.Equal(t, 0, AllianceStageReachedMask(710, 1000))
	assert.Equal(t, 1<<0, AllianceStageReachedMask(700, 1000))
	assert.Equal(t, (1<<0)|(1<<1), AllianceStageReachedMask(400, 1000))
	assert.Equal(t, (1<<0)|(1<<1)|(1<<2), AllianceStageReachedMask(0, 1000))
	assert.Equal(t, 0, AlliancePersonalReachedMask(0))
	assert.Equal(t, 1<<0, AlliancePersonalReachedMask(1))
	assert.Equal(t, (1<<0)|(1<<1), AlliancePersonalReachedMask(3))
	assert.Equal(t, (1<<0)|(1<<1)|(1<<2), AlliancePersonalReachedMask(5))

	cost1, pills1 := AllianceStageReward(2, 1<<0)
	assert.Equal(t, Cost{Stone: 240, Rep: 24}, cost1)
	assert.Zero(t, pills1)
	cost3, pills3 := AllianceStageReward(2, 1<<2)
	assert.Equal(t, AllianceKillReward(2), cost3)
	assert.Equal(t, AllianceKillPills, pills3)
	pCost3, pPills3 := AlliancePersonalReward(2, 1<<2)
	assert.Equal(t, Cost{Stone: 700, Rep: 70}, pCost3)
	assert.Equal(t, 1, pPills3)

	s := NewSect(42, 0)
	// 凑齐三名不同五行的弟子检验破甲与灵潮加成。
	c := s.newCandidate()
	c.Element = (s.Disciples[0].Element + 1) % Elements
	s.addDisciple(c, 0)
	team := []int{s.Disciples[0].Id, s.Disciples[1].Id, s.Disciples[2].Id}
	s.Disciples[0].Element, s.Disciples[1].Element, s.Disciples[2].Element = 0, 1, 2
	calc, err := s.AllianceAttackWithBoss(team, 0, AllianceBossContext{Kind: AllianceBossIronShell, Hp: 1000, MaxHp: 1000})
	require.NoError(t, err)
	assert.Equal(t, 20, calc.BonusPct)
	assert.Equal(t, calc.BaseDamage*120/100, calc.Damage)

	calc, err = s.AllianceAttackWithBoss(team[:2], 0, AllianceBossContext{Kind: AllianceBossSpiritTide, Hp: 1000, MaxHp: 1000, PriorAttackers: 2})
	require.NoError(t, err)
	assert.Equal(t, 30, calc.BonusPct)
	assert.Equal(t, calc.BaseDamage*130/100, calc.Damage)

	// 跨盟延续：同周合并位图，跨周清零周次数与位图但保留当日讨伐日期。
	s.SyncAllianceBoss("2026-W44", "2026-10-31", 2, 1<<0, 1<<0)
	s.SyncAllianceBoss("2026-W44", "2026-10-31", 1, 1<<1, 1<<1)
	day, attacks, stageMask, personalMask := s.AllianceBossState("2026-W44")
	assert.Equal(t, "2026-10-31", day)
	assert.Equal(t, 2, attacks)
	assert.Equal(t, (1<<0)|(1<<1), stageMask)
	assert.Equal(t, (1<<0)|(1<<1), personalMask)
	day, attacks, stageMask, personalMask = s.AllianceBossState("2026-W45")
	assert.Equal(t, "2026-10-31", day)
	assert.Zero(t, attacks)
	assert.Zero(t, stageMask)
	assert.Zero(t, personalMask)
}
