package model

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
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
// user_overview / ip_overview 是把多个单指标榜合并成一行的总览视图;
// 其余单指标标识保留,自动封禁扫描与旧前端仍在使用。
const (
	RiskMetricIpMultiUser     = "ip_multi_user"
	RiskMetricUserMultiIp     = "user_multi_ip"
	RiskMetricUa              = "ua"
	RiskMetricIpMultiToken    = "ip_multi_token"
	RiskMetricUserTinyRequest = "user_tiny_request"
	RiskMetricUserErrorBurst  = "user_error_burst"
	RiskMetricTokenMultiIp    = "token_multi_ip"
	RiskMetricUserOverview    = "user_overview"
	RiskMetricIpOverview      = "ip_overview"
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

// IpTokenRankItem 单 IP 使用多令牌排行项(批量测活的典型特征)。
type IpTokenRankItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	TokenCount   int    `json:"token_count" gorm:"column:token_count"`
	UserCount    int    `json:"user_count" gorm:"column:user_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// UserTinyRequestRankItem 用户微量请求排行项(自动测活的典型特征)。
type UserTinyRequestRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	TokenCount   int    `json:"token_count" gorm:"column:token_count"`
}

// UserErrorRankItem 用户错误请求排行项。
type UserErrorRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// TokenIpRankItem 单令牌被多 IP 使用排行项(密钥泄露/倒卖的典型特征)。
type TokenIpRankItem struct {
	TokenId      int    `json:"token_id" gorm:"column:token_id"`
	TokenName    string `json:"token_name" gorm:"column:token_name"`
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	IpCount      int    `json:"ip_count" gorm:"column:ip_count"`
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

// riskExcludeUsers 把全局白名单账号产生的日志行从风控统计中剔除。
// 这些账号的流量既不该让自己上榜被处置,也不该把自己使用的 IP 顶上 IP 维度排行,
// 否则运营者自己的出口地址会被自动封禁,连带拦下同出口的其他正常用户。
// 空切片必须短路:GORM 会把空切片渲染成 NOT IN (NULL),那会过滤掉全部行。
func riskExcludeUsers(tx *gorm.DB, userIds []int) *gorm.DB {
	if len(userIds) == 0 {
		return tx
	}
	return tx.Where("user_id NOT IN ?", userIds)
}

// GetIpMultiUserRanking 返回窗口内的全部 IP，按关联用户数排行。
func GetIpMultiUserRanking(hours int, limit int, excludeUserIds []int) ([]IpRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []IpRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("ip").
		Order("user_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserMultiIpRanking 返回窗口内使用 IP 数最多的用户(仅含使用 >1 IP 的用户)。
func GetUserMultiIpRanking(hours int, limit int, excludeUserIds []int) ([]UserRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(distinct ip) as ip_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []UserRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Having("count(distinct ip) > ?", 1).
		Order("ip_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetIpMultiTokenRanking 返回窗口内使用令牌数最多的 IP(批量测活检测)。
// 只统计带令牌的请求(token_id > 0),消费与错误日志均计入,
// 保证测活失败(密钥无效)的请求同样被观测到。
func GetIpMultiTokenRanking(hours int, limit int, excludeUserIds []int) ([]IpTokenRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("ip, count(distinct token_id) as token_count, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("token_id > 0")

	var items []IpTokenRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("ip").
		Order("token_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserTinyRequestRanking 返回窗口内微量请求数最多的用户(自动测活检测)。
// 微量请求指 prompt 与 completion tokens 均不超过 maxTokens 的成功消费请求;
// 该统计不依赖 IP/UA 日志开关,token 数始终入库。
func GetUserTinyRequestRanking(hours int, limit int, maxTokens int, excludeUserIds []int) ([]UserTinyRequestRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(*) as request_count, count(distinct token_id) as token_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type = ?", LogTypeConsume).
		Where("user_id > 0").
		Where("prompt_tokens <= ?", maxTokens).
		Where("completion_tokens <= ?", maxTokens)

	var items []UserTinyRequestRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Order("request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserErrorRanking 返回窗口内错误请求数最多的用户。
func GetUserErrorRanking(hours int, limit int, excludeUserIds []int) ([]UserErrorRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type = ?", LogTypeError).
		Where("user_id > 0")

	var items []UserErrorRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Order("request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetTokenMultiIpRanking 返回窗口内被最多不同 IP 使用的令牌(仅含 >1 IP 的令牌)。
// 单令牌短期被大量 IP 使用是密钥泄露或倒卖的典型特征。
func GetTokenMultiIpRanking(hours int, limit int, excludeUserIds []int) ([]TokenIpRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("token_id, max(token_name) as token_name, max(user_id) as user_id, max(username) as username, count(distinct ip) as ip_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("token_id > 0")

	var items []TokenIpRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("token_id").
		Having("count(distinct ip) > ?", 1).
		Order("ip_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// 合并统计项:原先分散在多个排行榜的指标聚合到一行,便于交叉判读
// (例如 IP 数高而令牌数低,说明单个令牌被多 IP 使用,疑似密钥泄露)。
// 三个维度共用同一组数值口径,具体的 UA、错误状态码分布走下钻详情。

// UserOverviewItem 是用户维度的合并统计项。
type UserOverviewItem struct {
	UserId           int    `json:"user_id" gorm:"column:user_id"`
	Username         string `json:"username" gorm:"column:username"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	IpCount          int    `json:"ip_count" gorm:"column:ip_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// IpOverviewItem 是 IP 维度的合并统计项。
type IpOverviewItem struct {
	Ip               string `json:"ip" gorm:"column:ip"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	UserCount        int    `json:"user_count" gorm:"column:user_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// UaOverviewItem 是 UA 维度的合并统计项:同一客户端标识被多少人、多少 IP 使用。
type UaOverviewItem struct {
	Ua               string `json:"ua" gorm:"column:ua"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	UserCount        int    `json:"user_count" gorm:"column:user_count"`
	IpCount          int    `json:"ip_count" gorm:"column:ip_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskOverviewQuery 是合并排行的查询参数。
type RiskOverviewQuery struct {
	Hours          int
	Limit          int
	TinyMaxTokens  int    // 微量请求判定阈值(输入与输出 tokens 均不超过该值)
	SortBy         string // 排序字段,不在白名单内时回退请求数
	SortOrder      string // asc | desc,默认 desc
	ExcludeUserIds []int  // 排除这些用户产生的日志行(风控白名单)
}

// riskOverviewDefaultSort 是合并排行的默认排序字段。
const riskOverviewDefaultSort = "request_count"

// 条件计数一律用 count(case when ... then 1 end):CASE 在条件不成立时返回 NULL 而被
// count 忽略,三库语义一致且返回类型确定为整数(sum 在 MySQL 下返回 DECIMAL)。
const (
	riskDistinctIpExpr    = "count(distinct case when ip <> '' then ip end)"
	riskDistinctUserExpr  = "count(distinct case when user_id > 0 then user_id end)"
	riskDistinctTokenExpr = "count(distinct case when token_id > 0 then token_id end)"
)

// riskErrorCountExpr 统计窗口内的错误请求数。
var riskErrorCountExpr = fmt.Sprintf("count(case when type = %d then 1 end)", LogTypeError)

// riskTinyRequestCountExpr 返回微量请求的条件计数表达式。
// maxTokens 先夹到合法区间再内联,保证进入 SQL 的始终是小整数。
func riskTinyRequestCountExpr(maxTokens int) string {
	if maxTokens < 1 {
		maxTokens = operation_setting.RiskDefaultTinyRequestMaxTokens
	} else if maxTokens > operation_setting.RiskMaxTinyRequestMaxTokens {
		maxTokens = operation_setting.RiskMaxTinyRequestMaxTokens
	}
	return fmt.Sprintf("count(case when type = %d and prompt_tokens <= %d and completion_tokens <= %d then 1 end)",
		LogTypeConsume, maxTokens, maxTokens)
}

// riskOverviewSharedSelects 返回三个维度共有的聚合列(不含分组列与维度专属的去重计数)。
func riskOverviewSharedSelects(tinyExpr string) []string {
	return []string{
		"count(*) as request_count",
		riskDistinctTokenExpr + " as token_count",
		tinyExpr + " as tiny_request_count",
		riskErrorCountExpr + " as error_count",
		"min(created_at) as first_seen",
		"max(created_at) as last_seen",
	}
}

// riskOverviewSharedSortExprs 返回三个维度共有的可排序字段映射。
// 调用方再补上自己维度专属的字段(ip_count / user_count)。
func riskOverviewSharedSortExprs(tinyExpr string) map[string]string {
	return map[string]string{
		"request_count":      "count(*)",
		"token_count":        riskDistinctTokenExpr,
		"tiny_request_count": tinyExpr,
		"error_count":        riskErrorCountExpr,
		"first_seen":         "min(created_at)",
		"last_seen":          "max(created_at)",
	}
}

// riskOverviewOrderBy 按白名单映射解析排序字段并拼出完整的 ORDER BY 表达式。
// sortBy 不在映射内时回退请求数,因此外部传入的任意字符串都不会进入 SQL;
// 表达式而非 SELECT 别名与本文件其余查询保持一致。tieBreaker 让同值行的顺序稳定。
func riskOverviewOrderBy(exprs map[string]string, sortBy string, sortOrder string, tieBreaker string) string {
	expr, ok := exprs[sortBy]
	if !ok {
		expr = exprs[riskOverviewDefaultSort]
	}
	direction := "desc"
	if strings.EqualFold(sortOrder, "asc") {
		direction = "asc"
	}
	return expr + " " + direction + ", " + tieBreaker + " asc"
}

// GetUserOverviewRanking 返回窗口内的用户合并排行。
// 一条 SQL 聚合出全部指标,保证按任一字段取 top N 时同行的其余指标都是真实值。
func GetUserOverviewRanking(query RiskOverviewQuery) ([]UserOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["ip_count"] = riskDistinctIpExpr

	selects := append([]string{
		"user_id",
		"max(username) as username",
		riskDistinctIpExpr + " as ip_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("user_id > 0")

	var items []UserOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("user_id").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "user_id")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// GetIpOverviewRanking 返回窗口内的 IP 合并排行。
// ExcludeUserIds 排除白名单用户产生的日志行:纯由白名单用户使用的 IP 因此完全不上榜,
// 混合使用的 IP 只统计非白名单部分。
func GetIpOverviewRanking(query RiskOverviewQuery) ([]IpOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["user_count"] = riskDistinctUserExpr

	selects := append([]string{
		"ip",
		riskDistinctUserExpr + " as user_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []IpOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("ip").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "ip")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// GetUaOverviewRanking 返回窗口内的 UA 合并排行:同一客户端标识被多少用户、多少 IP 使用。
// 同一 UA 覆盖大量用户与 IP 通常是脚本或代理工具,也是配置 UA 黑名单的依据。
func GetUaOverviewRanking(query RiskOverviewQuery) ([]UaOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["user_count"] = riskDistinctUserExpr
	sortExprs["ip_count"] = riskDistinctIpExpr

	selects := append([]string{
		"ua",
		riskDistinctUserExpr + " as user_count",
		riskDistinctIpExpr + " as ip_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ua <> ''")

	var items []UaOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("ua").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "ua")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// 下钻详情:围绕单个目标(用户 / IP / UA)展开它的关联对象与错误分布。
// 排行榜只放数值,具体的 UA 文本、错误状态码这类明细都在这里查。

// 下钻目标类型。
const (
	RiskDetailTypeUser = "user"
	RiskDetailTypeIp   = "ip"
	RiskDetailTypeUa   = "ua"
)

// RiskUaDetailItem 某目标使用的 UA 明细。
type RiskUaDetailItem struct {
	Ua           string `json:"ua" gorm:"column:ua"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskErrorStatusItem 某目标的错误状态码分布项。
type RiskErrorStatusItem struct {
	StatusCode int    `json:"status_code"`
	ErrorCode  string `json:"error_code"`
	Count      int    `json:"count"`
}

// RiskDetailTarget 指定下钻的目标。
type RiskDetailTarget struct {
	Type           string // user | ip | ua
	Value          string // 用户 ID / IP / UA 原文
	Hours          int
	ExcludeUserIds []int // 与排行榜口径一致地排除白名单用户
}

// riskDetailQuery 构造限定到单个下钻目标的基础查询。
func riskDetailQuery(target RiskDetailTarget) (*gorm.DB, error) {
	tx := LOG_DB.Table("logs").
		Where("created_at >= ?", normalizeRiskWindow(target.Hours)).
		Where("type IN ?", riskLogTypes())

	switch target.Type {
	case RiskDetailTypeUser:
		userId, err := strconv.Atoi(strings.TrimSpace(target.Value))
		if err != nil || userId <= 0 {
			return nil, errors.New("无效的用户 ID")
		}
		tx = tx.Where("user_id = ?", userId)
	case RiskDetailTypeIp:
		if strings.TrimSpace(target.Value) == "" {
			return nil, errors.New("IP 不能为空")
		}
		tx = tx.Where("ip = ?", target.Value)
	case RiskDetailTypeUa:
		if target.Value == "" {
			return nil, errors.New("UA 不能为空")
		}
		tx = tx.Where("ua = ?", target.Value)
	default:
		return nil, errors.New("无效的下钻类型")
	}

	return riskExcludeUsers(tx, target.ExcludeUserIds), nil
}

// GetRiskDetailUsers 下钻:目标关联的用户明细。
func GetRiskDetailUsers(target RiskDetailTarget) ([]RiskUserDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskUserDetailItem
	err = tx.Select("user_id, max(username) as username, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("user_id > 0").
		Group("user_id").
		Order("count(*) desc, user_id asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetRiskDetailIps 下钻:目标使用的 IP 明细。
func GetRiskDetailIps(target RiskDetailTarget) ([]RiskIpDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskIpDetailItem
	err = tx.Select("ip, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("ip <> ''").
		Group("ip").
		Order("count(*) desc, ip asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetRiskDetailUas 下钻:目标使用的 UA 明细。
func GetRiskDetailUas(target RiskDetailTarget) ([]RiskUaDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskUaDetailItem
	err = tx.Select("ua, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("ua <> ''").
		Group("ua").
		Order("count(*) desc, ua asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// riskErrorSampleLimit 错误状态码分布的采样条数上限。
const riskErrorSampleLimit = 2000

// GetRiskDetailErrorStatuses 下钻:目标的错误状态码分布,按出现次数降序。
// status_code 与 error_code 存放在 logs.other 的 JSON 里,而三库的 JSON 函数互不兼容,
// 因此取最近若干条错误日志在应用层解析聚合。第二个返回值表示样本已达上限,
// 即分布基于最近的样本而非窗口内全量错误。
func GetRiskDetailErrorStatuses(target RiskDetailTarget) ([]RiskErrorStatusItem, bool, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, false, err
	}

	var payloads []string
	if err := tx.Where("type = ?", LogTypeError).
		Order("created_at desc, id desc").
		Limit(riskErrorSampleLimit).
		Pluck("other", &payloads).Error; err != nil {
		return nil, false, err
	}

	grouped := map[string]*RiskErrorStatusItem{}
	for _, raw := range payloads {
		if raw == "" {
			continue
		}
		var parsed struct {
			StatusCode int    `json:"status_code"`
			ErrorCode  string `json:"error_code"`
		}
		if common.UnmarshalJsonStr(raw, &parsed) != nil {
			continue
		}
		if parsed.StatusCode == 0 && parsed.ErrorCode == "" {
			continue
		}
		key := strconv.Itoa(parsed.StatusCode) + "\x00" + parsed.ErrorCode
		if item, ok := grouped[key]; ok {
			item.Count++
			continue
		}
		grouped[key] = &RiskErrorStatusItem{
			StatusCode: parsed.StatusCode,
			ErrorCode:  parsed.ErrorCode,
			Count:      1,
		}
	}

	items := make([]RiskErrorStatusItem, 0, len(grouped))
	for _, item := range grouped {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		if items[i].StatusCode != items[j].StatusCode {
			return items[i].StatusCode < items[j].StatusCode
		}
		return items[i].ErrorCode < items[j].ErrorCode
	})
	return items, len(payloads) >= riskErrorSampleLimit, nil
}

// riskWhitelistIpSampleLimit 拉取白名单账号近期地址的条数上限。
// 白名单是运营者自用的少数账号,正常情况远达不到上限;触顶说明白名单里放了
// 高流量账号,此时豁免判定可能不完整,调用方会记录日志。
const riskWhitelistIpSampleLimit = 1000

// GetRecentIpsByUsers 返回窗口内这些用户使用过的去重 IP。
// 自动封禁用它判断封禁目标(单地址或 IPv6 归并后的前缀)是否覆盖全局白名单账号。
// 之所以取回地址在应用层比对而不是用 SQL 判定:封禁目标可能是 CIDR,
// 而三库都没有可移植的网段包含运算。第二个返回值表示已达采样上限。
func GetRecentIpsByUsers(hours int, userIds []int) ([]string, bool, error) {
	if len(userIds) == 0 {
		return nil, false, nil
	}

	var ips []string
	err := LOG_DB.Table("logs").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("user_id IN ?", userIds).
		Distinct().
		Limit(riskWhitelistIpSampleLimit).
		Pluck("ip", &ips).Error
	return ips, len(ips) >= riskWhitelistIpSampleLimit, err
}

// HasIpLogsSince 判断某 IP 在 since 之后是否还有风控统计口径内的日志行,
// excludeUserIds 与排行统计一样剔除全局白名单账号的行。
// 扫描规则用它区分「上次封禁之后仍在活动」与「同一批旧证据被再次扫到」。
func HasIpLogsSince(ip string, since int64, excludeUserIds []int) (bool, error) {
	tx := LOG_DB.Table("logs").
		Where("ip = ?", ip).
		Where("created_at > ?", since).
		Where("type IN ?", riskLogTypes())

	var ids []int
	err := riskExcludeUsers(tx, excludeUserIds).Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
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
