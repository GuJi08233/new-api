package model

import (
	"crypto/sha256"
	"encoding/hex"

	"gorm.io/gorm/clause"
)

// ChannelRecoveryState 持久化恢复目标的探测时间，供不同主节点和重启后的任务共享。
type ChannelRecoveryState struct {
	ChannelID    int    `gorm:"primaryKey;autoIncrement:false"`
	TargetKey    string `gorm:"type:varchar(64);primaryKey"`
	LastTestedAt int64  `gorm:"type:bigint;not null;index"`
}

// ChannelRecoveryTargetID 将目标范围与实际密钥绑定，密钥重排不改变身份，且不保存明文。
func ChannelRecoveryTargetID(key string, channelTarget bool) string {
	scope := "key:"
	if channelTarget {
		scope = "channel:"
	}
	digest := sha256.Sum256([]byte(scope + key))
	return hex.EncodeToString(digest[:])
}

func GetChannelRecoveryLastTests() (map[int]map[string]int64, error) {
	var states []ChannelRecoveryState
	// 调度必须读取主库，避免只读副本延迟导致另一节点提前重复探测。
	if err := DB.Find(&states).Error; err != nil {
		return nil, err
	}
	lastTests := make(map[int]map[string]int64)
	for _, state := range states {
		if lastTests[state.ChannelID] == nil {
			lastTests[state.ChannelID] = make(map[string]int64)
		}
		lastTests[state.ChannelID][state.TargetKey] = state.LastTestedAt
	}
	return lastTests, nil
}

// RecordChannelRecoveryTest 记录一次探测，不区分成功或失败；targetKey 由 ChannelRecoveryTargetID 生成。
func RecordChannelRecoveryTest(channelID int, targetKey string, testedAt int64) error {
	state := ChannelRecoveryState{ChannelID: channelID, TargetKey: targetKey, LastTestedAt: testedAt}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "target_key"}},
		DoNothing: true,
	}).Create(&state).Error; err != nil {
		return err
	}
	// 冲突时只推进时间；重复写入和迟到的旧探测都不能覆盖较新的记录。
	return DB.Model(&ChannelRecoveryState{}).
		Where("channel_id = ? AND target_key = ? AND last_tested_at < ?", channelID, targetKey, testedAt).
		Update("last_tested_at", testedAt).Error
}

// DeleteExpiredChannelRecoveryStates 清理已超出冷却保留期的目标，避免删除渠道或更换密钥后积累记录。
func DeleteExpiredChannelRecoveryStates(before int64) error {
	return DB.Where("last_tested_at < ?", before).Delete(&ChannelRecoveryState{}).Error
}
