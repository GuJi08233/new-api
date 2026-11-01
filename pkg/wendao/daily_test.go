package wendao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每天抽三件不重复的功课；没有主角时不抽历练，主殿不到 2 级时不抽万宝阁；同一天再换不会重抽。
func TestRollDailyPicksDistinctAvailableTasks(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		s := NewSect(seed, 0)
		s.RollDaily("2026-11-01", "2026-10-31", false)
		require.Len(t, s.Daily.Tasks, DailyTaskCount)
		seen := map[string]bool{}
		for _, task := range s.Daily.Tasks {
			assert.False(t, seen[task.Key], "duplicate task %s", task.Key)
			seen[task.Key] = true
			assert.NotEqual(t, TaskJourney, task.Key)
			assert.NotEqual(t, TaskTrade, task.Key)
		}
		before := s.Daily.Tasks
		s.RollDaily("2026-11-01", "2026-10-31", true)
		assert.Equal(t, before, s.Daily.Tasks)
	}
}

// 功课做满为止；没做完不能领，领过不能再领；三件都领了才能开宝箱，宝箱给聚气丹与一份灵药，只开一次。
func TestDailyClaimAndChest(t *testing.T) {
	s := NewSect(1, 0)
	s.RollDaily("2026-11-01", "2026-10-31", true)
	s.Daily.Tasks = []DailyTask{{Key: TaskHarvest, Need: 3}, {Key: TaskCraft, Need: 2}, {Key: TaskEvent, Need: 1}}
	s.Progress(TaskHarvest, 5)
	assert.Equal(t, 3, s.Daily.Tasks[0].Have)
	_, err := s.claimDaily(1)
	assert.ErrorIs(t, err, ErrUnavailable)

	stone := s.Res.Stone
	_, err = s.claimDaily(0)
	require.NoError(t, err)
	assert.Equal(t, stone+DailyTaskReward(1).Stone, s.Res.Stone)
	_, err = s.claimDaily(0)
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = s.openDailyChest()
	assert.ErrorIs(t, err, ErrUnavailable)

	s.trackDaily("craft", nil)
	s.trackDaily("craft", nil)
	s.trackDaily("event", nil)
	for i := 1; i < 3; i++ {
		_, err = s.claimDaily(i)
		require.NoError(t, err)
	}
	qi := s.Pills[PillQi]
	_, err = s.openDailyChest()
	require.NoError(t, err)
	assert.Equal(t, qi+1, s.Pills[PillQi])
	rares := 0
	for _, n := range s.Rare {
		rares += n
	}
	assert.Equal(t, 1, rares)
	assert.Equal(t, 1, s.Daily.Streak)
	_, err = s.openDailyChest()
	assert.ErrorIs(t, err, ErrUnavailable)
}

// 连续天数：昨天开了宝箱就接着算，断一天清零；连续第七天的宝箱多一颗洗髓丹。
func TestDailyStreak(t *testing.T) {
	s := NewSect(1, 0)
	finish := func() {
		for i := range s.Daily.Tasks {
			s.Daily.Tasks[i].Have = s.Daily.Tasks[i].Need
			_, err := s.claimDaily(i)
			require.NoError(t, err)
		}
		_, err := s.openDailyChest()
		require.NoError(t, err)
	}
	days := []string{"2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04", "2026-11-05", "2026-11-06", "2026-11-07"}
	for i, day := range days {
		yesterday := "2026-10-31"
		if i > 0 {
			yesterday = days[i-1]
		}
		s.RollDaily(day, yesterday, true)
		finish()
	}
	assert.Equal(t, 7, s.Daily.Streak)
	assert.Equal(t, 1, s.Pills[PillMarrow])

	s.RollDaily("2026-11-09", "2026-11-08", true)
	assert.Equal(t, 0, s.Daily.Streak, "missed 2026-11-08")
	s.RollDaily("2026-11-10", "2026-11-09", true)
	assert.Equal(t, 0, s.Daily.Streak, "the chest of 2026-11-09 was never opened")
}

// 每天能换掉一件没做完的功课，换来的不和别的重复；做完了的不能换。
func TestDailyReroll(t *testing.T) {
	s := NewSect(1, 0)
	s.RollDaily("2026-11-01", "2026-10-31", true)
	s.Daily.Tasks[0].Have = s.Daily.Tasks[0].Need
	assert.ErrorIs(t, s.rerollDaily(0), ErrUnavailable)
	old := s.Daily.Tasks[1].Key
	require.NoError(t, s.rerollDaily(1))
	assert.NotEqual(t, old, s.Daily.Tasks[1].Key)
	assert.NotEqual(t, s.Daily.Tasks[0].Key, s.Daily.Tasks[1].Key)
	assert.NotEqual(t, s.Daily.Tasks[2].Key, s.Daily.Tasks[1].Key)
	assert.ErrorIs(t, s.rerollDaily(2), ErrUnavailable, "one reroll a day")
}
