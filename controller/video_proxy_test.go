package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 视频内容按任务所属用户鉴权后才返回。响应只能留在用户自己的浏览器缓存里：
// public 会让 CDN 等共享缓存按 URL 保存，之后同一地址不经鉴权就能取到别人的视频。
// 上游自带的 public 缓存头也不能透传给客户端。
func TestVideoProxyResponsesArePrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		_, _ = w.Write([]byte("upstream-video"))
	}))
	t.Cleanup(upstream.Close)

	// 测试上游在 127.0.0.1 上，SSRF 防护会拦截，这里临时关闭。
	fetchSetting := system_setting.GetFetchSetting()
	oldSSRF := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	oldDB, oldReadDB, oldMemoryCache := model.DB, model.RO_DB, common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "video.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	model.DB, model.RO_DB, common.MemoryCacheEnabled = db, nil, false
	t.Cleanup(func() {
		fetchSetting.EnableSSRFProtection = oldSSRF
		model.DB, model.RO_DB, common.MemoryCacheEnabled = oldDB, oldReadDB, oldMemoryCache
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeKling, Key: "key", Name: "kling"}).Error)

	router := gin.New()
	router.GET("/v1/videos/:task_id/content", func(c *gin.Context) {
		c.Set("id", 7)
		VideoProxy(c)
	})

	tests := []struct {
		name      string
		taskID    string
		resultURL string
		wantBody  string
	}{
		{"上游视频", "task_upstream", upstream.URL + "/video.mp4", "upstream-video"},
		{"data URL", "task_inline", "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("inline-video")), "inline-video"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Create(&model.Task{
				TaskID:      tc.taskID,
				UserId:      7,
				ChannelId:   1,
				Status:      model.TaskStatusSuccess,
				PrivateData: model.TaskPrivateData{ResultURL: tc.resultURL},
			}).Error)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/videos/"+tc.taskID+"/content", nil))

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, tc.wantBody, rec.Body.String())
			assert.Equal(t, "private, max-age=86400", rec.Header().Get("Cache-Control"))
		})
	}
}
