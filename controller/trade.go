package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tradesim"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type tradeOrderRequest struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	Type   string `json:"type"`
	Qty    string `json:"qty"`
	Amount string `json:"amount"`
	Price  string `json:"price"`
}

type tradeTransferRequest struct {
	// Direction 是 in(从主钱包转入模拟盘)或 out(转回主钱包)，Amount 是额度，按美元填写，可以有小数。
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
}

// tradeDay 是模拟盘的记账日：每天的盈利转出上限与资产快照都按它切换，与签到一样按服务器本地时间 0 点切换。多个节点
// 必须用同一个时区(TZ)。
func tradeDay(t time.Time) string {
	return t.Format("2006-01-02")
}

// tradeDecimalMaxLength 是页面传来的数字最多几个字符，足够表示交易对的价格和数量。
const tradeDecimalMaxLength = 32

// parseTradeDecimal 解析页面传来的非负十进制数，空串为 0。只接受数字和一个小数点：decimal 能解析 1e1000000 这样的科学计数法，
// 拿去运算会占用大量内存。
func parseTradeDecimal(value string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, nil
	}
	if len(value) > tradeDecimalMaxLength || strings.Count(value, ".") > 1 || strings.Trim(value, "0123456789.") != "" ||
		strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return decimal.Zero, errors.New("invalid decimal")
	}
	return decimal.NewFromString(value)
}

func tradeQty(units int64) string {
	return tradesim.QtyFromUnits(units).String()
}

// tradeOrderView 是给页面看的委托：数量是十进制字符串，金额是额度单位，AvgPrice 是成交均价。均价按成交价值算；没有记成交价值的
// 委托按取整到额度单位的成交金额算，数量很小时会差几个价格最小单位。
func tradeOrderView(order *model.TradeOrder, perUsd int) gin.H {
	avgPrice := ""
	if order.FilledQty > 0 {
		value, err := decimal.NewFromString(order.FilledValue)
		if (err != nil || !value.IsPositive()) && perUsd > 0 {
			value = decimal.NewFromInt(int64(order.FilledAmount)).Div(decimal.NewFromInt(int64(perUsd)))
		}
		if value.IsPositive() {
			avgPrice = value.Div(tradesim.QtyFromUnits(order.FilledQty)).Round(tradesim.QtyDecimals).String()
		}
	}
	return gin.H{
		"id":            order.Id,
		"symbol":        order.Symbol,
		"side":          order.Side,
		"type":          order.Type,
		"status":        order.Status,
		"price":         order.Price,
		"qty":           tradeQty(order.Qty),
		"budget":        order.Budget,
		"filled_qty":    tradeQty(order.FilledQty),
		"filled_amount": order.FilledAmount,
		"fee":           order.Fee,
		"frozen":        order.Frozen,
		"avg_price":     avgPrice,
		"cancel_reason": order.CancelReason,
		"created_at":    order.CreatedAt,
		"finished_at":   order.FinishedAt,
	}
}

func tradeLedgerView(entry model.TradeLedger) gin.H {
	return gin.H{
		"id":         entry.Id,
		"type":       entry.Type,
		"symbol":     entry.Symbol,
		"order_id":   entry.OrderId,
		"qty":        tradeQty(entry.Qty),
		"price":      entry.Price,
		"amount":     entry.Amount,
		"fee":        entry.Fee,
		"pnl":        entry.Pnl,
		"balance":    entry.Balance,
		"created_at": entry.CreatedAt,
	}
}

// tradeSelfView 汇总用户的模拟盘账户：资金、按当前行情的估值与持仓、可以转出的额度、今天和累计的盈亏，以及主钱包余额
// (转入时用)。
func tradeSelfView(userId int) (gin.H, error) {
	setting := operation_setting.GetTradeSetting()
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	account, valuation, err := service.ValueTradeUser(userId)
	if err != nil {
		return nil, err
	}
	day := tradeDay(time.Now())
	profitUsed, err := model.GetTradeProfitOutUsed(userId, day)
	if err != nil {
		return nil, err
	}
	// 转回自己转入的额度不受限制，超出的部分是盈利，当天还能转出多少受上限约束；有全仓仓位时还要留下全仓占用与浮动亏损。
	profitCap := setting.DailyProfitOutUsd * perUsd
	withdrawable := valuation.Cross.Withdrawable
	if profitCap > 0 {
		withdrawable = min(withdrawable, account.QuotaPrincipal+max(profitCap-profitUsed, 0))
	}
	userQuota, err := model.GetUserQuota(userId, true)
	if err != nil {
		return nil, err
	}
	base, err := model.GetLatestTradeSnapshotBefore(userId, day)
	if err != nil {
		return nil, err
	}
	netIn := account.TotalIn - account.TotalOut
	todayPnl := valuation.Equity - netIn
	if base != nil {
		todayPnl -= base.Equity - base.NetIn
	}
	return gin.H{
		"enabled":              setting.Enabled,
		"quota_per_unit":       perUsd,
		"fee_bps":              setting.FeeBps,
		"max_order_usd":        setting.MaxOrderUsd,
		"max_position_usd":     setting.MaxPositionUsd,
		"daily_profit_out_usd": setting.DailyProfitOutUsd,
		"max_open_orders":      model.TradeMaxOpenOrders,
		"leaderboard":          setting.Enabled && setting.LeaderboardEnabled,
		"futures": gin.H{
			"enabled":          setting.Enabled && setting.FuturesEnabled,
			"taker_fee_bps":    setting.FuturesTakerFeeBps,
			"maker_fee_bps":    setting.FuturesMakerFeeBps,
			"max_leverage":     setting.FuturesMaxLeverage,
			"max_position_usd": setting.FuturesMaxPositionUsd,
		},
		"account":   account,
		"valuation": valuation,
		"total_pnl": valuation.Equity - netIn,
		"today_pnl": todayPnl,
		"withdrawable": gin.H{
			"quota":           withdrawable,
			"profit_out_used": profitUsed,
			"profit_out_cap":  profitCap,
		},
		"wallet": gin.H{"quota": userQuota},
	}, nil
}

// GetTradeSelf 返回当前用户的模拟盘账户，模拟盘关闭时也能看，方便把钱转出来。
func GetTradeSelf(c *gin.Context) {
	view, err := tradeSelfView(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// tradeSparklineTimeout 是行情列表等迷你走势的最长时间，超时的交易对不画走势，不拖慢整个列表。
const tradeSparklineTimeout = 5 * time.Second

// GetTradeMarket 返回开放交易的交易对：行情、交易规则与最近 24 小时的走势。
func GetTradeMarket(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	market := service.GetTradeMarket()
	var infos []operation_setting.TradeSymbolInfo
	for _, info := range operation_setting.TradeSymbols {
		if setting.SymbolEnabled(info.Symbol) {
			infos = append(infos, info)
		}
	}
	// 各交易对的走势并发去取；超时后这次列表不带走势，取到的留在缓存里给下一次用。
	ctx := c.Request.Context()
	sparklines := make([][]string, len(infos))
	var wg sync.WaitGroup
	for i, info := range infos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sparklines[i], _ = market.Sparkline(ctx, info.Symbol)
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(tradeSparklineTimeout):
	}
	items := make([]gin.H, 0, len(infos))
	for i, info := range infos {
		item := gin.H{"symbol": info.Symbol, "ticker": info.Ticker, "kind": info.Kind}
		if quote, ok := market.Quote(info.Symbol); ok {
			item["quote"] = quote
		}
		if rules, ok := market.Rules(info.Symbol); ok {
			item["rules"] = gin.H{
				"tick_size":    rules.TickSize.String(),
				"step_size":    rules.StepSize.String(),
				"min_qty":      rules.MinQty.String(),
				"min_notional": rules.MinNotional.String(),
			}
		}
		select {
		case <-done:
			item["sparkline"] = sparklines[i]
		default:
		}
		items = append(items, item)
	}
	common.ApiSuccess(c, gin.H{
		"symbols":   items,
		"connected": market.Status().Connected,
		"fee_bps":   setting.FeeBps,
	})
}

// GetTradeKlines 返回 K 线，按 Binance 的数组格式精简成 [开盘时间, 开, 高, 低, 收, 成交量]。
func GetTradeKlines(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "300"))
	endTime, _ := strconv.ParseInt(c.DefaultQuery("end_time", "0"), 10, 64)
	if _, ok := operation_setting.TradeSymbolOf(symbol); !ok || !operation_setting.GetTradeSetting().SymbolEnabled(symbol) {
		common.ApiErrorI18n(c, i18n.MsgTradeSymbolClosed)
		return
	}
	valid := false
	for _, allowed := range service.TradeKlineIntervals {
		valid = valid || interval == allowed
	}
	if !valid || limit < 1 || limit > service.TradeKlineMaxLimit || endTime < 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	klines, err := service.GetTradeMarket().Klines(c.Request.Context(), symbol, interval, limit, endTime)
	if err != nil {
		common.SysError(fmt.Sprintf("trade klines %s %s failed: %v", symbol, interval, err))
		common.ApiErrorI18n(c, i18n.MsgTradeMarketUnavailable)
		return
	}
	rows := make([][6]any, len(klines))
	for i, kline := range klines {
		rows[i] = [6]any{kline.OpenTime, kline.Open.String(), kline.High.String(), kline.Low.String(), kline.Close.String(), kline.Volume.String()}
	}
	common.ApiSuccess(c, rows)
}

// tradeStreamKeepalive 是行情推送的心跳间隔，没有行情变化时也定时写一行注释，免得代理把连接当成空闲断开。
const tradeStreamKeepalive = 15 * time.Second

// StreamTradeMarket 以 SSE 推送行情：symbols 的 24 小时行情(合约另有标记价格与资金费率)、kline 那个交易对的 1 分钟 K 线、
// book 那个交易对的盘口。market=futures 时推合约，否则推现货。现货只推开放交易的交易对，模拟盘关闭后断开；合约推所有支持的
// 合约，合约关掉以后还有仓位的照样有行情(行情中心只连着用得到的合约)。
func StreamTradeMarket(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	futures := c.Query("market") == "futures"
	if !futures && !setting.Enabled {
		common.ApiErrorI18n(c, i18n.MsgTradeDisabled)
		return
	}
	var symbols []string
	for _, symbol := range strings.Split(c.Query("symbols"), ",") {
		allowed := setting.SymbolEnabled(symbol)
		if futures {
			_, allowed = operation_setting.TradeFuturesSymbolOf(symbol)
		}
		if allowed && len(symbols) < len(operation_setting.TradeSymbols) {
			symbols = append(symbols, symbol)
		}
	}
	market := service.GetTradeMarket()
	if futures {
		market = service.GetFuturesMarket()
	}
	sub := market.Subscribe(symbols, c.Query("kline"), c.Query("book"))
	defer market.Unsubscribe(sub)
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()
	keepalive := time.NewTicker(tradeStreamKeepalive)
	defer keepalive.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event := <-sub.Events:
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Name, event.Data); err != nil {
				return
			}
		case <-keepalive.C:
			if !futures && !operation_setting.GetTradeSetting().Enabled {
				return
			}
			if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
				return
			}
		}
		c.Writer.Flush()
	}
}

// tradeOrderErrorMessage 把下单与撤单的错误翻译给用户，意料之外的错误记日志。
func tradeOrderErrorMessage(c *gin.Context, symbol string, err error) string {
	setting := operation_setting.GetTradeSetting()
	rules, _ := service.GetTradeMarket().Rules(symbol)
	switch {
	case errors.Is(err, service.ErrTradeDisabled):
		return i18n.T(c, i18n.MsgTradeDisabled)
	case errors.Is(err, service.ErrTradeSymbolClosed):
		return i18n.T(c, i18n.MsgTradeSymbolClosed)
	case errors.Is(err, service.ErrTradeMarketUnavailable), errors.Is(err, model.ErrTradeMarkUnavailable):
		return i18n.T(c, i18n.MsgTradeMarketUnavailable)
	case errors.Is(err, service.ErrTradeMarketStale):
		return i18n.T(c, i18n.MsgTradeMarketStale)
	case errors.Is(err, service.ErrTradeOrderInvalid):
		return i18n.T(c, i18n.MsgTradeOrderInvalid)
	case errors.Is(err, service.ErrTradeQtyTooSmall):
		return i18n.T(c, i18n.MsgTradeQtyTooSmall, map[string]any{"Min": rules.MinQty.String()})
	case errors.Is(err, service.ErrTradeNotionalTooSmall):
		return i18n.T(c, i18n.MsgTradeNotionalTooSmall, map[string]any{"Min": rules.MinNotional.String()})
	case errors.Is(err, service.ErrTradeOrderTooLarge):
		return i18n.T(c, i18n.MsgTradeOrderTooLarge, map[string]any{"Max": setting.MaxOrderUsd})
	case errors.Is(err, service.ErrTradePriceInvalid):
		return i18n.T(c, i18n.MsgTradePriceInvalid)
	case errors.Is(err, service.ErrTradeNoLiquidity):
		return i18n.T(c, i18n.MsgTradeNoLiquidity)
	case errors.Is(err, model.ErrTradeCashInsufficient):
		return i18n.T(c, i18n.MsgTradeCashInsufficient)
	case errors.Is(err, model.ErrTradePositionInsufficient):
		return i18n.T(c, i18n.MsgTradePositionInsufficient)
	case errors.Is(err, model.ErrTradePositionLimit):
		return i18n.T(c, i18n.MsgTradePositionLimit, map[string]any{"Max": setting.MaxPositionUsd})
	case errors.Is(err, model.ErrTradeOpenOrderLimit):
		return i18n.T(c, i18n.MsgTradeOpenOrderLimit, map[string]any{"Max": model.TradeMaxOpenOrders})
	case errors.Is(err, model.ErrTradeOrderNotOpen), errors.Is(err, gorm.ErrRecordNotFound):
		return i18n.T(c, i18n.MsgTradeOrderNotOpen)
	default:
		common.SysError(fmt.Sprintf("trade order failed for user %d: %v", c.GetInt("id"), err))
		return i18n.T(c, i18n.MsgTradeOrderFailed)
	}
}

func respondTradeOrderError(c *gin.Context, symbol string, err error) {
	common.ApiErrorMsg(c, tradeOrderErrorMessage(c, symbol, err))
}

// PlaceTradeOrder 下一笔委托。
func PlaceTradeOrder(c *gin.Context) {
	var req tradeOrderRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	qty, qtyErr := parseTradeDecimal(req.Qty)
	amount, amountErr := parseTradeDecimal(req.Amount)
	price, priceErr := parseTradeDecimal(req.Price)
	if qtyErr != nil || amountErr != nil || priceErr != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderInvalid)
		return
	}
	userId := c.GetInt("id")
	order, err := service.PlaceTradeOrder(c.Request.Context(), userId, service.TradeOrderRequest{
		Symbol: req.Symbol,
		Side:   req.Side,
		Type:   req.Type,
		Qty:    qty,
		Amount: amount,
		Price:  price,
	})
	if err != nil {
		respondTradeOrderError(c, req.Symbol, err)
		return
	}
	perUsd, _ := model.TradeQuotaPerUsd()
	common.ApiSuccess(c, tradeOrderView(order, perUsd))
}

// CancelTradeOrder 撤销自己挂着的一笔限价委托，模拟盘关闭时也能撤。
func CancelTradeOrder(c *gin.Context) {
	orderId, err := strconv.Atoi(c.Param("id"))
	if err != nil || orderId < 1 {
		common.ApiErrorI18n(c, i18n.MsgTradeOrderNotOpen)
		return
	}
	order, err := service.CancelTradeOrder(c.GetInt("id"), orderId)
	if err != nil {
		respondTradeOrderError(c, "", err)
		return
	}
	perUsd, _ := model.TradeQuotaPerUsd()
	common.ApiSuccess(c, tradeOrderView(order, perUsd))
}

// GetTradeOrders 分页列出委托：status=open 是挂着的，否则是已经结束的；symbol 为空时不限交易对。
func GetTradeOrders(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	orders, total, err := model.GetTradeOrders(c.GetInt("id"), c.Query("status") == model.TradeOrderStatusOpen, c.Query("symbol"),
		pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	perUsd, _ := model.TradeQuotaPerUsd()
	items := make([]gin.H, len(orders))
	for i := range orders {
		items[i] = tradeOrderView(&orders[i], perUsd)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// GetTradeOrderFills 返回一笔委托的每一次成交。
func GetTradeOrderFills(c *gin.Context) {
	orderId, err := strconv.Atoi(c.Param("id"))
	if err != nil || orderId < 1 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	fills, err := model.GetTradeOrderFills(c.GetInt("id"), orderId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(fills))
	for i, fill := range fills {
		items[i] = tradeLedgerView(fill)
	}
	common.ApiSuccess(c, items)
}

// tradeFillMarks 是 K 线图上最多标出的成交数。
const tradeFillMarks = 200

// GetTradeFills 返回用户在一个交易对上最近的成交，K 线图用它标出买卖点。
func GetTradeFills(c *gin.Context) {
	fills, err := model.GetTradeFills(c.GetInt("id"), c.Query("symbol"), tradeFillMarks)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(fills))
	for i, fill := range fills {
		items[i] = tradeLedgerView(fill)
	}
	common.ApiSuccess(c, items)
}

// GetTradeLedger 分页列出账单，type 为空时不限类型。
func GetTradeLedger(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	entries, total, err := model.GetTradeLedgers(c.GetInt("id"), c.Query("type"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(entries))
	for i, entry := range entries {
		items[i] = tradeLedgerView(entry)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// tradeNoticeMaxLimit 是一次最多取几条通知。
const tradeNoticeMaxLimit = 50

// GetTradeNotices 返回 id 大于 after 的通知(新的在前，最多 limit 条)与最新一条通知的 id，页面轮询它弹出提示。
func GetTradeNotices(c *gin.Context) {
	after, afterErr := strconv.Atoi(c.DefaultQuery("after", "0"))
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if afterErr != nil || limitErr != nil || after < 0 || limit < 1 || limit > tradeNoticeMaxLimit {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	notices, latestId, err := model.GetTradeNotices(c.GetInt("id"), after, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, len(notices))
	for i, notice := range notices {
		items[i] = gin.H{
			"id":         notice.Id,
			"kind":       notice.Kind,
			"market":     notice.Market,
			"symbol":     notice.Symbol,
			"side":       notice.Side,
			"action":     notice.Action,
			"order_id":   notice.OrderId,
			"qty":        tradeQty(notice.Qty),
			"price":      notice.Price,
			"pnl":        notice.Pnl,
			"amount":     notice.Amount,
			"created_at": notice.CreatedAt,
		}
	}
	common.ApiSuccess(c, gin.H{"items": items, "latest_id": latestId})
}

// tradeLeaderboardSize 是排行榜列出的人数。
const tradeLeaderboardSize = 100

// GetTradeLeaderboard 返回排行榜的前 100 名与自己的名次(没上榜为 null)，sort 是 profit(累计盈亏，默认)、return(收益率)或
// equity(总资产)。名单最多 TradeLeaderboardTTL 更新一次，refresh_seconds 告诉页面多久更新。
func GetTradeLeaderboard(c *gin.Context) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.LeaderboardEnabled {
		common.ApiErrorI18n(c, i18n.MsgTradeLeaderboardDisabled)
		return
	}
	board, err := service.GetTradeLeaderboard()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userId := c.GetInt("id")
	ranked := board.Ranked(c.Query("sort"))
	items := make([]gin.H, 0, min(len(ranked), tradeLeaderboardSize))
	var me gin.H
	for i, entry := range ranked {
		row := gin.H{
			"rank":        i + 1,
			"name":        entry.Name,
			"equity":      entry.Equity,
			"profit":      entry.Profit,
			"return_rate": entry.ReturnRate,
			"me":          entry.UserId == userId,
		}
		if i < tradeLeaderboardSize {
			items = append(items, row)
		}
		if entry.UserId == userId {
			me = row
		}
	}
	common.ApiSuccess(c, gin.H{
		"items":           items,
		"me":              me,
		"total":           len(ranked),
		"updated_at":      board.UpdatedAt,
		"refresh_seconds": int(service.TradeLeaderboardTTL / time.Second),
	})
}

// TransferTrade 在主钱包与模拟盘之间转账。转入要模拟盘开放；转出随时可以，关闭期间也能把钱拿出来。成功后返回最新的账户。
func TransferTrade(c *gin.Context) {
	var req tradeTransferRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	amount, err := parseTradeDecimal(req.Amount)
	if err != nil || !amount.IsPositive() {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	setting := operation_setting.GetTradeSetting()
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	isIn := req.Direction == "in"
	if req.Direction != "in" && req.Direction != "out" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if isIn && !setting.Enabled {
		common.ApiErrorI18n(c, i18n.MsgTradeDisabled)
		return
	}
	quota, err := common.QuotaFromDecimalStrict(amount.Mul(decimal.NewFromInt(int64(perUsd))).Floor())
	if err != nil || quota < 1 {
		common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		return
	}
	userId := c.GetInt("id")
	content, profit := "", 0
	if isIn {
		_, err = model.TransferQuotaToTrade(userId, quota)
		content = fmt.Sprintf("模拟盘转入额度 %s", logger.LogQuota(quota))
	} else {
		_, profit, err = model.TransferQuotaFromTrade(userId, quota, tradeDay(time.Now()), setting.DailyProfitOutUsd*perUsd, service.TradeAccountingMarket())
		content = fmt.Sprintf("模拟盘转出额度 %s，其中盈利 %s", logger.LogQuota(quota), logger.LogQuota(profit))
	}
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTradeAmountInvalid):
			common.ApiErrorI18n(c, i18n.MsgTradeAmountInvalid)
		case errors.Is(err, model.ErrTradeQuotaInsufficient):
			common.ApiErrorI18n(c, i18n.MsgTradeQuotaInsufficient)
		case errors.Is(err, model.ErrTradeWithdrawExceeded):
			common.ApiErrorI18n(c, i18n.MsgTradeWithdrawExceeded)
		case errors.Is(err, model.ErrTradeProfitOutLimit):
			common.ApiErrorI18n(c, i18n.MsgTradeProfitOutLimit, map[string]any{"Max": setting.DailyProfitOutUsd})
		case errors.Is(err, model.ErrQuotaOutOfRange):
			common.ApiErrorI18n(c, i18n.MsgQuotaExceedMax)
		case errors.Is(err, model.ErrTradeMarkUnavailable):
			common.ApiErrorI18n(c, i18n.MsgTradeMarketUnavailable)
		default:
			common.SysError(fmt.Sprintf("trade transfer failed for user %d: %v", userId, err))
			common.ApiErrorI18n(c, i18n.MsgTradeTransferFailed)
		}
		return
	}
	model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem, content,
		map[string]interface{}{"trade_direction": req.Direction, "quota": quota, "profit": profit})
	view, err := tradeSelfView(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
