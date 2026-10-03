package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gorilla/websocket"
)

// writeTimeout 是写一个 ping 最多等多久。几十字节的控制帧都写不出去，说明连接已经堵死了。
const writeTimeout = 2 * time.Second

// ErrNotConnected 表示 Sync 时没有连接，或者连接在等 pong 时断了。
var ErrNotConnected = errors.New("binance stream: not connected")

// errPlannedReconnect 表示连接是主动换掉的(用满 maxAge 或者服务端要关机)，连接稳定时 Run 立即重连，不退避。
var errPlannedReconnect = errors.New("planned reconnect")

// StreamHandler 接收行情推送。回调都在读连接的 goroutine 上执行，必须很快返回，而且不能调用 Sync：Sync 等的 pong
// 也要由这个 goroutine 读出来，在回调里调用只会一直等到 ctx 结束。传进来的指针归 handler 所有。
type StreamHandler interface {
	OnDepth(symbol string, depth *Depth, received time.Time)
	OnTicker(ticker *Ticker, received time.Time)
	OnKline(symbol string, kline *Kline, closed bool, received time.Time)
	// OnConnected 在每次连上之后以 true、每次断开之后以 false 调用。连上之后服务端不会先推一份快照，冷门交易对可能
	// 几秒都没有第一帧，断线前缓存的盘口也不能再当成最新的：需要时用 Client.Depth 补一份快照，两边的 LastUpdateID
	// 是同一个序列，可以比较新旧。
	OnConnected(connected bool)
}

// StreamConfig 是行情订阅的配置。
type StreamConfig struct {
	BaseURL       string            // 例如 wss://data-stream.binance.vision
	Symbols       []string          // 大写，例如 BTCUSDT
	DepthLevels   int               // 盘口档数 5、10 或 20，其他值按 20
	KlineInterval string            // K 线周期，例如 1m；为空时不订阅 K 线
	Dialer        *websocket.Dialer // 为空时用 websocket.DefaultDialer 的副本(代理取环境变量)，握手超时 15 秒
}

// Stream 用一条组合流连接订阅所有交易对的局部盘口(100ms)、迷你行情与 K 线，断线自动重连。
//
// 局部盘口只在前几档变化时才推送，冷门交易对(比如美股代币)几秒没有推送很正常，"多久没收到推送"说明不了盘口是否过期。
// 要确认手里的盘口是最新的，用 Sync；刚(重)连上时的情况见 StreamHandler.OnConnected。
type Stream struct {
	url     string // 没有交易对时为空，Run 不连接
	dialer  *websocket.Dialer
	handler StreamHandler

	// 以下是默认值，测试里会改短。
	backoff     []time.Duration // 连续第 n 次失败后等 backoff[n]，超出后一直用最后一项
	stableAfter time.Duration   // 连接撑过这么久，退避从头开始
	idleTimeout time.Duration   // 这么久什么都没收到(数据帧、ping、pong 都算)就断开重连
	keepalive   time.Duration   // 心跳 ping 的间隔，让冷门交易对没有推送时也有 pong 续上读超时
	pingGap     time.Duration   // 两个 ping 之间至少隔多久，见 Sync
	maxAge      time.Duration   // 连接用了这么久就主动换一条：Binance 会在 24 小时断开连接

	mu   sync.Mutex
	conn *streamConn // 当前连接，没有连接时为 nil
}

// NewStream 创建行情订阅，调用 Run 之后才会连接。
func NewStream(cfg StreamConfig, handler StreamHandler) *Stream {
	levels := cfg.DepthLevels
	if levels != 5 && levels != 10 {
		levels = 20
	}
	names := make([]string, 0, len(cfg.Symbols)*3)
	for _, symbol := range cfg.Symbols {
		lower := strings.ToLower(symbol)
		names = append(names, fmt.Sprintf("%s@depth%d@100ms", lower, levels), lower+"@miniTicker")
		if cfg.KlineInterval != "" {
			names = append(names, lower+"@kline_"+cfg.KlineInterval)
		}
	}
	streamURL := ""
	if len(names) > 0 {
		streamURL = strings.TrimRight(cfg.BaseURL, "/") + "/stream?streams=" + strings.Join(names, "/")
	}
	dialer := cfg.Dialer
	if dialer == nil {
		defaultDialer := *websocket.DefaultDialer
		defaultDialer.HandshakeTimeout = 15 * time.Second
		dialer = &defaultDialer
	}
	return &Stream{
		url:         streamURL,
		dialer:      dialer,
		handler:     handler,
		backoff:     []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second},
		stableAfter: time.Minute,
		idleTimeout: 30 * time.Second,
		keepalive:   5 * time.Second,
		pingGap:     500 * time.Millisecond,
		maxAge:      23 * time.Hour,
	}
}

// Run 连接并读取，断线后按 1、2、5、10、30 秒退避重连(连接稳定 60 秒后退避从头开始)，ctx 取消时关闭连接并返回。
// 连接用满 23 小时或者服务端通知要关机时主动换连接：连接稳定时立即重连，刚连上就被通知关机仍然退避，免得反复重连
// 撞上 Binance 每 5 分钟 300 次连接的限制。没有交易对时不连接，等到 ctx 结束。同一个 Stream 同时只能有一个 Run。
func (s *Stream) Run(ctx context.Context) {
	if s.url == "" {
		<-ctx.Done()
		return
	}
	failures := 0
	for {
		connectedAt, err := s.session(ctx)
		if ctx.Err() != nil {
			return
		}
		stable := !connectedAt.IsZero() && time.Since(connectedAt) >= s.stableAfter
		if stable {
			failures = 0
		}
		var delay time.Duration
		if !stable || !errors.Is(err, errPlannedReconnect) {
			delay = s.backoff[min(failures, len(s.backoff)-1)]
			failures++
			common.SysError(fmt.Sprintf("binance stream: %v, reconnecting in %v", err, delay))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// Sync 确认手里的行情是最新的：在当前连接上发一个 ping 并等它(或之后任意一个)的 pong 回来。pong 回来说明服务端在收到
// 这个 ping 之前推出的帧都已经读到并交给了 handler。没有连接、等待期间断线、ctx 结束时返回错误，ctx 应当带超时。
// 可以被多个 goroutine 并发调用。
//
// Binance 每条连接每秒最多收 5 个消息(ping 与 pong 都算)，超过就断开，反复超过会封 IP，所以不是每次 Sync 都立刻发
// 一个 ping：两个 ping 之间至少隔 pingGap(500 毫秒)，同时在等的 Sync(还有心跳)共用调用之后发出的同一个 ping，
// Sync 因此最多多等 pingGap。
func (s *Stream) Sync(ctx context.Context) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return ErrNotConnected
	}
	need := conn.nextSeq()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait, err := conn.ping(need, s.pingGap)
		if err != nil {
			return fmt.Errorf("%w: ping: %v", ErrNotConnected, err)
		}
		conn.mu.Lock()
		acked, closed, wake := conn.acked, conn.closed, conn.wake
		conn.mu.Unlock()
		if acked >= need {
			return nil
		}
		if closed {
			return ErrNotConnected
		}
		var retry <-chan time.Time
		if wait > 0 {
			retry = time.After(wait)
		}
		select {
		case <-wake:
		case <-retry:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Connected 表示当前有连接。
func (s *Stream) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// session 建立一条连接并一直读到断开，返回连接建立的时间(没连上时为零值)与断开的原因。
func (s *Stream) session(ctx context.Context) (time.Time, error) {
	ws, resp, err := s.dialer.DialContext(ctx, s.url, nil)
	if err != nil {
		if resp != nil {
			return time.Time{}, fmt.Errorf("dial: %w (http %d)", err, resp.StatusCode)
		}
		return time.Time{}, fmt.Errorf("dial: %w", err)
	}
	connectedAt := time.Now()
	conn := &streamConn{ws: ws, wake: make(chan struct{})}
	ws.SetReadLimit(maxResponseBytes)
	// 读超时在收到任何东西时续上：数据帧在 read 里续，服务端的 ping 与回给我们的 pong 在这两个回调里续。
	_ = ws.SetReadDeadline(connectedAt.Add(s.idleTimeout))
	replyPong := ws.PingHandler()
	ws.SetPingHandler(func(data string) error {
		_ = ws.SetReadDeadline(time.Now().Add(s.idleTimeout))
		return replyPong(data)
	})
	ws.SetPongHandler(func(data string) error {
		_ = ws.SetReadDeadline(time.Now().Add(s.idleTimeout))
		conn.acknowledge(data)
		return nil
	})

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	s.handler.OnConnected(true)

	readerDone := make(chan struct{})
	closedBy := make(chan error, 1)
	go func() {
		closedBy <- s.heartbeat(ctx, conn, readerDone)
	}()
	err = s.read(ws)
	close(readerDone)
	if cause := <-closedBy; cause != nil {
		err = cause
	}
	_ = ws.Close()

	s.mu.Lock()
	s.conn = nil
	s.mu.Unlock()
	conn.shutdown()
	s.handler.OnConnected(false)
	return connectedAt, err
}

// heartbeat 定时发心跳 ping。连接用满 maxAge、ping 发不出去或者 ctx 结束时关闭连接让读循环退出，并返回原因；
// 读循环自己先退出时返回 nil。
func (s *Stream) heartbeat(ctx context.Context, conn *streamConn, readerDone <-chan struct{}) error {
	ticker := time.NewTicker(s.keepalive)
	defer ticker.Stop()
	aged := time.NewTimer(s.maxAge)
	defer aged.Stop()
	for {
		select {
		case <-readerDone:
			return nil
		case <-ctx.Done():
			_ = conn.ws.Close()
			return ctx.Err()
		case <-aged.C:
			_ = conn.ws.Close()
			return fmt.Errorf("%w: connection is %v old", errPlannedReconnect, s.maxAge)
		case <-ticker.C:
			if _, err := conn.ping(conn.nextSeq(), s.pingGap); err != nil {
				_ = conn.ws.Close()
				return fmt.Errorf("keepalive ping: %w", err)
			}
		}
	}
}

// read 读帧并交给 handler，直到连接出错或者服务端通知要关机。格式不对的帧跳过，每条连接只记一次日志。
func (s *Stream) read(ws *websocket.Conn) error {
	logged := false
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		received := time.Now()
		_ = ws.SetReadDeadline(received.Add(s.idleTimeout))
		var frame struct {
			Stream string          `json:"stream"`
			Data   json.RawMessage `json:"data"`
		}
		err = common.Unmarshal(data, &frame)
		if err == nil && frame.Stream == "!serverShutdown" {
			return fmt.Errorf("%w: server is shutting down", errPlannedReconnect)
		}
		if err == nil {
			err = s.dispatch(frame.Stream, frame.Data, received)
		}
		if err != nil && !logged {
			logged = true
			common.SysError(fmt.Sprintf("binance stream: skipped malformed frame from stream %q: %v", frame.Stream, err))
		}
	}
}

// dispatch 解析一帧组合流的 data 并交给 handler。局部盘口的 data 里没有交易对，交易对一律取自流名称，
// 例如 "btcusdt@depth20@100ms" 是 BTCUSDT。
//
// encoding/json 匹配字段名不分大小写，Binance 的推送里又有只差大小写的字段(e 与 E、v 与 V)，所以下面会声明一些用不到的
// 字段：不声明的话，"e" 会被当成 "E" 解码而报错，K 线里主动买入量 "V"、"Q" 会覆盖成交量 "v"、"q"。
func (s *Stream) dispatch(stream string, data json.RawMessage, received time.Time) error {
	name, kind, _ := strings.Cut(stream, "@")
	symbol := strings.ToUpper(name)
	switch {
	case symbol == "":
		return errors.New("no symbol in stream name")
	case strings.HasPrefix(kind, "depth"):
		var payload depthPayload
		if err := common.Unmarshal(data, &payload); err != nil {
			return err
		}
		depth, err := payload.depth()
		if err != nil {
			return err
		}
		s.handler.OnDepth(symbol, depth, received)
	case kind == "miniTicker":
		var payload struct {
			EventType   string `json:"e"`
			EventTime   int64  `json:"E"`
			Close       string `json:"c"`
			Open        string `json:"o"`
			High        string `json:"h"`
			Low         string `json:"l"`
			Volume      string `json:"v"`
			QuoteVolume string `json:"q"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			return err
		}
		var p decimalParser
		ticker := &Ticker{
			Symbol:      symbol,
			EventTime:   payload.EventTime,
			Close:       p.parse("c", payload.Close),
			Open:        p.parse("o", payload.Open),
			High:        p.parse("h", payload.High),
			Low:         p.parse("l", payload.Low),
			Volume:      p.parse("v", payload.Volume),
			QuoteVolume: p.parse("q", payload.QuoteVolume),
		}
		if p.err != nil {
			return p.err
		}
		s.handler.OnTicker(ticker, received)
	case strings.HasPrefix(kind, "kline_"):
		var payload struct {
			K struct {
				OpenTime       int64  `json:"t"`
				CloseTime      int64  `json:"T"`
				Open           string `json:"o"`
				Close          string `json:"c"`
				High           string `json:"h"`
				Low            string `json:"l"`
				Volume         string `json:"v"`
				QuoteVolume    string `json:"q"`
				Trades         int64  `json:"n"`
				Closed         bool   `json:"x"`
				LastTradeID    int64  `json:"L"`
				TakerBuyVolume string `json:"V"`
				TakerBuyQuote  string `json:"Q"`
			} `json:"k"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			return err
		}
		var p decimalParser
		k := payload.K
		kline := &Kline{
			OpenTime:    k.OpenTime,
			Open:        p.parse("o", k.Open),
			High:        p.parse("h", k.High),
			Low:         p.parse("l", k.Low),
			Close:       p.parse("c", k.Close),
			Volume:      p.parse("v", k.Volume),
			CloseTime:   k.CloseTime,
			QuoteVolume: p.parse("q", k.QuoteVolume),
			Trades:      k.Trades,
		}
		if p.err != nil {
			return p.err
		}
		s.handler.OnKline(symbol, kline, k.Closed, received)
	default:
		return errors.New("unknown stream")
	}
	return nil
}

// streamConn 是一条连接上 ping/pong 的状态，Sync 与心跳共用。ping 带着这条连接上递增的序号，pong 原样带回。
type streamConn struct {
	ws *websocket.Conn

	mu     sync.Mutex
	sent   uint64        // 发出的最后一个 ping 的序号
	sentAt time.Time     // 发出最后一个 ping 的时间
	acked  uint64        // 收到的最大 pong 序号
	closed bool          // 连接已断开
	wake   chan struct{} // 收到新的 pong 或者连接断开时关闭，等待的 Sync 醒来重新检查
}

// nextSeq 是下一个 ping 的序号：从现在起发出的 ping 序号都不小于它。
func (c *streamConn) nextSeq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent + 1
}

// ping 确保发出过序号不小于 need 的 ping。已经发过(别的 Sync 或者心跳发的)时什么也不做；离上一个 ping 不到 gap 时
// 先不发，返回还要等多久。写 ping 时一直持有锁，线路上 ping 的序号因此是递增的。
func (c *streamConn) ping(need uint64, gap time.Duration) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sent >= need {
		return 0, nil
	}
	if wait := gap - time.Since(c.sentAt); wait > 0 {
		return wait, nil
	}
	c.sent++
	c.sentAt = time.Now()
	return 0, c.ws.WriteControl(websocket.PingMessage, []byte(strconv.FormatUint(c.sent, 10)), c.sentAt.Add(writeTimeout))
}

// acknowledge 记下 pong 带回的序号并唤醒等待的 Sync。不是我们发出过的序号(比如服务端主动发的 pong)不算数。
func (c *streamConn) acknowledge(appData string) {
	seq, err := strconv.ParseUint(appData, 10, 64)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil || seq <= c.acked || seq > c.sent || c.closed {
		return
	}
	c.acked = seq
	close(c.wake)
	c.wake = make(chan struct{})
}

// shutdown 标记连接已断开并唤醒等待的 Sync。
func (c *streamConn) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	close(c.wake)
}
