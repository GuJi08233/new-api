package controller

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
	"gorm.io/gorm"
)

func setupMediaProxyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldRODB := model.DB, model.RO_DB
	oldMemoryCache, oldRedis := common.MemoryCacheEnabled, common.RedisEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "media.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB, model.RO_DB = db, nil
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, oldLogType)
	t.Cleanup(func() {
		model.DB, model.RO_DB = oldDB, oldRODB
		common.MemoryCacheEnabled, common.RedisEnabled = oldMemoryCache, oldRedis
		common.SetDatabaseTypes(oldMainType, oldLogType)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Task{}, &model.Midjourney{}, &model.User{}))
	return db
}

// DNS 首次校验返回公网 IP，之后返回回环地址；所有 DNS 和 HTTP 流量都留在本机。
func useRebindingDNS(t *testing.T) *atomic.Int64 {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	var queries atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			n, addr, err := listener.ReadFrom(buffer)
			if err != nil {
				return
			}
			var request dnsmessage.Message
			if !assert.NoError(t, request.Unpack(buffer[:n])) {
				return
			}
			response := dnsmessage.Message{
				Header: dnsmessage.Header{
					ID: request.ID, Response: true, RecursionDesired: true, RecursionAvailable: true,
				},
				Questions: request.Questions,
			}
			for _, question := range request.Questions {
				if question.Type != dnsmessage.TypeA {
					continue
				}
				address := [4]byte{93, 184, 216, 34}
				if queries.Add(1) > 1 {
					address = [4]byte{127, 0, 0, 1}
				}
				response.Answers = append(response.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
					Body:   &dnsmessage.AResource{A: address},
				})
			}
			packet, err := response.Pack()
			if !assert.NoError(t, err) {
				return
			}
			if _, err = listener.WriteTo(packet, addr); !assert.NoError(t, err) {
				return
			}
		}
	}()
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, listener.LocalAddr().String())
		},
	}
	t.Cleanup(func() {
		net.DefaultResolver = oldResolver
		require.NoError(t, listener.Close())
		<-done
		service.GetSSRFProtectedHTTPClient().CloseIdleConnections()
		service.GetHttpClient().CloseIdleConnections()
		service.ResetProxyClientCache()
		service.InitHttpClient()
	})
	service.InitHttpClient()
	return &queries
}

func TestMediaProxyBlocksRebindingWithLegacyKeepAliveSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupMediaProxyTestDB(t)
	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = oldFetchSetting })
	*fetchSetting = system_setting.FetchSetting{EnableSSRFProtection: true, ApplyIPFilterForDomain: true}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		t.Setenv(key, "")
	}
	t.Setenv("NO_PROXY", "*")

	router := gin.New()
	router.GET("/video/:task_id", func(c *gin.Context) {
		c.Set("id", 7)
		VideoProxy(c)
	})
	router.GET("/mj/image/:id", relay.RelayMidjourneyImage)
	for _, channelSetting := range []struct {
		name     string
		settings dto.ChannelSettings
	}{
		{"default", dto.ChannelSettings{}},
		{"legacy", dto.ChannelSettings{DisableKeepAlive: true}},
		{"legacy-whitespace", dto.ChannelSettings{Proxy: " \t", DisableKeepAlive: true}},
	} {
		for _, media := range []string{"video", "image"} {
			t.Run(channelSetting.name+"/"+media, func(t *testing.T) {
				queries := useRebindingDNS(t)
				var privateRequests atomic.Int64
				privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					privateRequests.Add(1)
					_, _ = io.WriteString(w, "private-content")
				}))
				t.Cleanup(privateServer.Close)
				_, port, err := net.SplitHostPort(privateServer.Listener.Addr().String())
				require.NoError(t, err)
				fetchSetting.AllowedPorts = []string{port}
				mediaURL := "http://media-rebind.test:" + port + "/media"
				channel := &model.Channel{Name: "media", Type: constant.ChannelTypeKling}
				channel.SetSetting(channelSetting.settings)
				require.NoError(t, db.Create(channel).Error)
				var path string
				wantStatus := http.StatusBadGateway
				if media == "video" {
					task := &model.Task{
						TaskID: channelSetting.name, UserId: 7, ChannelId: channel.Id, Status: model.TaskStatusSuccess,
						PrivateData: model.TaskPrivateData{ResultURL: mediaURL},
					}
					require.NoError(t, db.Create(task).Error)
					path = "/video/" + url.PathEscape(task.TaskID)
				} else {
					task := &model.Midjourney{UserId: 7, ChannelId: channel.Id, ImageUrl: mediaURL}
					require.NoError(t, db.Create(task).Error)
					imageURL, err := url.Parse(setting.MjForwardImageURL(task.Id))
					require.NoError(t, err)
					path = imageURL.RequestURI()
					wantStatus = http.StatusInternalServerError
				}

				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

				assert.Equal(t, wantStatus, rec.Code, rec.Body.String())
				assert.Zero(t, privateRequests.Load(), "媒体下载不能访问重绑定后的内网地址")
				assert.GreaterOrEqual(t, queries.Load(), int64(2))
			})
		}
	}
}
