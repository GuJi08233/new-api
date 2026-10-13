package model

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 动态 IP 封禁表:与风控设置里的静态 IP 黑名单(配置项)互补,
// 支持临时封禁(到期自动失效)与永久封禁,由自动规则、Probe Guard 或管理员写入。
// 匹配走全量内存缓存:请求路径零查库,缓存在本节点变更后立即重载,
// 其他节点由后台周期同步兜底。

// 封禁来源。IP 封禁与账号禁用共用同一组标识:两者由同一批检测链路触发,
// 事件表里也用同一个字段记录,分开定义只会让「同一条规则封了 IP 又禁了账号」
// 在审计时对不上号。
const (
	RiskBanSourceManual      = "manual"       // 管理员手动添加
	RiskBanSourceAutoRule    = "auto_rule"    // 自动封禁规则(周期扫描)
	RiskBanSourceProbeGuard  = "probe_guard"  // Probe Guard 实时检测
	RiskBanSourceErrorGuard  = "error_guard"  // Error Guard 实时错误率检测
	RiskBanSourceUaBlacklist = "ua_blacklist" // UA 黑名单实时拦截(仅账号处置)
)

type IpBan struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	Target    string `json:"target" gorm:"type:varchar(128);uniqueIndex:uidx_ip_bans_target"` // 归一化的 IP 或 CIDR,每个目标只有一条
	Reason    string `json:"reason" gorm:"type:varchar(512)"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;index:idx_ip_bans_expires_at"` // 0 表示永久
	Source    string `json:"source" gorm:"type:varchar(32)"`
	CreatedBy int    `json:"created_by"` // 手动添加时的操作人,自动来源为 0
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_ip_bans_created_at"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
	// RecentHits 是列表页附带的近期拦截次数,不落库。
	RecentHits int64 `json:"recent_hits" gorm:"-"`
}

// NormalizeIpBanTarget 将封禁目标归一化:CIDR 归一化为网段基址,单 IP 统一展开格式。
// 归一化保证同一目标只有一种存储形态,upsert 与匹配都以它为键。
func NormalizeIpBanTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("IP 或 CIDR 不能为空")
	}
	if strings.Contains(target, "/") {
		prefix, err := netip.ParsePrefix(target)
		if err != nil {
			return "", fmt.Errorf("无效的 CIDR: %s", target)
		}
		return prefix.Masked().String(), nil
	}
	addr, err := netip.ParseAddr(target)
	if err != nil {
		return "", fmt.Errorf("无效的 IP: %s", target)
	}
	return addr.Unmap().String(), nil
}

// NormalizeAutoBanTarget 归一化自动封禁目标。
// IPv6 客户端通常能在运营商分配的整段前缀内自由更换地址(隐私扩展地址甚至每小时一换),
// 只封 /128 等于没封,因此按 ipv6PrefixLength 归并到前缀。
// IPv4 与本身已是 CIDR 的目标原样处理;ipv6PrefixLength 为 128 时不归并。
// 管理员手动封禁不走本函数,填什么封什么。
func NormalizeAutoBanTarget(target string, ipv6PrefixLength int) (string, error) {
	normalized, err := NormalizeIpBanTarget(target)
	if err != nil {
		return "", err
	}
	if strings.Contains(normalized, "/") {
		return normalized, nil
	}
	addr, err := netip.ParseAddr(normalized)
	if err != nil {
		return "", err
	}
	if addr.Is4() || ipv6PrefixLength <= 0 || ipv6PrefixLength >= addr.BitLen() {
		return normalized, nil
	}
	prefix, err := addr.Prefix(ipv6PrefixLength)
	if err != nil {
		return "", err
	}
	return prefix.Masked().String(), nil
}

// IpBanTargetCovers 判断封禁目标(已归一化的 IP 或 CIDR)是否覆盖给定地址。
func IpBanTargetCovers(target string, ip string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if strings.Contains(target, "/") {
		prefix, err := netip.ParsePrefix(target)
		if err != nil {
			return false
		}
		return prefix.Contains(addr)
	}
	targetAddr, err := netip.ParseAddr(target)
	if err != nil {
		return false
	}
	return targetAddr.Unmap() == addr
}

type ipBanCacheEntry struct {
	id        int
	target    string
	reason    string
	expiresAt int64
	addr      netip.Addr
	prefix    netip.Prefix
	isPrefix  bool
}

var (
	ipBanCacheMu sync.RWMutex
	ipBanCache   []ipBanCacheEntry
)

// ReloadIpBanCache 从数据库全量加载未过期的封禁进内存缓存。
// 封禁增删后必须调用;后台周期同步兜底多节点一致性。
func ReloadIpBanCache() error {
	now := common.GetTimestamp()
	var bans []*IpBan
	if err := DB.Where("expires_at = 0 OR expires_at > ?", now).Find(&bans).Error; err != nil {
		return err
	}

	entries := make([]ipBanCacheEntry, 0, len(bans))
	for _, ban := range bans {
		entry := ipBanCacheEntry{id: ban.Id, target: ban.Target, reason: ban.Reason, expiresAt: ban.ExpiresAt}
		if strings.Contains(ban.Target, "/") {
			prefix, err := netip.ParsePrefix(ban.Target)
			if err != nil {
				common.SysLog(fmt.Sprintf("ip ban #%d has invalid target %q, skipped", ban.Id, ban.Target))
				continue
			}
			entry.prefix = prefix.Masked()
			entry.isPrefix = true
		} else {
			addr, err := netip.ParseAddr(ban.Target)
			if err != nil {
				common.SysLog(fmt.Sprintf("ip ban #%d has invalid target %q, skipped", ban.Id, ban.Target))
				continue
			}
			entry.addr = addr.Unmap()
		}
		entries = append(entries, entry)
	}

	ipBanCacheMu.Lock()
	ipBanCache = entries
	ipBanCacheMu.Unlock()
	return nil
}

// MatchActiveIpBan 判断客户端 IP 是否命中未过期的动态封禁,命中返回封禁信息。
// 纯内存匹配,可被任意请求 goroutine 并发调用。
func MatchActiveIpBan(clientIP string) (*IpBan, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return nil, false
	}
	addr = addr.Unmap()
	now := common.GetTimestamp()

	ipBanCacheMu.RLock()
	defer ipBanCacheMu.RUnlock()
	for _, entry := range ipBanCache {
		if entry.expiresAt != 0 && entry.expiresAt <= now {
			continue
		}
		if entry.isPrefix {
			if entry.prefix.Contains(addr) {
				return &IpBan{Id: entry.id, Target: entry.target, Reason: entry.reason, ExpiresAt: entry.expiresAt}, true
			}
			continue
		}
		if entry.addr == addr {
			return &IpBan{Id: entry.id, Target: entry.target, Reason: entry.reason, ExpiresAt: entry.expiresAt}, true
		}
	}
	return nil, false
}

// GetIpBanStates 为管理页面批量读取生效的封禁，包含覆盖地址的所有网段。
// 输入为 CIDR 时返回与其重叠的封禁，解除其中一条不会掩盖其他生效记录。
func GetIpBanStates(targets []string) (map[string][]*IpBan, error) {
	states := make(map[string][]*IpBan, len(targets))
	if len(targets) == 0 {
		return states, nil
	}
	var bans []*IpBan
	if err := DB.Where("expires_at = 0 OR expires_at > ?", common.GetTimestamp()).Find(&bans).Error; err != nil {
		return nil, err
	}
	for _, target := range targets {
		states[target] = []*IpBan{}
		prefix, err := netip.ParsePrefix(target)
		for _, ban := range bans {
			matched := IpBanTargetCovers(ban.Target, target)
			if err == nil {
				banPrefix, parseErr := netip.ParsePrefix(ban.Target)
				if parseErr == nil {
					matched = prefix.Overlaps(banPrefix)
				} else {
					matched = IpBanTargetCovers(target, ban.Target)
				}
			}
			if matched {
				states[target] = append(states[target], ban)
			}
		}
	}
	return states, nil
}

// UpsertIpBan 写入或更新一条封禁并重载缓存。target 必须已归一化。
// 已存在同目标封禁时只延长:永久封禁不会被降级为临时,更晚的过期时间覆盖更早的。
// 返回生效的封禁记录与是否发生了变更(新建或延长)。
// 目标列有唯一索引:并发封禁同一目标时只有一个能插入,其余锁住那一行后按延长规则处理,
// 不会写出两条记录(否则解除其中一条后另一条仍然生效)。
func UpsertIpBan(target string, reason string, expiresAt int64, source string, createdBy int) (*IpBan, bool, error) {
	now := common.GetTimestamp()
	reason = strings.TrimSpace(reason)

	var ban IpBan
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		ban = IpBan{
			Target:    target,
			Reason:    reason,
			ExpiresAt: expiresAt,
			Source:    source,
			CreatedBy: createdBy,
			CreatedAt: now,
			UpdatedAt: now,
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ban)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected > 0 {
			changed = true
			return nil
		}

		ban = IpBan{}
		if err := lockForUpdate(tx).Where("target = ?", target).First(&ban).Error; err != nil {
			return err
		}
		// 已是永久封禁:永不降级,保持原状。
		if ban.ExpiresAt == 0 {
			return nil
		}
		// 现有为临时封禁:仅当新期限为永久、或晚于现有期限时才更新。
		// 现有已过期时,新期限(未来时间)必然更晚,自然落入更新分支重新生效。
		if expiresAt != 0 && expiresAt <= ban.ExpiresAt {
			return nil
		}
		ban.Reason = reason
		ban.ExpiresAt = expiresAt
		ban.Source = source
		ban.CreatedBy = createdBy
		ban.UpdatedAt = now
		changed = true
		return tx.Model(&ban).Select("reason", "expires_at", "source", "created_by", "updated_at").Updates(&ban).Error
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		if err := ReloadIpBanCache(); err != nil {
			common.SysLog("failed to reload ip ban cache: " + err.Error())
		}
	}
	return &ban, changed, nil
}

// IP 封禁列表的状态筛选。
const (
	IpBanStatusActive    = "active"    // 生效中(含永久)
	IpBanStatusExpired   = "expired"   // 已过期、尚未清理
	IpBanStatusPermanent = "permanent" // 永久
)

// IpBanQuery 是封禁列表的筛选条件,空值表示不限。
type IpBanQuery struct {
	Keyword string // 对目标与原因做包含匹配
	Status  string
	Source  string
}

// GetIpBans 分页查询封禁列表。
func GetIpBans(filter IpBanQuery, startIdx int, num int) ([]*IpBan, int64, error) {
	query := DB.Model(&IpBan{})
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("target LIKE ? OR reason LIKE ?", like, like)
	}
	now := common.GetTimestamp()
	switch filter.Status {
	case IpBanStatusActive:
		query = query.Where("expires_at = 0 OR expires_at > ?", now)
	case IpBanStatusExpired:
		query = query.Where("expires_at <> 0 AND expires_at <= ?", now)
	case IpBanStatusPermanent:
		query = query.Where("expires_at = 0")
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var bans []*IpBan
	err := query.Order("id desc").Offset(startIdx).Limit(num).Find(&bans).Error
	return bans, total, err
}

// GetIpBanById 按主键取一条封禁。
func GetIpBanById(id int) (*IpBan, error) {
	if id <= 0 {
		return nil, errors.New("无效的封禁 ID")
	}
	var ban IpBan
	err := DB.First(&ban, id).Error
	return &ban, err
}

// DeleteIpBanById 删除一条封禁(解封)并重载缓存。
func DeleteIpBanById(id int) error {
	if id <= 0 {
		return errors.New("无效的封禁 ID")
	}
	if err := DB.Delete(&IpBan{}, id).Error; err != nil {
		return err
	}
	if err := ReloadIpBanCache(); err != nil {
		common.SysLog("failed to reload ip ban cache: " + err.Error())
	}
	return nil
}

// CleanupExpiredIpBans 删除过期超过宽限期的临时封禁。
// 宽限期让管理员在列表里还能看到刚过期的记录;审计依赖 risk_events,不靠本表。
func CleanupExpiredIpBans(graceSeconds int64) (int64, error) {
	cutoff := common.GetTimestamp() - graceSeconds
	result := DB.Where("expires_at <> 0 AND expires_at < ?", cutoff).Delete(&IpBan{})
	return result.RowsAffected, result.Error
}

// CountRecentIpBanEvents 统计某 IP 在 since 之后被风控自动封禁的次数(用于累犯升级)。
// 以风控事件表为准,临时封禁过期删除后历史仍可追溯。与账号侧一致,只数自动处置:
// 管理员手动封禁不是规则判定的违规,不该让下一次自动封禁直接升一档。
func CountRecentIpBanEvents(ip string, since int64) (int64, error) {
	var count int64
	err := DB.Model(&RiskEvent{}).
		Where("event_type = ?", RiskEventBanIp).
		Where("ip = ?", ip).
		Where("COALESCE(rule, '') <> ?", RiskBanSourceManual).
		Where("created_at >= ?", since).
		Count(&count).Error
	return count, err
}

// LastIpBanEventAt 返回某封禁目标最近一次 ban_ip 事件(自动或手动)的时间,从未被封禁过时为 0。
// 扫描规则用它判断窗口内的证据是否早于上次封禁:手动封禁同样算作一次处置,
// 它之前的证据已被管理员处理过,不该在封禁到期后再触发自动封禁。
func LastIpBanEventAt(ip string) (int64, error) {
	var latest []int64
	err := DB.Model(&RiskEvent{}).
		Where("event_type = ?", RiskEventBanIp).
		Where("ip = ?", ip).
		Order("created_at desc").
		Limit(1).
		Pluck("created_at", &latest).Error
	if err != nil || len(latest) == 0 {
		return 0, err
	}
	return latest[0], nil
}
