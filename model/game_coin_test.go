package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedGameCoinUser(t *testing.T, id int, quota int, coins int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: id, Username: "game-coin-user", AffCode: "game-aff", Quota: quota}).Error)
	if coins > 0 {
		_, err := AdjustGameCoins(id, coins, "seed")
		require.NoError(t, err)
	}
}

func gameCoinLogTypes(t *testing.T, userId int) []string {
	t.Helper()
	var types []string
	require.NoError(t, DB.Model(&GameCoinLog{}).Where("user_id = ?", userId).Order("id asc").Pluck("type", &types).Error)
	return types
}

// 兑换按 1 币 = 1 美元额度入账，扣币与 quota 入账同进同退；余额不够时两边都不动。
func TestConvertGameCoinsToQuotaMovesValueAtomically(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1101, 100, 5)

	quota, balance, err := ConvertGameCoinsToQuota(1101, 2)
	require.NoError(t, err)
	assert.Equal(t, 2*int(common.QuotaPerUnit), quota)
	assert.Equal(t, 3, balance)

	userQuota, err := GetUserQuota(1101, true)
	require.NoError(t, err)
	assert.Equal(t, 100+2*int(common.QuotaPerUnit), userQuota)

	_, _, err = ConvertGameCoinsToQuota(1101, 4)
	require.ErrorIs(t, err, ErrGameCoinInsufficient)

	account, err := GetGameCoinAccount(1101)
	require.NoError(t, err)
	assert.Equal(t, 3, account.Balance)
	assert.Equal(t, 2, account.TotalConverted)
	userQuota, err = GetUserQuota(1101, true)
	require.NoError(t, err)
	assert.Equal(t, 100+2*int(common.QuotaPerUnit), userQuota)
	assert.Equal(t, []string{GameCoinLogTypeAdminAdjust, GameCoinLogTypeConvert}, gameCoinLogTypes(t, 1101))
}

// 主钱包容量不够时整笔回滚：游戏币不能被扣掉却换不到额度。
func TestConvertGameCoinsRollsBackWhenWalletIsFull(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1102, common.MaxQuota-10, 5)

	_, _, err := ConvertGameCoinsToQuota(1102, 1)
	require.ErrorIs(t, err, ErrQuotaOutOfRange)

	account, err := GetGameCoinAccount(1102)
	require.NoError(t, err)
	assert.Equal(t, 5, account.Balance)
	assert.Zero(t, account.TotalConverted)
	assert.Equal(t, []string{GameCoinLogTypeAdminAdjust}, gameCoinLogTypes(t, 1102))
	quota, err := GetUserQuota(1102, true)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota-10, quota)
}

// 管理员扣币不能透支，也不能给不存在的用户发币。
func TestAdjustGameCoinsNeverOverdraws(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1103, 0, 3)

	_, err := AdjustGameCoins(1103, -4, "too much")
	require.ErrorIs(t, err, ErrGameCoinInsufficient)
	balance, err := AdjustGameCoins(1103, -3, "all")
	require.NoError(t, err)
	assert.Zero(t, balance)

	_, err = AdjustGameCoins(9999, 5, "ghost")
	require.ErrorIs(t, err, ErrGameCoinUserNotFound)
	var accounts int64
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", 9999).Count(&accounts).Error)
	assert.Zero(t, accounts)
}

// 全站日限额在提现创建时占用、失败退回时释放；同一笔提现只能退款一次，退款后也不能再确认。
func TestGameCoinWithdrawalHoldsDailyLimitAndRefundsOnce(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1104, 0, 10)
	const day = "2026-09-24"

	first, balance, err := CreateGameCoinWithdrawal(1104, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", 3, day, 5)
	require.NoError(t, err)
	assert.Equal(t, 7, balance)
	assert.Equal(t, GameCoinWithdrawalStatusPending, first.Status)

	_, _, err = CreateGameCoinWithdrawal(1104, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", 3, day, 5)
	require.ErrorIs(t, err, ErrGameCoinDailyLimit)
	account, err := GetGameCoinAccount(1104)
	require.NoError(t, err)
	assert.Equal(t, 7, account.Balance, "被日限额拒绝的提现不得扣币")

	used, err := GetGameCoinWithdrawDayUsed(day)
	require.NoError(t, err)
	assert.Equal(t, 3, used)

	failed, err := FailGameCoinWithdrawal(first.Id, "test refund", "detail")
	require.NoError(t, err)
	assert.True(t, failed)
	failed, err = FailGameCoinWithdrawal(first.Id, "test refund", "detail")
	require.NoError(t, err)
	assert.False(t, failed, "重复退款必须是空操作")
	confirmed, err := ConfirmGameCoinWithdrawal(first.Id)
	require.NoError(t, err)
	assert.False(t, confirmed)

	account, err = GetGameCoinAccount(1104)
	require.NoError(t, err)
	assert.Equal(t, 10, account.Balance)
	assert.Zero(t, account.TotalWithdrawn)
	used, err = GetGameCoinWithdrawDayUsed(day)
	require.NoError(t, err)
	assert.Zero(t, used)
	assert.Equal(t, []string{GameCoinLogTypeAdminAdjust, GameCoinLogTypeWithdraw, GameCoinLogTypeWithdrawRefund}, gameCoinLogTypes(t, 1104))

	// 释放后的额度可以再次使用。
	_, _, err = CreateGameCoinWithdrawal(1104, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", 5, day, 5)
	require.NoError(t, err)
}

// nonce 按热钱包和链分开计算：换钱包或换网络后，旧记录不会把新钱包的 nonce 推到空洞后面。
func TestMaxGameCoinWithdrawalNonceIsScopedToWalletAndChain(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1105, 0, 10)
	const wallet = "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23"

	_, found, err := MaxGameCoinWithdrawalNonce(wallet, 11155111)
	require.NoError(t, err)
	assert.False(t, found)

	rows := []struct {
		from    string
		chainId int64
		nonce   int64
	}{
		{wallet, 11155111, 4},
		{wallet, 11155111, 7},
		{"0x0000000000000000000000000000000000000001", 11155111, 40},
		{wallet, 1, 90},
	}
	for _, row := range rows {
		withdrawal, _, err := CreateGameCoinWithdrawal(1105, "0x8ba1f109551bD432803012645Ac136ddd64DBA72", 1, "2026-09-24", 100)
		require.NoError(t, err)
		marked, err := MarkGameCoinWithdrawalSigned(withdrawal.Id, row.chainId, "0x64bf89340562AB4fE829a481E6797B012664425B", row.from, row.nonce, "0xhash", "0xraw")
		require.NoError(t, err)
		require.True(t, marked)
	}

	nonce, found, err := MaxGameCoinWithdrawalNonce(wallet, 11155111)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(7), nonce)
}
