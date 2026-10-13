package operation_setting

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// UaPatternRegexPrefix 标记一条 User-Agent 条目按正则匹配，否则按子串匹配。
// 风控 UA 黑名单与客户端 UA 准入规则共用这套写法，两者都不区分大小写。
const UaPatternRegexPrefix = "re:"

// UaPattern 是一条已编译的 User-Agent 匹配条目。
type UaPattern struct {
	substring string // 已转小写
	re        *regexp.Regexp
}

// CompileUaPattern 编译一条 User-Agent 条目：以 re: 开头时按不区分大小写的正则匹配，
// 否则按不区分大小写的子串匹配。返回的错误不带条目位置，由调用方补充。
func CompileUaPattern(entry string) (UaPattern, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return UaPattern{}, errors.New("不能为空")
	}
	pattern, isRegex := strings.CutPrefix(entry, UaPatternRegexPrefix)
	if !isRegex {
		return UaPattern{substring: strings.ToLower(entry)}, nil
	}
	if strings.TrimSpace(pattern) == "" {
		return UaPattern{}, errors.New("正则不能为空")
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return UaPattern{}, fmt.Errorf("不是合法正则: %s", err.Error())
	}
	return UaPattern{re: re}, nil
}

// Match 的 lowerUA 参数必须已转小写。
func (p UaPattern) Match(lowerUA string) bool {
	if p.re != nil {
		return p.re.MatchString(lowerUA)
	}
	return p.substring != "" && strings.Contains(lowerUA, p.substring)
}
