package model

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedGameCoinUser(t *testing.T, id int, quota int, coins int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: id, Username: "game-coin-user-" + strconv.Itoa(id), AffCode: "game-aff-" + strconv.Itoa(id), Quota: quota}).Error)
	if coins > 0 {
		_, err := AdjustGameCoins(id, coins, "seed")
		require.NoError(t, err)
	}
}

// createGameCoinTestWithdrawal 给用户申请一笔提现，收款地址与代币合约固定，代币所在的链由 chainId 给出。
func createGameCoinTestWithdrawal(userId int, amount int, day string, dailyLimit int, chainId int64) (*GameCoinWithdrawal, int, error) {
	withdrawal := &GameCoinWithdrawal{
		UserId:       userId,
		ToAddress:    "0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		Amount:       amount,
		Day:          day,
		ChainId:      chainId,
		TokenAddress: "0x64bf89340562AB4fE829a481E6797B012664425B",
	}
	balance, err := CreateGameCoinWithdrawal(withdrawal, dailyLimit)
	return withdrawal, balance, err
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
	used, err := GetGameCoinConvertDayUsed(1101, time.Now().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 2, used, "余额不足的兑换不占用日额度")
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
	used, err := GetGameCoinConvertDayUsed(1102, time.Now().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Zero(t, used, "主钱包入账失败必须退回已占用的日额度")
}

func TestConvertGameCoinsDailyLimitIsPerUserAndDay(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1110, 0, 40)
	seedGameCoinUser(t, 1111, 0, 40)
	now := time.Now()
	day := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	require.NoError(t, DB.Create(&GameCoinConvertDay{UserId: 1110, Day: yesterday, Used: 15}).Error)

	_, _, err := ConvertGameCoinsToQuota(1110, 16)
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
	_, _, err = ConvertGameCoinsToQuota(1110, 10)
	require.NoError(t, err, "前一天用完额度不影响今天")
	_, balance, err := ConvertGameCoinsToQuota(1110, 5)
	require.NoError(t, err)
	assert.Equal(t, 25, balance)
	_, _, err = ConvertGameCoinsToQuota(1110, 1)
	require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)

	_, balance, err = ConvertGameCoinsToQuota(1111, 15)
	require.NoError(t, err, "一个用户用完额度不影响其他用户")
	assert.Equal(t, 25, balance)
	for _, userId := range []int{1110, 1111} {
		used, err := GetGameCoinConvertDayUsed(userId, day)
		require.NoError(t, err)
		assert.Equal(t, 15, used)
		account, err := GetGameCoinAccount(userId)
		require.NoError(t, err)
		assert.Equal(t, 25, account.Balance)
		assert.Equal(t, 15, account.TotalConverted)
		quota, err := GetUserQuota(userId, true)
		require.NoError(t, err)
		assert.Equal(t, 15*int(common.QuotaPerUnit), quota)
	}
	used, err := GetGameCoinConvertDayUsed(1110, yesterday)
	require.NoError(t, err)
	assert.Equal(t, 15, used)
}

func TestConvertGameCoinsConcurrentRequestsCannotExceedDailyLimit(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1112, 0, 40)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, _, err := ConvertGameCoinsToQuota(1112, 10)
			results <- err
		}()
	}
	close(start)
	errors := []error{<-results, <-results}
	successes, rejected := 0, 0
	for _, err := range errors {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
			rejected++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, rejected)
	used, err := GetGameCoinConvertDayUsed(1112, time.Now().Format("2006-01-02"))
	require.NoError(t, err)
	assert.Equal(t, 10, used)
	account, err := GetGameCoinAccount(1112)
	require.NoError(t, err)
	assert.Equal(t, 30, account.Balance)
	assert.Equal(t, 10, account.TotalConverted)
	quota, err := GetUserQuota(1112, true)
	require.NoError(t, err)
	assert.Equal(t, 10*int(common.QuotaPerUnit), quota)
}

func TestConvertGameCoinsCountsExistingDailyLogs(t *testing.T) {
	for _, alreadyConverted := range []int{12, 20} {
		t.Run(strconv.Itoa(alreadyConverted), func(t *testing.T) {
			truncateTables(t)
			seedGameCoinUser(t, 1113, alreadyConverted*int(common.QuotaPerUnit), 30)
			now := time.Now()
			day := now.Format("2006-01-02")
			start, err := time.ParseInLocation("2006-01-02", day, time.Local)
			require.NoError(t, err)
			require.NoError(t, DB.Create(&[]GameCoinLog{
				{UserId: 1113, Type: GameCoinLogTypeConvert, Amount: -15, CreatedAt: start.Unix() - 1},
				{UserId: 1113, Type: GameCoinLogTypeConvert, Amount: -alreadyConverted, CreatedAt: start.Unix()},
				{UserId: 1113, Type: GameCoinLogTypeWithdraw, Amount: -100, CreatedAt: now.Unix()},
				{UserId: 9999, Type: GameCoinLogTypeConvert, Amount: -15, CreatedAt: now.Unix()},
			}).Error)

			used, err := GetGameCoinConvertDayUsed(1113, day)
			require.NoError(t, err)
			assert.Equal(t, alreadyConverted, used, "只计入当前用户在服务器本地日内的兑换")
			_, _, err = ConvertGameCoinsToQuota(1113, 4)
			require.ErrorIs(t, err, ErrGameCoinConvertDailyLimit)
			if alreadyConverted == 12 {
				_, _, err = ConvertGameCoinsToQuota(1113, 3)
				require.NoError(t, err)
				used, err = GetGameCoinConvertDayUsed(1113, day)
				require.NoError(t, err)
				assert.Equal(t, 15, used)
			}
		})
	}
}

func TestGameCoinConversionAndWithdrawalDailyLimitsAreIndependent(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1114, 0, 40)
	day := time.Now().Format("2006-01-02")
	withdrawal, _, err := createGameCoinTestWithdrawal(1114, 5, day, 5, 11155111)
	require.NoError(t, err)
	_, _, err = ConvertGameCoinsToQuota(1114, 15)
	require.NoError(t, err, "全站提现池用完仍然可以使用个人兑换额度")

	used, err := GetGameCoinWithdrawDayUsed(day)
	require.NoError(t, err)
	assert.Equal(t, 5, used, "兑换不占全站提现池")
	failed, err := FailGameCoinWithdrawal(withdrawal, "test refund", "")
	require.NoError(t, err)
	require.True(t, failed)
	used, err = GetGameCoinConvertDayUsed(1114, day)
	require.NoError(t, err)
	assert.Equal(t, 15, used, "提现退款不释放个人兑换额度")
	_, _, err = createGameCoinTestWithdrawal(1114, 5, day, 5, 11155111)
	require.NoError(t, err, "个人兑换额度用完仍然可以提现")
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

	first, balance, err := createGameCoinTestWithdrawal(1104, 3, day, 5, 11155111)
	require.NoError(t, err)
	assert.Equal(t, 7, balance)
	assert.Equal(t, GameCoinWithdrawalStatusPending, first.Status)

	_, _, err = createGameCoinTestWithdrawal(1104, 3, day, 5, 11155111)
	require.ErrorIs(t, err, ErrGameCoinDailyLimit)
	account, err := GetGameCoinAccount(1104)
	require.NoError(t, err)
	assert.Equal(t, 7, account.Balance, "被日限额拒绝的提现不得扣币")

	used, err := GetGameCoinWithdrawDayUsed(day)
	require.NoError(t, err)
	assert.Equal(t, 3, used)

	failed, err := FailGameCoinWithdrawal(first, "test refund", "detail")
	require.NoError(t, err)
	assert.True(t, failed)
	failed, err = FailGameCoinWithdrawal(first, "test refund", "detail")
	require.NoError(t, err)
	assert.False(t, failed, "重复退款必须是空操作")
	confirmed, err := ConfirmGameCoinWithdrawal(first, first.TxHash)
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
	_, _, err = createGameCoinTestWithdrawal(1104, 5, day, 5, 11155111)
	require.NoError(t, err)
}

// 退款依据的是调用方读到的那一刻：同时在跑的另一个 worker 已经把提现签出、广播或加价重签，拿着旧快照的判定作废，
// 正在上链的提现不会被退掉。拿着当前状态的判定照常退款。
func TestFailGameCoinWithdrawalIgnoresStaleCopies(t *testing.T) {
	truncateTables(t)
	seedGameCoinUser(t, 1115, 0, 10)
	withdrawal, _, err := createGameCoinTestWithdrawal(1115, 4, "2026-09-24", 10, 11155111)
	require.NoError(t, err)
	stalePending := *withdrawal

	signed := GameCoinSignedTx{
		FromAddress:       "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23",
		Nonce:             3,
		TxHash:            "0x" + strings.Repeat("a", 64),
		RawTx:             "0x02aa",
		GasLimit:          60000,
		MaxFeePerGas:      "4000000000",
		PriorityFeePerGas: "1000000000",
	}
	marked, err := MarkGameCoinWithdrawalSigned(withdrawal.Id, signed)
	require.NoError(t, err)
	require.True(t, marked)
	marked, err = MarkGameCoinWithdrawalSent(withdrawal.Id)
	require.NoError(t, err)
	require.True(t, marked)

	failed, err := FailGameCoinWithdrawal(&stalePending, "stale pending", "")
	require.NoError(t, err)
	assert.False(t, failed, "读到时还没签名，它已经广播了")

	var sent GameCoinWithdrawal
	require.NoError(t, DB.First(&sent, withdrawal.Id).Error)
	replacement := signed
	replacement.TxHash = "0x" + strings.Repeat("b", 64)
	replacement.RawTx = "0x02bb"
	replacement.MaxFeePerGas = "31125000000"
	replaced, err := ReplaceGameCoinWithdrawalTx(&sent, replacement)
	require.NoError(t, err)
	require.True(t, replaced)
	replaced, err = ReplaceGameCoinWithdrawalTx(&sent, replacement)
	require.NoError(t, err)
	assert.False(t, replaced, "读到的交易已经被换掉，不能再换一次")

	failed, err = FailGameCoinWithdrawal(&sent, "stale sent", "")
	require.NoError(t, err)
	assert.False(t, failed, "读到的是被替换掉的旧交易")
	account, err := GetGameCoinAccount(1115)
	require.NoError(t, err)
	assert.Equal(t, 6, account.Balance, "没有退款")

	var current GameCoinWithdrawal
	require.NoError(t, DB.First(&current, withdrawal.Id).Error)
	assert.Equal(t, GameCoinWithdrawalStatusSigned, current.Status)
	assert.Equal(t, signed.Nonce, current.Nonce)
	hashes, err := current.TxHashes()
	require.NoError(t, err)
	assert.Equal(t, []string{replacement.TxHash, signed.TxHash}, hashes)

	failed, err = FailGameCoinWithdrawal(&current, "refund", "")
	require.NoError(t, err)
	assert.True(t, failed)
	account, err = GetGameCoinAccount(1115)
	require.NoError(t, err)
	assert.Equal(t, 10, account.Balance)
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
		withdrawal, _, err := createGameCoinTestWithdrawal(1105, 1, "2026-09-24", 100, row.chainId)
		require.NoError(t, err)
		marked, err := MarkGameCoinWithdrawalSigned(withdrawal.Id, GameCoinSignedTx{FromAddress: row.from, Nonce: row.nonce, TxHash: "0xhash", RawTx: "0xraw"})
		require.NoError(t, err)
		require.True(t, marked)
	}

	nonce, found, err := MaxGameCoinWithdrawalNonce(wallet, 11155111)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(7), nonce)
}
