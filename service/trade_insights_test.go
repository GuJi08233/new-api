package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insightTestClient(server *httptest.Server) *tradeInsightClient {
	c := newTradeInsightClient()
	c.calendarURL, c.newsURL, c.leaderboardURL, c.hyperliquidURL = server.URL, server.URL, server.URL, server.URL
	c.transport = func(string) (*http.Client, error) { return server.Client(), nil }
	c.now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }
	return c
}

func insightTestSetting() *operation_setting.TradeSetting {
	s := operation_setting.DefaultTradeSetting()
	s.Enabled = true
	s.InsightsEnabled = true
	s.FuturesEnabled = true
	return s
}

func TestTradeInsightsCalendarPreservesZeroAndStaleCache(t *testing.T) {
	var failed atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "https://www.tradingview.com", r.Header.Get("Origin"))
		assert.Equal(t, "1", r.URL.Query().Get("minImportance"))
		if failed.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"result":[{"id":"42","date":"2026-10-06T13:00:00Z","country":"US","currency":"USD","title":"Employment","actual":0,"forecast":-0.5,"previous":null,"unit":"%"}]}`)
	}))
	defer server.Close()
	c := insightTestClient(server)
	var clock atomic.Int64
	clock.Store(c.now().Unix())
	c.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	view, ok := c.get(context.Background(), "calendar", insightTestSetting())
	require.True(t, ok)
	require.Equal(t, "ready", view.Status)
	rows, ok := view.Items.([]TradeCalendarEvent)
	require.True(t, ok)
	require.Len(t, rows, 1)
	assert.Equal(t, "0%", rows[0].Actual)
	assert.Equal(t, "-0.5%", rows[0].Forecast)
	assert.Empty(t, rows[0].Previous)
	clock.Add(int64(5 * time.Hour / time.Second))
	failed.Store(true)
	stale, _ := c.get(context.Background(), "calendar", insightTestSetting())
	assert.Equal(t, "stale", stale.Status)
	assert.Equal(t, "upstream_unavailable", stale.Error)
	assert.Equal(t, view.UpdatedAt, stale.UpdatedAt)
	assert.Equal(t, view.Items, stale.Items)
	c.get(context.Background(), "calendar", insightTestSetting())
	assert.EqualValues(t, 2, calls.Load(), "失败退避期间不再次打上游")
}

func TestTradeInsightsConcurrentReadersShareRefresh(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		fmt.Fprint(w, `{"result":[]}`)
	}))
	defer server.Close()
	c := insightTestClient(server)
	var wg sync.WaitGroup
	results := make(chan TradeInsightResponse, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, _ := c.get(context.Background(), "calendar", insightTestSetting())
			results <- view
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	close(results)
	for view := range results {
		assert.Equal(t, "ready", view.Status)
	}
	assert.EqualValues(t, 1, calls.Load())
}

func TestTradeInsightsNewsConfigurationAndUntrustedLinks(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "test-secret", r.Header.Get("api-key"))
		fmt.Fprint(w, `{"status":0,"data":{"data":[{"id":12,"title":"A &amp; B","content":"<p>Plain <b>text</b></p>","url":"javascript:alert(1)","create_time":"2026-10-06 20:00:00"}]}}`)
	}))
	defer server.Close()
	c := insightTestClient(server)
	c.key = func() string { return "" }
	view, _ := c.get(context.Background(), "news", insightTestSetting())
	assert.Equal(t, "disabled", view.Status)
	assert.Equal(t, "api_key_missing", view.Error)
	assert.Zero(t, calls.Load())
	c.key = func() string { return "test-secret" }
	view, _ = c.get(context.Background(), "news", insightTestSetting())
	require.Equal(t, "ready", view.Status)
	rows := view.Items.([]TradeNewsFlash)
	require.Len(t, rows, 1)
	assert.Equal(t, "A & B", rows[0].Title)
	assert.Equal(t, "Plain text", rows[0].Content)
	assert.Empty(t, rows[0].URL)
	assert.Equal(t, c.now().Unix(), rows[0].Time)
}

func TestTradeInsightsHTTPRejectsOversizeAndCredentialRedirect(t *testing.T) {
	var leaked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			fmt.Fprint(w, strings.Repeat("x", 33))
		case "/redirect":
			http.Redirect(w, r, "/sink", http.StatusFound)
		case "/sink":
			leaked.Add(1)
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	c := insightTestClient(server)
	var out any
	require.ErrorContains(t, c.request(context.Background(), "", server.URL+"/large", nil, nil, 32, &out), "too large")
	require.Error(t, c.request(context.Background(), "", server.URL+"/redirect", nil, map[string]string{"api-key": "secret"}, 100, &out))
	assert.Zero(t, leaked.Load())
}

func TestTradeInsightsWhaleSampleDistinguishesFailuresAndEmptyPositions(t *testing.T) {
	var failOne atomic.Bool
	var leaderboardCalls atomic.Int32
	failOne.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			leaderboardCalls.Add(1)
			fmt.Fprintf(w, `{"leaderboardRows":[{"ethAddress":"0x%040d","accountValue":"2000000"},{"ethAddress":"0x%040d","accountValue":"1500000"},{"ethAddress":"0x%040d","accountValue":"1500000"},{"ethAddress":"0x%040d","accountValue":"3"}]}`, 1, 2, 2, 3)
			return
		}
		var request map[string]string
		if !assert.NoError(t, common.DecodeJson(r.Body, &request)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "clearinghouseState", request["type"])
		if strings.HasSuffix(request["user"], "2") {
			if failOne.Load() {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			fmt.Fprint(w, `{"assetPositions":[]}`)
			return
		}
		fmt.Fprint(w, `{"assetPositions":[{"position":{"coin":"BTC","szi":"2","positionValue":"200000"}},{"position":{"coin":"ETH","szi":"-100","positionValue":"250000"}},{"position":{"coin":"SOL","szi":"1","positionValue":"100"}}]}`)
	}))
	defer server.Close()
	c := insightTestClient(server)
	rows, count, err := c.whales(context.Background(), "")
	require.ErrorIs(t, err, errTradeInsightPartial)
	assert.Equal(t, 1, count)
	require.Len(t, rows, 5)
	assert.Equal(t, "200000", rows[0].LongNotional)
	assert.Equal(t, 1, rows[0].LongCount)
	assert.Equal(t, "250000", rows[1].ShortNotional)
	assert.Equal(t, "0", rows[2].LongNotional)
	failOne.Store(false)
	_, count, err = c.whales(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "成功但无持仓的账户仍计入样本")
	assert.EqualValues(t, 1, leaderboardCalls.Load(), "仓位刷新复用当天地址池，避免重复下载大榜单")
}

func TestTradeInsightsMalformedResponseIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"error":"credentials secret"}`) }))
	defer server.Close()
	c := insightTestClient(server)
	view, _ := c.get(context.Background(), "calendar", insightTestSetting())
	assert.Equal(t, "unavailable", view.Status)
	assert.Equal(t, "upstream_unavailable", view.Error)
	assert.Zero(t, view.UpdatedAt)
	assert.Empty(t, view.Items)
}

func TestTradeInsightsLiquidationsUseExecutedSnapshotQuantity(t *testing.T) {
	now := time.Now()
	f := &tradeLiquidationFeed{}
	frame := fmt.Sprintf(`{"e":"forceOrder","E":%d,"o":{"s":"BTCUSDT","S":"SELL","q":"10","p":"100","ap":"99","z":"2","T":%d}}`, now.UnixMilli(), now.UnixMilli())
	require.NoError(t, f.record([]byte(frame), now))
	require.NoError(t, f.record([]byte(frame), now))
	view := f.view()
	rows := view.Items.([]TradeLiquidationSnapshot)
	require.Len(t, rows, 1)
	assert.Equal(t, "long", rows[0].Side)
	assert.Equal(t, "2", rows[0].Quantity)
	assert.Equal(t, "198", rows[0].Notional)
	assert.Equal(t, "binance_usdm_snapshots", view.Coverage)
	assert.Equal(t, "stale", view.Status)
	require.Error(t, f.record([]byte(strings.Replace(frame, `"z":"2"`, `"z":"1e100000"`, 1)), now))
	require.NoError(t, f.record([]byte(strings.Replace(frame, `"S":"SELL"`, `"S":"BUY"`, 1)), now))
	assert.Equal(t, "short", f.view().Items.([]TradeLiquidationSnapshot)[0].Side)
	coinMargined := strings.Replace(frame, `"e":"forceOrder"`, `"e":"forceOrder","st":2`, 1)
	coinMargined = strings.Replace(coinMargined, "BTCUSDT", "BTCUSD_PERP", 1)
	require.NoError(t, f.record([]byte(coinMargined), now))
	assert.Len(t, f.view().Items.([]TradeLiquidationSnapshot), 2, "币本位张数不能计入 USD-M 快照")
}

func TestTradeInsightsLiquidationWebsocketUsesLocalStream(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/market/ws/!forceOrder@arr", r.URL.Path)
		ws, err := upgrader.Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer ws.Close()
		frame := fmt.Sprintf(`{"e":"forceOrder","E":%d,"o":{"s":"ETHUSDT","S":"BUY","ap":"2000","z":"1","T":%d}}`, time.Now().UnixMilli(), time.Now().UnixMilli())
		assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(frame)))
	}))
	defer server.Close()
	f := &tradeLiquidationFeed{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, f.session(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), ""))
	rows := f.view().Items.([]TradeLiquidationSnapshot)
	require.Len(t, rows, 1)
	assert.Equal(t, "ETHUSDT", rows[0].Symbol)
}
