package service

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// Probe Guard:请求内实时检测「单 IP 在短窗口内遍历多个不同模型」的批量测活行为。
// 与周期扫描互补:扫描抓分钟级的统计特征(微量请求、多令牌),Probe Guard 秒级响应。
// 触发后对来源 IP 执行累犯升级封禁(EscalateIpBan),不封用户——测活者常持他人密钥,
// 封 IP 打击攻击源且避免误伤密钥主人。

// ErrProbeGuardBlocked 表示请求因命中批量测活检测被拒绝。
var ErrProbeGuardBlocked = errors.New("request blocked by probe guard")

// probeGuardCooldown 触发处置后同一 IP 的冷却时长,避免一次爆发重复升级封禁。
const probeGuardCooldown = time.Minute

var probeGuardWindow = newIpEventWindow()

// CheckProbeGuard 在模型解析完成后调用。返回 ErrProbeGuardBlocked 表示应拒绝当前请求。
// 白名单用户与管理员的请求计入窗口(与"仍计入统计"的白名单语义一致)但不触发处置。
func CheckProbeGuard(c *gin.Context, modelName string) error {
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled || !setting.ProbeGuardEnabled {
		return nil
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	ip, ok := normalizePublicIp(c.ClientIP())
	if !ok {
		return nil
	}

	distinct, triggered := recordProbeGuardRequest(setting, ip, modelName, time.Now())
	if !triggered {
		return nil
	}

	userId := c.GetInt("id")
	reason := fmt.Sprintf("IP %s 在 %d 秒内请求 %d 个不同模型(阈值 %d),疑似批量测活",
		ip, setting.ResolvedProbeGuardWindowSeconds(), distinct, setting.ResolvedProbeGuardModelCount())

	if userId > 0 && (isRiskWhitelisted(setting, userId) || model.IsAdmin(userId)) {
		common.SysLog("probe guard: whitelisted/admin user " + fmt.Sprint(userId) + " exempted, " + reason)
		return nil
	}

	if setting.ProbeGuardDryRun {
		recordRuleAlert("probe_guard", userId, "", ip, reason+"(演练模式,未封禁)")
		return nil
	}
	action := setting.ResolvedProbeGuardAction()
	if action == operation_setting.RiskRuleActionAlert {
		recordRuleAlert("probe_guard", userId, "", ip, reason+"(动作为仅告警,未封禁)")
		return nil
	}

	// 处置失败不放行请求:检测已确认,拒绝当前请求仍是安全侧
	applyRealtimeGuardAction(setting, model.RiskBanSourceProbeGuard, action,
		setting.ResolvedProbeGuardBanMinutes(), userId, ip, c.Request.UserAgent(), reason)
	common.SysLog(fmt.Sprintf("probe guard: blocked request, %s, user=%d, action=%s", reason, userId, action))
	markRiskBlocked(c)
	return ErrProbeGuardBlocked
}

// recordProbeGuardRequest 把请求记入该 IP 的滑动窗口,返回窗口内不同模型数与是否触发。
// 触发后进入冷却期,冷却内的后续命中不重复触发。
func recordProbeGuardRequest(setting *operation_setting.RiskControlSetting, ip string, modelName string, now time.Time) (int, bool) {
	window := time.Duration(setting.ResolvedProbeGuardWindowSeconds()) * time.Second
	threshold := setting.ResolvedProbeGuardModelCount()

	return probeGuardWindow.record(ip, modelName, now, window, probeGuardCooldown,
		func(events []ipWindowEvent) (int, bool) {
			distinct := map[string]struct{}{}
			for _, event := range events {
				distinct[event.label] = struct{}{}
			}
			return len(distinct), len(distinct) >= threshold
		})
}

// normalizePublicIp 解析客户端 IP 并排除私网/环回/链路本地等不具备封禁意义的地址。
func normalizePublicIp(raw string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return "", false
	}
	return addr.String(), true
}
