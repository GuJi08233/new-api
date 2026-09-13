package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
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
