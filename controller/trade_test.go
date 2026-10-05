package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 委托的成交均价按记下的成交价值算，数量很小时也不受金额取整到额度单位的影响；没有记成交价值的委托按成交金额算。
func TestTradeOrderViewAveragePrice(t *testing.T) {
	const perUsd = 500_000
	tests := []struct {
		name  string
		order model.TradeOrder
		want  string
	}{
		{
			name:  "0.00007 BTC at 85969.91, 6.0178937 USDT rounded up to 3008947 quota",
			order: model.TradeOrder{FilledQty: 7_000, FilledValue: "6.0178937", FilledAmount: 3_008_947},
			want:  "85969.91",
		},
		{
			name:  "filled before the value was recorded",
			order: model.TradeOrder{FilledQty: 7_000, FilledAmount: 3_008_947},
			want:  "85969.91428571",
		},
		{
			name:  "nothing filled",
			order: model.TradeOrder{},
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, tradeOrderView(&test.order, perUsd)["avg_price"])
		})
	}
}

// 钱包选择和整币数量在访问账户之前校验，非法输入不能落入默认额度转账路径。
func TestTradeTransferRejectsInvalidWalletInputs(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
	for _, tc := range []struct {
		name, body, message string
	}{
		{"unknown wallet", `{"direction":"in","amount":"1","wallet":"other"}`, "Invalid"},
		{"fractional coins", `{"direction":"in","amount":"1.5","wallet":"game_coin"}`, "whole number"},
		{"zero coins", `{"direction":"out","amount":"0","wallet":"game_coin"}`, "Invalid transfer amount"},
		{"negative coins", `{"direction":"out","amount":"-1","wallet":"game_coin"}`, "Invalid transfer amount"},
		{"oversized coins", `{"direction":"out","amount":"9007199254740992","wallet":"game_coin"}`, "Invalid transfer amount"},
		{"invalid direction", `{"direction":"send","amount":"1","wallet":"game_coin"}`, "Invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest("POST", "/api/trade/transfer", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Accept-Language", "en")
			ctx.Set("id", 9001)
			TransferTrade(ctx)
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.False(t, body.Success)
			assert.Contains(t, body.Message, tc.message)
		})
	}
}
