package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake(NewLogSource("127.0.0.1", ""), "waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

// withEthereumTokensForPaymentGuardTest pins the current token price so a test
// controls what a late payment is measured against.
func withEthereumTokensForPaymentGuardTest(t *testing.T, tokensJSON string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	original, had := common.OptionMap["EthereumSupportedTokens"]
	common.OptionMap["EthereumSupportedTokens"] = tokensJSON
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if had {
			common.OptionMap["EthereumSupportedTokens"] = original
		} else {
			delete(common.OptionMap, "EthereumSupportedTokens")
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func insertEthereumTopUpForPaymentGuardTest(t *testing.T, userID int, tradeNo string, createTime int64) {
	t.Helper()
	topUp := &TopUp{
		UserId:                userID,
		Amount:                2,
		Money:                 9.99,
		TradeNo:               tradeNo,
		PaymentMethod:         "ethereum",
		PaymentProvider:       PaymentProviderEthereum,
		ExpectedPaymentToken:  "0x0000000000000000000000000000000000000000",
		ExpectedPaymentAmount: "1000",
		Status:                common.TopUpStatusPending,
		CreateTime:            createTime,
	}
	require.NoError(t, topUp.Insert())
}

func ethereumPaymentForGuardTest(amount string) *ChainPayment {
	return &ChainPayment{
		Token:  "0x0000000000000000000000000000000000000000",
		Amount: amount,
		TxHash: "0xabc",
	}
}

func TestRechargeEthereumWithPaymentCheck_RejectsUnderpaidPayment(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 404, 0)
	insertEthereumTopUpForPaymentGuardTest(t, 404, "eth-underpaid-guard", time.Now().Unix())

	err := RechargeEthereumWithPaymentCheck(NewLogSource("127.0.0.1", ""), "eth-underpaid-guard", ethereumPaymentForGuardTest("999"))
	require.ErrorIs(t, err, ErrPaymentAmountMismatch)
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "eth-underpaid-guard"))
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 404))
}

// ExpectedPaymentAmount is frozen from a manually configured token price. Once
// the quote has lapsed, a payment that no longer covers the order at today's
// price must not settle at the stale rate, and the stranded funds must leave an
// admin-visible trail because the chain cannot refund them.
func TestRechargeEthereumWithPaymentCheck_ExpiredOrderUnderCurrentPriceIsClosed(t *testing.T) {
	truncateTables(t)
	// 2 units now cost 2 * 1 * 10^3 = 2000, above the 1000 that was quoted.
	withEthereumTokensForPaymentGuardTest(t, `[{"symbol":"ETH","address":"0x0000000000000000000000000000000000000000","decimals":3,"price":"1"}]`)

	insertUserForPaymentGuardTest(t, 606, 0)
	insertEthereumTopUpForPaymentGuardTest(t, 606, "eth-expired-guard", time.Now().Unix()-ChainOrderTTLSeconds()-1)

	err := RechargeEthereumWithPaymentCheck(NewLogSource("127.0.0.1", ""), "eth-expired-guard", ethereumPaymentForGuardTest("1000"))
	require.ErrorIs(t, err, ErrTopUpExpired)
	assert.Equal(t, common.TopUpStatusExpired, getTopUpStatusForPaymentGuardTest(t, "eth-expired-guard"))
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 606))

	var logs []Log
	require.NoError(t, DB.Where("user_id = ? AND type = ?", 606, LogTypeTopup).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Content, "需人工核对")
	assert.Contains(t, logs[0].Content, "0xabc")
}

// A payment whose block confirmation merely crossed the deadline is still
// honoured when it covers the order at the current price: there is no repricing
// arbitrage to prevent, and the money has already left the payer's wallet.
func TestRechargeEthereumWithPaymentCheck_LatePaymentCoveringCurrentPriceIsCredited(t *testing.T) {
	truncateTables(t)
	// 2 units now cost 2 * 0.5 * 10^3 = 1000, exactly what was paid.
	withEthereumTokensForPaymentGuardTest(t, `[{"symbol":"ETH","address":"0x0000000000000000000000000000000000000000","decimals":3,"price":"0.5"}]`)

	insertUserForPaymentGuardTest(t, 608, 0)
	insertEthereumTopUpForPaymentGuardTest(t, 608, "eth-late-guard", time.Now().Unix()-ChainOrderTTLSeconds()-1)

	err := RechargeEthereumWithPaymentCheck(NewLogSource("127.0.0.1", ""), "eth-late-guard", ethereumPaymentForGuardTest("1000"))
	require.NoError(t, err)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "eth-late-guard"))
	assert.Equal(t, 2*int(common.QuotaPerUnit), getUserQuotaForPaymentGuardTest(t, 608))
}

func TestRechargeEthereumWithPaymentCheck_CreditsPaidOrderWithinTTL(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 707, 0)
	insertEthereumTopUpForPaymentGuardTest(t, 707, "eth-fresh-guard", time.Now().Unix())

	err := RechargeEthereumWithPaymentCheck(NewLogSource("127.0.0.1", ""), "eth-fresh-guard", ethereumPaymentForGuardTest("1000"))
	require.NoError(t, err)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "eth-fresh-guard"))
	assert.Equal(t, 2*int(common.QuotaPerUnit), getUserQuotaForPaymentGuardTest(t, 707))
}

// A missing order is a permanent outcome; a callback redelivery cannot fix it,
// so it must not be reported as retryable.
func TestRechargeEthereumWithPaymentCheck_UnknownOrderIsNotRetryable(t *testing.T) {
	truncateTables(t)

	err := RechargeEthereumWithPaymentCheck(NewLogSource("127.0.0.1", ""), "eth-missing-guard", ethereumPaymentForGuardTest("1000"))
	require.ErrorIs(t, err, ErrTopUpNotFound)
	assert.NotErrorIs(t, err, ErrPaymentSettlementRetryable)
}

func TestCompleteSubscriptionOrderWithPaymentCheck_RejectsUnderpaidPayment(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 505, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 505)
	insertSubscriptionOrderForPaymentGuardTest(t, "eth-sub-underpaid-guard", 505, plan.Id, PaymentProviderEthereum)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", "eth-sub-underpaid-guard").
		Updates(map[string]interface{}{
			"expected_payment_token":  "0x0000000000000000000000000000000000000000",
			"expected_payment_amount": "1000",
		}).Error)

	err := CompleteSubscriptionOrderWithPaymentCheck(
		LogSource{},
		"eth-sub-underpaid-guard",
		"",
		PaymentProviderEthereum,
		"ethereum",
		ethereumPaymentForGuardTest("999"),
	)
	require.ErrorIs(t, err, ErrPaymentAmountMismatch)

	order := GetSubscriptionOrderByTradeNo("eth-sub-underpaid-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 505))
}

// A hosted-checkout hold of a few minutes is too short for a wallet signature,
// block inclusion and webhook delivery, so an on-chain subscription order keeps
// the chain quote window instead of the generic pending hold.
func TestCompleteSubscriptionOrderWithPaymentCheck_EthereumOrderUsesChainQuoteWindow(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 515, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 515)
	insertSubscriptionOrderForPaymentGuardTest(t, "eth-sub-window-guard", 515, plan.Id, PaymentProviderEthereum)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("trade_no = ?", "eth-sub-window-guard").
		Updates(map[string]interface{}{
			"expected_payment_token":  "0x0000000000000000000000000000000000000000",
			"expected_payment_amount": "1000",
			"create_time":             time.Now().Unix() - subscriptionPendingHoldSeconds() - 60,
		}).Error)

	err := CompleteSubscriptionOrderWithPaymentCheck(
		LogSource{},
		"eth-sub-window-guard",
		"",
		PaymentProviderEthereum,
		"ethereum",
		ethereumPaymentForGuardTest("1000"),
	)
	require.NoError(t, err)

	order := GetSubscriptionOrderByTradeNo("eth-sub-window-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	assert.EqualValues(t, 1, countUserSubscriptionsForPaymentGuardTest(t, 515))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder(LogSource{}, "sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}
