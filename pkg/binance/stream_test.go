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
