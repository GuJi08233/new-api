package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 风控管理台的聚合查询:活动趋势、拦截来源与规则排行、禁用账号列表、负责人候选。
// 风控事件在主库,调用日志在日志库,这里的查询都不跨库连接。

// RiskActivityBucketSeconds 是活动趋势与对象时间线的分桶粒度。按 UTC 整点分桶,
// 时区为整小时偏移时前端可以无损地合并成本地的更粗粒度。
const RiskActivityBucketSeconds = 3600

// riskBucketExpr 返回把 created_at 截到整点的表达式。取模在三库(以及日志库可选的 ClickHouse)
// 都是整数运算;粒度以字面量内联,保证 SELECT 与 GROUP BY 是同一个表达式(PostgreSQL 要求两者一致)。
func riskBucketExpr() string {
	return fmt.Sprintf("created_at - created_at %% %d", RiskActivityBucketSeconds)
}

// 风控事件的大类,事件列表的快捷筛选与概览统计共用。
const (
	RiskEventCategoryBlock = "block"
	RiskEventCategoryBan   = "ban"
	RiskEventCategoryUnban = "unban"
	RiskEventCategoryAlert = "alert"
)

var riskEventCategoryTypes = map[string][]string{
	RiskEventCategoryBlock: {RiskEventBlockIp, RiskEventBlockUa, RiskEventBlockClientUa},
	RiskEventCategoryBan:   {RiskEventBanIp, RiskEventBanAuto, RiskEventBanManual},
	RiskEventCategoryUnban: {RiskEventUnbanIp, RiskEventUnban, RiskEventUnbanAuto},
	RiskEventCategoryAlert: {RiskEventAlert},
}

// RiskEventCategoryTypes 返回事件大类包含的事件类型,大类无效时返回 nil。
func RiskEventCategoryTypes(category string) []string {
	return riskEventCategoryTypes[category]
}

// RiskActivityBucket 是一个整点内某类事件的落库条数与累计命中次数。
type RiskActivityBucket struct {
	Bucket    int64  `json:"bucket"`
	EventType string `json:"event_type"`
	Records   int64  `json:"records"`
	Hits      int64  `json:"hits"`
}

// GetRiskActivityBuckets 按整点与事件类型统计 since 之后的风控事件。
// 拦截事件按来源聚合落库,Hits 是累计命中次数,Records 是落库条数。
func GetRiskActivityBuckets(since int64) ([]RiskActivityBucket, error) {
	bucket := riskBucketExpr()
	var items []RiskActivityBucket
	err := DB.Model(&RiskEvent{}).
		Select(bucket+" AS bucket, event_type, count(*) AS records, COALESCE(sum(count), 0) AS hits").
		Where("created_at >= ?", since).
		Group(bucket).Group("event_type").
		Order(bucket + " asc").
		Find(&items).Error
	return items, err
}

// RiskTopBlockedIp 是窗口内被拦截最多的来源地址。
type RiskTopBlockedIp struct {
	Ip      string `json:"ip"`
	Hits    int64  `json:"hits"`
	Records int64  `json:"records"`
	LastAt  int64  `json:"last_at"`
}

// GetRiskTopBlockedIps 返回 since 之后拦截次数最多的来源地址。
func GetRiskTopBlockedIps(since int64, limit int) ([]RiskTopBlockedIp, error) {
	var items []RiskTopBlockedIp
	err := DB.Model(&RiskEvent{}).
		Select("ip, COALESCE(sum(count), 0) AS hits, count(*) AS records, max(created_at) AS last_at").
		Where("created_at >= ?", since).
		Where("event_type IN ?", riskEventCategoryTypes[RiskEventCategoryBlock]).
		Where("ip <> ''").
		Group("ip").
		Order("COALESCE(sum(count), 0) desc, ip asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// RiskDynamicBanRule 是动态封禁拦截在规则排行里的合并标识。这类拦截事件的规则是
// "ban:<目标>",逐个目标排行没有意义,合并成一行"动态 IP 封禁"。
const RiskDynamicBanRule = "ban:*"

// RiskTopRule 是窗口内最活跃的规则或防护来源。
type RiskTopRule struct {
	EventType string `json:"event_type"`
	RuleId    string `json:"rule_id"`
	Rule      string `json:"rule"`
	Hits      int64  `json:"hits"`
	Records   int64  `json:"records"`
	LastAt    int64  `json:"last_at"`
}

// GetRiskTopRules 返回 since 之后命中最多的规则:拦截、告警与自动处置事件按 (类型, 规则) 统计,
// 不含管理员手动处置与解封。
func GetRiskTopRules(since int64, limit int) ([]RiskTopRule, error) {
	ruleExpr := fmt.Sprintf("CASE WHEN event_type = '%s' AND rule LIKE 'ban:%%' THEN '%s' ELSE COALESCE(rule, '') END",
		RiskEventBlockIp, RiskDynamicBanRule)
	types := append(append([]string{}, riskEventCategoryTypes[RiskEventCategoryBlock]...),
		RiskEventAlert, RiskEventBanAuto, RiskEventBanIp)
	var items []RiskTopRule
	err := DB.Model(&RiskEvent{}).
		Select("event_type, COALESCE(rule_id, '') AS rule_id, "+ruleExpr+" AS rule, "+
			"COALESCE(sum(count), 0) AS hits, count(*) AS records, max(created_at) AS last_at").
		Where("created_at >= ?", since).
		Where("event_type IN ?", types).
		Where("COALESCE(rule, '') <> ?", RiskBanSourceManual).
		Group("event_type").Group("COALESCE(rule_id, '')").Group(ruleExpr).
		Order("COALESCE(sum(count), 0) desc, event_type asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

// RiskTimelineBucket 是下钻对象在一个整点内的调用与错误次数。
type RiskTimelineBucket struct {
	Bucket   int64 `json:"bucket"`
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
}

// GetRiskDetailTimeline 下钻:目标在窗口内按整点的调用与错误次数,口径与其他明细一致。
func GetRiskDetailTimeline(target RiskDetailTarget) ([]RiskTimelineBucket, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}
	bucket := riskBucketExpr()
	var items []RiskTimelineBucket
	err = tx.Select(bucket + " AS bucket, count(*) AS requests, " + riskErrorCountExpr + " AS errors").
		Group(bucket).
		Order(bucket + " asc").
		Find(&items).Error
	return items, err
}

// RiskDisabledUser 是一个当前被禁用的账号,附带最近一次禁用事件。
type RiskDisabledUser struct {
	Id               int    `json:"id"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Role             int    `json:"role"`
	DisableReason    string `json:"disable_reason"`
	DisableExpiresAt int64  `json:"disable_expires_at"`
	LastBanAt        int64  `json:"last_ban_at" gorm:"-"`
	LastBanType      string `json:"last_ban_type" gorm:"-"`
	LastBanRule      string `json:"last_ban_rule" gorm:"-"`
	LastBanOperator  int    `json:"last_ban_operator_id" gorm:"-"`
}

// 禁用账号列表的期限筛选。
const (
	RiskDisabledKindTemporary = "temporary"
	RiskDisabledKindPermanent = "permanent"
)

type RiskDisabledUserQuery struct {
	Keyword string
	Kind    string // temporary | permanent | 空表示全部
	Offset  int
	Limit   int
}

// GetRiskDisabledUsers 分页列出当前被禁用的账号。临时禁用按到期时间先后排列,便于查看即将恢复的账号;
// 其余按账号 ID 倒序。最近一次禁用事件只为当前页读取。
func GetRiskDisabledUsers(query RiskDisabledUserQuery) ([]RiskDisabledUser, int64, error) {
	tx := DB.Model(&User{}).Where("status = ?", common.UserStatusDisabled)
	switch query.Kind {
	case RiskDisabledKindTemporary:
		tx = tx.Where("disable_expires_at > 0")
	case RiskDisabledKindPermanent:
		tx = tx.Where("COALESCE(disable_expires_at, 0) = 0")
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := riskKeywordPattern(strings.ToLower(keyword))
		if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
			tx = tx.Where("id = ? OR LOWER(username) LIKE ? ESCAPE '!' OR LOWER(display_name) LIKE ? ESCAPE '!'", id, pattern, pattern)
		} else {
			tx = tx.Where("LOWER(username) LIKE ? ESCAPE '!' OR LOWER(display_name) LIKE ? ESCAPE '!'", pattern, pattern)
		}
	}
	var total int64
	if err := tx.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "id desc"
	if query.Kind == RiskDisabledKindTemporary {
		order = "disable_expires_at asc, id desc"
	}
	var users []RiskDisabledUser
	if err := tx.Select("id", "username", "display_name", "role", "disable_reason", "disable_expires_at").
		Order(order).Offset(query.Offset).Limit(query.Limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	if len(users) == 0 {
		return users, total, nil
	}
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	// 先按账号取最近一条禁用事件的 ID,再读这些事件:单个账号的大量历史事件不会挤掉其他账号。
	var latestIds []int
	if err := DB.Model(&RiskEvent{}).Select("max(id)").
		Where("user_id IN ?", ids).
		Where("event_type IN ?", []string{RiskEventBanAuto, RiskEventBanManual}).
		Group("user_id").
		Pluck("max(id)", &latestIds).Error; err != nil {
		return nil, 0, err
	}
	latest := make(map[int]RiskEvent, len(latestIds))
	if len(latestIds) > 0 {
		var events []RiskEvent
		if err := DB.Select("user_id", "event_type", "rule", "operator_id", "created_at").
			Where("id IN ?", latestIds).Find(&events).Error; err != nil {
			return nil, 0, err
		}
		for _, event := range events {
			latest[event.UserId] = event
		}
	}
	for index := range users {
		if event, ok := latest[users[index].Id]; ok {
			users[index].LastBanAt, users[index].LastBanType = event.CreatedAt, event.EventType
			users[index].LastBanRule, users[index].LastBanOperator = event.Rule, event.OperatorId
		}
	}
	return users, total, nil
}

// RiskAssignee 是可以负责风控事件的管理员。
type RiskAssignee struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        int    `json:"role"`
}

// GetRiskAssignees 返回已启用的管理员,事件分配与操作人显示共用。
func GetRiskAssignees() ([]RiskAssignee, error) {
	var items []RiskAssignee
	err := DB.Model(&User{}).Select("id", "username", "display_name", "role").
		Where("role >= ? AND status = ?", common.RoleAdminUser, common.UserStatusEnabled).
		Order("id asc").Limit(200).Find(&items).Error
	return items, err
}

// GetRecentBanBlockHits 返回各封禁目标 since 之后拦截的请求次数。动态封禁的拦截事件
// 记在 block_ip 下,规则为 "ban:<目标>"。只统计传入的目标(一页封禁记录)。
func GetRecentBanBlockHits(targets []string, since int64) (map[string]int64, error) {
	hits := make(map[string]int64, len(targets))
	if len(targets) == 0 {
		return hits, nil
	}
	rules := make([]string, 0, len(targets))
	for _, target := range targets {
		rules = append(rules, "ban:"+target)
	}
	var rows []struct {
		Rule string
		Hits int64
	}
	if err := DB.Model(&RiskEvent{}).Select("rule, COALESCE(sum(count), 0) AS hits").
		Where("event_type = ?", RiskEventBlockIp).
		Where("created_at >= ?", since).
		Where("rule IN ?", rules).
		Group("rule").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		hits[strings.TrimPrefix(row.Rule, "ban:")] = row.Hits
	}
	return hits, nil
}

// RiskEventReviewTarget 指定批量处理中的一条事件及其读取时的版本。
type RiskEventReviewTarget struct {
	Id      int `json:"id"`
	Version int `json:"version"`
}

// RiskEventReviewResult 是批量处理中一条事件的结果。
type RiskEventReviewResult struct {
	Id      int        `json:"id"`
	Success bool       `json:"success"`
	Message string     `json:"message,omitempty"`
	Event   *RiskEvent `json:"event,omitempty"`
}

// ReviewRiskEventsBatch 逐条处理事件,每条单独提交并检查版本:一条被别人改过只让这一条失败。
// status 为空串、note 或 assigneeId 为 nil 时保留每条事件原来的值,但至少要改一项。
func ReviewRiskEventsBatch(targets []RiskEventReviewTarget, status string, note *string, assigneeId *int, operatorId int) ([]RiskEventReviewResult, error) {
	if len(targets) == 0 || len(targets) > 100 {
		return nil, errors.New("批量处理每次支持 1 到 100 条事件")
	}
	if (status != "" && !IsValidRiskReviewStatus(status)) || (note != nil && utf8.RuneCountInString(*note) > 2000) ||
		(assigneeId != nil && *assigneeId < 0) || (status == "" && note == nil && assigneeId == nil) {
		return nil, errors.New("事件处理参数无效")
	}
	results := make([]RiskEventReviewResult, 0, len(targets))
	seen := make(map[int]bool, len(targets))
	for _, target := range targets {
		if seen[target.Id] {
			continue
		}
		seen[target.Id] = true
		event, err := reviewRiskEvent(target.Id, target.Version, status, note, assigneeId, operatorId)
		if err != nil {
			message := err.Error()
			if errors.Is(err, gorm.ErrRecordNotFound) {
				message = "事件不存在"
			}
			results = append(results, RiskEventReviewResult{Id: target.Id, Message: message})
			continue
		}
		results = append(results, RiskEventReviewResult{Id: target.Id, Success: true, Event: event})
	}
	return results, nil
}
