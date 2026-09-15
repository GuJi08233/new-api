package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaReserveRejectsCompetingSpendsAndIgnoresReplica(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 801, Username: "quota-reserve", Quota: 100}).Error)
	replica, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := replica.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldReplica, oldBatch := RO_DB, common.BatchUpdateEnabled
	RO_DB, common.BatchUpdateEnabled = replica, true
	t.Cleanup(func() { RO_DB, common.BatchUpdateEnabled = oldReplica, oldBatch })
	// 两个请求共用最初的余额，恰有一个条件更新成功。
	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; ok, err := TryReserveUserQuota(801, 70); results <- ok; errs <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for ok := range results {
		if ok {
			count++
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, count)
	quota, err := GetUserQuota(801, false)
	require.NoError(t, err)
	assert.Equal(t, 30, quota)
	require.NoError(t, IncreaseUserQuota(801, 20, false))
	batchUpdate()
	quota, err = GetUserQuota(801, false)
	require.NoError(t, err)
	assert.Equal(t, 50, quota)
}

func TestTokenQuotaReserveTracksUnlimitedAndRefund(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{Id: 802, UserId: 801, Key: "atomic-token", RemainQuota: 100, Status: common.TokenStatusEnabled}).Error)
	ok, err := TryReserveTokenQuota(802, 801, "atomic-token", 70)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = TryReserveTokenQuota(802, 801, "atomic-token", 70)
	require.NoError(t, err)
	assert.False(t, ok)
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", 802).Update("unlimited_quota", true).Error)
	ok, err = TryReserveTokenQuota(802, 801, "atomic-token", 50)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, IncreaseTokenQuota(802, "atomic-token", 20))
	token, err := GetTokenById(802)
	require.NoError(t, err)
	assert.Equal(t, 0, token.RemainQuota)
	assert.Equal(t, 100, token.UsedQuota)
}

func TestRechargeEpayAtomicDuplicateAndCapacityRollback(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 803, Username: "epay-atomic", Quota: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 803, TradeNo: "epay-atomic", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Amount: 1, Status: common.TopUpStatusPending}).Error)
	source := NewLogSource("127.0.0.1", "")
	require.NoError(t, RechargeEpay(source, "epay-atomic", "alipay"))
	require.NoError(t, RechargeEpay(source, "epay-atomic", "alipay"))
	quota, err := GetUserQuota(803, true)
	require.NoError(t, err)
	assert.Equal(t, 500100, quota)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 803).Update("quota", common.MaxQuota-10).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 803, TradeNo: "epay-overflow", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Amount: 1, Status: common.TopUpStatusPending}).Error)
	require.ErrorIs(t, RechargeEpay(source, "epay-overflow", "alipay"), ErrQuotaOutOfRange)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo("epay-overflow").Status)
	quota, err = GetUserQuota(803, true)
	require.NoError(t, err)
	assert.Equal(t, common.MaxQuota-10, quota)
}

func TestManualCreemCreditsStoredQuotaUnits(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 804, Username: "creem-units", Quota: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 804, TradeNo: "creem-units", PaymentProvider: PaymentProviderCreem, PaymentMethod: PaymentMethodCreem, Amount: 700, Status: common.TopUpStatusPending}).Error)
	require.NoError(t, ManualCompleteTopUp(NewLogSource("127.0.0.1", ""), "creem-units"))
	quota, err := GetUserQuota(804, true)
	require.NoError(t, err)
	assert.Equal(t, 800, quota)
}

func TestTokenReserveUsesCurrentCredentialState(t *testing.T) {
	truncateTables(t)
	token := &Token{Id: 805, UserId: 1, Key: "current-key", RemainQuota: 20, UnlimitedQuota: false, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, DB.Create(token).Error)
	ok, err := TryReserveTokenQuota(token.Id, token.UserId, token.Key, 30)
	require.NoError(t, err)
	assert.False(t, ok, "stale unlimited flag must not bypass current limit")
	ok, err = TryReserveTokenQuota(token.Id, token.UserId, "old-key", 10)
	require.NoError(t, err)
	assert.False(t, ok, "rotated credential must not reserve quota")
	require.NoError(t, DB.Model(token).Update("status", common.TokenStatusDisabled).Error)
	ok, err = TryReserveTokenQuota(token.Id, token.UserId, token.Key, 10)
	require.NoError(t, err)
	assert.False(t, ok, "disabled token cannot reserve")
	require.NoError(t, DB.Model(token).Updates(map[string]interface{}{"status": common.TokenStatusEnabled, "expired_time": common.GetTimestamp() - 1}).Error)
	ok, err = TryReserveTokenQuota(token.Id, token.UserId, token.Key, 10)
	require.NoError(t, err)
	assert.False(t, ok, "expired token cannot reserve")
	current, err := GetTokenById(token.Id)
	require.NoError(t, err)
	assert.Equal(t, 20, current.RemainQuota)
	assert.Zero(t, current.UsedQuota)
}

func TestTokenConfigurationCannotRestoreSpentQuota(t *testing.T) {
	truncateTables(t)
	token := &Token{Id: 807, UserId: 1, Key: "configuration-key", Name: "old", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, ModelLimitsEnabled: true, ModelLimits: "model"}
	require.NoError(t, DB.Create(token).Error)
	stale := *token
	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 60))
	stale.Status = common.TokenStatusDisabled
	require.NoError(t, stale.UpdateConfiguration(true, nil, 100))
	current, err := GetTokenById(token.Id)
	require.NoError(t, err)
	assert.Equal(t, 40, current.RemainQuota)
	require.NoError(t, DisableModelLimits(token.Id))
	current, err = GetTokenById(token.Id)
	require.NoError(t, err)
	assert.Equal(t, 40, current.RemainQuota)
	assert.False(t, current.ModelLimitsEnabled)
	stale.Name = "changed"
	requested := 200
	require.Error(t, stale.UpdateConfiguration(false, &requested, 100))
	current, err = GetTokenById(token.Id)
	require.NoError(t, err)
	assert.Equal(t, 40, current.RemainQuota)
	assert.Equal(t, "old", current.Name)
}

func TestTaskRefundRollsBackAndRetriesAtomically(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 808, Username: "refund-atomic", Quota: 40, UsedQuota: 60, RequestCount: 1}).Error)
	require.NoError(t, DB.Create(&Token{Id: 808, UserId: 808, Key: "refund-atomic", RemainQuota: common.MaxQuota - 10, UsedQuota: 60}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 808, UsedQuota: 60}).Error)
	task := &Task{TaskID: "refund-atomic", UserId: 808, ChannelId: 808, Quota: 60, RefundPending: true, Status: TaskStatusFailure, Progress: "100%", PrivateData: TaskPrivateData{TokenId: 808}}
	require.NoError(t, DB.Create(task).Error)
	_, _, err := RefundTaskBilling(task.ID)
	require.ErrorIs(t, err, ErrQuotaOutOfRange)
	userQuota, err := GetUserQuota(808, true)
	require.NoError(t, err)
	assert.Equal(t, 40, userQuota)
	var pending Task
	require.NoError(t, DB.First(&pending, task.ID).Error)
	assert.True(t, pending.RefundPending)
	assert.Equal(t, 60, pending.Quota)
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", 808).Update("remain_quota", 40).Error)
	_, quota, err := RefundTaskBilling(task.ID)
	require.NoError(t, err)
	assert.Equal(t, 60, quota)
	_, quota, err = RefundTaskBilling(task.ID)
	require.NoError(t, err)
	assert.Zero(t, quota)
	var user User
	require.NoError(t, DB.First(&user, 808).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var token Token
	require.NoError(t, DB.First(&token, 808).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	require.NoError(t, DB.First(&pending, task.ID).Error)
	assert.False(t, pending.RefundPending)
	assert.Zero(t, pending.Quota)
}
