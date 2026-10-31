package sect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 守擂弟子按阵容顺序满血上场(在外历练、受伤也照常守擂)，离开宗门的弟子跳过；全部打败才赢，输一场就结束。
func TestArenaBattleFacesTheLineupInOrder(t *testing.T) {
	home := NewSect(11, 0)
	away := NewSect(12, 0)
	away.Disciples[0].Injured = 5 * hour
	away.Disciples[1].Mission = 7
	lineup, err := away.ArenaLineup([]int{away.Disciples[1].Id, away.Disciples[0].Id})
	require.NoError(t, err)
	_, err = away.ArenaLineup([]int{away.Disciples[0].Id, away.Disciples[0].Id})
	assert.ErrorIs(t, err, ErrAction)
	_, err = away.ArenaLineup([]int{99})
	assert.ErrorIs(t, err, ErrAction)

	defenders := away.ArenaDefenders(append(lineup, 99))
	require.Len(t, defenders, 2, "a disciple who left the sect is skipped")
	assert.Equal(t, away.Disciples[1].Name, defenders[0].Name)
	assert.Equal(t, ArenaPower(defenders), defenders[0].Power+defenders[1].Power)

	tour, err := NewArenaBattle(5, home, []int{home.Disciples[0].Id}, defenders, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, tour.Rounds())
	assert.Equal(t, defenders[0].Name, tour.Opponent.Name)
	for round := 0; round < 2; round++ {
		require.NoError(t, tour.Play("pick", 0))
		assert.Equal(t, defenders[round].Name, tour.Opponent.Name)
		assert.Equal(t, defenders[round].MaxHp, tour.Opponent.Hp, "each defender starts at full health")
		tour.Opponent.Hp, tour.Intent = 1, MoveGuard
		require.NoError(t, tour.Play(MoveStrike, 0))
	}
	assert.True(t, tour.Over())
	assert.True(t, tour.Champion())
	assert.Equal(t, defenders[0].MaxHp, defenders[0].Hp, "the battle never changes the defender's lineup")

	reward := home.RecordArena(tour, "2026-10-31", 0)
	assert.Equal(t, Cost{Stone: 20 * home.Buildings[BuildingHall] * 2, Rep: 4*2 + 8}, reward)
	assert.Equal(t, ArenaStats{Battles: 1, Wins: 1, Day: "2026-10-31", DayRuns: 1}, home.Arena)
}

// 每天前 ArenaDailyRuns 次论剑才有资源，换一天重新计算。
func TestArenaDailyRewards(t *testing.T) {
	s := NewSect(13, 0)
	tour := &Tournament{Wins: 1, Arena: []*Fighter{{}, {}}}
	for range ArenaDailyRuns {
		assert.NotZero(t, s.RecordArena(tour, "2026-10-31", 0).Rep)
	}
	assert.Equal(t, Cost{}, s.RecordArena(tour, "2026-10-31", 0))
	assert.NotZero(t, s.RecordArena(tour, "2026-11-01", 0).Rep)
}
