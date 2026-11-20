package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ErrProbePayloadBlocked 表示请求因命中单轮内容测活防护被拒绝。
var ErrProbePayloadBlocked = errors.New("request blocked by probe payload guard")

const (
	probePayloadPingPrompt   = "Call the probe_ping function with ok=true to acknowledge readiness. You must use the tool."
	maxProbePayloadTextBytes = 64 * 1024
)

type probePayloadTextCache struct {
	raw   string
	rules map[string]string
}

var cachedProbePayloadTextRules atomic.Pointer[probePayloadTextCache]

type ProbePayloadMatch struct {
	RuleId string `json:"rule_id"`
	Rule   string `json:"rule"`
}

// MatchProbePayloadText 对单段文本执行归一化与完整精确匹配，预览与实时中转共用同一逻辑。
func MatchProbePayloadText(text string, rules string) *ProbePayloadMatch {
	if len(text) > maxProbePayloadTextBytes {
		return nil
	}
	cache := cachedProbePayloadTextRules.Load()
	if cache == nil || cache.raw != rules {
		parsed, err := operation_setting.ParseProbePayloadTextRules(rules)
		if err != nil {
			return nil
		}
		cache = &probePayloadTextCache{raw: rules, rules: parsed}
		cachedProbePayloadTextRules.Store(cache)
	}
	normalized := operation_setting.NormalizeProbePayloadText(text)
	if rule, found := cache.rules[normalized]; found {
		return &ProbePayloadMatch{
			RuleId: fmt.Sprintf("text:%x", sha256.Sum256([]byte(normalized))),
			Rule:   rule,
		}
	}
	return nil
}

// MatchProbePayloadRequest 仅检测单轮纯文本用户请求（允许携带系统提示与工具定义）；
// 多轮对话、工具消息、会话续接以及图片/音频/文件请求一律跳过，避免误伤正常流量。
func MatchProbePayloadRequest(request dto.Request, setting *operation_setting.RiskControlSetting) *ProbePayloadMatch {
	if setting == nil || request == nil {
		return nil
	}
	text, ok := extractSingleTurnProbeText(request)
	if !ok {
		return nil
	}
	if match := MatchProbePayloadText(text, setting.ProbePayloadTextRules); match != nil {
		return match
	}
	if setting.ProbePayloadPingEnabled &&
		operation_setting.NormalizeProbePayloadText(text) == operation_setting.NormalizeProbePayloadText(probePayloadPingPrompt) &&
		matchesProbePingTool(request) {
		return &ProbePayloadMatch{
			RuleId: "tool:probe_ping",
			Rule:   probePayloadPingPrompt,
		}
	}
	return nil
}

// CheckProbePayloadGuard 在请求解析完成、计费与选路之前执行单轮内容测活检测。
// 正常请求仅做纯内存结构判定，不触发任何数据库访问；命中后按跨节点窗口累计，
// 达阈值后通过统一风控处置链路执行演练告警或封禁/禁用。
func CheckProbePayloadGuard(c *gin.Context, request dto.Request, startTime time.Time) error {
	setting := operation_setting.GetRiskControlSetting()
	if setting == nil || !setting.Enabled || !setting.ProbePayloadEnabled {
		return nil
	}
	if c == nil || request == nil {
		return nil
	}
	userId := c.GetInt("id")
	if userId <= 0 || isRiskWhitelisted(setting, userId) || c.GetInt("role") >= common.RoleAdminUser {
		return nil
	}

	match := MatchProbePayloadRequest(request, setting)
	if match == nil {
		return nil
	}

	action := setting.ResolvedProbePayloadAction()
	enforcing := !setting.ProbePayloadDryRun && action != operation_setting.RiskRuleActionAlert
	requestId := c.GetString(common.RequestIdKey)
	policy := model.RiskProbeBatchPolicy{
		WindowSeconds: setting.ResolvedProbePayloadWindowSeconds(),
		Threshold:     setting.ResolvedProbePayloadThreshold(),
		RecordOffense: enforcing,
	}
	res, err := model.RecordRiskProbePayloadMatch(userId, startTime, match.RuleId, match.Rule, requestId, policy)
	if err != nil {
		common.SysError("probe payload guard: failed to record match: " + err.Error())
		return nil
	}
	if res.User.Role >= common.RoleAdminUser {
		return nil
	}
	if res.Ignored {
		if enforcing {
			markRiskBlocked(c)
			return ErrProbePayloadBlocked
		}
		return nil
	}
	if !res.ThresholdReached {
		return nil
	}

	ip := c.ClientIP()
	ua := ""
	if c.Request != nil {
		ua = c.Request.UserAgent()
	}
	reason := fmt.Sprintf("用户 #%d 在 %d 秒内命中 %d 次单轮测活规则 [%s] (阈值 %d)",
		userId, policy.WindowSeconds, res.MatchedRequests, match.Rule, policy.Threshold)
	evidence := realtimeGuardEvidence(setting, model.RiskBanSourceProbePayload, res.MatchedRequests)
	evidence.MatchedRule = match.Rule

	if setting.ProbePayloadDryRun {
		_ = recordRuleAlert(model.RiskBanSourceProbePayload, userId, res.User.Username, ip, reason+"(演练模式,未处置)", evidence)
		return nil
	}
	if action == operation_setting.RiskRuleActionAlert {
		_ = recordRuleAlert(model.RiskBanSourceProbePayload, userId, res.User.Username, ip, reason+"(动作为仅告警,未处置)", evidence)
		return nil
	}

	// 封禁来源 IP 时仍按公网 IP 校验，避免私网地址写入封禁表。
	banIpTarget := ""
	if publicIp, ok := normalizePublicIp(ip); ok {
		banIpTarget = publicIp
	}
	if operation_setting.RiskActionBansIp(action) && banIpTarget != "" {
		if _, banErr := EscalateIpBan(banIpTarget, reason, model.RiskBanSourceProbePayload, setting.ResolvedProbePayloadBanMinutes(), evidence); banErr != nil {
			common.SysLog(fmt.Sprintf("%s: failed to ban ip %s: %s", model.RiskBanSourceProbePayload, banIpTarget, banErr.Error()))
		}
	}
	if operation_setting.RiskActionDisablesUser(action) {
		if disErr := escalateUserBan(setting, userId, reason, model.RiskBanSourceProbePayload, setting.ResolvedProbePayloadBanMinutes(), ip, ua, evidence); disErr != nil {
			common.SysLog(fmt.Sprintf("%s: failed to disable user %d: %s", model.RiskBanSourceProbePayload, userId, disErr.Error()))
		}
	}
	common.SysLog(fmt.Sprintf("probe payload guard: blocked request, %s, action=%s", reason, action))
	markRiskBlocked(c)
	return ErrProbePayloadBlocked
}

func extractSingleTurnProbeText(request dto.Request) (string, bool) {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if r == nil || r.Prompt != nil || r.Input != nil || r.Prefix != nil || r.Suffix != nil || hasRawJSONValue(r.Audio) {
			return "", false
		}
		var content any
		users := 0
		for _, message := range r.Messages {
			if hasRawJSONValue(message.ToolCalls) || message.ToolCallId != "" || hasRawJSONValue(message.Tools) {
				return "", false
			}
			switch message.Role {
			case "system", "developer":
			case "user":
				users++
				content = message.Content
			default:
				return "", false
			}
			if users > 1 {
				return "", false
			}
		}
		if users == 1 {
			return extractPureTextContent(content, "text")
		}
	case *dto.ClaudeRequest:
		if r == nil || r.Prompt != "" || hasRawJSONValue(r.Container) || len(r.Messages) != 1 || r.Messages[0].Role != "user" {
			return "", false
		}
		return extractPureTextContent(r.Messages[0].Content, "text")
	case *dto.OpenAIResponsesRequest:
		if r == nil || r.PreviousResponseID != "" || hasRawJSONValue(r.Conversation) || hasRawJSONValue(r.Prompt) {
			return "", false
		}
		input := gjson.ParseBytes(r.Input)
		if input.Type == gjson.String {
			return boundedProbePayloadText(input.Str)
		}
		if !input.IsArray() {
			return "", false
		}
		users, valid := 0, true
		var content gjson.Result
		input.ForEach(func(_, message gjson.Result) bool {
			kind := message.Get("type").String()
			if !message.IsObject() || (kind != "" && kind != "message") || message.Get("tool_calls").Exists() || message.Get("function_call").Exists() {
				valid = false
				return false
			}
			switch message.Get("role").String() {
			case "system", "developer":
			case "user":
				users++
				content = message.Get("content")
			default:
				valid = false
			}
			return valid && users <= 1
		})
		if !valid || users != 1 {
			return "", false
		}
		if content.Type == gjson.String {
			return boundedProbePayloadText(content.Str)
		}
		if !content.IsArray() {
			return "", false
		}
		var parts []string
		size := 0
		content.ForEach(func(_, block gjson.Result) bool {
			text := block.Get("text")
			if block.Get("type").String() != "input_text" || text.Type != gjson.String {
				valid = false
				return false
			}
			size += len(text.Str) + 1
			if size > maxProbePayloadTextBytes {
				valid = false
				return false
			}
			parts = append(parts, text.Str)
			return true
		})
		if valid {
			return boundedProbePayloadText(strings.Join(parts, "\n"))
		}
	}
	return "", false
}

func boundedProbePayloadText(text string) (string, bool) {
	return text, text != "" && len(text) <= maxProbePayloadTextBytes
}

func hasRawJSONValue(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null" && trimmed != "[]"
}

func extractPureTextContent(content any, textType string) (string, bool) {
	if text, ok := content.(string); ok {
		return boundedProbePayloadText(text)
	}
	blocks, ok := content.([]any)
	if !ok || len(blocks) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(blocks))
	size := 0
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok || block["type"] != textType {
			return "", false
		}
		text, ok := block["text"].(string)
		if !ok {
			return "", false
		}
		size += len(text) + 1
		if size > maxProbePayloadTextBytes {
			return "", false
		}
		parts = append(parts, text)
	}
	return boundedProbePayloadText(strings.Join(parts, "\n"))
}

func matchesProbePingTool(request dto.Request) bool {
	var name string
	var schema, choice any
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if len(r.Tools) != 1 || r.Tools[0].Type != "function" || hasRawJSONValue(r.Functions) {
			return false
		}
		name, schema, choice = r.Tools[0].Function.Name, r.Tools[0].Function.Parameters, r.ToolChoice
	case *dto.OpenAIResponsesRequest:
		var tools []struct {
			Type       string `json:"type"`
			Name       string `json:"name"`
			Parameters any    `json:"parameters"`
		}
		if common.Unmarshal(r.Tools, &tools) != nil || len(tools) != 1 || tools[0].Type != "function" || common.Unmarshal(r.ToolChoice, &choice) != nil {
			return false
		}
		name, schema = tools[0].Name, tools[0].Parameters
	case *dto.ClaudeRequest:
		tools, ok := r.Tools.([]any)
		if !ok || len(tools) != 1 || hasRawJSONValue(r.McpServers) {
			return false
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			return false
		}
		name, _ = tool["name"].(string)
		schema, choice = tool["input_schema"], r.ToolChoice
		if kind, exists := tool["type"]; exists && kind != "custom" {
			return false
		}
	default:
		return false
	}
	return name == "probe_ping" && isProbePingSchema(schema) && requiresProbePing(choice)
}

func isProbePingSchema(value any) bool {
	schema, ok := value.(map[string]any)
	if !ok || schema["type"] != "object" {
		return false
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		return false
	}
	property, ok := properties["ok"].(map[string]any)
	if !ok || property["type"] != "boolean" {
		return false
	}
	required, ok := schema["required"].([]any)
	return ok && len(required) == 1 && required[0] == "ok"
}

func requiresProbePing(choice any) bool {
	if choice == "required" {
		return true
	}
	object, ok := choice.(map[string]any)
	if !ok {
		return false
	}
	switch object["type"] {
	case "any":
		return true
	case "tool":
		return object["name"] == "probe_ping"
	case "function":
		if object["name"] == "probe_ping" {
			return true
		}
		function, ok := object["function"].(map[string]any)
		return ok && function["name"] == "probe_ping"
	}
	return false
}
