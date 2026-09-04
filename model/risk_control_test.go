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

	items, err := GetIpMultiUserRanking(24, 50, nil)
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

	items, err := GetUserMultiIpRanking(24, 50, nil)
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

	items, err := GetIpMultiUserRanking(10000, 50, nil)
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

	items, err := GetIpMultiTokenRanking(24, 50, nil)
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

	items, err := GetUserTinyRequestRanking(24, 50, 16, nil)
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

	items, err := GetUserErrorRanking(24, 50, nil)
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

	items, err := GetTokenMultiIpRanking(24, 50, nil)
	require.NoError(t, err)
	require.Len(t, items, 1, "只有使用 >1 IP 的令牌上榜")
	assert.Equal(t, 101, items[0].TokenId)
	assert.Equal(t, 1, items[0].UserId)
	assert.Equal(t, "u1", items[0].Username)
	assert.Equal(t, 3, items[0].IpCount)
	assert.Equal(t, 3, items[0].RequestCount)
}

// TestScanRankingsExcludeWhitelistUsers 覆盖全局白名单的核心契约:白名单账号的流量
// 不进入自动封禁扫描所读的排行,因此它独占的出口地址永不上榜、永不被自动封禁,
// 混合出口只统计非白名单部分。
func TestScanRankingsExcludeWhitelistUsers(t *testing.T) {
	truncateTables(t)

	// 账号 9 是全局白名单(运营者自己):独占 ip-own 且使用 3 个令牌,
	// 正是 ip_multi_token 规则最容易命中的形态。
	insertProbeLog(t, 9, "owner", "ip-own", 901, LogTypeConsume, 1, 1, 1)
	insertProbeLog(t, 9, "owner", "ip-own", 902, LogTypeConsume, 1, 1, 1)
	insertProbeLog(t, 9, "owner", "ip-own", 903, LogTypeError, 0, 0, 1)
	// ip-mixed 是白名单账号与普通账号共用的出口
	insertProbeLog(t, 9, "owner", "ip-mixed", 901, LogTypeConsume, 1, 1, 1)
	insertProbeLog(t, 3, "u3", "ip-mixed", 301, LogTypeError, 0, 0, 1)
	// ip-bad 只有普通账号
	insertProbeLog(t, 1, "u1", "ip-bad", 101, LogTypeConsume, 1, 1, 1)
	insertProbeLog(t, 2, "u2", "ip-bad", 102, LogTypeError, 0, 0, 1)

	whitelist := []int{9}

	// 不排除时白名单账号自己就是榜首 —— 这正是会误封运营者出口地址的路径
	unfiltered, err := GetIpMultiTokenRanking(24, 50, nil)
	require.NoError(t, err)
	require.Len(t, unfiltered, 3)
	assert.Equal(t, "ip-own", unfiltered[0].Ip)
	assert.Equal(t, 3, unfiltered[0].TokenCount)

	tokens, err := GetIpMultiTokenRanking(24, 50, whitelist)
	require.NoError(t, err)
	require.Len(t, tokens, 2, "白名单账号独占的出口完全不上榜")
	assert.Equal(t, "ip-bad", tokens[0].Ip)
	assert.Equal(t, 2, tokens[0].TokenCount)
	assert.Equal(t, "ip-mixed", tokens[1].Ip)
	assert.Equal(t, 1, tokens[1].TokenCount, "混合出口只统计非白名单部分")

	ips, err := GetIpMultiUserRanking(24, 50, whitelist)
	require.NoError(t, err)
	require.Len(t, ips, 2)
	assert.Equal(t, "ip-bad", ips[0].Ip)
	assert.Equal(t, 2, ips[0].UserCount)
	assert.Equal(t, "ip-mixed", ips[1].Ip)
	assert.Equal(t, 1, ips[1].UserCount, "白名单账号不计入该出口的关联用户数")

	// 用户维度同样剔除:白名单账号跨 2 个 IP 本会上榜,排除后只剩单 IP 账号(不满足 >1)
	multiIp, err := GetUserMultiIpRanking(24, 50, whitelist)
	require.NoError(t, err)
	assert.Empty(t, multiIp)

	errorItems, err := GetUserErrorRanking(24, 50, whitelist)
	require.NoError(t, err)
	require.Len(t, errorItems, 2)
	for _, item := range errorItems {
		assert.NotEqual(t, 9, item.UserId, "白名单账号不出现在错误排行")
	}
}

func TestGetRecentIpsByUsers(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 9, "owner", "198.51.100.7", "", LogTypeConsume, 1)
	insertRiskLog(t, 9, "owner", "198.51.100.7", "", LogTypeError, 2) // 去重
	insertRiskLog(t, 9, "owner", "2001:db8:1:2::5", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "203.0.113.9", "", LogTypeError, 1)        // 他人的地址
	insertRiskLog(t, 9, "owner", "198.51.100.8", "", LogTypeConsume, 48) // 窗口外
	insertRiskLog(t, 9, "owner", "198.51.100.9", "", LogTypeManage, 1)   // 非风控日志类型
	insertRiskLog(t, 9, "owner", "", "", LogTypeConsume, 1)              // 空 IP

	ips, truncated, err := GetRecentIpsByUsers(24, []int{9})
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.ElementsMatch(t, []string{"198.51.100.7", "2001:db8:1:2::5"}, ips)

	// 放宽窗口后窗口外的地址进入结果
	wide, _, err := GetRecentIpsByUsers(72, []int{9})
	require.NoError(t, err)
	assert.Contains(t, wide, "198.51.100.8")

	// 候选为空时不查库
	empty, truncated, err := GetRecentIpsByUsers(24, nil)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Empty(t, empty)
}

func TestNormalizeAutoBanTarget(t *testing.T) {
	cases := []struct {
		name         string
		target       string
		prefixLength int
		want         string
	}{
		{name: "IPv4 不归并", target: "203.0.113.7", prefixLength: 64, want: "203.0.113.7"},
		{name: "IPv6 归并到 /64", target: "2001:db8:1:2:3:4:5:6", prefixLength: 64, want: "2001:db8:1:2::/64"},
		{name: "IPv6 归并到 /48", target: "2001:db8:1:2:3:4:5:6", prefixLength: 48, want: "2001:db8:1::/48"},
		{name: "前缀 128 表示按单地址封禁", target: "2001:db8:1:2:3:4:5:6", prefixLength: 128, want: "2001:db8:1:2:3:4:5:6"},
		{name: "前缀为 0 时不归并", target: "2001:db8::1", prefixLength: 0, want: "2001:db8::1"},
		{name: "已是 CIDR 的目标原样返回", target: "2001:db8:1:2::/64", prefixLength: 48, want: "2001:db8:1:2::/64"},
		{name: "IPv4 映射地址按 IPv4 处理", target: "::ffff:203.0.113.7", prefixLength: 64, want: "203.0.113.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAutoBanTarget(tc.target, tc.prefixLength)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	_, err := NormalizeAutoBanTarget("not-an-ip", 64)
	assert.Error(t, err)
}

func TestIpBanTargetCovers(t *testing.T) {
	cases := []struct {
		name   string
		target string
		ip     string
		want   bool
	}{
		{name: "单地址相等", target: "203.0.113.7", ip: "203.0.113.7", want: true},
		{name: "单地址不等", target: "203.0.113.7", ip: "203.0.113.8", want: false},
		{name: "CIDR 覆盖", target: "203.0.113.0/24", ip: "203.0.113.99", want: true},
		{name: "CIDR 之外", target: "203.0.113.0/24", ip: "203.0.114.1", want: false},
		{name: "IPv6 /64 覆盖同段其他地址", target: "2001:db8:1:2::/64", ip: "2001:db8:1:2:aaaa::9", want: true},
		{name: "IPv6 /64 不覆盖邻段", target: "2001:db8:1:2::/64", ip: "2001:db8:1:3::1", want: false},
		{name: "IPv4 映射地址按 IPv4 比对", target: "203.0.113.7", ip: "::ffff:203.0.113.7", want: true},
		{name: "非法地址", target: "203.0.113.7", ip: "garbage", want: false},
		{name: "非法目标", target: "garbage", ip: "203.0.113.7", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IpBanTargetCovers(tc.target, tc.ip))
		})
	}
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

func TestHasIpLogsSince(t *testing.T) {
	truncateTables(t)

	since := time.Now().Add(-2 * time.Hour).Unix()
	insertRiskLog(t, 1, "u1", "198.51.100.70", "", LogTypeConsume, 3)    // since 之前
	insertRiskLog(t, 9, "owner", "198.51.100.70", "", LogTypeConsume, 1) // since 之后,但属于白名单账号
	insertRiskLog(t, 2, "u2", "198.51.100.71", "", LogTypeConsume, 1)    // 别的地址

	active, err := HasIpLogsSince("198.51.100.70", since, nil)
	require.NoError(t, err)
	assert.True(t, active, "不排除任何用户时,since 之后的行算活动")

	active, err = HasIpLogsSince("198.51.100.70", since, []int{9})
	require.NoError(t, err)
	assert.False(t, active, "白名单账号的行不算,与排行统计口径一致")

	insertRiskLog(t, 3, "u3", "198.51.100.70", "", LogTypeManage, 1) // 非风控日志类型
	active, err = HasIpLogsSince("198.51.100.70", since, []int{9})
	require.NoError(t, err)
	assert.False(t, active)

	insertRiskLog(t, 3, "u3", "198.51.100.70", "", LogTypeError, 1)
	active, err = HasIpLogsSince("198.51.100.70", since, []int{9})
	require.NoError(t, err)
	assert.True(t, active, "错误日志同样是活动")
}

// 多账号关联统计的取证口径:注册/登录/签到这类账号事件构成证据,
// 充值日志必须排除——它的 IP 是支付网关回调地址,纳入统计会把所有付费用户串成一伙。
func TestGetMultiAccountRankingExcludesTopupSource(t *testing.T) {
	truncateTables(t)

	// gateway-ip 上有 3 个用户的充值回调,但那只是支付网关的地址
	insertRiskLog(t, 1, "u1", "gateway-ip", "", LogTypeTopup, 1)
	insertRiskLog(t, 2, "u2", "gateway-ip", "", LogTypeTopup, 1)
	insertRiskLog(t, 3, "u3", "gateway-ip", "", LogTypeTopup, 1)

	// home-ip 上 2 个账号注册 + 1 个账号登录,是真正的多号证据
	insertRiskLog(t, 11, "m1", "home-ip", "", LogTypeRegister, 10)
	insertRiskLog(t, 12, "m2", "home-ip", "", LogTypeRegister, 10)
	insertRiskLog(t, 11, "m1", "home-ip", "", LogTypeLogin, 1)

	// single-ip 只有 1 个账号,达不到阈值
	insertRiskLog(t, 21, "s1", "single-ip", "", LogTypeRegister, 5)

	items, err := GetMultiAccountRanking(MultiAccountQuery{Hours: 24 * 7})
	require.NoError(t, err)
	require.Len(t, items, 1, "只有 home-ip 应当上榜")

	assert.Equal(t, "home-ip", items[0].Ip)
	assert.Equal(t, 2, items[0].UserCount)
	assert.Equal(t, 2, items[0].RegisterCount)
	assert.Equal(t, 1, items[0].LoginCount)
	assert.Equal(t, 3, items[0].EventCount)
}

// 调用日志默认不算证据,打开 IncludeRequests 才计入——两种口径的窗口上限不同,
// 混淆会让默认口径悄悄退化成 7 天。
func TestGetMultiAccountRankingIncludeRequests(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 31, "c1", "call-ip", "", LogTypeConsume, 1)
	insertRiskLog(t, 32, "c2", "call-ip", "", LogTypeError, 1)

	items, err := GetMultiAccountRanking(MultiAccountQuery{Hours: 24})
	require.NoError(t, err)
	assert.Empty(t, items, "默认口径不看调用与错误日志")

	items, err = GetMultiAccountRanking(MultiAccountQuery{Hours: 24, IncludeRequests: true})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 2, items[0].UserCount)

	minUsers, maxWindow := MultiAccountQuery{IncludeRequests: true}.EffectiveLimits()
	assert.Equal(t, 2, minUsers, "阈值下限是 2:单账号地址不构成关联")
	assert.Equal(t, multiAccountRequestMaxWindowHours, maxWindow)

	_, accountWindow := MultiAccountQuery{}.EffectiveLimits()
	assert.Equal(t, multiAccountMaxWindowHours, accountWindow)
	assert.Greater(t, accountWindow, maxWindow, "账号事件口径行数少,窗口应当更长")
}

// 下钻要能分清每个账号的证据构成:哪个号是在这个地址上注册的,是研判的关键。
func TestGetMultiAccountUsers(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 41, "a1", "shared-ip", "", LogTypeRegister, 20)
	insertRiskLog(t, 41, "a1", "shared-ip", "", LogTypeLogin, 2)
	insertRiskLog(t, 42, "a2", "shared-ip", "", LogTypeLogin, 1)
	insertRiskLog(t, 43, "a3", "other-ip", "", LogTypeRegister, 1)

	items, err := GetMultiAccountUsers("shared-ip", MultiAccountQuery{Hours: 24 * 7})
	require.NoError(t, err)
	require.Len(t, items, 2)

	// 在此注册的账号排在前面
	assert.Equal(t, 41, items[0].UserId)
	assert.Equal(t, 1, items[0].RegisterCount)
	assert.Equal(t, 1, items[0].LoginCount)
	assert.Equal(t, 2, items[0].EventCount)

	assert.Equal(t, 42, items[1].UserId)
	assert.Zero(t, items[1].RegisterCount, "该账号只在此登录,不是在此注册")

	_, err = GetMultiAccountUsers("", MultiAccountQuery{})
	assert.Error(t, err, "缺少地址时应当报错而不是全表聚合")
}

// 白名单账号的行不参与统计,与其他风控榜单口径一致:
// 运营者自己的出口地址不该因为自己多个账号在用就被列为可疑。
func TestGetMultiAccountRankingExcludesWhitelist(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 51, "w1", "office-ip", "", LogTypeLogin, 1)
	insertRiskLog(t, 52, "w2", "office-ip", "", LogTypeLogin, 1)
	insertRiskLog(t, 53, "w3", "office-ip", "", LogTypeLogin, 1)

	items, err := GetMultiAccountRanking(MultiAccountQuery{Hours: 24})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 3, items[0].UserCount)

	items, err = GetMultiAccountRanking(MultiAccountQuery{Hours: 24, ExcludeUserIds: []int{51, 52}})
	require.NoError(t, err)
	assert.Empty(t, items, "剔除白名单后只剩 1 个账号,达不到阈值")
}
