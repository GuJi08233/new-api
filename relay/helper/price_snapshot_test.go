package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type pricingSnapshotRequestBody struct {
	entered chan struct{}
	release chan struct{}
	read    bool
}

func (b *pricingSnapshotRequestBody) Read(data []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	b.read = true
	close(b.entered)
	<-b.release
	return copy(data, "{}"), nil
}

func (*pricingSnapshotRequestBody) Close() error { return nil }

func TestModelPriceHelperKeepsOneSnapshotDuringPricingUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousQuota := model.DB, common.QuotaPerUnit
	previousConfig := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		previousConfig[key] = value
		return nil
	}))
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	model.DB = db
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.QuotaPerUnit = previousQuota
		require.NoError(t, config.GlobalConfig.LoadFromDB(previousConfig))
		common.OptionMapRWMutex.Unlock()
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		"billing_setting.billing_mode": `{"snapshot-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"snapshot-model":"tier(\"old\", p * 2)"}`,
		"QuotaPerUnit":                 "500000",
	}))

	body := &pricingSnapshotRequestBody{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(body.release) }) }
	defer release()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	ctx.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{OriginModelName: "snapshot-model", UserGroup: "default", UsingGroup: "default"}
	type pricingResult struct {
		data types.PriceData
		err  error
	}
	result := make(chan pricingResult, 1)
	go func() {
		data, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
		result <- pricingResult{data: data, err: err}
	}()
	<-body.entered

	// 读取表达式后尚未换算额度时，配置发布不能取得写权限，否则会混用新旧单价。
	canPublish := common.OptionMapRWMutex.TryLock()
	if canPublish {
		common.OptionMapRWMutex.Unlock()
	}
	assert.False(t, canPublish, "预扣计算必须把表达式和额度换算参数读成一个快照")
	updated := make(chan error, 1)
	go func() {
		updated <- model.UpdateOptionsBulk(map[string]string{
			"billing_setting.billing_mode": `{"snapshot-model":"tiered_expr"}`,
			"billing_setting.billing_expr": `{"snapshot-model":"tier(\"new\", p * 4)"}`,
			"QuotaPerUnit":                 "1000000",
		})
	}()
	release()
	oldResult := <-result
	require.NoError(t, oldResult.err)
	assert.Equal(t, 1000, oldResult.data.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.Equal(t, "old", info.TieredBillingSnapshot.EstimatedTier)
	require.NoError(t, <-updated)

	info = &relaycommon.RelayInfo{
		OriginModelName: "snapshot-model", UserGroup: "default", UsingGroup: "default",
		BillingRequestInput: &billingexpr.RequestInput{},
	}
	newResult, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 4000, newResult.QuotaToPreConsume)
	assert.Equal(t, "new", info.TieredBillingSnapshot.EstimatedTier)
	assert.True(t, HasModelBillingConfig("snapshot-model"))
}
