package service

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// Error Guard:响应完成后实时统计「单 IP 短窗口内被拒绝多少次」,按状态码筛选。
// 与 Probe Guard 互补——后者看请求了多少不同模型,前者看被拒了多少次,
// 因此能抓到反复拿无效密钥试探(401)、乱传参数(400)这类只在响应里才现形的行为。
// 触发后对来源 IP 执行累犯升级封禁,拦截由请求入口已有的动态封禁检查完成,
// 本模块只负责计数与处置。

// errorGuardCooldown 触发处置后同一 IP 的冷却时长,避免一次爆发重复升级封禁。
const errorGuardCooldown = time.Minute

var errorGuardWindow = newIpEventWindow()

// RecordErrorGuardResponse 在响应完成后记录一次错误状态码。
// 只在风控总开关与 Error Guard 均开启时工作;非关注状态码、私网地址、
// 白名单/管理员用户,以及已处于封禁中的 IP 都不参与计数。
func RecordErrorGuardResponse(c *gin.Context, statusCode int) {
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled || !setting.ErrorGuardEnabled {
		return
	}
	if !matchErrorGuardStatusCode(setting, statusCode) {
		return
	}
	// 风控自己产生的拒绝不能计入,否则封禁会自我延长
	if common.GetContextKeyBool(c, constant.ContextKeyRiskBlocked) {
		return
	}
	ip, ok := normalizePublicIp(c.ClientIP())
	if !ok {
		return
	}
	// 已被封禁的 IP,其错误响应多半就是封禁本身产生的
	if _, banned := model.MatchActiveIpBan(ip); banned {
		return
	}

	// 豁免判定放在计数之后:与 Probe Guard 语义一致(白名单与管理员的请求计入窗口
	// 但不触发处置),同时把 model.IsAdmin 的查库挪出热路径 —— 否则一个卡在
	// 重试循环里的客户端会让每条错误响应都读一次库。
	count, triggered := recordErrorGuardEvent(setting, ip, statusCode, time.Now())
	if !triggered {
		return
	}

	userId := c.GetInt("id")
	if userId > 0 && (isRiskWhitelisted(setting, userId) || model.IsAdmin(userId)) {
		return
	}

	reason := fmt.Sprintf("IP %s 在 %d 秒内产生 %d 次错误响应(状态码 %v,阈值 %d)",
		ip, setting.ResolvedErrorGuardWindowSeconds(), count,
		setting.ResolvedErrorGuardStatusCodes(), setting.ResolvedErrorGuardThreshold())

	if setting.ErrorGuardDryRun {
		recordRuleAlert("error_guard", userId, "", ip, reason+"(演练模式,未封禁)")
		return
	}
	action := setting.ResolvedErrorGuardAction()
	if action == operation_setting.RiskRuleActionAlert {
		recordRuleAlert("error_guard", userId, "", ip, reason+"(动作为仅告警,未封禁)")
		return
	}

	applyRealtimeGuardAction(setting, model.RiskBanSourceErrorGuard, action,
		setting.ResolvedErrorGuardBanMinutes(), userId, ip, c.Request.UserAgent(), reason)
	common.SysLog(fmt.Sprintf("error guard: %s, user=%d, action=%s", reason, userId, action))
}

// matchErrorGuardStatusCode 判断状态码是否在关注集合内。
func matchErrorGuardStatusCode(setting *operation_setting.RiskControlSetting, statusCode int) bool {
	for _, code := range setting.ResolvedErrorGuardStatusCodes() {
		if code == statusCode {
			return true
		}
	}
	return false
}

// recordErrorGuardEvent 把一次错误响应记入该 IP 的滑动窗口,
// 返回窗口内的匹配错误次数与是否触发处置。
func recordErrorGuardEvent(setting *operation_setting.RiskControlSetting, ip string, statusCode int, now time.Time) (int, bool) {
	window := time.Duration(setting.ResolvedErrorGuardWindowSeconds()) * time.Second
	threshold := setting.ResolvedErrorGuardThreshold()

	return errorGuardWindow.record(ip, strconv.Itoa(statusCode), now, window, errorGuardCooldown,
		func(events []ipWindowEvent) (int, bool) {
			return len(events), len(events) >= threshold
		})
}
