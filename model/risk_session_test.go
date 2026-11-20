package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSharedSessionRanking(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()

	s1 := "sess-shared-alpha"
	s2 := "sess-single-user"
	c1 := "cache-shared-beta"
	seed := []Log{
		{UserId: 101, Username: "alice", TokenId: 10, TokenName: "tok-a", Ip: "198.51.100.1", Type: LogTypeConsume, Quota: 500, CreatedAt: now - 300,
			SessionId: s1, SessionSource: "header:x-claude-code-session-id", SessionHash: common.SessionIdentifierHash(s1),
			CacheKey: c1, CacheSource: "body:prompt_cache_key", CacheHash: common.SessionIdentifierHash(c1)},
		{UserId: 102, Username: "bob", TokenId: 11, TokenName: "tok-b", Ip: "203.0.113.5", Type: LogTypeError, Quota: 0, CreatedAt: now - 100,
			SessionId: s1, SessionSource: "body:metadata.user_id.session_id", SessionHash: common.SessionIdentifierHash(s1),
			CacheKey: c1, CacheSource: "body:prompt_cache_key", CacheHash: common.SessionIdentifierHash(c1)},
		{UserId: 103, Username: "charlie", TokenId: 12, TokenName: "tok-c", Ip: "192.0.2.9", Type: LogTypeConsume, Quota: 200, CreatedAt: now - 50,
			SessionId: s2, SessionSource: "header:session-id", SessionHash: common.SessionIdentifierHash(s2)},
	}
	for i := range seed {
		require.NoError(t, LOG_DB.Create(&seed[i]).Error)
	}

	items, total, err := GetSharedSessionRanking(SharedSessionQuery{
		Kind:     SharedSessionKindSession,
		Hours:    24,
		MinUsers: 2,
		Keyword:  "alice",
		Limit:    20,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	assert.Equal(t, s1, items[0].Identifier)
	assert.Equal(t, 2, items[0].UserCount, "按单个用户关键词搜索不应隐藏共用同一会话的其余账号")
	assert.EqualValues(t, 2, items[0].RequestCount)
	assert.EqualValues(t, 1, items[0].ErrorCount)
	assert.EqualValues(t, 500, items[0].Quota)
	assert.ElementsMatch(t, []string{"198.51.100.1", "203.0.113.5"}, items[0].Ips)

	// 排除其中一个白名单账号后，只剩 1 个账号，不再上榜
	excluded, totalExcluded, err := GetSharedSessionRanking(SharedSessionQuery{
		Kind:           SharedSessionKindCache,
		Hours:          24,
		MinUsers:       2,
		ExcludeUserIds: []int{101},
		Limit:          20,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, totalExcluded)
	assert.Empty(t, excluded)
}

func TestGetTokenOverviewRanking(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	seed := []Log{
		{UserId: 10, Username: "u10", TokenId: 50, TokenName: "shared-key", Ip: "198.51.100.1", Type: LogTypeConsume, Quota: 100, CreatedAt: now - 200},
		{UserId: 10, Username: "u10", TokenId: 50, TokenName: "shared-key", Ip: "198.51.100.2", Type: LogTypeConsume, Quota: 200, CreatedAt: now - 100},
		{UserId: 10, Username: "u10", TokenId: 50, TokenName: "shared-key", Ip: "198.51.100.3", Type: LogTypeError, Quota: 0, CreatedAt: now - 50},
		{UserId: 11, Username: "u11", TokenId: 51, TokenName: "normal-key", Ip: "203.0.113.1", Type: LogTypeConsume, Quota: 50, CreatedAt: now - 20},
	}
	for i := range seed {
		require.NoError(t, LOG_DB.Create(&seed[i]).Error)
	}

	query := RiskOverviewQuery{Hours: 24, Limit: 20, TinyMaxTokens: 16, SortBy: "ip_count", SortOrder: "desc"}
	total, err := CountRiskOverview(RiskMetricTokenOverview, query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)

	items, err := GetTokenOverviewRanking(query)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, 50, items[0].TokenId)
	assert.Equal(t, 3, items[0].IpCount)
	assert.Equal(t, 3, items[0].RequestCount)
	assert.Equal(t, 1, items[0].ErrorCount)
	assert.EqualValues(t, 300, items[0].Quota)
}

func TestRiskProbeUserStateBatchAndReset(t *testing.T) {
	truncateTables(t)
	user := User{Username: "probe_user", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "pbu1"}
	require.NoError(t, DB.Create(&user).Error)

	base := time.Unix(1700000000, 0)
	policy := RiskProbeBatchPolicy{WindowSeconds: 30, Threshold: 3, RecordOffense: true}

	r1, err := recordRiskProbePayloadMatchAt(user.Id, base, "text:1", "say OK", "req-1", policy, base)
	require.NoError(t, err)
	assert.False(t, r1.ThresholdReached)
	assert.Equal(t, 1, r1.MatchedRequests)

	r2, err := recordRiskProbePayloadMatchAt(user.Id, base.Add(5*time.Second), "text:1", "say OK", "req-2", policy, base.Add(5*time.Second))
	require.NoError(t, err)
	assert.False(t, r2.ThresholdReached)
	assert.Equal(t, 2, r2.MatchedRequests)

	r3, err := recordRiskProbePayloadMatchAt(user.Id, base.Add(10*time.Second), "text:1", "say OK", "req-3", policy, base.Add(10*time.Second))
	require.NoError(t, err)
	assert.True(t, r3.ThresholdReached)
	assert.Equal(t, 3, r3.MatchedRequests)
	assert.Equal(t, 1, r3.State.OffenseCount)

	// 在上次触发之前发起的在途请求应被忽略，不会计入下一轮窗口
	inflight, err := recordRiskProbePayloadMatchAt(user.Id, base.Add(9*time.Second), "text:1", "say OK", "req-4", policy, base.Add(11*time.Second))
	require.NoError(t, err)
	assert.True(t, inflight.Ignored)
	assert.False(t, inflight.ThresholdReached)

	require.NoError(t, ResetRiskProbeUserState(user.Id))
	state, err := GetRiskProbeUserState(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 0, state.OffenseCount)
	assert.Empty(t, state.LastRuleId)
}

func TestIpBanUpdateAndRelatedUserTraceability(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	user := User{Username: "cascade_target", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, DisableReason: "banned", AffCode: "cst1"}
	require.NoError(t, DB.Create(&user).Error)

	ban, changed, err := UpsertIpBan("198.51.100.77", "initial", 0, RiskBanSourceManual, 1, true)
	require.NoError(t, err)
	require.True(t, changed)
	assert.True(t, ban.AutoBanUser)

	matched, ok := MatchActiveIpBan("198.51.100.77")
	require.True(t, ok)
	assert.True(t, matched.AutoBanUser)

	require.NoError(t, RecordIpBanUserBan(ban.Id, user.Id, "198.51.100.77", "cascade reason"))
	counts, err := GetIpBanRelatedUserCounts([]int{ban.Id})
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[ban.Id])

	related, err := GetIpBanRelatedUsers(ban.Id, 50)
	require.NoError(t, err)
	require.Len(t, related, 1)
	assert.Equal(t, user.Id, related[0].UserId)
	assert.Equal(t, "cascade_target", related[0].Username)

	updated, err := UpdateIpBanById(ban.Id, "updated reason", common.GetTimestamp()+3600, false)
	require.NoError(t, err)
	assert.Equal(t, "updated reason", updated.Reason)
	assert.False(t, updated.AutoBanUser)

	matched, ok = MatchActiveIpBan("198.51.100.77")
	require.True(t, ok)
	assert.False(t, matched.AutoBanUser)

	// 解除 IP 封禁时同步清理关联表
	_, err = ReleaseRiskIpBan(ban.Id, 1, "manual unban")
	require.NoError(t, err)
	counts, err = GetIpBanRelatedUserCounts([]int{ban.Id})
	require.NoError(t, err)
	assert.Zero(t, counts[ban.Id])
}

// TestUpgradeFromLegacyRiskTables 验证旧版 logs 与 ip_bans 表平滑升级到新结构，不丢失历史记录。
func TestUpgradeFromLegacyRiskTables(t *testing.T) {
	truncateTables(t)

	type legacyLog struct {
		Id        int    `gorm:"primaryKey"`
		UserId    int    `gorm:"index"`
		CreatedAt int64  `gorm:"bigint"`
		Type      int
		Content   string
		Username  string
		TokenId   int
		Ip        string
		Ua        string
	}
	legacyLogTable := "logs"

	require.NoError(t, DB.Migrator().DropTable(&Log{}, &IpBan{}, &IpBanUserBan{}, &RiskProbeUserState{}))
	t.Cleanup(func() {
		_ = DB.Migrator().DropTable(&Log{}, &IpBan{}, &IpBanUserBan{}, &RiskProbeUserState{})
		_ = DB.AutoMigrate(&Log{}, &IpBan{}, &IpBanUserBan{}, &RiskProbeUserState{})
		_ = ReloadIpBanCache()
	})
	require.NoError(t, DB.Table(legacyLogTable).AutoMigrate(&legacyLog{}))
	require.NoError(t, DB.AutoMigrate(&legacyIpBan{}))

	now := common.GetTimestamp()
	require.NoError(t, DB.Table(legacyLogTable).Create(&legacyLog{UserId: 9, Username: "old", CreatedAt: now - 10, Type: LogTypeConsume, Ip: "198.51.100.8"}).Error)
	require.NoError(t, DB.Create(&legacyIpBan{Target: "198.51.100.8", Reason: "legacy ban", ExpiresAt: 0, CreatedAt: now - 10, UpdatedAt: now - 10}).Error)

	require.NoError(t, migrateIpBanTargetUniqueness(DB))
	require.NoError(t, DB.AutoMigrate(&Log{}, &IpBan{}, &IpBanUserBan{}, &RiskProbeUserState{}))
	require.NoError(t, ReloadIpBanCache())

	var oldLogs []Log
	require.NoError(t, DB.Find(&oldLogs).Error)
	require.Len(t, oldLogs, 1)
	assert.Equal(t, "198.51.100.8", oldLogs[0].Ip)
	assert.Empty(t, oldLogs[0].SessionHash)

	ban, ok := MatchActiveIpBan("198.51.100.8")
	require.True(t, ok)
	assert.Equal(t, "legacy ban", ban.Reason)
	assert.False(t, ban.AutoBanUser)

	cov, err := GetRiskLogCoverage(now - 3600)
	require.NoError(t, err)
	assert.EqualValues(t, 1, cov.Requests)
	assert.EqualValues(t, 0, cov.WithSession)
}
