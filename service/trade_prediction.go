package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

// 仅读取公开数据；这里没有签名钱包、私钥或真实下单 API。
// 模拟手续费沿用原项目 crypto_fees_v2，与 Polymarket 当前公开 crypto fee 曲线一致。
var predictionFeeRate = decimal.RequireFromString("0.07")

type TradePredictionPrices struct {
	Bid string `json:"bid"`
	Ask string `json:"ask"`
}

type TradePredictionMarketView struct {
	Enabled        bool                      `json:"enabled"`
	Status         string                    `json:"status"`
	Source         string                    `json:"source"`
	ServerTime     int64                     `json:"server_time"`
	Round          *TradePredictionRoundView `json:"round"`
	Up             TradePredictionPrices     `json:"up"`
	Down           TradePredictionPrices     `json:"down"`
	UpdatedAt      int64                     `json:"updated_at"`
	FeeRate        string                    `json:"fee_rate"`
	MinAmountUsd   int                       `json:"min_amount_usd"`
	MaxAmountUsd   int                       `json:"max_amount_usd"`
	MaxPositionUsd int                       `json:"max_position_usd"`
}

type TradePredictionRoundView struct {
	model.TradePredictionRound
	Status string `json:"status"`
}

func PredictionRoundView(round model.TradePredictionRound) TradePredictionRoundView {
	status := "open"
	if common.GetTimestamp() >= round.EndTime {
		status = "pending"
	}
	if round.Outcome != "" {
		status = "settled"
	}
	return TradePredictionRoundView{TradePredictionRound: round, Status: status}
}

type tradePredictionLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}
type tradePredictionBook struct {
	Market      string                 `json:"market"`
	AssetId     string                 `json:"asset_id"`
	Timestamp   string                 `json:"timestamp"`
	Bids        []tradePredictionLevel `json:"bids"`
	Asks        []tradePredictionLevel `json:"asks"`
	expiresAtMs int64
}

type tradePredictionClient struct {
	gammaURL, clobURL string
	// 预测页的实时数据：目标价接口与 BTC 价格、成交推送，见 trade_prediction_live.go。
	cryptoURL, liveURL string
	transport          func(string) (*http.Client, error)
	dialer             func(string) (*websocket.Dialer, error)
	group              singleflight.Group
	mu                 sync.Mutex
	view               TradePredictionMarketView
	cacheUntil         time.Time
	live               tradePredictionLive
}

func newTradePredictionClient() *tradePredictionClient {
	return &tradePredictionClient{gammaURL: "https://gamma-api.polymarket.com", clobURL: "https://clob.polymarket.com",
		cryptoURL: "https://polymarket.com", liveURL: "wss://ws-live-data.polymarket.com/",
		transport: func(proxy string) (*http.Client, error) {
			client, _, err := tradeMarketTransport(proxy)
			return client, err
		},
		dialer: func(proxy string) (*websocket.Dialer, error) {
			_, dialer, err := tradeMarketTransport(proxy)
			return dialer, err
		}}
}

var tradePredictions = newTradePredictionClient()
var tradePredictionStart sync.Once

func (client *tradePredictionClient) getJSON(ctx context.Context, endpoint string, out any) error {
	transport, err := client.transport(operation_setting.GetTradeSetting().ProxyUrl)
	if err != nil {
		return err
	}
	copyClient := *transport
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("prediction redirect refused") }
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := copyClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("prediction provider status %d", response.StatusCode)
	}
	const maxBody = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxBody {
		return errors.New("prediction response exceeds limit")
	}
	return common.Unmarshal(data, out)
}

func (client *tradePredictionClient) discover(ctx context.Context, windowStart int64) (*model.TradePredictionRound, error) {
	slug := "btc-updown-5m-" + strconv.FormatInt(windowStart, 10)
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
		if err != nil || end.Unix() != windowStart+model.TradePredictionWindow {
			continue
		}
		var outcomes, tokens []string
		if common.UnmarshalJsonStr(market.Outcomes, &outcomes) != nil || common.UnmarshalJsonStr(market.Tokens, &tokens) != nil || len(outcomes) != 2 || len(tokens) != 2 {
			continue
		}
		round := model.TradePredictionRound{WindowStart: windowStart, EndTime: end.Unix(), Slug: slug, Title: market.Question, ConditionId: market.ConditionId}
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
		if err := model.SaveTradePredictionRound(round); err != nil {
			return nil, err
		}
		return model.GetTradePredictionRound(windowStart)
	}
	return nil, model.ErrTradePredictionQuote
}

func (client *tradePredictionClient) book(ctx context.Context, round *model.TradePredictionRound, side string) (*tradePredictionBook, error) {
	token := round.UpToken
	if side == model.TradePredictionDown {
		token = round.DownToken
	}
	started := time.Now()
	book := &tradePredictionBook{}
	if err := client.getJSON(ctx, client.clobURL+"/book?token_id="+url.QueryEscape(token), book); err != nil {
		return nil, err
	}
	stamp, err := strconv.ParseInt(book.Timestamp, 10, 64)
	staleMs := int64(operation_setting.GetTradeSetting().StaleMs)
	if staleMs <= 0 {
		staleMs = 3000
	}
	now := time.Now().UnixMilli()
	if err != nil || book.AssetId != token || book.Market != round.ConditionId || now-stamp > staleMs || stamp > now+1000 || now-started.UnixMilli() > staleMs {
		return nil, model.ErrTradePredictionQuote
	}
	book.expiresAtMs = min(stamp+staleMs, started.UnixMilli()+staleMs)
	best := predictionBestPrices(book)
	if best.Bid != "" && best.Ask != "" {
		bid, _ := decimal.NewFromString(best.Bid)
		ask, _ := decimal.NewFromString(best.Ask)
		if bid.GreaterThanOrEqual(ask) {
			return nil, model.ErrTradePredictionQuote
		}
	}
	return book, nil
}

// parsePredictionNumber 拒绝科学记数法和巨大指数，外部盘口同样是未受信任输入。
func parsePredictionNumber(raw string) (decimal.Decimal, error) {
	if raw == "" || len(raw) > 40 || strings.Count(raw, ".") > 1 || strings.Trim(raw, "0123456789.") != "" || strings.HasPrefix(raw, ".") || strings.HasSuffix(raw, ".") {
		return decimal.Zero, model.ErrTradePredictionQuote
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() {
		return decimal.Zero, model.ErrTradePredictionQuote
	}
	return value, nil
}

func predictionBestPrices(book *tradePredictionBook) TradePredictionPrices {
	view := TradePredictionPrices{}
	for _, level := range book.Bids {
		price, err := parsePredictionNumber(level.Price)
		size, sizeErr := parsePredictionNumber(level.Size)
		if err != nil || sizeErr != nil || !size.IsPositive() || !price.IsPositive() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
			continue
		}
		previous, _ := decimal.NewFromString(view.Bid)
		if view.Bid == "" || price.GreaterThan(previous) {
			view.Bid = price.String()
		}
	}
	for _, level := range book.Asks {
		price, err := parsePredictionNumber(level.Price)
		size, sizeErr := parsePredictionNumber(level.Size)
		if err != nil || sizeErr != nil || !size.IsPositive() || !price.IsPositive() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
			continue
		}
		previous, _ := decimal.NewFromString(view.Ask)
		if view.Ask == "" || price.LessThan(previous) {
			view.Ask = price.String()
		}
	}
	return view
}

func GetTradePredictionMarket(ctx context.Context) TradePredictionMarketView {
	return tradePredictions.market(ctx)
}

func (client *tradePredictionClient) market(ctx context.Context) TradePredictionMarketView {
	setting := operation_setting.GetTradeSetting()
	view := TradePredictionMarketView{Enabled: setting.Enabled && setting.PredictionEnabled, Status: "unavailable", Source: "Polymarket", ServerTime: common.GetTimestamp(), FeeRate: predictionFeeRate.String(), MinAmountUsd: 1, MaxAmountUsd: setting.MaxOrderUsd, MaxPositionUsd: setting.MaxPositionUsd}
	if !view.Enabled {
		var active int64
		if err := model.DB.Model(&model.TradePredictionPosition{}).Where("window_start = ? AND status = ?", view.ServerTime/model.TradePredictionWindow*model.TradePredictionWindow, model.TradePredictionActive).Count(&active).Error; err != nil || active == 0 {
			view.Status = "disabled"
			return view
		}
	}
	client.mu.Lock()
	cached, until := client.view, client.cacheUntil
	client.mu.Unlock()
	if time.Now().Before(until) && cached.Round != nil && view.ServerTime < cached.Round.EndTime {
		cached.ServerTime = view.ServerTime
		cached.Enabled, cached.MaxAmountUsd, cached.MaxPositionUsd = view.Enabled, view.MaxAmountUsd, view.MaxPositionUsd
		return cached
	}
	result, err, _ := client.group.Do("market", func() (any, error) {
		windowStart := view.ServerTime / model.TradePredictionWindow * model.TradePredictionWindow
		round, err := model.GetTradePredictionRound(windowStart)
		if err != nil {
			round, err = client.discover(ctx, windowStart)
		}
		if err != nil {
			return view, nil
		}
		roundView := PredictionRoundView(*round)
		view.Round = &roundView
		up, err := client.book(ctx, round, model.TradePredictionUp)
		if err != nil {
			return view, nil
		}
		down, err := client.book(ctx, round, model.TradePredictionDown)
		if err != nil {
			return view, nil
		}
		if time.Now().UnixMilli() > min(up.expiresAtMs, down.expiresAtMs) || common.GetTimestamp() >= round.EndTime {
			return view, nil
		}
		view.Up, view.Down = predictionBestPrices(up), predictionBestPrices(down)
		view.Status, view.UpdatedAt = "ready", common.GetTimestamp()
		client.mu.Lock()
		client.view, client.cacheUntil = view, time.Now().Add(time.Second)
		client.mu.Unlock()
		return view, nil
	})
	if err != nil {
		return view
	}
	return result.(TradePredictionMarketView)
}

type TradePredictionOrderRequest struct {
	WindowStart int64
	Side        string
	Amount      decimal.Decimal
	LimitPrice  decimal.Decimal
}

// matchPredictionBook 按真实深度吃单，限价保护保证不滑到用户未接受的赔率。
// 买入 amount 为含费预算，卖出 shares 为份数；不足可部分成交，不虚构剩余流动性。
func matchPredictionBook(book *tradePredictionBook, buy bool, amount, shares, limit decimal.Decimal, perUsd int) (model.TradePredictionFill, error) {
	if !limit.IsPositive() || limit.GreaterThanOrEqual(decimal.NewFromInt(1)) || perUsd <= 0 {
		return model.TradePredictionFill{}, ErrTradeOrderInvalid
	}
	levels := book.Bids
	if buy {
		levels = book.Asks
	}
	type levelValue struct{ price, shares decimal.Decimal }
	parsed := make([]levelValue, 0, len(levels))
	for _, level := range levels {
		price, err := parsePredictionNumber(level.Price)
		size, sizeErr := parsePredictionNumber(level.Size)
		if err != nil || sizeErr != nil || !price.IsPositive() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) || size.GreaterThan(decimal.NewFromInt(1_000_000_000)) {
			return model.TradePredictionFill{}, model.ErrTradePredictionQuote
		}
		if !size.IsPositive() {
			continue
		}
		parsed = append(parsed, levelValue{price, size.Truncate(4)})
	}
	sort.Slice(parsed, func(i, j int) bool {
		if buy {
			return parsed[i].price.LessThan(parsed[j].price)
		}
		return parsed[i].price.GreaterThan(parsed[j].price)
	})
	pricing := tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}
	budget := 0
	remaining := amount
	if buy {
		// 买入的预算先换成整数额度再换回美元(截断，不会比预算多)，按它吃单：成交金额加手续费合起来向上取整也不会超过预算。
		var err error
		if budget, err = pricing.CreditQuota(amount); err != nil {
			return model.TradePredictionFill{}, model.ErrTradeAmountInvalid
		}
		remaining = decimal.NewFromInt(int64(budget)).Div(pricing.QuotaPerUsd).Truncate(12)
	}
	quantity, value, fee := decimal.Zero, decimal.Zero, decimal.Zero
	for _, level := range parsed {
		if buy && level.price.GreaterThan(limit) || !buy && level.price.LessThan(limit) {
			break
		}
		feePerShare := predictionFeeRate.Mul(level.price).Mul(decimal.NewFromInt(1).Sub(level.price))
		qty := level.shares
		if buy {
			qty = decimal.Min(qty, remaining.Div(level.price.Add(feePerShare)).Truncate(4))
		} else {
			qty = decimal.Min(qty, shares.Sub(quantity))
		}
		qty = decimal.Min(qty, decimal.NewFromInt(1_000_000).Sub(quantity)).Truncate(4)
		if !qty.IsPositive() {
			break
		}
		quantity = quantity.Add(qty)
		value = value.Add(qty.Mul(level.price))
		fee = fee.Add(qty.Mul(feePerShare))
		if buy {
			remaining = remaining.Sub(qty.Mul(level.price.Add(feePerShare)))
		}
	}
	if !quantity.IsPositive() {
		return model.TradePredictionFill{}, ErrTradeNoLiquidity
	}
	feeQuota, err := pricing.DebitQuota(fee)
	if err != nil {
		return model.TradePredictionFill{}, model.ErrTradeAmountInvalid
	}
	var quota int
	if buy {
		// 买入一共扣成交金额加手续费向上取整，手续费单独向上取整，剩下的记成交金额。
		total, err := pricing.DebitQuota(value.Add(fee))
		if err != nil || total > budget {
			return model.TradePredictionFill{}, ErrTradeQtyTooSmall
		}
		feeQuota = min(feeQuota, total)
		quota = total - feeQuota
	} else {
		if quota, err = pricing.CreditQuota(value); err != nil {
			return model.TradePredictionFill{}, model.ErrTradeAmountInvalid
		}
		if feeQuota > quota {
			return model.TradePredictionFill{}, ErrTradeQtyTooSmall
		}
	}
	if quota >= common.MaxQuota-feeQuota {
		return model.TradePredictionFill{}, model.ErrTradeAmountInvalid
	}
	return model.TradePredictionFill{Qty: quantity.Shift(8).IntPart(), Amount: quota, Fee: feeQuota, Price: value.Div(quantity).Round(8).String(), ExpiresAtMs: book.expiresAtMs}, nil
}

func BuyTradePrediction(ctx context.Context, userId int, request TradePredictionOrderRequest) (*model.TradePredictionPosition, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.PredictionEnabled {
		return nil, ErrTradeDisabled
	}
	if (request.Side != model.TradePredictionUp && request.Side != model.TradePredictionDown) || request.Amount.LessThan(decimal.NewFromInt(1)) || request.Amount.GreaterThan(decimal.NewFromInt(int64(setting.MaxOrderUsd))) || request.Amount.Exponent() < -8 {
		return nil, ErrTradeOrderInvalid
	}
	if request.WindowStart != common.GetTimestamp()/model.TradePredictionWindow*model.TradePredictionWindow {
		return nil, model.ErrTradePredictionClosed
	}
	round, err := model.GetTradePredictionRound(request.WindowStart)
	if err != nil {
		round, err = tradePredictions.discover(ctx, request.WindowStart)
	}
	if err != nil {
		return nil, err
	}
	book, err := tradePredictions.book(ctx, round, request.Side)
	if err != nil {
		return nil, model.ErrTradePredictionQuote
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	fill, err := matchPredictionBook(book, true, request.Amount, decimal.Zero, request.LimitPrice, perUsd)
	if err != nil {
		return nil, err
	}
	fill.UserId, fill.WindowStart, fill.Side, fill.Market = userId, request.WindowStart, request.Side, TradeAccountingMarket()
	fill.MaxCost, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.MaxPositionUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, model.ErrTradeAmountInvalid
	}
	return model.BuyTradePrediction(fill)
}

func SellTradePrediction(ctx context.Context, userId, positionId int, shares, limit decimal.Decimal) (*model.TradePredictionPosition, error) {
	// 关闭功能只停止买入；已有持仓仍允许退出和自动结算。
	position, err := model.GetTradePredictionPosition(userId, positionId)
	if err != nil {
		return nil, err
	}
	if position.Status != model.TradePredictionActive {
		return nil, model.ErrTradePositionInsufficient
	}
	if position.WindowStart != common.GetTimestamp()/model.TradePredictionWindow*model.TradePredictionWindow {
		return nil, model.ErrTradePredictionClosed
	}
	available := tradesim.QtyFromUnits(position.Qty)
	if shares.IsZero() {
		shares = available
	}
	if !shares.IsPositive() || shares.GreaterThan(available) || !shares.Equal(shares.Truncate(4)) {
		return nil, ErrTradeOrderInvalid
	}
	round, err := model.GetTradePredictionRound(position.WindowStart)
	if err != nil {
		return nil, err
	}
	book, err := tradePredictions.book(ctx, round, position.Side)
	if err != nil {
		return nil, model.ErrTradePredictionQuote
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	fill, err := matchPredictionBook(book, false, decimal.Zero, shares, limit, perUsd)
	if err != nil {
		return nil, err
	}
	fill.UserId, fill.WindowStart, fill.Side = userId, position.WindowStart, position.Side
	return model.SellTradePrediction(positionId, fill)
}

// officialOutcome 读 Polymarket 的官方结果：先看 CLOB，CLOB 还没标出赢家时看 Gamma(结算之后 CLOB 往往要过十几分钟才标
// 赢家)。两处都只认已经结算的市场，概率到 0/1、时间已过或另一个交易所的 BTC 涨跌都不能当作官方结果。
func (client *tradePredictionClient) officialOutcome(ctx context.Context, round model.TradePredictionRound) (string, error) {
	outcome, err := client.clobOutcome(ctx, round)
	if err == nil {
		return outcome, nil
	}
	if outcome, gammaErr := client.gammaOutcome(ctx, round); gammaErr == nil {
		return outcome, nil
	}
	return "", err
}

// clobOutcome 只认 CLOB 已关闭市场上唯一被标为 winner 的 token；Polymarket 判五五开(is_50_50_outcome、没有赢家、两边价格
// 都是 0.5)时返回 TradePredictionSplit。
func (client *tradePredictionClient) clobOutcome(ctx context.Context, round model.TradePredictionRound) (string, error) {
	var market struct {
		ConditionId string `json:"condition_id"`
		Closed      bool   `json:"closed"`
		Split       bool   `json:"is_50_50_outcome"`
		Tokens      []struct {
			TokenId string      `json:"token_id"`
			Outcome string      `json:"outcome"`
			Price   json.Number `json:"price"`
			Winner  bool        `json:"winner"`
		} `json:"tokens"`
	}
	if err := client.getJSON(ctx, client.clobURL+"/markets/"+url.PathEscape(round.ConditionId), &market); err != nil {
		return "", err
	}
	if !market.Closed || market.ConditionId != round.ConditionId || len(market.Tokens) != 2 {
		return "", model.ErrTradePredictionOutcome
	}
	result := ""
	seenUp, seenDown := false, false
	halves := 0
	for _, token := range market.Tokens {
		side := strings.ToUpper(token.Outcome)
		if side == model.TradePredictionUp && token.TokenId == round.UpToken && !seenUp {
			seenUp = true
		} else if side == model.TradePredictionDown && token.TokenId == round.DownToken && !seenDown {
			seenDown = true
		} else {
			return "", model.ErrTradePredictionOutcome
		}
		if price, err := decimal.NewFromString(token.Price.String()); err == nil && price.Equal(decimal.New(5, -1)) {
			halves++
		}
		if token.Winner {
			if result != "" {
				return "", model.ErrTradePredictionOutcome
			}
			result = side
		}
	}
	if !seenUp || !seenDown {
		return "", model.ErrTradePredictionOutcome
	}
	if result == "" && market.Split && halves == 2 {
		return model.TradePredictionSplit, nil
	}
	if result == "" {
		return "", model.ErrTradePredictionOutcome
	}
	return result, nil
}

// gammaOutcome 读 Gamma 上同一个市场的结算：市场已关闭、UMA 结算状态是 resolved、两边的结算价恰好是 1 和 0(赢家)或者都是
// 0.5(五五开)才算数；交易时 outcomePrices 是概率，不会恰好是这些值，而且那时状态不是 resolved。
func (client *tradePredictionClient) gammaOutcome(ctx context.Context, round model.TradePredictionRound) (string, error) {
	var markets []struct {
		Slug        string `json:"slug"`
		ConditionId string `json:"conditionId"`
		Closed      bool   `json:"closed"`
		Resolution  string `json:"umaResolutionStatus"`
		Outcomes    string `json:"outcomes"`
		Prices      string `json:"outcomePrices"`
		Tokens      string `json:"clobTokenIds"`
	}
	if err := client.getJSON(ctx, client.gammaURL+"/markets?closed=true&slug="+url.QueryEscape(round.Slug), &markets); err != nil {
		return "", err
	}
	for _, market := range markets {
		if market.Slug != round.Slug || market.ConditionId != round.ConditionId {
			continue
		}
		var outcomes, prices, tokens []string
		if !market.Closed || market.Resolution != "resolved" || common.UnmarshalJsonStr(market.Outcomes, &outcomes) != nil ||
			common.UnmarshalJsonStr(market.Prices, &prices) != nil || common.UnmarshalJsonStr(market.Tokens, &tokens) != nil ||
			len(outcomes) != 2 || len(prices) != 2 || len(tokens) != 2 {
			return "", model.ErrTradePredictionOutcome
		}
		winners, halves := []string{}, 0
		seen := map[string]bool{}
		for i, outcome := range outcomes {
			side := strings.ToUpper(outcome)
			if side == model.TradePredictionUp && tokens[i] != round.UpToken || side == model.TradePredictionDown && tokens[i] != round.DownToken ||
				side != model.TradePredictionUp && side != model.TradePredictionDown || seen[side] {
				return "", model.ErrTradePredictionOutcome
			}
			seen[side] = true
			price, err := decimal.NewFromString(prices[i])
			switch {
			case err != nil:
				return "", model.ErrTradePredictionOutcome
			case price.Equal(decimal.NewFromInt(1)):
				winners = append(winners, side)
			case price.Equal(decimal.New(5, -1)):
				halves++
			case !price.IsZero():
				return "", model.ErrTradePredictionOutcome
			}
		}
		if len(winners) == 1 && halves == 0 {
			return winners[0], nil
		}
		if halves == 2 {
			return model.TradePredictionSplit, nil
		}
		return "", model.ErrTradePredictionOutcome
	}
	return "", model.ErrTradePredictionOutcome
}

// StartTradePrediction 只启动恢复/结算任务，不依赖是否有人打开页面；关闭功能后仍处理已有持仓。
func StartTradePrediction() {
	tradePredictionStart.Do(func() {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				tradePredictions.settlePending(context.Background())
				<-ticker.C
			}
		}()
	})
}

func (client *tradePredictionClient) settlePending(ctx context.Context) {
	var rounds []model.TradePredictionRound
	err := model.DB.Where("end_time <= ? AND outcome = ?", common.GetTimestamp(), "").Order("last_checked_at, window_start").Limit(50).Find(&rounds).Error
	if err != nil {
		common.SysError("prediction round recovery: " + err.Error())
		return
	}
	for _, round := range rounds {
		// 轮换检查次序，网络故障或长期未决的旧轮次不能饿死新轮次。
		if err := model.DB.Model(&model.TradePredictionRound{}).Where("window_start = ?", round.WindowStart).Update("last_checked_at", common.GetTimestamp()).Error; err != nil {
			common.SysError("prediction recovery cursor: " + err.Error())
			continue
		}
		outcome, err := client.officialOutcome(ctx, round)
		if err != nil {
			continue
		}
		if err := model.ResolveTradePredictionRound(round.WindowStart, outcome); err != nil {
			common.SysError("prediction result persistence: " + err.Error())
		}
	}
	var positions []model.TradePredictionPosition
	err = model.DB.Where("status = ? AND window_start IN (?)", model.TradePredictionActive,
		model.DB.Model(&model.TradePredictionRound{}).Select("window_start").Where("outcome <> ?", "")).Order("id").Limit(1000).Find(&positions).Error
	if err != nil {
		common.SysError("prediction position recovery: " + err.Error())
		return
	}
	for _, position := range positions {
		if err := model.SettleTradePredictionPosition(position.Id); err != nil {
			common.SysError(fmt.Sprintf("prediction settlement position %d: %v", position.Id, err))
		}
	}
}

// ValueTradePredictions 用已缓存的可卖价估值；断线/旧轮次未决时按剩余成本显示，不参与保证金或可转出金额。
func ValueTradePredictions(positions []model.TradePredictionPosition) (int, error) {
	tradePredictions.mu.Lock()
	view := tradePredictions.view
	tradePredictions.mu.Unlock()
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, position := range positions {
		value := position.Cost
		if view.Status == "ready" && view.Round != nil && view.Round.WindowStart == position.WindowStart && view.UpdatedAt >= common.GetTimestamp()-5 {
			bid := view.Up.Bid
			if position.Side == model.TradePredictionDown {
				bid = view.Down.Bid
			}
			price, err := parsePredictionNumber(bid)
			if err == nil {
				value, err = (tradesim.Pricing{QuotaPerUsd: decimal.NewFromInt(int64(perUsd))}).CreditQuota(price.Mul(tradesim.QtyFromUnits(position.Qty)))
				if err != nil {
					return 0, err
				}
			}
		}
		if value < 0 || total >= common.MaxQuota-value {
			return 0, model.ErrTradeAmountInvalid
		}
		total += value
	}
	return total, nil
}
