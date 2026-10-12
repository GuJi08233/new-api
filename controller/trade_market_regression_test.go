package controller

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 停用现货币对后仍保留本人的持仓、借币和挂单；别人的资产不会开放自己的交易页。
func TestTradeMarketKeepsUserExposureAfterSymbolDisabled(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, gameSetting(nil))
	require.NoError(t, env.db.AutoMigrate(&model.TradePosition{}, &model.TradeSpotAssetLoan{}, &model.TradeOrder{}))
	previous := operation_setting.GetTradeSetting()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled, setting.Symbols = true, []string{"ETHUSDT"}
	operation_setting.SetTradeSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(previous) })
	env.router.GET("/api/trade/market", middleware.UserAuth(), GetTradeMarket)
	env.router.GET("/api/trade/klines", middleware.UserAuth(), GetTradeKlines)
	player, other := env.player(0, gameTestIpA), env.player(0, gameTestIpB)
	require.NoError(t, env.db.Create(&model.TradePosition{UserId: player.user.Id, Symbol: "BTCUSDT", Qty: 100_000_000}).Error)
	require.NoError(t, env.db.Create(&model.TradeSpotAssetLoan{UserId: player.user.Id, Symbol: "NVDABUSDT", Interest: 100}).Error)
	require.NoError(t, env.db.Create(&model.TradeOrder{UserId: player.user.Id, Symbol: "TSLABUSDT", Status: model.TradeOrderStatusOpen}).Error)
	require.NoError(t, env.db.Create(&model.TradePosition{UserId: other.user.Id, Symbol: "AMDBUSDT", Qty: 100_000_000}).Error)
	for _, enabled := range []bool{true, false} {
		updated := setting.Clone()
		updated.Enabled = enabled
		operation_setting.SetTradeSettingForTest(updated)
		view := player.ok(http.MethodGet, "/api/trade/market", nil)
		items, ok := view["symbols"].([]any)
		require.True(t, ok)
		got := map[string]bool{}
		for _, raw := range items {
			item := raw.(map[string]any)
			got[item["symbol"].(string)] = item["open"].(bool)
		}
		assert.Equal(t, map[string]bool{"ETHUSDT": enabled, "BTCUSDT": false, "NVDABUSDT": false, "TSLABUSDT": false}, got)
	}
	// 没有外部行情连接：自己的停用币对会进入行情查询，别人的则在访问检查处被拒绝。
	message := player.fails(http.MethodGet, "/api/trade/klines?symbol=BTCUSDT&interval=1m", nil)
	assert.Equal(t, gameMessage(i18n.MsgTradeMarketUnavailable), message)
	message = player.fails(http.MethodGet, "/api/trade/klines?symbol=AMDBUSDT&interval=1m", nil)
	assert.Equal(t, gameMessage(i18n.MsgTradeSymbolClosed), message)
}

// 行情 SSE 同样允许订阅仍持有的停用币对，让卖出页保持盘口推送。
func TestTradeStreamKeepsDisabledHeldSpotBook(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(nil))
	require.NoError(t, env.db.AutoMigrate(&model.TradePosition{}, &model.TradeSpotAssetLoan{}, &model.TradeOrder{}))
	previous := operation_setting.GetTradeSetting()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled, setting.Symbols = false, nil
	operation_setting.SetTradeSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(previous) })
	player := env.player(0, gameTestIpA)
	require.NoError(t, env.db.Create(&model.TradePosition{UserId: player.user.Id, Symbol: "BTCUSDT", Qty: 1}).Error)
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) {
		c.Set("id", player.user.Id)
		StreamTradeMarket(c)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream?symbols=BTCUSDT&book=BTCUSDT", nil)
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if scanner.Text() != "event: book" {
			continue
		}
		require.True(t, scanner.Scan())
		var book struct {
			Symbol string `json:"s"`
		}
		require.NoError(t, common.UnmarshalJsonStr(scanner.Text()[len("data: "):], &book))
		assert.Equal(t, "BTCUSDT", book.Symbol)
		return
	}
	require.NoError(t, scanner.Err())
	t.Fatal("held spot book was not streamed")
}
