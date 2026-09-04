package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetIpBanCache 清空内存缓存,保证用例间互不干扰。
func resetIpBanCache(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM ip_bans").Error)
	require.NoError(t, ReloadIpBanCache())
	t.Cleanup(func() {
		DB.Exec("DELETE FROM ip_bans")
		_ = ReloadIpBanCache()
	})
}

func TestNormalizeIpBanTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "1.2.3.4", want: "1.2.3.4"},
		{input: " 1.2.3.4 ", want: "1.2.3.4"},
		{input: "10.0.5.6/8", want: "10.0.0.0/8"}, // CIDR 归一化为网段基址
		{input: "2001:db8::1", want: "2001:db8::1"},
		{input: "2001:db8::/32", want: "2001:db8::/32"},
		{input: "::ffff:1.2.3.4", want: "1.2.3.4"}, // IPv4-mapped 展开
		{input: "", wantErr: true},
		{input: "not-an-ip", wantErr: true},
		{input: "1.2.3.4/33", wantErr: true},
	}
	for _, test := range tests {
		got, err := NormalizeIpBanTarget(test.input)
		if test.wantErr {
			assert.Error(t, err, "input %q", test.input)
			continue
		}
		require.NoError(t, err, "input %q", test.input)
		assert.Equal(t, test.want, got, "input %q", test.input)
	}
}

func TestUpsertIpBanAndMatch(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	future := common.GetTimestamp() + 600

	// 新建临时封禁:精确 IP 命中,其他 IP 不命中
	ban, changed, err := UpsertIpBan("1.2.3.4", "测试", future, RiskBanSourceManual, 9)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, RiskBanSourceManual, ban.Source)

	matched, ok := MatchActiveIpBan("1.2.3.4")
	require.True(t, ok)
	assert.Equal(t, "1.2.3.4", matched.Target)
	_, ok = MatchActiveIpBan("1.2.3.5")
	assert.False(t, ok)

	// CIDR 封禁:网段内命中,网段外不命中
	_, changed, err = UpsertIpBan("10.0.0.0/8", "网段", 0, RiskBanSourceAutoRule, 0)
	require.NoError(t, err)
	assert.True(t, changed)
	matched, ok = MatchActiveIpBan("10.20.30.40")
	require.True(t, ok)
	assert.Equal(t, "10.0.0.0/8", matched.Target)
	_, ok = MatchActiveIpBan("11.0.0.1")
	assert.False(t, ok)

	// 已过期的临时封禁不命中
	past := common.GetTimestamp() - 10
	require.NoError(t, DB.Model(&IpBan{}).Where("target = ?", "1.2.3.4").Update("expires_at", past).Error)
	require.NoError(t, ReloadIpBanCache())
	_, ok = MatchActiveIpBan("1.2.3.4")
	assert.False(t, ok)
}

func TestUpsertIpBanExtendSemantics(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	now := common.GetTimestamp()

	// 临时封禁只能延长,不能缩短
	_, changed, err := UpsertIpBan("5.6.7.8", "first", now+600, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	require.True(t, changed)

	_, changed, err = UpsertIpBan("5.6.7.8", "shorter", now+60, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.False(t, changed, "更短的期限不应覆盖现有封禁")

	ban, changed, err := UpsertIpBan("5.6.7.8", "longer", now+7200, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.EqualValues(t, now+7200, ban.ExpiresAt)

	// 升级为永久
	ban, changed, err = UpsertIpBan("5.6.7.8", "permanent", 0, RiskBanSourceAutoRule, 0)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.EqualValues(t, 0, ban.ExpiresAt)

	// 永久封禁不可被降级,重复永久也不视为变更
	_, changed, err = UpsertIpBan("5.6.7.8", "downgrade", now+600, RiskBanSourceManual, 0)
	require.NoError(t, err)
	assert.False(t, changed, "永久封禁不应被降级为临时")
	_, changed, err = UpsertIpBan("5.6.7.8", "again", 0, RiskBanSourceManual, 0)
	require.NoError(t, err)
	assert.False(t, changed, "重复永久封禁不应视为变更")

	// 已过期的临时封禁被新封禁重新生效
	require.NoError(t, DB.Exec("DELETE FROM ip_bans").Error)
	_, _, err = UpsertIpBan("9.9.9.9", "old", now-100, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	ban, changed, err = UpsertIpBan("9.9.9.9", "rearmed", now+600, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.EqualValues(t, now+600, ban.ExpiresAt)
}

func TestCleanupExpiredIpBans(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	now := common.GetTimestamp()
	seed := []*IpBan{
		{Target: "1.1.1.1", ExpiresAt: now - 100000, CreatedAt: now, UpdatedAt: now}, // 过期超宽限,应删除
		{Target: "2.2.2.2", ExpiresAt: now - 10, CreatedAt: now, UpdatedAt: now},     // 刚过期,宽限内保留
		{Target: "3.3.3.3", ExpiresAt: now + 600, CreatedAt: now, UpdatedAt: now},    // 未过期
		{Target: "4.4.4.4", ExpiresAt: 0, CreatedAt: now, UpdatedAt: now},            // 永久
	}
	for _, ban := range seed {
		require.NoError(t, DB.Create(ban).Error)
	}

	deleted, err := CleanupExpiredIpBans(3600)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	var remaining []IpBan
	require.NoError(t, DB.Order("target asc").Find(&remaining).Error)
	require.Len(t, remaining, 3)
	assert.Equal(t, "2.2.2.2", remaining[0].Target)
}

func TestCountRecentIpBanEvents(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBanIp, Ip: "1.2.3.4", CreatedAt: now - 100},
		{EventType: RiskEventBanIp, Ip: "1.2.3.4", CreatedAt: now - 200},
		{EventType: RiskEventBanIp, Ip: "1.2.3.4", CreatedAt: now - 100000}, // 窗口外
		{EventType: RiskEventBanIp, Ip: "5.6.7.8", CreatedAt: now - 100},    // 其他 IP
		{EventType: RiskEventBlockIp, Ip: "1.2.3.4", CreatedAt: now - 100},  // 拦截事件不计入
	}
	for _, e := range seed {
		require.NoError(t, InsertRiskEvent(e))
	}

	count, err := CountRecentIpBanEvents("1.2.3.4", now-3600)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}
