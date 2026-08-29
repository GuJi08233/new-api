package model

import (
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// 风控事件表:持久化黑名单拦截、自动/手动封禁、解禁与告警记录,存主库。
// 拦截类事件由 service 层按 (类型+IP+UA+规则) 聚合限流后写入,Count 为窗口内累计次数;
// 封禁/解禁事件逐条直写。拦截与告警事件按保留天数周期清理,封禁与解禁记录永久保留。

const (
	RiskEventBlockUa   = "block_ua"   // UA 黑名单拦截
	RiskEventBlockIp   = "block_ip"   // IP 黑名单/动态封禁拦截
	RiskEventBanAuto   = "ban_auto"   // 风控自动封禁用户
	RiskEventBanManual = "ban_manual" // 管理员手动封禁用户
	RiskEventUnban     = "unban"      // 管理员解除用户封禁
	RiskEventBanIp     = "ban_ip"     // IP 被封禁(自动升级或手动添加)
	RiskEventUnbanIp   = "unban_ip"   // IP 封禁被解除
	RiskEventAlert     = "alert"      // 自动规则命中告警(动作为仅告警)
)

// IsValidRiskEventType 判断字符串是否为已支持的风控事件类型。
func IsValidRiskEventType(eventType string) bool {
	switch eventType {
	case RiskEventBlockUa, RiskEventBlockIp, RiskEventBanAuto,
		RiskEventBanManual, RiskEventUnban, RiskEventBanIp,
		RiskEventUnbanIp, RiskEventAlert:
		return true
	}
	return false
}

type RiskEvent struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index:idx_risk_events_created_at"`
	EventType  string `json:"event_type" gorm:"type:varchar(32);index:idx_risk_events_type"`
	UserId     int    `json:"user_id" gorm:"index:idx_risk_events_user_id"`
	Username   string `json:"username" gorm:"type:varchar(64)"`
	Ip         string `json:"ip" gorm:"type:varchar(64);index:idx_risk_events_ip"`
	Ua         string `json:"ua" gorm:"type:varchar(512)"`
	Rule       string `json:"rule" gorm:"type:varchar(255)"`
	Reason     string `json:"reason" gorm:"type:varchar(512)"`
	Count      int    `json:"count"`
	OperatorId int    `json:"operator_id"`
}

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
	return DB.Create(event).Error
}

// GetRiskEvents 分页查询风控事件,按时间倒序。eventType/userId/ip 为空(零值)时不参与过滤。
func GetRiskEvents(eventType string, userId int, ip string, startIdx int, num int) ([]*RiskEvent, int64, error) {
	query := DB.Model(&RiskEvent{})
	if eventType != "" {
		query = query.Where("event_type = ?", eventType)
	}
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if ip != "" {
		query = query.Where("ip = ?", ip)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var events []*RiskEvent
	err := query.Order("created_at desc, id desc").Offset(startIdx).Limit(num).Find(&events).Error
	return events, total, err
}

// CleanupRiskEvents 清理 before 之前的拦截与告警事件。
// 封禁(ban_auto/ban_manual)与解禁(unban)记录是处置审计,不参与清理。
func CleanupRiskEvents(before int64) (int64, error) {
	result := DB.Where("created_at < ?", before).
		Where("event_type IN ?", []string{RiskEventBlockUa, RiskEventBlockIp, RiskEventAlert}).
		Delete(&RiskEvent{})
	return result.RowsAffected, result.Error
}
