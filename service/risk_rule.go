package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type RiskRuleHit struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Username string `json:"username,omitempty"`
	Observed int    `json:"observed"`
	Reason   string `json:"reason"`
}

type RiskRulePreview struct {
	Rule         operation_setting.RiskAutoBanRule `json:"rule"`
	Version      string                            `json:"version"`
	Hits         []RiskRuleHit                     `json:"hits"`
	Checked      int                               `json:"checked"`
	Limit        int                               `json:"limit"`
	LimitReached bool                              `json:"limit_reached"`
}

// PreviewRiskRule 与扫描共用统计与严格大于的阈值判定，只读日志，不执行处置。
func PreviewRiskRule(rule operation_setting.RiskAutoBanRule, tinyMaxTokens int, exemptUserIds []int) (*RiskRulePreview, error) {
	window := rule.WindowHours
	if window <= 0 {
		window = operation_setting.RiskDefaultWindowHours
	}
	result := &RiskRulePreview{Rule: rule, Hits: []RiskRuleHit{}, Limit: riskScanLimit}
	result.Version = operation_setting.RiskRuleVersion(struct {
		Rule          operation_setting.RiskAutoBanRule
		TinyMaxTokens int
	}{rule, tinyMaxTokens})
	var err error
	switch rule.Metric {
	case operation_setting.RiskMetricIpMultiUser:
		var items []model.IpRankItem
		items, err = model.GetIpMultiUserRanking(window, riskScanLimit, exemptUserIds)
		result.Checked = len(items)
		for _, item := range items {
			if item.UserCount > rule.Threshold {
				result.Hits = append(result.Hits, RiskRuleHit{Type: "ip", Value: item.Ip, Observed: item.UserCount,
					Reason: fmt.Sprintf("IP %s 在 %d 小时内关联 %d 个用户(阈值 %d)", item.Ip, window, item.UserCount, rule.Threshold)})
			}
		}
	case operation_setting.RiskMetricIpMultiToken:
		var items []model.IpTokenRankItem
		items, err = model.GetIpMultiTokenRanking(window, riskScanLimit, exemptUserIds)
		result.Checked = len(items)
		for _, item := range items {
			if item.TokenCount > rule.Threshold {
				result.Hits = append(result.Hits, RiskRuleHit{Type: "ip", Value: item.Ip, Observed: item.TokenCount,
					Reason: fmt.Sprintf("IP %s 在 %d 小时内使用 %d 个令牌(阈值 %d),疑似批量测活", item.Ip, window, item.TokenCount, rule.Threshold)})
			}
		}
	case operation_setting.RiskMetricUserMultiIp:
		var items []model.UserRankItem
		items, err = model.GetUserMultiIpRanking(window, riskScanLimit, exemptUserIds)
		result.Checked = len(items)
		for _, item := range items {
			if item.IpCount > rule.Threshold {
				result.Hits = append(result.Hits, RiskRuleHit{Type: "user", Value: fmt.Sprint(item.UserId), Username: item.Username, Observed: item.IpCount,
					Reason: fmt.Sprintf("用户 %s(#%d)在 %d 小时内使用 %d 个 IP(阈值 %d)", item.Username, item.UserId, window, item.IpCount, rule.Threshold)})
			}
		}
	case operation_setting.RiskMetricUserTinyRequest:
		var items []model.UserTinyRequestRankItem
		items, err = model.GetUserTinyRequestRanking(window, riskScanLimit, tinyMaxTokens, exemptUserIds)
		result.Checked = len(items)
		for _, item := range items {
			if item.RequestCount > rule.Threshold {
				result.Hits = append(result.Hits, RiskRuleHit{Type: "user", Value: fmt.Sprint(item.UserId), Username: item.Username, Observed: item.RequestCount,
					Reason: fmt.Sprintf("用户 %s(#%d)在 %d 小时内发起 %d 次微量请求(输入输出均 ≤ %d tokens,阈值 %d),疑似自动测活", item.Username, item.UserId, window, item.RequestCount, tinyMaxTokens, rule.Threshold)})
			}
		}
	case operation_setting.RiskMetricUserErrorBurst:
		var items []model.UserErrorRankItem
		items, err = model.GetUserErrorRanking(window, riskScanLimit, exemptUserIds)
		result.Checked = len(items)
		for _, item := range items {
			if item.RequestCount > rule.Threshold {
				result.Hits = append(result.Hits, RiskRuleHit{Type: "user", Value: fmt.Sprint(item.UserId), Username: item.Username, Observed: item.RequestCount,
					Reason: fmt.Sprintf("用户 %s(#%d)在 %d 小时内产生 %d 次错误请求(阈值 %d)", item.Username, item.UserId, window, item.RequestCount, rule.Threshold)})
			}
		}
	default:
		return nil, errors.New("自动封禁规则指标无效")
	}
	result.LimitReached = result.Checked == riskScanLimit
	return result, err
}

func scanRiskRule(rule operation_setting.RiskAutoBanRule, tinyMaxTokens int, exemptUserIds []int) (int, error) {
	preview, err := PreviewRiskRule(rule, tinyMaxTokens, exemptUserIds)
	if err != nil {
		return 0, err
	}
	var failures []error
	for _, hit := range preview.Hits {
		evidence := &model.RiskRuleEvidence{RuleId: rule.Id, Name: rule.Name, Version: preview.Version,
			Metric: rule.Metric, WindowSeconds: rule.WindowHours * 3600, Threshold: rule.Threshold,
			Observed: hit.Observed, Comparison: ">", Action: rule.Action, BanMinutes: rule.BanMinutes}
		if rule.Metric == operation_setting.RiskMetricUserTinyRequest {
			evidence.TinyMaxTokens = tinyMaxTokens
		}
		if hit.Type == "ip" {
			err = applyIpRuleAction(rule, hit.Value, rule.WindowHours, hit.Reason, evidence)
		} else {
			var id int
			_, err = fmt.Sscan(hit.Value, &id)
			if err == nil && operation_setting.RiskActionDisablesUser(rule.Action) {
				err = DisableUserForRisk(id, hit.Reason, model.RiskBanSourceAutoRule, rule.BanMinutes, "", evidence)
			} else if err == nil {
				err = recordRuleAlert(rule.Metric, id, hit.Username, "", hit.Reason, evidence)
			}
		}
		if err != nil {
			failures = append(failures, err)
			common.SysError("risk control scan: " + err.Error())
		}
	}
	return len(preview.Hits), errors.Join(failures...)
}

func withRiskEvidence(event model.RiskEvent, evidence ...*model.RiskRuleEvidence) model.RiskEvent {
	if len(evidence) > 0 && evidence[0] != nil {
		event.Evidence = evidence[0]
		event.RuleId = evidence[0].RuleId
	}
	return event
}

func realtimeGuardEvidence(setting *operation_setting.RiskControlSetting, source string, observed int) *model.RiskRuleEvidence {
	evidence := &model.RiskRuleEvidence{RuleId: source, Name: source, Metric: source, Comparison: ">="}
	if source == model.RiskBanSourceProbeGuard {
		evidence.WindowSeconds = setting.ResolvedProbeGuardWindowSeconds()
		evidence.Threshold = setting.ResolvedProbeGuardModelCount()
		evidence.Action = setting.ResolvedProbeGuardAction()
		evidence.DryRun = setting.ProbeGuardDryRun
		evidence.BanMinutes = setting.ResolvedProbeGuardBanMinutes()
	} else {
		evidence.WindowSeconds = setting.ResolvedErrorGuardWindowSeconds()
		evidence.Threshold = setting.ResolvedErrorGuardThreshold()
		evidence.Action = setting.ResolvedErrorGuardAction()
		evidence.DryRun = setting.ErrorGuardDryRun
		evidence.BanMinutes = setting.ResolvedErrorGuardBanMinutes()
		evidence.StatusCodes = append([]int{}, setting.ResolvedErrorGuardStatusCodes()...)
	}
	evidence.Version = operation_setting.RiskRuleVersion(evidence)
	evidence.Observed = observed
	return evidence
}
