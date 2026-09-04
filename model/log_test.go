package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLogTestContext(userAgent string) *gin.Context {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("User-Agent", userAgent)
	return &gin.Context{Request: request}
}

func TestGetRequestLogUaDisabled(t *testing.T) {
	original := common.IsGlobalRecordUaLogEnabled()
	t.Cleanup(func() {
		common.SetGlobalRecordUaLogEnvEnabled(false)
		common.SetGlobalRecordUaLogEnabled(original)
	})

	common.SetGlobalRecordUaLogEnvEnabled(false)
	common.SetGlobalRecordUaLogEnabled(false)
	if got := getRequestLogUa(newLogTestContext("test-agent")); got != "" {
		t.Fatalf("getRequestLogUa() = %q when disabled, want empty string", got)
	}
}

func TestGetRequestLogUaTruncatesByRune(t *testing.T) {
	original := common.IsGlobalRecordUaLogEnabled()
	t.Cleanup(func() {
		common.SetGlobalRecordUaLogEnvEnabled(false)
		common.SetGlobalRecordUaLogEnabled(original)
	})

	common.SetGlobalRecordUaLogEnvEnabled(false)
	common.SetGlobalRecordUaLogEnabled(true)

	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{name: "empty", userAgent: "", want: ""},
		{name: "ascii exact limit", userAgent: strings.Repeat("a", maxLogUaLength), want: strings.Repeat("a", maxLogUaLength)},
		{name: "ascii over limit", userAgent: strings.Repeat("a", maxLogUaLength+1), want: strings.Repeat("a", maxLogUaLength)},
		{name: "unicode over limit", userAgent: strings.Repeat("界", maxLogUaLength+1), want: strings.Repeat("界", maxLogUaLength)},
		{name: "invalid UTF-8", userAgent: "agent-\xff", want: "agent-�"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRequestLogUa(newLogTestContext(tt.userAgent))
			if got != tt.want {
				t.Fatalf("getRequestLogUa() rune length = %d, want %d", utf8.RuneCountInString(got), utf8.RuneCountInString(tt.want))
			}
		})
	}
}

func TestRecordRequestLogsPersistSanitizedUa(t *testing.T) {
	truncateTables(t)
	originalUa := common.IsGlobalRecordUaLogEnabled()
	originalExport := common.DataExportEnabled
	t.Cleanup(func() {
		common.SetGlobalRecordUaLogEnvEnabled(false)
		common.SetGlobalRecordUaLogEnabled(originalUa)
		common.DataExportEnabled = originalExport
	})

	common.SetGlobalRecordUaLogEnvEnabled(false)
	common.SetGlobalRecordUaLogEnabled(true)
	common.DataExportEnabled = false
	context := newLogTestContext("client-\xff")
	context.Set("username", "log-user")

	RecordConsumeLog(context, 1001, RecordConsumeLogParams{ModelName: "test-model"})
	RecordErrorLog(context, 1001, 0, "test-model", "", "upstream error", 0, 1, false, "default", nil)

	var logs []Log
	if err := LOG_DB.Where("user_id = ?", 1001).Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("query request logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("persisted logs = %d, want 2", len(logs))
	}
	for _, log := range logs {
		if log.Ua != "client-�" {
			t.Fatalf("log type %d UA = %q, want sanitized value", log.Type, log.Ua)
		}
	}
	if !LOG_DB.Migrator().HasColumn(&Log{}, "ua") {
		t.Fatal("logs table is missing the ua column after AutoMigrate")
	}
}

// 注册、签到这类账号审计日志的来源必须始终可溯源：IP / UA 不随中转调用日志的隐私
// 开关一起关掉，否则运营者一关调用日志的 IP 记录，一人多号就再也追不回注册来源。
func TestRecordAuditLogKeepsSourceWhenRequestLogsDisabled(t *testing.T) {
	truncateTables(t)
	originalIp := common.IsGlobalRecordIpLogEnabled()
	originalUa := common.IsGlobalRecordUaLogEnabled()
	t.Cleanup(func() {
		common.SetGlobalRecordIpLogEnvEnabled(false)
		common.SetGlobalRecordIpLogEnabled(originalIp)
		common.SetGlobalRecordUaLogEnvEnabled(false)
		common.SetGlobalRecordUaLogEnabled(originalUa)
	})
	common.SetGlobalRecordIpLogEnvEnabled(false)
	common.SetGlobalRecordIpLogEnabled(false)
	common.SetGlobalRecordUaLogEnvEnabled(false)
	common.SetGlobalRecordUaLogEnabled(false)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", nil)
	context.Request.Header.Set("User-Agent", strings.Repeat("界", maxLogUaLength+1))
	context.Request.RemoteAddr = "203.0.113.7:1234"

	RecordAuditLog(ClientLogSource(context), 2001, "new-user", LogTypeSystem, "用户注册（密码注册）",
		map[string]interface{}{"register_method": "password"})

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ?", 2001).First(&log).Error)
	assert.Equal(t, "203.0.113.7", log.Ip)
	assert.Equal(t, strings.Repeat("界", maxLogUaLength), log.Ua)
	assert.Equal(t, "new-user", log.Username)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.Equal(t, "password", other["register_method"])
}

// 账号相关的日志写入口都必须把来源落到 Ip / Ua 列。新增一类日志时若忘了接上
// LogSource，这里会直接失败——来源缺一类，追查滥用时就断一环。
func TestAccountLogWritersPersistSource(t *testing.T) {
	truncateTables(t)
	source := NewLogSource("198.51.100.9", "audit-agent/2.0")

	tests := []struct {
		name   string
		userId int
		write  func(userId int)
	}{
		{
			name:   "RecordLog",
			userId: 3001,
			write:  func(userId int) { RecordLog(source, userId, LogTypeSystem, "签到") },
		},
		{
			name:   "RecordLogWithAdminInfo",
			userId: 3002,
			write: func(userId int) {
				RecordLogWithAdminInfo(source, userId, LogTypeManage, "[风控] 自动禁用", map[string]interface{}{"source": "risk_control"})
			},
		},
		{
			name:   "RecordAuditLog",
			userId: 3003,
			write: func(userId int) {
				RecordAuditLog(source, userId, "u", LogTypeSystem, "用户注册", map[string]interface{}{"register_method": "password"})
			},
		},
		{
			name:   "RecordLoginLog",
			userId: 3004,
			write: func(userId int) {
				RecordLoginLog(source, userId, "u", "Logged in", "login", nil, nil)
			},
		},
		{
			name:   "RecordOperationAuditLog",
			userId: 3005,
			write: func(userId int) {
				RecordOperationAuditLog(source, userId, "POST /api/channel", "channel.create", nil, nil, nil)
			},
		},
		{
			name:   "RecordTopupLog",
			userId: 3006,
			write:  func(userId int) { RecordTopupLog(source, userId, "充值成功", "epay", "alipay") },
		},
		{
			name:   "RecordTaskBillingLog",
			userId: 3007,
			write: func(userId int) {
				RecordTaskBillingLog(RecordTaskBillingLogParams{
					UserId: userId, LogType: LogTypeRefund, Content: "退款", Source: source,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.write(tt.userId)

			var log Log
			require.NoError(t, LOG_DB.Where("user_id = ?", tt.userId).First(&log).Error)
			assert.Equal(t, source.Ip, log.Ip)
			assert.Equal(t, source.Ua, log.Ua)
		})
	}
}

func TestGetModelAvailabilityByGroupHourlyBuckets(t *testing.T) {
	truncateTables(t)

	nowBucket := time.Now().Unix() / 3600
	bucketStart := func(hoursAgo int64) int64 { return (nowBucket - hoursAgo) * 3600 }
	// 取桶内中点写入，避免测试执行期间跨过整点导致数据滑出统计窗口
	bucketMid := func(hoursAgo int64) int64 { return bucketStart(hoursAgo) + 1800 }

	logs := []*Log{
		// alpha：1 小时前 3 成功 1 失败，3 小时前 1 失败
		{ModelName: "alpha", Type: LogTypeConsume, CreatedAt: bucketMid(1), Group: "default"},
		{ModelName: "alpha", Type: LogTypeConsume, CreatedAt: bucketMid(1), Group: "default"},
		{ModelName: "alpha", Type: LogTypeConsume, CreatedAt: bucketMid(1), Group: "default"},
		{ModelName: "alpha", Type: LogTypeError, CreatedAt: bucketMid(1), Group: "default"},
		{ModelName: "alpha", Type: LogTypeError, CreatedAt: bucketMid(3), Group: "default"},
		// beta：另一分组，全部成功
		{ModelName: "beta", Type: LogTypeConsume, CreatedAt: bucketMid(1), Group: "vip"},
		// 窗口外（25 小时前）不计入
		{ModelName: "alpha", Type: LogTypeError, CreatedAt: bucketMid(25), Group: "default"},
		// 空模型名与非 consume/error 类型不计入
		{ModelName: "", Type: LogTypeConsume, CreatedAt: bucketMid(1), Group: "default"},
		{ModelName: "alpha", Type: LogTypeTopup, CreatedAt: bucketMid(1), Group: "default"},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	result, err := GetModelAvailabilityByGroup("", 24)
	require.NoError(t, err)

	byName := make(map[string]ModelAvailability, len(result))
	for _, item := range result {
		byName[item.ModelName] = item
	}
	require.Contains(t, byName, "alpha")
	require.Contains(t, byName, "beta")
	require.NotContains(t, byName, "")

	alpha := byName["alpha"]
	assert.Equal(t, 5, alpha.TotalCount)
	assert.Equal(t, 3, alpha.SuccessCount)
	assert.Equal(t, 2, alpha.ErrorCount)
	assert.InDelta(t, 60.0, alpha.SuccessRate, 0.001)
	assert.Equal(t, ModelAvailabilityStatusError, alpha.Status)

	// 时间轴必须是连续整点小时桶，前端按顺序渲染依赖该契约
	require.Len(t, alpha.Hourly, 24)
	start := alpha.Hourly[0].Timestamp
	require.Zero(t, start%3600)
	for i, bucket := range alpha.Hourly {
		require.Equal(t, start+int64(i)*3600, bucket.Timestamp)
	}

	hourlySuccess, hourlyError := 0, 0
	for _, bucket := range alpha.Hourly {
		hourlySuccess += bucket.SuccessCount
		hourlyError += bucket.ErrorCount
	}
	assert.Equal(t, alpha.SuccessCount, hourlySuccess)
	assert.Equal(t, alpha.ErrorCount, hourlyError)

	idxOf := func(ts int64) int {
		idx := int((ts - start) / 3600)
		require.GreaterOrEqual(t, idx, 0)
		require.Less(t, idx, len(alpha.Hourly))
		return idx
	}
	recentBucket := alpha.Hourly[idxOf(bucketStart(1))]
	assert.Equal(t, 3, recentBucket.SuccessCount)
	assert.Equal(t, 1, recentBucket.ErrorCount)
	olderBucket := alpha.Hourly[idxOf(bucketStart(3))]
	assert.Equal(t, 0, olderBucket.SuccessCount)
	assert.Equal(t, 1, olderBucket.ErrorCount)

	beta := byName["beta"]
	assert.Equal(t, 1, beta.TotalCount)
	assert.Equal(t, ModelAvailabilityStatusNormal, beta.Status)
	assert.InDelta(t, 100.0, beta.SuccessRate, 0.001)

	// 分组过滤只统计该分组的日志
	vipResult, err := GetModelAvailabilityByGroup("vip", 24)
	require.NoError(t, err)
	require.Len(t, vipResult, 1)
	assert.Equal(t, "beta", vipResult[0].ModelName)

	// hours 超上限被钳制，桶数不会无限膨胀
	clampedResult, err := GetModelAvailabilityByGroup("", 100000)
	require.NoError(t, err)
	for _, item := range clampedResult {
		assert.Len(t, item.Hourly, maxAvailabilityHours)
	}
}

func TestWilsonLowerBound(t *testing.T) {
	assert.Zero(t, wilsonLowerBound(0, 0))
	assert.Zero(t, wilsonLowerBound(5, 0))
	assert.InDelta(t, 0.0, wilsonLowerBound(0, 5), 1e-9)
	// 已知参考值（z=1.96）
	assert.InDelta(t, 0.2307, wilsonLowerBound(3, 5), 0.0005)
	assert.InDelta(t, 0.9287, wilsonLowerBound(50, 50), 0.0005)
	assert.InDelta(t, 0.0260, wilsonLowerBound(5, 83), 0.0005)
	assert.InDelta(t, 0.2065, wilsonLowerBound(1, 1), 0.0005)
	// 同成功率下样本量越大，下界越高
	assert.Greater(t, wilsonLowerBound(950, 1000), wilsonLowerBound(95, 100))
	// 大样本的高成功率优于小样本的 100%
	assert.Greater(t, wilsonLowerBound(3050, 3099), wilsonLowerBound(50, 50))
}

func TestGetUserModelAvailabilityRanking(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "avail-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(user).Error)

	modelNames := []string{"big-good", "small-perfect", "big-bad", "all-fail", "never-called"}
	for i, name := range modelNames {
		require.NoError(t, DB.Create(&Ability{Group: "default", Model: name, ChannelId: i + 1, Enabled: true}).Error)
	}

	createdAt := (time.Now().Unix()/3600-1)*3600 + 1800
	var logs []*Log
	appendLogs := func(modelName string, success, failure int) {
		for i := 0; i < success; i++ {
			logs = append(logs, &Log{ModelName: modelName, Type: LogTypeConsume, CreatedAt: createdAt, Group: "default"})
		}
		for i := 0; i < failure; i++ {
			logs = append(logs, &Log{ModelName: modelName, Type: LogTypeError, CreatedAt: createdAt, Group: "default"})
		}
	}
	appendLogs("big-good", 48, 2)      // 96%，n=50 → 可靠度约 86.5%
	appendLogs("small-perfect", 20, 0) // 100%，n=20 → 可靠度约 83.9%
	appendLogs("big-bad", 6, 4)        // 60%，n=10 → 可靠度约 31.3%
	appendLogs("all-fail", 0, 3)       // 0% → 可靠度 0，但有数据，排在无数据之前
	require.NoError(t, LOG_DB.Create(&logs).Error)

	result, err := GetUserModelAvailability(user.Id, 24)
	require.NoError(t, err)
	require.Len(t, result, len(modelNames))

	gotOrder := make([]string, 0, len(result))
	for _, item := range result {
		gotOrder = append(gotOrder, item.ModelName)
	}
	// 大样本高成功率 > 小样本 100% > 低成功率 > 全失败 > 无调用记录
	assert.Equal(t, []string{"big-good", "small-perfect", "big-bad", "all-fail", "never-called"}, gotOrder)

	assert.Greater(t, result[0].Reliability, result[1].Reliability)
	assert.InDelta(t, 100.0, result[1].SuccessRate, 0.001)
	assert.Equal(t, ModelAvailabilityStatusNoData, result[4].Status)
}
