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
)

// 短线事件结算若行情连续 15 分钟彻底不可用，超时自动退还本金，防止资金永久冻结。
const tradeShortFallbackRefundSec = int64(900)

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

func tradeShortSymbolAllowed(symbol string, setting *operation_setting.TradeSetting) bool {
	if symbol == "" || setting == nil {
		return false
	}
	if _, ok := operation_setting.TradeSymbolOf(symbol); ok && setting.SymbolEnabled(symbol) {
		return true
	}
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); ok && slices.Contains(setting.FuturesSymbols, symbol) {
		return true
	}
	return false
}

// currentAssetReferencePrice 读取品种最新参考价(优先现货、再合约最新价/标记价)，同时支持传入现货或合约代码。
func currentAssetReferencePrice(symbol string) (decimal.Decimal, bool) {
	if p, ok := tradeMarket.LastPrice(symbol); ok && p.IsPositive() {
		return p, true
	}
	if p, ok := futuresMarket.LastPrice(symbol); ok && p.IsPositive() {
		return p, true
	}
	if mark, ok := futuresMarket.MarkPrice(symbol); ok && mark.Mark.IsPositive() {
		return mark.Mark, true
	}
	// 若传的是永续代码(如 NVDAUSDT)，尝试查找对应现货(NVDABUSDT)。
	if info, ok := operation_setting.TradeFuturesSymbolOf(symbol); ok && info.Symbol != "" {
		if p, ok := tradeMarket.LastPrice(info.Symbol); ok && p.IsPositive() {
			return p, true
		}
	}
	// 若传的是现货代码(如 NVDABUSDT)，尝试查找对应永续(NVDAUSDT)。
	if info, ok := operation_setting.TradeSymbolOf(symbol); ok && info.Futures != "" {
		if p, ok := futuresMarket.LastPrice(info.Futures); ok && p.IsPositive() {
			return p, true
		}
		if mark, ok := futuresMarket.MarkPrice(info.Futures); ok && mark.Mark.IsPositive() {
			return mark.Mark, true
		}
	}
	return decimal.Zero, false
}

// freshAssetPriceForShortOrder 下单前确认行情连接新鲜再读取锚定价格，防止断线陈旧价被套利。
func freshAssetPriceForShortOrder(ctx context.Context, symbol string) (decimal.Decimal, error) {
	staleMs := operation_setting.GetTradeSetting().StaleMs
	if staleMs <= 0 {
		staleMs = 3000
	}
	now := time.Now()
	if _, ok := operation_setting.TradeSymbolOf(symbol); ok {
		if err := tradeMarket.awaitFreshBook(ctx, now, staleMs); err == nil {
			if p, ok := tradeMarket.LastPrice(symbol); ok && p.IsPositive() {
				return p, nil
			}
		}
	}
	if _, ok := operation_setting.TradeFuturesSymbolOf(symbol); ok {
		if err := futuresMarket.awaitFreshBook(ctx, now, staleMs); err == nil {
			if p, ok := futuresMarket.LastPrice(symbol); ok && p.IsPositive() {
				return p, nil
			}
			if mark, ok := futuresMarket.MarkPrice(symbol); ok && mark.Mark.IsPositive() {
				return mark.Mark, nil
			}
		}
	}
	// 兜底：若标的对应现货在连，等待现货盘口确认。
	if info, ok := operation_setting.TradeFuturesSymbolOf(symbol); ok && info.Symbol != "" {
		if err := tradeMarket.awaitFreshBook(ctx, now, staleMs); err == nil {
			if p, ok := tradeMarket.LastPrice(info.Symbol); ok && p.IsPositive() {
				return p, nil
			}
		}
	}
	return decimal.Zero, ErrTradeMarketUnavailable
}

// fetchAssetBarForWindow 读取指定时间窗口对应的 Binance 1m K 线，返回开盘价、收盘价、最高价与最低价。
func fetchAssetBarForWindow(ctx context.Context, symbol string, startSec, endSec int64) (openP, closeP, highP, lowP decimal.Decimal, ok bool) {
	spanMin := max(2, int((endSec-startSec)/60)+3)
	if spanMin > 500 {
		spanMin = 500
	}
	endMs := (endSec + 60) * 1000
	klines, err := futuresMarket.Klines(ctx, symbol, "1m", spanMin, endMs)
	if err != nil || len(klines) == 0 {
		klines, err = tradeMarket.Klines(ctx, symbol, "1m", spanMin, endMs)
	}
	if (err != nil || len(klines) == 0) && symbol != "" {
		if info, found := operation_setting.TradeFuturesSymbolOf(symbol); found && info.Symbol != "" {
			klines, err = tradeMarket.Klines(ctx, info.Symbol, "1m", spanMin, endMs)
		}
	}
	if err != nil || len(klines) == 0 {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, false
	}
	startMs := startSec * 1000
	targetEndMs := endSec * 1000
	matched := 0
	for _, bar := range klines {
		if bar.CloseTime < startMs || bar.OpenTime >= targetEndMs {
			continue
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
		last := klines[len(klines)-1]
		return last.Open, last.Close, last.High, last.Low, last.Close.IsPositive()
	}
	return openP, closeP, highP, lowP, closeP.IsPositive()
}

// GetTradeShortPoolRounds 自动确保当前周期的热门 PK 擂台与区间竞猜共享奖池局存在，并返回实时池子与赔率。
func GetTradeShortPoolRounds(kind string, durationSec int64) ([]TradeShortPoolRoundView, error) {
	setting := operation_setting.GetTradeSetting()
	now := common.GetTimestamp()
	if durationSec <= 0 {
		durationSec = 900
	}
	var rounds []*model.TradeShortRound
	switch kind {
	case model.TradeShortKindPK:
		if durationSec != 900 && durationSec != 3600 {
			durationSec = 900
		}
		windowStart := now / durationSec * durationSec
		// 投注截止为本轮前 25% 时间(如 15m 局前 3m45s，1h 局前 15m)，进入锁盘期后同时展示下一轮可提前下注。
		lockDuration := max(int64(180), durationSec/4)
		windows := []int64{windowStart}
		if now >= windowStart+lockDuration {
			windows = append(windows, windowStart+durationSec)
		}
		for _, ws := range windows {
			for _, m := range TradePKMatchups {
				if !tradeShortSymbolAllowed(m.SymbolA, setting) || !tradeShortSymbolAllowed(m.SymbolB, setting) {
					continue
				}
				pA, _ := currentAssetReferencePrice(m.SymbolA)
				pB, _ := currentAssetReferencePrice(m.SymbolB)
				startA, startB := "", ""
				if ws == windowStart {
					if pA.IsPositive() {
						startA = pA.String()
					}
					if pB.IsPositive() {
						startB = pB.String()
					}
				}
				lockAt := ws + lockDuration
				if ws > windowStart {
					lockAt = ws // 预售局在开赛瞬间封盘，完全零前瞻。
				}
				r, err := model.EnsureTradeShortRound(model.TradeShortRound{
					RoundKey:    fmt.Sprintf("pk:%s:%d:%d", m.Id, durationSec, ws),
					Kind:        model.TradeShortKindPK,
					Title:       m.Title,
					SymbolA:     m.SymbolA,
					SymbolB:     m.SymbolB,
					DurationSec: durationSec,
					LockTime:    lockAt,
					StartTime:   ws,
					EndTime:     ws + durationSec,
					StartPriceA: startA,
					StartPriceB: startB,
					FeeBps:      setting.ShortEventFeeBps,
					Status:      model.TradeShortRoundOpen,
				})
				if err == nil {
					rounds = append(rounds, r)
				}
			}
		}
	case model.TradeShortKindRange:
		if durationSec != 900 && durationSec != 3600 && durationSec != 14400 {
			durationSec = 3600
		}
		windowStart := now / durationSec * durationSec
		lockDuration := max(int64(180), durationSec/4)
		featured := []string{"BTCUSDT", "ETHUSDT", "NVDAUSDT", "ANTUSDT", "OPENAIUSDT", "XAUUSDT"}
		threshold := tradeShortRangeThresholdBps(durationSec)
		for _, sym := range featured {
			if !tradeShortSymbolAllowed(sym, setting) {
				continue
			}
			ws := windowStart
			lockAt := ws + lockDuration
			if now >= lockAt {
				ws = windowStart + durationSec
				lockAt = ws
			}
			pA, _ := currentAssetReferencePrice(sym)
			startA := ""
			if ws == windowStart && pA.IsPositive() {
				startA = pA.String()
			}
			r, err := model.EnsureTradeShortRound(model.TradeShortRound{
				RoundKey:     fmt.Sprintf("range:%s:%d:%d", sym, durationSec, ws),
				Kind:         model.TradeShortKindRange,
				Title:        sym,
				SymbolA:      sym,
				DurationSec:  durationSec,
				LockTime:     lockAt,
				StartTime:    ws,
				EndTime:      ws + durationSec,
				ThresholdBps: threshold,
				StartPriceA:  startA,
				FeeBps:       setting.ShortEventFeeBps,
				Status:       model.TradeShortRoundOpen,
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
			Locked:          now >= r.LockTime || r.Status != model.TradeShortRoundOpen,
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

func payoutBpsForInstantShort(kind, choice string, baseBps int) int {
	if baseBps < operation_setting.TradeMinShortPayoutBps {
		baseBps = 18500
	}
	switch kind {
	case model.TradeShortKindRange:
		return min(35000, baseBps*175/100) // 4 档区间约 3.23x
	case model.TradeShortKindTouch:
		if choice == "NO_TOUCH" {
			return max(11000, baseBps-1500) // 约 1.70x
		}
		return min(24000, baseBps+2500) // 触碰约 2.10x
	default:
		return baseBps // 涨跌 / 即时 PK 默认 1.85x
	}
}

func PlaceTradeShortOrder(ctx context.Context, userId int, req PlaceTradeShortRequest) (*model.TradeShortOrder, error) {
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
			UserId:       userId,
			RoundId:      round.Id,
			Kind:         round.Kind,
			Mode:         model.TradeShortModePool,
			SymbolA:      round.SymbolA,
			SymbolB:      round.SymbolB,
			Choice:       req.Choice,
			DurationSec:  round.DurationSec,
			ThresholdBps: round.ThresholdBps,
			EntryPriceA:  round.StartPriceA,
			EntryPriceB:  round.StartPriceB,
			Stake:        stakeQuota,
			MaxActive:    maxActiveQuota,
			ExpiresAt:    round.EndTime,
			Market:       TradeAccountingMarket(),
		})
	}

	// 即时合约模式：校验品种、周期、并同步获取最新锚定价格。
	if !validChoiceForShortKind(req.Kind, req.Choice) {
		return nil, model.ErrTradeShortInvalid
	}
	if !tradeShortSymbolAllowed(req.SymbolA, setting) {
		return nil, ErrTradeSymbolClosed
	}
	thresholdBps := 0
	switch req.Kind {
	case model.TradeShortKindFlash:
		if !slices.Contains(tradeShortFlashDurations, req.DurationSec) {
			return nil, model.ErrTradeShortInvalid
		}
	case model.TradeShortKindPK:
		if !slices.Contains(tradeShortPKDurations, req.DurationSec) || req.SymbolB == "" || req.SymbolB == req.SymbolA || !tradeShortSymbolAllowed(req.SymbolB, setting) {
			return nil, model.ErrTradeShortInvalid
		}
	case model.TradeShortKindRange:
		if !slices.Contains(tradeShortRangeDurations, req.DurationSec) {
			return nil, model.ErrTradeShortInvalid
		}
		thresholdBps = tradeShortRangeThresholdBps(req.DurationSec)
	case model.TradeShortKindTouch:
		if !slices.Contains(tradeShortTouchDurations, req.DurationSec) {
			return nil, model.ErrTradeShortInvalid
		}
		thresholdBps = tradeShortTouchThresholdBps(req.DurationSec)
	default:
		return nil, model.ErrTradeShortInvalid
	}

	entryA, err := freshAssetPriceForShortOrder(ctx, req.SymbolA)
	if err != nil {
		return nil, err
	}
	entryBStr := ""
	if req.Kind == model.TradeShortKindPK {
		entryB, err := freshAssetPriceForShortOrder(ctx, req.SymbolB)
		if err != nil {
			return nil, err
		}
		entryBStr = entryB.String()
	}

	now := common.GetTimestamp()
	return model.PlaceTradeShortOrder(model.TradeShortOrderInput{
		UserId:       userId,
		Kind:         req.Kind,
		Mode:         model.TradeShortModeInstant,
		SymbolA:      req.SymbolA,
		SymbolB:      req.SymbolB,
		Choice:       req.Choice,
		DurationSec:  req.DurationSec,
		ThresholdBps: thresholdBps,
		EntryPriceA:  entryA.String(),
		EntryPriceB:  entryBStr,
		Stake:        stakeQuota,
		PayoutBps:    payoutBpsForInstantShort(req.Kind, req.Choice, setting.ShortEventPayoutBps),
		MaxActive:    maxActiveQuota,
		ExpiresAt:    now + req.DurationSec,
		Market:       TradeAccountingMarket(),
	})
}

// EvaluateShortRangeBucket 按相对开盘价涨跌幅(万分比)判定所属区间档位 B0..B3。
func EvaluateShortRangeBucket(startPrice, endPrice decimal.Decimal, thresholdBps int) string {
	if !startPrice.IsPositive() || !endPrice.IsPositive() || thresholdBps <= 0 {
		return model.TradeShortOutcomeRefund
	}
	changeBps := endPrice.Sub(startPrice).Mul(decimal.NewFromInt(10000)).Div(startPrice)
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

// StartTradeShortEvents 启动短线事件后台结算循环(3 秒轮询，负责触碰提前结算、即时单到期结算与共享奖池结算)。
func StartTradeShortEvents() {
	tradeShortStartOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(3 * time.Second)
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

	// 1. 盘中检查活跃的极速触碰单：触及目标即刻提前结算派彩！
	var activeTouch []model.TradeShortOrder
	if err := model.DB.Where("mode = ? AND kind = ? AND status = ? AND expires_at > ?",
		model.TradeShortModeInstant, model.TradeShortKindTouch, model.TradeShortStatusActive, now).
		Limit(200).Find(&activeTouch).Error; err == nil {
		for _, ord := range activeTouch {
			entry, err := decimal.NewFromString(ord.EntryPriceA)
			if err != nil || !entry.IsPositive() {
				continue
			}
			cur, ok := currentAssetReferencePrice(ord.SymbolA)
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

	// 3. 到期的共享奖池局结算(同时填充开始后尚未记录开盘价的预售局)
	var openRounds []model.TradeShortRound
	if err := model.DB.Where("status = ? AND start_time <= ?", model.TradeShortRoundOpen, now).
		Order("end_time ASC").Limit(100).Find(&openRounds).Error; err == nil {
		for _, round := range openRounds {
			if round.StartPriceA == "" || (round.SymbolB != "" && round.StartPriceB == "") {
				updates := map[string]any{}
				if round.StartPriceA == "" {
					if pA, ok := currentAssetReferencePrice(round.SymbolA); ok {
						updates["start_price_a"] = pA.String()
						round.StartPriceA = pA.String()
					}
				}
				if round.SymbolB != "" && round.StartPriceB == "" {
					if pB, ok := currentAssetReferencePrice(round.SymbolB); ok {
						updates["start_price_b"] = pB.String()
						round.StartPriceB = pB.String()
					}
				}
				if len(updates) > 0 {
					_ = model.DB.Model(&model.TradeShortRound{}).Where("id = ?", round.Id).Updates(updates).Error
				}
			}
			if round.EndTime <= now {
				settleDuePoolShortRound(ctx, round, now)
			}
		}
	}
}

func settleDueInstantShortOrder(ctx context.Context, ord model.TradeShortOrder, now int64) {
	entryA, errA := decimal.NewFromString(ord.EntryPriceA)
	if errA != nil || !entryA.IsPositive() {
		_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
		return
	}
	_, closeA, highA, lowA, okA := fetchAssetBarForWindow(ctx, ord.SymbolA, ord.CreatedAt, ord.ExpiresAt)
	if !okA {
		if liveA, ok := currentAssetReferencePrice(ord.SymbolA); ok {
			closeA, highA, lowA, okA = liveA, liveA, liveA, true
		}
	}
	if !okA {
		if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
			_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, "", "")
		}
		return
	}

	outcome := model.TradeShortOutcomeRefund
	settleBStr := ""
	switch ord.Kind {
	case model.TradeShortKindFlash:
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
			_, closeB, _, _, okB := fetchAssetBarForWindow(ctx, ord.SymbolB, ord.CreatedAt, ord.ExpiresAt)
			if !okB {
				if liveB, ok := currentAssetReferencePrice(ord.SymbolB); ok {
					closeB, okB = liveB, true
				}
			}
			if !okB {
				if now-ord.ExpiresAt >= tradeShortFallbackRefundSec {
					_ = model.SettleTradeShortInstantOrder(ord.Id, model.TradeShortOutcomeRefund, closeA.String(), "")
				}
				return
			}
			settleBStr = closeB.String()
			outcome = EvaluateShortPKOutcome(entryA, closeA, entryB, closeB)
		}
	case model.TradeShortKindRange:
		outcome = EvaluateShortRangeBucket(entryA, closeA, ord.ThresholdBps)
	case model.TradeShortKindTouch:
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
	if err := model.SettleTradeShortInstantOrder(ord.Id, outcome, closeA.String(), settleBStr); err != nil {
		common.SysError(fmt.Sprintf("short event instant settle %d: %v", ord.Id, err))
	}
}

func settleDuePoolShortRound(ctx context.Context, round model.TradeShortRound, now int64) {
	openA, closeA, _, _, okA := fetchAssetBarForWindow(ctx, round.SymbolA, round.StartTime, round.EndTime)
	if !okA {
		if liveA, ok := currentAssetReferencePrice(round.SymbolA); ok {
			openA, closeA, okA = liveA, liveA, true
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
		openB, closeB, _, _, okB := fetchAssetBarForWindow(ctx, round.SymbolB, round.StartTime, round.EndTime)
		if !okB {
			if liveB, ok := currentAssetReferencePrice(round.SymbolB); ok {
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
