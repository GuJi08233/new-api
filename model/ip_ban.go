package model

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 动态 IP 封禁表:与风控设置里的静态 IP 黑名单(配置项)互补,
// 支持临时封禁(到期自动失效)与永久封禁,由自动规则、Probe Guard 或管理员写入。
// 匹配走全量内存缓存:请求路径零查库,缓存在本节点变更后立即重载,
// 其他节点由后台周期同步兜底。

// IP 封禁来源。
const (
	IpBanSourceManual     = "manual"      // 管理员手动添加
	IpBanSourceAutoRule   = "auto_rule"   // 自动封禁规则(周期扫描)
	IpBanSourceProbeGuard = "probe_guard" // Probe Guard 实时检测
	IpBanSourceErrorGuard = "error_guard" // Error Guard 实时错误率检测
)

type IpBan struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	Target    string `json:"target" gorm:"type:varchar(128);index:idx_ip_bans_target"` // 归一化的 IP 或 CIDR
	Reason    string `json:"reason" gorm:"type:varchar(512)"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint;index:idx_ip_bans_expires_at"` // 0 表示永久
	Source    string `json:"source" gorm:"type:varchar(32)"`
	CreatedBy int    `json:"created_by"` // 手动添加时的操作人,自动来源为 0
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_ip_bans_created_at"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
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

// UpsertIpBan 写入或更新一条封禁并重载缓存。target 必须已归一化。
// 已存在同目标封禁时只延长:永久封禁不会被降级为临时,更晚的过期时间覆盖更早的。
// 返回生效的封禁记录与是否发生了变更(新建或延长)。
func UpsertIpBan(target string, reason string, expiresAt int64, source string, createdBy int) (*IpBan, bool, error) {
	now := common.GetTimestamp()
	reason = strings.TrimSpace(reason)

	var ban IpBan
	err := DB.Where("target = ?", target).First(&ban).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ban = IpBan{
			Target:    target,
			Reason:    reason,
			ExpiresAt: expiresAt,
			Source:    source,
			CreatedBy: createdBy,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := DB.Create(&ban).Error; err != nil {
			return nil, false, err
		}
		if err := ReloadIpBanCache(); err != nil {
			common.SysLog("failed to reload ip ban cache: " + err.Error())
		}
		return &ban, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	// 已是永久封禁:永不降级,保持原状。
	if ban.ExpiresAt == 0 {
		return &ban, false, nil
	}
	// 现有为临时封禁:仅当新期限为永久、或晚于现有期限时才更新。
	// 现有已过期时,新期限(未来时间)必然更晚,自然落入更新分支重新生效。
	if expiresAt != 0 && expiresAt <= ban.ExpiresAt {
		return &ban, false, nil
	}

	ban.Reason = reason
	ban.ExpiresAt = expiresAt
	ban.Source = source
	ban.CreatedBy = createdBy
	ban.UpdatedAt = now
	if err := DB.Model(&ban).Select("reason", "expires_at", "source", "created_by", "updated_at").Updates(&ban).Error; err != nil {
		return nil, false, err
	}
	if err := ReloadIpBanCache(); err != nil {
		common.SysLog("failed to reload ip ban cache: " + err.Error())
	}
	return &ban, true, nil
}

// GetIpBans 分页查询封禁列表,keyword 对目标与原因做包含匹配。
func GetIpBans(keyword string, startIdx int, num int) ([]*IpBan, int64, error) {
	query := DB.Model(&IpBan{})
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("target LIKE ? OR reason LIKE ?", like, like)
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

// CountRecentIpBanEvents 统计某 IP 在 since 之后被封禁过的次数(用于累犯升级)。
// 以风控事件表为准,临时封禁过期删除后历史仍可追溯。
func CountRecentIpBanEvents(ip string, since int64) (int64, error) {
	var count int64
	err := DB.Model(&RiskEvent{}).
		Where("event_type = ?", RiskEventBanIp).
		Where("ip = ?", ip).
		Where("created_at >= ?", since).
		Count(&count).Error
	return count, err
}

// LastIpBanEventAt 返回某封禁目标最近一次 ban_ip 事件的时间,从未被封禁过时为 0。
// 扫描规则用它判断窗口内的证据是否早于上次封禁。
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
