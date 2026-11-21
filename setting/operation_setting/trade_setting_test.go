package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 美股代币、股票与大宗商品合约都要标出标的市场，前端按它画交易时段(web/classic/src/pages/Trade/marketSession.js)；加密货币
// 没有标的市场。
func TestTradeSymbolsDeclareUnderlyingSession(t *testing.T) {
	for _, info := range TradeSymbols {
		if info.Kind == TradeKindCrypto {
			assert.Empty(t, info.Session, info.Ticker)
			continue
		}
		assert.Contains(t, []string{TradeSessionUS, TradeSessionKRX, TradeSessionCME}, info.Session, info.Ticker)
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
	} {
		assert.Error(t, ValidateTradeOption(TradeSettingPrefix+key, val), key)
	}
}
