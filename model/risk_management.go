package model

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetManualUserRiskState 将状态、原因与临时到期时间原子更新，防止手动永久禁用
// 被后台的到期恢复误解除。权限判断所用的角色必须在事务内再次确认。
func SetManualUserRiskState(tx *gorm.DB, userId, expectedRole, status int, reason string) error {
	if status != common.UserStatusEnabled && status != common.UserStatusDisabled {
		return errors.New("账号状态无效")
	}
	if status == common.UserStatusDisabled && expectedRole == common.RoleRootUser {
		return errors.New("不能禁用 root 账号")
	}
	if err := LockUserRole(tx, userId, expectedRole); err != nil {
		return err
	}
	if status == common.UserStatusEnabled {
		reason = ""
	}
	return tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"status": status, "disable_reason": reason, "disable_expires_at": 0,
	}).Error
}

// AddRiskExemptUser 在主库当前名单上追加，锁定名单行避免不同节点批量追加时丢项。
func AddRiskExemptUser(userId, expectedRole int, accountOnly bool) (bool, error) {
	optionWriteMu.Lock()
	defer optionWriteMu.Unlock()
	key := operation_setting.RiskControlSettingPrefix + "whitelist_user_ids"
	if accountOnly {
		key = operation_setting.RiskControlSettingPrefix + "public_key_user_ids"
	}
	value := ""
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := LockUserRole(tx, userId, expectedRole); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: key, Value: "[]"}).Error; err != nil {
			return err
		}
		var option Option
		if err := lockForUpdate(tx).Where(&Option{Key: key}).First(&option).Error; err != nil {
			return err
		}
		if err := operation_setting.ValidateRiskControlOption(key, option.Value); err != nil {
			return err
		}
		ids := []int{}
		if err := common.UnmarshalJsonStr(option.Value, &ids); err != nil {
			return err
		}
		for _, id := range ids {
			if id == userId {
				value = option.Value
				return nil
			}
		}
		ids = append(ids, userId)
		sort.Ints(ids)
		data, err := common.Marshal(ids)
		if err != nil {
			return err
		}
		value = string(data)
		if err := operation_setting.ValidateRiskControlOption(key, value); err != nil {
			return err
		}
		changed = true
		return tx.Model(&Option{}).Where(&Option{Key: key}).Update("value", value).Error
	})
	if err != nil {
		return false, err
	}
	return changed, publishOptions(map[string]string{key: value})
}

// ReleaseRiskIpBan 删除指定封禁并与解封事件同一事务提交，不删除覆盖该地址的其他记录。
func ReleaseRiskIpBan(id, operatorId int, reason string) (*IpBan, error) {
	var ban IpBan
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&ban, id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&IpBan{}, id).Error; err != nil {
			return err
		}
		if reason == "" {
			reason = ban.Reason
		}
		return tx.Create(&RiskEvent{EventType: RiskEventUnbanIp, Ip: ban.Target, Rule: ban.Source,
			Reason: truncateRiskField(reason, 512), OperatorId: operatorId, Count: 1,
			CreatedAt: common.GetTimestamp(), ReviewStatus: RiskReviewPending}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := ReloadIpBanCache(); err != nil {
		common.SysError("failed to reload ip ban cache: " + err.Error())
	}
	return &ban, nil
}
