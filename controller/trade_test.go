package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
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
