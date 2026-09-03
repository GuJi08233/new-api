package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertRiskLog(t *testing.T, userId int, username string, ip string, ua string, logType int, ageHours int) {
	t.Helper()
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: time.Now().Add(-time.Duration(ageHours) * time.Hour).Unix(),
		Type:      logType,
		Ip:        ip,
		Ua:        ua,
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		t.Fatalf("insert risk log: %v", err)
	}
}

func TestGetIpMultiUserRanking(t *testing.T) {
	truncateTables(t)

	// ip-a 关联 3 个用户,ip-b 关联 1 个用户(也应出现,按用户数排在后面),
	// ip-c 关联 2 个用户但在窗口外
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 3, "u3", "ip-a", "", LogTypeError, 2)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1) // 重复请求不增加 user_count
	insertRiskLog(t, 4, "u4", "ip-b", "", LogTypeConsume, 1)
	insertRiskLog(t, 5, "u5", "ip-c", "", LogTypeConsume, 30)
	insertRiskLog(t, 6, "u6", "ip-c", "", LogTypeConsume, 30)
	// 管理/充值日志不参与统计
	insertRiskLog(t, 7, "u7", "ip-a", "", LogTypeManage, 1)

	items, err := GetIpMultiUserRanking(24, 50)
	if err != nil {
		t.Fatalf("GetIpMultiUserRanking: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (all in-window ips, ranked by user count)", len(items))
	}
	if items[0].Ip != "ip-a" || items[0].UserCount != 3 || items[0].RequestCount != 4 {
		t.Fatalf("got %+v, want ip-a user_count=3 request_count=4", items[0])
	}
	if items[1].Ip != "ip-b" || items[1].UserCount != 1 || items[1].RequestCount != 1 {
		t.Fatalf("got %+v, want ip-b user_count=1 request_count=1", items[1])
	}
}

func TestGetUserMultiIpRanking(t *testing.T) {
	truncateTables(t)

	// 用户 1 使用 3 个 IP,用户 2 使用 1 个 IP(不应出现)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-b", "", LogTypeConsume, 2)
	insertRiskLog(t, 1, "u1", "ip-c", "", LogTypeError, 3)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeConsume, 1)
	// 空 IP 不参与统计
	insertRiskLog(t, 1, "u1", "", "", LogTypeConsume, 1)

	items, err := GetUserMultiIpRanking(24, 50)
	if err != nil {
		t.Fatalf("GetUserMultiIpRanking: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1 (only user 1)", len(items))
	}
	if items[0].UserId != 1 || items[0].Username != "u1" || items[0].IpCount != 3 || items[0].RequestCount != 3 {
		t.Fatalf("got %+v, want user 1 ip_count=3 request_count=3", items[0])
	}
}

func TestGetUaOverviewRanking(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 1, "u1", "ip-a", "curl/8.0", LogTypeConsume, 1)
	insertRiskLog(t, 2, "u2", "ip-b", "curl/8.0", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "python-requests/2.31", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1) // 空 UA 不参与

	items, err := GetUaOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "curl/8.0", items[0].Ua)
	assert.Equal(t, 2, items[0].UserCount, "同一 UA 被两个用户使用")
	assert.Equal(t, 2, items[0].IpCount)
	assert.Equal(t, 2, items[0].RequestCount)

	// 白名单用户(1)的日志被排除后,python-requests 完全消失,curl 只剩用户 2 的一次
	filtered, err := GetUaOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16, ExcludeUserIds: []int{1}})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, "curl/8.0", filtered[0].Ua)
	assert.Equal(t, 1, filtered[0].UserCount)
	assert.Equal(t, 1, filtered[0].IpCount)
	assert.Equal(t, 1, filtered[0].RequestCount)
}

func TestRiskDetailQueries(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 2)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-b", "", LogTypeConsume, 1)

	users, err := GetRiskDetailUsers(RiskDetailTarget{Type: RiskDetailTypeIp, Value: "ip-a", Hours: 24})
	require.NoError(t, err)
	require.Len(t, users, 2, "ip-a 关联两个用户")
	assert.Equal(t, 1, users[0].UserId)
	assert.Equal(t, 2, users[0].RequestCount)
	assert.Positive(t, users[0].FirstSeen)
	assert.GreaterOrEqual(t, users[0].LastSeen, users[0].FirstSeen)

	ips, err := GetRiskDetailIps(RiskDetailTarget{Type: RiskDetailTypeUser, Value: "1", Hours: 24})
	require.NoError(t, err)
	require.Len(t, ips, 2, "用户 1 使用两个 IP")
	assert.Equal(t, "ip-a", ips[0].Ip)
	assert.Equal(t, 2, ips[0].RequestCount)

	// 详情与排行榜口径一致:排除白名单用户后关联列表随之收敛
	filtered, err := GetRiskDetailUsers(RiskDetailTarget{Type: RiskDetailTypeIp, Value: "ip-a", Hours: 24, ExcludeUserIds: []int{1}})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, 2, filtered[0].UserId)

	userIds, err := GetIpAssociatedUserIds("ip-a", 24)
	if err != nil {
		t.Fatalf("GetIpAssociatedUserIds: %v", err)
	}
	if len(userIds) != 2 {
		t.Fatalf("associated user ids = %v, want 2 ids", userIds)
	}
}

func TestRiskWindowAndLimitNormalization(t *testing.T) {
	truncateTables(t)

	// 窗口上限 7 天:8 天前的日志即使 hours 传 10000 也不应统计
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 24*8)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeConsume, 24*8)

	items, err := GetIpMultiUserRanking(10000, 50)
	if err != nil {
		t.Fatalf("GetIpMultiUserRanking: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0 (out of max window)", len(items))
	}
}

// insertProbeLog 插入带令牌与 token 计数的日志,供测活检测排行测试使用。
func insertProbeLog(t *testing.T, userId int, username string, ip string, tokenId int, logType int, promptTokens int, completionTokens int, ageHours int) {
	t.Helper()
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        time.Now().Add(-time.Duration(ageHours) * time.Hour).Unix(),
		Type:             logType,
		Ip:               ip,
		TokenId:          tokenId,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		t.Fatalf("insert probe log: %v", err)
	}
}

func TestGetIpMultiTokenRanking(t *testing.T) {
	truncateTables(t)

	// ip-a 使用 3 个令牌(其中一次是错误日志,也应计入);
	// ip-b 使用 1 个令牌;token_id=0 与空 IP 的日志不计入。
	insertProbeLog(t, 1, "u1", "ip-a", 101, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 1, "u1", "ip-a", 102, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 2, "u2", "ip-a", 103, LogTypeError, 0, 0, 2)
	insertProbeLog(t, 3, "u3", "ip-b", 201, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 3, "u3", "ip-b", 0, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 3, "u3", "", 202, LogTypeConsume, 100, 100, 1)

	items, err := GetIpMultiTokenRanking(24, 50)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "ip-a", items[0].Ip)
	assert.Equal(t, 3, items[0].TokenCount)
	assert.Equal(t, 2, items[0].UserCount)
	assert.Equal(t, 3, items[0].RequestCount)
	assert.Equal(t, "ip-b", items[1].Ip)
	assert.Equal(t, 1, items[1].TokenCount)
}

func TestGetUserTinyRequestRanking(t *testing.T) {
	truncateTables(t)

	// 用户 1:3 次微量请求 + 1 次正常请求;用户 2:1 次微量请求;
	// 错误日志与超阈值请求不计入。
	insertProbeLog(t, 1, "u1", "ip-a", 101, LogTypeConsume, 5, 1, 1)
	insertProbeLog(t, 1, "u1", "ip-a", 101, LogTypeConsume, 8, 0, 1)
	insertProbeLog(t, 1, "u1", "ip-a", 102, LogTypeConsume, 16, 16, 2)
	insertProbeLog(t, 1, "u1", "ip-a", 101, LogTypeConsume, 500, 300, 1)
	insertProbeLog(t, 2, "u2", "ip-b", 201, LogTypeConsume, 1, 1, 1)
	insertProbeLog(t, 2, "u2", "ip-b", 201, LogTypeError, 1, 1, 1)
	insertProbeLog(t, 3, "u3", "ip-c", 301, LogTypeConsume, 17, 1, 1)

	items, err := GetUserTinyRequestRanking(24, 50, 16)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[0].UserId)
	assert.Equal(t, "u1", items[0].Username)
	assert.Equal(t, 3, items[0].RequestCount)
	assert.Equal(t, 2, items[0].TokenCount)
	assert.Equal(t, 2, items[1].UserId)
	assert.Equal(t, 1, items[1].RequestCount)
}

func TestGetUserErrorRanking(t *testing.T) {
	truncateTables(t)

	// 用户 1:2 次错误;用户 2:1 次错误;消费日志与窗口外错误不计入。
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeError, 1)
	insertRiskLog(t, 1, "u1", "ip-b", "", LogTypeError, 2)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeError, 1)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeError, 48)

	items, err := GetUserErrorRanking(24, 50)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[0].UserId)
	assert.Equal(t, 2, items[0].RequestCount)
	assert.Equal(t, 2, items[1].UserId)
	assert.Equal(t, 1, items[1].RequestCount)
}

func TestGetTokenMultiIpRanking(t *testing.T) {
	truncateTables(t)

	// 令牌 101 被 3 个 IP 使用(含错误日志);令牌 201 只有 1 个 IP(不出现);
	// token_id=0 与空 IP 不计入。
	insertProbeLog(t, 1, "u1", "ip-a", 101, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 1, "u1", "ip-b", 101, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 1, "u1", "ip-c", 101, LogTypeError, 0, 0, 2)
	insertProbeLog(t, 2, "u2", "ip-a", 201, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 2, "u2", "", 201, LogTypeConsume, 100, 100, 1)
	insertProbeLog(t, 3, "u3", "ip-a", 0, LogTypeConsume, 100, 100, 1)

	items, err := GetTokenMultiIpRanking(24, 50)
	require.NoError(t, err)
	require.Len(t, items, 1, "只有使用 >1 IP 的令牌上榜")
	assert.Equal(t, 101, items[0].TokenId)
	assert.Equal(t, 1, items[0].UserId)
	assert.Equal(t, "u1", items[0].Username)
	assert.Equal(t, 3, items[0].IpCount)
	assert.Equal(t, 3, items[0].RequestCount)
}

// insertOverviewLog 插入一条指定字段的日志,ageHours 为距今小时数。
func insertOverviewLog(t *testing.T, ageHours int, log Log) {
	t.Helper()
	log.CreatedAt = time.Now().Add(-time.Duration(ageHours) * time.Hour).Unix()
	require.NoError(t, LOG_DB.Create(&log).Error)
}

// seedOverviewLogs 构造合并排行测试用的日志:
// 用户 1 有 5 条(3 次微量消费、1 次普通消费、1 次错误),覆盖空 IP / 空 UA / token_id=0;
// 用户 2 有 2 条错误;用户 3 的日志在窗口外;管理日志不参与统计。
func seedOverviewLogs(t *testing.T) {
	t.Helper()
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-a", Ua: "ua-x", TokenId: 101, PromptTokens: 5, CompletionTokens: 1})
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-b", Ua: "ua-x", TokenId: 101, PromptTokens: 200, CompletionTokens: 100})
	insertOverviewLog(t, 2, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-a", Ua: "ua-y", TokenId: 102, PromptTokens: 16, CompletionTokens: 16})
	insertOverviewLog(t, 3, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-c"})
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, PromptTokens: 1, CompletionTokens: 1})
	insertOverviewLog(t, 1, Log{UserId: 2, Username: "u2", Type: LogTypeError, Ip: "ip-a", Ua: "ua-z", TokenId: 201})
	insertOverviewLog(t, 1, Log{UserId: 2, Username: "u2", Type: LogTypeError, Ip: "ip-a", Ua: "ua-z", TokenId: 201})
	insertOverviewLog(t, 30, Log{UserId: 3, Username: "u3", Type: LogTypeConsume, Ip: "ip-d", Ua: "ua-w", TokenId: 301})
	insertOverviewLog(t, 1, Log{UserId: 4, Username: "u4", Type: LogTypeManage, Ip: "ip-a", Ua: "ua-x"})
}

func TestGetUserOverviewRanking(t *testing.T) {
	truncateTables(t)
	seedOverviewLogs(t)

	items, err := GetUserOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16})
	require.NoError(t, err)
	require.Len(t, items, 2, "窗口外用户与管理日志不上榜")

	top := items[0]
	assert.Equal(t, 1, top.UserId)
	assert.Equal(t, "u1", top.Username)
	assert.Equal(t, 5, top.RequestCount)
	assert.Equal(t, 3, top.IpCount, "空 IP 不计入去重计数")
	assert.Equal(t, 2, top.TokenCount, "token_id=0 不计入去重计数")
	assert.Equal(t, 3, top.TinyRequestCount, "阈值取等号仍算微量,错误日志不算")
	assert.Equal(t, 1, top.ErrorCount)
	assert.Greater(t, top.LastSeen, top.FirstSeen)

	second := items[1]
	assert.Equal(t, 2, second.UserId)
	assert.Equal(t, 2, second.RequestCount)
	assert.Equal(t, 2, second.ErrorCount)
	assert.Equal(t, 0, second.TinyRequestCount, "错误日志的零 token 不算微量请求")
}

func TestGetIpOverviewRanking(t *testing.T) {
	truncateTables(t)
	seedOverviewLogs(t)

	items, err := GetIpOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16})
	require.NoError(t, err)
	require.Len(t, items, 3, "空 IP 的日志不上榜")

	byIp := map[string]IpOverviewItem{}
	for _, item := range items {
		byIp[item.Ip] = item
	}

	assert.Equal(t, "ip-a", items[0].Ip, "按请求数排序 ip-a 居首")
	ipA := byIp["ip-a"]
	assert.Equal(t, 4, ipA.RequestCount)
	assert.Equal(t, 2, ipA.UserCount)
	assert.Equal(t, 3, ipA.TokenCount)
	assert.Equal(t, 2, ipA.TinyRequestCount)
	assert.Equal(t, 2, ipA.ErrorCount)

	ipC := byIp["ip-c"]
	assert.Equal(t, 1, ipC.RequestCount)
	assert.Equal(t, 0, ipC.TokenCount)
	assert.Equal(t, 1, ipC.ErrorCount)
}

func TestRiskOverviewExcludesWhitelistUsers(t *testing.T) {
	truncateTables(t)
	seedOverviewLogs(t)

	users, err := GetUserOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16, ExcludeUserIds: []int{1}})
	require.NoError(t, err)
	require.Len(t, users, 1, "白名单用户整行消失")
	assert.Equal(t, 2, users[0].UserId)

	// 混合使用的 IP 只统计非白名单部分,纯由白名单用户使用的 IP 完全不上榜
	ips, err := GetIpOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: 16, ExcludeUserIds: []int{1}})
	require.NoError(t, err)
	require.Len(t, ips, 1)
	assert.Equal(t, "ip-a", ips[0].Ip)
	assert.Equal(t, 2, ips[0].RequestCount)
	assert.Equal(t, 1, ips[0].UserCount)
	assert.Equal(t, 0, ips[0].TinyRequestCount)
	assert.Equal(t, 2, ips[0].ErrorCount)
}

func TestGetRiskDetailUasAndErrorStatuses(t *testing.T) {
	truncateTables(t)

	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-a", Ua: "curl/8.0"})
	insertOverviewLog(t, 2, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-a", Ua: "curl/8.0"})
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-b", Ua: "python-requests/2.31"})
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-a", Ua: "curl/8.0", Other: `{"status_code":429,"error_code":"rate_limit"}`})
	insertOverviewLog(t, 2, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-a", Ua: "curl/8.0", Other: `{"status_code":429,"error_code":"rate_limit"}`})
	insertOverviewLog(t, 3, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-a", Ua: "curl/8.0", Other: `{"status_code":401,"error_code":"invalid_api_key"}`})
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-a"})                               // 无 other
	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeError, Ip: "ip-a", Other: "not-json"})            // 解析失败
	insertOverviewLog(t, 1, Log{UserId: 2, Username: "u2", Type: LogTypeError, Ip: "ip-a", Other: `{"status_code":500}`}) // 他人的错误

	target := RiskDetailTarget{Type: RiskDetailTypeUser, Value: "1", Hours: 24}

	uas, err := GetRiskDetailUas(target)
	require.NoError(t, err)
	require.Len(t, uas, 2, "空 UA 的行不上榜")
	assert.Equal(t, "curl/8.0", uas[0].Ua)
	assert.Equal(t, 5, uas[0].RequestCount, "消费与错误请求都计入 UA 明细")
	assert.Equal(t, "python-requests/2.31", uas[1].Ua)
	assert.Equal(t, 1, uas[1].RequestCount)

	statuses, sampled, err := GetRiskDetailErrorStatuses(target)
	require.NoError(t, err)
	assert.False(t, sampled, "样本量远低于采样上限")
	require.Len(t, statuses, 2, "无法解析的 other 与他人的错误都不计入")
	assert.Equal(t, 429, statuses[0].StatusCode)
	assert.Equal(t, "rate_limit", statuses[0].ErrorCode)
	assert.Equal(t, 2, statuses[0].Count)
	assert.Equal(t, 401, statuses[1].StatusCode)
	assert.Equal(t, "invalid_api_key", statuses[1].ErrorCode)
	assert.Equal(t, 1, statuses[1].Count)
}

func TestGetRiskDetailByUa(t *testing.T) {
	truncateTables(t)

	insertOverviewLog(t, 1, Log{UserId: 1, Username: "u1", Type: LogTypeConsume, Ip: "ip-a", Ua: "curl/8.0"})
	insertOverviewLog(t, 1, Log{UserId: 2, Username: "u2", Type: LogTypeConsume, Ip: "ip-b", Ua: "curl/8.0"})
	insertOverviewLog(t, 1, Log{UserId: 3, Username: "u3", Type: LogTypeConsume, Ip: "ip-c", Ua: "other/1.0"})

	target := RiskDetailTarget{Type: RiskDetailTypeUa, Value: "curl/8.0", Hours: 24}

	users, err := GetRiskDetailUsers(target)
	require.NoError(t, err)
	require.Len(t, users, 2, "UA 维度下钻出使用它的用户")

	ips, err := GetRiskDetailIps(target)
	require.NoError(t, err)
	require.Len(t, ips, 2, "UA 维度下钻出使用它的 IP")
}

func TestRiskDetailTargetValidation(t *testing.T) {
	cases := []struct {
		name   string
		target RiskDetailTarget
	}{
		{name: "未知类型", target: RiskDetailTarget{Type: "token", Value: "1"}},
		{name: "用户 ID 非数字", target: RiskDetailTarget{Type: RiskDetailTypeUser, Value: "abc"}},
		{name: "用户 ID 非正数", target: RiskDetailTarget{Type: RiskDetailTypeUser, Value: "0"}},
		{name: "IP 为空白", target: RiskDetailTarget{Type: RiskDetailTypeIp, Value: "  "}},
		{name: "UA 为空", target: RiskDetailTarget{Type: RiskDetailTypeUa, Value: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetRiskDetailUsers(tc.target)
			assert.Error(t, err)
			_, _, err = GetRiskDetailErrorStatuses(tc.target)
			assert.Error(t, err)
		})
	}
}

func TestRiskOverviewSorting(t *testing.T) {
	truncateTables(t)
	seedOverviewLogs(t)

	// 用户 1 请求数最多(5)但错误数少(1);用户 2 反之(2 请求 / 2 错误)。
	// 排序字段决定 top N 取自哪个维度,这是合并视图正确性的关键。
	cases := []struct {
		name       string
		sortBy     string
		sortOrder  string
		wantTopUid int
	}{
		{name: "默认按请求数降序", wantTopUid: 1},
		{name: "按错误数降序", sortBy: "error_count", wantTopUid: 2},
		{name: "按错误数升序", sortBy: "error_count", sortOrder: "asc", wantTopUid: 1},
		{name: "按 IP 数降序", sortBy: "ip_count", wantTopUid: 1},
		{name: "未知字段回退请求数", sortBy: "quota); drop table logs;--", wantTopUid: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := GetUserOverviewRanking(RiskOverviewQuery{
				Hours: 24, Limit: 50, TinyMaxTokens: 16,
				SortBy: tc.sortBy, SortOrder: tc.sortOrder,
			})
			require.NoError(t, err)
			require.NotEmpty(t, items)
			assert.Equal(t, tc.wantTopUid, items[0].UserId)
		})
	}
}

func TestRiskOverviewTinyThresholdFallback(t *testing.T) {
	truncateTables(t)
	seedOverviewLogs(t)

	// 阈值为 0 或越界时回退默认值,不能让 SQL 里出现 <= 0 而把微量请求全部算成 0
	for _, maxTokens := range []int{0, -1, 1 << 20} {
		items, err := GetUserOverviewRanking(RiskOverviewQuery{Hours: 24, Limit: 50, TinyMaxTokens: maxTokens})
		require.NoError(t, err)
		require.NotEmpty(t, items)
		assert.Equal(t, 1, items[0].UserId)
		assert.Positive(t, items[0].TinyRequestCount, "maxTokens=%d 时应回退到合法阈值", maxTokens)
	}
}

func TestDisableRegularUserWritesReason(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "victim", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)

	changed, err := DisableRegularUser(user.Id, "触发风控规则")
	require.NoError(t, err)
	require.True(t, changed)

	var saved User
	require.NoError(t, DB.First(&saved, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, saved.Status)
	assert.Equal(t, "触发风控规则", saved.DisableReason)

	// 已禁用用户重复处置幂等
	changed, err = DisableRegularUser(user.Id, "再次")
	require.NoError(t, err)
	assert.False(t, changed)
}
