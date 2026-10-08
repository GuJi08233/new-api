package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预测页的实时数据：目标价取官方 TWAP 口径；价格推送合并快照与逐秒更新，按轮次与 since 只回新点；成交只收本轮的、同一笔交易
// 合并成份额最大的那条、按时间新的在前、去掉交易者信息并丢弃异常数据；关闭预测后什么都不返回。
func TestTradePredictionLiveServesTargetPriceAndTrades(t *testing.T) {
	previous := operation_setting.GetTradeSetting()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled, setting.PredictionEnabled = true, true
	operation_setting.SetTradeSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(previous) })

	now := time.Now()
	window := now.Unix() / model.TradePredictionWindow * model.TradePredictionWindow
	slug := fmt.Sprintf("btc-updown-5m-%d", window)
	var mu sync.Mutex
	openQuery := ""
	client := predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/crypto/crypto-price" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		openQuery = r.URL.RawQuery
		mu.Unlock()
		fmt.Fprint(w, `{"openPrice":80511.67582223278,"closePrice":null,"completed":false}`)
	})

	start := window * 1000
	latest := now.UnixMilli() / 1000 * 1000
	trade := func(slug, tx, side, outcome string, size, price float64, at int64) string {
		return fmt.Sprintf(`{"payload":{"proxyWallet":"0xabc","name":"someone","pseudonym":"Fond-Oyster","slug":%q,"eventSlug":%q,"transactionHash":%q,"side":%q,"outcome":%q,"size":%v,"price":%v,"timestamp":%d},"topic":"activity","type":"trades"}`,
			slug, slug, tx, side, outcome, size, price, at)
	}
	subscribed := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		select {
		case subscribed <- string(message):
		default:
		}
		for _, message := range []string{
			fmt.Sprintf(`{"payload":{"data":[{"timestamp":%d,"value":80400.5},{"timestamp":%d,"value":80450.25},{"timestamp":%d,"value":0},{"timestamp":%d,"value":80500}]},"topic":"crypto_prices_twap_sixty","type":"subscribe"}`, start-90_000, start-60_000, start-45_000, start-30_000),
			"PONG",
			fmt.Sprintf(`{"payload":{"symbol":"eth/usd","timestamp":%d,"value":2500},"topic":"crypto_prices_twap_sixty","type":"update"}`, latest),
			fmt.Sprintf(`{"payload":{"symbol":"btc/usd","timestamp":%d,"value":80510,"window_s":60},"topic":"crypto_prices_twap_sixty","type":"update"}`, latest),
			trade(slug, "0x1", "BUY", "Up", 4, 0.36, window+10),
			trade(slug, "0x1", "BUY", "Down", 6, 0.64, window+10),
			trade(slug, "0x1", "BUY", "Down", 10, 0.64, window+10),
			trade(fmt.Sprintf("btc-updown-5m-%d", window-300), "0x2", "BUY", "Down", 3, 0.5, window+11),
			trade(slug, "0x3", "BUY", "Up", 1, 1.5, window+12),
			trade(slug, "0x4", "BUY", "Maybe", 1, 0.5, window+12),
			trade(slug, "0x5", "SELL", "Down", 5.5, 0.64, window+20),
			trade(slug, "0x5", "BUY", "Down", 5.5, 0.64, window+20),
			trade(slug, "0x6", "BUY", "Up", 2, 0.4, window+15),
			fmt.Sprintf(`{"payload":{"symbol":"btc/usd","timestamp":%d,"value":80520.75,"window_s":60},"topic":"crypto_prices_twap_sixty","type":"update"}`, latest),
		} {
			if conn.WriteMessage(websocket.TextMessage, []byte(message)) != nil {
				return
			}
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	client.liveURL = "ws" + strings.TrimPrefix(feed.URL, "http")
	client.dialer = func(string) (*websocket.Dialer, error) { return websocket.DefaultDialer, nil }
	t.Cleanup(func() {
		client.live.mu.Lock()
		client.live.lastUsed = time.Time{}
		client.live.mu.Unlock()
		feed.Close()
	})

	var view TradePredictionLiveView
	require.Eventually(t, func() bool {
		view = client.liveView(0, now)
		return view.OpenPrice > 0 && view.Price == 80520.75
	}, 5*time.Second, 20*time.Millisecond)
	subscription := <-subscribed
	assert.Contains(t, subscription, `"topic":"crypto_prices_twap_sixty"`)
	assert.Contains(t, subscription, `"topic":"activity"`)
	assert.Contains(t, subscription, slug)
	assert.True(t, view.Enabled)
	assert.True(t, view.Connected)
	assert.Equal(t, window, view.WindowStart)
	assert.Equal(t, 80511.67582223278, view.OpenPrice)
	assert.Equal(t, latest, view.PriceAt)
	assert.Equal(t, []TradePredictionPricePoint{{start - 60_000, 80450.25}, {start - 30_000, 80500}, {latest, 80520.75}}, view.Points)
	assert.Equal(t, []TradePredictionTrade{
		{Side: "SELL", Outcome: model.TradePredictionDown, Size: 5.5, Price: 0.64, Time: window + 20},
		{Side: "BUY", Outcome: model.TradePredictionUp, Size: 2, Price: 0.4, Time: window + 15},
		{Side: "BUY", Outcome: model.TradePredictionDown, Size: 10, Price: 0.64, Time: window + 10},
	}, view.Trades)
	mu.Lock()
	assert.Contains(t, openQuery, "eventStartTime="+strings.ReplaceAll(time.Unix(window, 0).UTC().Format(time.RFC3339), ":", "%3A"))
	assert.Contains(t, openQuery, "twapLookbackSeconds=60")
	mu.Unlock()

	assert.Equal(t, []TradePredictionPricePoint{{latest, 80520.75}}, client.liveView(start-30_000, now).Points)

	setting.PredictionEnabled = false
	operation_setting.SetTradeSettingForTest(setting)
	disabled := client.liveView(0, now)
	assert.False(t, disabled.Enabled)
	assert.Zero(t, disabled.Price)
	assert.Empty(t, disabled.Points)
	assert.Empty(t, disabled.Trades)
}
