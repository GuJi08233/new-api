package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// TradePKMatchup 描述一场热门资产对决擂台。
type TradePKMatchup struct {
	Id      string `json:"id"`
	Title   string `json:"title"`
	SymbolA string `json:"symbol_a"`
	TickerA string `json:"ticker_a"`
	SymbolB string `json:"symbol_b"`
	TickerB string `json:"ticker_b"`
}

var TradePKMatchups = []TradePKMatchup{
	{Id: "ant_vs_openai", Title: "Anthropic vs OpenAI", SymbolA: "ANTUSDT", TickerA: "ANT", SymbolB: "OPENAIUSDT", TickerB: "OPENAI"},
	{Id: "nvda_vs_amd", Title: "英伟达 vs 超威半导体", SymbolA: "NVDAUSDT", TickerA: "NVDA", SymbolB: "AMDUSDT", TickerB: "AMD"},
	{Id: "nvda_vs_tsm", Title: "英伟达 vs 台积电", SymbolA: "NVDAUSDT", TickerA: "NVDA", SymbolB: "TSMUSDT", TickerB: "TSM"},
	{Id: "googl_vs_msft", Title: "谷歌 vs 微软", SymbolA: "GOOGLUSDT", TickerA: "GOOGL", SymbolB: "MSFTUSDT", TickerB: "MSFT"},
	{Id: "btc_vs_eth", Title: "比特币 vs 以太坊", SymbolA: "BTCUSDT", TickerA: "BTC", SymbolB: "ETHUSDT", TickerB: "ETH"},
	{Id: "btc_vs_xau", Title: "比特币 vs 现货黄金", SymbolA: "BTCUSDT", TickerA: "BTC", SymbolB: "XAUUSDT", TickerB: "XAU"},
	{Id: "zhipu_vs_moonshot", Title: "智谱 AI vs 月之暗面", SymbolA: "ZHIPUUSDT", TickerA: "ZHIPU", SymbolB: "MOONSHOTUSDT", TickerB: "MOONSHOT"},
	{Id: "baba_vs_tencent", Title: "阿里巴巴 vs 腾讯", SymbolA: "BABAUSDT", TickerA: "BABA", SymbolB: "TENCENTUSDT", TickerB: "TENCENT"},
}

var (
	tradeShortFlashDurations = []int64{60, 180, 300, 900, 3600}
	tradeShortPKDurations    = []int64{300, 900, 3600}
	tradeShortRangeDurations = []int64{900, 3600, 14400}
	tradeShortTouchDurations = []int64{900, 3600}

	tradeShortFeaturedSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "NVDAUSDT", "ANTUSDT", "OPENAIUSDT", "XAUUSDT"}

	// 即时短线只允许流动性充足的盘口(买卖点差 <= 20 bps)，杜绝清淡或休市标的宽点差跳价套利。
	tradeShortMaxSpreadBps = decimal.NewFromInt(20)
	// 永续合约即时单禁止跨越 |资金费率| >= 0.03% 的资金费结算点，杜绝除息跳空套利。
	tradeShortMaxFundingRate = decimal.RequireFromString("0.0003")
)

const (
	// 短线事件结算若行情连续 15 分钟彻底不可用，超时自动退还本金，防止资金永久冻结。
	tradeShortFallbackRefundSec = int64(900)
	// 到期 15 秒内优先使用同一市场的实时盘口中间价结算，与入场锚定价严格同源；延迟超过该窗口再回退到同一市场的 1m K 线。
	tradeShortLiveSettleGraceSec = int64(15)
)

func tradeShortRangeThresholdBps(durationSec int64) int {
	switch durationSec {
	case 900:
		return 50 // ±0.5%
	case 3600:
		return 150 // ±1.5%
	default:
		return 250 // ±2.5%
	}
}

func tradeShortTouchThresholdBps(durationSec int64) int {
	if durationSec <= 900 {
		return 100 // ±1.0%
	}
	return 200 // ±2.0%
}

// TradeShortPoolRoundView 是给前端展示的共享奖池局信息(含当前各选项资金池、实时预估赔率与倒计时)。
type TradeShortPoolRoundView struct {
	model.TradeShortRound
	Pools       map[string]int     `json:"pools"`
	Multipliers map[string]float64 `json:"multipliers"`
	PriceA      string             `json:"price_a"`
	PriceB      string             `json:"price_b"`
	Locked      bool               `json:"locked"`
}

// resolveShortEventMarket 为短线品种确定唯一规范行情源(spot 或 futures)及对应 Binance 代码。
// 同名主流币优先走现货(无永续资金费除息跳空)；仅在永续开放时允许使用永续数据源；下单、盘中监控与到期结算严格同源，消除跨市场基差套利。
func resolveShortEventMarket(symbol string, setting *operation_setting.TradeSetting) (source string, market *TradeMarket, binanceSymbol string, ok bool) {
	if symbol == "" || setting == nil {
		return "", nil, "", false
	}
	if _, isSpot := operation_setting.TradeSymbolOf(symbol); isSpot && setting.SymbolEnabled(symbol) {
		return model.TradeShortSourceSpot, tradeMarket, symbol, true
	}
	if info, isFut := operation_setting.TradeFuturesSymbolOf(symbol); isFut {
		if setting.FuturesEnabled && slices.Contains(setting.FuturesSymbols, symbol) {
			return model.TradeShortSourceFutures, futuresMarket, symbol, true
		}
		if info.Symbol != "" && setting.SymbolEnabled(info.Symbol) {
			return model.TradeShortSourceSpot, tradeMarket, info.Symbol, true
		}
	}
	return "", nil, "", false
}

// shortEventMarketClient 按已记录的规范数据源返回对应的行情中心与 Binance 交易对，绝不跨市场切换。
func shortEventMarketClient(sourceMarket, symbol string) (market *TradeMarket, binanceSymbol string, ok bool) {
	switch sourceMarket {
	case model.TradeShortSourceSpot:
		if _, isSpot := operation_setting.TradeSymbolOf(symbol); isSpot {
			return tradeMarket, symbol, true
		}
		if info, isFut := operation_setting.TradeFuturesSymbolOf(symbol); isFut && info.Symbol != "" {
			return tradeMarket, info.Symbol, true
		}
		return nil, "", false
	case model.TradeShortSourceFutures:
		if _, isFut := operation_setting.TradeFuturesSymbolOf(symbol); isFut {
			return futuresMarket, symbol, true
		}
		if info, isSpot := operation_setting.TradeSymbolOf(symbol); isSpot && info.Futures != "" {
			return futuresMarket, info.Futures, true
		}
		return nil, "", false
	default:
		// 兼容升级前未写 source_market 的历史单。
		_, m, sym, found := resolveShortEventMarket(symbol, operation_setting.GetTradeSetting())
		return m, sym, found
	}
}

func tradeShortSymbolAllowed(symbol string, setting *operation_setting.TradeSetting) bool {
	_, _, _, ok := resolveShortEventMarket(symbol, setting)
	return ok
}

// shortEventLiveMidPrice 从指定规范市场读取实时盘口中间价(买一与卖一均值)，避免使用滞后的 24h miniTicker 成交价。
func shortEventLiveMidPrice(sourceMarket, symbol string) (decimal.Decimal, bool) {
	market, binanceSym, ok := shortEventMarketClient(sourceMarket, symbol)
	if !ok {
		return decimal.Zero, false
	}
	mid, _, ok := market.FreshMidPrice(binanceSym)
	return mid, ok
}

// currentAssetReferencePrice 读取品种最新参考价(仅用于前端展示，优先规范市场的实时盘口中间价)。
func currentAssetReferencePrice(symbol string) (decimal.Decimal, bool) {
	source, market, binanceSym, ok := resolveShortEventMarket(symbol, operation_setting.GetTradeSetting())
	if ok {
		if mid, _, okMid := market.FreshMidPrice(binanceSym); okMid {
			return mid, true
		}
		if p, okLast := market.LastPrice(binanceSym); okLast && p.IsPositive() {
			return p, true
		}
		if source == model.TradeShortSourceFutures {
			if mark, okMark := market.MarkPrice(binanceSym); okMark && mark.Mark.IsPositive() {
				return mark.Mark, true
			}
		}
	}
	if p, ok := tradeMarket.LastPrice(symbol); ok && p.IsPositive() {
		return p, true
	}
	if p, ok := futuresMarket.LastPrice(symbol); ok && p.IsPositive() {
		return p, true
	}
	if mark, ok := futuresMarket.MarkPrice(symbol); ok && mark.Mark.IsPositive() {
		return mark.Mark, true
	}
	return decimal.Zero, false
}

// freshShortOrderPrice 下单前在规范行情源上执行 ping/pong 确认盘口新鲜，读取买一/卖一中间价，
// 同时校验盘口点差与永续资金费率结算窗口，消除滞后价、宽点差与除息跳空套利。
func freshShortOrderPrice(ctx context.Context, arrived time.Time, symbol string, expiresAt int64, setting *operation_setting.TradeSetting) (decimal.Decimal, string, error) {
	source, market, binanceSym, ok := resolveShortEventMarket(symbol, setting)
	if !ok {
		return decimal.Zero, "", ErrTradeSymbolClosed
	}
	staleMs := setting.StaleMs
	if staleMs <= 0 {
		staleMs = 3000
	}
	if err := market.awaitFreshBook(ctx, arrived, staleMs); err != nil {
		return decimal.Zero, "", err
	}
	mid, spreadBps, ok := market.FreshMidPrice(binanceSym)
	if !ok {
		return decimal.Zero, "", ErrTradeMarketUnavailable
	}
	if spreadBps.GreaterThan(tradeShortMaxSpreadBps) {
		return decimal.Zero, "", model.ErrTradeShortSpreadTooWide
	}
	if source == model.TradeShortSourceFutures {
		if mark, hasMark := market.MarkPrice(binanceSym); hasMark && mark.NextFundingTime > 0 {
			fundingSec := mark.NextFundingTime / 1000
			if fundingSec >= arrived.Unix()-5 && fundingSec <= expiresAt+5 &&
				mark.FundingRate.Abs().GreaterThanOrEqual(tradeShortMaxFundingRate) {
				return decimal.Zero, "", model.ErrTradeShortFundingWindow
			}
		}
	}
	return mid, source, nil
}

// fetchShortEventBar 严格从指定同一市场读取 [startSec, endSec) 窗口对应的 1m K 线，绝不跨现货/合约回退。
// strictOpen 为真时仅统计 OpenTime >= startSec*1000 的完整窗口 K 线，杜绝把下单前的历史影线算进触碰判定。
func fetchShortEventBar(ctx context.Context, sourceMarket, symbol string, startSec, endSec int64, strictOpen bool) (openP, closeP, highP, lowP decimal.Decimal, ok bool) {
	market, binanceSym, found := shortEventMarketClient(sourceMarket, symbol)
	if !found {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, false
	}
	spanMin := max(2, int((endSec-startSec)/60)+3)
	if spanMin > 500 {
		spanMin = 500
	}
	endMs := (endSec + 60) * 1000
	klines, err := market.Klines(ctx, binanceSym, "1m", spanMin, endMs)
	if err != nil || len(klines) == 0 {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, false
	}
	startMs := startSec * 1000
	targetEndMs := endSec * 1000
	matched := 0
	for _, bar := range klines {
		if strictOpen {
			if bar.OpenTime < startMs || bar.OpenTime >= targetEndMs {
				continue
			}
		} else {
			if bar.CloseTime < startMs || bar.OpenTime >= targetEndMs {
				continue
			}
		}
		if matched == 0 {
			openP, highP, lowP = bar.Open, bar.High, bar.Low
		}
		if bar.High.GreaterThan(highP) {
			highP = bar.High
		}
		if bar.Low.LessThan(lowP) {
			lowP = bar.Low
		}
		closeP = bar.Close
		matched++
	}
	if matched == 0 {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, false
	}
	return openP, closeP, highP, lowP, closeP.IsPositive()
}

// GetTradeShortPoolRounds 自动确保下一轮可投注的共享奖池局存在(开赛瞬间即封盘，LockTime == StartTime，彻底零盘中偷跑)，
// 同时展示仍有注单正在进行中的当期局。支持 PK 擂台、区间竞猜与极速触碰三类零平台对赌奖池。
func GetTradeShortPoolRounds(kind string, durationSec int64) ([]TradeShortPoolRoundView, error) {
	setting := operation_setting.GetTradeSetting()
	now := common.GetTimestamp()
	if durationSec <= 0 {
		durationSec = 900
	}
	var rounds []*model.TradeShortRound
	appendActiveIfExists := func(roundKey string) {
		var existing model.TradeShortRound
		if err := model.DB.Where("round_key = ? AND status = ? AND total_pool > 0", roundKey, model.TradeShortRoundOpen).First(&existing).Error; err == nil {
			rounds = append(rounds, &existing)
		}
	}
	switch kind {
	case model.TradeShortKindPK:
		if durationSec != 900 && durationSec != 3600 {
			durationSec = 900
		}
		windowStart := now / durationSec * durationSec
		nextStart := windowStart + durationSec
		for _, m := range TradePKMatchups {
			srcA, _, _, okA := resolveShortEventMarket(m.SymbolA, setting)
			srcB, _, _, okB := resolveShortEventMarket(m.SymbolB, setting)
			if !okA || !okB {
				continue
			}
			appendActiveIfExists(fmt.Sprintf("pk:%s:%d:%d", m.Id, durationSec, windowStart))
			r, err := model.EnsureTradeShortRound(model.TradeShortRound{
				RoundKey:      fmt.Sprintf("pk:%s:%d:%d", m.Id, durationSec, nextStart),
				Kind:          model.TradeShortKindPK,
				Title:         m.Title,
				SymbolA:       m.SymbolA,
				SymbolB:       m.SymbolB,
				SourceMarketA: srcA,
				SourceMarketB: srcB,
				DurationSec:   durationSec,
				LockTime:      nextStart, // 开赛瞬间封盘，彻底杜绝盘中看着走势下注。
				StartTime:     nextStart,
				EndTime:       nextStart + durationSec,
				FeeBps:        setting.ShortEventFeeBps,
				Status:        model.TradeShortRoundOpen,
			})
			if err == nil {
				rounds = append(rounds, r)
			}
		}
	case model.TradeShortKindRange:
		if durationSec != 900 && durationSec != 3600 && durationSec != 14400 {
			durationSec = 3600
		}
		windowStart := now / durationSec * durationSec
		nextStart := windowStart + durationSec
		threshold := tradeShortRangeThresholdBps(durationSec)
		for _, sym := range tradeShortFeaturedSymbols {
			srcA, _, _, okA := resolveShortEventMarket(sym, setting)
			if !okA {
				continue
			}
			appendActiveIfExists(fmt.Sprintf("range:%s:%d:%d", sym, durationSec, windowStart))
			r, err := model.EnsureTradeShortRound(model.TradeShortRound{
				RoundKey:      fmt.Sprintf("range:%s:%d:%d", sym, durationSec, nextStart),
				Kind:          model.TradeShortKindRange,
				Title:         sym,
				SymbolA:       sym,
				SourceMarketA: srcA,
				DurationSec:   durationSec,
				LockTime:      nextStart,
				StartTime:     nextStart,
				EndTime:       nextStart + durationSec,
				ThresholdBps:  threshold,
				FeeBps:        setting.ShortEventFeeBps,
				Status:        model.TradeShortRoundOpen,
			})
			if err == nil {
				rounds = append(rounds, r)
			}
		}
	case model.TradeShortKindTouch:
		if durationSec != 900 && durationSec != 3600 {
			durationSec = 900
		}
		windowStart := now / durationSec * durationSec
		nextStart := windowStart + durationSec
		threshold := tradeShortTouchThresholdBps(durationSec)
		for _, sym := range tradeShortFeaturedSymbols {
			srcA, _, _, okA := resolveShortEventMarket(sym, setting)
			if !okA {
				continue
			}
			appendActiveIfExists(fmt.Sprintf("touch:%s:%d:%d", sym, durationSec, windowStart))
			r, err := model.EnsureTradeShortRound(model.TradeShortRound{
				RoundKey:      fmt.Sprintf("touch:%s:%d:%d", sym, durationSec, nextStart),
				Kind:          model.TradeShortKindTouch,
				Title:         sym,
				SymbolA:       sym,
				SourceMarketA: srcA,
				DurationSec:   durationSec,
				LockTime:      nextStart,
				StartTime:     nextStart,
				EndTime:       nextStart + durationSec,
				ThresholdBps:  threshold,
				FeeBps:        setting.ShortEventFeeBps,
				Status:        model.TradeShortRoundOpen,
			})
			if err == nil {
				rounds = append(rounds, r)
			}
		}
	}

	ids := make([]int, len(rounds))
	for i, r := range rounds {
		ids[i] = r.Id
	}
	poolsMap, err := model.GetTradeShortRoundPools(ids)
	if err != nil {
		return nil, err
	}
	views := make([]TradeShortPoolRoundView, 0, len(rounds))
	for _, r := range rounds {
		pools := poolsMap[r.Id]
		if pools == nil {
			pools = map[string]int{}
		}
		mults := map[string]float64{}
		dist := float64(r.TotalPool) * float64(10000-r.FeeBps) / 10000.0
		for choice, amt := range pools {
			if amt > 0 && dist > 0 {
				mults[choice] = dist / float64(amt)
			}
		}
		pA, _ := currentAssetReferencePrice(r.SymbolA)
		pB := decimal.Zero
		if r.SymbolB != "" {
			pB, _ = currentAssetReferencePrice(r.SymbolB)
		}
		views = append(views, TradeShortPoolRoundView{
			TradeShortRound: *r,
			Pools:           pools,
			Multipliers:     mults,
			PriceA:          pA.String(),
			PriceB:          pB.String(),
			Locked:          now >= r.LockTime || now >= r.StartTime || r.Status != model.TradeShortRoundOpen,
		})
	}
	return views, nil
}

type PlaceTradeShortRequest struct {
	Kind        string
	Mode        string
	RoundId     int
	SymbolA     string
	SymbolB     string
	Choice      string
	DurationSec int64
	AmountUsd   decimal.Decimal
}

func validChoiceForShortKind(kind, choice string) bool {
	switch kind {
	case model.TradeShortKindPK:
		return choice == "A" || choice == "B"
	case model.TradeShortKindFlash:
		return choice == "UP" || choice == "DOWN"
	case model.TradeShortKindRange:
		return choice == "B0" || choice == "B1" || choice == "B2" || choice == "B3"
	case model.TradeShortKindTouch:
		return choice == "TOUCH_UP" || choice == "TOUCH_DOWN" || choice == "NO_TOUCH"
	default:
		return false
	}
}

func payoutBpsForInstantShort(baseBps int) int {
	if baseBps < operation_setting.TradeMinShortPayoutBps {
		return 18500
	}
	return min(operation_setting.TradeMaxShortPayoutBps, baseBps)
}

func PlaceTradeShortOrder(ctx context.Context, userId int, req PlaceTradeShortRequest) (*model.TradeShortOrder, error) {
	arrived := time.Now()
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.ShortEventsEnabled {
		return nil, model.ErrTradeShortDisabled
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Choice = strings.ToUpper(strings.TrimSpace(req.Choice))
	req.SymbolA = strings.ToUpper(strings.TrimSpace(req.SymbolA))
	req.SymbolB = strings.ToUpper(strings.TrimSpace(req.SymbolB))

	if req.Mode == "" {
		req.Mode = model.TradeShortModeInstant
	}
	if req.AmountUsd.LessThan(decimal.NewFromInt(1)) ||
		req.AmountUsd.GreaterThan(decimal.NewFromInt(int64(setting.ShortEventMaxOrderUsd))) ||
		req.AmountUsd.Exponent() < -4 {
		return nil, model.ErrTradeShortInvalid
	}
	perUsd, err := model.TradeQuotaPerUsd()
	if err != nil {
		return nil, err
	}
	stakeQuota, err := common.QuotaFromDecimalStrict(req.AmountUsd.Mul(decimal.NewFromInt(int64(perUsd))).Floor())
	if err != nil || stakeQuota <= 0 {
		return nil, model.ErrTradeShortInvalid
	}
	maxActiveQuota, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.ShortEventMaxActiveUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil {
		return nil, model.ErrTradeAmountInvalid
	}
	if err := EnsureTradeSpotPricesFresh(ctx, userId); err != nil {
		return nil, err
	}

	if req.Mode == model.TradeShortModePool {
		round, err := model.GetTradeShortRound(req.RoundId)
		if err != nil {
			return nil, model.ErrTradeShortRoundClosed
		}
		if !validChoiceForShortKind(round.Kind, req.Choice) {
			return nil, model.ErrTradeShortInvalid
		}
		return model.PlaceTradeShortOrder(model.TradeShortOrderInput{
			UserId:        userId,
			RoundId:       round.Id,
			Kind:          round.Kind,
			Mode:          model.TradeShortModePool,
			SymbolA:       round.SymbolA,
			SymbolB:       round.SymbolB,
			SourceMarketA: round.SourceMarketA,
			SourceMarketB: round.SourceMarketB,
			Choice:        req.Choice,
			DurationSec:   round.DurationSec,
			ThresholdBps:  round.ThresholdBps,
			EntryPriceA:   round.StartPriceA,
			EntryPriceB:   round.StartPriceB,
			Stake:         stakeQuota,
			MaxActive:     maxActiveQuota,
			ExpiresAt:     round.EndTime,
			Market:        TradeAccountingMarket(),
		})
	}

	// 即时固定赔率合约仅限 2 选 1 对称方向单(极速涨跌 / 即时 PK)；区间与触碰因胜率高度依赖波动率状态，只开放零平台风险的共享奖池模式。
	if req.Kind != model.TradeShortKindFlash && req.Kind != model.TradeShortKindPK {
		return nil, model.ErrTradeShortModeNotAllowed
	}
	if !validChoiceForShortKind(req.Kind, req.Choice) {
		return nil, model.ErrTradeShortInvalid
	}
	if !tradeShortSymbolAllowed(req.SymbolA, setting) {
		return nil, ErrTradeSymbolClosed
	}
	switch req.Kind {
	case model.TradeShortKindFlash:
		if !slices.Contains(tradeShortFlashDurations, req.DurationSec) {
			return nil, model.ErrTradeShortInvalid
		}
	case model.TradeShortKindPK:
		if !slices.Contains(tradeShortPKDurations, req.DurationSec) || req.SymbolB == "" || req.SymbolB == req.SymbolA || !tradeShortSymbolAllowed(req.SymbolB, setting) {
			return nil, model.ErrTradeShortInvalid
		}
	}

	expiresAt := arrived.Unix() + req.DurationSec
	entryA, sourceA, err := freshShortOrderPrice(ctx, arrived, req.SymbolA, expiresAt, setting)
	if err != nil {
		return nil, err
	}
	entryBStr, sourceB := "", ""
	if req.Kind == model.TradeShortKindPK {
		entryB, srcB, err := freshShortOrderPrice(ctx, arrived, req.SymbolB, expiresAt, setting)
		if err != nil {
			return nil, err
		}
		entryBStr, sourceB = entryB.String(), srcB
	}

	now := common.GetTimestamp()
	return model.PlaceTradeShortOrder(model.TradeShortOrderInput{
		UserId:        userId,
		Kind:          req.Kind,
		Mode:          model.TradeShortModeInstant,
		SymbolA:       req.SymbolA,
		SymbolB:       req.SymbolB,
		SourceMarketA: sourceA,
		SourceMarketB: sourceB,
		Choice:        req.Choice,
		DurationSec:   req.DurationSec,
		EntryPriceA:   entryA.String(),
		EntryPriceB:   entryBStr,
		Stake:         stakeQuota,
		PayoutBps:     payoutBpsForInstantShort(setting.ShortEventPayoutBps),
		MaxActive:     maxActiveQuota,
		ExpiresAt:     now + req.DurationSec,
		Market:        TradeAccountingMarket(),
	})
}

// EvaluateShortRangeBucket 按相对开盘价涨跌幅(万分比)判定所属区间档位 B0..B3；平盘(0%)一律退本，防止休市/横盘品种白给 B2 获胜。
func EvaluateShortRangeBucket(startPrice, endPrice decimal.Decimal, thresholdBps int) string {
	if !startPrice.IsPositive() || !endPrice.IsPositive() || thresholdBps <= 0 {
		return model.TradeShortOutcomeRefund
	}
	changeBps := endPrice.Sub(startPrice).Mul(decimal.NewFromInt(10000)).Div(startPrice)
	if changeBps.IsZero() {
		return model.TradeShortOutcomeRefund
	}
	th := decimal.NewFromInt(int64(thresholdBps))
	switch {
	case changeBps.LessThan(th.Neg()):
		return "B0"
	case changeBps.LessThan(decimal.Zero):
		return "B1"
	case changeBps.LessThanOrEqual(th):
		return "B2"
	default:
		return "B3"
	}
}

// EvaluateShortPKOutcome 比较 A、B 两标的在周期内的涨跌幅高低；完全相等退本。
func EvaluateShortPKOutcome(startA, endA, startB, endB decimal.Decimal) string {
	if !startA.IsPositive() || !endA.IsPositive() || !startB.IsPositive() || !endB.IsPositive() {
		return model.TradeShortOutcomeRefund
	}
	retA := endA.Sub(startA).Div(startA)
	retB := endB.Sub(startB).Div(startB)
	cmp := retA.Cmp(retB)
	if cmp > 0 {
		return "A"
	}
	if cmp < 0 {
		return "B"
	}
	return model.TradeShortOutcomeRefund
}

var tradeShortStartOnce sync.Once

// StartTradeShortEvents 启动短线事件后台结算循环(仅主节点运行，每秒轮询：负责触碰提前结算、即时单同源到期结算、共享奖池结算与崩溃补发)。
func StartTradeShortEvents() {
	if !common.IsMasterNode {
		return
	}
	tradeShortStartOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				settlePendingShortEvents(context.Background())
			}
		}()
	})
}

func settlePendingShortEvents(ctx context.Context) {
	if !model.DB.Migrator().HasTable(&model.TradeShortOrder{}) {
		return
	}
	now := common.GetTimestamp()

	// 0. 崩溃恢复：补发状态已落库但尚有 active 注单的奖池局。
	if pendingIds, err := model.ListUnsettledTradeShortPoolRoundIds(50); err == nil {
		for _, rid := range pendingIds {
			if err := model.SettleTradeShortPoolRound(rid, "", "", "", "", ""); err != nil {
				common.SysError(fmt.Sprintf("short event pool recovery %d: %v", rid, err))
			}
		}
	}

	// 1. 兼容升级前遗留的活跃即时触碰单：严格按同一市场实时中间价判定提前触发。
	var activeTouch []model.TradeShortOrder
	if err := model.DB.Where("mode = ? AND kind = ? AND status = ? AND expires_at > ?",
		model.TradeShortModeInstant, model.TradeShortKindTouch, model.TradeShortStatusActive, now).
		Limit(200).Find(&activeTouch).Error; err == nil {
		for _, ord := range activeTouch {
			entry, err := decimal.NewFromString(ord.EntryPriceA)
			if err != nil || !entry.IsPositive() {
				continue
			}
			cur, ok := shortEventLiveMidPrice(ord.SourceMarketA, ord.SymbolA)
			if !ok {
				continue
			}
			ratio := decimal.NewFromInt(int64(ord.ThresholdBps)).Div(decimal.NewFromInt(10000))
			upper := entry.Mul(decimal.NewFromInt(1).Add(ratio))
			lower := entry.Mul(decimal.NewFromInt(1).Sub(ratio))
			if cur.GreaterThanOrEqual(upper) {
				_ = model.SettleTradeShortInstantOrder(ord.Id, "TOUCH_UP", cur.String(), "")
			} else if cur.LessThanOrEqual(lower) {
				_ = model.SettleTradeShortInstantOrder(ord.Id, "TOUCH_DOWN", cur.String(), "")
			}
		}
	}

	// 2. 到期的即时单结算
	var dueOrders []model.TradeShortOrder
	if err := model.DB.Where("mode = ? AND status = ? AND expires_at <= ?",
		model.TradeShortModeInstant, model.TradeShortStatusActive, now).
		Order("expires_at ASC").Limit(200).Find(&dueOrders).Error; err == nil {
		for _, ord := range dueOrders {
			settleDueInstantShortOrder(ctx, ord, now)
		}
	}

	// 3. 进行中与到期的共享奖池局(开赛后第一时间锁定真实中间价；触碰奖池盘中触及即刻提前结算)
	var openRounds []model.TradeShortRound
	if err := model.DB.Where("status = ? AND start_time <= ?", model.TradeShortRoundOpen, now).
		Order("end_time ASC").Limit(100).Find(&openRounds).Error; err == nil {
		for _, round := range openRounds {
			if round.StartPriceA == "" || (round.SymbolB != "" && round.StartPriceB == "") {
				updates := map[string]any{}
				if round.StartPriceA == "" {
					var pA decimal.Decimal
					var ok bool
					if now-round.StartTime <= tradeShortLiveSettleGraceSec {
						pA, ok = shortEventLiveMidPrice(round.SourceMarketA, round.SymbolA)
					} else {
						pA, _, _, _, ok = fetchShortEventBar(ctx, round.SourceMarketA, round.SymbolA, round.StartTime, round.StartTime+60, true)
					}
					if ok && pA.IsPositive() {
						updates["start_price_a"] = pA.String()
						round.StartPriceA = pA.String()
					}
				}
				if round.SymbolB != "" && round.StartPriceB == "" {
					var pB decimal.Decimal
					var ok bool
					if now-round.StartTime <= tradeShortLiveSettleGraceSec {
						pB, ok = shortEventLiveMidPrice(round.SourceMarketB, round.SymbolB)
					} else {
						pB, _, _, _, ok = fetchShortEventBar(ctx, round.SourceMarketB, round.SymbolB, round.StartTime, round.StartTime+60, true)
					}
					if ok && pB.IsPositive() {
						updates["start_price_b"] = pB.String()
						round.StartPriceB = pB.String()
					}
				}
				if len(updates) > 0 {
					_ = model.DB.Model(&model.TradeShortRound{}).Where("id = ?", round.Id).Updates(updates).Error
				}
			}
			// 触碰共享奖池局在开赛后盘中实时监测，触及上下轨立即提前结算派彩！
			if round.Kind == model.TradeShortKindTouch && round.EndTime > now && round.StartPriceA != "" && round.ThresholdBps > 0 {
				if startA, err := decimal.NewFromString(round.StartPriceA); err == nil && startA.IsPositive() {
					if cur, ok := shortEventLiveMidPrice(round.SourceMarketA, round.SymbolA); ok {
						ratio := decimal.NewFromInt(int64(round.ThresholdBps)).Div(decimal.NewFromInt(10000))
						upper := startA.Mul(decimal.NewFromInt(1).Add(ratio))
						lower := startA.Mul(decimal.NewFromInt(1).Sub(ratio))
						if cur.GreaterThanOrEqual(upper) {
							_ = model.SettleTradeShortPoolRound(round.Id, "TOUCH_UP", startA.String(), "", cur.String(), "")
							continue
						}
						if cur.LessThanOrEqual(lower) {
							_ = model.SettleTradeShortPoolRound(round.Id, "TOUCH_DOWN", startA.String(), "", cur.String(), "")
							continue
						}
					}
				}
			}
			if round.EndTime <= now {
				settleDuePoolShortRound(ctx, round, now)
			}
		}
	}
}

func resolveInstantClosePrice(ctx context.Context, sourceMarket, symbol string, startSec, expiresAt, now int64) (decimal.Decimal, bool) {
	// 正常准点到期(15 秒内)直接使用同一市场的实时盘口中间价，与下单入场价完全同源。
	if now-expiresAt <= tradeShortLiveSettleGraceSec {
		if live, ok := shortEventLiveMidPrice(sourceMarket, symbol); ok {
			return live, true
		}
	}
	_, closeP, _, _, ok := fetchShortEventBar(ctx, sourceMarket, symbol, startSec, expiresAt, false)
	return closeP, ok
}

func settleDueInstantShortOrder(ctx context.Context, ord model.TradeShortOrder, now int64) {
	entryA, errA := decimal.NewFromString(ord.EntryPriceA)
	if errA != nil || !entryA.IsPositive() {
		_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
		return
	}

	outcome := model.TradeShortOutcomeRefund
	settleAStr, settleBStr := "", ""
	switch ord.Kind {
	case model.TradeShortKindFlash:
		closeA, okA := resolveInstantClosePrice(ctx, ord.SourceMarketA, ord.SymbolA, ord.CreatedAt, ord.ExpiresAt, now)
		if !okA {
			if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
				_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
			}
			return
		}
		settleAStr = closeA.String()
		switch closeA.Cmp(entryA) {
		case 1:
			outcome = "UP"
		case -1:
			outcome = "DOWN"
		default:
			outcome = model.TradeShortOutcomeRefund
		}
	case model.TradeShortKindPK:
		entryB, errB := decimal.NewFromString(ord.EntryPriceB)
		if errB != nil || !entryB.IsPositive() {
			outcome = model.TradeShortOutcomeRefund
		} else {
			closeA, okA := resolveInstantClosePrice(ctx, ord.SourceMarketA, ord.SymbolA, ord.CreatedAt, ord.ExpiresAt, now)
			closeB, okB := resolveInstantClosePrice(ctx, ord.SourceMarketB, ord.SymbolB, ord.CreatedAt, ord.ExpiresAt, now)
			if !okA || !okB {
				if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
					_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
				}
				return
			}
			settleAStr, settleBStr = closeA.String(), closeB.String()
			outcome = EvaluateShortPKOutcome(entryA, closeA, entryB, closeB)
		}
	case model.TradeShortKindRange:
		// 仅兼容升级前历史遗留单。
		closeA, okA := resolveInstantClosePrice(ctx, ord.SourceMarketA, ord.SymbolA, ord.CreatedAt, ord.ExpiresAt, now)
		if !okA {
			if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
				_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
			}
			return
		}
		settleAStr = closeA.String()
		outcome = EvaluateShortRangeBucket(entryA, closeA, ord.ThresholdBps)
	case model.TradeShortKindTouch:
		// 仅兼容升级前历史遗留单；严格排除下单前开盘的 K 线，杜绝回溯旧影线。
		_, closeA, highA, lowA, okA := fetchShortEventBar(ctx, ord.SourceMarketA, ord.SymbolA, ord.CreatedAt, ord.ExpiresAt, true)
		if !okA {
			if liveA, ok := shortEventLiveMidPrice(ord.SourceMarketA, ord.SymbolA); ok {
				closeA, highA, lowA, okA = liveA, liveA, liveA, true
			}
		}
		if !okA {
			if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
				_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
			}
			return
		}
		settleAStr = closeA.String()
		ratio := decimal.NewFromInt(int64(ord.ThresholdBps)).Div(decimal.NewFromInt(10000))
		upper := entryA.Mul(decimal.NewFromInt(1).Add(ratio))
		lower := entryA.Mul(decimal.NewFromInt(1).Sub(ratio))
		hitUp := highA.GreaterThanOrEqual(upper)
		hitDown := lowA.LessThanOrEqual(lower)
		switch {
		case hitUp && hitDown:
			outcome = model.TradeShortOutcomeRefund
		case hitUp:
			outcome = "TOUCH_UP"
		case hitDown:
			outcome = "TOUCH_DOWN"
		default:
			outcome = "NO_TOUCH"
		}
	}
	if err := model.SettleTradeShortInstantOrder(ord.Id, outcome, settleAStr, settleBStr); err != nil {
		common.SysError(fmt.Sprintf("short event instant settle %d: %v", ord.Id, err))
	}
}

func settleDuePoolShortRound(ctx context.Context, round model.TradeShortRound, now int64) {
	openA, closeA, highA, lowA, okA := fetchShortEventBar(ctx, round.SourceMarketA, round.SymbolA, round.StartTime, round.EndTime, true)
	if !okA {
		if liveA, ok := shortEventLiveMidPrice(round.SourceMarketA, round.SymbolA); ok {
			openA, closeA, highA, lowA, okA = liveA, liveA, liveA, liveA, true
		}
	}
	if !okA {
		if now-round.EndTime >= tradeShortFallbackRefundSec {
			_ = model.SettleTradeShortPoolRound(round.Id, model.TradeShortOutcomeRefund, round.StartPriceA, round.StartPriceB, "", "")
		}
		return
	}
	startA := openA
	if parsed, err := decimal.NewFromString(round.StartPriceA); err == nil && parsed.IsPositive() {
		startA = parsed
	}
	startBStr, endBStr := round.StartPriceB, ""
	outcome := model.TradeShortOutcomeRefund
	switch round.Kind {
	case model.TradeShortKindPK:
		openB, closeB, _, _, okB := fetchShortEventBar(ctx, round.SourceMarketB, round.SymbolB, round.StartTime, round.EndTime, true)
		if !okB {
			if liveB, ok := shortEventLiveMidPrice(round.SourceMarketB, round.SymbolB); ok {
				openB, closeB, okB = liveB, liveB, true
			}
		}
		if !okB {
			if now-round.EndTime >= tradeShortFallbackRefundSec {
				_ = model.SettleTradeShortPoolRound(round.Id, model.TradeShortOutcomeRefund, startA.String(), startBStr, closeA.String(), "")
			}
			return
		}
		startB := openB
		if parsed, err := decimal.NewFromString(round.StartPriceB); err == nil && parsed.IsPositive() {
			startB = parsed
		}
		startBStr = startB.String()
		endBStr = closeB.String()
		outcome = EvaluateShortPKOutcome(startA, closeA, startB, closeB)
	case model.TradeShortKindRange:
		outcome = EvaluateShortRangeBucket(startA, closeA, round.ThresholdBps)
	case model.TradeShortKindTouch:
		if round.ThresholdBps > 0 && startA.IsPositive() {
			ratio := decimal.NewFromInt(int64(round.ThresholdBps)).Div(decimal.NewFromInt(10000))
			upper := startA.Mul(decimal.NewFromInt(1).Add(ratio))
			lower := startA.Mul(decimal.NewFromInt(1).Sub(ratio))
			hitUp := highA.GreaterThanOrEqual(upper)
			hitDown := lowA.LessThanOrEqual(lower)
			switch {
			case hitUp && hitDown:
				outcome = model.TradeShortOutcomeRefund
			case hitUp:
				outcome = "TOUCH_UP"
			case hitDown:
				outcome = "TOUCH_DOWN"
			default:
				outcome = "NO_TOUCH"
			}
		}
	}
	if err := model.SettleTradeShortPoolRound(round.Id, outcome, startA.String(), startBStr, closeA.String(), endBStr); err != nil {
		common.SysError(fmt.Sprintf("short event pool settle %d: %v", round.Id, err))
	}
}

func valueTradeShortOrders(orders []model.TradeShortOrder, accounting bool) (int, error) {
	if len(orders) == 0 {
		return 0, nil
	}
	// 涉及游戏币本金结算的划转时，若有尚未结算的短线单，暂停划转待其自动结算后再估值，杜绝亏损偷转。
	if accounting {
		return 0, model.ErrTradeMarkUnavailable
	}
	total := 0
	for _, ord := range orders {
		if ord.Stake < 0 || total >= common.MaxQuota-ord.Stake {
			return 0, model.ErrTradeAmountInvalid
		}
		total += ord.Stake
	}
	return total, nil
}
