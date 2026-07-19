package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RankingQuotaTotal struct {
	ModelName   string `json:"model_name"`
	TotalTokens int64  `json:"total_tokens"`
}

type RankingQuotaBucket struct {
	ModelName string `json:"model_name"`
	Bucket    int64  `json:"bucket"`
	Tokens    int64  `json:"tokens"`
}

func GetRankingQuotaTotals(startTime int64, endTime int64) ([]RankingQuotaTotal, error) {
	var rows []RankingQuotaTotal
	query := DB.Table("quota_data").
		Select("model_name, sum(token_used) as total_tokens").
		Where("model_name <> ''").
		Group("model_name").
		Having("sum(token_used) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64) ([]RankingQuotaBucket, error) {
	if bucketSize <= 0 {
		bucketSize = 3600
	}
	bucketExpr := rankingBucketExpr(bucketSize)
	var rows []RankingQuotaBucket
	query := DB.Table("quota_data").
		Select(fmt.Sprintf("model_name, %s as bucket, sum(token_used) as tokens", bucketExpr)).
		Where("model_name <> ''").
		Group(fmt.Sprintf("model_name, %s", bucketExpr)).
		Having("sum(token_used) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func rankingBucketExpr(bucketSize int64) string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fmt.Sprintf("FLOOR(created_at / %d) * %d", bucketSize, bucketSize)
	}
	return fmt.Sprintf("(created_at / %d) * %d", bucketSize, bucketSize)
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

// UserRequestRanking represents a user's request count ranking entry.
type UserRequestRanking struct {
	UserID       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
}

// UserQuotaRanking represents a user's quota consumption ranking entry.
type UserQuotaRanking struct {
	UserID     int    `json:"user_id"`
	Username   string `json:"username"`
	TotalQuota int64  `json:"total_quota"`
}

// UserRankingSummary represents aggregate stats for the user rankings page.
type UserRankingSummary struct {
	TotalRequests int64 `json:"total_requests"`
	TotalQuota    int64 `json:"total_quota"`
	TotalTokens   int64 `json:"total_tokens"`
}

func GetUserRequestRankings(startTime int64, endTime int64, limit int) ([]UserRequestRanking, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []UserRequestRanking
	query := DB.Table("quota_data").
		Select("user_id, username, sum(count) as request_count").
		Where("username <> ''").
		Group("user_id, username").
		Having("sum(count) > 0").
		Order("request_count DESC").
		Limit(limit)
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserQuotaRankings(startTime int64, endTime int64, limit int) ([]UserQuotaRanking, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []UserQuotaRanking
	query := DB.Table("quota_data").
		Select("user_id, username, sum(quota) as total_quota").
		Where("username <> ''").
		Group("user_id, username").
		Having("sum(quota) > 0").
		Order("total_quota DESC").
		Limit(limit)
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserRankingSummary(startTime int64, endTime int64) (*UserRankingSummary, error) {
	var summary UserRankingSummary
	query := DB.Table("quota_data").
		Select("COALESCE(sum(count), 0) as total_requests, COALESCE(sum(quota), 0) as total_quota, COALESCE(sum(token_used), 0) as total_tokens")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Scan(&summary).Error
	return &summary, err
}
