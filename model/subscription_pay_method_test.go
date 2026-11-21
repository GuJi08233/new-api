package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whitelist gates every subscription payment entry point, so a plan must
// never accept a method the admin unchecked, and a plan saved before the field
// existed (empty whitelist) must keep accepting everything.
func TestSubscriptionPlanAllowsPaymentMethod(t *testing.T) {
	const ethToken = "ethereum:0x64bf89340562ab4fe829a481e6797b012664425b"

	tests := []struct {
		name            string
		whitelist       string
		allowBalancePay *bool
		method          string
		want            bool
	}{
		{"空白名单放行余额", "", nil, SubscriptionPayMethodBalance, true},
		{"空白名单放行易支付通道", "", nil, "epay", true},
		{"空白名单仍受旧开关限制", "", common.GetPointer(false), SubscriptionPayMethodBalance, false},
		{"旧开关不影响其他方式", "", common.GetPointer(false), "stripe", true},
		{"只允许余额时拒绝易支付", SubscriptionPayMethodBalance, nil, "epay", false},
		{"只允许余额时放行余额", SubscriptionPayMethodBalance, nil, SubscriptionPayMethodBalance, true},
		{"白名单与旧开关取交集", SubscriptionPayMethodBalance, common.GetPointer(false), SubscriptionPayMethodBalance, false},
		{"只允许网关时拒绝余额", "epay,stripe", nil, SubscriptionPayMethodBalance, false},
		{"只允许网关时放行网关", "epay,stripe", nil, "stripe", true},
		{"代币地址大小写不敏感", ethToken, nil, "ethereum:0x64BF89340562AB4FE829A481E6797B012664425B", true},
		{"未列出的代币被拒绝", ethToken, nil, "ethereum:0x0000000000000000000000000000000000000000", false},
		{"空白名单放行游戏币", "", nil, SubscriptionPayMethodGameCoin, true},
		{"禁止余额且空白名单时拒绝游戏币防绕过", "", common.GetPointer(false), SubscriptionPayMethodGameCoin, false},
		{"显式勾选游戏币白名单时允许专属套餐", SubscriptionPayMethodGameCoin, common.GetPointer(false), SubscriptionPayMethodGameCoin, true},
		{"空方法名一律拒绝", "", nil, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := &SubscriptionPlan{
				AllowedPaymentMethods: tc.whitelist,
				AllowBalancePay:       tc.allowBalancePay,
			}
			assert.Equal(t, tc.want, plan.AllowsPaymentMethod(tc.method))
		})
	}
}

func TestNormalizeSubscriptionPayMethods(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"去空白并小写", " Epay , STRIPE ", "epay,stripe"},
		{"去重保序", "epay,balance,EPAY", "epay,balance"},
		{"丢弃空项", ",epay,,", "epay"},
		{"空串保持为空", "  ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeSubscriptionPayMethods(tc.input))
		})
	}

	// 下单入口用它构造代币键，必须和白名单里存的形式一致
	assert.Equal(t,
		"ethereum:0x64bf89340562ab4fe829a481e6797b012664425b",
		SubscriptionEthereumPayMethod(" 0x64BF89340562AB4FE829A481E6797B012664425B "),
	)
}

func TestPurchaseSubscriptionWithGameCoins(t *testing.T) {
	truncateTables(t)
	prevGame := operation_setting.GetGameSetting()
	setting := operation_setting.DefaultGameSetting()
	setting.Enabled = true
	setting.SubscriptionEnabled = true
	setting.SubscriptionRequireUnlocked = true
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetGameSettingForTest(prevGame) })

	const userId = 6510
	seedTradeUser(t, userId, 0)
	plan := &SubscriptionPlan{
		Title:         "Game Coin Monthly",
		PriceAmount:   5,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   int64(tradeUsd(10)),
	}
	require.NoError(t, DB.Create(plan).Error)

	// 带有未解锁流水时被拒绝。
	require.NoError(t, DB.Create(&GameCoinAccount{
		UserId:      userId,
		Balance:     coin(10),
		LockedWager: coin(5),
	}).Error)
	_, err := PurchaseSubscriptionWithGameCoins(LogSource{}, userId, plan.Id)
	require.ErrorIs(t, err, ErrGameCoinLockedWager)

	// 流水解锁后扣游戏币并开通订阅。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", userId).Update("locked_wager", 0).Error)
	balance, err := PurchaseSubscriptionWithGameCoins(LogSource{}, userId, plan.Id)
	require.NoError(t, err)
	assert.Equal(t, coin(5), balance)

	var subs []UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", userId, plan.Id).Find(&subs).Error)
	require.Len(t, subs, 1)
	assert.Equal(t, SubscriptionStatusActive, subs[0].Status)

	var orders []SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ? AND payment_method = ?", userId, PaymentMethodGameCoin).Find(&orders).Error)
	require.Len(t, orders, 1)

	var logs []GameCoinLog
	require.NoError(t, DB.Where("user_id = ? AND type = ?", userId, GameCoinLogTypeSubscription).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, -coin(5), logs[0].Amount)
}
