package model

import (
	"errors"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 风控事件表:持久化黑名单拦截、自动/手动封禁、解禁与告警记录,存主库。
// 拦截类事件由 service 层按 (类型+用户+IP+规则) 聚合限流后写入,Count 为窗口内累计次数;
// 封禁/解禁事件逐条直写。拦截与告警事件按保留天数周期清理,封禁与解禁记录永久保留。

const (
	RiskEventBlockUa       = "block_ua"        // UA 黑名单拦截
	RiskEventBlockIp       = "block_ip"        // IP 黑名单/动态封禁拦截
	RiskEventBlockClientUa = "block_client_ua" // 客户端 UA 准入规则拦截
	RiskEventBanAuto       = "ban_auto"        // 风控自动封禁用户
	RiskEventBanManual     = "ban_manual"      // 管理员手动封禁用户
	RiskEventUnban         = "unban"           // 管理员解除用户封禁
	RiskEventUnbanAuto     = "unban_auto"      // 账号的临时禁用到期,后台自动恢复
	RiskEventBanIp         = "ban_ip"          // IP 被封禁(自动升级或手动添加)
	RiskEventUnbanIp       = "unban_ip"        // IP 封禁被解除
	RiskEventAlert         = "alert"           // 自动规则命中告警(动作为仅告警)
)

// IsValidRiskEventType 判断字符串是否为已支持的风控事件类型。
func IsValidRiskEventType(eventType string) bool {
	switch eventType {
	case RiskEventBlockUa, RiskEventBlockIp, RiskEventBlockClientUa, RiskEventBanAuto,
		RiskEventBanManual, RiskEventUnban, RiskEventUnbanAuto,
		RiskEventBanIp, RiskEventUnbanIp, RiskEventAlert:
		return true
	}
	return false
}

type RiskEvent struct {
	Id            int               `json:"id" gorm:"primaryKey"`
	CreatedAt     int64             `json:"created_at" gorm:"bigint;index:idx_risk_events_created_at"`
	EventType     string            `json:"event_type" gorm:"type:varchar(32);index:idx_risk_events_type"`
	UserId        int               `json:"user_id" gorm:"index:idx_risk_events_user_id"`
	Username      string            `json:"username" gorm:"type:varchar(64)"`
	Ip            string            `json:"ip" gorm:"type:varchar(64);index:idx_risk_events_ip"`
	Ua            string            `json:"ua" gorm:"type:varchar(512)"`
	Rule          string            `json:"rule" gorm:"type:varchar(255)"`
	Reason        string            `json:"reason" gorm:"type:varchar(512)"`
	Count         int               `json:"count"`
	OperatorId    int               `json:"operator_id"`
	RuleId        string            `json:"rule_id" gorm:"type:varchar(64);index:idx_risk_events_rule_id"`
	RuleSnapshot  string            `json:"-" gorm:"type:text"`
	Evidence      *RiskRuleEvidence `json:"evidence,omitempty" gorm:"-"`
	ReviewStatus  string            `json:"review_status" gorm:"type:varchar(16);index:idx_risk_events_review_status"`
	ReviewNote    string            `json:"review_note" gorm:"type:text"`
	AssigneeId    int               `json:"assignee_id" gorm:"index:idx_risk_events_assignee_id"`
	ReviewedBy    int               `json:"reviewed_by"`
	ReviewedAt    int64             `json:"reviewed_at" gorm:"bigint"`
	ReviewVersion int               `json:"review_version"`
}

// RiskRuleEvidence 保存命中时的规则与观测值，不随之后的配置修改而改变。
type RiskRuleEvidence struct {
	RuleId        string `json:"rule_id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Metric        string `json:"metric"`
	WindowSeconds int    `json:"window_seconds"`
	Threshold     int    `json:"threshold"`
	Observed      int    `json:"observed"`
	Comparison    string `json:"comparison"`
	Action        string `json:"action"`
	DryRun        bool   `json:"dry_run"`
	BanMinutes    int    `json:"ban_minutes"`
	TinyMaxTokens int    `json:"tiny_max_tokens,omitempty"`
	StatusCodes   []int  `json:"status_codes,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
	Offense       int    `json:"offense,omitempty"`
}

const (
	RiskReviewPending       = "pending"
	RiskReviewConfirmed     = "confirmed"
	RiskReviewFalsePositive = "false_positive"
)

func IsValidRiskReviewStatus(status string) bool {
	return status == RiskReviewPending || status == RiskReviewConfirmed || status == RiskReviewFalsePositive
}

type RiskEventReview struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	EventId    int    `json:"event_id" gorm:"index:idx_risk_event_reviews_event_id"`
	Status     string `json:"status" gorm:"type:varchar(16)"`
	Note       string `json:"note" gorm:"type:text"`
	AssigneeId int    `json:"assignee_id"`
	OperatorId int    `json:"operator_id"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
}

var ErrRiskReviewChanged = errors.New("事件已被其他管理员更新，请刷新后重试")

// truncateRiskField 按列宽截断字符串,在 rune 边界截断并保证 UTF-8 合法,
// 与 logs 表 UA 入库的处理保持一致,避免超宽值导致整条事件写入失败。
func truncateRiskField(value string, maxLen int) string {
	end := 0
	for count := 0; count < maxLen && end < len(value); count++ {
		_, size := utf8.DecodeRuneInString(value[end:])
		end += size
	}
	value = value[:end]
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, string(utf8.RuneError))
	}
	return value
}

// InsertRiskEvent 写入一条风控事件。字段按列宽截断,CreatedAt/Count 未设置时补默认值。
func InsertRiskEvent(event *RiskEvent) error {
	if event == nil {
		return nil
	}
	if event.CreatedAt <= 0 {
		event.CreatedAt = common.GetTimestamp()
	}
	if event.Count < 1 {
		event.Count = 1
	}
	event.EventType = truncateRiskField(event.EventType, 32)
	event.Username = truncateRiskField(event.Username, 64)
	event.Ip = truncateRiskField(event.Ip, 64)
	event.Ua = truncateRiskField(event.Ua, 512)
	event.Rule = truncateRiskField(event.Rule, 255)
	event.Reason = truncateRiskField(event.Reason, 512)
	if event.ReviewStatus == "" {
		event.ReviewStatus = RiskReviewPending
	}
	if event.Evidence != nil {
		event.RuleId = event.Evidence.RuleId
		data, err := common.Marshal(event.Evidence)
		if err != nil {
			return err
		}
		event.RuleSnapshot = string(data)
	}
	return DB.Create(event).Error
}

// GetRiskEvents 分页查询风控事件,按时间倒序。eventType/userId/ip 为空(零值)时不参与过滤。
func GetRiskEvents(eventType string, userId int, ip string, startIdx int, num int) ([]*RiskEvent, int64, error) {
	return QueryRiskEvents(RiskEventQuery{EventType: eventType, UserId: userId, Ip: ip}, startIdx, num)
}

type RiskEventQuery struct {
	EventType    string
	EventTypes   []string // 事件大类展开后的类型,与 EventType 同时给出时取交集
	UserId       int
	Ip           string
	Ua           string
	StartAt      int64
	EndAt        int64
	RuleId       string
	ReviewStatus string
	AssigneeId   int
	Unassigned   bool   // 只看尚未分配负责人的事件
	Keyword      string // 按用户名、UA、规则与原因做包含匹配
}

func QueryRiskEvents(filter RiskEventQuery, startIdx int, num int) ([]*RiskEvent, int64, error) {
	query := DB.Model(&RiskEvent{})
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if len(filter.EventTypes) > 0 {
		query = query.Where("event_type IN ?", filter.EventTypes)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Ip != "" {
		// 地址查询同时匹配覆盖它的 CIDR 封禁事件，三库均使用普通 IN 查询。
		targets := []string{filter.Ip}
		if addr, err := netip.ParseAddr(filter.Ip); err == nil {
			addr = addr.Unmap()
			targets = append(targets, addr.String())
			for bits := 0; bits <= addr.BitLen(); bits++ {
				prefix, _ := addr.Prefix(bits)
				targets = append(targets, prefix.Masked().String())
			}
		} else if prefix, err := netip.ParsePrefix(filter.Ip); err == nil {
			targets = append(targets, prefix.Masked().String())
		}
		query = query.Where("ip IN ?", targets)
	}
	if filter.Ua != "" {
		query = query.Where("ua = ?", filter.Ua)
	}
	if filter.StartAt > 0 {
		query = query.Where("created_at >= ?", filter.StartAt)
	}
	if filter.EndAt > 0 {
		query = query.Where("created_at <= ?", filter.EndAt)
	}
	if filter.RuleId != "" {
		query = query.Where("rule_id = ? OR (COALESCE(rule_id, '') = '' AND rule = ?)", filter.RuleId, filter.RuleId)
	}
	if filter.ReviewStatus == RiskReviewPending {
		query = query.Where("COALESCE(review_status, '') IN ?", []string{"", RiskReviewPending})
	} else if filter.ReviewStatus != "" {
		query = query.Where("review_status = ?", filter.ReviewStatus)
	}
	if filter.AssigneeId > 0 {
		query = query.Where("assignee_id = ?", filter.AssigneeId)
	} else if filter.Unassigned {
		query = query.Where("COALESCE(assignee_id, 0) = 0")
	}
	if filter.Keyword != "" {
		// LOWER 让三库都不区分大小写(PostgreSQL 的 LIKE 区分大小写)
		pattern := riskKeywordPattern(strings.ToLower(filter.Keyword))
		query = query.Where("LOWER(username) LIKE ? ESCAPE '!' OR LOWER(ua) LIKE ? ESCAPE '!' OR LOWER(rule) LIKE ? ESCAPE '!' OR LOWER(reason) LIKE ? ESCAPE '!'",
			pattern, pattern, pattern, pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var events []*RiskEvent
	err := query.Order("created_at desc, id desc").Offset(startIdx).Limit(num).Find(&events).Error
	for _, event := range events {
		if event.ReviewStatus == "" {
			event.ReviewStatus = RiskReviewPending
		}
		if event.RuleSnapshot != "" {
			if decodeErr := common.UnmarshalJsonStr(event.RuleSnapshot, &event.Evidence); decodeErr != nil {
				common.SysError("risk control: invalid rule snapshot: " + decodeErr.Error())
			}
		}
	}
	return events, total, err
}

// ReviewRiskEvent 以版本检查防止管理员覆盖彼此的备注，更新与历史记录同一事务提交。
func ReviewRiskEvent(id, version int, status, note string, assigneeId, operatorId int) (*RiskEvent, error) {
	if utf8.RuneCountInString(note) > 2000 || assigneeId < 0 {
		return nil, errors.New("事件处理参数无效")
	}
	return reviewRiskEvent(id, version, status, &note, &assigneeId, operatorId)
}

// reviewRiskEvent 是单条与批量处理共用的实现。note、assigneeId 为 nil 时保留事件原有的值，
// 历史记录写入处理后的实际值。
func reviewRiskEvent(id, version int, status string, note *string, assigneeId *int, operatorId int) (*RiskEvent, error) {
	if id <= 0 || version < 0 || !IsValidRiskReviewStatus(status) {
		return nil, errors.New("事件处理参数无效")
	}
	var event RiskEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&event, id).Error; err != nil {
			return err
		}
		if event.ReviewVersion != version {
			return ErrRiskReviewChanged
		}
		if assigneeId != nil && *assigneeId > 0 && *assigneeId != event.AssigneeId {
			var assignee User
			if err := tx.Select("id", "role", "status").First(&assignee, *assigneeId).Error; err != nil {
				return errors.New("负责人不存在")
			}
			if assignee.Role < common.RoleAdminUser || assignee.Status != common.UserStatusEnabled {
				return errors.New("负责人必须是已启用的管理员")
			}
		}
		now := common.GetTimestamp()
		updates := map[string]interface{}{"review_status": status, "reviewed_by": operatorId,
			"reviewed_at": now, "review_version": version + 1}
		effectiveNote, effectiveAssignee := event.ReviewNote, event.AssigneeId
		if note != nil {
			effectiveNote = *note
			updates["review_note"] = effectiveNote
		}
		if assigneeId != nil {
			effectiveAssignee = *assigneeId
			updates["assignee_id"] = effectiveAssignee
		}
		result := tx.Model(&RiskEvent{}).Where("id = ? AND COALESCE(review_version, 0) = ?", id, version).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRiskReviewChanged
		}
		if err := tx.Create(&RiskEventReview{EventId: id, Status: status, Note: effectiveNote,
			AssigneeId: effectiveAssignee, OperatorId: operatorId, CreatedAt: now}).Error; err != nil {
			return err
		}
		return tx.First(&event, id).Error
	})
	if err != nil {
		return nil, err
	}
	if event.RuleSnapshot != "" {
		if decodeErr := common.UnmarshalJsonStr(event.RuleSnapshot, &event.Evidence); decodeErr != nil {
			common.SysError("risk control: invalid rule snapshot: " + decodeErr.Error())
		}
	}
	return &event, nil
}

func GetRiskEventReviews(eventId int) ([]RiskEventReview, error) {
	var reviews []RiskEventReview
	err := DB.Where("event_id = ?", eventId).Order("id desc").Limit(100).Find(&reviews).Error
	return reviews, err
}

func GetRiskEventById(id int) (*RiskEvent, error) {
	var event RiskEvent
	if err := DB.First(&event, id).Error; err != nil {
		return nil, err
	}
	if event.ReviewStatus == "" {
		event.ReviewStatus = RiskReviewPending
	}
	if event.RuleSnapshot != "" {
		if err := common.UnmarshalJsonStr(event.RuleSnapshot, &event.Evidence); err != nil {
			return nil, err
		}
	}
	return &event, nil
}

// CleanupRiskEvents 清理 before 之前的拦截与告警事件。
// 封禁(ban_auto/ban_manual)与解禁(unban/unban_auto)记录是处置审计,不参与清理;
// ban_auto 还是账号累犯升级的计数依据,清掉会让累犯次数凭空归零。
func CleanupRiskEvents(before int64) (int64, error) {
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&RiskEvent{}).Where("created_at < ?", before).
			Where("event_type IN ?", []string{RiskEventBlockUa, RiskEventBlockIp, RiskEventBlockClientUa, RiskEventAlert})
		if err := tx.Where("event_id IN (?)", query.Select("id")).Delete(&RiskEventReview{}).Error; err != nil {
			return err
		}
		result := tx.Where("created_at < ?", before).
			Where("event_type IN ?", []string{RiskEventBlockUa, RiskEventBlockIp, RiskEventBlockClientUa, RiskEventAlert}).Delete(&RiskEvent{})
		affected = result.RowsAffected
		return result.Error
	})
	return affected, err
}
