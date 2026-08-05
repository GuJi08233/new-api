package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveRankingPeriodCalendarBoundaries pins the contract the rankings UI
// depends on: "today" means today, not the last 24 hours, and the comparison
// window is the preceding calendar unit. Rolling windows would make the
// displayed range disagree with the label on every tab.
func TestResolveRankingPeriodCalendarBoundaries(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	// A Thursday, so the weekly window must start three days earlier.
	now := time.Date(2026, time.August, 6, 15, 42, 17, 0, loc)

	cases := []struct {
		period        string
		start         time.Time
		previousStart time.Time
	}{
		{
			period:        "today",
			start:         time.Date(2026, time.August, 6, 0, 0, 0, 0, loc),
			previousStart: time.Date(2026, time.August, 5, 0, 0, 0, 0, loc),
		},
		{
			period:        "week",
			start:         time.Date(2026, time.August, 3, 0, 0, 0, 0, loc),
			previousStart: time.Date(2026, time.July, 27, 0, 0, 0, 0, loc),
		},
		{
			period:        "month",
			start:         time.Date(2026, time.August, 1, 0, 0, 0, 0, loc),
			previousStart: time.Date(2026, time.July, 1, 0, 0, 0, 0, loc),
		},
		{
			period:        "year",
			start:         time.Date(2026, time.January, 1, 0, 0, 0, 0, loc),
			previousStart: time.Date(2025, time.January, 1, 0, 0, 0, 0, loc),
		},
	}

	for _, tc := range cases {
		t.Run(tc.period, func(t *testing.T) {
			window, err := ResolveRankingPeriod(tc.period, now)
			require.NoError(t, err)
			assert.Equal(t, tc.start.Unix(), window.Start)
			assert.Equal(t, now.Unix(), window.End)
			assert.Equal(t, tc.previousStart.Unix(), window.PreviousStart)
			assert.Equal(t, tc.start.Unix()-1, window.PreviousEnd,
				"previous window must end right before the current one, leaving no gap or overlap")
		})
	}
}

// TestResolveRankingPeriodSundayStartsWeekOnMonday guards the weekday shift:
// Go numbers Sunday as 0, so a naive conversion puts Sunday at the start of a
// new week instead of the end of the current one.
func TestResolveRankingPeriodSundayStartsWeekOnMonday(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	sunday := time.Date(2026, time.August, 9, 10, 0, 0, 0, loc)

	window, err := ResolveRankingPeriod("week", sunday)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.August, 3, 0, 0, 0, 0, loc).Unix(), window.Start)
}

// TestResolveRankingPeriodDefaultsAndRejects keeps the empty period aligned
// with the UI default and rejects unknown ids rather than silently serving a
// different window.
func TestResolveRankingPeriodDefaultsAndRejects(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, time.August, 6, 15, 42, 17, 0, loc)

	defaulted, err := ResolveRankingPeriod("", now)
	require.NoError(t, err)
	week, err := ResolveRankingPeriod("week", now)
	require.NoError(t, err)
	assert.Equal(t, week, defaulted)

	_, err = ResolveRankingPeriod("decade", now)
	assert.Error(t, err)
}
