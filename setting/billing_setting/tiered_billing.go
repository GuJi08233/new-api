package billing_setting

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	BillingModeField      = "billing_mode"
	BillingExprField      = "billing_expr"
	BillingExprOptionKey  = "billing_setting.billing_expr"
	// PluginBillingExprOption stores per-plugin task usage expressions keyed by
	// PluginBillingExprKey(plugin, model).
	PluginBillingExprOption = "billing_setting.plugin_billing_expr"
	maxTaskExprSmokeTests   = 64
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr,
// billing_setting.plugin_billing_expr
type BillingSetting struct {
	BillingMode       map[string]string `json:"billing_mode"`
	BillingExpr       map[string]string `json:"billing_expr"`
	PluginBillingExpr map[string]string `json:"plugin_billing_expr"`
}

var billingSetting = BillingSetting{
	BillingMode:       make(map[string]string),
	BillingExpr:       make(map[string]string),
	PluginBillingExpr: make(map[string]string),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

func PluginBillingExprKey(pluginKey, model string) string {
	return pluginKey + "::" + model
}

func SplitPluginBillingExprKey(key string) (plugin, model string, ok bool) {
	plugin, model, ok = strings.Cut(key, "::")
	if !ok || !jsplugin.ValidPluginKey(plugin) || strings.TrimSpace(model) == "" {
		return "", "", false
	}
	return plugin, model, true
}

func GetPluginBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.PluginBillingExpr)
}

func GetPluginBillingExpr(pluginKey, model string) (string, bool) {
	expression, ok := billingSetting.PluginBillingExpr[PluginBillingExprKey(pluginKey, model)]
	return expression, ok
}

// ResolveTaskBillingExpr selects the executing plugin's override before the
// model expression, retaining the model alias fallback and explicit modes.
func ResolveTaskBillingExpr(pluginKey, model, mappedModel string) (string, bool) {
	if pluginKey != "" {
		if expr, ok := GetPluginBillingExpr(pluginKey, model); ok {
			return expr, true
		}
		if mappedModel != "" && mappedModel != model {
			if expr, ok := GetPluginBillingExpr(pluginKey, mappedModel); ok {
				return expr, true
			}
		}
	}
	if GetBillingMode(model) == BillingModeTieredExpr {
		return GetBillingExpr(model)
	}
	if mappedModel != "" && mappedModel != model && GetBillingMode(mappedModel) == BillingModeTieredExpr {
		expression, ok := GetBillingExpr(mappedModel)
		return expression, ok && strings.TrimSpace(expression) != ""
	}
	return "", false
}

// TaskExprCompatible checks the schema contract even for usage references in
// branches that the current request would not evaluate.
func TaskExprCompatible(expression string, schema map[string]jsplugin.UsageFieldSchema) bool {
	if strings.TrimSpace(expression) == "" {
		return false
	}
	if _, err := billingexpr.CompileFromCache(expression); err != nil {
		return false
	}
	for key := range billingexpr.UsedUsageKeys(expression) {
		if _, exists := schema[key]; !exists {
			return false
		}
	}
	return true
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func ValidateExprMapJSONString(jsonStr string) error {
	expressions := make(map[string]string)
	if err := common.UnmarshalJsonStr(jsonStr, &expressions); err != nil {
		return fmt.Errorf("invalid tiered billing expression map: %w", err)
	}
	for modelName, exprStr := range expressions {
		if strings.TrimSpace(exprStr) == "" {
			return fmt.Errorf("model %s has an empty tiered billing expression", modelName)
		}
		if err := smokeTestExpr(exprStr); err != nil {
			return fmt.Errorf("model %s has an invalid tiered billing expression: %w", modelName, err)
		}
	}
	return nil
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if err := billingexpr.ValidateCost(result); err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: %w", v.P, v.C, err)
			}
		}
	}
	return nil
}

// ValidatePluginExprMapJSONString checks per-plugin task usage expressions.
// Usage facts are only known once a plugin reports them, so expressions are
// compiled here and checked for a valid cost when they run.
func ValidatePluginExprMapJSONString(jsonStr string) error {
	expressions := make(map[string]string)
	if err := common.UnmarshalJsonStr(jsonStr, &expressions); err != nil {
		return fmt.Errorf("invalid plugin billing expression map: %w", err)
	}
	for key, exprStr := range expressions {
		if _, _, ok := SplitPluginBillingExprKey(key); !ok {
			return fmt.Errorf("invalid plugin billing expression key %q", key)
		}
		if strings.TrimSpace(exprStr) == "" {
			return fmt.Errorf("%s has an empty billing expression", key)
		}
		if _, err := billingexpr.CompileFromCache(exprStr); err != nil {
			return fmt.Errorf("%s has an invalid billing expression: %w", key, err)
		}
	}
	return nil
}

// SmokeTestTaskExpr checks a task usage expression against a plugin's usage
// schema: every usage key must be declared and every boundary vector must
// produce a finite non-negative cost.
func SmokeTestTaskExpr(exprStr string, schema map[string]jsplugin.UsageFieldSchema) error {
	if _, err := billingexpr.CompileFromCache(exprStr); err != nil {
		return err
	}
	for key := range billingexpr.UsedUsageKeys(exprStr) {
		if _, declared := schema[key]; !declared {
			return fmt.Errorf("usage key %q is not declared by the task plugin", key)
		}
	}

	for _, usage := range taskUsageSmokeVectors(schema) {
		for _, request := range billingExprSmokeRequests() {
			request.Usage = usage
			result, _, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{}, request)
			if err != nil {
				return fmt.Errorf("usage vector %v: run failed: %w", usage, err)
			}
			if math.IsNaN(result) || math.IsInf(result, 0) || result < 0 {
				return fmt.Errorf("usage vector %v: result must be finite and non-negative, got %f", usage, result)
			}
		}
	}
	return nil
}

type usageSmokeDimension struct {
	name   string
	values []any
}

func taskUsageSmokeVectors(schema map[string]jsplugin.UsageFieldSchema) []map[string]any {
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)

	dimensions := make([]usageSmokeDimension, 0, len(names))
	for _, name := range names {
		field := schema[name]
		if len(field.Enum) > 0 {
			values := make([]any, len(field.Enum))
			for index, value := range field.Enum {
				values[index] = value
			}
			dimensions = append(dimensions, usageSmokeDimension{name: name, values: values})
			continue
		}
		if field.Type == "boolean" {
			dimensions = append(dimensions, usageSmokeDimension{name: name, values: []any{false, true}})
			continue
		}
		limit := relaycommon.MaxTaskDurationSeconds
		if field.Unit == "count" {
			limit = dto.MaxImageN
		}
		if field.Unit == "token" || field.Unit == "credit" {
			limit = common.MaxQuota
		}
		dimensions = append(dimensions, usageSmokeDimension{
			name:   name,
			values: []any{float64(0), float64(1), float64(limit)},
		})
	}

	if usageSmokeCombinationCount(dimensions, maxTaskExprSmokeTests) > maxTaskExprSmokeTests {
		for index := range dimensions {
			field := schema[dimensions[index].name]
			if len(field.Enum) <= 2 {
				continue
			}
			dimensions[index].values = []any{field.Enum[0], field.Enum[len(field.Enum)-1]}
		}
	}

	vectors := make([]map[string]any, 0, maxTaskExprSmokeTests)
	var appendVectors func(int, map[string]any)
	appendVectors = func(index int, current map[string]any) {
		if len(vectors) >= maxTaskExprSmokeTests {
			return
		}
		if index == len(dimensions) {
			vector := make(map[string]any, len(current))
			maps.Copy(vector, current)
			vectors = append(vectors, vector)
			return
		}
		for _, value := range dimensions[index].values {
			current[dimensions[index].name] = value
			appendVectors(index+1, current)
		}
		delete(current, dimensions[index].name)
	}
	appendVectors(0, make(map[string]any, len(dimensions)))

	combinationCount := usageSmokeCombinationCount(dimensions, maxTaskExprSmokeTests)
	if combinationCount > maxTaskExprSmokeTests && len(vectors) > 0 {
		last := make(map[string]any, len(dimensions))
		for _, dimension := range dimensions {
			last[dimension.name] = dimension.values[len(dimension.values)-1]
		}
		vectors[len(vectors)-1] = last
	}
	return vectors
}

func usageSmokeCombinationCount(dimensions []usageSmokeDimension, stopAfter int) int {
	count := 1
	for _, dimension := range dimensions {
		if len(dimension.values) == 0 {
			return 0
		}
		if count > stopAfter/len(dimension.values) {
			return stopAfter + 1
		}
		count *= len(dimension.values)
	}
	return count
}

func billingExprSmokeRequests() []billingexpr.RequestInput {
	return []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}
}
