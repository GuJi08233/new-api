package controller

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyHandlersUseChannelConnectionSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldPrices := ratio_setting.ModelPrice2JSONString()
	oldGroups := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
	})
	// 免费测试模型使提交路径完整执行，同时不引入与代理无关的扣费操作。
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0,"mj_upscale":0}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	for _, endpoint := range []string{"submit", "seed", "followup", "poll"} {
		for _, disableKeepAlive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/disableKeepAlive=%v", endpoint, disableKeepAlive), func(t *testing.T) {
				db := setupMediaProxyTestDB(t)
				service.InitHttpClient()
				service.ResetProxyClientCache()
				t.Cleanup(service.GetHttpClient().CloseIdleConnections)
				t.Cleanup(service.ResetProxyClientCache)
				var directRequests, proxyRequests, proxyConnections atomic.Int64
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					directRequests.Add(1)
					http.Error(w, "channel proxy was bypassed", http.StatusBadGateway)
				}))
				t.Cleanup(origin.Close)
				submitTime := time.Now().UnixMilli()
				proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyRequests.Add(1)
					assert.True(t, r.URL.IsAbs(), "HTTP 代理应收到完整上游 URL")
					assert.Equal(t, "origin-secret", r.Header.Get("mj-api-secret"))
					w.Header().Set("Content-Type", "application/json")
					if endpoint == "poll" {
						assert.Equal(t, "/mj/task/list-by-condition", r.URL.Path)
						var body struct {
							IDs []string `json:"ids"`
						}
						if assert.NoError(t, common.DecodeJson(r.Body, &body)) {
							assert.Equal(t, []string{"origin-task"}, body.IDs)
						}
						_, _ = fmt.Fprintf(w, `[{"id":"origin-task","status":"IN_PROGRESS","progress":"50%%","submitTime":%d}]`, submitTime)
						return
					}
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = io.WriteString(w, `{"code":1,"result":"upstream-result"}`)
				}))
				proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						proxyConnections.Add(1)
					}
				}
				proxy.Start()
				t.Cleanup(proxy.Close)
				channelSettings := dto.ChannelSettings{Proxy: proxy.URL, DisableKeepAlive: disableKeepAlive}
				channel := &model.Channel{
					Name: "midjourney", Type: constant.ChannelTypeMidjourney, Status: common.ChannelStatusEnabled,
					Key: "origin-secret", BaseURL: &origin.URL,
				}
				channel.SetSetting(channelSettings)
				require.NoError(t, db.Create(channel).Error)
				require.NoError(t, db.Create(&model.User{Id: 7, Username: "mj-user", Group: "default", Quota: 1000000}).Error)
				task := &model.Midjourney{
					MjId: "origin-task", UserId: 7, ChannelId: channel.Id,
					Status: "SUCCESS", Progress: "100%", SubmitTime: submitTime,
				}
				if endpoint == "poll" {
					task.Status, task.Progress = "IN_PROGRESS", "0%"
				}
				require.NoError(t, db.Create(task).Error)

				for i := 0; i < 3; i++ {
					if endpoint == "poll" {
						summary := runMidjourneyTaskUpdateOnce(context.Background(), nil)
						assert.Equal(t, 1, summary.ChannelsScanned)
						continue
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Set("id", 7)
					c.Set("base_url", origin.URL)
					c.Set("channel_id", channel.Id)
					common.SetContextKey(c, constant.ContextKeyChannelKey, channel.Key)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSettings)
					if endpoint != "submit" {
						// seed/派生任务必须使用数据库中的原任务渠道，而非当前选中的渠道。
						common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{Proxy: "invalid://selected-channel"})
					}
					if endpoint == "seed" {
						c.Params = gin.Params{{Key: "id", Value: task.MjId}}
						c.Request = httptest.NewRequest(http.MethodGet, "/mj/task/origin-task/image-seed", nil)
						require.Nil(t, relay.RelayMidjourneyTaskImageSeed(c))
					} else {
						info := &relaycommon.RelayInfo{
							UserId: 7, UserGroup: "default", UsingGroup: "default", StartTime: time.Now(),
							OriginModelName: "mj_imagine", RelayMode: relayconstant.RelayModeMidjourneyImagine,
						}
						c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"cat"}`))
						if endpoint == "followup" {
							info.OriginModelName, info.RelayMode = "mj_upscale", relayconstant.RelayModeMidjourneyChange
							c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", strings.NewReader(`{"taskId":"origin-task","action":"UPSCALE","index":1}`))
						}
						c.Request.Header.Set("Content-Type", "application/json")
						require.Nil(t, relay.RelayMidjourneySubmit(c, info))
					}
					assert.Equal(t, http.StatusOK, rec.Code)
					var response dto.MidjourneyResponse
					require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &response))
					assert.Equal(t, 1, response.Code)
					assert.Equal(t, "upstream-result", response.Result)
				}
				if endpoint == "poll" {
					require.NoError(t, db.First(task, task.Id).Error)
					assert.Equal(t, "50%", task.Progress)
				}
				assert.Zero(t, directRequests.Load())
				assert.Equal(t, int64(3), proxyRequests.Load())
				wantConnections := int64(1)
				if disableKeepAlive {
					wantConnections = 3
				}
				assert.Equal(t, wantConnections, proxyConnections.Load())
			})
		}
	}
}
