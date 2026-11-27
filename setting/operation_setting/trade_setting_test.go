package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 美股代币、股票与大宗商品合约都要标出标的市场，前端按它画交易时段(web/classic/src/pages/Trade/marketSession.js)；加密货币
// 没有标的市场。
func TestTradeSymbolsDeclareUnderlyingSession(t *testing.T) {
	seenTickers := map[string]bool{}
	seenSymbols := map[string]bool{}
	seenFutures := map[string]bool{}
	for _, info := range TradeSymbols {
		assert.NotEmpty(t, info.Ticker)
		assert.False(t, seenTickers[info.Ticker], "duplicate ticker: %s", info.Ticker)
		seenTickers[info.Ticker] = true
		if info.Symbol != "" {
			assert.False(t, seenSymbols[info.Symbol], "duplicate spot symbol: %s", info.Symbol)
			seenSymbols[info.Symbol] = true
		}
		if info.Futures != "" {
			assert.False(t, seenFutures[info.Futures], "duplicate futures symbol: %s", info.Futures)
			seenFutures[info.Futures] = true
		}
		if info.Kind == TradeKindCrypto {
			assert.Empty(t, info.Session, info.Ticker)
			continue
		}
		assert.Contains(t, []string{
			TradeSessionUS,
			TradeSessionKRX,
			TradeSessionHK,
			TradeSessionCN,
			TradeSessionCME,
			TradeSessionPremarket,
		}, info.Session, info.Ticker)
	}
}

func TestValidateTradeBonusAndWagerOptions(t *testing.T) {
	assert.NoError(t, ValidateTradeSetting(DefaultTradeSetting()))
	for key, val := range map[string]string{
		"bonus_enabled":              "true",
		"bonus_pool_daily_usd":       "1000",
		"bonus_first_amount_usd":     "100",
		"bonus_first_profit_cap_usd": "20",
		"bonus_active_days":          "7",
		"bonus_min_used_usd":         "0.5",
		"bonus_match_enabled":        "true",
		"bonus_match_period":         "weekly",
		"bonus_match_max_usd":        "50",
		"bonus_task_wager_coins":     "5",
		"bonus_turnstile_enabled":    "false",
		"trade_wager_spot_bps":       "10000",
		"trade_wager_futures_bps":    "3000",
		"ranking_bonus_enabled":      "true",
		"ranking_bonus_period":       "daily",
		"ranking_bonus_top_n":        "10",
		"ranking_bonus_amount_usd":   "50",
		"short_events_enabled":       "true",
		"short_event_fee_bps":        "300",
		"short_event_payout_bps":     "18500",
		"short_event_max_order_usd":  "100",
		"short_event_max_active_usd": "500",
		"sprint_enabled":             "true",
		"sprint_entry_fee_usd":       "10",
		"sprint_fee_bps":             "500",
	} {
		assert.NoError(t, ValidateTradeOption(TradeSettingPrefix+key, val), key)
	}
	for key, val := range map[string]string{
		"bonus_pool_daily_usd":       "-1",
		"bonus_first_amount_usd":     "100001",
		"bonus_min_used_usd":         "NaN",
		"bonus_match_period":         "monthly",
		"bonus_task_wager_coins":     "-5",
		"trade_wager_spot_bps":       "10001",
		"trade_wager_futures_bps":    "-1",
		"ranking_bonus_period":       "monthly",
		"ranking_bonus_top_n":        "0",
		"ranking_bonus_amount_usd":   "-1",
		"short_event_fee_bps":        "1001",
		"short_event_payout_bps":     "9999",
		"short_event_max_order_usd":  "0",
		"short_event_max_active_usd": "0",
		"sprint_entry_fee_usd":       "0",
		"sprint_fee_bps":             "2001",
	} {
		assert.Error(t, ValidateTradeOption(TradeSettingPrefix+key, val), key)
	}
}

func TestTradeRankingBonusUsdTiers(t *testing.T) {
	assert.Equal(t, 0, TradeRankingBonusUsd(0, 10, 20))
	assert.Equal(t, 0, TradeRankingBonusUsd(11, 10, 20))
	assert.Equal(t, 20, TradeRankingBonusUsd(1, 10, 20))
	assert.Equal(t, 14, TradeRankingBonusUsd(2, 10, 20))
	assert.Equal(t, 14, TradeRankingBonusUsd(3, 10, 20))
	assert.Equal(t, 8, TradeRankingBonusUsd(4, 10, 20))
	assert.Equal(t, 8, TradeRankingBonusUsd(10, 10, 20))
	assert.Equal(t, 1, TradeRankingBonusUsd(5, 10, 1))
}
