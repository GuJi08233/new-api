package perfmetrics

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupPerfTestDB provisions an in-memory SQLite database with the perf_metrics
// table so summary aggregation can be exercised against real persisted rows,
// which is the path that serves historical (already flushed) metrics.
func setupPerfTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:perf-summary-ttft?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	model.DB = db
	t.Cleanup(func() {
		require.NoError(t, db.Exec("DELETE FROM perf_metrics").Error)
	})
}

// TestQuerySummaryAllMetrics verifies the summary values and the historical
// bucket series consumed by performance trend clients.
func TestQuerySummaryAllMetrics(t *testing.T) {
	setupPerfTestDB(t)

	bucketTs := time.Now().Unix()

	// Streaming model: 4 requests, TTFT summed to 6000ms over 4 samples -> avg 1500ms.
	require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{
		ModelName:      "stream-model",
		Group:          "default",
		BucketTs:       bucketTs,
		RequestCount:   4,
		SuccessCount:   3,
		TotalLatencyMs: 8000,
		TtftSumMs:      6000,
		TtftCount:      4,
		OutputTokens:   400,
		GenerationMs:   4000,
	}))

	// Non-streaming model: no TTFT samples at all.
	require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{
		ModelName:      "non-stream-model",
		Group:          "default",
		BucketTs:       bucketTs,
		RequestCount:   2,
		SuccessCount:   2,
		TotalLatencyMs: 3000,
		TtftSumMs:      0,
		TtftCount:      0,
		OutputTokens:   200,
		GenerationMs:   2000,
	}))

	result, err := QuerySummaryAll(24, nil)
	require.NoError(t, err)

	byName := map[string]ModelSummary{}
	for _, m := range result.Models {
		byName[m.ModelName] = m
	}

	stream, ok := byName["stream-model"]
	require.True(t, ok, "streaming model should be present in summary")
	assert.Equal(t, int64(1500), stream.AvgTtftMs, "avg TTFT should be ttft_sum_ms/ttft_count")
	require.Len(t, stream.RecentSeries, 1)
	assert.Equal(t, bucketTs, stream.RecentSeries[0].Ts)
	assert.Equal(t, int64(1500), stream.RecentSeries[0].AvgTtftMs)
	assert.InDelta(t, 75, stream.RecentSeries[0].SuccessRate, 0.001)
	assert.InDelta(t, 100, stream.RecentSeries[0].AvgTps, 0.001)

	nonStream, ok := byName["non-stream-model"]
	require.True(t, ok, "non-streaming model should be present in summary")
	assert.Equal(t, int64(0), nonStream.AvgTtftMs, "avg TTFT must stay 0 when no TTFT samples exist")
}
