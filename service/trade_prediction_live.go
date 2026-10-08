package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
)

// 预测页的实时数据，照 Polymarket 的页面：Chainlink BTC 60 秒 TWAP(页面上的当前价，也是结算口径)、本轮目标价(回合开始那一刻的
// TWAP)和本轮的实时成交。价格与成交走同一条 Polymarket 实时推送，有人打开预测页才连，3 分钟没人看就断开；目标价走官方接口。
// 只读公开数据，只用于展示，不参与成交与结算。
const (
	predictionLiveIdle    = 3 * time.Minute
	predictionLiveHistory = 7 * time.Minute
	predictionLiveStale   = 15 * time.Second
	predictionLivePing    = 5 * time.Second
	predictionLiveSilence = 30 * time.Second
	predictionLiveMaxWait = 30 * time.Second
	predictionOpenRetry   = 2 * time.Second
	predictionTradeLimit  = 20
	// 价格推送与目标价超出这个范围按脏数据丢弃。
	predictionMaxBtcPrice = 100_000_000
	// 一笔成交的份额上限，超出按脏数据丢弃。
	predictionMaxTradeSize = 1_000_000_000
	predictionTwapTopic    = "crypto_prices_twap_sixty"
	predictionTradeTopic   = "activity"
)

var errPredictionPrice = errors.New("prediction price unavailable")
var errPredictionLiveIdle = errors.New("prediction live data idle")

// TradePredictionPricePoint 是一个 TWAP 价格点，Time 是 Chainlink 的毫秒时间戳。
type TradePredictionPricePoint struct {
	Time  int64   `json:"time"`
	Price float64 `json:"price"`
}

// TradePredictionTrade 是 Polymarket 上本轮的一笔成交，只留方向、份额、价格和时间(秒)，不带交易者信息。
type TradePredictionTrade struct {
	Side    string  `json:"side"`
	Outcome string  `json:"outcome"`
	Size    float64 `json:"size"`
	Price   float64 `json:"price"`
	Time    int64   `json:"time"`
}

// TradePredictionLiveView 是预测页每秒拉一次的实时数据。OpenPrice 与 Price 为 0 表示还没拿到；Points 只含 since 之后、
// 本轮开始前 60 秒起的价格点；Trades 是本轮最近的成交，新的在前。
type TradePredictionLiveView struct {
	Enabled     bool                        `json:"enabled"`
	Connected   bool                        `json:"connected"`
	WindowStart int64                       `json:"window_start"`
	OpenPrice   float64                     `json:"open_price"`
	Price       float64                     `json:"price"`
	PriceAt     int64                       `json:"price_at"`
	Points      []TradePredictionPricePoint `json:"points"`
	Trades      []TradePredictionTrade      `json:"trades"`
}

type tradePredictionLive struct {
	mu          sync.Mutex
	running     bool
	connected   bool
	lastUsed    time.Time
	points      []TradePredictionPricePoint
	openWindow  int64
	openPrice   float64
	openTried   time.Time
	openBusy    bool
	tradeWindow int64
	trades      []predictionTradeRecord
}

// predictionTradeRecord 是一笔链上成交。Polymarket 的成交推送按参与者各发一条：吃单方一条，被吃的每个挂单各一条，交易哈希
// 相同。按哈希合并成一行，留份额最大的那条，也就是吃单方的整笔成交。
type predictionTradeRecord struct {
	trade TradePredictionTrade
	tx    string
}

type predictionLiveSubscription struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Filters string `json:"filters"`
}

func predictionWindowStart(now time.Time) int64 {
	return now.Unix() / model.TradePredictionWindow * model.TradePredictionWindow
}

func predictionSlug(windowStart int64) string {
	return "btc-updown-5m-" + strconv.FormatInt(windowStart, 10)
}

func GetTradePredictionLive(since int64) TradePredictionLiveView {
	return tradePredictions.liveView(since, time.Now())
}

// liveView 记下有人在看：没连上就起推送连接，目标价没拿到就在后台补拉，本次直接返回已有的数据。
func (client *tradePredictionClient) liveView(since int64, now time.Time) TradePredictionLiveView {
	setting := operation_setting.GetTradeSetting()
	windowStart := predictionWindowStart(now)
	view := TradePredictionLiveView{Enabled: setting.Enabled && setting.PredictionEnabled, WindowStart: windowStart,
		Points: []TradePredictionPricePoint{}, Trades: []TradePredictionTrade{}}
	if !view.Enabled {
		return view
	}
	live := &client.live
	live.mu.Lock()
	defer live.mu.Unlock()
	live.lastUsed = now
	if !live.running {
		live.running = true
		go client.runLive()
	}
	if live.openWindow != windowStart && !live.openBusy && now.Sub(live.openTried) >= predictionOpenRetry {
		live.openBusy, live.openTried = true, now
		go client.refreshOpenPrice(windowStart)
	}
	view.Connected = live.connected
	if live.openWindow == windowStart {
		view.OpenPrice = live.openPrice
	}
	if live.tradeWindow == windowStart {
		for _, record := range live.trades {
			view.Trades = append(view.Trades, record.trade)
		}
	}
	from := max(since, windowStart*1000-60_000-1)
	for _, point := range live.points {
		if point.Time > from {
			view.Points = append(view.Points, point)
		}
	}
	if count := len(live.points); count > 0 && now.UnixMilli()-live.points[count-1].Time <= predictionLiveStale.Milliseconds() {
		view.Price, view.PriceAt = live.points[count-1].Price, live.points[count-1].Time
	}
	return view
}

// runLive 维持推送连接，断了按 1、2、4…最多 30 秒重连；没人看了就退出，只有这里会把 running 置回 false。
func (client *tradePredictionClient) runLive() {
	delay := time.Second
	for client.liveWanted() {
		started := time.Now()
		if err := client.streamLive(); err != nil && !errors.Is(err, errPredictionLiveIdle) {
			common.SysLog("prediction live data: " + err.Error())
		}
		client.live.mu.Lock()
		client.live.connected = false
		client.live.mu.Unlock()
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		if !client.liveWanted() {
			return
		}
		time.Sleep(delay)
		delay = min(delay*2, predictionLiveMaxWait)
	}
}

func (client *tradePredictionClient) liveIdle() bool {
	client.live.mu.Lock()
	defer client.live.mu.Unlock()
	return time.Since(client.live.lastUsed) > predictionLiveIdle
}

func (client *tradePredictionClient) liveWanted() bool {
	live := &client.live
	live.mu.Lock()
	defer live.mu.Unlock()
	if time.Since(live.lastUsed) <= predictionLiveIdle {
		return true
	}
	live.running, live.connected, live.points, live.trades = false, false, nil, nil
	return false
}

func (client *tradePredictionClient) streamLive() error {
	dialer, err := client.dialer(operation_setting.GetTradeSetting().ProxyUrl)
	if err != nil {
		return err
	}
	conn, _, err := dialer.Dial(client.liveURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	done := make(chan struct{})
	defer close(done)
	idle := make(chan struct{})
	go client.writeLive(conn, done, idle)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(predictionLiveSilence))
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-idle:
				return errPredictionLiveIdle
			default:
				return err
			}
		}
		client.applyLiveMessage(data, time.Now())
	}
}

// writeLive 是连接上唯一写数据的协程：先订阅 BTC 的 TWAP 和本轮的成交，换轮时改订新一轮的成交；Polymarket 要求每 5 秒发一次
// 文本 PING。没人看了就关连接，让读循环退出。
func (client *tradePredictionClient) writeLive(conn *websocket.Conn, done <-chan struct{}, idle chan<- struct{}) {
	write := func(data []byte) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if conn.WriteMessage(websocket.TextMessage, data) != nil {
			_ = conn.Close()
			return false
		}
		return true
	}
	request := func(action string, subscriptions ...predictionLiveSubscription) bool {
		data, err := common.Marshal(map[string]any{"action": action, "subscriptions": subscriptions})
		return err == nil && write(data)
	}
	trades := func(windowStart int64) predictionLiveSubscription {
		return predictionLiveSubscription{Topic: predictionTradeTopic, Type: "trades", Filters: `{"event_slug":"` + predictionSlug(windowStart) + `"}`}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	subscribed := int64(0)
	pinged := time.Now()
	for {
		if windowStart := predictionWindowStart(time.Now()); windowStart != subscribed {
			if subscribed == 0 {
				if !request("subscribe", predictionLiveSubscription{Topic: predictionTwapTopic, Type: "*", Filters: `{"symbol":"btc/usd"}`}, trades(windowStart)) {
					return
				}
				client.live.mu.Lock()
				client.live.connected = true
				client.live.mu.Unlock()
			} else if !request("unsubscribe", trades(subscribed)) || !request("subscribe", trades(windowStart)) {
				return
			}
			subscribed = windowStart
		}
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		if client.liveIdle() {
			close(idle)
			_ = conn.Close()
			return
		}
		if time.Since(pinged) >= predictionLivePing {
			if !write([]byte("PING")) {
				return
			}
			pinged = time.Now()
		}
	}
}

// applyLiveMessage 记下一条推送。TWAP：订阅后先来一份最近一分钟的快照(payload.data)，之后每秒一个点；点按时间排序去重，只留
// 最近 7 分钟。成交：只收当前这一轮的，换轮清空，同一笔交易合并，新的在前，最多留 20 笔。心跳回包和别的主题直接忽略。
func (client *tradePredictionClient) applyLiveMessage(data []byte, received time.Time) {
	type rawPoint struct {
		Timestamp int64       `json:"timestamp"`
		Value     json.Number `json:"value"`
	}
	var message struct {
		Topic   string `json:"topic"`
		Type    string `json:"type"`
		Payload struct {
			Symbol    string      `json:"symbol"`
			Timestamp int64       `json:"timestamp"`
			Value     json.Number `json:"value"`
			Data      []rawPoint  `json:"data"`
			Slug      string      `json:"slug"`
			Side      string      `json:"side"`
			Outcome   string      `json:"outcome"`
			Size      json.Number `json:"size"`
			Price     json.Number `json:"price"`
			Tx        string      `json:"transactionHash"`
		} `json:"payload"`
	}
	if len(data) == 0 || data[0] != '{' || common.Unmarshal(data, &message) != nil {
		return
	}
	payload := message.Payload
	if message.Topic == predictionTradeTopic {
		if message.Type == "trades" {
			client.applyLiveTrade(payload.Slug, payload.Tx, payload.Side, payload.Outcome, payload.Size, payload.Price, payload.Timestamp, received)
		}
		return
	}
	if (message.Topic != "" && message.Topic != predictionTwapTopic) || (payload.Symbol != "" && payload.Symbol != "btc/usd") {
		return
	}
	raw := payload.Data
	if len(raw) == 0 {
		raw = []rawPoint{{Timestamp: payload.Timestamp, Value: payload.Value}}
	}
	nowMs := received.UnixMilli()
	oldest := nowMs - predictionLiveHistory.Milliseconds()
	points := make([]TradePredictionPricePoint, 0, len(raw))
	for _, item := range raw {
		price, err := predictionBtcPrice(item.Value)
		if err != nil || item.Timestamp < oldest || item.Timestamp > nowMs+60_000 {
			continue
		}
		points = append(points, TradePredictionPricePoint{Time: item.Timestamp, Price: price})
	}
	if len(points) == 0 {
		return
	}
	live := &client.live
	live.mu.Lock()
	defer live.mu.Unlock()
	merged := make(map[int64]float64, len(live.points)+len(points))
	for _, point := range live.points {
		if point.Time >= oldest {
			merged[point.Time] = point.Price
		}
	}
	for _, point := range points {
		merged[point.Time] = point.Price
	}
	live.points = live.points[:0]
	for at, price := range merged {
		live.points = append(live.points, TradePredictionPricePoint{Time: at, Price: price})
	}
	sort.Slice(live.points, func(i, j int) bool { return live.points[i].Time < live.points[j].Time })
}

func (client *tradePredictionClient) applyLiveTrade(slug, tx, side, outcome string, rawSize, rawPrice json.Number, at int64, received time.Time) {
	windowStart, err := strconv.ParseInt(strings.TrimPrefix(slug, "btc-updown-5m-"), 10, 64)
	side, outcome = strings.ToUpper(side), strings.ToUpper(outcome)
	size, sizeErr := strconv.ParseFloat(string(rawSize), 64)
	price, priceErr := strconv.ParseFloat(string(rawPrice), 64)
	if err != nil || slug != predictionSlug(windowStart) || (side != "BUY" && side != "SELL") ||
		(outcome != model.TradePredictionUp && outcome != model.TradePredictionDown) ||
		sizeErr != nil || !(size > 0 && size < predictionMaxTradeSize) || priceErr != nil || !(price > 0 && price < 1) ||
		at <= 0 || at > received.Unix()+60 {
		return
	}
	live := &client.live
	live.mu.Lock()
	defer live.mu.Unlock()
	if windowStart < live.tradeWindow {
		return
	}
	if windowStart > live.tradeWindow {
		live.tradeWindow, live.trades = windowStart, nil
	}
	trade := TradePredictionTrade{Side: side, Outcome: outcome, Size: size, Price: price, Time: at}
	for i := range live.trades {
		if tx != "" && live.trades[i].tx == tx {
			if size > live.trades[i].trade.Size {
				live.trades[i].trade = trade
			}
			return
		}
	}
	// 推送大体按时间先后到达，个别晚到的按时间插回去，新的在前。
	index := sort.Search(len(live.trades), func(i int) bool { return live.trades[i].trade.Time <= at })
	if index >= predictionTradeLimit {
		return
	}
	live.trades = append(live.trades[:index], append([]predictionTradeRecord{{trade: trade, tx: tx}}, live.trades[index:]...)...)
	live.trades = live.trades[:min(len(live.trades), predictionTradeLimit)]
}

func predictionBtcPrice(raw json.Number) (float64, error) {
	price, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsNaN(price) || price <= 0 || price >= predictionMaxBtcPrice {
		return 0, errPredictionPrice
	}
	return price, nil
}

func (client *tradePredictionClient) refreshOpenPrice(windowStart int64) {
	price, err := client.openPrice(context.Background(), windowStart)
	live := &client.live
	live.mu.Lock()
	defer live.mu.Unlock()
	live.openBusy = false
	if err == nil {
		live.openWindow, live.openPrice = windowStart, price
	}
}

// openPrice 是这一轮的目标价：回合开始那一刻往前 60 秒的 Chainlink TWAP，和官方结算同一个口径。回合刚开始时官方可能还没算好，
// 返回错误，稍后再拉。
func (client *tradePredictionClient) openPrice(ctx context.Context, windowStart int64) (float64, error) {
	start := time.Unix(windowStart, 0).UTC()
	query := url.Values{
		"symbol":              {"BTC"},
		"variant":             {"fiveminute"},
		"eventStartTime":      {start.Format(time.RFC3339)},
		"endDate":             {start.Add(time.Duration(model.TradePredictionWindow) * time.Second).Format(time.RFC3339)},
		"twapEnabled":         {"true"},
		"twapLookbackSeconds": {"60"},
	}
	var result struct {
		OpenPrice json.Number `json:"openPrice"`
	}
	if err := client.getJSON(ctx, client.cryptoURL+"/api/crypto/crypto-price?"+query.Encode(), &result); err != nil {
		return 0, err
	}
	return predictionBtcPrice(result.OpenPrice)
}
