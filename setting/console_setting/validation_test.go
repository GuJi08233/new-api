package console_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 后端长度限制与前端输入框一致，按 UTF-16 码元计数而不是字节数：
// 中文占 1 个，代理对表情占 2 个。
func TestValidateConsoleSettingsCountsCharactersLikeTheFrontend(t *testing.T) {
	faq := func(question string) string {
		return `[{"question":"` + question + `","answer":"答案"}]`
	}
	announcement := func(content string) string {
		return `[{"content":"` + content + `","publishDate":"2026-09-16T00:00:00Z"}]`
	}
	cases := []struct {
		name    string
		kind    string
		payload string
		valid   bool
	}{
		{"200 个中文问题按字符数放行", "FAQ", faq(strings.Repeat("问", 200)), true},
		{"201 个中文问题超限", "FAQ", faq(strings.Repeat("问", 201)), false},
		{"100 个表情占 200 个码元放行", "FAQ", faq(strings.Repeat("😀", 100)), true},
		{"101 个表情超限", "FAQ", faq(strings.Repeat("😀", 101)), false},
		{"500 个中文公告放行", "Announcements", announcement(strings.Repeat("告", 500)), true},
		{"501 个中文公告超限", "Announcements", announcement(strings.Repeat("告", 501)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConsoleSettings(tc.payload, tc.kind)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			assert.Error(t, err)
		})
	}
}
