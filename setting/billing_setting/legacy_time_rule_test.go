package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindLegacyAlwaysTrueTimeRules(t *testing.T) {
	cases := []struct {
		name  string
		exprs map[string]string
		want  []LegacyTimeRule
	}{
		{
			name:  "同日区间用 || 恒真",
			exprs: map[string]string{"gpt-4o": `p * 2 * (hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 17 ? 2 : 1)`},
			want: []LegacyTimeRule{
				{Model: "gpt-4o", Func: "hour", Timezone: "Asia/Shanghai", Start: "9", End: "17"},
			},
		},
		{
			name:  "跨夜区间用 || 正确",
			exprs: map[string]string{"gpt-4o": `p * (hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)`},
			want:  nil,
		},
		{
			name:  "同日区间用 && 正确",
			exprs: map[string]string{"gpt-4o": `p * (hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 17 ? 2 : 1)`},
			want:  nil,
		},
		{
			name:  "边界相等不是恒真区间",
			exprs: map[string]string{"gpt-4o": `p * (hour("UTC") >= 9 || hour("UTC") < 9 ? 2 : 1)`},
			want:  nil,
		},
		{
			name:  "函数名不同不构成同一条件",
			exprs: map[string]string{"gpt-4o": `p * (hour("UTC") >= 9 || weekday("UTC") < 17 ? 2 : 1)`},
			want:  nil,
		},
		{
			name:  "时区不同不构成同一条件",
			exprs: map[string]string{"gpt-4o": `p * (hour("UTC") >= 9 || hour("Asia/Shanghai") < 17 ? 2 : 1)`},
			want:  nil,
		},
		{
			name: "同一表达式内多条规则各自判定",
			exprs: map[string]string{
				"claude": `p * (hour("UTC") >= 1 || hour("UTC") < 5 ? 2 : 1) * (weekday("UTC") >= 6 || weekday("UTC") < 2 ? 3 : 1)`,
			},
			want: []LegacyTimeRule{
				{Model: "claude", Func: "hour", Timezone: "UTC", Start: "1", End: "5"},
			},
		},
		{
			name:  "无时间规则的普通表达式",
			exprs: map[string]string{"gpt-4o": `p * 2 + c * 8`},
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FindLegacyAlwaysTrueTimeRules(tc.exprs))
		})
	}
}

// 分组专属表达式承载同一批旧规则，审计必须覆盖它们并标出所属分组，
// 否则管理员会以为存量规则已经全部确认过。
func TestFindLegacyAlwaysTrueTimeRulesTagsGroup(t *testing.T) {
	rules := findLegacyAlwaysTrueTimeRules("vip", map[string]string{
		"gpt-4o": `p * (hour("UTC") >= 9 || hour("UTC") < 17 ? 2 : 1)`,
	})
	require.Len(t, rules, 1)
	assert.Equal(t, LegacyTimeRule{
		Group: "vip", Model: "gpt-4o", Func: "hour", Timezone: "UTC", Start: "9", End: "17",
	}, rules[0])
}

func TestFindLegacyAlwaysTrueTimeRulesSortsByModel(t *testing.T) {
	rules := FindLegacyAlwaysTrueTimeRules(map[string]string{
		"z-model": `p * (hour("UTC") >= 9 || hour("UTC") < 17 ? 2 : 1)`,
		"a-model": `p * (hour("UTC") >= 1 || hour("UTC") < 3 ? 2 : 1)`,
	})
	require.Len(t, rules, 2)
	assert.Equal(t, "a-model", rules[0].Model)
	assert.Equal(t, "z-model", rules[1].Model)
}
