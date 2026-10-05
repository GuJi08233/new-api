package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeNoticeNotificationKindsAndChannelContent(t *testing.T) {
	tests := []struct {
		kind, notifyType, title, market, side, action, description string
		pnl                                                        int
	}{
		{model.TradeNoticeTakeProfit, dto.NotifyTypeTradeTakeProfit, "模拟盘止盈已触发", model.TradeNoticeFutures,
			model.TradeFuturesLong, model.TradeFuturesClose, "平多仓", 1000},
		{model.TradeNoticeStopLoss, dto.NotifyTypeTradeStopLoss, "模拟盘止损已触发", model.TradeNoticeFutures,
			model.TradeFuturesShort, model.TradeFuturesClose, "平空仓", -2000},
		{model.TradeNoticeLiquidation, dto.NotifyTypeTradeLiquidation, "模拟盘仓位已强平", model.TradeNoticeFutures,
			model.TradeFuturesLong, model.TradeFuturesClose, "平多仓", -3000},
		{model.TradeNoticeFill, dto.NotifyTypeTradeFill, "模拟盘挂单已成交", model.TradeNoticeSpot,
			model.TradeSideSell, "", "卖出", 0},
	}
	for _, tt := range tests {
		for _, channel := range []string{dto.NotifyTypeEmail, dto.NotifyTypeWebhook, dto.NotifyTypeBark, dto.NotifyTypeGotify} {
			t.Run(tt.kind+"/"+channel, func(t *testing.T) {
				notice := model.TradeNotice{Kind: tt.kind, Market: tt.market, Symbol: "BTCUSDT", Side: tt.side, Action: tt.action,
					Qty: 12_500_000, Price: "25000.5", Pnl: tt.pnl}
				data, err := tradeNoticeNotification(notice, channel)
				require.NoError(t, err)
				assert.Equal(t, tt.notifyType, data.Type)
				assert.Equal(t, tt.title, data.Title)
				content := data.Content
				for _, value := range data.Values {
					content = strings.Replace(content, dto.ContentValueParam, fmt.Sprint(value), 1)
				}
				assert.Contains(t, content, "BTCUSDT "+tt.description+"，数量 0.125，成交价 25000.5 USDT")
				assert.NotContains(t, content, dto.ContentValueParam)
				if tt.market == model.TradeNoticeFutures {
					assert.Contains(t, content, "已实现盈亏 "+logger.FormatQuota(tt.pnl))
				} else {
					assert.NotContains(t, content, "已实现盈亏")
				}
				if channel == dto.NotifyTypeBark || channel == dto.NotifyTypeGotify {
					assert.NotContains(t, content, "<a")
					assert.NotContains(t, content, "<br")
				} else {
					assert.Contains(t, content, "<a href='")
				}
			})
		}
	}
	_, err := tradeNoticeNotification(model.TradeNotice{Kind: model.TradeNoticeCover}, dto.NotifyTypeEmail)
	require.Error(t, err, "quota-cover events continue through their existing after-commit notifier")
}

func TestDispatchTradeNoticesRecordsFailuresWithoutRetrying(t *testing.T) {
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.TradeNotice{}).Error)
	t.Cleanup(func() { model.DB.Where("1 = 1").Delete(&model.TradeNotice{}) })
	notices := []model.TradeNotice{
		{UserId: 4350, Kind: model.TradeNoticeFill, CreatedAt: common.GetTimestamp(), NotifyState: model.TradeNoticeNotifyPending},
		{UserId: 4350, Kind: model.TradeNoticeStopLoss, CreatedAt: common.GetTimestamp(), NotifyState: model.TradeNoticeNotifyPending},
	}
	require.NoError(t, model.DB.Create(&notices).Error)
	deliveryError := errors.New("notification provider unavailable")
	var attempted []int
	send := func(notice model.TradeNotice) error {
		var stored model.TradeNotice
		require.NoError(t, model.DB.First(&stored, notice.Id).Error)
		assert.Equal(t, model.TradeNoticeNotifySending, stored.NotifyState, "claim must commit before contacting the channel")
		attempted = append(attempted, notice.Id)
		if notice.Kind == model.TradeNoticeFill {
			return deliveryError
		}
		return nil
	}
	err := dispatchTradeNotices(send)
	require.ErrorIs(t, err, deliveryError)
	assert.Contains(t, err.Error(), fmt.Sprintf("notice %d user 4350", notices[0].Id))
	assert.Equal(t, []int{notices[0].Id, notices[1].Id}, attempted, "one failed event must not suppress the following stop-loss event")
	require.NoError(t, model.DB.First(&notices[0], notices[0].Id).Error)
	require.NoError(t, model.DB.First(&notices[1], notices[1].Id).Error)
	assert.Equal(t, model.TradeNoticeNotifyFailed, notices[0].NotifyState)
	assert.Equal(t, model.TradeNoticeNotifySent, notices[1].NotifyState)
	require.NoError(t, dispatchTradeNotices(send))
	assert.Len(t, attempted, 2, "neither successes nor ambiguous failures should be resent")
}
