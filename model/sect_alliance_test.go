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
	_, err := CreateSectAlliance(leader, "青云盟", week, testAllianceBoss, func(string) (string, error) { return "", broke })
	assert.ErrorIs(t, err, broke)
	var count int64
	require.NoError(t, DB.Model(&SectAlliance{}).Count(&count).Error)
	assert.Zero(t, count, "a failed payment leaves no alliance behind")

	alliance, err := CreateSectAlliance(leader, "青云盟", week, testAllianceBoss, func(string) (string, error) { return "paid", nil })
	require.NoError(t, err)
	_, err = CreateSectAlliance(member, "青云盟", week, testAllianceBoss, func(string) (string, error) { return "paid", nil })
	assert.ErrorIs(t, err, ErrSectAllianceName)
	require.NoError(t, JoinSectAlliance(member, alliance.Id, week, testAllianceBoss, func(int) int { return 2 }))
	assert.ErrorIs(t, JoinSectAlliance(late, alliance.Id, week, testAllianceBoss, func(int) int { return 2 }), ErrSectAllianceFull)
	assert.ErrorIs(t, JoinSectAlliance(member, alliance.Id, week, testAllianceBoss, func(int) int { return 9 }), ErrSectAllianceJoined)

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
	hit, err := AttackSectAllianceBoss(member, day, week, testAllianceBoss, func(string) (string, int64, error) { return "hit", 60, nil }, reward)
	require.NoError(t, err)
	assert.Equal(t, SectAllianceAttack{Damage: 60}, *hit)
	_, err = AttackSectAllianceBoss(member, day, week, testAllianceBoss, func(string) (string, int64, error) { return "hit", 60, nil }, reward)
	assert.ErrorIs(t, err, ErrSectAllianceAttacked)
	hit, err = AttackSectAllianceBoss(leader, day, week, testAllianceBoss, func(string) (string, int64, error) { return "hit", 500, nil }, reward)
	require.NoError(t, err)
	assert.Equal(t, SectAllianceAttack{Damage: 40, Killed: true}, *hit, "damage is capped at the remaining HP")

	_, row, err := GetSectAllianceOf(member)
	require.NoError(t, err)
	assert.Equal(t, []int{60 + 10, 1 + 1, 1}, []int{row.PendingStone, row.PendingRep, row.PendingPills})
	var claimed []int
	require.NoError(t, ClaimSectAllianceRewards(member, func(data string, stone, rep, pills int) (string, error) {
		claimed = []int{stone, rep, pills}
		return "claimed", nil
	}))
	assert.Equal(t, []int{70, 2, 1}, claimed)
	assert.ErrorIs(t, ClaimSectAllianceRewards(member, func(string, int, int, int) (string, error) { return "", nil }), ErrSectAllianceNothing)
	save, err := GetGameSave(member, sectGameKey)
	require.NoError(t, err)
	assert.Equal(t, "claimed", save)

	assert.ErrorIs(t, LeaveSectAlliance(member, leader), ErrSectAllianceNotLeader)
	require.NoError(t, LeaveSectAlliance(leader, leader))
	current, row, err := GetSectAllianceOf(member)
	require.NoError(t, err)
	assert.Equal(t, member, current.LeaderId, "the top contributor takes over")
	assert.Equal(t, SectAllianceRoleLeader, row.Role)
	assert.Equal(t, 1, current.Members)
	require.NoError(t, LeaveSectAlliance(member, member))
	require.NoError(t, DB.Model(&SectAlliance{}).Count(&count).Error)
	assert.Zero(t, count, "the last member leaving dissolves the alliance")
}

// 换周时周 Boss 回满血，上周击杀了就升一级。
func TestSectAllianceBossRollsOverWeekly(t *testing.T) {
	alliance := &SectAlliance{Members: 3, BossLevel: 1}
	alliance.rollWeek("2026-W44", testAllianceBoss)
	assert.Equal(t, int64(300), alliance.BossHp)
	alliance.BossHp = 0
	alliance.rollWeek("2026-W44", testAllianceBoss)
	assert.Zero(t, alliance.BossHp, "the same week keeps a defeated boss down")
	alliance.rollWeek("2026-W45", testAllianceBoss)
	assert.Equal(t, 2, alliance.BossLevel)
	assert.Equal(t, int64(600), alliance.BossMaxHp)
	alliance.BossHp = 10
	alliance.rollWeek("2026-W46", testAllianceBoss)
	assert.Equal(t, 2, alliance.BossLevel, "a boss that survived the week keeps its level")
}
