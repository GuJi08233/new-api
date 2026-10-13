package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRiskEventReviewsProtectConcurrentEditsAndKeepHistory(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	require.NoError(t, db.Create(&User{Id: 1, Username: "admin", AffCode: "admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&User{Id: 2, Username: "ordinary", AffCode: "ordinary", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	event := &RiskEvent{EventType: RiskEventAlert, Rule: "ip_multi_user", Evidence: &RiskRuleEvidence{
		RuleId: "office-network", Version: "v1", Threshold: 2, Observed: 3, Action: "alert", Comparison: ">",
	}}
	require.NoError(t, InsertRiskEvent(event))
	updated, err := ReviewRiskEvent(event.Id, 0, RiskReviewFalsePositive, "shared office", 1, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, updated.ReviewVersion)
	_, err = ReviewRiskEvent(event.Id, 0, RiskReviewConfirmed, "stale edit", 1, 3)
	require.ErrorIs(t, err, ErrRiskReviewChanged)
	_, err = ReviewRiskEvent(event.Id, 1, RiskReviewConfirmed, "invalid assignee", 2, 1)
	require.Error(t, err)
	rows, total, err := QueryRiskEvents(RiskEventQuery{RuleId: "office-network", ReviewStatus: RiskReviewFalsePositive, AssigneeId: 1}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, "shared office", rows[0].ReviewNote)
	assert.Equal(t, 2, rows[0].Evidence.Threshold)
	_, err = ReviewRiskEvent(event.Id, 1, RiskReviewPending, "needs more evidence", 0, 3)
	require.NoError(t, err)
	history, err := GetRiskEventReviews(event.Id)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, "needs more evidence", history[0].Note)
	assert.Equal(t, "shared office", history[1].Note)
	assert.Equal(t, 3, history[0].OperatorId)
}

func TestRiskEventReviewAcceptsLegacyNullDefaultsAndCleansWithEvent(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	event := &RiskEvent{EventType: RiskEventAlert, CreatedAt: 100, Count: 1}
	require.NoError(t, db.Create(event).Error)
	require.NoError(t, db.Model(event).Updates(map[string]interface{}{"review_status": nil, "review_version": nil}).Error)
	_, total, err := QueryRiskEvents(RiskEventQuery{ReviewStatus: RiskReviewPending}, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	_, err = ReviewRiskEvent(event.Id, 0, RiskReviewConfirmed, "legacy event", 0, 1)
	require.NoError(t, err)
	deleted, err := CleanupRiskEvents(200)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	history, err := GetRiskEventReviews(event.Id)
	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestRiskManualDispositionClearsTemporaryExpiryAndRejectsRoleChanges(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	user := &User{Id: 10, Username: "temporary", Role: common.RoleCommonUser, Status: common.UserStatusDisabled,
		DisableReason: "auto ban", DisableExpiresAt: common.GetTimestamp() - 1}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetManualUserRiskState(tx, 10, common.RoleCommonUser, common.UserStatusDisabled, "manual permanent")
	}))
	restored, err := RestoreExpiredUserBans(common.GetTimestamp())
	require.NoError(t, err)
	assert.Empty(t, restored)
	require.NoError(t, db.First(user, 10).Error)
	assert.Zero(t, user.DisableExpiresAt)
	assert.Equal(t, "manual permanent", user.DisableReason)
	require.NoError(t, db.Model(user).Update("role", common.RoleAdminUser).Error)
	err = db.Transaction(func(tx *gorm.DB) error {
		return SetManualUserRiskState(tx, 10, common.RoleCommonUser, common.UserStatusEnabled, "")
	})
	require.ErrorIs(t, err, ErrUserRoleChanged)
	require.NoError(t, db.First(user, 10).Error)
	assert.Equal(t, common.UserStatusDisabled, user.Status)
}

func TestRiskExemptionAppendUsesStoredListAndIsIdempotent(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	require.NoError(t, db.Create(&User{Id: 10, Username: "target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	key := operation_setting.RiskControlSettingPrefix + "whitelist_user_ids"
	require.NoError(t, db.Create(&Option{Key: key, Value: "[99]"}).Error)
	changed, err := AddRiskExemptUser(10, common.RoleCommonUser, false)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []int{10, 99}, operation_setting.GetRiskControlSetting().WhitelistUserIds)
	changed, err = AddRiskExemptUser(10, common.RoleCommonUser, false)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRiskRuleIdentityPersistsAcrossEditsAndReordering(t *testing.T) {
	setupRiskAdminModelTest(t)
	key := operation_setting.RiskControlSettingPrefix + "auto_ban_rules"
	rules := []operation_setting.RiskAutoBanRule{
		{Name: "legacy rule", Metric: "ip_multi_user", Enabled: true, WindowHours: 24, Threshold: 2, Action: "alert"},
		{Id: "named-rule", Name: "named rule", Metric: "user_multi_ip", Enabled: true, WindowHours: 24, Threshold: 2, Action: "alert"},
	}
	data, err := common.Marshal(rules)
	require.NoError(t, err)
	require.NoError(t, UpdateOptionsBulk(map[string]string{key: string(data)}))
	stored, err := ReadOptionValue(key)
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalJsonStr(stored, &rules))
	require.NotEmpty(t, rules[0].Id)
	legacyId := rules[0].Id
	rules[0].Name, rules[0].Threshold = "edited legacy rule", 5
	rules[0], rules[1] = rules[1], rules[0]
	data, err = common.Marshal(rules)
	require.NoError(t, err)
	require.NoError(t, UpdateOption(key, string(data)))
	stored, err = ReadOptionValue(key)
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalJsonStr(stored, &rules))
	assert.Equal(t, "named-rule", rules[0].Id)
	assert.Equal(t, legacyId, rules[1].Id)
	assert.Equal(t, "edited legacy rule", rules[1].Name)
	assert.Equal(t, 5, rules[1].Threshold)
}

func TestRiskIpReleaseRollsBackWithoutAuditAndKeepsCoveringBan(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	exact, _, err := UpsertIpBan("198.51.100.12", "exact", 0, RiskBanSourceManual, 1)
	require.NoError(t, err)
	cover, _, err := UpsertIpBan("198.51.100.0/24", "network", 0, RiskBanSourceManual, 1)
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("risk_release_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "risk_events" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}))
	_, err = ReleaseRiskIpBan(exact.Id, 9, "approved release")
	require.Error(t, err)
	_, err = GetIpBanById(exact.Id)
	require.NoError(t, err, "审计失败时删除也要回滚")
	require.NoError(t, db.Callback().Create().Remove("risk_release_audit_failure"))
	_, err = ReleaseRiskIpBan(exact.Id, 9, "approved release")
	require.NoError(t, err)
	states, err := GetIpBanStates([]string{exact.Target})
	require.NoError(t, err)
	require.Len(t, states[exact.Target], 1)
	assert.Equal(t, cover.Id, states[exact.Target][0].Id)
	events, _, err := QueryRiskEvents(RiskEventQuery{EventType: RiskEventUnbanIp, Ip: exact.Target}, 0, 20)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "approved release", events[0].Reason)
	assert.Equal(t, 9, events[0].OperatorId)
}

func TestRiskOverviewPaginationReachesBeyondPreviousTop200(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	logs := make([]Log, 201)
	for index := range logs {
		logs[index] = Log{UserId: index + 1, Username: fmt.Sprintf("member-%03d", index+1), Ip: fmt.Sprintf("198.51.%d.%d", index/255, index%255+1),
			Ua: fmt.Sprintf("client-%03d", index+1), CreatedAt: common.GetTimestamp(), Type: LogTypeConsume}
	}
	require.NoError(t, db.Create(&logs).Error)
	for _, metric := range []string{RiskMetricUserOverview, RiskMetricIpOverview, RiskMetricUa} {
		total, err := CountRiskOverview(metric, RiskOverviewQuery{Hours: 24})
		require.NoError(t, err)
		assert.EqualValues(t, 201, total)
	}
	page, err := GetUserOverviewRanking(RiskOverviewQuery{Hours: 24, Offset: 200, Limit: 20})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, 201, page[0].UserId)
	query := RiskOverviewQuery{Hours: 24, Keyword: "member-201", ExcludeUserIds: []int{1}, Limit: 20}
	total, err := CountRiskOverview(RiskMetricUserOverview, query)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	page, err = GetUserOverviewRanking(query)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, 201, page[0].UserId)
	require.NoError(t, db.Create(&Log{UserId: 300, Username: "literal%_name", CreatedAt: common.GetTimestamp(), Type: LogTypeConsume}).Error)
	total, err = CountRiskOverview(RiskMetricUserOverview, RiskOverviewQuery{Hours: 24, Keyword: "%_"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total, "搜索中的百分号和下划线作为普通文本")
}

func TestRiskMultiAccountSearchAndPaginationUseSamePopulation(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	logs := []Log{
		{UserId: 1, Type: LogTypeRegister, Ip: "198.51.100.2", CreatedAt: common.GetTimestamp()},
		{UserId: 2, Type: LogTypeRegister, Ip: "198.51.100.2", CreatedAt: common.GetTimestamp()},
		{UserId: 1, Type: LogTypeRegister, Ip: "198.51.100.1", CreatedAt: common.GetTimestamp()},
		{UserId: 2, Type: LogTypeRegister, Ip: "198.51.100.1", CreatedAt: common.GetTimestamp()},
	}
	require.NoError(t, db.Create(&logs).Error)
	query := MultiAccountQuery{Hours: 168, Limit: 1, MinUsers: 2}
	total, err := CountMultiAccountRanking(query)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	first, err := GetMultiAccountRanking(query)
	require.NoError(t, err)
	require.Len(t, first, 1)
	query.Offset = 1
	second, err := GetMultiAccountRanking(query)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.NotEqual(t, first[0].Ip, second[0].Ip, "同指标数值的相邻页也应保持稳定且不重复")
	query.Offset, query.Keyword = 0, "198.51.100.2"
	total, err = CountMultiAccountRanking(query)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	page, err := GetMultiAccountRanking(query)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, query.Keyword, page[0].Ip)
	query.ExcludeUserIds = []int{2}
	total, err = CountMultiAccountRanking(query)
	require.NoError(t, err)
	assert.Zero(t, total, "计数也要排除全局豁免账号并应用最少关联数")
}

func setupRiskAdminModelTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB, previousReplica := DB, LOG_DB, RO_DB
	previousOptions := common.OptionMap
	previousRisk := operation_setting.GetRiskControlSetting()
	previousTrade, previousGame := operation_setting.GetTradeSetting(), operation_setting.GetGameSetting()
	riskStaging, err := config.ConfigToMap(config.GlobalConfig.Get("risk_control_setting"))
	require.NoError(t, err)
	ipStaging, err := config.ConfigToMap(config.GlobalConfig.Get("ip_location_setting"))
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "risk-admin.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &User{}, &IpBan{}, &RiskEvent{}, &RiskEventReview{}, &RiskScanRun{}, &Log{}))
	DB, LOG_DB, RO_DB = db, db, nil
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		DB, LOG_DB, RO_DB = previousDB, previousLogDB, previousReplica
		require.NoError(t, ReloadIpBanCache())
		common.OptionMap = previousOptions
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("risk_control_setting"), riskStaging))
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("ip_location_setting"), ipStaging))
		operation_setting.SetRiskControlSettingForTest(previousRisk)
		operation_setting.SyncIpLocationSetting()
		operation_setting.SetTradeSettingForTest(previousTrade)
		operation_setting.SetGameSettingForTest(previousGame)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestRiskSettingsBulkRejectsInvalidValuesWithoutPartialSave(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	previous := operation_setting.GetRiskControlSetting()
	require.Error(t, UpdateOptionsBulk(map[string]string{
		"risk_control_setting.enabled":                 "true",
		"risk_control_setting.probe_guard_model_count": "1",
	}))
	var rows int64
	require.NoError(t, db.Model(&Option{}).Count(&rows).Error)
	assert.Zero(t, rows)
	assert.Empty(t, common.OptionMap)
	assert.Equal(t, previous, operation_setting.GetRiskControlSetting())
}

func TestRiskSettingsBulkDatabaseFailureRollsBackAndPreservesRuntime(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	previous := operation_setting.GetRiskControlSetting()
	writes := 0
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("risk_save_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			writes++
			if writes == 2 {
				tx.AddError(errors.New("injected option write failure"))
			}
		}
	}))
	require.ErrorContains(t, UpdateOptionsBulk(map[string]string{
		"risk_control_setting.enabled":                 "true",
		"risk_control_setting.probe_guard_model_count": "7",
	}), "injected option write failure")
	require.NoError(t, db.Callback().Create().Remove("risk_save_failure"))
	var rows int64
	require.NoError(t, db.Model(&Option{}).Count(&rows).Error)
	assert.Zero(t, rows)
	assert.Empty(t, common.OptionMap)
	assert.Equal(t, previous, operation_setting.GetRiskControlSetting())
}

func TestRiskSettingsBulkAndSingleWritesPublishEffectiveValues(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		"risk_control_setting.enabled":                 "true",
		"risk_control_setting.probe_guard_model_count": "7",
		"risk_control_setting.whitelist_user_ids":      "[42]",
		"ip_location_setting.auto_lookup":              "false",
	}))
	current := operation_setting.GetRiskControlSetting()
	assert.True(t, current.Enabled)
	assert.Equal(t, 7, current.ProbeGuardModelCount)
	assert.Equal(t, []int{42}, current.WhitelistUserIds)
	assert.False(t, operation_setting.GetIpLocationSetting().AutoLookup)
	require.NoError(t, UpdateOption("risk_control_setting.enabled", "false"))
	assert.False(t, operation_setting.GetRiskControlSetting().Enabled)
	require.NoError(t, db.Model(&Option{}).Where("key = ?", "risk_control_setting.probe_guard_model_count").Update("value", "9").Error)
	loadOptionsFromDatabase()
	assert.Equal(t, 9, operation_setting.GetRiskControlSetting().ProbeGuardModelCount)
}

func TestGetRiskUserStatesReadsCurrentPrimaryState(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	user := User{Id: 42, Username: "current", Role: common.RoleAdminUser, Status: common.UserStatusDisabled, DisableReason: "review", DisableExpiresAt: 12345}
	require.NoError(t, db.Create(&user).Error)
	replica, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "replica.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, replica.AutoMigrate(&User{}))
	require.NoError(t, replica.Create(&User{Id: 42, Username: "stale", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	RO_DB = replica
	t.Cleanup(func() { sqlDB, err := replica.DB(); require.NoError(t, err); require.NoError(t, sqlDB.Close()) })
	states, err := GetRiskUserStates([]int{42, 999})
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, common.RoleAdminUser, states[42].Role)
	assert.Equal(t, common.UserStatusDisabled, states[42].Status)
	assert.Equal(t, "current", states[42].Username)
	assert.Equal(t, "review", states[42].DisableReason)
	assert.EqualValues(t, 12345, states[42].DisableExpiresAt)
}

func TestGetIpBanStatesIncludesCoveringAndOverlappingNetworks(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	now := time.Now().Unix()
	bans := []IpBan{
		{Target: "198.51.100.0/24", ExpiresAt: 0},
		{Target: "198.51.100.7", ExpiresAt: now + 600},
		{Target: "198.51.100.0/25", ExpiresAt: now - 1},
		{Target: "203.0.113.0/24", ExpiresAt: 0},
		{Target: "2001:db8:1::/64", ExpiresAt: 0},
	}
	require.NoError(t, db.Create(&bans).Error)
	states, err := GetIpBanStates([]string{"198.51.100.7", "198.51.100.0/25", "203.0.113.8", "2001:db8:1::9"})
	require.NoError(t, err)
	require.Len(t, states["198.51.100.7"], 2)
	require.Len(t, states["198.51.100.0/25"], 2)
	require.Len(t, states["203.0.113.8"], 1)
	require.Len(t, states["2001:db8:1::9"], 1)
	require.NoError(t, db.Delete(&bans[1]).Error)
	states, err = GetIpBanStates([]string{"198.51.100.7"})
	require.NoError(t, err)
	require.Len(t, states["198.51.100.7"], 1)
	assert.Equal(t, "198.51.100.0/24", states["198.51.100.7"][0].Target)
}

func TestQueryRiskEventsFiltersTimeUaAndCoveringNetworks(t *testing.T) {
	db := setupRiskAdminModelTest(t)
	events := []RiskEvent{
		{CreatedAt: 1000, EventType: RiskEventBanIp, Ip: "198.51.100.0/24", Ua: "client"},
		{CreatedAt: 2000, EventType: RiskEventBlockIp, Ip: "198.51.100.7", Ua: "client"},
		{CreatedAt: 2000, EventType: RiskEventBlockIp, Ip: "203.0.113.7", Ua: "client"},
		{CreatedAt: 2000, EventType: RiskEventBlockIp, Ip: "198.51.100.7", Ua: "other"},
	}
	require.NoError(t, db.Create(&events).Error)
	rows, total, err := QueryRiskEvents(RiskEventQuery{Ip: "198.51.100.7", Ua: "client", StartAt: 1000, EndAt: 2000}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, rows, 2)
	assert.Equal(t, events[1].Id, rows[0].Id)
	assert.Equal(t, events[0].Id, rows[1].Id)
	rows, total, err = QueryRiskEvents(RiskEventQuery{Ip: "198.51.100.7", Ua: "client", StartAt: 1500}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, events[1].Id, rows[0].Id)
}
