package relay

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 放大/变换这类任务会换回原任务的渠道，绑定在该渠道上的客户端 UA 规则必须在换渠道后补判。
func TestRelayMidjourneySubmitRejectsBoundChannelClientUaRule(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousDB, previousReadDB := model.DB, model.RO_DB
	t.Cleanup(func() { model.DB, model.RO_DB = previousDB, previousReadDB })
	t.Cleanup(operation_setting.SetClientUaRuleSettingForTest([]operation_setting.ClientUaRule{
		{Name: "channel-9-cli-only", ChannelIDs: []int{9}, UaAllow: []string{"claude-cli/"}},
	}))

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mj-client-ua.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.DB, model.RO_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Midjourney{}, &model.RiskEvent{}))

	task := &model.Midjourney{UserId: 1, MjId: "task-1", ChannelId: 9, Status: "SUCCESS", Prompt: "a cat"}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 9, Name: "mj-origin", Key: "sk-origin", Status: common.ChannelStatusEnabled, Group: "default",
		Type: constant.ChannelTypeMidjourney,
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := `{"taskId":"task-1","action":"UPSCALE","index":1}`
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("User-Agent", "Claude-Desktop/0.14.10")
	storage, err := common.CreateBodyStorage([]byte(body))
	require.NoError(t, err)
	ctx.Set(common.KeyBodyStorage, storage)
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	ctx.Set("id", 1)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeMidjourneyChange,
		OriginModelName: "mj_imagine",
		UserId:          1,
	}
	resp := RelayMidjourneySubmit(ctx, info)

	require.NotNil(t, resp)
	assert.NotEqual(t, 1, resp.Code)
	assert.Contains(t, resp.Description, "channel-9-cli-only")
}
