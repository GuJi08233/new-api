package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
)

const ChannelRecoveryChannelTarget = -1

// ChannelRecoveryTarget 固定探测前的密钥身份和禁用记录。KeyList、TestedKey 仅供内部校验，
// 不得写入日志或作为接口响应返回。
type ChannelRecoveryTarget struct {
	ChannelID int
	KeyIndex  int
	// TestedKeyIndex 是 TestedKey 在渠道密钥列表中的编号，供探测日志标明用了哪把密钥；
	// 单密钥渠道没有编号，取 ChannelRecoveryChannelTarget。
	TestedKeyIndex  int
	KeyList         string
	TestedKey       string
	IsMultiKey      bool
	DisabledAt      int64
	DisabledReason  string
	ChannelStatusAt int64
}

// NewChannelRecoveryTarget 从渠道快照选定实际要测试的密钥，不推进轮询索引。
func NewChannelRecoveryTarget(channel *Channel, keyIndex int) (ChannelRecoveryTarget, bool) {
	if channel == nil || channel.Id <= 0 ||
		(channel.Status != common.ChannelStatusEnabled && channel.Status != common.ChannelStatusAutoDisabled) {
		return ChannelRecoveryTarget{}, false
	}
	target := ChannelRecoveryTarget{
		ChannelID:       channel.Id,
		KeyIndex:        keyIndex,
		TestedKeyIndex:  ChannelRecoveryChannelTarget,
		KeyList:         channel.Key,
		IsMultiKey:      channel.ChannelInfo.IsMultiKey,
		ChannelStatusAt: channel.GetStatusTime(),
	}
	if keyIndex == ChannelRecoveryChannelTarget {
		if channel.Status != common.ChannelStatusAutoDisabled {
			return ChannelRecoveryTarget{}, false
		}
		target.DisabledAt = target.ChannelStatusAt
		target.DisabledReason, _ = channel.GetOtherInfo()["status_reason"].(string)
		if !channel.ChannelInfo.IsMultiKey {
			target.TestedKey = channel.Key
			return target, true
		}
		for index, key := range channel.GetKeys() {
			status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
			if !exists || status == common.ChannelStatusEnabled {
				target.TestedKey = key
				target.TestedKeyIndex = index
				return target, true
			}
		}
		return ChannelRecoveryTarget{}, false
	}
	if keyIndex < 0 || !channel.ChannelInfo.IsMultiKey {
		return ChannelRecoveryTarget{}, false
	}
	keys := channel.GetKeys()
	if keyIndex >= len(keys) || channel.ChannelInfo.MultiKeyStatusList[keyIndex] != common.ChannelStatusAutoDisabled {
		return ChannelRecoveryTarget{}, false
	}
	target.TestedKey = keys[keyIndex]
	target.TestedKeyIndex = keyIndex
	target.DisabledAt = channel.ChannelInfo.MultiKeyDisabledTime[keyIndex]
	target.DisabledReason = channel.ChannelInfo.MultiKeyDisabledReason[keyIndex]
	return target, true
}

// RecoverChannel 仅应用仍与探测前身份、禁用记录一致的成功结果。
// 进程锁协调本节点缓存更新；主库事务与行锁负责跨实例并发，SQLite 冲突时安全报错。
func RecoverChannel(target ChannelRecoveryTarget) (changed bool, channelRecovered bool, err error) {
	if target.ChannelID <= 0 || target.KeyIndex < ChannelRecoveryChannelTarget {
		return false, false, nil
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	err = DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", target.ChannelID).First(&channel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if channel.Status != common.ChannelStatusEnabled && channel.Status != common.ChannelStatusAutoDisabled {
			return nil
		}
		if channel.Key != target.KeyList || channel.ChannelInfo.IsMultiKey != target.IsMultiKey {
			return nil
		}
		setting := dto.ChannelSettings{}
		if channel.Setting == nil || common.UnmarshalJsonStr(*channel.Setting, &setting) != nil || !setting.AutoRecoveryEnabled {
			return nil
		}

		updates := make(map[string]interface{})
		if target.KeyIndex == ChannelRecoveryChannelTarget {
			reason, _ := channel.GetOtherInfo()["status_reason"].(string)
			if channel.Status != common.ChannelStatusAutoDisabled || channel.GetStatusTime() != target.ChannelStatusAt || reason != target.DisabledReason {
				return nil
			}
			if channel.ChannelInfo.IsMultiKey {
				testedKeyEnabled := false
				for index, key := range channel.GetKeys() {
					status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
					if key == target.TestedKey && (!exists || status == common.ChannelStatusEnabled) {
						testedKeyEnabled = true
						break
					}
				}
				if !testedKeyEnabled {
					return nil
				}
			} else if channel.Key != target.TestedKey {
				return nil
			}
		} else {
			// 整渠道在探测期间再次被禁用时，旧密钥结果不能解除这次新的禁用。
			// 首把密钥恢复后渠道已启用，不限制状态时间，允许同轮其他密钥继续恢复。
			if channel.Status == common.ChannelStatusAutoDisabled && channel.GetStatusTime() != target.ChannelStatusAt {
				return nil
			}
			keys := channel.GetKeys()
			if !channel.ChannelInfo.IsMultiKey || target.KeyIndex >= len(keys) || keys[target.KeyIndex] != target.TestedKey {
				return nil
			}
			if channel.ChannelInfo.MultiKeyStatusList[target.KeyIndex] != common.ChannelStatusAutoDisabled ||
				channel.ChannelInfo.MultiKeyDisabledTime[target.KeyIndex] != target.DisabledAt ||
				channel.ChannelInfo.MultiKeyDisabledReason[target.KeyIndex] != target.DisabledReason {
				return nil
			}
			delete(channel.ChannelInfo.MultiKeyStatusList, target.KeyIndex)
			delete(channel.ChannelInfo.MultiKeyDisabledReason, target.KeyIndex)
			delete(channel.ChannelInfo.MultiKeyDisabledTime, target.KeyIndex)
			updates["channel_info"] = channel.ChannelInfo
		}

		recovered := channel.Status == common.ChannelStatusAutoDisabled
		if recovered {
			info := channel.GetOtherInfo()
			info["status_reason"] = ""
			info["status_time"] = common.GetTimestamp()
			otherInfo, err := common.Marshal(info)
			if err != nil {
				return err
			}
			updates["status"] = common.ChannelStatusEnabled
			updates["other_info"] = string(otherInfo)
		}
		result := tx.Model(&Channel{}).Where("id = ? AND status = ?", channel.Id, channel.Status).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if recovered {
			if err := tx.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", true).Error; err != nil {
				return err
			}
		}
		changed = true
		channelRecovered = recovered
		return nil
	})
	if err != nil {
		return false, false, err
	}
	if changed {
		InitChannelCache()
	}
	return changed, channelRecovered, nil
}
