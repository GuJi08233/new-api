package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
)

type TradeLiquidationSnapshot struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Notional string `json:"notional"`
	Time     int64  `json:"time"`
	TimeMs   int64  `json:"time_ms"`
}

type tradeLiquidationFeed struct {
	mu        sync.Mutex
	items     []TradeLiquidationSnapshot
	connected bool
	updatedAt int64
	startedAt int64
	lastError string
}

var tradeLiquidations = &tradeLiquidationFeed{}
var tradeInsightsOnce sync.Once

// StartTradeInsights 仅订阅只读强平快照。每节点保留最近 24 小时至多 200 条，重启会清空，不提供历史全量统计。
func StartTradeInsights() {
	tradeInsightsOnce.Do(func() { go tradeLiquidations.supervise() })
}

func (f *tradeLiquidationFeed) supervise() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var cancel context.CancelFunc
	var done chan struct{}
	var activeURL, activeProxy string
	for {
		setting := operation_setting.GetTradeSetting()
		target := ""
		if setting.Enabled && setting.InsightsEnabled && setting.FuturesEnabled {
			target = strings.TrimRight(setting.FuturesWsUrl, "/")
		}
		if target != activeURL || setting.ProxyUrl != activeProxy {
			if cancel != nil {
				cancel()
				<-done
				cancel = nil
			}
			activeURL, activeProxy = target, setting.ProxyUrl
			if target != "" {
				ctx, stop := context.WithCancel(context.Background())
				cancel = stop
				done = make(chan struct{})
				go func(target, proxy string, finished chan struct{}) { defer close(finished); f.run(ctx, target, proxy) }(target, activeProxy, done)
			}
		}
		<-ticker.C
	}
}

func (f *tradeLiquidationFeed) run(ctx context.Context, baseURL, proxy string) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_ = f.session(ctx, baseURL, proxy)
		f.mu.Lock()
		f.connected = false
		f.lastError = "upstream_unavailable"
		f.mu.Unlock()
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (f *tradeLiquidationFeed) session(ctx context.Context, baseURL, proxy string) error {
	_, dialer, err := tradeMarketTransport(proxy)
	if err != nil {
		return err
	}
	dialer.HandshakeTimeout = 12 * time.Second
	baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/ws")
	baseURL = strings.TrimSuffix(baseURL, "/market")
	ws, response, err := dialer.DialContext(ctx, baseURL+"/market/ws/!forceOrder@arr", nil)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(64 << 10)
	now := time.Now()
	_ = ws.SetReadDeadline(now.Add(90 * time.Second))
	f.mu.Lock()
	f.connected = true
	f.lastError = ""
	f.updatedAt = now.Unix()
	if f.startedAt == 0 {
		f.startedAt = now.Unix()
	}
	f.mu.Unlock()
	ws.SetPongHandler(func(string) error {
		at := time.Now()
		_ = ws.SetReadDeadline(at.Add(90 * time.Second))
		f.mu.Lock()
		f.updatedAt = at.Unix()
		f.mu.Unlock()
		return nil
	})
	pingHandler := ws.PingHandler()
	ws.SetPingHandler(func(data string) error {
		_ = ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		return pingHandler(data)
	})
	readerDone, keeperDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(keeperDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				ws.Close()
				return
			case <-readerDone:
				return
			case <-ticker.C:
				if err := ws.WriteControl(websocket.PingMessage, []byte("insights"), time.Now().Add(5*time.Second)); err != nil {
					ws.Close()
					return
				}
			}
		}
	}()
	defer func() { close(readerDone); <-keeperDone }()
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return err
		}
		now = time.Now()
		_ = ws.SetReadDeadline(now.Add(90 * time.Second))
		if err = f.record(data, now); err != nil {
			return err
		}
	}
}

func (f *tradeLiquidationFeed) record(data []byte, received time.Time) error {
	// 必须声明大小写相近的键，防止编码器将 e/E、s/S 或 p/P 错配。
	var frame struct {
		Event      string `json:"e"`
		EventTime  int64  `json:"E"`
		SymbolType *int   `json:"st"`
		Order      struct {
			Symbol  string `json:"s"`
			Side    string `json:"S"`
			Average string `json:"ap"`
			Filled  string `json:"z"`
			Time    int64  `json:"T"`
		} `json:"o"`
	}
	if err := common.Unmarshal(data, &frame); err != nil {
		return err
	}
	if frame.Event != "forceOrder" {
		return errors.New("unexpected liquidation event")
	}
	// Binance 合并 UM/CM 推送后，同一 fstream 也会发送币本位合约。其数量是合约张数，
	// 不能按币数乘价格；本接口明确只覆盖 USD-M，旧版本不带 st 的 USD-M 帧仍兼容。
	if frame.SymbolType != nil && *frame.SymbolType != 1 {
		return nil
	}
	o := frame.Order
	if o.Symbol == "" || len(o.Symbol) > 32 || (o.Side != "BUY" && o.Side != "SELL") || o.Time <= 0 || o.Time > received.Add(time.Minute).UnixMilli() {
		return errors.New("invalid liquidation snapshot")
	}
	price, err := tradeInsightDecimal(o.Average)
	if err != nil || price.IsNegative() {
		return errors.New("invalid liquidation price")
	}
	qty, err := tradeInsightDecimal(o.Filled)
	if err != nil || qty.IsNegative() {
		return errors.New("invalid liquidation quantity")
	}
	if price.IsZero() || qty.IsZero() {
		return nil
	}
	side := "long"
	if o.Side == "BUY" {
		side = "short"
	}
	item := TradeLiquidationSnapshot{Symbol: o.Symbol, Side: side, Price: price.String(), Quantity: qty.String(), Notional: price.Mul(qty).String(), Time: o.Time / 1000, TimeMs: o.Time}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedAt = received.Unix()
	for _, old := range f.items {
		if old == item {
			return nil
		}
	}
	items := make([]TradeLiquidationSnapshot, 0, 200)
	items = append(items, item)
	for _, old := range f.items {
		if len(items) == 200 {
			break
		}
		if old.Time >= received.Add(-24*time.Hour).Unix() {
			items = append(items, old)
		}
	}
	f.items = items
	return nil
}

func (f *tradeLiquidationFeed) view() TradeInsightResponse {
	view, _ := tradeInsightView("liquidations")
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]TradeLiquidationSnapshot, 0, len(f.items))
	for _, item := range f.items {
		if item.Time >= time.Now().Add(-24*time.Hour).Unix() {
			items = append(items, item)
		}
	}
	view.Items, view.UpdatedAt, view.CollectionStartedAt, view.Error = items, f.updatedAt, f.startedAt, f.lastError
	if f.connected {
		view.Status = "ready"
	} else if f.updatedAt > 0 {
		view.Status = "stale"
	}
	if view.Status == "unavailable" && view.Error == "" {
		view.Error = "connecting"
	}
	return view
}
