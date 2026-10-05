package model

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 不是用户当场操作的事都记一条通知：挂着的限价单之后成交、强平、亏空从站内额度扣除；页面按 id 取比上次新的。
func TestTradeNoticesRecordBackgroundEvents(t *testing.T) {
	truncateTables(t)
	seedTradeFuturesAccount(t, 4016, 120)
	order, err := PlaceTradeOrder(TradeOrderInput{UserId: 4016, Symbol: "BTCUSDT", Side: TradeSideBuy, Type: TradeOrderTypeLimit,
		Price: "100", Qty: tradeFuturesCoin, Rest: true, Freeze: tradeUsd(100.1)})
	require.NoError(t, err)
	notices, latestId, err := GetTradeNotices(4016, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, notices, "placing an order is the user's own action")
	assert.Zero(t, latestId)

	_, err = FillTradeOrder(order.Id, TradeFill{Qty: tradeFuturesCoin, Value: decimal.NewFromInt(100), Amount: tradeUsd(100), Fee: tradeUsd(0.1), Price: "100"})
	require.NoError(t, err)
	openTradeFutures(t, 4016, tradeFuturesSymbol, TradeFuturesIsolated, TradeFuturesLong, tradeFuturesCoin, "100", 10, 0, nil)
	liquidated, err := LiquidateTradeFuturesPosition(4016, tradeFuturesSymbol, TradeFuturesLong, decimal.RequireFromString("50"),
		tradeTestMarket{}.Brackets(tradeFuturesSymbol), 4)
	require.NoError(t, err)
	require.True(t, liquidated)

	notices, latestId, err = GetTradeNotices(4016, 0, 10)
	require.NoError(t, err)
	require.Len(t, notices, 3)
	assert.Equal(t, notices[0].Id, latestId)
	// 开仓后剩 9.9 资金，强平收回 10 保证金 - 50 亏损 - 50 × 0.04% = 0.02 手续费，资金变成 -30.12，从站内额度扣。
	assert.Equal(t, TradeNotice{Id: notices[0].Id, UserId: 4016, Kind: TradeNoticeCover, Market: TradeNoticeFutures, Amount: tradeUsd(30.12),
		CreatedAt: notices[0].CreatedAt}, notices[0])
	assert.Equal(t, TradeNoticeLiquidation, notices[1].Kind)
	assert.Equal(t, TradeFuturesLong, notices[1].Side)
	assert.Equal(t, "50", notices[1].Price)
	assert.Equal(t, -tradeUsd(50), notices[1].Pnl)
	assert.Equal(t, TradeNoticeFill, notices[2].Kind)
	assert.Equal(t, TradeNoticeSpot, notices[2].Market)
	assert.Equal(t, order.Id, notices[2].OrderId)
	assert.Equal(t, tradeFuturesCoin, notices[2].Qty)

	newer, _, err := GetTradeNotices(4016, notices[1].Id, 10)
	require.NoError(t, err)
	require.Len(t, newer, 1)
	assert.Equal(t, TradeNoticeCover, newer[0].Kind)
}
