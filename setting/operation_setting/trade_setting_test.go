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
