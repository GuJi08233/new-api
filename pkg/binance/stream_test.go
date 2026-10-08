package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamServer 是测试用的组合流服务端。每条连接升级之后交给测试去推帧、断开，服务端自己在后台一直读，
// 这样 gorilla 才会回 pong。
type streamServer struct {
	url   string
	conns chan *serverConn
}

type serverConn struct {
	ws      *websocket.Conn
	streams string        // 请求里的 streams 参数
	done    chan struct{} // 服务端读到连接断开时关闭
}

// newStreamServer 起服务端；configure 在每条连接开始读之前调用，用来换掉 ping 处理。
func newStreamServer(t *testing.T, configure func(ws *websocket.Conn)) *streamServer {
	server := &streamServer{conns: make(chan *serverConn, 16)}
	var upgrader websocket.Upgrader
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream" {
			http.NotFound(w, r)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if configure != nil {
			configure(ws)
		}
		conn := &serverConn{ws: ws, streams: r.URL.Query().Get("streams"), done: make(chan struct{})}
		server.conns <- conn
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				_ = ws.Close()
				close(conn.done)
				return
			}
		}
	}))
	t.Cleanup(httpServer.Close)
	server.url = "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/"
	return server
}

func (s *streamServer) accept(t *testing.T) *serverConn {
	t.Helper()
	select {
	case conn := <-s.conns:
		return conn
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the stream did not connect")
		return nil
	}
}

func (c *serverConn) send(t *testing.T, frame string) {
	t.Helper()
	require.NoError(t, c.ws.WriteMessage(websocket.TextMessage, []byte(frame)))
}

// recorder 按顺序记下 handler 收到的回调。
type recorder struct {
	events chan any
}

type depthEvent struct {
	symbol string
	depth  *Depth
}

type tickerEvent struct {
	ticker *Ticker
}

type klineEvent struct {
	symbol string
	kline  *Kline
	closed bool
}

type markEvent struct {
	mark *MarkPrice
}

type aggTradeEvent struct {
	symbol string
	trade  *AggTrade
}

type connectedEvent bool

func newRecorder() *recorder {
	return &recorder{events: make(chan any, 64)}
}

func (r *recorder) OnDepth(symbol string, depth *Depth, _ time.Time) {
	r.events <- depthEvent{symbol: symbol, depth: depth}
}

func (r *recorder) OnTicker(ticker *Ticker, _ time.Time) {
	r.events <- tickerEvent{ticker: ticker}
}

func (r *recorder) OnKline(symbol string, kline *Kline, closed bool, _ time.Time) {
	r.events <- klineEvent{symbol: symbol, kline: kline, closed: closed}
}

func (r *recorder) OnMarkPrice(mark *MarkPrice, _ time.Time) {
	r.events <- markEvent{mark: mark}
}

func (r *recorder) OnAggTrade(symbol string, trade *AggTrade, _ time.Time) {
	r.events <- aggTradeEvent{symbol: symbol, trade: trade}
}

func (r *recorder) OnConnected(connected bool) {
	r.events <- connectedEvent(connected)
}

func (r *recorder) next(t *testing.T) any {
	t.Helper()
	select {
	case event := <-r.events:
		return event
	case <-time.After(2 * time.Second):
		require.FailNow(t, "no stream event within 2s")
		return nil
	}
}

// newTestStream 订阅 BTCUSDT 与 NVDABUSDT 的 5 档盘口、迷你行情与 1 分钟 K 线。退避改短；心跳改成一小时，
// 测试里的 ping 都由 Sync 发出。
func newTestStream(server *streamServer, events *recorder) *Stream {
	s := NewStream(StreamConfig{
		BaseURL:       server.url,
		Symbols:       []string{"BTCUSDT", "NVDABUSDT"},
		DepthLevels:   5,
		Ticker:        true,
		KlineInterval: "1m",
	}, events)
	s.backoff = []time.Duration{10 * time.Millisecond}
	s.keepalive = time.Hour
	s.pingGap = 50 * time.Millisecond
	return s
}

// runStream 在后台跑 s.Run。返回的 stop 取消 ctx 并确认 Run 已经返回，测试结束时也会调用。
func runStream(t *testing.T, s *Stream) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			assert.Fail(t, "Run did not return after its ctx was canceled")
		}
	}
	t.Cleanup(stop)
	return stop
}

func TestStreamDeliversFramesSyncsAndReconnects(t *testing.T) {
	server := newStreamServer(t, nil)
	events := newRecorder()
	s := newTestStream(server, events)
	stop := runStream(t, s)

	first := server.accept(t)
	assert.Equal(t, "btcusdt@depth5@100ms/btcusdt@miniTicker/btcusdt@kline_1m/nvdabusdt@depth5@100ms/nvdabusdt@miniTicker/nvdabusdt@kline_1m", first.streams)
	require.Equal(t, connectedEvent(true), events.next(t))
	assert.True(t, s.Connected())

	// 格式不对的帧跳过(缺了 asks 的盘口也不能当成空盘口交出去)，连接不断，后面的帧照常处理。
	// 推送的格式照抄 Binance 文档，包括只差大小写的字段。
	first.send(t, `{"stream":"btcusdt@depth5@100ms","data":{"lastUpdateId":1,"bids":[["not-a-number","1"]],"asks":[]}}`)
	first.send(t, `{"stream":"btcusdt@depth5@100ms","data":{"lastUpdateId":2,"bids":[["65000","1"]]}}`)
	first.send(t, `{"stream":"nvdabusdt@depth5@100ms","data":{"lastUpdateId":42,"bids":[["181.20","0.5"],["181.10","2"]],"asks":[["181.30","1.25"],["181.40","3"]]}}`)
	first.send(t, `{"stream":"btcusdt@miniTicker","data":{"e":"24hrMiniTicker","E":1672515782136,"s":"BTCUSDT","c":"65000.10","o":"64000","h":"65500","l":"63900.5","v":"1234.5","q":"80000000.25"}}`)
	first.send(t, `{"stream":"btcusdt@kline_1m","data":{"e":"kline","E":1672515782136,"s":"BTCUSDT","k":{"t":1672515780000,"T":1672515839999,"s":"BTCUSDT","i":"1m","f":100,"L":200,`+
		`"o":"0.0010","c":"0.0020","h":"0.0025","l":"0.0015","v":"1000","n":100,"x":true,"q":"1.0000","V":"500","Q":"0.500","B":"123456"}}}`)

	assert.Equal(t, depthEvent{symbol: "NVDABUSDT", depth: &Depth{
		LastUpdateID: 42,
		Bids:         []Level{{Price: d("181.20"), Qty: d("0.5")}, {Price: d("181.10"), Qty: d("2")}},
		Asks:         []Level{{Price: d("181.30"), Qty: d("1.25")}, {Price: d("181.40"), Qty: d("3")}},
	}}, events.next(t))
	assert.Equal(t, tickerEvent{ticker: &Ticker{Symbol: "BTCUSDT", EventTime: 1672515782136, Close: d("65000.10"), Open: d("64000"),
		High: d("65500"), Low: d("63900.5"), Volume: d("1234.5"), QuoteVolume: d("80000000.25")}}, events.next(t))
	assert.Equal(t, klineEvent{symbol: "BTCUSDT", kline: &Kline{OpenTime: 1672515780000, Open: d("0.0010"), High: d("0.0025"), Low: d("0.0015"),
		Close: d("0.0020"), Volume: d("1000"), CloseTime: 1672515839999, QuoteVolume: d("1.0000"), Trades: 100}, closed: true}, events.next(t))

	// Sync 返回时，服务端在收到 ping 之前推出的帧已经交给了 handler。
	first.send(t, `{"stream":"btcusdt@depth5@100ms","data":{"lastUpdateId":43,"bids":[["65000","1"]],"asks":[["65000.1","2"]]}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Sync(ctx))
	select {
	case event := <-events.events:
		assert.Equal(t, depthEvent{symbol: "BTCUSDT", depth: &Depth{
			LastUpdateID: 43,
			Bids:         []Level{{Price: d("65000"), Qty: d("1")}},
			Asks:         []Level{{Price: d("65000.1"), Qty: d("2")}},
		}}, event)
	default:
		require.FailNow(t, "Sync returned before the frame sent ahead of its ping reached the handler")
	}
	// 紧接着再 Sync 一次：离上一个 ping 不到 pingGap，要等够了再发自己的 ping。
	require.NoError(t, s.Sync(ctx))

	// 服务端断开后按退避重连
	require.NoError(t, first.ws.Close())
	assert.Equal(t, connectedEvent(false), events.next(t))
	server.accept(t)
	assert.Equal(t, connectedEvent(true), events.next(t))

	stop()
	assert.Equal(t, connectedEvent(false), events.next(t))
	assert.False(t, s.Connected())
	assert.ErrorIs(t, s.Sync(ctx), ErrNotConnected)
}

func TestStreamSyncSendsAtMostOnePingPerGap(t *testing.T) {
	// Binance 每条连接每秒最多收 5 个 ping/pong，超过就断开，所以同时调用再多的 Sync，一个 pingGap 里也只能发一个 ping。
	var pings atomic.Int32
	server := newStreamServer(t, func(ws *websocket.Conn) {
		replyPong := ws.PingHandler()
		ws.SetPingHandler(func(data string) error {
			pings.Add(1)
			return replyPong(data)
		})
	})
	events := newRecorder()
	s := newTestStream(server, events)
	s.pingGap = time.Hour
	stop := runStream(t, s)
	conn := server.accept(t)
	require.Equal(t, connectedEvent(true), events.next(t))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	results := make(chan error, 10)
	for range 10 {
		go func() {
			results <- s.Sync(ctx)
		}()
	}
	succeeded := 0
	for range 10 {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
				continue
			}
			// 在第一个 ping 发出之后才开始的 Sync 要等下一个 ping，pingGap 内等不到
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		case <-time.After(2 * time.Second):
			require.FailNow(t, "Sync ignored its ctx deadline")
		}
	}
	assert.GreaterOrEqual(t, succeeded, 1)

	// 断开之后服务端读完了客户端发来的所有帧，计数才是最终的
	stop()
	select {
	case <-conn.done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the server did not see the connection close")
	}
	assert.Equal(t, int32(1), pings.Load())
}

func TestStreamSyncTimesOutWithoutPong(t *testing.T) {
	server := newStreamServer(t, func(ws *websocket.Conn) {
		ws.SetPingHandler(func(string) error { return nil })
	})
	events := newRecorder()
	s := newTestStream(server, events)
	runStream(t, s)
	server.accept(t)
	require.Equal(t, connectedEvent(true), events.next(t))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	require.ErrorIs(t, s.Sync(ctx), context.DeadlineExceeded)
	assert.Less(t, time.Since(started), time.Second)
}

func TestStreamSyncFailsWhenConnectionDrops(t *testing.T) {
	pinged := make(chan struct{}, 1)
	server := newStreamServer(t, func(ws *websocket.Conn) {
		ws.SetPingHandler(func(string) error {
			select {
			case pinged <- struct{}{}:
			default:
			}
			return nil
		})
	})
	events := newRecorder()
	s := newTestStream(server, events)
	runStream(t, s)
	conn := server.accept(t)
	require.Equal(t, connectedEvent(true), events.next(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- s.Sync(ctx)
	}()
	select {
	case <-pinged:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "Sync did not send a ping")
	}
	require.NoError(t, conn.ws.Close())
	select {
	case err := <-result:
		assert.ErrorIs(t, err, ErrNotConnected)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "Sync kept waiting after the connection dropped")
	}
}

func TestStreamReconnectsWhenConnectionGoesSilent(t *testing.T) {
	// 服务端什么都不推、也不回 pong：连接其实已经死了，读超时之后要换一条。
	server := newStreamServer(t, func(ws *websocket.Conn) {
		ws.SetPingHandler(func(string) error { return nil })
	})
	events := newRecorder()
	s := newTestStream(server, events)
	s.idleTimeout = 200 * time.Millisecond
	runStream(t, s)

	server.accept(t)
	require.Equal(t, connectedEvent(true), events.next(t))
	assert.Equal(t, connectedEvent(false), events.next(t))
	server.accept(t)
	assert.Equal(t, connectedEvent(true), events.next(t))
}

func TestStreamPlannedReconnectSkipsBackoff(t *testing.T) {
	tests := []struct {
		name           string
		maxAge         time.Duration
		serverShutdown bool
	}{
		{name: "connection reaches max age", maxAge: 100 * time.Millisecond},
		{name: "server announces shutdown", maxAge: time.Hour, serverShutdown: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newStreamServer(t, nil)
			events := newRecorder()
			s := newTestStream(server, events)
			s.backoff = []time.Duration{time.Hour} // 走了退避，测试就会超时
			s.stableAfter = 0
			s.maxAge = test.maxAge
			runStream(t, s)

			first := server.accept(t)
			require.Equal(t, connectedEvent(true), events.next(t))
			if test.serverShutdown {
				first.send(t, `{"stream":"!serverShutdown","data":{"e":"serverShutdown","E":1770123456789}}`)
			}
			assert.Equal(t, connectedEvent(false), events.next(t))
			server.accept(t)
			assert.Equal(t, connectedEvent(true), events.next(t))
		})
	}
}

// 合约的盘口推送是 depthUpdate 事件(两侧叫 b、a，版本取 u)，标记价格推送里资金费率可以为负。
func TestStreamParsesFuturesDepthAndMarkPrice(t *testing.T) {
	server := newStreamServer(t, nil)
	events := newRecorder()
	s := NewStream(StreamConfig{BaseURL: server.url, Symbols: []string{"BTCUSDT"}, DepthLevels: 20, MarkPrice: true}, events)
	s.keepalive = time.Hour
	runStream(t, s)

	conn := server.accept(t)
	assert.Equal(t, "btcusdt@depth20@100ms/btcusdt@markPrice@1s", conn.streams)
	require.Equal(t, connectedEvent(true), events.next(t))
	conn.send(t, `{"stream":"btcusdt@depth20@100ms","data":{"e":"depthUpdate","E":1791044805993,"T":1791044805992,"s":"BTCUSDT",`+
		`"U":11727350456530,"u":11727350464793,"pu":11727350456340,"b":[["84756.30","9.447"]],"a":[["84756.40","1.5"],["84756.50","0.2"]]}}`)
	conn.send(t, `{"stream":"btcusdt@markPrice@1s","data":{"e":"markPriceUpdate","E":1791044809001,"s":"BTCUSDT","p":"84756.35776087",`+
		`"ap":"84756.35776087","P":"84830.21202428","i":"84788.51847826","r":"-0.00005353","T":1791072000000}}`)

	assert.Equal(t, depthEvent{symbol: "BTCUSDT", depth: &Depth{
		LastUpdateID: 11727350464793,
		Bids:         []Level{{Price: d("84756.30"), Qty: d("9.447")}},
		Asks:         []Level{{Price: d("84756.40"), Qty: d("1.5")}, {Price: d("84756.50"), Qty: d("0.2")}},
	}}, events.next(t))
	assert.Equal(t, markEvent{mark: &MarkPrice{Symbol: "BTCUSDT", EventTime: 1791044809001, Mark: d("84756.35776087"),
		Index: d("84788.51847826"), FundingRate: d("-0.00005353"), NextFundingTime: 1791072000000}}, events.next(t))
}

// 归集成交：现货带着不用的 M、合约带着 nq 与 st，买方是否挂单方只看 m(只差大小写的 M 不能把它盖掉)；没有价格的成交跳过。
func TestStreamParsesAggTrades(t *testing.T) {
	server := newStreamServer(t, nil)
	events := newRecorder()
	s := NewStream(StreamConfig{BaseURL: server.url, Symbols: []string{"BTCUSDT"}, AggTrades: true}, events)
	s.keepalive = time.Hour
	runStream(t, s)

	conn := server.accept(t)
	assert.Equal(t, "btcusdt@aggTrade", conn.streams)
	require.Equal(t, connectedEvent(true), events.next(t))
	conn.send(t, `{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1791482952136,"s":"BTCUSDT","a":4085799055,"p":"80655.20000000",`+
		`"q":"0.00188000","f":6749254870,"l":6749254870,"T":1791482952136,"m":false,"M":true}}`)
	conn.send(t, `{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1791482959470,"a":3479777121,"s":"BTCUSDT","p":"0",`+
		`"q":"0.507","f":1,"l":1,"T":1791482959329,"m":true}}`)
	conn.send(t, `{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1791482959470,"a":3479777122,"s":"BTCUSDT","p":"80606.80",`+
		`"q":"0.507","nq":"0.507","f":8158518466,"l":8158518471,"T":1791482959329,"m":true,"st":1}}`)

	assert.Equal(t, aggTradeEvent{symbol: "BTCUSDT", trade: &AggTrade{ID: 4085799055, Price: d("80655.20000000"), Qty: d("0.00188000"),
		Time: 1791482952136, BuyerMaker: false}}, events.next(t))
	assert.Equal(t, aggTradeEvent{symbol: "BTCUSDT", trade: &AggTrade{ID: 3479777122, Price: d("80606.80"), Qty: d("0.507"),
		Time: 1791482959329, BuyerMaker: true}}, events.next(t))
}
