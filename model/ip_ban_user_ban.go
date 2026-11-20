package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IpBanUserBan 记录因命中启用「连带封号」的 IP 封禁规则而被自动禁用的账号，
// 用结构化外键精确追溯 ban_id -> user_id，不依赖字符串匹配。
type IpBanUserBan struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	BanId     int    `json:"ban_id" gorm:"uniqueIndex:uidx_ip_ban_user_bans_ban_user;index:idx_ip_ban_user_bans_ban_id;not null"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex:uidx_ip_ban_user_bans_ban_user;index:idx_ip_ban_user_bans_user_id;not null"`
	BannedIp  string `json:"banned_ip" gorm:"type:varchar(64);index"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func (IpBanUserBan) TableName() string {
	return "ip_ban_user_bans"
}

// IpBanRelatedUser 是 IP 封禁关联禁用账号的展示模型。
type IpBanRelatedUser struct {
	UserId           int    `json:"user_id"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Status           int    `json:"status"`
	DisableReason    string `json:"disable_reason"`
	DisableExpiresAt int64  `json:"disable_expires_at"`
	BannedIp         string `json:"banned_ip"`
	BannedAt         int64  `json:"banned_at"`
}

// RecordIpBanUserBan 写入或刷新一条 (ban_id, user_id) 关联记录。
func RecordIpBanUserBan(banId, userId int, bannedIp, reason string) error {
	if banId <= 0 || userId <= 0 {
		return nil
	}
	record := IpBanUserBan{
		BanId:     banId,
		UserId:    userId,
		BannedIp:  truncateRiskField(strings.TrimSpace(bannedIp), 64),
		Reason:    truncateRiskField(strings.TrimSpace(reason), 255),
		CreatedAt: common.GetTimestamp(),
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ban_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"banned_ip", "reason", "created_at"}),
	}).Create(&record).Error
}

// GetIpBanRelatedUsers 按 ban_id 列出该 IP 封禁规则连带禁用的账号及当前主库状态。
func GetIpBanRelatedUsers(banId int, limit int) ([]IpBanRelatedUser, error) {
	if banId <= 0 {
		return nil, errors.New("无效的封禁 ID")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []IpBanRelatedUser
	err := DB.Table("ip_ban_user_bans AS b").
		Select("b.user_id AS user_id, u.username AS username, u.display_name AS display_name, u.status AS status, u.disable_reason AS disable_reason, u.disable_expires_at AS disable_expires_at, b.banned_ip AS banned_ip, b.created_at AS banned_at").
		Joins("LEFT JOIN users u ON u.id = b.user_id").
		Where("b.ban_id = ?", banId).
		Order("b.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// GetIpBanRelatedUserCounts 批量统计每条 IP 封禁规则连带禁用的账号数。
func GetIpBanRelatedUserCounts(banIds []int) (map[int]int64, error) {
	counts := make(map[int]int64, len(banIds))
	if len(banIds) == 0 {
		return counts, nil
	}
	var rows []struct {
		BanId int   `gorm:"column:ban_id"`
		Count int64 `gorm:"column:count"`
	}
	err := DB.Model(&IpBanUserBan{}).
		Select("ban_id, COUNT(*) AS count").
		Where("ban_id IN ?", banIds).
		Group("ban_id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.BanId] = row.Count
	}
	return counts, nil
}

// deleteIpBanUserBansTx 在同一事务内级联清理 IP 封禁对应的连带封号记录。
func deleteIpBanUserBansTx(tx *gorm.DB, banIds []int) error {
	if len(banIds) == 0 {
		return nil
	}
	return tx.Where("ban_id IN ?", banIds).Delete(&IpBanUserBan{}).Error
}
