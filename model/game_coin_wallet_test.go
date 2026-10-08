package model

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGameCoinWalletTest(t *testing.T) *gorm.DB {
	t.Helper()
	truncateTables(t)
	return DB
}

func TestGameCoinWalletStartsAtZeroAndPreservesExistingAccounting(t *testing.T) {
	db := setupGameCoinWalletTest(t)
	require.NoError(t, db.Create(&User{Id: 6010, Username: "game-wallet"}).Error)
	account, err := GetGameCoinAccount(6010)
	require.NoError(t, err)
	assert.Equal(t, GameCoinAccount{UserId: 6010}, account)
	var count int64
	require.NoError(t, db.Model(&GameCoinAccount{}).Count(&count).Error)
	assert.Zero(t, count, "reading an empty wallet must not create funds or a wallet row")

	err = db.Transaction(func(tx *gorm.DB) error {
		locked, err := lockGameCoinAccountTx(tx, 6010)
		if err != nil {
			return err
		}
		assert.Zero(t, locked.Balance, "creating the wallet must not grant coins")
		return nil
	})
	require.NoError(t, err)
	// 模拟已有游戏钱包的数据；只操作余额，历史累计收益、兑换、提现与下注统计都须保持原值。
	existing := GameCoinAccount{UserId: 6010, Balance: 20, TotalEarned: 41, TotalConverted: 5,
		TotalWithdrawn: 3, TotalWagerNet: -13, TotalWagered: 100}
	require.NoError(t, db.Model(&GameCoinAccount{}).Where("user_id = ?", 6010).Updates(existing).Error)
	err = db.Transaction(func(tx *gorm.DB) error {
		balance, err := applyGameCoinDeltaTx(tx, 6010, 2, GameCoinLog{Type: GameCoinLogTypeTradeIn, RefId: "trade-1"})
		if err != nil {
			return err
		}
		assert.Equal(t, 22, balance)
		balance, err = applyGameCoinDeltaTx(tx, 6010, -7, GameCoinLog{Type: GameCoinLogTypeTradeOut, RefId: "trade-2"})
		if err != nil {
			return err
		}
		assert.Equal(t, 15, balance)
		return nil
	})
	require.NoError(t, err)
	account, err = GetGameCoinAccount(6010)
	require.NoError(t, err)
	existing.Balance, existing.CreatedAt, existing.UpdatedAt = 15, account.CreatedAt, account.UpdatedAt
	assert.Equal(t, existing, account)
	var logs []GameCoinLog
	require.NoError(t, db.Where("user_id = ?", 6010).Order("id ASC").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, []int{2, -7}, []int{logs[0].Amount, logs[1].Amount})
	assert.Equal(t, []int{22, 15}, []int{logs[0].Balance, logs[1].Balance})
	assert.Equal(t, []string{"trade-1", "trade-2"}, []string{logs[0].RefId, logs[1].RefId})
}

func TestGameCoinWalletRejectsInvalidChangesWithoutMutation(t *testing.T) {
	db := setupGameCoinWalletTest(t)
	tests := []struct {
		name           string
		balance, delta int
		wantErr        error
	}{
		{"zero", 10, 0, ErrGameCoinAmountInvalid},
		{"oversized credit", 10, common.MaxQuota, ErrGameCoinAmountInvalid},
		{"oversized debit", 10, -common.MaxQuota, ErrGameCoinAmountInvalid},
		{"minimum integer debit", 10, math.MinInt64, ErrGameCoinAmountInvalid},
		{"insufficient funds", 10, -11, ErrGameCoinInsufficient},
		{"balance overflow", common.MaxQuota - 2, 2, ErrGameCoinAmountInvalid},
		{"corrupt negative balance", -1, 1, ErrGameCoinAmountInvalid},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := 6020 + i
			require.NoError(t, db.Create(&User{Id: userId, Username: fmt.Sprintf("game-wallet-%d", userId), AffCode: fmt.Sprintf("g%d", userId)}).Error)
			require.NoError(t, db.Create(&GameCoinAccount{UserId: userId, Balance: tt.balance}).Error)
			err := db.Transaction(func(tx *gorm.DB) error {
				_, err := applyGameCoinDeltaTx(tx, userId, tt.delta, GameCoinLog{Type: GameCoinLogTypeTradeOut})
				return err
			})
			require.ErrorIs(t, err, tt.wantErr)
			account, err := GetGameCoinAccount(userId)
			require.NoError(t, err)
			assert.Equal(t, tt.balance, account.Balance)
			var count int64
			require.NoError(t, db.Model(&GameCoinLog{}).Where("user_id = ?", userId).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := applyGameCoinDeltaTx(tx, 9999, 1, GameCoinLog{Type: GameCoinLogTypeTradeIn})
		return err
	})
	require.ErrorIs(t, err, ErrGameCoinUserNotFound)
	var count int64
	require.NoError(t, db.Model(&GameCoinAccount{}).Where("user_id = ?", 9999).Count(&count).Error)
	assert.Zero(t, count, "a nonexistent user must not receive a wallet")
}

func TestGameCoinWalletLedgerFailureRollsBackBalance(t *testing.T) {
	db := setupGameCoinWalletTest(t)
	require.NoError(t, db.Create(&User{Id: 6030, Username: "game-wallet-rollback"}).Error)
	require.NoError(t, db.Create(&GameCoinAccount{UserId: 6030, Balance: 10}).Error)
	// 在当前测试事务的流水写入处注入故障，三个数据库都验证余额扣减与流水的原子性。
	ledgerErr := errors.New("test ledger unavailable")
	const callback = "test:game_coin_ledger_failure"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "game_coin_logs" {
			tx.AddError(ledgerErr)
		}
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove(callback)) })
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := applyGameCoinDeltaTx(tx, 6030, -4, GameCoinLog{Type: GameCoinLogTypeTradeOut})
		return err
	})
	require.ErrorIs(t, err, ledgerErr)
	account, err := GetGameCoinAccount(6030)
	require.NoError(t, err)
	assert.Equal(t, 10, account.Balance)
	var count int64
	require.NoError(t, db.Model(&GameCoinLog{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestGameCoinQuotaUsesTradeConversionWithoutOverflow(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	quota, err := GameCoinQuota(3)
	require.NoError(t, err)
	assert.Equal(t, 1_500_000, quota)
	maximumCoins := (common.MaxQuota - 1) / 500_000
	quota, err = GameCoinQuota(maximumCoins)
	require.NoError(t, err)
	assert.Equal(t, maximumCoins*500_000, quota)
	for _, coins := range []int{-1, 0, maximumCoins + 1, common.MaxQuota, math.MaxInt64} {
		_, err = GameCoinQuota(coins)
		require.ErrorIs(t, err, ErrGameCoinAmountInvalid)
	}
	for _, perUsd := range []float64{0, -1, 0.1, math.NaN(), math.Inf(1), common.MaxQuota} {
		common.QuotaPerUnit = perUsd
		_, err = GameCoinQuota(1)
		require.Error(t, err)
	}
}
