package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedQuotaData(t *testing.T, userId int, username string, createdAt int64, tokenUsed, count, quota int) {
	t.Helper()
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    userId,
		Username:  username,
		ModelName: "gpt-test",
		CreatedAt: createdAt,
		TokenUsed: tokenUsed,
		Count:     count,
		Quota:     quota,
	}).Error)
}

// The rankings page shows three user leaderboards driven by the same aggregation:
// token usage, request count and quota spent. Each one must order by its own
// metric and drop users who are zero on it, otherwise a user who only made free
// calls tops the spending board (and vice versa).
func TestGetUserRankingsOrdersByMetricAndDropsZeroRows(t *testing.T) {
	truncateTables(t)

	// heavy: most tokens, fewest requests. chatty: most requests, least spend.
	// spender: most quota. freeloader: real traffic but zero quota. ghost: quota
	// carried over with no tokens and no requests.
	seedQuotaData(t, 7001, "heavy", 1000, 900, 2, 300)
	seedQuotaData(t, 7001, "heavy", 1100, 100, 1, 100)
	seedQuotaData(t, 7002, "chatty", 1000, 200, 50, 50)
	seedQuotaData(t, 7003, "spender", 1000, 300, 5, 900)
	seedQuotaData(t, 7004, "freeloader", 1000, 500, 10, 0)
	seedQuotaData(t, 7005, "ghost", 1000, 0, 0, 500)
	// Outside the window entirely, must not reach any board.
	seedQuotaData(t, 7006, "stale", 500, 9999, 999, 9999)

	tests := []struct {
		name     string
		metric   UserRankingMetric
		expected []string
	}{
		{"按 Token 用量排序", UserRankingByTokens, []string{"heavy", "freeloader", "spender", "chatty"}},
		{"按请求次数排序", UserRankingByRequests, []string{"chatty", "freeloader", "spender", "heavy"}},
		{"按消费额度排序", UserRankingByQuota, []string{"spender", "ghost", "heavy", "chatty"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := GetUserRankings(900, 2000, tc.metric, 10)
			require.NoError(t, err)

			names := make([]string, 0, len(rows))
			for _, row := range rows {
				names = append(names, row.Username)
			}
			assert.Equal(t, tc.expected, names)
		})
	}

	// Every board returns all three aggregates per row, so switching a board's
	// metric never needs a schema change on the response.
	rows, err := GetUserRankings(900, 2000, UserRankingByQuota, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, UserRanking{
		UserID:       7003,
		Username:     "spender",
		RequestCount: 5,
		TotalQuota:   900,
		TotalTokens:  300,
	}, rows[0])

	_, err = GetUserRankings(900, 2000, UserRankingMetric("total_quota; DROP TABLE quota_data"), 10)
	assert.Error(t, err)
}
