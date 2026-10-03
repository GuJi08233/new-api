package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
)

const (
	// maxResponseBytes 是一个 REST 响应体或一帧推送最多读多少字节。
	maxResponseBytes = 8 << 20
	// maxErrorMsgBytes 是错误响应体不是 Binance 的错误 JSON 时，APIError.Msg 最多保留多少字节。
	maxErrorMsgBytes = 200
	// defaultCooldown 是被限频而响应里没有 Retry-After 时暂停多久。
	defaultCooldown = time.Minute
	// maxCooldownSeconds 是 Retry-After 的上限。Binance 封 IP 最长 3 天，上限只是挡住离谱的值。
	maxCooldownSeconds = 3 * 24 * 60 * 60
)

// ErrRateLimited 表示被 Binance 限频(HTTP 429，或者封 IP 的 418)后还在冷却期，请求没有发出去。
var ErrRateLimited = errors.New("binance: rate limited, cooling down")

// APIError 是 Binance 的非 2xx 响应。Code 与 Msg 取自 {"code":-1121,"msg":"Invalid symbol."}；响应体不是这种 JSON 时
// Code 为 0，Msg 是截到 200 字节的响应体。Status 为 451 表示服务器所在地区被 Binance 限制，要换行情镜像或者走代理。
type APIError struct {
	Status int
	Code   int
	Msg    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("binance: http %d, code %d: %s", e.Status, e.Code, e.Msg)
}

// Is 让 429 与 418 的 APIError 也满足 errors.Is(err, ErrRateLimited)：触发冷却的这次请求与冷却期内的请求可以一样处理。
func (e *APIError) Is(target error) bool {
	return target == ErrRateLimited && (e.Status == http.StatusTooManyRequests || e.Status == http.StatusTeapot)
}

// Client 是 Binance 现货或 U 本位合约行情的 REST 客户端，可以被多个 goroutine 并发使用。
type Client struct {
	baseURL string
	http    *http.Client
	// futures 为真时请求合约的 /fapi 接口。
	futures bool

	mu        sync.Mutex
	coolUntil time.Time // 被限频后，在这之前的请求直接返回 ErrRateLimited
}

// NewClient 创建 REST 客户端。baseURL 例如 https://data-api.binance.vision；httpClient 为 nil 时用 15 秒超时的默认客户端。
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// NewFuturesClient 创建 U 本位合约行情的 REST 客户端。baseURL 例如 https://fapi.binance.com。
func NewFuturesClient(baseURL string, httpClient *http.Client) *Client {
	client := NewClient(baseURL, httpClient)
	client.futures = true
	return client
}

// endpoint 返回现货或合约的接口路径，name 例如 klines。
func (c *Client) endpoint(name string) string {
	if c.futures {
		return "/fapi/v1/" + name
	}
	return "/api/v3/" + name
}

// get 发一个 GET 请求，把 2xx 响应体解码到 out。被限频(429/418)后按 Retry-After(没有时 60 秒)冷却，冷却期内直接返回
// ErrRateLimited、不发请求：被限频后还接着请求，Binance 会封 IP，而且封得越来越久。
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	c.mu.Lock()
	cooling := time.Now().Before(c.coolUntil)
	c.mu.Unlock()
	if cooling {
		return ErrRateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
		cooldown := defaultCooldown
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			cooldown = time.Duration(min(seconds, maxCooldownSeconds)) * time.Second
		}
		c.mu.Lock()
		if until := time.Now().Add(cooldown); until.After(c.coolUntil) {
			c.coolUntil = until
		}
		c.mu.Unlock()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("binance %s: read response: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var payload struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if common.Unmarshal(body, &payload) == nil && payload.Msg != "" {
			return &APIError{Status: resp.StatusCode, Code: payload.Code, Msg: payload.Msg}
		}
		return &APIError{Status: resp.StatusCode, Msg: strings.TrimSpace(string(body[:min(len(body), maxErrorMsgBytes)]))}
	}
	if err := common.Unmarshal(body, out); err != nil {
		return fmt.Errorf("binance %s: decode response: %w", path, err)
	}
	return nil
}

// Klines 查 K 线，按开盘时间从早到晚。limit <= 0 时用 Binance 的默认条数(500)，endTime(毫秒) <= 0 时查到最新一根。
func (c *Client) Klines(ctx context.Context, symbol, interval string, limit int, endTime int64) ([]Kline, error) {
	query := url.Values{"symbol": {symbol}, "interval": {interval}}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if endTime > 0 {
		query.Set("endTime", strconv.FormatInt(endTime, 10))
	}
	// 每根 K 线是一个数组：[开盘时间, "开", "高", "低", "收", "成交量", 收盘时间, "成交额", 成交笔数, ...]，
	// 时间与笔数是数字，价格与数量是字符串。
	var rows [][]json.RawMessage
	if err := c.get(ctx, c.endpoint("klines"), query, &rows); err != nil {
		return nil, err
	}
	klines := make([]Kline, 0, len(rows))
	for _, row := range rows {
		var kline Kline
		var open, high, low, closePrice, volume, quoteVolume string
		fields := []any{&kline.OpenTime, &open, &high, &low, &closePrice, &volume, &kline.CloseTime, &quoteVolume, &kline.Trades}
		if len(row) < len(fields) {
			return nil, fmt.Errorf("binance klines: row has %d fields, want at least %d", len(row), len(fields))
		}
		for i, field := range fields {
			if err := common.Unmarshal(row[i], field); err != nil {
				return nil, fmt.Errorf("binance klines: field %d: %w", i, err)
			}
		}
		var p decimalParser
		kline.Open = p.parse("open", open)
		kline.High = p.parse("high", high)
		kline.Low = p.parse("low", low)
		kline.Close = p.parse("close", closePrice)
		kline.Volume = p.parse("volume", volume)
		kline.QuoteVolume = p.parse("quoteVolume", quoteVolume)
		if p.err != nil {
			return nil, fmt.Errorf("binance klines: %w", p.err)
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

// Tickers 查多个交易对的 24 小时迷你行情。symbols 为空时直接返回空结果：不带 symbols 参数查的是全市场。合约接口不支持按列表查，
// 查全市场再挑出要的。
func (c *Client) Tickers(ctx context.Context, symbols []string) ([]Ticker, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	query := url.Values{}
	if !c.futures {
		list, err := common.Marshal(symbols)
		if err != nil {
			return nil, err
		}
		query = url.Values{"symbols": {string(list)}, "type": {"MINI"}}
	}
	var rows []struct {
		Symbol      string `json:"symbol"`
		OpenPrice   string `json:"openPrice"`
		HighPrice   string `json:"highPrice"`
		LowPrice    string `json:"lowPrice"`
		LastPrice   string `json:"lastPrice"`
		Volume      string `json:"volume"`
		QuoteVolume string `json:"quoteVolume"`
	}
	if err := c.get(ctx, c.endpoint("ticker/24hr"), query, &rows); err != nil {
		return nil, err
	}
	tickers := make([]Ticker, 0, len(symbols))
	for _, row := range rows {
		if !slices.Contains(symbols, row.Symbol) {
			continue
		}
		var p decimalParser
		ticker := Ticker{
			Symbol:      row.Symbol,
			Close:       p.parse("lastPrice", row.LastPrice),
			Open:        p.parse("openPrice", row.OpenPrice),
			High:        p.parse("highPrice", row.HighPrice),
			Low:         p.parse("lowPrice", row.LowPrice),
			Volume:      p.parse("volume", row.Volume),
			QuoteVolume: p.parse("quoteVolume", row.QuoteVolume),
		}
		if p.err != nil {
			return nil, fmt.Errorf("binance ticker %s: %w", row.Symbol, p.err)
		}
		tickers = append(tickers, ticker)
	}
	return tickers, nil
}

// ExchangeInfo 查交易对的交易规则，按交易对索引。symbols 为空时直接返回空结果：不带 symbols 参数查的是全市场。合约接口不支持
// 按列表查，查全市场(约 1 MB)再挑出要的。
func (c *Client) ExchangeInfo(ctx context.Context, symbols []string) (map[string]SymbolInfo, error) {
	if len(symbols) == 0 {
		return map[string]SymbolInfo{}, nil
	}
	query := url.Values{}
	if !c.futures {
		list, err := common.Marshal(symbols)
		if err != nil {
			return nil, err
		}
		query.Set("symbols", string(list))
	}
	// 各种过滤器都解到同一个结构里，再按 filterType 只取用得到的几种：这几个字段名在所有过滤器里都是字符串。
	var resp struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			Filters    []struct {
				FilterType     string `json:"filterType"`
				TickSize       string `json:"tickSize"`
				StepSize       string `json:"stepSize"`
				MinQty         string `json:"minQty"`
				MaxQty         string `json:"maxQty"`
				MinNotional    string `json:"minNotional"`
				MaxNotional    string `json:"maxNotional"`
				Notional       string `json:"notional"`
				MultiplierUp   string `json:"multiplierUp"`
				MultiplierDown string `json:"multiplierDown"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := c.get(ctx, c.endpoint("exchangeInfo"), query, &resp); err != nil {
		return nil, err
	}
	infos := make(map[string]SymbolInfo, len(symbols))
	for _, symbol := range resp.Symbols {
		if !slices.Contains(symbols, symbol.Symbol) {
			continue
		}
		info := SymbolInfo{Symbol: symbol.Symbol, BaseAsset: symbol.BaseAsset, QuoteAsset: symbol.QuoteAsset, Status: symbol.Status}
		var p decimalParser
		hasNotional := false
		var legacyMinNotional decimal.Decimal
		for _, filter := range symbol.Filters {
			switch filter.FilterType {
			case "PRICE_FILTER":
				info.TickSize = p.parse("tickSize", filter.TickSize)
			case "LOT_SIZE":
				info.StepSize = p.parse("stepSize", filter.StepSize)
				info.MinQty = p.parse("minQty", filter.MinQty)
				info.MaxQty = p.parse("maxQty", filter.MaxQty)
			case "MARKET_LOT_SIZE":
				info.MarketMaxQty = p.parse("maxQty", filter.MaxQty)
			case "NOTIONAL":
				hasNotional = true
				info.MinNotional = p.parse("minNotional", filter.MinNotional)
				info.MaxNotional = p.parse("maxNotional", filter.MaxNotional)
			case "MIN_NOTIONAL":
				if c.futures {
					legacyMinNotional = p.parse("notional", filter.Notional)
				} else {
					legacyMinNotional = p.parse("minNotional", filter.MinNotional)
				}
			case "PERCENT_PRICE":
				info.PriceUp = p.parse("multiplierUp", filter.MultiplierUp)
				info.PriceDown = p.parse("multiplierDown", filter.MultiplierDown)
			}
		}
		if p.err != nil {
			return nil, fmt.Errorf("binance exchangeInfo %s: %w", symbol.Symbol, p.err)
		}
		if !hasNotional {
			info.MinNotional = legacyMinNotional
		}
		infos[symbol.Symbol] = info
	}
	return infos, nil
}

// Depth 查盘口快照。limit <= 0 时用 Binance 的默认档数(100)。
func (c *Client) Depth(ctx context.Context, symbol string, limit int) (*Depth, error) {
	query := url.Values{"symbol": {symbol}}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var payload depthPayload
	if err := c.get(ctx, c.endpoint("depth"), query, &payload); err != nil {
		return nil, err
	}
	depth, err := payload.depth()
	if err != nil {
		return nil, fmt.Errorf("binance depth %s: %w", symbol, err)
	}
	return depth, nil
}

// MarkPrices 查合约全部交易对当前的标记价格与资金费率，挑出 symbols 里的，只用于合约客户端。连上推送之后补一份：标记价格
// 每秒推一次，刚连上的这一秒里没有。
func (c *Client) MarkPrices(ctx context.Context, symbols []string) ([]MarkPrice, error) {
	if !c.futures {
		return nil, errors.New("binance: mark prices are only available on the futures api")
	}
	var rows []struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		Time            int64  `json:"time"`
	}
	if err := c.get(ctx, "/fapi/v1/premiumIndex", url.Values{}, &rows); err != nil {
		return nil, err
	}
	marks := make([]MarkPrice, 0, len(symbols))
	for _, row := range rows {
		if !slices.Contains(symbols, row.Symbol) {
			continue
		}
		var p decimalParser
		mark := MarkPrice{
			Symbol:          row.Symbol,
			EventTime:       row.Time,
			Mark:            p.parse("markPrice", row.MarkPrice),
			Index:           p.parse("indexPrice", row.IndexPrice),
			FundingRate:     p.parseSigned("lastFundingRate", row.LastFundingRate),
			NextFundingTime: row.NextFundingTime,
		}
		if p.err != nil {
			return nil, fmt.Errorf("binance premiumIndex %s: %w", row.Symbol, p.err)
		}
		marks = append(marks, mark)
	}
	return marks, nil
}

// FundingRates 查一个合约在 startTime(毫秒，含)之后已经结算的资金费，按结算时间从早到晚，最多 limit 条(<= 0 时用 Binance 的
// 默认 100 条)。只用于合约客户端。
func (c *Client) FundingRates(ctx context.Context, symbol string, startTime int64, limit int) ([]FundingRate, error) {
	if !c.futures {
		return nil, errors.New("binance: funding rates are only available on the futures api")
	}
	query := url.Values{"symbol": {symbol}, "startTime": {strconv.FormatInt(startTime, 10)}}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var rows []struct {
		Symbol      string `json:"symbol"`
		FundingTime int64  `json:"fundingTime"`
		FundingRate string `json:"fundingRate"`
		MarkPrice   string `json:"markPrice"`
	}
	if err := c.get(ctx, "/fapi/v1/fundingRate", query, &rows); err != nil {
		return nil, err
	}
	rates := make([]FundingRate, 0, len(rows))
	for _, row := range rows {
		var p decimalParser
		rate := FundingRate{
			Symbol:      row.Symbol,
			FundingTime: row.FundingTime,
			Rate:        p.parseSigned("fundingRate", row.FundingRate),
			MarkPrice:   p.parse("markPrice", row.MarkPrice),
		}
		if p.err != nil {
			return nil, fmt.Errorf("binance fundingRate %s: %w", symbol, p.err)
		}
		rates = append(rates, rate)
	}
	return rates, nil
}
