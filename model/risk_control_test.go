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

func TestGetUaRanking(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 1, "u1", "ip-a", "curl/8.0", LogTypeConsume, 1)
	insertRiskLog(t, 2, "u2", "ip-b", "curl/8.0", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "python-requests/2.31", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1) // 空 UA 不参与

	items, err := GetUaRanking(24, 50)
	if err != nil {
		t.Fatalf("GetUaRanking: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Ua != "curl/8.0" || items[0].UserCount != 2 || items[0].RequestCount != 2 {
		t.Fatalf("top item = %+v, want curl/8.0 user_count=2 request_count=2", items[0])
	}
}

func TestRiskDetailQueries(t *testing.T) {
	truncateTables(t)

	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-a", "", LogTypeConsume, 2)
	insertRiskLog(t, 2, "u2", "ip-a", "", LogTypeConsume, 1)
	insertRiskLog(t, 1, "u1", "ip-b", "", LogTypeConsume, 1)

	users, err := GetIpUserDetail("ip-a", 24)
	if err != nil {
		t.Fatalf("GetIpUserDetail: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ip-a users = %d, want 2", len(users))
	}
	if users[0].UserId != 1 || users[0].RequestCount != 2 {
		t.Fatalf("top user = %+v, want user 1 request_count=2", users[0])
	}
	if users[0].FirstSeen <= 0 || users[0].LastSeen < users[0].FirstSeen {
		t.Fatalf("first/last seen invalid: %+v", users[0])
	}

	ips, err := GetUserIpDetail(1, 24)
	if err != nil {
		t.Fatalf("GetUserIpDetail: %v", err)
	}
	if len(ips) != 2 {
		t.Fatalf("user 1 ips = %d, want 2", len(ips))
	}
	if ips[0].Ip != "ip-a" || ips[0].RequestCount != 2 {
		t.Fatalf("top ip = %+v, want ip-a request_count=2", ips[0])
	}

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
