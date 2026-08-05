package perfmetrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

var hotBuckets sync.Map

// seriesSchema is a stable client cache/schema marker. Do not change it when
// hiding fields or making response-only privacy hardening changes.
const seriesSchema = "dbcd0a3c01b55203"

const (
	summaryCacheTTL        = time.Minute
	summaryCacheMaxEntries = 32
	maxSeriesPoints        = 180
)

type summaryCacheItem struct {
	expiresAt time.Time
	data      SummaryAllResult
}

var (
	summaryCacheMu sync.RWMutex
	summaryCache   = map[string]summaryCacheItem{}
)

func Init() {
	go flushLoop()
}

func RecordRelaySample(info *relaycommon.RelayInfo, success bool, outputTokens int64) {
	if info == nil {
		return
	}
	now := time.Now()
	hasTtft := info.IsStream && info.HasSendResponse()
	ttftMs := int64(0)
	if hasTtft {
		ttftMs = info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
	}
	latencyMs := now.Sub(info.StartTime).Milliseconds()
	generationMs := latencyMs
	if hasTtft {
		generationMs = now.Sub(info.FirstResponseTime).Milliseconds()
	}
	if generationMs <= 0 {
		generationMs = latencyMs
	}
	Record(Sample{
		Model:        info.OriginModelName,
		Group:        info.UsingGroup,
		LatencyMs:    latencyMs,
		TtftMs:       ttftMs,
		HasTtft:      hasTtft,
		Success:      success,
		OutputTokens: outputTokens,
		GenerationMs: generationMs,
	})
}

func Record(sample Sample) {
	setting := perf_metrics_setting.GetSetting()
	if !setting.Enabled || sample.Model == "" {
		return
	}
	if sample.Group == "" {
		sample.Group = "default"
	}
	if sample.LatencyMs < 0 {
		sample.LatencyMs = 0
	}

	key := bucketKey{
		model:    sample.Model,
		group:    sample.Group,
		bucketTs: bucketStart(time.Now().Unix()),
	}
	actual, _ := hotBuckets.LoadOrStore(key, &atomicBucket{})
	actual.(*atomicBucket).add(sample)
	recordRedis(key, sample)
}

func Query(params QueryParams) (QueryResult, error) {
	if params.Hours <= 0 {
		params.Hours = 24
	}
	if params.Hours > 24*30 {
		params.Hours = 24 * 30
	}
	endTs := time.Now().Unix()
	startTs := endTs - int64(params.Hours)*3600

	merged := map[bucketKey]counters{}
	rows, err := model.GetPerfMetrics(params.Model, params.Group, startTs, endTs)
	if err != nil {
		return QueryResult{}, err
	}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{
			model:    row.ModelName,
			group:    row.Group,
			bucketTs: row.BucketTs,
		}, counters{
			requestCount:       row.RequestCount,
			successCount:       row.SuccessCount,
			totalLatencyMs:     row.TotalLatencyMs,
			ttftSumMs:          row.TtftSumMs,
			ttftCount:          row.TtftCount,
			outputTokens:       row.OutputTokens,
			generationMs:       row.GenerationMs,
			streamOutputTokens: row.StreamOutputTokens,
			streamGenerationMs: row.StreamGenerationMs,
		})
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.model != params.Model || k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if params.Group != "" && k.group != params.Group {
			return true
		}
		mergeCounters(merged, k, value.(*atomicBucket).snapshot())
		return true
	})

	return buildQueryResult(params.Model, merged), nil
}

func QuerySummaryAll(params SummaryParams) (SummaryAllResult, error) {
	if params.EndTs <= 0 {
		params.EndTs = time.Now().Unix()
	}
	if params.StartTs <= 0 || params.StartTs > params.EndTs {
		params.StartTs = params.EndTs - 24*3600
	}

	cacheKey := summaryCacheKey(params)
	if cached, ok := loadSummaryCache(cacheKey); ok {
		return cached, nil
	}

	startTs, endTs := params.StartTs, params.EndTs
	allowedGroups := allowedGroupSet(params.Groups)

	rows, err := model.GetPerfMetricsSummaryBucketsAll(startTs, endTs, params.Groups)
	if err != nil {
		return SummaryAllResult{}, err
	}

	totals := map[string]counters{}
	modelBuckets := map[string]map[int64]counters{}
	for _, row := range rows {
		value := counters{
			requestCount:       row.RequestCount,
			successCount:       row.SuccessCount,
			totalLatencyMs:     row.TotalLatencyMs,
			ttftSumMs:          row.TtftSumMs,
			ttftCount:          row.TtftCount,
			outputTokens:       row.OutputTokens,
			generationMs:       row.GenerationMs,
			streamOutputTokens: row.StreamOutputTokens,
			streamGenerationMs: row.StreamGenerationMs,
		}
		mergeModelTotals(totals, row.ModelName, value)
		mergeModelBucket(modelBuckets, row.ModelName, row.BucketTs, value)
	}

	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs < startTs || k.bucketTs > endTs {
			return true
		}
		if allowedGroups != nil {
			if _, ok := allowedGroups[k.group]; !ok {
				return true
			}
		}
		snap := value.(*atomicBucket).snapshot()
		if snap.requestCount == 0 {
			return true
		}
		mergeModelTotals(totals, k.model, snap)
		mergeModelBucket(modelBuckets, k.model, k.bucketTs, snap)
		return true
	})

	seriesPoints := seriesPointLimit(startTs, endTs)
	models := make([]ModelSummary, 0, len(totals))
	for name, total := range totals {
		if total.requestCount == 0 {
			continue
		}
		tps, fromStream := throughput(total)
		models = append(models, ModelSummary{
			ModelName:          name,
			AvgLatencyMs:       avg(total.totalLatencyMs, total.requestCount),
			AvgTtftMs:          avg(total.ttftSumMs, total.ttftCount),
			SuccessRate:        successRate(total),
			AvgTps:             tps,
			TpsFromStream:      fromStream,
			TtftSampleCount:    total.ttftCount,
			RecentSuccessRates: recentSuccessRates(modelBuckets[name], seriesPoints),
			RecentSeries:       recentModelSeries(modelBuckets[name], seriesPoints),
			RequestCount:       total.requestCount,
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].RequestCount == models[j].RequestCount {
			return models[i].ModelName < models[j].ModelName
		}
		return models[i].RequestCount > models[j].RequestCount
	})
	if params.Limit > 0 && len(models) > params.Limit {
		models = models[:params.Limit]
	}

	result := SummaryAllResult{StartTime: startTs, EndTime: endTs, Models: models}
	storeSummaryCache(cacheKey, result)
	return result, nil
}

// seriesPointLimit keeps the inline sparkline series covering the whole
// requested window instead of a fixed bucket count, which would silently show
// only the last hour when the bucket size is configured below one hour.
func seriesPointLimit(startTs int64, endTs int64) int {
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	points := int((endTs-startTs)/bucketSeconds) + 1
	if points < 2 {
		points = 2
	}
	if points > maxSeriesPoints {
		points = maxSeriesPoints
	}
	return points
}

func summaryCacheKey(params SummaryParams) string {
	groups := append([]string(nil), params.Groups...)
	sort.Strings(groups)
	return fmt.Sprintf("%d:%d:%d:%v", params.StartTs, params.EndTs, params.Limit, groups)
}

func loadSummaryCache(key string) (SummaryAllResult, bool) {
	summaryCacheMu.RLock()
	defer summaryCacheMu.RUnlock()
	item, ok := summaryCache[key]
	if !ok || time.Now().After(item.expiresAt) {
		return SummaryAllResult{}, false
	}
	return item.data, true
}

func storeSummaryCache(key string, data SummaryAllResult) {
	summaryCacheMu.Lock()
	defer summaryCacheMu.Unlock()
	if len(summaryCache) > summaryCacheMaxEntries {
		summaryCache = map[string]summaryCacheItem{}
	}
	summaryCache[key] = summaryCacheItem{
		expiresAt: time.Now().Add(summaryCacheTTL),
		data:      data,
	}
}

func mergeModelTotals(totals map[string]counters, modelName string, value counters) {
	if value.requestCount == 0 {
		return
	}
	current := totals[modelName]
	current.merge(value)
	totals[modelName] = current
}

func mergeModelBucket(modelBuckets map[string]map[int64]counters, modelName string, bucketTs int64, value counters) {
	if value.requestCount == 0 {
		return
	}
	if _, ok := modelBuckets[modelName]; !ok {
		modelBuckets[modelName] = map[int64]counters{}
	}
	current := modelBuckets[modelName][bucketTs]
	current.merge(value)
	modelBuckets[modelName][bucketTs] = current
}

func recentSuccessRates(buckets map[int64]counters, limit int) []float64 {
	series := recentModelSeries(buckets, limit)
	rates := make([]float64, 0, len(series))
	for _, point := range series {
		rates = append(rates, point.SuccessRate)
	}
	return rates
}

func recentModelSeries(buckets map[int64]counters, limit int) []BucketPoint {
	if len(buckets) == 0 || limit <= 0 {
		return nil
	}
	timestamps := make([]int64, 0, len(buckets))
	for ts := range buckets {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	if len(timestamps) > limit {
		timestamps = timestamps[len(timestamps)-limit:]
	}

	series := make([]BucketPoint, 0, len(timestamps))
	for _, ts := range timestamps {
		series = append(series, bucketPoint(ts, buckets[ts]))
	}
	return series
}

func allowedGroupSet(groups []string) map[string]struct{} {
	if groups == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		allowed[group] = struct{}{}
	}
	return allowed
}

func bucketStart(ts int64) int64 {
	bucketSeconds := perf_metrics_setting.GetBucketSeconds()
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	return ts - (ts % bucketSeconds)
}

func mergeCounters(merged map[bucketKey]counters, key bucketKey, value counters) {
	if value.requestCount == 0 {
		return
	}
	current := merged[key]
	current.merge(value)
	merged[key] = current
}

func buildQueryResult(modelName string, merged map[bucketKey]counters) QueryResult {
	groupBuckets := map[string]map[int64]counters{}
	for key, value := range merged {
		if value.requestCount == 0 {
			continue
		}
		if _, ok := groupBuckets[key.group]; !ok {
			groupBuckets[key.group] = map[int64]counters{}
		}
		groupBuckets[key.group][key.bucketTs] = value
	}

	groups := make([]string, 0, len(groupBuckets))
	for group := range groupBuckets {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	results := make([]GroupResult, 0, len(groups))
	for _, group := range groups {
		buckets := groupBuckets[group]
		timestamps := make([]int64, 0, len(buckets))
		for ts := range buckets {
			timestamps = append(timestamps, ts)
		}
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i] < timestamps[j]
		})

		total := counters{}
		series := make([]BucketPoint, 0, len(timestamps))
		for _, ts := range timestamps {
			value := buckets[ts]
			total.merge(value)
			series = append(series, bucketPoint(ts, value))
		}

		tps, _ := throughput(total)
		results = append(results, GroupResult{
			Group:        group,
			AvgTtftMs:    avg(total.ttftSumMs, total.ttftCount),
			AvgLatencyMs: avg(total.totalLatencyMs, total.requestCount),
			SuccessRate:  successRate(total),
			AvgTps:       tps,
			Series:       series,
		})
	}

	return QueryResult{
		ModelName:    modelName,
		SeriesSchema: seriesSchema,
		Groups:       results,
	}
}

func bucketPoint(ts int64, value counters) BucketPoint {
	tps, _ := throughput(value)
	return BucketPoint{
		Ts:           ts,
		AvgTtftMs:    avg(value.ttftSumMs, value.ttftCount),
		AvgLatencyMs: avg(value.totalLatencyMs, value.requestCount),
		SuccessRate:  successRate(value),
		AvgTps:       tps,
		RequestCount: value.requestCount,
	}
}

func avg(sum int64, count int64) float64 {
	if count <= 0 {
		return 0
	}
	return roundPerfFloat(float64(sum) / float64(count))
}

func successRate(value counters) float64 {
	if value.requestCount <= 0 {
		return 0
	}
	return roundPerfFloat(float64(value.successCount) / float64(value.requestCount) * 100)
}

// throughput prefers streaming-only samples, where the generation phase is
// measured from the first token onward and is therefore comparable across
// models. Non-streaming requests fold the whole upstream wait into the
// generation window, so they are only used when a model has no streaming
// traffic at all; the returned flag says which basis was used.
func throughput(value counters) (float64, bool) {
	if value.streamOutputTokens > 0 && value.streamGenerationMs > 0 {
		return roundPerfFloat(float64(value.streamOutputTokens) / (float64(value.streamGenerationMs) / 1000)), true
	}
	if value.outputTokens > 0 && value.generationMs > 0 {
		return roundPerfFloat(float64(value.outputTokens) / (float64(value.generationMs) / 1000)), false
	}
	return 0, false
}

func roundPerfFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

func recordRedis(key bucketKey, sample Sample) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	redisKey := redisBucketKey(key)
	pipe := common.RDB.TxPipeline()
	pipe.HIncrBy(ctx, redisKey, "req", 1)
	if sample.Success {
		pipe.HIncrBy(ctx, redisKey, "ok", 1)
	}
	if sample.LatencyMs > 0 {
		pipe.HIncrBy(ctx, redisKey, "lat", sample.LatencyMs)
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		pipe.HIncrBy(ctx, redisKey, "ttft", sample.TtftMs)
		pipe.HIncrBy(ctx, redisKey, "ttft_n", 1)
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		pipe.HIncrBy(ctx, redisKey, "out", sample.OutputTokens)
		pipe.HIncrBy(ctx, redisKey, "gen_ms", sample.GenerationMs)
		if sample.HasTtft {
			pipe.HIncrBy(ctx, redisKey, "s_out", sample.OutputTokens)
			pipe.HIncrBy(ctx, redisKey, "s_gen_ms", sample.GenerationMs)
		}
	}
	pipe.Expire(ctx, redisKey, time.Hour)
	_, _ = pipe.Exec(ctx)
}

func mergeRedisActiveBuckets(merged map[bucketKey]counters, params QueryParams, startTs int64, endTs int64) {
	if !common.RedisEnabled || common.RDB == nil || params.Model == "" || params.Group == "" {
		return
	}
	active := bucketStart(time.Now().Unix())
	if active < startTs || active > endTs {
		return
	}
	key := bucketKey{model: params.Model, group: params.Group, bucketTs: active}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	values, err := common.RDB.HGetAll(ctx, redisBucketKey(key)).Result()
	if err != nil || len(values) == 0 {
		return
	}
	mergeCounters(merged, key, redisCounters(values))
}

func redisBucketKey(key bucketKey) string {
	return fmt.Sprintf("perf:%s:%s:%d", key.model, key.group, key.bucketTs)
}
