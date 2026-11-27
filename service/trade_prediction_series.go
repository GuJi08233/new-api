package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// TradePredictionMarketSpec 描述一个支持的多币种/多周期 Polymarket 涨跌预测市场。
type TradePredictionMarketSpec struct {
	MarketKey    string `json:"market_key"`
	Asset        string `json:"asset"`
	Interval     string `json:"interval"`
	IntervalSec  int64  `json:"interval_sec"`
	BinanceSpot  string `json:"binance_spot"`
	SlugPrefix   string `json:"-"`
	DefaultBtc5m bool   `json:"-"`
}

var TradePredictionMarkets = []TradePredictionMarketSpec{
	{MarketKey: "btc-5m", Asset: "BTC", Interval: "5m", IntervalSec: 300, BinanceSpot: "BTCUSDT", SlugPrefix: "btc-updown-5m-", DefaultBtc5m: true},
	{MarketKey: "eth-5m", Asset: "ETH", Interval: "5m", IntervalSec: 300, BinanceSpot: "ETHUSDT", SlugPrefix: "eth-updown-5m-"},
	{MarketKey: "sol-5m", Asset: "SOL", Interval: "5m", IntervalSec: 300, BinanceSpot: "SOLUSDT", SlugPrefix: "sol-updown-5m-"},
	{MarketKey: "xrp-5m", Asset: "XRP", Interval: "5m", IntervalSec: 300, BinanceSpot: "XRPUSDT", SlugPrefix: "xrp-updown-5m-"},
	{MarketKey: "btc-15m", Asset: "BTC", Interval: "15m", IntervalSec: 900, BinanceSpot: "BTCUSDT", SlugPrefix: "btc-updown-15m-"},
	{MarketKey: "eth-15m", Asset: "ETH", Interval: "15m", IntervalSec: 900, BinanceSpot: "ETHUSDT", SlugPrefix: "eth-updown-15m-"},
	{MarketKey: "sol-15m", Asset: "SOL", Interval: "15m", IntervalSec: 900, BinanceSpot: "SOLUSDT", SlugPrefix: "sol-updown-15m-"},
	{MarketKey: "xrp-15m", Asset: "XRP", Interval: "15m", IntervalSec: 900, BinanceSpot: "XRPUSDT", SlugPrefix: "xrp-updown-15m-"},
}

func ResolveTradePredictionMarketSpec(asset, interval string) (TradePredictionMarketSpec, bool) {
	a := strings.ToUpper(strings.TrimSpace(asset))
	iv := strings.ToLower(strings.TrimSpace(interval))
	if a == "" {
		a = "BTC"
	}
	if iv == "" {
		iv = "5m"
	}
	for _, spec := range TradePredictionMarkets {
		if spec.Asset == a && spec.Interval == iv {
			return spec, true
		}
	}
	return TradePredictionMarketSpec{}, false
}

type tradePredictionSeriesCacheEntry struct {
	view  TradePredictionMarketView
	until time.Time
}

var tradePredictionSeriesCache struct {
	mu      sync.Mutex
	entries map[string]tradePredictionSeriesCacheEntry
}

func seriesRoundToLegacy(round model.TradePredictionSeriesRound) model.TradePredictionRound {
	return model.TradePredictionRound{
		WindowStart:   round.WindowStart,
		EndTime:       round.EndTime,
		Slug:          round.Slug,
		Title:         round.Title,
		ConditionId:   round.ConditionId,
		UpToken:       round.UpToken,
		DownToken:     round.DownToken,
		Outcome:       round.Outcome,
		SettledAt:     round.SettledAt,
		LastCheckedAt: round.LastCheckedAt,
	}
}

func (client *tradePredictionClient) discoverSeries(ctx context.Context, spec TradePredictionMarketSpec, windowStart int64) (*model.TradePredictionSeriesRound, error) {
	slug := spec.SlugPrefix + strconv.FormatInt(windowStart, 10)
	var markets []struct {
		Slug            string `json:"slug"`
		Question        string `json:"question"`
		ConditionId     string `json:"conditionId"`
		Outcomes        string `json:"outcomes"`
		Tokens          string `json:"clobTokenIds"`
		EndDate         string `json:"endDate"`
		Closed          bool   `json:"closed"`
		AcceptingOrders bool   `json:"acceptingOrders"`
	}
	if err := client.getJSON(ctx, client.gammaURL+"/markets?slug="+url.QueryEscape(slug), &markets); err != nil {
		return nil, err
	}
	for _, market := range markets {
		if market.Slug != slug || market.Closed || !market.AcceptingOrders {
			continue
		}
		end, err := time.Parse(time.RFC3339Nano, market.EndDate)
		if err != nil || end.Unix() != windowStart+spec.IntervalSec {
			continue
		}
		var outcomes, tokens []string
		if common.UnmarshalJsonStr(market.Outcomes, &outcomes) != nil || common.UnmarshalJsonStr(market.Tokens, &tokens) != nil || len(outcomes) != 2 || len(tokens) != 2 {
			continue
		}
		round := model.TradePredictionSeriesRound{
			Slug:        slug,
			MarketKey:   spec.MarketKey,
			Asset:       spec.Asset,
			IntervalSec: spec.IntervalSec,
			WindowStart: windowStart,
			EndTime:     end.Unix(),
			Title:       market.Question,
			ConditionId: market.ConditionId,
		}
		for i, side := range outcomes {
			if tokens[i] == "" || len(tokens[i]) > 100 || strings.Trim(tokens[i], "0123456789") != "" {
				return nil, model.ErrTradePredictionQuote
			}
			switch strings.ToUpper(side) {
			case model.TradePredictionUp:
				round.UpToken = tokens[i]
			case model.TradePredictionDown:
				round.DownToken = tokens[i]
			}
		}
		if err := model.SaveTradePredictionSeriesRound(round); err != nil {
			return nil, err
		}
		return model.GetTradePredictionSeriesRound(slug)
	}
	return nil, model.ErrTradePredictionQuote
}

// GetTradePredictionMarketFor 支持多币种/多周期预测行情；默认 BTC 5m 走原实现保持完全兼容。
func GetTradePredictionMarketFor(ctx context.Context, asset, interval string) (TradePredictionMarketView, bool) {
	spec, ok := ResolveTradePredictionMarketSpec(asset, interval)
	if !ok {
		return TradePredictionMarketView{}, false
	}
	if spec.DefaultBtc5m {
		return tradePredictions.market(ctx), true
	}
	return tradePredictions.marketSeries(ctx, spec), true
}

func (client *tradePredictionClient) marketSeries(ctx context.Context, spec TradePredictionMarketSpec) TradePredictionMarketView {
	setting := operation_setting.GetTradeSetting()
	view := TradePredictionMarketView{
		Enabled:        setting.Enabled && setting.PredictionEnabled,
		Status:         "unavailable",
		Source:         "Polymarket",
		ServerTime:     common.GetTimestamp(),
		FeeRate:        predictionFeeRate.String(),
		MinAmountUsd:   1,
		MaxAmountUsd:   setting.MaxOrderUsd,
		MaxPositionUsd: setting.MaxPositionUsd,
	}
	windowStart := view.ServerTime / spec.IntervalSec * spec.IntervalSec
	slug := spec.SlugPrefix + strconv.FormatInt(windowStart, 10)
	if !view.Enabled {
		var active int64
		if err := model.DB.Model(&model.TradePredictionSeriesPosition{}).Where("slug = ? AND status = ?", slug, model.TradePredictionActive).Count(&active).Error; err != nil || active == 0 {
			view.Status = "disabled"
			return view
		}
	}
	tradePredictionSeriesCache.mu.Lock()
	cached := tradePredictionSeriesCache.entries[spec.MarketKey]
	tradePredictionSeriesCache.mu.Unlock()
	if time.Now().Before(cached.until) && cached.view.Round != nil && view.ServerTime < cached.view.Round.EndTime {
		out := cached.view
		out.ServerTime = view.ServerTime
		out.Enabled, out.MaxAmountUsd, out.MaxPositionUsd = view.Enabled, view.MaxAmountUsd, view.MaxPositionUsd
		return out
	}
	result, err, _ := client.group.Do("market:"+spec.MarketKey, func() (any, error) {
		round, err := model.GetTradePredictionSeriesRound(slug)
		if err != nil {
			round, err = client.discoverSeries(ctx, spec, windowStart)
		}
		if err != nil {
			return view, nil
		}
		legacy := seriesRoundToLegacy(*round)
		roundView := PredictionRoundView(legacy)
		view.Round = &roundView
		up, err := client.book(ctx, &legacy, model.TradePredictionUp)
		if err != nil {
			return view, nil
		}
		down, err := client.book(ctx, &legacy, model.TradePredictionDown)
		if err != nil {
			return view, nil
		}
		if time.Now().UnixMilli() > min(up.expiresAtMs, down.expiresAtMs) || common.GetTimestamp() >= round.EndTime {
			return view, nil
		}
		view.Up, view.Down = predictionBestPrices(up), predictionBestPrices(down)
		view.Status, view.UpdatedAt = "ready", common.GetTimestamp()
		tradePredictionSeriesCache.mu.Lock()
		if tradePredictionSeriesCache.entries == nil {
			tradePredictionSeriesCache.entries = map[string]tradePredictionSeriesCacheEntry{}
		}
		tradePredictionSeriesCache.entries[spec.MarketKey] = tradePredictionSeriesCacheEntry{view: view, until: time.Now().Add(time.Second)}
		tradePredictionSeriesCache.mu.Unlock()
		return view, nil
	})
	if err != nil {
		return view
	}
	return result.(TradePredictionMarketView)
}

// GetTradePredictionLiveFor 为非 BTC-5m 预测市场提供基于 Binance 实时价格与本轮开盘参考价的展示走势。
func GetTradePredictionLiveFor(ctx context.Context, asset, interval string, since int64) (TradePredictionLiveView, bool) {
	spec, ok := ResolveTradePredictionMarketSpec(asset, interval)
	if !ok {
		return TradePredictionLiveView{}, false
	}
	if spec.DefaultBtc5m {
		return GetTradePredictionLive(since), true
	}
	setting := operation_setting.GetTradeSetting()
	now := time.Now()
	windowStart := now.Unix() / spec.IntervalSec * spec.IntervalSec
	view := TradePredictionLiveView{
		Enabled:     setting.Enabled && setting.PredictionEnabled,
		WindowStart: windowStart,
		Points:      []TradePredictionPricePoint{},
		Trades:      []TradePredictionTrade{},
	}
	if !view.Enabled {
		return view, true
	}
	price, okPrice := currentAssetReferencePrice(spec.BinanceSpot)
	if okPrice {
		view.Connected = true
		view.Price, _ = price.Float64()
		view.PriceAt = now.UnixMilli()
	}
	// 用本轮对应的 1m K 线拼出目标价与走势点，仅用于展示。
	klines, err := tradeMarket.Klines(ctx, spec.BinanceSpot, "1m", 20, 0)
	if err != nil || len(klines) == 0 {
		klines, _ = futuresMarket.Klines(ctx, spec.BinanceSpot, "1m", 20, 0)
	}
	fromMs := windowStart*1000 - 60_000
	for _, bar := range klines {
		if bar.OpenTime == windowStart*1000 {
			view.OpenPrice, _ = bar.Open.Float64()
		}
		if bar.OpenTime >= fromMs && bar.OpenTime > since {
			v, _ := bar.Close.Float64()
			view.Points = append(view.Points, TradePredictionPricePoint{Time: bar.OpenTime, Price: v})
		}
	}
	if view.OpenPrice == 0 && len(view.Points) > 0 {
		view.OpenPrice = view.Points[0].Price
	}
	if okPrice && (len(view.Points) == 0 || view.PriceAt > view.Points[len(view.Points)-1].Time) && view.PriceAt > since {
		view.Points = append(view.Points, TradePredictionPricePoint{Time: view.PriceAt, Price: view.Price})
	}
	return view, true
}

func BuyTradePredictionSeries(ctx context.Context, userId int, spec TradePredictionMarketSpec, request TradePredictionOrderRequest) (*model.TradePredictionSeriesPosition, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.PredictionEnabled {
		return nil, ErrTradeDisabled
	}
	if (request.Side != model.TradePredictionUp && request.Side != model.TradePredictionDown) ||
		request.Amount.LessThan(decimal.NewFromInt(1)) ||
		request.Amount.GreaterThan(decimal.NewFromInt(int64(setting.MaxOrderUsd))) ||
		request.Amount.Exponent() < -8 {
		return nil, ErrTradeOrderInvalid
	}
	expectedWindow := common.GetTimestamp() / spec.IntervalSec * spec.IntervalSec
	if request.WindowStart != expectedWindow {
		return nil, model.ErrTradePredictionClosed
	}
	if err := EnsureTradeSpotPricesFresh(ctx, userId); err != nil {
		return nil, err
	}
	client := tradePredictions
	slug := spec.SlugPrefix + strconv.FormatInt(request.WindowStart, 10)
	round, err := model.GetTradePredictionSeriesRound(slug)
	if err != nil {
		round, err = client.discoverSeries(ctx, spec, request.WindowStart)
	}
	if err != nil {
		return nil, err
	}
	legacy := seriesRoundToLegacy(*round)
	depth := client.lockDepth(&legacy, request.Side)
	defer depth.Unlock()
	book, err := client.book(ctx, &legacy, request.Side)
	if err != nil {
		return nil, model.ErrTradePredictionQuote
	}
	if err := depth.acceptBook(book); err != nil {
		return nil, err
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	fill, err := matchPredictionBook(book, true, request.Amount, decimal.Zero, request.LimitPrice, perUsd, depth.asks)
	if err != nil {
		return nil, err
	}
	fill.UserId, fill.WindowStart, fill.Side, fill.Market = userId, request.WindowStart, request.Side, TradeAccountingMarket()
	fill.MaxCost, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.MaxPositionUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, model.ErrTradeAmountInvalid
	}
	position, err := model.BuyTradePredictionSeries(slug, spec.MarketKey, spec.Asset, spec.IntervalSec, fill.TradePredictionFill)
	if err == nil {
		for price, qty := range fill.takes {
			depth.asks[price] = depth.asks[price].Add(qty)
		}
	}
	return position, err
}

func SellTradePredictionSeries(ctx context.Context, userId, positionId int, shares, limit decimal.Decimal) (*model.TradePredictionSeriesPosition, error) {
	position, err := model.GetTradePredictionSeriesPosition(userId, positionId)
	if err != nil {
		return nil, err
	}
	if position.Status != model.TradePredictionActive {
		return nil, model.ErrTradePositionInsufficient
	}
	if position.IntervalSec <= 0 || position.WindowStart != common.GetTimestamp()/position.IntervalSec*position.IntervalSec {
		return nil, model.ErrTradePredictionClosed
	}
	available := tradesim.QtyFromUnits(position.Qty)
	if shares.IsZero() {
		shares = available
	}
	if !shares.IsPositive() || shares.GreaterThan(available) || !shares.Equal(shares.Truncate(4)) {
		return nil, ErrTradeOrderInvalid
	}
	round, err := model.GetTradePredictionSeriesRound(position.Slug)
	if err != nil {
		return nil, err
	}
	legacy := seriesRoundToLegacy(*round)
	client := tradePredictions
	depth := client.lockDepth(&legacy, position.Side)
	defer depth.Unlock()
	book, err := client.book(ctx, &legacy, position.Side)
	if err != nil {
		return nil, model.ErrTradePredictionQuote
	}
	if err := depth.acceptBook(book); err != nil {
		return nil, err
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	fill, err := matchPredictionBook(book, false, decimal.Zero, shares, limit, perUsd, depth.bids)
	if err != nil {
		return nil, err
	}
	fill.UserId, fill.WindowStart, fill.Side = userId, position.WindowStart, position.Side
	position, err = model.SellTradePredictionSeries(positionId, fill.TradePredictionFill)
	if err == nil {
		for price, qty := range fill.takes {
			depth.bids[price] = depth.bids[price].Add(qty)
		}
	}
	return position, err
}

func (client *tradePredictionClient) settlePendingSeries(ctx context.Context) {
	if !model.DB.Migrator().HasTable(&model.TradePredictionSeriesRound{}) {
		return
	}
	var rounds []model.TradePredictionSeriesRound
	err := model.DB.Where("end_time <= ? AND outcome = ?", common.GetTimestamp(), "").Order("last_checked_at, window_start").Limit(50).Find(&rounds).Error
	if err != nil {
		common.SysError("prediction series round recovery: " + err.Error())
		return
	}
	for _, round := range rounds {
		if err := model.DB.Model(&model.TradePredictionSeriesRound{}).Where("slug = ?", round.Slug).Update("last_checked_at", common.GetTimestamp()).Error; err != nil {
			continue
		}
		outcome, err := client.officialOutcome(ctx, seriesRoundToLegacy(round))
		if err != nil {
			continue
		}
		if err := model.ResolveTradePredictionSeriesRound(round.Slug, outcome); err != nil {
			common.SysError("prediction series result persistence: " + err.Error())
		}
	}
	var positions []model.TradePredictionSeriesPosition
	err = model.DB.Where("status = ? AND slug IN (?)", model.TradePredictionActive,
		model.DB.Model(&model.TradePredictionSeriesRound{}).Select("slug").Where("outcome <> ?", "")).Order("id").Limit(1000).Find(&positions).Error
	if err != nil {
		common.SysError("prediction series position recovery: " + err.Error())
		return
	}
	for _, position := range positions {
		if err := model.SettleTradePredictionSeriesPosition(position.Id); err != nil {
			common.SysError(fmt.Sprintf("prediction series settlement position %d: %v", position.Id, err))
		}
	}
}

func valueTradeSeriesPredictions(positions []model.TradePredictionSeriesPosition, accounting bool) (int, error) {
	if len(positions) == 0 {
		return 0, nil
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	total := 0
	now := common.GetTimestamp()
	rounds := make(map[string]*model.TradePredictionSeriesRound)
	tradePredictionSeriesCache.mu.Lock()
	snapshot := make(map[string]TradePredictionMarketView, len(tradePredictionSeriesCache.entries))
	for k, v := range tradePredictionSeriesCache.entries {
		snapshot[k] = v.view
	}
	tradePredictionSeriesCache.mu.Unlock()
	for _, position := range positions {
		value := position.Cost
		price, priced := decimal.Zero, false
		if accounting {
			round := rounds[position.Slug]
			if round == nil {
				round, err = model.GetTradePredictionSeriesRound(position.Slug)
				if err != nil {
					return 0, err
				}
				rounds[position.Slug] = round
			}
			switch round.Outcome {
			case model.TradePredictionUp, model.TradePredictionDown:
				priced = true
				if position.Side == round.Outcome {
					price = decimal.NewFromInt(1)
				}
			case model.TradePredictionSplit:
				price, priced = decimal.New(5, -1), true
			case "":
				if now >= round.EndTime {
					return 0, model.ErrTradeMarkUnavailable
				}
			default:
				return 0, model.ErrTradeMarkUnavailable
			}
		}
		if !priced {
			if view, ok := snapshot[position.MarketKey]; ok && view.Status == "ready" && view.Round != nil &&
				view.Round.Slug == position.Slug && view.UpdatedAt >= now-5 && view.UpdatedAt <= now {
				bid := view.Up.Bid
				if position.Side == model.TradePredictionDown {
					bid = view.Down.Bid
				}
				if parsed, parseErr := parsePredictionNumber(bid); parseErr == nil && (!accounting || parsed.LessThanOrEqual(decimal.NewFromInt(1))) {
					price, priced = parsed, true
				}
			}
		}
		if accounting && !priced {
			return 0, model.ErrTradeMarkUnavailable
		}
		if priced {
			value, err = (tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}).CreditQuota(price.Mul(tradesim.QtyFromUnits(position.Qty)))
			if err != nil {
				return 0, err
			}
		}
		if value < 0 || total >= common.MaxQuota-value {
			return 0, model.ErrTradeAmountInvalid
		}
		total += value
	}
	return total, nil
}
