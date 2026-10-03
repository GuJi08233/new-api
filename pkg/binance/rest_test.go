package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

// newRESTClient 起一个测试服务端，返回指向它的客户端。地址带着结尾的斜杠，请求路径仍然要对。
func newRESTClient(t *testing.T, handler http.HandlerFunc) *Client {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL+"/", nil)
}

func TestClientKlinesParsesRowsAndOmitsUnsetEndTime(t *testing.T) {
	queries := make(chan url.Values, 2)
	client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/klines", r.URL.Path)
		queries <- r.URL.Query()
		_, _ = w.Write([]byte(`[
			[1499040000000,"0.01634790","0.80000000","0.01575800","0.01577100","148976.11427815",1499644799999,"2434.19055334",308,"1756.87402397","28.46694368","0"],
			[1499644800000,"0.01577100","0.01600000","0.01500000","0.01590000","1000.5",1500249599999,"15.9",42,"500","8","0"]
		]`))
	})

	klines, err := client.Klines(context.Background(), "BTCUSDT", "1w", 2, 1500249599999)
	require.NoError(t, err)
	assert.Equal(t, []Kline{
		{OpenTime: 1499040000000, Open: d("0.01634790"), High: d("0.80000000"), Low: d("0.01575800"), Close: d("0.01577100"),
			Volume: d("148976.11427815"), CloseTime: 1499644799999, QuoteVolume: d("2434.19055334"), Trades: 308},
		{OpenTime: 1499644800000, Open: d("0.01577100"), High: d("0.01600000"), Low: d("0.01500000"), Close: d("0.01590000"),
			Volume: d("1000.5"), CloseTime: 1500249599999, QuoteVolume: d("15.9"), Trades: 42},
	}, klines)
	assert.Equal(t, url.Values{"symbol": {"BTCUSDT"}, "interval": {"1w"}, "limit": {"2"}, "endTime": {"1500249599999"}}, <-queries)

	// endTime 为 0 时不能发 endTime=0：那是 1970 年，一根 K 线都查不到。
	_, err = client.Klines(context.Background(), "BTCUSDT", "1w", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, url.Values{"symbol": {"BTCUSDT"}, "interval": {"1w"}, "limit": {"2"}}, <-queries)
}

func TestClientTickersSendsSymbolsAsJSONArray(t *testing.T) {
	var requests atomic.Int32
	client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/api/v3/ticker/24hr", r.URL.Path)
		assert.Equal(t, "MINI", r.URL.Query().Get("type"))
		var symbols []string
		assert.NoError(t, common.UnmarshalJsonStr(r.URL.Query().Get("symbols"), &symbols))
		assert.Equal(t, []string{"BTCUSDT", "NVDABUSDT"}, symbols)
		_, _ = w.Write([]byte(`[
			{"symbol":"BTCUSDT","openPrice":"64000.00000000","highPrice":"65500.00000000","lowPrice":"63900.50000000","lastPrice":"65000.10000000",
			 "volume":"1234.50000000","quoteVolume":"80000000.25000000","openTime":1699900000000,"closeTime":1699986399999,"firstId":1,"lastId":2,"count":2},
			{"symbol":"NVDABUSDT","openPrice":"180.10","highPrice":"182.00","lowPrice":"179.50","lastPrice":"181.25",
			 "volume":"52.3","quoteVolume":"9480.1","openTime":1699900000000,"closeTime":1699986399999,"firstId":-1,"lastId":-1,"count":0}
		]`))
	})

	tickers, err := client.Tickers(context.Background(), []string{"BTCUSDT", "NVDABUSDT"})
	require.NoError(t, err)
	assert.Equal(t, []Ticker{
		{Symbol: "BTCUSDT", Close: d("65000.10000000"), Open: d("64000.00000000"), High: d("65500.00000000"), Low: d("63900.50000000"),
			Volume: d("1234.50000000"), QuoteVolume: d("80000000.25000000")},
		{Symbol: "NVDABUSDT", Close: d("181.25"), Open: d("180.10"), High: d("182.00"), Low: d("179.50"), Volume: d("52.3"), QuoteVolume: d("9480.1")},
	}, tickers)

	// 不带 symbols 参数查的是全市场，空列表不能发出请求。
	tickers, err = client.Tickers(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, tickers)
	assert.Equal(t, int32(1), requests.Load())
}

func TestClientExchangeInfoReadsFilters(t *testing.T) {
	client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/exchangeInfo", r.URL.Path)
		var symbols []string
		assert.NoError(t, common.UnmarshalJsonStr(r.URL.Query().Get("symbols"), &symbols))
		assert.Equal(t, []string{"BTCUSDT", "OLDUSDT"}, symbols)
		// BTCUSDT 同时有旧的 MIN_NOTIONAL 与新的 NOTIONAL，以 NOTIONAL 为准；MARKET_LOT_SIZE 单独记下，不能覆盖 LOT_SIZE。
		// OLDUSDT 只有 MIN_NOTIONAL，也没有 LOT_SIZE。
		_, _ = w.Write([]byte(`{"timezone":"UTC","serverTime":1565246363776,"rateLimits":[],"exchangeFilters":[],"symbols":[
			{"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","baseAssetPrecision":8,"quoteAsset":"USDT","quotePrecision":8,"filters":[
				{"filterType":"PRICE_FILTER","minPrice":"0.01000000","maxPrice":"1000000.00000000","tickSize":"0.01000000"},
				{"filterType":"LOT_SIZE","minQty":"0.00001000","maxQty":"9000.00000000","stepSize":"0.00001000"},
				{"filterType":"ICEBERG_PARTS","limit":10},
				{"filterType":"MARKET_LOT_SIZE","minQty":"0.00000000","maxQty":"119.23000000","stepSize":"0.00000000"},
				{"filterType":"MIN_NOTIONAL","minNotional":"1.00000000","applyToMarket":true,"avgPriceMins":5},
				{"filterType":"NOTIONAL","minNotional":"5.00000000","applyMinToMarket":true,"maxNotional":"9000000.00000000","applyMaxToMarket":false,"avgPriceMins":5},
				{"filterType":"MAX_NUM_ORDERS","maxNumOrders":200}
			],"permissionSets":[["SPOT"]]},
			{"symbol":"OLDUSDT","status":"BREAK","baseAsset":"OLD","quoteAsset":"USDT","filters":[
				{"filterType":"PRICE_FILTER","minPrice":"0.00010000","maxPrice":"1000.00000000","tickSize":"0.00010000"},
				{"filterType":"MIN_NOTIONAL","minNotional":"10.00000000","applyToMarket":true,"avgPriceMins":5}
			]}
		]}`))
	})

	infos, err := client.ExchangeInfo(context.Background(), []string{"BTCUSDT", "OLDUSDT"})
	require.NoError(t, err)
	assert.Equal(t, map[string]SymbolInfo{
		"BTCUSDT": {Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", Status: "TRADING", TickSize: d("0.01000000"),
			StepSize: d("0.00001000"), MinQty: d("0.00001000"), MaxQty: d("9000.00000000"), MarketMaxQty: d("119.23000000"),
			MinNotional: d("5.00000000"), MaxNotional: d("9000000.00000000")},
		"OLDUSDT": {Symbol: "OLDUSDT", BaseAsset: "OLD", QuoteAsset: "USDT", Status: "BREAK", TickSize: d("0.00010000"), MinNotional: d("10.00000000")},
	}, infos)
}

func TestClientDepthKeepsBestPriceFirst(t *testing.T) {
	client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/depth", r.URL.Path)
		assert.Equal(t, url.Values{"symbol": {"BNBBTC"}, "limit": {"5"}}, r.URL.Query())
		_, _ = w.Write([]byte(`{"lastUpdateId":1027024,"bids":[["4.00000000","431.00000000"],["3.99000000","9.00000000"]],
			"asks":[["4.00000200","12.00000000"],["4.10000000","1.50000000"]]}`))
	})

	depth, err := client.Depth(context.Background(), "BNBBTC", 5)
	require.NoError(t, err)
	assert.Equal(t, &Depth{
		LastUpdateID: 1027024,
		Bids:         []Level{{Price: d("4.00000000"), Qty: d("431.00000000")}, {Price: d("3.99000000"), Qty: d("9.00000000")}},
		Asks:         []Level{{Price: d("4.00000200"), Qty: d("12.00000000")}, {Price: d("4.10000000"), Qty: d("1.50000000")}},
	}, depth)
}

func TestClientReturnsAPIErrors(t *testing.T) {
	restricted := "Service unavailable from a restricted location according to 'b. Eligibility' in https://www.binance.com/en/terms. " +
		"Please contact customer service if you believe you received this message in error."
	page := strings.Repeat("0123456789", 30)
	tests := []struct {
		name   string
		status int
		body   string
		want   *APIError
	}{
		{name: "binance error json", status: http.StatusBadRequest, body: `{"code":-1121,"msg":"Invalid symbol."}`,
			want: &APIError{Status: http.StatusBadRequest, Code: -1121, Msg: "Invalid symbol."}},
		{name: "restricted location json", status: http.StatusUnavailableForLegalReasons, body: `{"code":0,"msg":"` + restricted + `"}`,
			want: &APIError{Status: http.StatusUnavailableForLegalReasons, Msg: restricted}},
		{name: "plain body is truncated", status: http.StatusUnavailableForLegalReasons, body: page,
			want: &APIError{Status: http.StatusUnavailableForLegalReasons, Msg: page[:200]}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})
			_, err := client.Depth(context.Background(), "BTCUSDT", 5)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, test.want, apiErr)
			assert.NotErrorIs(t, err, ErrRateLimited)
		})
	}
}

func TestClientRejectsInvalidDecimals(t *testing.T) {
	bodies := map[string]string{
		"/api/v3/depth":       `{"lastUpdateId":1,"bids":[["1.2.3","1"]],"asks":[]}`,
		"/api/v3/ticker/24hr": `[{"symbol":"BTCUSDT","openPrice":"1","highPrice":"1","lowPrice":"1","volume":"1","quoteVolume":"1"}]`,
		"/api/v3/klines":      `[[1,"1","1","1","1","-5",2,"1",3,"0","0","0"]]`,
		"/api/v3/exchangeInfo": `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT",
			"filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01x"}]}]}`,
	}
	client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodies[r.URL.Path]))
	})
	ctx := context.Background()

	_, err := client.Depth(ctx, "BTCUSDT", 5)
	assert.ErrorContains(t, err, `"1.2.3"`)
	// 缺字段不能当成 0 价格
	_, err = client.Tickers(ctx, []string{"BTCUSDT"})
	assert.ErrorContains(t, err, "lastPrice")
	_, err = client.Klines(ctx, "BTCUSDT", "1m", 1, 0)
	assert.ErrorContains(t, err, `"-5"`)
	_, err = client.ExchangeInfo(ctx, []string{"BTCUSDT"})
	assert.ErrorContains(t, err, `"0.01x"`)
}

func TestClientCoolsDownAfterRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
	}{
		{name: "429 with Retry-After", status: http.StatusTooManyRequests, retryAfter: "2"},
		{name: "418 ip ban without Retry-After", status: http.StatusTeapot},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			client := newRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"code":-1003,"msg":"Too many requests."}`))
			})
			ctx := context.Background()

			_, err := client.Depth(ctx, "BTCUSDT", 5)
			require.ErrorIs(t, err, ErrRateLimited)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, &APIError{Status: test.status, Code: -1003, Msg: "Too many requests."}, apiErr)

			// 冷却期内任何请求都不再发出
			_, err = client.Klines(ctx, "BTCUSDT", "1m", 10, 0)
			assert.Equal(t, ErrRateLimited, err)
			_, err = client.Tickers(ctx, []string{"BTCUSDT"})
			assert.Equal(t, ErrRateLimited, err)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

// 合约接口不能按列表查交易规则与 24 小时行情，查全市场后只留下要的交易对；合约的 MIN_NOTIONAL 用 notional 字段。
func TestFuturesClientReadsRulesAndTickers(t *testing.T) {
	client := NewFuturesClient("", nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("symbols"))
		switch r.URL.Path {
		case "/fapi/v1/exchangeInfo":
			_, _ = w.Write([]byte(`{"timezone":"UTC","symbols":[
				{"symbol":"NVDAUSDT","status":"TRADING","baseAsset":"NVDA","quoteAsset":"USDT","contractType":"TRADIFI_PERPETUAL","filters":[
					{"filterType":"PRICE_FILTER","minPrice":"0.01","maxPrice":"100000","tickSize":"0.01000"},
					{"filterType":"LOT_SIZE","minQty":"0.01","maxQty":"10000","stepSize":"0.01"},
					{"filterType":"MARKET_LOT_SIZE","minQty":"0.01","maxQty":"3000","stepSize":"0.01"},
					{"filterType":"MAX_NUM_ORDERS","limit":200},
					{"filterType":"MIN_NOTIONAL","notional":"5"},
					{"filterType":"PERCENT_PRICE","multiplierUp":"1.0300","multiplierDown":"0.9700","multiplierDecimal":"4"}
				]},
				{"symbol":"ETHUSDT","status":"TRADING","baseAsset":"ETH","quoteAsset":"USDT","filters":[]}
			]}`))
		case "/fapi/v1/ticker/24hr":
			_, _ = w.Write([]byte(`[
				{"symbol":"NVDAUSDT","priceChange":"-1.71","lastPrice":"234.74","openPrice":"236.45","highPrice":"236.62","lowPrice":"233.86","volume":"193573.17","quoteVolume":"45434328.77"},
				{"symbol":"ETHUSDT","lastPrice":"3000","openPrice":"2900","highPrice":"3100","lowPrice":"2800","volume":"1","quoteVolume":"3000"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client.baseURL = server.URL

	infos, err := client.ExchangeInfo(context.Background(), []string{"NVDAUSDT"})
	require.NoError(t, err)
	assert.Equal(t, map[string]SymbolInfo{"NVDAUSDT": {Symbol: "NVDAUSDT", BaseAsset: "NVDA", QuoteAsset: "USDT", Status: "TRADING",
		TickSize: d("0.01000"), StepSize: d("0.01"), MinQty: d("0.01"), MaxQty: d("10000"), MarketMaxQty: d("3000"),
		MinNotional: d("5"), PriceUp: d("1.0300"), PriceDown: d("0.9700")}}, infos)

	tickers, err := client.Tickers(context.Background(), []string{"NVDAUSDT"})
	require.NoError(t, err)
	assert.Equal(t, []Ticker{{Symbol: "NVDAUSDT", Close: d("234.74"), Open: d("236.45"), High: d("236.62"), Low: d("233.86"),
		Volume: d("193573.17"), QuoteVolume: d("45434328.77")}}, tickers)
}

// 标记价格与已结算的资金费率只在合约接口上有，资金费率可以为负。
func TestFuturesClientReadsMarkPricesAndFundingRates(t *testing.T) {
	var fundingQuery url.Values
	client := NewFuturesClient("", nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`[
				{"symbol":"BTCUSDT","markPrice":"84756.54044928","indexPrice":"84788.51847826","estimatedSettlePrice":"84834.33","lastFundingRate":"0.00005250","interestRate":"0.0001","nextFundingTime":1791072000000,"time":1791044697000},
				{"symbol":"ETHUSDT","markPrice":"3000","indexPrice":"3001","lastFundingRate":"-0.0001","nextFundingTime":1791072000000,"time":1791044697000}
			]`))
		case "/fapi/v1/fundingRate":
			fundingQuery = r.URL.Query()
			_, _ = w.Write([]byte(`[
				{"symbol":"BTCUSDT","fundingTime":1791043200000,"fundingRate":"-0.00002100","markPrice":"84500.10000000"},
				{"symbol":"BTCUSDT","fundingTime":1791072000001,"fundingRate":"0.00010000","markPrice":"84756.00000000"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client.baseURL = server.URL

	marks, err := client.MarkPrices(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	assert.Equal(t, []MarkPrice{{Symbol: "BTCUSDT", EventTime: 1791044697000, Mark: d("84756.54044928"), Index: d("84788.51847826"),
		FundingRate: d("0.00005250"), NextFundingTime: 1791072000000}}, marks)

	rates, err := client.FundingRates(context.Background(), "BTCUSDT", 1791043200000, 10)
	require.NoError(t, err)
	assert.Equal(t, url.Values{"symbol": {"BTCUSDT"}, "startTime": {"1791043200000"}, "limit": {"10"}}, fundingQuery)
	assert.Equal(t, []FundingRate{
		{Symbol: "BTCUSDT", FundingTime: 1791043200000, Rate: d("-0.00002100"), MarkPrice: d("84500.10000000")},
		{Symbol: "BTCUSDT", FundingTime: 1791072000001, Rate: d("0.00010000"), MarkPrice: d("84756.00000000")},
	}, rates)

	_, err = NewClient(server.URL, nil).FundingRates(context.Background(), "BTCUSDT", 0, 0)
	assert.Error(t, err, "the spot api has no funding rates")
}
