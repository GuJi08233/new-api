package service

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// riskRegexCache 缓存已编译的 UA 黑名单正则,避免每次请求重复编译。
var riskRegexCache sync.Map // map[string]*regexp.Regexp

// riskCidrCache 缓存已解析的 IP 黑名单条目(*net.IPNet),避免每次请求重复解析。
var riskCidrCache sync.Map // map[string]*net.IPNet

// riskScanLimit 单轮自动封禁扫描每个规则处理的排行榜条目上限。
const riskScanLimit = 500

// ErrRiskBlocked 表示请求因命中风控黑名单(UA 或 IP)被拦截。
var ErrRiskBlocked = errors.New("request blocked by risk control")

// looksLikeRegex 粗略判断一条黑名单条目是否应按正则处理。
// 不含正则元字符的条目一律走大小写不敏感子串匹配,更符合直觉。
func looksLikeRegex(pattern string) bool {
	return strings.ContainsAny(pattern, `\^$.|?*+()[]{}`)
}

// matchUaEntry 判断 UA 是否命中单条黑名单规则。
func matchUaEntry(entry string, ua string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	if looksLikeRegex(entry) {
		compiled, ok := riskRegexCache.Load(entry)
		if !ok {
			re, err := regexp.Compile(entry)
			if err != nil {
				// 正则非法时退化为子串匹配,避免整条规则失效
				return strings.Contains(strings.ToLower(ua), strings.ToLower(entry))
			}
			compiled, _ = riskRegexCache.LoadOrStore(entry, re)
		}
		if re, ok := compiled.(*regexp.Regexp); ok {
			return re.MatchString(ua)
		}
		return false
	}
	return strings.Contains(strings.ToLower(ua), strings.ToLower(entry))
}

// MatchUaBlacklist 返回 UA 是否命中黑名单,以及命中的规则。
func MatchUaBlacklist(ua string) (bool, string) {
	if ua == "" {
		return false, ""
	}
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled || len(setting.UaBlacklist) == 0 {
		return false, ""
	}
	for _, entry := range setting.UaBlacklist {
		if matchUaEntry(entry, ua) {
			return true, strings.TrimSpace(entry)
		}
	}
	return false, ""
}

// matchIpEntry 判断 IP 是否命中单条黑名单规则(精确 IP 或 CIDR)。
func matchIpEntry(entry string, ip net.IP) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" || ip == nil {
		return false
	}
	if cached, ok := riskCidrCache.Load(entry); ok {
		if ipNet, ok := cached.(*net.IPNet); ok && ipNet != nil {
			return ipNet.Contains(ip)
		}
		return false
	}
	var ipNet *net.IPNet
	if _, parsed, err := net.ParseCIDR(entry); err == nil {
		ipNet = parsed
	} else if exact := net.ParseIP(entry); exact != nil {
		// 精确 IP 归一化为单地址 CIDR,统一用 Contains 判断
		bits := 32
		if exact.To4() == nil {
			bits = 128
		}
		ipNet = &net.IPNet{IP: exact, Mask: net.CIDRMask(bits, bits)}
	}
	// 解析失败缓存 nil,避免每次请求重复解析非法条目
	riskCidrCache.LoadOrStore(entry, ipNet)
	return ipNet != nil && ipNet.Contains(ip)
}

// MatchIpBlacklist 返回 IP 是否命中黑名单,以及命中的规则。
func MatchIpBlacklist(ipStr string) (bool, string) {
	if ipStr == "" {
		return false, ""
	}
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled || len(setting.IpBlacklist) == 0 {
		return false, ""
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false, ""
	}
	for _, entry := range setting.IpBlacklist {
		if matchIpEntry(entry, ip) {
			return true, strings.TrimSpace(entry)
		}
	}
	return false, ""
}

// isRiskWhitelisted 判断用户是否在风控白名单内(永不自动处置)。
func isRiskWhitelisted(setting *operation_setting.RiskControlSetting, userId int) bool {
	if setting == nil {
		return false
	}
	for _, id := range setting.WhitelistUserIds {
		if id == userId {
			return true
		}
	}
	return false
}

// CheckRequestRisk 在请求分发阶段做 IP / UA 黑名单校验。
// 未命中返回 nil;UA 命中且动作为 disable_user 时会同时禁用当前用户;
// IP 命中一律直接拒绝调用。总开关关闭或黑名单全空时零开销返回。
func CheckRequestRisk(c *gin.Context) error {
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled {
		return nil
	}
	if len(setting.UaBlacklist) == 0 && len(setting.IpBlacklist) == 0 {
		return nil
	}

	if ip := c.ClientIP(); ip != "" {
		if matched, rule := MatchIpBlacklist(ip); matched {
			common.SysLog(fmt.Sprintf("risk control: blocked request, ip=%q hit rule=%q", ip, rule))
			return ErrRiskBlocked
		}
	}

	ua := c.Request.UserAgent()
	matched, rule := MatchUaBlacklist(ua)
	if !matched {
		return nil
	}

	if setting.UaBlacklistAction == operation_setting.RiskUaActionDisableUser {
		userId := c.GetInt("id")
		if userId > 0 && !isRiskWhitelisted(setting, userId) {
			reason := fmt.Sprintf("UA 命中黑名单规则 [%s] 触发自动封禁", rule)
			if err := DisableUserForRisk(userId, reason); err != nil {
				common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", userId, err.Error()))
			}
		}
	}
	common.SysLog(fmt.Sprintf("risk control: blocked request, ua=%q hit rule=%q", ua, rule))
	return ErrRiskBlocked
}

// DisableUserForRisk 因风控原因禁用用户,复用用户管理的禁用语义:
// 置 status=禁用 → 落库 → 失效用户缓存与其全部令牌缓存 → 记录管理日志。
// 对 root/admin 角色、白名单用户、已禁用用户幂等跳过。
func DisableUserForRisk(userId int, reason string) error {
	if userId <= 0 {
		return nil
	}
	setting := operation_setting.GetRiskControlSetting()
	if isRiskWhitelisted(setting, userId) {
		return nil
	}

	user, err := model.GetUserById(userId, false)
	if err != nil {
		return err
	}
	if user == nil {
		return nil
	}
	// 保护管理员/超级管理员账户,避免误封
	if user.Role >= common.RoleAdminUser {
		return nil
	}
	if user.Status == common.UserStatusDisabled {
		return nil // 已禁用,幂等
	}

	user.Status = common.UserStatusDisabled
	if err := user.Update(false); err != nil {
		return err
	}
	if err := model.InvalidateUserCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("risk control: failed to invalidate user cache for %d: %s", userId, err.Error()))
	}
	if err := model.InvalidateUserTokensCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("risk control: failed to invalidate tokens cache for %d: %s", userId, err.Error()))
	}
	model.RecordLogWithAdminInfo(userId, model.LogTypeManage, "[风控] "+reason, map[string]interface{}{
		"source": "risk_control",
		"reason": reason,
	})
	common.SysLog(fmt.Sprintf("risk control: disabled user %d, reason=%s", userId, reason))
	return nil
}

var riskControlDaemonOnce sync.Once

// RiskControlDaemon 启动风控自动封禁后台扫描。仅在 Master 节点运行。
func RiskControlDaemon() {
	if !common.IsMasterNode {
		return
	}
	riskControlDaemonOnce.Do(func() {
		for {
			setting := operation_setting.GetRiskControlSetting()
			if setting == nil || !setting.Enabled || !hasEnabledAutoBanRule(setting) {
				time.Sleep(1 * time.Minute)
				continue
			}
			runRiskScan(setting)

			scanMinutes := setting.ScanMinutes
			if scanMinutes <= 0 {
				scanMinutes = operation_setting.RiskDefaultScanMinutes
			}
			time.Sleep(time.Duration(scanMinutes) * time.Minute)
		}
	})
}

func hasEnabledAutoBanRule(setting *operation_setting.RiskControlSetting) bool {
	for _, rule := range setting.AutoBanRules {
		if rule.Enabled {
			return true
		}
	}
	return false
}

// runRiskScan 执行一轮自动封禁扫描:逐条评估启用的规则。
func runRiskScan(setting *operation_setting.RiskControlSetting) {
	for _, rule := range setting.AutoBanRules {
		if !rule.Enabled {
			continue
		}
		window := rule.WindowHours
		if window <= 0 {
			window = operation_setting.RiskDefaultWindowHours
		}
		switch rule.Metric {
		case operation_setting.RiskMetricIpMultiUser:
			scanIpMultiUser(rule, window)
		case operation_setting.RiskMetricUserMultiIp:
			scanUserMultiIp(rule, window)
		}
	}
}

// scanIpMultiUser 处理「单 IP 关联多用户」规则:命中的 IP 下全部关联用户被处置。
func scanIpMultiUser(rule operation_setting.RiskAutoBanRule, window int) {
	items, err := model.GetIpMultiUserRanking(window, riskScanLimit)
	if err != nil {
		common.SysLog("risk control scan (ip_multi_user) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.UserCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("IP %s 在 %d 小时内关联 %d 个用户(阈值 %d)", item.Ip, window, item.UserCount, rule.Threshold)
		if rule.Action != operation_setting.RiskRuleActionDisableUser {
			common.SysLog("risk control alert: " + reason)
			continue
		}
		userIds, err := model.GetIpAssociatedUserIds(item.Ip, window)
		if err != nil {
			common.SysLog("risk control: failed to fetch users for ip " + item.Ip + ": " + err.Error())
			continue
		}
		for _, uid := range userIds {
			if disErr := DisableUserForRisk(uid, reason); disErr != nil {
				common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", uid, disErr.Error()))
			}
		}
	}
}

// scanUserMultiIp 处理「单用户使用多 IP」规则:命中的用户被处置。
func scanUserMultiIp(rule operation_setting.RiskAutoBanRule, window int) {
	items, err := model.GetUserMultiIpRanking(window, riskScanLimit)
	if err != nil {
		common.SysLog("risk control scan (user_multi_ip) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.IpCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("用户 %s(#%d)在 %d 小时内使用 %d 个 IP(阈值 %d)", item.Username, item.UserId, window, item.IpCount, rule.Threshold)
		if rule.Action != operation_setting.RiskRuleActionDisableUser {
			common.SysLog("risk control alert: " + reason)
			continue
		}
		if disErr := DisableUserForRisk(item.UserId, reason); disErr != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", item.UserId, disErr.Error()))
		}
	}
}
