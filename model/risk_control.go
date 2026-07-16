package model

import (
	"time"
)

// 风控排行榜/下钻查询。全部实时聚合 logs 表(走 LOG_DB),不落新表。
// 三库兼容要点:
//   - count(distinct x)、min/max 三库均支持;
//   - HAVING / ORDER BY 一律写完整聚合表达式,不引用 SELECT 别名
//     (PostgreSQL 的 HAVING 不支持别名);
//   - ip / ua / user_id / username 均非保留字,无需 quote。

const (
	riskMaxWindowHours     = 24 * 7 // 统计窗口上限 7 天,防止全表扫描
	riskDefaultWindowHours = 24
	riskDefaultLimit       = 50
	riskMaxLimit           = 500
)

// 风控排行榜维度标识,供 controller 路由分发使用。
const (
	RiskMetricIpMultiUser = "ip_multi_user"
	RiskMetricUserMultiIp = "user_multi_ip"
	RiskMetricUa          = "ua"
)

// IpRankItem 单 IP 关联多用户排行项。
type IpRankItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	UserCount    int    `json:"user_count" gorm:"column:user_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// UserRankItem 单用户使用多 IP 排行项。
type UserRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	IpCount      int    `json:"ip_count" gorm:"column:ip_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// UaRankItem UA 排行项。
type UaRankItem struct {
	Ua           string `json:"ua" gorm:"column:ua"`
	UserCount    int    `json:"user_count" gorm:"column:user_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// RiskUserDetailItem 某 IP 下钻出的关联用户明细。
type RiskUserDetailItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskIpDetailItem 某用户下钻出的 IP 明细。
type RiskIpDetailItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// normalizeRiskWindow 归一化统计窗口(小时)。
func normalizeRiskWindow(hours int) int64 {
	if hours <= 0 {
		hours = riskDefaultWindowHours
	}
	if hours > riskMaxWindowHours {
		hours = riskMaxWindowHours
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}

func normalizeRiskLimit(limit int) int {
	if limit <= 0 {
		limit = riskDefaultLimit
	}
	if limit > riskMaxLimit {
		limit = riskMaxLimit
	}
	return limit
}

// riskLogTypes 参与风控统计的日志类型(消费 + 错误)。
func riskLogTypes() []int {
	return []int{LogTypeConsume, LogTypeError}
}

// GetIpMultiUserRanking 返回窗口内关联用户数最多的 IP(仅含关联 >1 用户的 IP)。
func GetIpMultiUserRanking(hours int, limit int) ([]IpRankItem, error) {
	start := normalizeRiskWindow(hours)
	limit = normalizeRiskLimit(limit)

	var items []IpRankItem
	err := LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Group("ip").
		Having("count(distinct user_id) > ?", 1).
		Order("user_count desc, request_count desc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// GetUserMultiIpRanking 返回窗口内使用 IP 数最多的用户(仅含使用 >1 IP 的用户)。
func GetUserMultiIpRanking(hours int, limit int) ([]UserRankItem, error) {
	start := normalizeRiskWindow(hours)
	limit = normalizeRiskLimit(limit)

	var items []UserRankItem
	err := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(distinct ip) as ip_count, count(*) as request_count").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Group("user_id").
		Having("count(distinct ip) > ?", 1).
		Order("ip_count desc, request_count desc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// GetUaRanking 返回窗口内请求数最多的 UA。
func GetUaRanking(hours int, limit int) ([]UaRankItem, error) {
	start := normalizeRiskWindow(hours)
	limit = normalizeRiskLimit(limit)

	var items []UaRankItem
	err := LOG_DB.Table("logs").
		Select("ua, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ua <> ''").
		Group("ua").
		Order("request_count desc, user_count desc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// GetIpUserDetail 下钻:某 IP 在窗口内关联的用户明细。
func GetIpUserDetail(ip string, hours int) ([]RiskUserDetailItem, error) {
	start := normalizeRiskWindow(hours)

	var items []RiskUserDetailItem
	err := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ip = ?", ip).
		Group("user_id").
		Order("request_count desc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetUserIpDetail 下钻:某用户在窗口内使用的 IP 明细。
func GetUserIpDetail(userId int, hours int) ([]RiskIpDetailItem, error) {
	start := normalizeRiskWindow(hours)

	var items []RiskIpDetailItem
	err := LOG_DB.Table("logs").
		Select("ip, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("user_id = ?", userId).
		Where("ip <> ''").
		Group("ip").
		Order("request_count desc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetIpAssociatedUserIds 返回某 IP 在窗口内关联的全部用户 ID(供自动封禁使用)。
func GetIpAssociatedUserIds(ip string, hours int) ([]int, error) {
	start := normalizeRiskWindow(hours)

	var userIds []int
	err := LOG_DB.Table("logs").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ip = ?", ip).
		Where("user_id > 0").
		Distinct().
		Pluck("user_id", &userIds).Error
	return userIds, err
}
