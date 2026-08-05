package perfmetrics

import "sync/atomic"

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model        string
	Group        string
	LatencyMs    int64
	TtftMs       int64
	HasTtft      bool
	Success      bool
	OutputTokens int64
	GenerationMs int64
}

type QueryParams struct {
	Model string
	Group string
	Hours int
}

// SummaryParams selects the window for a cross-model performance summary.
// Limit caps how many models are returned (0 means no cap).
type SummaryParams struct {
	StartTs int64
	EndTs   int64
	Groups  []string
	Limit   int
}

type BucketPoint struct {
	Ts           int64   `json:"ts"`
	AvgTtftMs    float64 `json:"avg_ttft_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
	RequestCount int64   `json:"request_count"`
}

type GroupResult struct {
	Group        string        `json:"group"`
	AvgTtftMs    float64       `json:"avg_ttft_ms"`
	AvgLatencyMs float64       `json:"avg_latency_ms"`
	SuccessRate  float64       `json:"success_rate"`
	AvgTps       float64       `json:"avg_tps"`
	Series       []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string        `json:"model_name"`
	SeriesSchema string        `json:"series_schema"`
	Groups       []GroupResult `json:"groups"`
}

type ModelSummary struct {
	ModelName          string        `json:"model_name"`
	AvgLatencyMs       float64       `json:"avg_latency_ms"`
	AvgTtftMs          float64       `json:"avg_ttft_ms"`
	SuccessRate        float64       `json:"success_rate"`
	AvgTps             float64       `json:"avg_tps"`
	RecentSuccessRates []float64     `json:"recent_success_rates,omitempty"`
	RecentSeries       []BucketPoint `json:"recent_series,omitempty"`
	RequestCount       int64         `json:"request_count"`
	// TtftSampleCount is the number of streaming requests behind AvgTtftMs; it
	// is 0 for models that are only ever called non-streaming, where the
	// time-to-first-token is not observable.
	TtftSampleCount int64 `json:"ttft_sample_count"`
	// TpsFromStream reports whether AvgTps was derived from streaming-only
	// samples (comparable across models) or fell back to all requests.
	TpsFromStream bool `json:"tps_from_stream"`
}

type SummaryAllResult struct {
	StartTime int64          `json:"start_time"`
	EndTime   int64          `json:"end_time"`
	Models    []ModelSummary `json:"models"`
}

type bucketKey struct {
	model    string
	group    string
	bucketTs int64
}

type counters struct {
	requestCount       int64
	successCount       int64
	totalLatencyMs     int64
	ttftSumMs          int64
	ttftCount          int64
	outputTokens       int64
	generationMs       int64
	streamOutputTokens int64
	streamGenerationMs int64
}

func (c *counters) merge(other counters) {
	c.requestCount += other.requestCount
	c.successCount += other.successCount
	c.totalLatencyMs += other.totalLatencyMs
	c.ttftSumMs += other.ttftSumMs
	c.ttftCount += other.ttftCount
	c.outputTokens += other.outputTokens
	c.generationMs += other.generationMs
	c.streamOutputTokens += other.streamOutputTokens
	c.streamGenerationMs += other.streamGenerationMs
}

type atomicBucket struct {
	requestCount       atomic.Int64
	successCount       atomic.Int64
	totalLatencyMs     atomic.Int64
	ttftSumMs          atomic.Int64
	ttftCount          atomic.Int64
	outputTokens       atomic.Int64
	generationMs       atomic.Int64
	streamOutputTokens atomic.Int64
	streamGenerationMs atomic.Int64
}

func (b *atomicBucket) add(sample Sample) {
	b.requestCount.Add(1)
	if sample.Success {
		b.successCount.Add(1)
	}
	if sample.LatencyMs > 0 {
		b.totalLatencyMs.Add(sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		b.ttftSumMs.Add(sample.TtftMs)
		b.ttftCount.Add(1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		b.outputTokens.Add(sample.OutputTokens)
		b.generationMs.Add(sample.GenerationMs)
		if sample.HasTtft {
			b.streamOutputTokens.Add(sample.OutputTokens)
			b.streamGenerationMs.Add(sample.GenerationMs)
		}
	}
}

func (b *atomicBucket) snapshot() counters {
	return counters{
		requestCount:       b.requestCount.Load(),
		successCount:       b.successCount.Load(),
		totalLatencyMs:     b.totalLatencyMs.Load(),
		ttftSumMs:          b.ttftSumMs.Load(),
		ttftCount:          b.ttftCount.Load(),
		outputTokens:       b.outputTokens.Load(),
		generationMs:       b.generationMs.Load(),
		streamOutputTokens: b.streamOutputTokens.Load(),
		streamGenerationMs: b.streamGenerationMs.Load(),
	}
}

func (b *atomicBucket) drain() counters {
	return counters{
		requestCount:       b.requestCount.Swap(0),
		successCount:       b.successCount.Swap(0),
		totalLatencyMs:     b.totalLatencyMs.Swap(0),
		ttftSumMs:          b.ttftSumMs.Swap(0),
		ttftCount:          b.ttftCount.Swap(0),
		outputTokens:       b.outputTokens.Swap(0),
		generationMs:       b.generationMs.Swap(0),
		streamOutputTokens: b.streamOutputTokens.Swap(0),
		streamGenerationMs: b.streamGenerationMs.Swap(0),
	}
}

func (b *atomicBucket) addCounters(c counters) {
	if c.requestCount != 0 {
		b.requestCount.Add(c.requestCount)
	}
	if c.successCount != 0 {
		b.successCount.Add(c.successCount)
	}
	if c.totalLatencyMs != 0 {
		b.totalLatencyMs.Add(c.totalLatencyMs)
	}
	if c.ttftSumMs != 0 {
		b.ttftSumMs.Add(c.ttftSumMs)
	}
	if c.ttftCount != 0 {
		b.ttftCount.Add(c.ttftCount)
	}
	if c.outputTokens != 0 {
		b.outputTokens.Add(c.outputTokens)
	}
	if c.generationMs != 0 {
		b.generationMs.Add(c.generationMs)
	}
	if c.streamOutputTokens != 0 {
		b.streamOutputTokens.Add(c.streamOutputTokens)
	}
	if c.streamGenerationMs != 0 {
		b.streamGenerationMs.Add(c.streamGenerationMs)
	}
}
