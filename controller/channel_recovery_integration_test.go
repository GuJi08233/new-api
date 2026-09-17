package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const channelRecoveryOpenAIResponse = `{"id":"recovery-test","object":"chat.completion","model":"gpt-recovery-integration","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

type channelRecoveryProbe struct {
	Path          string
	Authorization string
	GoogleAPIKey  string
	Body          map[string]any
	Err           error
}

// 所有恢复探测都只能访问本例的本地上游，错误的适配器回退也不会触达真实服务。
type channelRecoveryLocalTransport struct {
	host      string
	transport http.RoundTripper
}

func (transport channelRecoveryLocalTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "http" || request.URL.Host != transport.host {
		return nil, fmt.Errorf("unexpected recovery upstream: %s", request.URL.Redacted())
	}
	return transport.transport.RoundTrip(request)
}

func newChannelRecoveryUpstream(t *testing.T, status int, response string, beforeResponse func() error) (*httptest.Server, <-chan channelRecoveryProbe) {
	t.Helper()
	probes := make(chan channelRecoveryProbe, 4)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		probe := channelRecoveryProbe{
			Path:          request.URL.Path,
			Authorization: request.Header.Get("Authorization"),
			GoogleAPIKey:  request.Header.Get("x-goog-api-key"),
		}
		probe.Err = common.DecodeJson(request.Body, &probe.Body)
		if probe.Err == nil && beforeResponse != nil {
			probe.Err = beforeResponse()
		}
		probes <- probe
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)
	return server, probes
}

func setupChannelRecoveryIntegrationTest(t *testing.T, server *httptest.Server) *gorm.DB {
	t.Helper()
	db := setupChannelMultiKeyConversionTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelRecoveryState{}, &model.Model{}))

	previousReadDB := model.RO_DB
	previousRequestInterval := common.RequestInterval
	previousDataExport := common.DataExportEnabled
	previousLogConsume := common.LogConsumeEnabled
	previousGinMode := gin.Mode()
	previousModelRatio := ratio_setting.ModelRatio2JSONString()
	previousGroupRatio := ratio_setting.GroupRatio2JSONString()
	model.RO_DB = nil
	common.RequestInterval = 0
	common.DataExportEnabled = false
	common.LogConsumeEnabled = true
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		model.RO_DB = previousReadDB
		common.RequestInterval = previousRequestInterval
		common.DataExportEnabled = previousDataExport
		common.LogConsumeEnabled = previousLogConsume
		gin.SetMode(previousGinMode)
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-recovery-integration":1,"gemini-recovery-integration":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"channel-recovery-integration":1}`))
	require.NoError(t, db.Create(&model.User{
		Username: "recovery-root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "channel-recovery-integration",
		Quota:    1_000_000,
		// 通知路径显式使用空邮箱，不发送邮件或调用通知服务。
		Email:   "",
		Setting: `{"notify_type":"email"}`,
	}).Error)

	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	client := service.GetHttpClient()
	previousClient := *client
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	client.Transport = channelRecoveryLocalTransport{host: serverURL.Host, transport: server.Client().Transport}
	client.Timeout = 5 * time.Second
	t.Cleanup(func() {
		client.CloseIdleConnections()
		*client = previousClient
	})
	service.InitTokenEncoders()
	return db
}

func channelRecoveryTestChannel(baseURL string) *model.Channel {
	channel := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "recovery-integration",
		Key:     "sk-recovery-target",
		Status:  common.ChannelStatusAutoDisabled,
		Models:  "gpt-recovery-integration",
		Group:   "channel-recovery-integration",
		BaseURL: common.GetPointer(baseURL),
	}
	channel.SetSetting(dto.ChannelSettings{AutoRecoveryEnabled: true, AutoRecoveryIntervalMinutes: 30})
	channel.SetOtherInfo(map[string]any{"status_reason": "upstream quota exhausted", "status_time": common.GetTimestamp() - 3600})
	return channel
}

func TestRunChannelRecoveryTaskUsesConfiguredUpstream(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		gemini      bool
		multiKey    bool
		keyTarget   bool
	}{
		{name: "openai single key channel", channelType: constant.ChannelTypeOpenAI},
		{name: "oa2 openai whole channel with available keys", channelType: constant.ChannelTypeOA2, multiKey: true},
		{name: "oa2 gemini auto disabled key", channelType: constant.ChannelTypeOA2, gemini: true, multiKey: true, keyTarget: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := channelRecoveryOpenAIResponse
			if test.gemini {
				response = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
			}
			server, probes := newChannelRecoveryUpstream(t, http.StatusOK, response, nil)
			db := setupChannelRecoveryIntegrationTest(t, server)
			channel := channelRecoveryTestChannel(server.URL)
			channel.Type = test.channelType
			if test.gemini {
				channel.Models = "gemini-recovery-integration"
				channel.SetOtherSettings(dto.ChannelOtherSettings{OA2GeminiEnabled: true, OA2BaseURLGemini: server.URL})
			} else if test.channelType == constant.ChannelTypeOA2 {
				channel.SetOtherSettings(dto.ChannelOtherSettings{OA2OpenAIEnabled: true, OA2BaseURLOpenAI: server.URL})
			}
			if test.multiKey {
				channel.Key += "\nsk-recovery-other"
				channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}
				if test.keyTarget {
					channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusManuallyDisabled}
					channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: common.GetTimestamp() - 3600, 1: common.GetTimestamp() - 3600}
				}
			}
			require.NoError(t, channel.Insert())

			summary, err := runChannelRecoveryTask(context.Background(), nil)
			require.NoError(t, err)
			assert.Equal(t, channelRecoverySummary{Channels: 1, Tested: 1, Recovered: 1}, summary)
			require.Len(t, probes, 1)
			probe := <-probes
			require.NoError(t, probe.Err)
			if test.gemini {
				assert.Equal(t, "/v1beta/models/gemini-recovery-integration:generateContent", probe.Path)
				assert.Equal(t, "sk-recovery-target", probe.GoogleAPIKey)
				assert.Contains(t, probe.Body, "contents")
				assert.NotContains(t, probe.Body, "messages")
			} else {
				assert.Equal(t, "/v1/chat/completions", probe.Path)
				assert.Equal(t, "Bearer sk-recovery-target", probe.Authorization)
				assert.Equal(t, "gpt-recovery-integration", probe.Body["model"])
				assert.Contains(t, probe.Body, "messages")
			}
			var restored model.Channel
			require.NoError(t, db.First(&restored, channel.Id).Error)
			assert.Equal(t, common.ChannelStatusEnabled, restored.Status)
			assert.Equal(t, channel.Key, restored.Key)
			if test.keyTarget {
				assert.Equal(t, map[int]int{1: common.ChannelStatusManuallyDisabled}, restored.ChannelInfo.MultiKeyStatusList)
			}
			var ability model.Ability
			require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.True(t, ability.Enabled)
		})
	}
}

// 在上游返回成功前写入管理员变更，确定性复现探测结果晚于人工操作到达。
func TestRunChannelRecoveryTaskPreservesChangesDuringProbe(t *testing.T) {
	tests := []struct {
		name       string
		multiKey   bool
		keyTarget  bool
		mutation   string
		wantStatus int
	}{
		{name: "manual disable single key channel", mutation: "disable channel", wantStatus: common.ChannelStatusManuallyDisabled},
		{name: "manual disable whole multi key channel", multiKey: true, mutation: "disable channel", wantStatus: common.ChannelStatusManuallyDisabled},
		{name: "manual disable tested key", multiKey: true, keyTarget: true, mutation: "disable key", wantStatus: common.ChannelStatusAutoDisabled},
		{name: "delete channel", mutation: "delete"},
		{name: "replace single key", mutation: "replace key", wantStatus: common.ChannelStatusAutoDisabled},
		{name: "replace tested multi key", multiKey: true, keyTarget: true, mutation: "replace key", wantStatus: common.ChannelStatusAutoDisabled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var db *gorm.DB
			var channel *model.Channel
			server, probes := newChannelRecoveryUpstream(t, http.StatusOK, channelRecoveryOpenAIResponse, func() error {
				switch test.mutation {
				case "disable channel":
					if !model.UpdateChannelStatusManual(channel.Id, common.ChannelStatusManuallyDisabled, "operator disabled during recovery") {
						return fmt.Errorf("manual channel disable did not change the channel")
					}
					return nil
				case "disable key":
					info := channel.ChannelInfo
					info.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusManuallyDisabled}
					return db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("channel_info", info).Error
				case "delete":
					return channel.Delete()
				case "replace key":
					key := "sk-replacement"
					if test.multiKey {
						key += "\nsk-recovery-other"
					}
					return db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("key", key).Error
				default:
					return fmt.Errorf("unknown test mutation %q", test.mutation)
				}
			})
			db = setupChannelRecoveryIntegrationTest(t, server)
			channel = channelRecoveryTestChannel(server.URL)
			if test.multiKey {
				channel.Key += "\nsk-recovery-other"
				channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}
				if test.keyTarget {
					channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusManuallyDisabled}
					channel.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: common.GetTimestamp() - 3600}
				}
			}
			require.NoError(t, channel.Insert())

			summary, err := runChannelRecoveryTask(context.Background(), nil)
			require.NoError(t, err)
			assert.Equal(t, channelRecoverySummary{Channels: 1, Tested: 1}, summary)
			require.Len(t, probes, 1)
			probe := <-probes
			require.NoError(t, probe.Err)
			assert.Equal(t, "Bearer sk-recovery-target", probe.Authorization)

			var after model.Channel
			err = db.First(&after, channel.Id).Error
			if test.mutation == "delete" {
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				var abilityCount int64
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
				assert.Zero(t, abilityCount)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, after.Status)
			if test.mutation == "replace key" {
				expectedKey := "sk-replacement"
				if test.multiKey {
					expectedKey += "\nsk-recovery-other"
				}
				assert.Equal(t, expectedKey, after.Key)
			}
			if test.keyTarget {
				expectedStatus := common.ChannelStatusAutoDisabled
				if test.mutation == "disable key" {
					expectedStatus = common.ChannelStatusManuallyDisabled
				}
				assert.Equal(t, expectedStatus, after.ChannelInfo.MultiKeyStatusList[0])
				assert.Equal(t, common.ChannelStatusManuallyDisabled, after.ChannelInfo.MultiKeyStatusList[1])
			}
			var ability model.Ability
			require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.False(t, ability.Enabled)
		})
	}
}

func TestRunChannelRecoveryTaskUsesPersistedFailureCooldown(t *testing.T) {
	server, probes := newChannelRecoveryUpstream(t, http.StatusTooManyRequests, `{"error":{"message":"quota exhausted","type":"rate_limit_error"}}`, nil)
	db := setupChannelRecoveryIntegrationTest(t, server)
	channel := channelRecoveryTestChannel(server.URL)
	require.NoError(t, channel.Insert())

	first, err := runChannelRecoveryTask(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, channelRecoverySummary{Channels: 1, Tested: 1, Failed: 1}, first)
	require.Len(t, probes, 1)
	require.NoError(t, (<-probes).Err)
	lastTests, err := model.GetChannelRecoveryLastTests()
	require.NoError(t, err)
	targetID := model.ChannelRecoveryTargetID(channel.Key, true)
	require.Contains(t, lastTests, channel.Id)
	assert.Positive(t, lastTests[channel.Id][targetID])

	second, err := runChannelRecoveryTask(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, channelRecoverySummary{}, second)
	assert.Empty(t, probes, "a later task must observe the persisted cooldown")

	// 直接推进夹具中的冷却状态，不等待时间经过，也不依赖进程内缓存的清理。
	require.NoError(t, db.Model(&model.ChannelRecoveryState{}).
		Where("channel_id = ? AND target_key = ?", channel.Id, targetID).
		Update("last_tested_at", common.GetTimestamp()-3600).Error)
	third, err := runChannelRecoveryTask(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, channelRecoverySummary{Channels: 1, Tested: 1, Failed: 1}, third)
	require.Len(t, probes, 1)
	require.NoError(t, (<-probes).Err)
	var unchanged model.Channel
	require.NoError(t, db.First(&unchanged, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, unchanged.Status)
}

// 探测把渠道拷贝成单密钥来固定被测密钥，测试日志仍必须标明真实的密钥编号，
// 管理员才能从「模型测试」记录定位到究竟是哪一把密钥。
func TestRunChannelRecoveryTaskLogsTestedKeyIndex(t *testing.T) {
	server, probes := newChannelRecoveryUpstream(t, http.StatusOK, channelRecoveryOpenAIResponse, nil)
	db := setupChannelRecoveryIntegrationTest(t, server)
	channel := channelRecoveryTestChannel(server.URL)
	// 渠道本身可用，只有第二把密钥被自动禁用：编号必须是 1，而不是探测副本里的 0
	channel.Status = common.ChannelStatusEnabled
	channel.Key = "sk-recovery-other\nsk-recovery-target"
	channel.ChannelInfo = model.ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyStatusList:   map[int]int{1: common.ChannelStatusAutoDisabled},
		MultiKeyDisabledTime: map[int]int64{1: common.GetTimestamp() - 3600},
	}
	require.NoError(t, channel.Insert())

	summary, err := runChannelRecoveryTask(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, channelRecoverySummary{Channels: 1, Tested: 1, Recovered: 1}, summary)
	require.Len(t, probes, 1)
	probe := <-probes
	require.NoError(t, probe.Err)
	assert.Equal(t, "Bearer sk-recovery-target", probe.Authorization)

	var consumeLog model.Log
	require.NoError(t, db.Where("channel_id = ? AND type = ?", channel.Id, model.LogTypeConsume).First(&consumeLog).Error)
	other := make(map[string]any)
	require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok, "channel test logs must carry admin_info")
	assert.Equal(t, true, adminInfo["is_multi_key"])
	assert.EqualValues(t, 1, adminInfo["multi_key_index"])
}
