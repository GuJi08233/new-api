package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRiskBatchPreviewsAndAppliesOnlyExplicitAuthorizedTargets(t *testing.T) {
	db := riskAdminFixture(t)
	users := []model.User{
		{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
		{Id: 2, Username: "selected", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 3, Username: "other-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 4, Username: "temporary", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, DisableExpiresAt: common.GetTimestamp() + 60},
		{Id: 5, Username: "same-ip-unselected", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
	}
	for index := range users {
		users[index].AffCode = users[index].Username
	}
	require.NoError(t, db.Create(&users).Error)
	for _, id := range []int{2, 5} {
		require.NoError(t, db.Create(&model.Log{UserId: id, Type: model.LogTypeConsume, Ip: "198.51.100.10", CreatedAt: common.GetTimestamp()}).Error)
	}
	request := riskBatchRequest{Action: "disable_user", Preview: true, Targets: []riskBatchTarget{
		{Type: "user", Value: "2"}, {Type: "user", Value: "3"}, {Type: "user", Value: "4"}, {Type: "user", Value: "1"}, {Type: "user", Value: "999"},
	}}
	response := riskAdminRequest(t, ManageRiskBatch, common.RoleAdminUser, http.MethodPost, "/api/risk/batch", request)
	var result struct {
		Success bool
		Data    struct{ Results []riskBatchResult }
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Len(t, result.Data.Results, 5)
	assert.True(t, result.Data.Results[0].Allowed)
	assert.False(t, result.Data.Results[1].Allowed)
	assert.True(t, result.Data.Results[2].Allowed)
	assert.False(t, result.Data.Results[3].Allowed)
	var selected model.User
	require.NoError(t, db.First(&selected, 2).Error)
	assert.Equal(t, common.UserStatusEnabled, selected.Status, "预览不改变状态")
	request.Preview, request.Reason = false, "manual review"
	response = riskAdminRequest(t, ManageRiskBatch, common.RoleAdminUser, http.MethodPost, "/api/risk/batch", request)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, "success", result.Data.Results[0].Status)
	assert.Equal(t, "failed", result.Data.Results[1].Status)
	assert.Equal(t, "success", result.Data.Results[2].Status)
	for _, id := range []int{2, 4} {
		var user model.User
		require.NoError(t, db.First(&user, id).Error)
		assert.Equal(t, common.UserStatusDisabled, user.Status)
		assert.Zero(t, user.DisableExpiresAt)
		assert.Equal(t, request.Reason, user.DisableReason)
	}
	for _, id := range []int{1, 3, 5} {
		var user model.User
		require.NoError(t, db.First(&user, id).Error)
		assert.Equal(t, common.UserStatusEnabled, user.Status)
	}
}

func TestRiskBatchProtectsRootAndCurrentIpAndReturnsPerTargetResults(t *testing.T) {
	db := riskAdminFixture(t)
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	response := riskAdminRequest(t, ManageRiskBatch, common.RoleRootUser, http.MethodPost, "/api/risk/batch", riskBatchRequest{
		Action: "disable_user", Targets: []riskBatchTarget{{Type: "user", Value: "1"}}, Reason: "review",
	})
	var result struct {
		Success bool
		Data    struct{ Results []riskBatchResult }
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Data.Results[0].Allowed)
	assert.Contains(t, result.Data.Results[0].Message, "root")
	response = riskAdminRequest(t, func(c *gin.Context) {
		c.Request.RemoteAddr = "192.0.2.1:1234"
		ManageRiskBatch(c)
	}, common.RoleAdminUser, http.MethodPost, "/api/risk/batch", riskBatchRequest{
		Action: "ban_ip", Targets: []riskBatchTarget{{Type: "ip", Value: "192.0.2.0/24"}, {Type: "ip", Value: "198.51.100.12"}, {Type: "ip", Value: "invalid"}},
		Reason: "review", ExpireMinutes: 30,
	})
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "failed", result.Data.Results[0].Status)
	assert.Equal(t, "success", result.Data.Results[1].Status)
	assert.Equal(t, "failed", result.Data.Results[2].Status)
	var bans []model.IpBan
	require.NoError(t, db.Find(&bans).Error)
	require.Len(t, bans, 1)
	assert.Equal(t, "198.51.100.12", bans[0].Target)
	assert.Greater(t, bans[0].ExpiresAt, common.GetTimestamp())
	response = riskAdminRequest(t, ManageRiskBatch, common.RoleAdminUser, http.MethodPost, "/api/risk/batch", riskBatchRequest{
		Action: "whitelist_global", Preview: true, Targets: []riskBatchTarget{{Type: "user", Value: "1"}},
	})
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestRiskOverviewKeepsAvailableDataWhenLogDatabaseFails(t *testing.T) {
	db := riskAdminFixture(t)
	require.NoError(t, db.Create(&model.User{Id: 10, Username: "disabled", Role: common.RoleCommonUser, Status: common.UserStatusDisabled}).Error)
	run, err := model.StartRiskScan("test-master", 1)
	require.NoError(t, err)
	require.NoError(t, model.FinishRiskScan(run, 0, errors.New("log query failed")))
	require.NoError(t, db.Migrator().DropTable(&model.Log{}))
	response := riskAdminRequest(t, GetRiskOverview, common.RoleAdminUser, http.MethodGet, "/api/risk/overview", nil)
	var result struct {
		Success bool
		Data    struct {
			Counts model.RiskDispositionCounts
			Scan   model.RiskScanRun
			Errors map[string]string
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.EqualValues(t, 1, result.Data.Counts.DisabledUsers)
	assert.Equal(t, "failed", result.Data.Scan.Status)
	assert.Contains(t, result.Data.Errors, "coverage")
}

func TestRiskRulePreviewApiRejectsInvalidRuleWithoutMutations(t *testing.T) {
	db := riskAdminFixture(t)
	response := riskAdminRequest(t, PreviewRiskRule, common.RoleRootUser, http.MethodPost, "/api/risk/rules/preview", map[string]interface{}{
		"rule": operation_setting.RiskAutoBanRule{Id: "invalid", Metric: "user_multi_ip", WindowHours: 24, Threshold: 1, Action: "ban_ip"},
	})
	var result struct{ Success bool }
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	var rows int64
	require.NoError(t, db.Model(&model.IpBan{}).Count(&rows).Error)
	assert.Zero(t, rows)
}

func riskAdminFixture(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB, previousReplica := model.DB, model.LOG_DB, model.RO_DB
	previousOptions, previousRedis := common.OptionMap, common.RedisEnabled
	previousRisk := operation_setting.GetRiskControlSetting()
	previousTrade, previousGame := operation_setting.GetTradeSetting(), operation_setting.GetGameSetting()
	riskStaging, err := config.ConfigToMap(config.GlobalConfig.Get("risk_control_setting"))
	require.NoError(t, err)
	ipStaging, err := config.ConfigToMap(config.GlobalConfig.Get("ip_location_setting"))
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "risk-admin.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.IpBan{}, &model.RiskEvent{}, &model.RiskEventReview{}, &model.RiskScanRun{}, &model.Log{}, &model.Token{}))
	model.DB, model.LOG_DB, model.RO_DB = db, db, nil
	common.OptionMap, common.RedisEnabled = make(map[string]string), false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, model.RO_DB = previousDB, previousLogDB, previousReplica
		common.OptionMap, common.RedisEnabled = previousOptions, previousRedis
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

func riskAdminRequest(t *testing.T, handler gin.HandlerFunc, role int, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 1)
	c.Set("role", role)
	c.Set("username", "operator")
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(data))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return recorder
}

func TestUpdateRiskSettingsValidatesWholeRequestAndReportsFields(t *testing.T) {
	db := riskAdminFixture(t)
	previous := operation_setting.GetRiskControlSetting()
	recorder := riskAdminRequest(t, UpdateRiskSettings, common.RoleRootUser, http.MethodPut, "/api/option/risk", map[string]string{
		"risk_control_setting.enabled":               "true",
		"risk_control_setting.error_guard_threshold": "1",
		"UnrelatedOption":                            "true",
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var result struct {
		Success bool
		Data    struct {
			FieldErrors map[string]string `json:"field_errors"`
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Data.FieldErrors, "risk_control_setting.error_guard_threshold")
	assert.Contains(t, result.Data.FieldErrors, "UnrelatedOption")
	var rows int64
	require.NoError(t, db.Model(&model.Option{}).Count(&rows).Error)
	assert.Zero(t, rows)
	assert.Equal(t, previous, operation_setting.GetRiskControlSetting())
}

func TestUpdateRiskSettingsPublishesTogetherAndDoesNotExposeSecrets(t *testing.T) {
	db := riskAdminFixture(t)
	recorder := riskAdminRequest(t, UpdateRiskSettings, common.RoleRootUser, http.MethodPut, "/api/option/risk", map[string]string{
		"risk_control_setting.enabled":                 "true",
		"risk_control_setting.probe_guard_model_count": "7",
		"ip_location_setting.gitee_api_key":            "test-secret",
	})
	require.Equal(t, http.StatusOK, recorder.Code)
	var result struct {
		Success bool
		Data    struct{ Options map[string]string }
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.True(t, operation_setting.GetRiskControlSetting().Enabled)
	assert.Equal(t, 7, operation_setting.GetRiskControlSetting().ProbeGuardModelCount)
	assert.Equal(t, "test-secret", operation_setting.GetIpLocationSetting().GiteeApiKey)
	assert.NotContains(t, recorder.Body.String(), "test-secret")
	assert.Equal(t, "true", result.Data.Options["risk_control_setting.enabled"])
	assert.Equal(t, "7", result.Data.Options["risk_control_setting.probe_guard_model_count"])
	assert.NotContains(t, result.Data.Options, "ip_location_setting.gitee_api_key")
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.NotContains(t, logs[0].Other, "test-secret")
}

func TestRiskStateReportsCurrentPermissionsAndExemptions(t *testing.T) {
	db := riskAdminFixture(t)
	users := []model.User{
		{Id: 42, Username: "member", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "u42"},
		{Id: 43, Username: "disabled", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, DisableReason: "review", DisableExpiresAt: 12345, AffCode: "u43"},
		{Id: 44, Username: "admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "u44"},
		{Id: 45, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "u45"},
	}
	require.NoError(t, db.Create(&users).Error)
	operation_setting.SetRiskControlSettingForTest(&operation_setting.RiskControlSetting{WhitelistUserIds: []int{42}, PublicKeyUserIds: []int{43}})
	cases := []struct {
		name, path                                 string
		role                                       int
		canDisable, canEnable, exempt, whitelisted bool
	}{
		{"whitelisted manual action", "/api/risk/state?type=user&value=42", common.RoleAdminUser, true, false, true, true},
		{"temporary shared account can be restored or permanently disabled", "/api/risk/state?type=user&value=43", common.RoleAdminUser, true, true, true, false},
		{"admin cannot manage peer", "/api/risk/state?type=user&value=44", common.RoleAdminUser, false, false, true, false},
		{"root can manage admin", "/api/risk/state?type=user&value=44", common.RoleRootUser, true, false, true, false},
		{"root cannot disable root", "/api/risk/state?type=user&value=45", common.RoleRootUser, false, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := riskAdminRequest(t, GetRiskState, tc.role, http.MethodGet, tc.path, nil)
			var result struct {
				Success bool
				Data    struct{ User model.RiskUserState }
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
			require.True(t, result.Success)
			assert.Equal(t, tc.canDisable, result.Data.User.CanDisable)
			assert.Equal(t, tc.canEnable, result.Data.User.CanEnable)
			assert.Equal(t, tc.exempt, result.Data.User.AccountExempt)
			assert.Equal(t, tc.whitelisted, result.Data.User.Whitelisted)
		})
	}
}

func TestRiskRankingsIncludesCurrentUserState(t *testing.T) {
	db := riskAdminFixture(t)
	require.NoError(t, db.Create(&model.User{Id: 42, Username: "current", Role: common.RoleAdminUser, Status: common.UserStatusDisabled, AffCode: "rank42"}).Error)
	require.NoError(t, db.Create(&model.Log{UserId: 42, Username: "historical", Type: model.LogTypeConsume, CreatedAt: time.Now().Unix()}).Error)
	recorder := riskAdminRequest(t, GetRiskRankings, common.RoleAdminUser, http.MethodGet, "/api/risk/rankings?metric=user_overview", nil)
	var result struct {
		Success bool
		Data    struct {
			Meta struct {
				UserStates map[int]model.RiskUserState `json:"user_states"`
			}
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Contains(t, result.Data.Meta.UserStates, 42)
	state := result.Data.Meta.UserStates[42]
	assert.Equal(t, "current", state.Username)
	assert.Equal(t, common.UserStatusDisabled, state.Status)
	assert.False(t, state.CanManage)
	assert.False(t, state.CanEnable)
}

func TestRiskRankingsStateFailureKeepsInvestigationEvidence(t *testing.T) {
	db := riskAdminFixture(t)
	require.NoError(t, db.Create(&model.Log{UserId: 42, Username: "historical", Type: model.LogTypeConsume, CreatedAt: time.Now().Unix()}).Error)
	require.NoError(t, db.Migrator().DropTable(&model.User{}))
	recorder := riskAdminRequest(t, GetRiskRankings, common.RoleAdminUser, http.MethodGet, "/api/risk/rankings?metric=user_overview", nil)
	var result struct {
		Success bool
		Data    struct {
			Items []model.UserOverviewItem
			Meta  struct {
				StateAvailable bool                        `json:"state_available"`
				UserStates     map[int]model.RiskUserState `json:"user_states"`
			}
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Len(t, result.Data.Items, 1)
	assert.Equal(t, 42, result.Data.Items[0].UserId)
	assert.False(t, result.Data.Meta.StateAvailable)
	assert.Empty(t, result.Data.Meta.UserStates)
}

func TestRiskEventsRejectsInvalidTimeRange(t *testing.T) {
	for _, path := range []string{
		"/api/risk/events?start_timestamp=20&end_timestamp=10",
		"/api/risk/events?start_timestamp=invalid",
		"/api/risk/events?end_timestamp=-1",
	} {
		recorder := riskAdminRequest(t, GetRiskEvents, common.RoleAdminUser, http.MethodGet, path, nil)
		var result struct {
			Success bool
			Message string
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.Equal(t, "时间范围无效", result.Message)
	}
}

// 禁用账号列表附带当前管理权限:普通管理员看得到被禁用的同级管理员,但不能启用他。
func TestRiskDisabledUsersIncludesPermissions(t *testing.T) {
	db := riskAdminFixture(t)
	users := []model.User{
		{Id: 51, Username: "member", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, AffCode: "u51"},
		{Id: 52, Username: "peer-admin", Role: common.RoleAdminUser, Status: common.UserStatusDisabled, AffCode: "u52"},
		{Id: 53, Username: "active", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "u53"},
	}
	require.NoError(t, db.Create(&users).Error)
	recorder := riskAdminRequest(t, GetRiskDisabledUsers, common.RoleAdminUser, http.MethodGet, "/api/risk/disabled_users?p=1&page_size=20", nil)
	var result struct {
		Success bool
		Data    struct {
			Page struct {
				Total int
				Items []model.RiskDisabledUser
			}
			UserStates map[int]model.RiskUserState `json:"user_states"`
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, 2, result.Data.Page.Total)
	assert.True(t, result.Data.UserStates[51].CanEnable)
	assert.False(t, result.Data.UserStates[52].CanEnable)

	recorder = riskAdminRequest(t, GetRiskDisabledUsers, common.RoleAdminUser, http.MethodGet, "/api/risk/disabled_users?kind=forever", nil)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	assert.False(t, result.Success)
}

// 批量处理的备注是可选字段:省略时保留每条事件原有备注,显式传空串时清空。
func TestRiskEventReviewBatchApiKeepsOrClearsNote(t *testing.T) {
	db := riskAdminFixture(t)
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "operator", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "op1"}).Error)
	event := &model.RiskEvent{EventType: model.RiskEventAlert, Reason: "alert"}
	require.NoError(t, model.InsertRiskEvent(event))
	_, err := model.ReviewRiskEvent(event.Id, 0, model.RiskReviewPending, "first look", 0, 1)
	require.NoError(t, err)

	type response struct {
		Success bool
		Data    struct{ Results []model.RiskEventReviewResult }
	}
	var result response
	recorder := riskAdminRequest(t, ReviewRiskEventsBatch, common.RoleAdminUser, http.MethodPost, "/api/risk/event_reviews", map[string]any{
		"items": []model.RiskEventReviewTarget{{Id: event.Id, Version: 1}}, "status": model.RiskReviewConfirmed,
	})
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.True(t, result.Data.Results[0].Success)
	assert.Equal(t, "first look", result.Data.Results[0].Event.ReviewNote)

	recorder = riskAdminRequest(t, ReviewRiskEventsBatch, common.RoleAdminUser, http.MethodPost, "/api/risk/event_reviews", map[string]any{
		"items": []model.RiskEventReviewTarget{{Id: event.Id, Version: 2}}, "status": model.RiskReviewFalsePositive, "note": "",
	})
	result = response{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.True(t, result.Data.Results[0].Success)
	assert.Empty(t, result.Data.Results[0].Event.ReviewNote)
	assert.Equal(t, model.RiskReviewFalsePositive, result.Data.Results[0].Event.ReviewStatus)
}
