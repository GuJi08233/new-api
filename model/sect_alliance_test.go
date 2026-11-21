package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAllianceBoss 是测试用的周 Boss：每名成员 100 血，击杀奖励 10 灵石、1 声望、1 颗丹。
var testAllianceBoss = SectAllianceBoss{
	Hp:   func(level int, members int) int64 { return int64(100 * level * members) },
	Kill: func(level int) (int, int, int) { return 10 * level, level, 1 },
	Kind: func(week string) string {
		if week == "2026-W45" {
			return "blood_fury"
		}
		return "iron_shell"
	},
}

// 仙盟的写入要么全成要么全不成：建盟时宗门付不起就没有仙盟；满员不能再进；捐献、讨伐与领奖和宗门存档在同一个事务里；击杀时
// 本周出过力的成员都记一份击杀奖励；盟主退盟由贡献最高的成员接任，最后一人离开时解散。
func TestSectAllianceTransactions(t *testing.T) {
	truncateTables(t)
	const leader, member, late = 3101, 3102, 3103
	for _, id := range []int{leader, member, late} {
		seedGameUser(t, id, 0)
	}
	week, day := "2026-W44", "2026-10-31"
	broke := errors.New("cannot pay")
	_, err := CreateSectAlliance(leader, "青云盟", week, testAllianceBoss, func(string) (string, SectAllianceCarry, error) {
		return "", SectAllianceCarry{}, broke
	})
	assert.ErrorIs(t, err, broke)
	var count int64
	require.NoError(t, DB.Model(&SectAlliance{}).Count(&count).Error)
	assert.Zero(t, count, "a failed payment leaves no alliance behind")

	alliance, err := CreateSectAlliance(leader, "青云盟", week, testAllianceBoss, func(string) (string, SectAllianceCarry, error) {
		return "paid", SectAllianceCarry{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "iron_shell", alliance.BossKind)
	_, err = CreateSectAlliance(member, "青云盟", week, testAllianceBoss, func(string) (string, SectAllianceCarry, error) {
		return "paid", SectAllianceCarry{}, nil
	})
	assert.ErrorIs(t, err, ErrSectAllianceName)
	require.NoError(t, JoinSectAlliance(member, alliance.Id, week, testAllianceBoss, func(int) int { return 2 }, nil))
	assert.ErrorIs(t, JoinSectAlliance(late, alliance.Id, week, testAllianceBoss, func(int) int { return 2 }, nil), ErrSectAllianceFull)
	assert.ErrorIs(t, JoinSectAlliance(member, alliance.Id, week, testAllianceBoss, func(int) int { return 9 }, nil), ErrSectAllianceJoined)

	_, err = DonateSectAlliance(member, func(string) (string, int64, error) { return "donated", 50, nil })
	require.NoError(t, err)
	_, err = UpgradeSectAlliance(member, func(int) int64 { return 10 })
	assert.ErrorIs(t, err, ErrSectAllianceNotLeader)
	upgraded, err := UpgradeSectAlliance(leader, func(int) int64 { return 40 })
	require.NoError(t, err)
	assert.Equal(t, 2, upgraded.Level)
	assert.Equal(t, int64(10), upgraded.Funds)

	// 建盟时一个人，周 Boss 是 100 血：成员打 60，盟主打满补刀。
	reward := func(damage int64) (int, int) { return int(damage), 1 }
	planHit := func(dmg int64) func(string, SectAllianceAttackCtx) (SectAllianceAttackPlan, error) {
		return func(_ string, ctx SectAllianceAttackCtx) (SectAllianceAttackPlan, error) {
			if ctx.AlreadyAttacked {
				return SectAllianceAttackPlan{}, ErrSectAllianceAttacked
			}
			return SectAllianceAttackPlan{SaveData: "hit", BaseDamage: dmg, Damage: dmg}, nil
		}
	}
	hit, err := AttackSectAllianceBoss(member, day, week, testAllianceBoss, planHit(60), reward, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(60), hit.Damage)
	assert.False(t, hit.Killed)
	_, err = AttackSectAllianceBoss(member, day, week, testAllianceBoss, planHit(60), reward, nil)
	assert.ErrorIs(t, err, ErrSectAllianceAttacked)
	hit, err = AttackSectAllianceBoss(leader, day, week, testAllianceBoss, planHit(500), reward, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(40), hit.Damage, "damage is capped at the remaining HP")
	assert.True(t, hit.Killed)

	_, row, err := GetSectAllianceOf(member)
	require.NoError(t, err)
	assert.Equal(t, []int{60 + 10, 1 + 1, 1}, []int{row.PendingStone, row.PendingRep, row.PendingPills})
	var claimed []int
	require.NoError(t, ClaimSectAllianceRewards(member, func(data string, m *SectAllianceMember) (string, error) {
		if m.PendingStone == 0 && m.PendingRep == 0 && m.PendingPills == 0 {
			return "", ErrSectAllianceNothing
		}
		claimed = []int{m.PendingStone, m.PendingRep, m.PendingPills}
		return "claimed", nil
	}))
	assert.Equal(t, []int{70, 2, 1}, claimed)
	assert.ErrorIs(t, ClaimSectAllianceRewards(member, func(_ string, m *SectAllianceMember) (string, error) {
		if m.PendingStone == 0 && m.PendingRep == 0 && m.PendingPills == 0 {
			return "", ErrSectAllianceNothing
		}
		return "", nil
	}), ErrSectAllianceNothing)
	save, err := GetGameSave(member, sectGameKey)
	require.NoError(t, err)
	assert.Equal(t, "claimed", save)

	assert.ErrorIs(t, LeaveSectAlliance(member, leader, nil), ErrSectAllianceNotLeader)
	require.NoError(t, LeaveSectAlliance(leader, leader, nil))
	current, row, err := GetSectAllianceOf(member)
	require.NoError(t, err)
	assert.Equal(t, member, current.LeaderId, "the top contributor takes over")
	assert.Equal(t, SectAllianceRoleLeader, row.Role)
	assert.Equal(t, 1, current.Members)
	require.NoError(t, LeaveSectAlliance(member, member, nil))
	require.NoError(t, DB.Model(&SectAlliance{}).Count(&count).Error)
	assert.Zero(t, count, "the last member leaving dissolves the alliance")
}

// 换周时周 Boss 回满血，上周击杀了就升一级，并重置全盟阶段位图与快照新一周机制。
func TestSectAllianceBossRollsOverWeekly(t *testing.T) {
	alliance := &SectAlliance{Members: 3, BossLevel: 1}
	alliance.rollWeek("2026-W44", testAllianceBoss)
	assert.Equal(t, int64(300), alliance.BossHp)
	assert.Equal(t, "iron_shell", alliance.BossKind)
	alliance.BossHp = 0
	alliance.BossStageMask = 7
	alliance.rollWeek("2026-W44", testAllianceBoss)
	assert.Zero(t, alliance.BossHp, "the same week keeps a defeated boss down")
	assert.Equal(t, 7, alliance.BossStageMask)
	alliance.rollWeek("2026-W45", testAllianceBoss)
	assert.Equal(t, 2, alliance.BossLevel)
	assert.Equal(t, int64(600), alliance.BossMaxHp)
	assert.Equal(t, "blood_fury", alliance.BossKind)
	assert.Zero(t, alliance.BossStageMask)
	alliance.BossHp = 10
	alliance.rollWeek("2026-W46", testAllianceBoss)
	assert.Equal(t, 2, alliance.BossLevel, "a boss that survived the week keeps its level")
	assert.Zero(t, alliance.BossStageMask)
}

// 全盟协作阶段(30%/60%/100%)、个人周目标(1/3/5 次)、跨盟资格延续防重与击败后补刀拒收。
func TestSectAllianceMilestonesPersonalAndCrossAllianceGuard(t *testing.T) {
	truncateTables(t)
	const alice, bob, carol = 3201, 3202, 3203
	for _, id := range []int{alice, bob, carol} {
		seedGameUser(t, id, 0)
	}
	fullBoss := SectAllianceBoss{
		Hp:   func(level int, members int) int64 { return 1000 },
		Kind: func(week string) string { return "spirit_tide" },
		StageMask: func(hp, maxHp int64) int {
			dealt := maxHp - max(hp, int64(0))
			mask := 0
			if dealt*100 >= 30*maxHp {
				mask |= 1 << 0
			}
			if dealt*100 >= 60*maxHp {
				mask |= 1 << 1
			}
			if hp <= 0 {
				mask |= 1 << 2
			}
			return mask
		},
		StageReward: func(level, bit int) (int, int, int) {
			switch bit {
			case 1 << 0:
				return 120 * level, 12 * level, 0
			case 1 << 1:
				return 200 * level, 20 * level, 0
			case 1 << 2:
				return 400 * level, 40 * level, 1
			default:
				return 0, 0, 0
			}
		},
		PersonalMask: func(attacks int) int {
			mask := 0
			if attacks >= 1 {
				mask |= 1 << 0
			}
			if attacks >= 3 {
				mask |= 1 << 1
			}
			if attacks >= 5 {
				mask |= 1 << 2
			}
			return mask
		},
		PersonalReward: func(level, bit int) (int, int, int) {
			switch bit {
			case 1 << 0:
				return 100 * level, 10 * level, 0
			case 1 << 1:
				return 200 * level, 20 * level, 0
			case 1 << 2:
				return 350 * level, 35 * level, 1
			default:
				return 0, 0, 0
			}
		},
	}
	week := "2026-W44"
	reward := func(damage int64) (int, int) { return int(damage / 10), 1 }
	planHit := func(dmg int64, seenPrior *int) func(string, SectAllianceAttackCtx) (SectAllianceAttackPlan, error) {
		return func(_ string, ctx SectAllianceAttackCtx) (SectAllianceAttackPlan, error) {
			if seenPrior != nil {
				*seenPrior = ctx.PriorAttackers
			}
			if ctx.AlreadyAttacked {
				return SectAllianceAttackPlan{}, ErrSectAllianceAttacked
			}
			return SectAllianceAttackPlan{SaveData: "ok", BaseDamage: dmg, Damage: dmg}, nil
		}
	}

	allianceA, err := CreateSectAlliance(alice, "青岚盟", week, fullBoss, func(string) (string, SectAllianceCarry, error) {
		return "ok", SectAllianceCarry{}, nil
	})
	require.NoError(t, err)
	require.NoError(t, JoinSectAlliance(bob, allianceA.Id, week, fullBoss, func(int) int { return 10 }, nil))

	// Alice 打掉 350（35%），触发个人 1 次目标与全盟 30% 阶段；此时 priorAttackers=0。
	prior := -1
	hitA, err := AttackSectAllianceBoss(alice, "2026-10-26", week, fullBoss, planHit(350, &prior), reward, nil)
	require.NoError(t, err)
	assert.Zero(t, prior)
	assert.Equal(t, []int{1}, hitA.PersonalGained)
	assert.Equal(t, []int{1}, hitA.StagesGained)

	// Bob 紧接着出手打掉 300（累计 65%）：priorAttackers=1，Bob 拿到个人 1 次 + 全盟 30% 与 60%；Alice 同步获得全盟 60%。
	hitB, err := AttackSectAllianceBoss(bob, "2026-10-26", week, fullBoss, planHit(300, &prior), reward, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, prior)
	assert.Equal(t, []int{1}, hitB.PersonalGained)
	assert.Equal(t, []int{1, 2}, hitB.StagesGained)
	_, aliceRow, err := GetSectAllianceOf(alice)
	require.NoError(t, err)
	assert.Equal(t, (1<<0)|(1<<1), aliceRow.WeekStageMask)
	assert.Equal(t, 35+100+120+200, aliceRow.PendingStone)

	// Alice 带着本周已获得的资格（day + attacks=1 + stageMask=0b11 + personalMask=0b1）退盟并转投另一个仙盟 B。
	var carried SectAllianceCarry
	require.NoError(t, LeaveSectAlliance(alice, alice, func(data string, m *SectAllianceMember) (string, error) {
		carried = SectAllianceCarry{AttackDay: m.AttackDay, Attacks: m.WeekAttacks, StageMask: m.WeekStageMask, PersonalMask: m.WeekPersonalMask}
		return data, nil
	}))
	allianceB, err := CreateSectAlliance(carol, "赤霞盟", week, fullBoss, func(string) (string, SectAllianceCarry, error) {
		return "ok", SectAllianceCarry{}, nil
	})
	require.NoError(t, err)
	require.NoError(t, JoinSectAlliance(alice, allianceB.Id, week, fullBoss, func(int) int { return 10 },
		func(string) (SectAllianceCarry, error) { return carried, nil }))

	// 当天换盟后不能再讨伐。
	_, err = AttackSectAllianceBoss(alice, "2026-10-26", week, fullBoss, planHit(400, nil), reward, nil)
	assert.ErrorIs(t, err, ErrSectAllianceAttacked)

	// 次日在仙盟 B 讨伐击杀（打 1000 血）：不再重复发个人 1 次、全盟 30% 和全盟 60%，只发新达成的击杀阶段(3)！
	hitA2, err := AttackSectAllianceBoss(alice, "2026-10-27", week, fullBoss, planHit(1000, nil), reward, nil)
	require.NoError(t, err)
	assert.True(t, hitA2.Killed)
	assert.Empty(t, hitA2.PersonalGained)
	assert.Equal(t, []int{3}, hitA2.StagesGained)
	assert.Equal(t, 100+400, hitA2.RewardStone)
	assert.Equal(t, 1, hitA2.RewardPills)

	// 已经被击杀后，同盟 Carol 再补刀必须被拒且 BossKills 保持 1。
	_, err = AttackSectAllianceBoss(carol, "2026-10-27", week, fullBoss, planHit(100, nil), reward, nil)
	assert.ErrorIs(t, err, ErrSectAllianceAttacked)
	afterB, _, err := GetSectAllianceOf(carol)
	require.NoError(t, err)
	assert.Equal(t, 1, afterB.BossKills)

	// 低活跃单人盟：Bob 在次周连续 5 天独自讨伐，即使没击杀妖王也能拿满 1/3/5 次三档个人目标含破境丹。
	nextWeek := "2026-W45"
	for i, d := range []string{"2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06"} {
		hit, err := AttackSectAllianceBoss(bob, d, nextWeek, fullBoss, planHit(50, nil), reward, nil)
		require.NoError(t, err)
		assert.False(t, hit.Killed)
		switch i + 1 {
		case 1:
			assert.Equal(t, []int{1}, hit.PersonalGained)
		case 3:
			assert.Equal(t, []int{2}, hit.PersonalGained)
		case 5:
			assert.Equal(t, []int{3}, hit.PersonalGained)
			assert.Equal(t, 1, hit.RewardPills)
		default:
			assert.Empty(t, hit.PersonalGained)
		}
	}
}
