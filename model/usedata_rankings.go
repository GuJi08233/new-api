package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RankingQuotaTotal struct {
	ModelName     string `json:"model_name"`
	TotalTokens   int64  `json:"total_tokens"`
	TotalRequests int64  `json:"total_requests"`
}

type RankingQuotaBucket struct {
	ModelName string `json:"model_name"`
	Bucket    int64  `json:"bucket"`
	Tokens    int64  `json:"tokens"`
}

func GetRankingQuotaTotals(startTime int64, endTime int64) ([]RankingQuotaTotal, error) {
	var rows []RankingQuotaTotal
	query := ReadDB().Table("quota_data").
		Select("model_name, sum(token_used) as total_tokens, sum(count) as total_requests").
		Where("model_name <> ''").
		Group("model_name").
		// Request-only models (e.g. image generation with zero token_used) must
		// still reach the request-count leaderboard.
		Having("sum(token_used) > 0 OR sum(count) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

// GetRankingQuotaBuckets aggregates token usage into fixed-size buckets.
// bucketShift moves bucket boundaries off the Unix epoch so they line up with
// local calendar units (timezone offset, plus a weekday anchor for week-sized
// buckets); the returned bucket value is still a UTC epoch second.
func GetRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64, bucketShift int64) ([]RankingQuotaBucket, error) {
	if bucketSize <= 0 {
		bucketSize = 3600
	}
	bucketExpr := rankingBucketExpr(bucketSize, bucketShift)
	var rows []RankingQuotaBucket
	query := ReadDB().Table("quota_data").
		Select(fmt.Sprintf("model_name, %s as bucket, sum(token_used) as tokens", bucketExpr)).
		Where("model_name <> ''").
		Group(fmt.Sprintf("model_name, %s", bucketExpr)).
		Having("sum(token_used) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func rankingBucketExpr(bucketSize int64, bucketShift int64) string {
	// created_at is always a positive recent epoch second, so integer division
	// truncation and FLOOR agree here; MySQL still needs FLOOR because "/"
	// yields a decimal there.
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fmt.Sprintf("FLOOR((created_at + %d) / %d) * %d - %d", bucketShift, bucketSize, bucketSize, bucketShift)
	}
	return fmt.Sprintf("((created_at + %d) / %d) * %d - %d", bucketShift, bucketSize, bucketSize, bucketShift)
}

func applyRankingQuotaTimeRange(query *gorm.DB, startTime int64, endTime int64) *gorm.DB {
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}
	return query
}

// UserRanking is one user's aggregated usage inside a ranking window. The three
// user leaderboards (token usage, request count, quota spent) are the same
// aggregation ordered by a different metric, so they share one row shape and one
// query builder rather than three copies that drift apart.
type UserRanking struct {
	UserID       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
	TotalQuota   int64  `json:"total_quota"`
	TotalTokens  int64  `json:"total_tokens"`
}

// UserRankingMetric selects which aggregate a user leaderboard is ordered by.
type UserRankingMetric string

const (
	UserRankingByTokens   UserRankingMetric = "total_tokens"
	UserRankingByRequests UserRankingMetric = "request_count"
	UserRankingByQuota    UserRankingMetric = "total_quota"
)

// UserRankingSummary represents aggregate stats for the user rankings page.
type UserRankingSummary struct {
	TotalRequests int64 `json:"total_requests"`
	TotalQuota    int64 `json:"total_quota"`
	TotalTokens   int64 `json:"total_tokens"`
}

// GetUserRankings returns the top users of a window ordered by metric. Users
// whose value for that metric is zero are dropped, so free-model traffic does
// not pad the quota board and quota-only rows do not pad the token board.
// PostgreSQL rejects select aliases in HAVING, so the filter repeats the
// aggregate expression instead of reusing the alias the way ORDER BY can.
func GetUserRankings(startTime int64, endTime int64, metric UserRankingMetric, limit int) ([]UserRanking, error) {
	var having string
	switch metric {
	case UserRankingByTokens:
		having = "sum(token_used) > 0"
	case UserRankingByRequests:
		having = "sum(count) > 0"
	case UserRankingByQuota:
		having = "sum(quota) > 0"
	default:
		return nil, fmt.Errorf("invalid user ranking metric: %s", metric)
	}
	if limit <= 0 {
		limit = 50
	}

	var rows []UserRanking
	query := ReadDB().Table("quota_data").
		Select("user_id, username, sum(count) as request_count, sum(quota) as total_quota, sum(token_used) as total_tokens").
		Where("username <> ''").
		Group("user_id, username").
		Having(having).
		Order(string(metric) + " DESC").
		Limit(limit)
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserRankingSummary(startTime int64, endTime int64) (*UserRankingSummary, error) {
	var summary UserRankingSummary
	query := ReadDB().Table("quota_data").
		Select("COALESCE(sum(count), 0) as total_requests, COALESCE(sum(quota), 0) as total_quota, COALESCE(sum(token_used), 0) as total_tokens")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Scan(&summary).Error
	return &summary, err
}
