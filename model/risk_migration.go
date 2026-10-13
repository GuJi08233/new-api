package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ipBanTargetUniqueIndex 保证每个封禁目标只有一条记录。原来的 idx_ip_bans_target 是普通索引,
// 并发封禁同一目标(多节点,或扫描与实时防护同时命中)会写出多行,解除其中一条后另一条仍然生效。
const ipBanTargetUniqueIndex = "uidx_ip_bans_target"

// migrateIpBanTargetUniqueness 在 AutoMigrate 之前把旧库的 ip_bans.target 改成唯一索引:
// 先合并重复行,每个目标只留效力最强的一条(永久优先,其次到期最晚,再其次最新),
// 再建唯一索引、删掉旧的普通索引。封禁历史记在 risk_events 里,删掉的重复行不影响审计。
// 新库没有这张表,由 AutoMigrate 直接建出唯一索引。
// MySQL 的 DDL 会隐式提交,合并之后、建索引之前若有旧版节点写进重复行,建索引失败,
// 下次启动重新合并即可。
func migrateIpBanTargetUniqueness(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&IpBan{}) || migrator.HasIndex(&IpBan{}, ipBanTargetUniqueIndex) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var targets []string
		if err := tx.Model(&IpBan{}).Group("target").Having("count(*) > ?", 1).Pluck("target", &targets).Error; err != nil {
			return err
		}
		merged := 0
		for _, target := range targets {
			var bans []IpBan
			if err := tx.Where("target = ?", target).Find(&bans).Error; err != nil {
				return err
			}
			sort.Slice(bans, func(i, j int) bool {
				if (bans[i].ExpiresAt == 0) != (bans[j].ExpiresAt == 0) {
					return bans[i].ExpiresAt == 0
				}
				if bans[i].ExpiresAt != bans[j].ExpiresAt {
					return bans[i].ExpiresAt > bans[j].ExpiresAt
				}
				return bans[i].Id > bans[j].Id
			})
			duplicateIds := make([]int, 0, len(bans)-1)
			for _, ban := range bans[1:] {
				duplicateIds = append(duplicateIds, ban.Id)
			}
			if err := tx.Where("id IN ?", duplicateIds).Delete(&IpBan{}).Error; err != nil {
				return err
			}
			merged += len(duplicateIds)
		}
		if err := tx.Migrator().CreateIndex(&IpBan{}, ipBanTargetUniqueIndex); err != nil {
			return fmt.Errorf("create ip ban target unique index: %w", err)
		}
		if tx.Migrator().HasIndex(&IpBan{}, "idx_ip_bans_target") {
			if err := tx.Migrator().DropIndex(&IpBan{}, "idx_ip_bans_target"); err != nil {
				return fmt.Errorf("drop legacy ip ban target index: %w", err)
			}
		}
		if merged > 0 {
			common.SysLog(fmt.Sprintf("risk control: merged %d duplicate ip ban rows before adding the unique target index", merged))
		}
		return nil
	})
}

// 风控 UA 黑名单原来按"含正则元字符就当正则(区分大小写),否则子串"判断写法,
// 含 . 的版本号也会被当成正则。现在与客户端 UA 准入规则统一为 UaPattern 的写法:
// 子串,或 re: 开头的正则,都不区分大小写。迁移只做一次,标记与改写在同一事务里提交
// (做法同 migrateGameCoinUnits):旧版按正则生效的条目加上 re: 前缀;按子串生效、
// 但恰好以 re: 开头的条目转义成等价正则;其余保持原样。新旧程序不能混跑。
const riskUaPatternSyntaxOption = "RiskUaPatternSyntax"

func migrateRiskUaPatternSyntax() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: riskUaPatternSyntaxOption, Value: "1"})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}
		key := operation_setting.RiskControlSettingPrefix + "ua_blacklist"
		var values []string
		if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Pluck("value", &values).Error; err != nil {
			return err
		}
		if len(values) == 0 {
			return nil
		}
		var entries []string
		if err := common.UnmarshalJsonStr(values[0], &entries); err != nil {
			// 解析不了的旧值原样保留,回放时由配置校验拒绝并记日志
			common.SysError("risk control: ua blacklist is not a string array, syntax migration skipped: " + err.Error())
			return nil
		}
		converted := make([]string, len(entries))
		for index, entry := range entries {
			entry = strings.TrimSpace(entry)
			legacyRegex := strings.ContainsAny(entry, `\^$.|?*+()[]{}`)
			if legacyRegex {
				// 旧版遇到编译不了的正则退化为子串匹配
				_, err := regexp.Compile(entry)
				legacyRegex = err == nil
			}
			switch {
			case legacyRegex:
				converted[index] = operation_setting.UaPatternRegexPrefix + entry
			case strings.HasPrefix(entry, operation_setting.UaPatternRegexPrefix):
				converted[index] = operation_setting.UaPatternRegexPrefix + regexp.QuoteMeta(entry)
			default:
				converted[index] = entry
			}
		}
		data, err := common.Marshal(converted)
		if err != nil {
			return err
		}
		if string(data) == values[0] {
			return nil
		}
		if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", string(data)).Error; err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf("risk control: ua blacklist migrated to the re: prefix syntax: %s", string(data)))
		return nil
	})
}
