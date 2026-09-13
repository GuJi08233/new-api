package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePaymentReceivedData(t *testing.T) {
	tokenWord := strings.Repeat("0", 24) + "1111111111111111111111111111111111111111"
	amountWord := strings.Repeat("0", 63) + "a"

	token, amount, err := parsePaymentReceivedData("0x" + tokenWord + amountWord)
	require.NoError(t, err)
	require.Equal(t, "0x1111111111111111111111111111111111111111", token)
	require.Equal(t, "10", amount)
}

func TestCalcPayAmountDecimalKeepsFractionalSubscriptionPrice(t *testing.T) {
	amount, err := calcPayAmountDecimal(decimal.NewFromFloat(9.99), "1", 6)
	require.NoError(t, err)
	require.Equal(t, "9990000", amount)
}

// A contract address does not identify a chain, so a payment event is only
// trustworthy when its network matches the configured one.
func TestIsConfiguredChainNetwork(t *testing.T) {
	original := setting.EthereumChainId
	t.Cleanup(func() { setting.EthereumChainId = original })
	setting.EthereumChainId = 11155111 // Sepolia

	testCases := []struct {
		name    string
		network string
		want    bool
	}{
		{name: "configured network", network: "ETH_SEPOLIA", want: true},
		{name: "configured network lowercase", network: "eth_sepolia", want: true},
		{name: "mainnet event on testnet config", network: "ETH_MAINNET", want: false},
		{name: "same contract address on another chain", network: "BASE_SEPOLIA", want: false},
		{name: "unrecognised network fails closed", network: "SOME_FUTURE_CHAIN", want: false},
		{name: "absent network stays permissive", network: "", want: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isConfiguredChainNetwork(tc.network))
		})
	}
}
