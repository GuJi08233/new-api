package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyIpBan 是加唯一索引之前的 ip_bans 表结构,目标列只有普通索引。
type legacyIpBan struct {
	Id        int    `gorm:"primaryKey"`
	Target    string `gorm:"type:varchar(128);index:idx_ip_bans_target"`
	Reason    string `gorm:"type:varchar(512)"`
	ExpiresAt int64  `gorm:"bigint;index:idx_ip_bans_expires_at"`
	Source    string `gorm:"type:varchar(32)"`
	CreatedBy int
	CreatedAt int64 `gorm:"bigint;index:idx_ip_bans_created_at"`
	UpdatedAt int64 `gorm:"bigint"`
}

func (legacyIpBan) TableName() string { return "ip_bans" }

// 旧库里同一目标可能有多行(并发封禁写出来的)。迁移把每个目标合并成效力最强的一行:
// 永久优先,其次到期最晚;之后目标列唯一,再次迁移什么也不做。
func TestMigrateIpBanTargetUniquenessMergesDuplicates(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Migrator().DropTable(&IpBan{}))
	t.Cleanup(func() {
		_ = DB.Migrator().DropTable(&IpBan{})
		_ = DB.AutoMigrate(&IpBan{})
		_ = ReloadIpBanCache()
	})
	require.NoError(t, DB.AutoMigrate(&legacyIpBan{}))

	now := common.GetTimestamp()
	seed := []legacyIpBan{
		{Target: "198.51.100.1", Reason: "short", ExpiresAt: now + 60},
		{Target: "198.51.100.1", Reason: "permanent", ExpiresAt: 0},
		{Target: "198.51.100.1", Reason: "long", ExpiresAt: now + 7200},
		{Target: "198.51.100.2", Reason: "earlier", ExpiresAt: now + 600},
		{Target: "198.51.100.2", Reason: "later", ExpiresAt: now + 1200},
		{Target: "198.51.100.3", Reason: "single", ExpiresAt: now + 60},
	}
	for i := range seed {
		require.NoError(t, DB.Create(&seed[i]).Error)
	}

	for range 2 {
		require.NoError(t, migrateIpBanTargetUniqueness(DB))
	}

	var bans []IpBan
	require.NoError(t, DB.Order("target asc").Find(&bans).Error)
	require.Len(t, bans, 3)
	assert.Equal(t, []string{"permanent", "later", "single"}, []string{bans[0].Reason, bans[1].Reason, bans[2].Reason})
	assert.True(t, DB.Migrator().HasIndex(&IpBan{}, ipBanTargetUniqueIndex))
	assert.False(t, DB.Migrator().HasIndex(&IpBan{}, "idx_ip_bans_target"))
	assert.Error(t, DB.Create(&IpBan{Target: "198.51.100.3", CreatedAt: now, UpdatedAt: now}).Error, "目标列已唯一")
}

// 每个封禁目标只有一行:同一目标再次封禁走延长规则,不会写出第二行,解除那一行就真正解封。
func TestUpsertIpBanKeepsOneRowPerTarget(t *testing.T) {
	truncateTables(t)
	resetIpBanCache(t)

	now := common.GetTimestamp()
	first, changed, err := UpsertIpBan("203.0.113.9", "first", now+600, RiskBanSourceProbeGuard, 0)
	require.NoError(t, err)
	require.True(t, changed)
	second, changed, err := UpsertIpBan("203.0.113.9", "second", now+1200, RiskBanSourceErrorGuard, 0)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, first.Id, second.Id)

	var count int64
	require.NoError(t, DB.Model(&IpBan{}).Where("target = ?", "203.0.113.9").Count(&count).Error)
	assert.EqualValues(t, 1, count)

	_, err = ReleaseRiskIpBan(second.Id, 1, "release")
	require.NoError(t, err)
	_, banned := MatchActiveIpBan("203.0.113.9")
	assert.False(t, banned)
}

// 风控 UA 黑名单改用 re: 前缀之后,旧条目按原来生效的方式换写:旧版当正则用的条目加前缀,
// 按子串用的保持原样,恰好以 re: 开头的子串转义成等价正则。迁移只做一次。
func TestMigrateRiskUaPatternSyntax(t *testing.T) {
	truncateTables(t)
	key := operation_setting.RiskControlSettingPrefix + "ua_blacklist"
	require.NoError(t, DB.AutoMigrate(&Option{}))
	cleanup := func() {
		DB.Where(commonKeyCol+" IN ?", []string{riskUaPatternSyntaxOption, key}).Delete(&Option{})
	}
	cleanup()
	t.Cleanup(cleanup)
	legacy := `["curl","^Go-http-client","python-requests/2.31","bad[regex","re:foo"]`
	require.NoError(t, DB.Create(&Option{Key: key, Value: legacy}).Error)

	require.NoError(t, migrateRiskUaPatternSyntax())
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", key).First(&option).Error)
	var entries []string
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &entries))
	assert.Equal(t, []string{"curl", "re:^Go-http-client", `re:python-requests/2.31`, "bad[regex", "re:re:foo"}, entries)

	// 标记已写入,管理员之后保存的新写法不会被再次改写
	require.NoError(t, DB.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", `["^literal"]`).Error)
	require.NoError(t, migrateRiskUaPatternSyntax())
	require.NoError(t, DB.Where(commonKeyCol+" = ?", key).First(&option).Error)
	assert.Equal(t, `["^literal"]`, option.Value)
}

// 管理员手动封禁不计入自动封禁的累犯次数,与账号侧只数 ban_auto 一致。
func TestRecentBanCountsIgnoreManualDispositions(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	seed := []*RiskEvent{
		{EventType: RiskEventBanIp, Ip: "192.0.2.10", Rule: RiskBanSourceProbeGuard, CreatedAt: now - 100},
		{EventType: RiskEventBanIp, Ip: "192.0.2.10", Rule: RiskBanSourceManual, CreatedAt: now - 50},
		{EventType: RiskEventBanAuto, UserId: 77, Rule: RiskBanSourceAutoRule, CreatedAt: now - 100},
		{EventType: RiskEventBanManual, UserId: 77, CreatedAt: now - 50},
	}
	for _, event := range seed {
		require.NoError(t, InsertRiskEvent(event))
	}

	ipCount, err := CountRecentIpBanEvents("192.0.2.10", now-3600)
	require.NoError(t, err)
	assert.EqualValues(t, 1, ipCount)
	userCount, err := CountRecentUserBanEvents(77, now-3600)
	require.NoError(t, err)
	assert.EqualValues(t, 1, userCount)

	// 但手动处置仍是"上次处置"的时间点:它之前的证据已被管理员处理过
	lastIp, err := LastIpBanEventAt("192.0.2.10")
	require.NoError(t, err)
	assert.EqualValues(t, now-50, lastIp)
	lastUser, err := LastUserBanEventAt(77)
	require.NoError(t, err)
	assert.EqualValues(t, now-50, lastUser)
}
