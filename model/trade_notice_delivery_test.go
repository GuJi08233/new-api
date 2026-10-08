package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTradeNoticeDeliveryRequiresCommittedEventAndSkipsHistory(t *testing.T) {
	truncateTables(t)
	aborted := errors.New("aborted fill")
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := addTradeNoticeTx(tx, TradeNotice{UserId: 4030, Kind: TradeNoticeFill}); err != nil {
			return err
		}
		return aborted
	})
	require.ErrorIs(t, err, aborted)
	claimed, err := ClaimTradeNoticeNotifications(common.GetTimestamp()-60, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed, "a rolled-back fill must never send a notification")

	history := TradeNotice{UserId: 4030, Kind: TradeNoticeFill, CreatedAt: common.GetTimestamp()}
	require.NoError(t, DB.Create(&history).Error)
	expired := TradeNotice{UserId: 4030, Kind: TradeNoticeFill, CreatedAt: common.GetTimestamp() - 3600, NotifyState: TradeNoticeNotifyPending}
	require.NoError(t, DB.Create(&expired).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		for _, kind := range []string{TradeNoticeTakeProfit, TradeNoticeStopLoss, TradeNoticeLiquidation, TradeNoticeFill, TradeNoticeCover} {
			if err := addTradeNoticeTx(tx, TradeNotice{UserId: 4030, Kind: kind}); err != nil {
				return err
			}
		}
		return nil
	}))

	claimed, err = ClaimTradeNoticeNotifications(common.GetTimestamp()-60, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 4)
	kinds := make([]string, 0, len(claimed))
	for _, notice := range claimed {
		kinds = append(kinds, notice.Kind)
	}
	assert.ElementsMatch(t, []string{TradeNoticeTakeProfit, TradeNoticeStopLoss, TradeNoticeLiquidation, TradeNoticeFill}, kinds)
	require.NoError(t, DB.First(&expired, expired.Id).Error)
	assert.Equal(t, TradeNoticeNotifyExpired, expired.NotifyState)
	require.NoError(t, DB.First(&history, history.Id).Error)
	assert.Empty(t, history.NotifyState, "upgrading must not enqueue old in-app notices")

	claimedAgain, err := ClaimTradeNoticeNotifications(common.GetTimestamp()-60, 10)
	require.NoError(t, err)
	assert.Empty(t, claimedAgain, "another poll or node must not claim the same events again")
	require.NoError(t, FinishTradeNoticeNotification(claimed[0].Id, true))
	require.NoError(t, FinishTradeNoticeNotification(claimed[1].Id, false))
	require.NoError(t, DB.First(&claimed[0], claimed[0].Id).Error)
	require.NoError(t, DB.First(&claimed[1], claimed[1].Id).Error)
	assert.Equal(t, TradeNoticeNotifySent, claimed[0].NotifyState)
	assert.Equal(t, TradeNoticeNotifyFailed, claimed[1].NotifyState)
	claimedAgain, err = ClaimTradeNoticeNotifications(common.GetTimestamp()-60, 10)
	require.NoError(t, err)
	assert.Empty(t, claimedAgain, "an ambiguous external failure must not cause repeated notifications")
}
