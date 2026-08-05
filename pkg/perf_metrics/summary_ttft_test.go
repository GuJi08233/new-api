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
	summaryCacheMu.Lock()
	summaryCache = map[string]summaryCacheItem{}
	summaryCacheMu.Unlock()
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
		ModelName:          "stream-model",
		Group:              "default",
		BucketTs:           bucketTs,
		RequestCount:       4,
		SuccessCount:       3,
		TotalLatencyMs:     8000,
		TtftSumMs:          6000,
		TtftCount:          4,
		OutputTokens:       400,
		GenerationMs:       4000,
		StreamOutputTokens: 400,
		StreamGenerationMs: 4000,
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

	result, err := QuerySummaryAll(SummaryParams{StartTs: bucketTs - 3600, EndTs: bucketTs})
	require.NoError(t, err)

	byName := map[string]ModelSummary{}
	for _, m := range result.Models {
		byName[m.ModelName] = m
	}

	stream, ok := byName["stream-model"]
	require.True(t, ok, "streaming model should be present in summary")
	assert.InDelta(t, 1500, stream.AvgTtftMs, 0.001, "avg TTFT should be ttft_sum_ms/ttft_count")
	assert.Equal(t, int64(4), stream.TtftSampleCount)
	assert.True(t, stream.TpsFromStream, "throughput must come from streaming samples when available")
	require.Len(t, stream.RecentSeries, 1)
	assert.Equal(t, bucketTs, stream.RecentSeries[0].Ts)
	assert.InDelta(t, 1500, stream.RecentSeries[0].AvgTtftMs, 0.001)
	assert.InDelta(t, 75, stream.RecentSeries[0].SuccessRate, 0.001)
	assert.InDelta(t, 100, stream.RecentSeries[0].AvgTps, 0.001)
	assert.Equal(t, int64(4), stream.RecentSeries[0].RequestCount)

	nonStream, ok := byName["non-stream-model"]
	require.True(t, ok, "non-streaming model should be present in summary")
	assert.InDelta(t, 0, nonStream.AvgTtftMs, 0.001, "avg TTFT must stay 0 when no TTFT samples exist")
	assert.Equal(t, int64(0), nonStream.TtftSampleCount, "clients need this to tell 'no data' from 'zero latency'")
	assert.False(t, nonStream.TpsFromStream, "throughput falls back to all requests without streaming samples")
	assert.InDelta(t, 100, nonStream.AvgTps, 0.001)
}

// TestQuerySummaryAllThroughputPrefersStreamingSamples pins the throughput
// basis: a model whose non-streaming requests fold the upstream wait into the
// generation window must still report the streaming-only rate, otherwise its
// TPS is dragged down by a quantity that is not generation time.
func TestQuerySummaryAllThroughputPrefersStreamingSamples(t *testing.T) {
	setupPerfTestDB(t)

	bucketTs := time.Now().Unix()
	require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{
		ModelName:      "mixed-model",
		Group:          "default",
		BucketTs:       bucketTs,
		RequestCount:   2,
		SuccessCount:   2,
		TotalLatencyMs: 12000,
		TtftSumMs:      2000,
		TtftCount:      1,
		// 1000 tokens over 11s of wall clock across both requests, but only
		// 1s of real generation time on the single streaming request.
		OutputTokens:       1000,
		GenerationMs:       11000,
		StreamOutputTokens: 500,
		StreamGenerationMs: 1000,
	}))

	result, err := QuerySummaryAll(SummaryParams{StartTs: bucketTs - 3600, EndTs: bucketTs})
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.InDelta(t, 500, result.Models[0].AvgTps, 0.001)
	assert.True(t, result.Models[0].TpsFromStream)
}

// TestQuerySummaryAllLimit ensures the response stays bounded; the summary
// carries an inline series per model, so an uncapped list grows with the
// model catalogue.
func TestQuerySummaryAllLimit(t *testing.T) {
	setupPerfTestDB(t)

	bucketTs := time.Now().Unix()
	for _, name := range []string{"model-a", "model-b", "model-c"} {
		require.NoError(t, model.UpsertPerfMetric(&model.PerfMetric{
			ModelName:      name,
			Group:          "default",
			BucketTs:       bucketTs,
			RequestCount:   int64(len(name)),
			SuccessCount:   1,
			TotalLatencyMs: 1000,
		}))
	}

	result, err := QuerySummaryAll(SummaryParams{StartTs: bucketTs - 3600, EndTs: bucketTs, Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Models, 2)
	assert.Equal(t, bucketTs-3600, result.StartTime)
	assert.Equal(t, bucketTs, result.EndTime)
}
