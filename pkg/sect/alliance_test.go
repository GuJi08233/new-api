package sect

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
