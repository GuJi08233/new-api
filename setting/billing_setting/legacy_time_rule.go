package billing_setting

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 旧版 classic 时间规则生成器对同日区间错用了 ||，生成的条件恒为真，倍率因此全天生效。
// 生成器已修正，但存量表达式仍按原文计费；由于无法区分生成器产物和管理员刻意写的恒真
// 兜底规则，这里只做启动时只读审计并列出可疑项，不改写任何配置。
var timeRangeOrCondition = regexp.MustCompile(
	`(hour|minute|weekday|month|day)\("([^"]*)"\)\s*>=\s*(-?[0-9]+(?:\.[0-9]+)?)\s*\|\|\s*(hour|minute|weekday|month|day)\("([^"]*)"\)\s*<\s*(-?[0-9]+(?:\.[0-9]+)?)`)

// LegacyTimeRule 是一条恒真的时间区间条件及其所属模型。
type LegacyTimeRule struct {
	Model    string
	Func     string
	Timezone string
	Start    string
	End      string
}

// FindLegacyAlwaysTrueTimeRules 找出计费表达式中恒真的时间区间条件。
// 跨夜区间（start >= end，例如 21-6 点）用 || 是正确的，只有同日区间才应为 &&。
func FindLegacyAlwaysTrueTimeRules(exprs map[string]string) []LegacyTimeRule {
	var found []LegacyTimeRule
	for model, expr := range exprs {
		for _, m := range timeRangeOrCondition.FindAllStringSubmatch(expr, -1) {
			// RE2 不支持反向引用，同一条件两侧的函数名与时区必须在此比对。
			if m[1] != m[4] || m[2] != m[5] {
				continue
			}
			start, startErr := strconv.ParseFloat(m[3], 64)
			end, endErr := strconv.ParseFloat(m[6], 64)
			if startErr != nil || endErr != nil || start >= end {
				continue
			}
			found = append(found, LegacyTimeRule{
				Model:    model,
				Func:     m[1],
				Timezone: m[2],
				Start:    m[3],
				End:      m[6],
			})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Model < found[j].Model })
	return found
}

// WarnLegacyAlwaysTrueTimeRules 在启动时审计存量计费表达式并打印待人工确认的清单。
func WarnLegacyAlwaysTrueTimeRules() {
	rules := FindLegacyAlwaysTrueTimeRules(GetBillingExprCopy())
	if len(rules) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "检测到 %d 条恒真的时间区间计费规则，倍率实际全天生效，请在模型定价编辑器中逐条确认：", len(rules))
	for _, r := range rules {
		fmt.Fprintf(&b, "\n  模型 %s：%s(%q) >= %s || %s(%q) < %s；若本意是 %s-%s 时段，应改用 &&",
			r.Model, r.Func, r.Timezone, r.Start, r.Func, r.Timezone, r.End, r.Start, r.End)
	}
	common.SysError(b.String())
}
