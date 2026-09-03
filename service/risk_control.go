package service

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	return matchUaBlacklist(operation_setting.GetRiskControlSetting(), ua)
}

func matchUaBlacklist(setting *operation_setting.RiskControlSetting, ua string) (bool, string) {
	if ua == "" {
		return false, ""
	}
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
	return matchIpBlacklist(operation_setting.GetRiskControlSetting(), ipStr)
}

func matchIpBlacklist(setting *operation_setting.RiskControlSetting, ipStr string) (bool, string) {
	if ipStr == "" {
		return false, ""
	}
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

// isRiskWhitelisted 判断用户是否在风控白名单内:完全豁免,既不拦截也不处置。
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

// isAccountBanExempt 判断用户的账号是否豁免自动禁用。
// 除完全白名单外,持有公开/共享密钥的账号也豁免:滥用者不是账号主人,
// 禁用账号会连带打死所有正常使用者,这类场景只应封禁来源 IP。
func isAccountBanExempt(setting *operation_setting.RiskControlSetting, userId int) bool {
	if setting == nil {
		return false
	}
	if isRiskWhitelisted(setting, userId) {
		return true
	}
	for _, id := range setting.PublicKeyUserIds {
		if id == userId {
			return true
		}
	}
	return false
}

// CheckRequestRisk 在请求分发阶段做动态 IP 封禁与 IP / UA 黑名单校验。
// 动态 IP 封禁(手动添加或自动升级产生)独立于风控总开关始终生效;
// 静态黑名单受总开关控制,总开关关闭或黑名单全空时该部分零开销返回。
// 白名单用户完全豁免(不拦截、不自动处置),但其请求仍正常记录日志、计入风控统计。
// UA 命中且动作为 disable_user 时会同时禁用当前用户;
// 命中拦截会写入风控事件(按来源聚合限流)。
func CheckRequestRisk(c *gin.Context) error {
	return checkRequestRisk(c, operation_setting.GetRiskControlSetting())
}

func checkRequestRisk(c *gin.Context, setting *operation_setting.RiskControlSetting) error {
	userId := c.GetInt("id")
	whitelisted := userId > 0 && isRiskWhitelisted(setting, userId)
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	if !whitelisted && blockedByIpBan(c, ip, userId, ua) {
		return ErrRiskBlocked
	}

	if setting == nil || !setting.Enabled {
		return nil
	}
	if len(setting.UaBlacklist) == 0 && len(setting.IpBlacklist) == 0 {
		return nil
	}
	if whitelisted {
		return nil
	}

	if ip != "" {
		if matched, rule := matchIpBlacklist(setting, ip); matched {
			common.SysLog(fmt.Sprintf("risk control: blocked request, ip=%q hit rule=%q", ip, rule))
			recordBlockEvent(model.RiskEventBlockIp, userId, ip, ua, rule)
			markRiskBlocked(c)
			return ErrRiskBlocked
		}
	}

	matched, rule := matchUaBlacklist(setting, ua)
	if !matched {
		return nil
	}

	if setting.UaBlacklistAction == operation_setting.RiskUaActionDisableUser && userId > 0 {
		reason := fmt.Sprintf("UA 命中黑名单规则 [%s] 触发自动封禁", rule)
		if err := disableUserForRisk(setting, userId, reason, ip, ua); err != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", userId, err.Error()))
		}
	}
	common.SysLog(fmt.Sprintf("risk control: blocked request, ua=%q hit rule=%q", ua, rule))
	recordBlockEvent(model.RiskEventBlockUa, userId, ip, ua, rule)
	markRiskBlocked(c)
	return ErrRiskBlocked
}

// blockedByIpBan 判断来源地址是否命中生效中的动态封禁,命中时记录拦截事件并打标。
// 动态封禁独立于风控总开关:它是显式处置记录(手动添加或自动升级产生),
// 关闭检测开关不应让已生效的封禁失效。
func blockedByIpBan(c *gin.Context, ip string, userId int, ua string) bool {
	if ip == "" {
		return false
	}
	ban, matched := model.MatchActiveIpBan(ip)
	if !matched {
		return false
	}
	common.SysLog(fmt.Sprintf("risk control: blocked request, ip=%q hit active ban target=%q", ip, ban.Target))
	recordBlockEvent(model.RiskEventBlockIp, userId, ip, ua, "ban:"+ban.Target)
	markRiskBlocked(c)
	return true
}

// CheckIpBan 只做动态 IP 封禁校验,供中转链路以外的入口(模型列表、注册、验证码)复用。
// 全局白名单账号豁免;未认证的请求没有账号身份,一律按封禁处理。
// 纯内存匹配,未命中时零开销。
func CheckIpBan(c *gin.Context) bool {
	setting := operation_setting.GetRiskControlSetting()
	userId := c.GetInt("id")
	if userId > 0 && isRiskWhitelisted(setting, userId) {
		return false
	}
	return blockedByIpBan(c, c.ClientIP(), userId, c.Request.UserAgent())
}

// markRiskBlocked 标记本次请求是被风控主动拒绝的,供 Error Guard 排除自身产生的错误响应。
func markRiskBlocked(c *gin.Context) {
	if c != nil {
		c.Set(string(constant.ContextKeyRiskBlocked), true)
	}
}

// recordBlockEvent 记录一条黑名单拦截事件,按 (类型+用户+IP+规则) 聚合限流,
// 同一来源的重试风暴在窗口内合并为一条带累计次数的记录。
// 聚合键有意不含 UA:UA 由客户端任意填写、基数无界,注册与验证码这类未认证入口上
// 被封禁的地址只要每个请求换一个 UA,就能让每次拦截都单独落库并各占一个聚合桶。
// UA 只作为样本写入事件,取窗口内最近一次命中的值。
func recordBlockEvent(eventType string, userId int, ip string, ua string, rule string) {
	key := eventType + "\x00" + strconv.Itoa(userId) + "\x00" + ip + "\x00" + rule
	recordRiskEventThrottled(key, riskBlockEventWindow, model.RiskEvent{
		EventType: eventType,
		UserId:    userId,
		Ip:        ip,
		Ua:        ua,
		Rule:      rule,
	}, time.Now())
}

// DisableUserForRisk 因风控原因禁用用户,复用用户管理的禁用语义:
// 置 status=禁用 → 落库 → 失效用户缓存与其全部令牌缓存 → 记录管理日志与封禁事件。
// 对 root/admin 角色、白名单用户、已禁用用户幂等跳过。
// sourceIp 为触发处置的来源 IP(扫描规则为命中 IP,请求内处置为客户端 IP),可为空。
func DisableUserForRisk(userId int, reason string, sourceIp string) error {
	return disableUserForRisk(operation_setting.GetRiskControlSetting(), userId, reason, sourceIp, "")
}

func disableUserForRisk(setting *operation_setting.RiskControlSetting, userId int, reason string, sourceIp string, sourceUa string) error {
	if userId <= 0 {
		return nil
	}
	if isAccountBanExempt(setting, userId) {
		// 豁免不能是静默的:否则规则对着一个永远处置不了的账号空转,你无从发现
		recordExemptSkipAlert(userId, sourceIp, reason)
		return nil
	}

	changed, err := model.DisableRegularUser(userId, reason)
	if err != nil {
		return err
	}
	if !changed {
		return nil
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
	username, _ := model.GetUsernameById(userId, false)
	insertRiskEventLogged(model.RiskEvent{
		EventType: model.RiskEventBanAuto,
		UserId:    userId,
		Username:  username,
		Ip:        sourceIp,
		Ua:        sourceUa,
		Reason:    reason,
	})
	common.SysLog(fmt.Sprintf("risk control: disabled user %d, reason=%s", userId, reason))
	return nil
}

var riskControlDaemonOnce sync.Once

// riskEventCleanupInterval 拦截/告警事件保留期清理的执行周期。
const riskEventCleanupInterval = 12 * time.Hour

// ipBanCacheSyncInterval 动态 IP 封禁缓存的周期同步间隔。
// 本节点变更后立即重载,该同步兜底多节点部署下其他节点的封禁生效与过期失效。
const ipBanCacheSyncInterval = time.Minute

// expiredIpBanGraceSeconds 过期临时封禁的保留宽限期,便于列表页查看刚过期的记录。
const expiredIpBanGraceSeconds = 72 * 3600

// RiskControlDaemon 启动风控后台任务:
// 全部节点运行拦截事件冲刷循环与 IP 封禁缓存同步;
// 仅 Master 节点运行自动封禁扫描与事件/过期封禁清理。
func RiskControlDaemon() {
	riskControlDaemonOnce.Do(func() {
		if err := model.ReloadIpBanCache(); err != nil {
			common.SysLog("risk control: failed to load ip ban cache: " + err.Error())
		}
		go func() {
			for {
				time.Sleep(riskEventFlushInterval)
				flushStaleRiskEvents(time.Now())
			}
		}()
		go func() {
			for {
				time.Sleep(ipBanCacheSyncInterval)
				if err := model.ReloadIpBanCache(); err != nil {
					common.SysLog("risk control: failed to sync ip ban cache: " + err.Error())
				}
			}
		}()

		if !common.IsMasterNode {
			return
		}
		var lastCleanup time.Time
		for {
			setting := operation_setting.GetRiskControlSetting()
			if time.Since(lastCleanup) >= riskEventCleanupInterval {
				cutoff := time.Now().AddDate(0, 0, -setting.ResolvedEventRetentionDays()).Unix()
				if deleted, err := model.CleanupRiskEvents(cutoff); err != nil {
					common.SysLog("risk control: failed to cleanup risk events: " + err.Error())
				} else if deleted > 0 {
					common.SysLog(fmt.Sprintf("risk control: cleaned up %d expired risk events", deleted))
				}
				if deleted, err := model.CleanupExpiredIpBans(expiredIpBanGraceSeconds); err != nil {
					common.SysLog("risk control: failed to cleanup expired ip bans: " + err.Error())
				} else if deleted > 0 {
					common.SysLog(fmt.Sprintf("risk control: cleaned up %d expired ip bans", deleted))
				}
				lastCleanup = time.Now()
			}
			if setting == nil || !setting.Enabled || !hasEnabledAutoBanRule(setting) {
				time.Sleep(1 * time.Minute)
				continue
			}
			runRiskScan(setting)

			scanMinutes := setting.ScanMinutes
			if scanMinutes <= 0 {
				scanMinutes = operation_setting.RiskDefaultScanMinutes
			} else if scanMinutes > operation_setting.RiskMaxScanMinutes {
				scanMinutes = operation_setting.RiskMaxScanMinutes
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
// 全局白名单账号的日志行在查询阶段就被剔除,因此它们既不会自己上榜被禁用,
// 也不会把自己使用的 IP 顶上 IP 维度排行后连带封掉整个出口地址。
func runRiskScan(setting *operation_setting.RiskControlSetting) {
	exempt := setting.WhitelistUserIds
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
			scanIpMultiUser(rule, window, exempt)
		case operation_setting.RiskMetricUserMultiIp:
			scanUserMultiIp(rule, window, exempt)
		case operation_setting.RiskMetricIpMultiToken:
			scanIpMultiToken(rule, window, exempt)
		case operation_setting.RiskMetricUserTinyRequest:
			scanUserTinyRequest(rule, window, setting.ResolvedTinyRequestMaxTokens(), exempt)
		case operation_setting.RiskMetricUserErrorBurst:
			scanUserErrorBurst(rule, window, exempt)
		}
	}
}

// recordRuleAlert 记录一条自动规则告警事件,按 (指标+目标) 长窗口聚合,
// 避免周期扫描对同一目标反复刷屏。
func recordRuleAlert(metric string, userId int, username string, ip string, reason string) {
	common.SysLog("risk control alert: " + reason)
	key := model.RiskEventAlert + "\x00" + metric + "\x00" + strconv.Itoa(userId) + "\x00" + ip
	recordRiskEventThrottled(key, riskAlertEventWindow, model.RiskEvent{
		EventType: model.RiskEventAlert,
		UserId:    userId,
		Username:  username,
		Ip:        ip,
		Rule:      metric,
		Reason:    reason,
	}, time.Now())
}

// recordExemptSkipAlert 记录一条「命中规则但因白名单跳过账号处置」的告警。
// 与其他告警共用聚合窗口,同一账号的反复命中不会刷屏。
// 有意不查用户名:本函数可能被重试循环反复触发(例如用户级白名单账号命中 UA 黑名单),
// 而事件已带 user_id,列表页据此展示即可。
func recordExemptSkipAlert(userId int, sourceIp string, reason string) {
	recordRuleAlert("exempt:disable_user", userId, "", sourceIp,
		fmt.Sprintf("用户 #%d 命中处置但已跳过:该账号在白名单内。命中原因:%s", userId, reason))
}

// disableIpAssociatedUsers 处置某 IP 在窗口内关联的全部用户(IP 维度规则共用)。
func disableIpAssociatedUsers(ip string, window int, reason string) {
	userIds, err := model.GetIpAssociatedUserIds(ip, window)
	if err != nil {
		common.SysLog("risk control: failed to fetch users for ip " + ip + ": " + err.Error())
		return
	}
	for _, uid := range userIds {
		if disErr := DisableUserForRisk(uid, reason, ip); disErr != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", uid, disErr.Error()))
		}
	}
}

// ipBanOffenseLookback 累犯计数的回溯窗口:统计该 IP 近 90 天内的封禁事件次数。
const ipBanOffenseLookback = 90 * 24 * time.Hour

// whitelistIpLookbackHours 判定「该地址是否仍有全局白名单账号在用」的回溯窗口(小时)。
// 取得太长会让一个曾被白名单账号用过的动态 IP 长期免疫,太短会在账号闲置时失效。
const whitelistIpLookbackHours = 24

// isWhitelistedBanTarget 判断封禁目标是否覆盖近期有全局白名单账号在用的地址。
// 全局白名单的语义是「这个账号的任何流量都不该触发处置」,而封掉它的出口地址
// 会连带拦下同出口的其他正常用户,因此这类目标不参与自动封禁。
// 目标可能是 IPv6 归并后的整段前缀,所以判定必须按网段包含而不是地址相等。
// 判定依据 logs.ip,该列只在开启 IP 日志(全局或该用户自己的设置)时才有值。
// 查询失败时返回 false 让封禁照常执行:保护机制不该让一次日志库抖动等于风控整体停摆,
// 而白名单账号自身始终豁免封禁检查,不会被自己触发的封禁锁在门外。
func isWhitelistedBanTarget(setting *operation_setting.RiskControlSetting, target string) bool {
	if setting == nil || len(setting.WhitelistUserIds) == 0 {
		return false
	}
	ips, truncated, err := model.GetRecentIpsByUsers(whitelistIpLookbackHours, setting.WhitelistUserIds)
	if err != nil {
		common.SysLog(fmt.Sprintf("risk control: failed to load whitelist ips while evaluating %s: %s", target, err.Error()))
		return false
	}
	if truncated {
		common.SysLog(fmt.Sprintf("risk control: whitelist ip sample hit the cap while evaluating %s, exemption may be incomplete", target))
	}
	// 全局 IP 日志关闭、白名单账号又没有单独开启时,这里必然查不到地址,豁免形同虚设;
	// 实时防护直接用连接地址封禁、不依赖日志,照样会封掉白名单账号的出口。
	// 设置页在同样条件下显示警告,这里留一条日志给只看后台的人。
	if len(ips) == 0 && !common.IsGlobalRecordIpLogEnabled() {
		common.SysLog(fmt.Sprintf("risk control: whitelist ip exemption is inactive while evaluating %s: ip logging is disabled and no whitelist ip has been recorded", target))
		return false
	}
	for _, ip := range ips {
		if model.IpBanTargetCovers(target, ip) {
			return true
		}
	}
	return false
}

// EscalateIpBan 封禁某 IP。fixedMinutes > 0 时使用该固定时长,不参与累犯升级
// (规则级时长覆盖);为 0 时按累犯次数走全局阶梯:首次 → 再犯加时 → 达到配置次数后永久。
// 累犯次数以 ban_ip 风控事件为准,临时封禁过期删除后历史仍可追溯。
// 实际发生新建/延长时写入 ban_ip 事件并返回动作描述;
// 已有更长效封禁覆盖时返回空串(幂等,不重复记事件)。
// IPv6 目标按配置的前缀长度归并(默认 /64),否则客户端换个地址就绕过。
// 全局白名单账号近期使用过的地址一律跳过,只记告警——这条兜底覆盖实时防护
// 与扫描规则的全部自动封禁路径;管理员手动封禁不经过本函数,不受影响。
func EscalateIpBan(ip string, reason string, source string, fixedMinutes int) (string, error) {
	setting := operation_setting.GetRiskControlSetting()
	target, err := model.NormalizeAutoBanTarget(ip, setting.ResolvedIpBanIpv6PrefixLength())
	if err != nil {
		return "", err
	}
	if isWhitelistedBanTarget(setting, target) {
		recordRuleAlert(source, 0, "", target, fmt.Sprintf(
			"IP %s 命中处置但已跳过封禁:近 %d 小时内有全局白名单账号在使用该地址。命中原因:%s",
			target, whitelistIpLookbackHours, reason))
		return "", nil
	}
	now := time.Now()

	offenses, err := model.CountRecentIpBanEvents(target, now.Add(-ipBanOffenseLookback).Unix())
	if err != nil {
		return "", err
	}
	offense := int(offenses) + 1

	var expiresAt int64
	var action string
	permanentOffense := setting.ResolvedIpBanPermanentOffense()
	switch {
	case fixedMinutes > 0:
		expiresAt = now.Add(time.Duration(fixedMinutes) * time.Minute).Unix()
		action = fmt.Sprintf("临时封禁 %d 分钟(第 %d 次违规,规则固定时长)", fixedMinutes, offense)
	case permanentOffense > 0 && offense >= permanentOffense:
		expiresAt = 0
		action = fmt.Sprintf("永久封禁(第 %d 次违规)", offense)
	default:
		// 违规次数超出阶梯长度时停在最后一档
		ladder := setting.ResolvedIpBanEscalationMinutes()
		step := offense - 1
		if step >= len(ladder) {
			step = len(ladder) - 1
		}
		minutes := ladder[step]
		expiresAt = now.Add(time.Duration(minutes) * time.Minute).Unix()
		if offense == 1 {
			action = fmt.Sprintf("临时封禁 %d 分钟(首次违规)", minutes)
		} else {
			action = fmt.Sprintf("临时封禁 %d 分钟(第 %d 次违规)", minutes, offense)
		}
	}

	ban, changed, err := model.UpsertIpBan(target, reason, expiresAt, source, 0)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", nil
	}
	insertRiskEventLogged(model.RiskEvent{
		EventType: model.RiskEventBanIp,
		Ip:        target,
		Rule:      source,
		Reason:    fmt.Sprintf("%s:%s", action, reason),
	})
	common.SysLog(fmt.Sprintf("risk control: ip %s banned (%s), source=%s, ban_id=%d", target, action, source, ban.Id))
	return action, nil
}

// applyIpRuleAction 执行 IP 维度规则命中后的处置动作。
// ban_both 会同时封禁来源 IP 与该 IP 在窗口内关联的账号;公开密钥账号只会被跳过账号处置,
// IP 封禁照常生效。
func applyIpRuleAction(rule operation_setting.RiskAutoBanRule, ip string, window int, reason string) {
	if rule.Action == operation_setting.RiskRuleActionAlert || rule.Action == "" {
		recordRuleAlert(rule.Metric, 0, "", ip, reason)
		return
	}
	if operation_setting.RiskActionBansIp(rule.Action) {
		if _, err := EscalateIpBan(ip, reason, model.IpBanSourceAutoRule, rule.BanMinutes); err != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to ban ip %s: %s", ip, err.Error()))
		}
	}
	if operation_setting.RiskActionDisablesUser(rule.Action) {
		disableIpAssociatedUsers(ip, window, reason)
	}
}

// applyRealtimeGuardAction 执行实时防护(Probe / Error Guard)命中后的处置:
// 按动作封禁来源 IP、禁用账号或两者都做。账号处置会跳过完全白名单与公开密钥账号,
// 因此对共享密钥的场景可以配 ban_both —— IP 被封,账号主人不受影响。
func applyRealtimeGuardAction(setting *operation_setting.RiskControlSetting, source string,
	action string, banMinutes int, userId int, ip string, ua string, reason string) {
	if operation_setting.RiskActionBansIp(action) {
		if _, err := EscalateIpBan(ip, reason, source, banMinutes); err != nil {
			common.SysLog(fmt.Sprintf("%s: failed to ban ip %s: %s", source, ip, err.Error()))
		}
	}
	if operation_setting.RiskActionDisablesUser(action) && userId > 0 {
		if err := disableUserForRisk(setting, userId, reason, ip, ua); err != nil {
			common.SysLog(fmt.Sprintf("%s: failed to disable user %d: %s", source, userId, err.Error()))
		}
	}
}

// scanIpMultiUser 处理「单 IP 关联多用户」规则。
func scanIpMultiUser(rule operation_setting.RiskAutoBanRule, window int, exemptUserIds []int) {
	items, err := model.GetIpMultiUserRanking(window, riskScanLimit, exemptUserIds)
	if err != nil {
		common.SysLog("risk control scan (ip_multi_user) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.UserCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("IP %s 在 %d 小时内关联 %d 个用户(阈值 %d)", item.Ip, window, item.UserCount, rule.Threshold)
		applyIpRuleAction(rule, item.Ip, window, reason)
	}
}

// scanUserMultiIp 处理「单用户使用多 IP」规则:命中的用户被处置。
func scanUserMultiIp(rule operation_setting.RiskAutoBanRule, window int, exemptUserIds []int) {
	items, err := model.GetUserMultiIpRanking(window, riskScanLimit, exemptUserIds)
	if err != nil {
		common.SysLog("risk control scan (user_multi_ip) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.IpCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("用户 %s(#%d)在 %d 小时内使用 %d 个 IP(阈值 %d)", item.Username, item.UserId, window, item.IpCount, rule.Threshold)
		if !operation_setting.RiskActionDisablesUser(rule.Action) {
			recordRuleAlert(rule.Metric, item.UserId, item.Username, "", reason)
			continue
		}
		if disErr := DisableUserForRisk(item.UserId, reason, ""); disErr != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", item.UserId, disErr.Error()))
		}
	}
}

// scanIpMultiToken 处理「单 IP 使用多令牌」规则(批量测活)。
func scanIpMultiToken(rule operation_setting.RiskAutoBanRule, window int, exemptUserIds []int) {
	items, err := model.GetIpMultiTokenRanking(window, riskScanLimit, exemptUserIds)
	if err != nil {
		common.SysLog("risk control scan (ip_multi_token) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.TokenCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("IP %s 在 %d 小时内使用 %d 个令牌(阈值 %d),疑似批量测活", item.Ip, window, item.TokenCount, rule.Threshold)
		applyIpRuleAction(rule, item.Ip, window, reason)
	}
}

// scanUserTinyRequest 处理「用户微量请求」规则(自动测活):命中的用户被处置。
func scanUserTinyRequest(rule operation_setting.RiskAutoBanRule, window int, maxTokens int, exemptUserIds []int) {
	items, err := model.GetUserTinyRequestRanking(window, riskScanLimit, maxTokens, exemptUserIds)
	if err != nil {
		common.SysLog("risk control scan (user_tiny_request) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.RequestCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("用户 %s(#%d)在 %d 小时内发起 %d 次微量请求(输入输出均 ≤ %d tokens,阈值 %d),疑似自动测活", item.Username, item.UserId, window, item.RequestCount, maxTokens, rule.Threshold)
		if !operation_setting.RiskActionDisablesUser(rule.Action) {
			recordRuleAlert(rule.Metric, item.UserId, item.Username, "", reason)
			continue
		}
		if disErr := DisableUserForRisk(item.UserId, reason, ""); disErr != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", item.UserId, disErr.Error()))
		}
	}
}

// scanUserErrorBurst 处理「用户错误请求爆发」规则:命中的用户被处置。
func scanUserErrorBurst(rule operation_setting.RiskAutoBanRule, window int, exemptUserIds []int) {
	items, err := model.GetUserErrorRanking(window, riskScanLimit, exemptUserIds)
	if err != nil {
		common.SysLog("risk control scan (user_error_burst) failed: " + err.Error())
		return
	}
	for _, item := range items {
		if item.RequestCount <= rule.Threshold {
			continue
		}
		reason := fmt.Sprintf("用户 %s(#%d)在 %d 小时内产生 %d 次错误请求(阈值 %d)", item.Username, item.UserId, window, item.RequestCount, rule.Threshold)
		if !operation_setting.RiskActionDisablesUser(rule.Action) {
			recordRuleAlert(rule.Metric, item.UserId, item.Username, "", reason)
			continue
		}
		if disErr := DisableUserForRisk(item.UserId, reason, ""); disErr != nil {
			common.SysLog(fmt.Sprintf("risk control: failed to disable user %d: %s", item.UserId, disErr.Error()))
		}
	}
}
