package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"golang.org/x/sync/singleflight"
)

// TradeInsightResponse 明确区分成功的空数据、旧缓存与源不可用；时间均为 Unix 秒。
type TradeInsightResponse struct {
	Source              string `json:"source"`
	Coverage            string `json:"coverage"`
	Status              string `json:"status"`
	UpdatedAt           int64  `json:"updated_at"`
	Error               string `json:"error"`
	Items               any    `json:"items"`
	SampleSize          int    `json:"sample_size,omitempty"`
	SampleTarget        int    `json:"sample_target,omitempty"`
	CollectionStartedAt int64  `json:"collection_started_at,omitempty"`
}

type tradeInsightEntry struct {
	view TradeInsightResponse
	next time.Time
}

type tradeInsightClient struct {
	mu               sync.Mutex
	cache            map[string]tradeInsightEntry
	group            singleflight.Group
	whaleAddresses   []string
	whaleAddressesAt time.Time
	// 以下注入点只供测试替换，生产地址固定，不接受用户传入 URL。
	calendarURL, leaderboardURL, hyperliquidURL, newsURL string
	transport                                            func(string) (*http.Client, error)
	key                                                  func() string
	now                                                  func() time.Time
}

func newTradeInsightClient() *tradeInsightClient {
	return &tradeInsightClient{
		cache:          make(map[string]tradeInsightEntry),
		calendarURL:    "https://economic-calendar.tradingview.com/events",
		leaderboardURL: "https://stats-data.hyperliquid.xyz/Mainnet/leaderboard",
		hyperliquidURL: "https://api.hyperliquid.xyz/info",
		newsURL:        "https://api-pro.theblockbeats.info/v1/newsflash/important",
		transport: func(proxy string) (*http.Client, error) {
			client, _, err := tradeMarketTransport(proxy)
			return client, err
		},
		key: func() string { return os.Getenv("BLOCKBEATS_API_KEY") },
		now: time.Now,
	}
}

var tradeInsights = newTradeInsightClient()

func tradeInsightView(kind string) (TradeInsightResponse, bool) {
	view := TradeInsightResponse{Status: "unavailable", Items: []any{}}
	switch kind {
	case "calendar":
		view.Source, view.Coverage = "TradingView", "global_high_importance"
	case "liquidations":
		view.Source, view.Coverage = "Binance", "binance_usdm_snapshots"
	case "whales":
		view.Source, view.Coverage, view.SampleTarget = "Hyperliquid", "leaderboard_top20_sample", 20
	case "news":
		view.Source, view.Coverage = "BlockBeats", "important_news_cn"
	case "companies":
		view.Source, view.Coverage = "wtfibought / Binance RWA", "static_company_identity"
	default:
		return view, false
	}
	return view, true
}

func GetTradeInsights(ctx context.Context, kind string) (TradeInsightResponse, bool) {
	return tradeInsights.get(ctx, kind, operation_setting.GetTradeSetting())
}

func (c *tradeInsightClient) get(ctx context.Context, kind string, setting *operation_setting.TradeSetting) (TradeInsightResponse, bool) {
	view, ok := tradeInsightView(kind)
	if !ok {
		return view, false
	}
	if !setting.Enabled || !setting.InsightsEnabled || kind == "liquidations" && !setting.FuturesEnabled {
		view.Status, view.Error = "disabled", "feature_disabled"
		return view, true
	}
	key := ""
	if kind == "news" {
		key = c.key()
		if key == "" {
			view.Status, view.Error = "disabled", "api_key_missing"
			return view, true
		}
	}
	if kind == "liquidations" {
		return tradeLiquidations.view(), true
	}
	if kind == "companies" {
		view.Status, view.Items = "ready", tradeCompanyIdentities
		return view, true
	}
	c.mu.Lock()
	entry, exists := c.cache[kind]
	c.mu.Unlock()
	if exists && c.now().Before(entry.next) {
		return entry.view, true
	}
	// 多个用户同一时刻打开页面只触发一次上游刷新。请求取消不取消其他等待者的刷新。
	result := c.group.DoChan(kind, func() (any, error) {
		c.mu.Lock()
		current, cached := c.cache[kind]
		c.mu.Unlock()
		if cached && c.now().Before(current.next) {
			return current.view, nil
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		fresh := view
		var err error
		ttl := 10 * time.Minute
		switch kind {
		case "calendar":
			fresh.Items, err = c.calendar(fetchCtx, setting.ProxyUrl)
			ttl = 4 * time.Hour
		case "news":
			fresh.Items, err = c.news(fetchCtx, setting.ProxyUrl, key)
		case "whales":
			fresh.Items, fresh.SampleSize, err = c.whales(fetchCtx, setting.ProxyUrl)
		}
		now := c.now()
		if errors.Is(err, errTradeInsightPartial) {
			fresh.Status, fresh.Error, fresh.UpdatedAt = "ready", "partial_upstream_failure", now.Unix()
			ttl = time.Minute
		} else if err != nil {
			fresh = view
			if cached && current.view.UpdatedAt > 0 {
				fresh = current.view
				fresh.Status = "stale"
			}
			fresh.Error = "upstream_unavailable"
			ttl = time.Minute
		} else {
			fresh.Status, fresh.Error, fresh.UpdatedAt = "ready", "", now.Unix()
		}
		c.mu.Lock()
		c.cache[kind] = tradeInsightEntry{view: fresh, next: now.Add(ttl)}
		c.mu.Unlock()
		return fresh, nil
	})
	select {
	case <-ctx.Done():
		if exists && entry.view.UpdatedAt > 0 {
			view = entry.view
			view.Status = "stale"
		}
		view.Error = "request_cancelled"
		return view, true
	case r := <-result:
		return r.Val.(TradeInsightResponse), true
	}
}

func (c *tradeInsightClient) request(ctx context.Context, proxy, target string, body any, headers map[string]string, maxBytes int64, out any) error {
	method := http.MethodGet
	var data []byte
	var err error
	if body != nil {
		method = http.MethodPost
		data, err = common.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	client, err := c.transport(proxy)
	if err != nil {
		return err
	}
	// 不携带新闻凭据跟随重定向，避免第三方跳转把 key 发给其他域名。
	bounded := *client
	bounded.Timeout = 12 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(req)
	if err != nil {
		return errors.New("upstream transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream status %d", response.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return errors.New("upstream response too large")
	}
	return common.Unmarshal(data, out)
}

func tradeInsightSafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.String()
}

// 仅静态标的身份，不包含会过期的股价、市值、估值或管理层。as_of 留空：原种子未提供资料实际采集日期。
var tradeCompanyIdentities = []struct {
	Ticker   string `json:"ticker"`
	Name     string `json:"name"`
	Industry string `json:"industry"`
	Homepage string `json:"homepage"`
	AsOf     string `json:"as_of"`
}{
	{"NVDA", "Nvidia", "Semiconductors", "https://www.nvidia.com", ""},
	{"TSLA", "Tesla", "Automotive", "https://www.tesla.com", ""},
	{"AMD", "Advanced Micro Devices", "Semiconductors", "https://www.amd.com", ""},
	{"MU", "Micron Technology", "Semiconductors", "https://www.micron.com", ""},
	{"SNDK", "SanDisk", "Data storage", "https://www.sandisk.com", ""},
	{"CRCL", "Circle Internet Group", "Financial technology", "https://www.circle.com", ""},
	{"MSTR", "Strategy", "Software / Bitcoin treasury", "https://www.strategy.com", ""},
	{"SPCX", "SpaceX", "Aerospace", "https://www.spacex.com", ""},
	{"QQQ", "Invesco QQQ Trust", "ETF", "https://www.invesco.com", ""},
	{"SOXL", "Direxion Daily Semiconductor Bull 3X Shares", "ETF", "https://www.direxion.com", ""},
}
