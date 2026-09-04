package model

import (
	"github.com/QuantumNous/new-api/common"
)

// 账号封禁的累犯计数与到期恢复。与 ip_ban.go 对称:两者共用风控设置里的同一套
// 升级阶梯,区别只在处置对象——IP 封禁落在 ip_bans 表,账号封禁落在 users 行上的
// status 与 disable_expires_at。账号没有独立的封禁表,因为 status 本身就是权威状态,
// 再存一份会与登录、relay 的既有拒绝路径产生两个事实来源。

// RestoredUserBan 描述一个临时禁用到期后被恢复的账号。
type RestoredUserBan struct {
	UserId   int
	Username string
}

// userBanRestoreBatch 单轮自动恢复处理的账号数上限,超出的留给下一轮。
const userBanRestoreBatch = 500

// RestoreExpiredUserBans 把到期的临时禁用账号恢复为启用,并清空封禁原因与到期时间。
// 更新语句重复了筛选条件,保证与管理员的并发操作不冲突:管理员在这中间手动禁用账号
// 会把 disable_expires_at 清零,该行随即不再满足条件,不会被误放出来。
func RestoreExpiredUserBans(now int64) ([]RestoredUserBan, error) {
	var candidates []User
	if err := DB.Model(&User{}).
		Select("id", "username").
		Where("status = ?", common.UserStatusDisabled).
		Where("disable_expires_at > 0 AND disable_expires_at <= ?", now).
		Limit(userBanRestoreBatch).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	restored := make([]RestoredUserBan, 0, len(candidates))
	for _, candidate := range candidates {
		result := DB.Model(&User{}).
			Where("id = ? AND status = ?", candidate.Id, common.UserStatusDisabled).
			Where("disable_expires_at > 0 AND disable_expires_at <= ?", now).
			Updates(map[string]interface{}{
				"status":             common.UserStatusEnabled,
				"disable_reason":     "",
				"disable_expires_at": 0,
			})
		if result.Error != nil {
			return restored, result.Error
		}
		if result.RowsAffected > 0 {
			restored = append(restored, RestoredUserBan{UserId: candidate.Id, Username: candidate.Username})
		}
	}
	return restored, nil
}

// CountRecentUserBanEvents 统计某账号在 since 之后被风控自动禁用的次数(用于累犯升级)。
// 与 IP 侧一样以风控事件表为准:临时禁用到期恢复后 users 行上不留痕迹,历史只在事件里。
func CountRecentUserBanEvents(userId int, since int64) (int64, error) {
	var count int64
	err := DB.Model(&RiskEvent{}).
		Where("event_type = ?", RiskEventBanAuto).
		Where("user_id = ?", userId).
		Where("created_at >= ?", since).
		Count(&count).Error
	return count, err
}

// LastUserBanEventAt 返回某账号最近一次被自动禁用的时间,从未被自动禁用过时为 0。
// 扫描规则用它判断窗口内的证据是否早于上次处置。
func LastUserBanEventAt(userId int) (int64, error) {
	var latest []int64
	err := DB.Model(&RiskEvent{}).
		Where("event_type = ?", RiskEventBanAuto).
		Where("user_id = ?", userId).
		Order("created_at desc").
		Limit(1).
		Pluck("created_at", &latest).Error
	if err != nil || len(latest) == 0 {
		return 0, err
	}
	return latest[0], nil
}
