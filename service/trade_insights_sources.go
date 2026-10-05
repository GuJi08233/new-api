package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type TradeCalendarEvent struct {
	ID       string `json:"id"`
	Time     int64  `json:"time"`
	Country  string `json:"country"`
	Currency string `json:"currency"`
	Title    string `json:"title"`
	Actual   string `json:"actual"`
	Forecast string `json:"forecast"`
	Previous string `json:"previous"`
}

func (c *tradeInsightClient) calendar(ctx context.Context, proxy string) ([]TradeCalendarEvent, error) {
	now := c.now().UTC()
	query := url.Values{"from": {now.Add(-72 * time.Hour).Format(time.RFC3339)}, "to": {now.Add(7 * 24 * time.Hour).Format(time.RFC3339)}, "minImportance": {"1"}}
	var response struct {
		Result *[]struct {
			ID       json.Number  `json:"id"`
			Date     string       `json:"date"`
			Country  string       `json:"country"`
			Currency string       `json:"currency"`
			Title    string       `json:"title"`
			Actual   *json.Number `json:"actual"`
			Forecast *json.Number `json:"forecast"`
			Previous *json.Number `json:"previous"`
			Scale    string       `json:"scale"`
			Unit     string       `json:"unit"`
		} `json:"result"`
	}
	if err := c.request(ctx, proxy, c.calendarURL+"?"+query.Encode(), nil, map[string]string{"Origin": "https://www.tradingview.com"}, 4<<20, &response); err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, errors.New("calendar result missing")
	}
	rows := make([]TradeCalendarEvent, 0, len(*response.Result))
	for _, event := range *response.Result {
		at, err := time.Parse(time.RFC3339, event.Date)
		if err != nil || event.ID == "" || event.Title == "" || len(event.Title) > 512 || len(event.Country) > 4 || len(event.Currency) > 8 {
			return nil, errors.New("invalid calendar event")
		}
		if at.Before(now.Add(-72*time.Hour)) || at.After(now.Add(7*24*time.Hour)) {
			continue
		}
		values := [3]string{}
		for i, n := range []*json.Number{event.Actual, event.Forecast, event.Previous} {
			if n == nil {
				continue
			}
			value, err := tradeInsightDecimal(n.String())
			if err != nil {
				return nil, err
			}
			values[i] = value.String()
			if event.Scale == "K" || event.Scale == "M" || event.Scale == "B" || event.Scale == "T" {
				values[i] += event.Scale
			}
			if event.Unit == "%" {
				values[i] += "%"
			}
		}
		rows = append(rows, TradeCalendarEvent{ID: event.ID.String(), Time: at.Unix(), Country: event.Country, Currency: event.Currency, Title: event.Title, Actual: values[0], Forecast: values[1], Previous: values[2]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Time < rows[j].Time })
	if len(rows) > 500 {
		rows = rows[:500]
	}
	return rows, nil
}

type TradeNewsFlash struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
	Time    int64  `json:"time"`
}

var tradeNewsHTML = regexp.MustCompile(`<[^>]*>`)

func (c *tradeInsightClient) news(ctx context.Context, proxy, key string) ([]TradeNewsFlash, error) {
	var response struct {
		Status *int `json:"status"`
		Data   struct {
			Data *[]struct {
				ID         int64  `json:"id"`
				Title      string `json:"title"`
				Content    string `json:"content"`
				URL        string `json:"url"`
				CreateTime string `json:"create_time"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := c.request(ctx, proxy, c.newsURL+"?page=1&size=20&lang=cn", nil, map[string]string{"api-key": key}, 2<<20, &response); err != nil {
		return nil, err
	}
	if response.Status == nil || *response.Status != 0 || response.Data.Data == nil {
		return nil, errors.New("invalid news response")
	}
	rows := make([]TradeNewsFlash, 0, 20)
	for _, item := range *response.Data.Data {
		// 原站 cn 快讯为北京时间的墙钟时间，带时区的字符串则尊重源的时区。
		at, err := time.Parse(time.RFC3339, item.CreateTime)
		if err != nil {
			at, err = time.ParseInLocation("2006-01-02 15:04:05", item.CreateTime, time.FixedZone("Asia/Shanghai", 8*3600))
		}
		if err != nil || item.ID <= 0 || item.Title == "" || len(item.Title) > 1024 || len(item.Content) > 32000 {
			return nil, errors.New("invalid news item")
		}
		rows = append(rows, TradeNewsFlash{ID: item.ID, Title: html.UnescapeString(tradeNewsHTML.ReplaceAllString(item.Title, "")), Content: html.UnescapeString(tradeNewsHTML.ReplaceAllString(item.Content, "")), URL: tradeInsightSafeURL(item.URL), Time: at.Unix()})
		if len(rows) == 20 {
			break
		}
	}
	return rows, nil
}

type TradeWhaleCoin struct {
	Coin          string `json:"coin"`
	LongNotional  string `json:"long_notional"`
	ShortNotional string `json:"short_notional"`
	LongCount     int    `json:"long_count"`
	ShortCount    int    `json:"short_count"`
}

// 超长指数也是上游不可信输入，不允许 decimal 在转换时分配无界内存。
func tradeInsightDecimal(raw string) (decimal.Decimal, error) {
	if raw == "" || len(raw) > 64 || strings.ContainsAny(raw, "eE") {
		return decimal.Zero, errors.New("invalid source number")
	}
	d, err := decimal.NewFromString(raw)
	if err != nil || d.Abs().GreaterThan(decimal.NewFromInt(1_000_000_000_000_000)) {
		return decimal.Zero, errors.New("invalid source number")
	}
	return d, nil
}

var errTradeInsightPartial = errors.New("partial upstream failure")

// 排行榜目前约 40MB，地址池按原站方式每日刷新，仓位另按十分钟刷新。
// 下载上限仍有界，避免大文件每十分钟反复传输与解析。
func (c *tradeInsightClient) whaleSampleAddresses(ctx context.Context, proxy string) ([]string, error) {
	c.mu.Lock()
	addresses, at := c.whaleAddresses, c.whaleAddressesAt
	c.mu.Unlock()
	if len(addresses) > 0 && c.now().Before(at.Add(24*time.Hour)) {
		return addresses, nil
	}
	var board struct {
		Rows *[]struct {
			Address string `json:"ethAddress"`
			Value   string `json:"accountValue"`
		} `json:"leaderboardRows"`
	}
	if err := c.request(ctx, proxy, c.leaderboardURL, nil, nil, 64<<20, &board); err != nil {
		return nil, err
	}
	if board.Rows == nil {
		return nil, errors.New("leaderboard missing")
	}
	type candidate struct {
		address string
		value   decimal.Decimal
	}
	candidates := make([]candidate, 0)
	seen := map[string]bool{}
	for _, row := range *board.Rows {
		address := strings.ToLower(row.Address)
		if len(address) != 42 || !strings.HasPrefix(address, "0x") || seen[address] {
			continue
		}
		if _, err := hex.DecodeString(address[2:]); err != nil {
			continue
		}
		value, err := tradeInsightDecimal(row.Value)
		if err != nil || value.LessThan(decimal.NewFromInt(1_000_000)) {
			continue
		}
		seen[address] = true
		candidates = append(candidates, candidate{address: address, value: value})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].value.GreaterThan(candidates[j].value) })
	if len(candidates) > 20 {
		candidates = candidates[:20]
	}
	if len(candidates) == 0 {
		return nil, errors.New("no eligible sample")
	}
	addresses = make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		addresses = append(addresses, candidate.address)
	}
	c.mu.Lock()
	c.whaleAddresses, c.whaleAddressesAt = addresses, c.now()
	c.mu.Unlock()
	return addresses, nil
}

func (c *tradeInsightClient) whales(ctx context.Context, proxy string) ([]TradeWhaleCoin, int, error) {
	addresses, err := c.whaleSampleAddresses(ctx, proxy)
	if err != nil {
		return nil, 0, err
	}
	coins := map[string]*TradeWhaleCoin{}
	for _, name := range []string{"BTC", "ETH", "SOL", "XRP", "DOGE"} {
		coins[name] = &TradeWhaleCoin{Coin: name, LongNotional: "0", ShortNotional: "0"}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	successes := 0
	for _, address := range addresses {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var state struct {
				Positions *[]struct {
					Position struct {
						Coin  string `json:"coin"`
						Size  string `json:"szi"`
						Value string `json:"positionValue"`
					} `json:"position"`
				} `json:"assetPositions"`
			}
			if err := c.request(ctx, proxy, c.hyperliquidURL, map[string]string{"type": "clearinghouseState", "user": address}, nil, 1<<20, &state); err != nil || state.Positions == nil {
				return
			}
			type position struct {
				coin        string
				size, value decimal.Decimal
			}
			positions := make([]position, 0)
			for _, raw := range *state.Positions {
				if _, tracked := coins[raw.Position.Coin]; !tracked {
					continue
				}
				size, err := tradeInsightDecimal(raw.Position.Size)
				if err != nil {
					return
				}
				value, err := tradeInsightDecimal(raw.Position.Value)
				if err != nil || value.IsNegative() {
					return
				}
				if size.IsZero() || value.LessThan(decimal.NewFromInt(100_000)) {
					continue
				}
				positions = append(positions, position{raw.Position.Coin, size, value})
			}
			mu.Lock()
			defer mu.Unlock()
			successes++
			for _, p := range positions {
				coin := coins[p.coin]
				if p.size.IsPositive() {
					value, _ := decimal.NewFromString(coin.LongNotional)
					coin.LongNotional = value.Add(p.value).String()
					coin.LongCount++
				} else {
					value, _ := decimal.NewFromString(coin.ShortNotional)
					coin.ShortNotional = value.Add(p.value).String()
					coin.ShortCount++
				}
			}
		}(address)
	}
	wg.Wait()
	if successes == 0 {
		return nil, 0, errors.New("all sampled accounts failed")
	}
	rows := make([]TradeWhaleCoin, 0, 5)
	for _, name := range []string{"BTC", "ETH", "SOL", "XRP", "DOGE"} {
		rows = append(rows, *coins[name])
	}
	if successes < len(addresses) {
		return rows, successes, errTradeInsightPartial
	}
	return rows, successes, nil
}
