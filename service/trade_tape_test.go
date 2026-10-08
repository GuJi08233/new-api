package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/binance"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 最新成交：页面在看时才连成交推送，几个页面共用一条连接；连上先用 REST 补最近的成交，之后推增量；补回来的成交比已推的还早时
// 整份重推；新来的页面直接拿到整份；没人看以后断开。
func TestTradeTapeStreamsRecentTradesToViewers(t *testing.T) {
	var mu sync.Mutex
	restTrades := `[{"a":100,"p":"80000.10","q":"0.5","f":1,"l":1,"T":1791482992000,"m":true,"M":true},` +
		`{"a":101,"p":"80000.20","q":"0.25","f":2,"l":2,"T":1791482992100,"m":false,"M":true}]`
	var restGate chan struct{}
	dials := 0
	streams := ""
	conns := make(chan *websocket.Conn, 4)
	closed := make(chan struct{}, 4)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/aggTrades":
			mu.Lock()
			body, gate := restTrades, restGate
			mu.Unlock()
			if gate != nil {
				<-gate
			}
			fmt.Fprint(w, body)
		case "/stream":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			mu.Lock()
			dials++
			streams = r.URL.Query().Get("streams")
			mu.Unlock()
			conns <- conn
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					closed <- struct{}{}
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	m := newTradeMarket(false)
	m.generation = 1
	m.config = tradeMarketConfig{RestURL: server.URL, WsURL: "ws" + strings.TrimPrefix(server.URL, "http"), Symbols: []string{"BTCUSDT"}}
	m.client = binance.NewClient(server.URL, server.Client())
	t.Cleanup(func() {
		m.tapeMu.Lock()
		for _, tape := range m.tapes {
			if tape.cancel != nil {
				tape.cancel()
			}
		}
		m.tapeMu.Unlock()
	})

	// nextTrades 推一轮，返回 sub 收到的下一个最新成交事件(跳过行情状态等)。
	nextTrades := func(sub *TradeSubscriber) tradeTradesEvent {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			m.pushDirty()
			select {
			case event := <-sub.Events:
				if event.Name != "trades" {
					continue
				}
				var trades tradeTradesEvent
				require.NoError(t, common.Unmarshal(event.Data, &trades))
				return trades
			case <-time.After(20 * time.Millisecond):
			}
		}
		require.FailNow(t, "no trades event within 5s")
		return tradeTradesEvent{}
	}
	trade := func(id int64, price, qty string, at int64, buyerMaker bool) tradeTapeTrade {
		return tradeTapeTrade{ID: id, Price: price, Qty: qty, Time: at, BuyerMaker: buyerMaker}
	}

	first := m.Subscribe([]string{"BTCUSDT"}, "", "", "BTCUSDT")
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Reset: true, Trades: []tradeTapeTrade{}}, nextTrades(first))
	conn := <-conns
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Trades: []tradeTapeTrade{
		trade(100, "80000.1", "0.5", 1791482992000, true), trade(101, "80000.2", "0.25", 1791482992100, false),
	}}, nextTrades(first))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1791482993000,`+
		`"s":"BTCUSDT","a":102,"p":"80001","q":"1","f":3,"l":4,"T":1791482993000,"m":false,"M":true}}`)))
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Trades: []tradeTapeTrade{trade(102, "80001", "1", 1791482993000, false)}}, nextTrades(first))

	second := m.Subscribe([]string{"BTCUSDT"}, "", "", "BTCUSDT")
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Reset: true, Trades: []tradeTapeTrade{
		trade(100, "80000.1", "0.5", 1791482992000, true), trade(101, "80000.2", "0.25", 1791482992100, false),
		trade(102, "80001", "1", 1791482993000, false),
	}}, nextTrades(second))

	// 断线重连：连上后先推来一笔新成交，REST 补回的断线期间的成交晚到、比已有的最新一笔早，列表不能只往后追加，整份重推。
	gate := make(chan struct{})
	mu.Lock()
	restTrades = `[{"a":102,"p":"80001","q":"1","f":3,"l":4,"T":1791482993000,"m":false},` +
		`{"a":103,"p":"80002","q":"2","f":5,"l":5,"T":1791482994000,"m":true},` +
		`{"a":104,"p":"80003","q":"3","f":6,"l":6,"T":1791482995000,"m":false}]`
	restGate = gate
	mu.Unlock()
	require.NoError(t, conn.Close())
	conn = <-conns
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1791482996000,`+
		`"s":"BTCUSDT","a":105,"p":"80004","q":"4","f":7,"l":7,"T":1791482996000,"m":true,"M":true}}`)))
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Trades: []tradeTapeTrade{trade(105, "80004", "4", 1791482996000, true)}}, nextTrades(first))
	close(gate)
	assert.Equal(t, tradeTradesEvent{Symbol: "BTCUSDT", Reset: true, Trades: []tradeTapeTrade{
		trade(100, "80000.1", "0.5", 1791482992000, true), trade(101, "80000.2", "0.25", 1791482992100, false),
		trade(102, "80001", "1", 1791482993000, false), trade(103, "80002", "2", 1791482994000, true),
		trade(104, "80003", "3", 1791482995000, false), trade(105, "80004", "4", 1791482996000, true),
	}}, nextTrades(first))
	mu.Lock()
	assert.Equal(t, 2, dials, "two pages share one connection; the second dial is the reconnect")
	assert.Equal(t, "btcusdt@aggTrade", streams)
	mu.Unlock()

	m.Unsubscribe(first)
	m.Unsubscribe(second)
	m.tapeMu.Lock()
	m.tapes["BTCUSDT"].idleSince = time.Now().Add(-tradeTapeLinger)
	m.tapeMu.Unlock()
	m.pruneTapes()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the trade stream stayed open after the last viewer left")
	}
	m.tapeMu.Lock()
	assert.Empty(t, m.tapes)
	m.tapeMu.Unlock()
}
