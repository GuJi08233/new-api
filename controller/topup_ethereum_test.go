package controller

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The contract carries orderIds as raw bytes32, so a trade number must fit in
// 32 bytes for every user id and survive the encode/decode round trip.
func TestNewEthereumTradeNoFitsBytes32ForLargestUserId(t *testing.T) {
	// Same prefix/random-length pairs as RequestEthereumPay and RequestEthereumSubscriptionPay.
	for prefix, randomLen := range map[string]int{"ETH-": 6, "ETHSUB-": 4} {
		t.Run(prefix, func(t *testing.T) {
			tradeNo, err := newEthereumTradeNo(prefix, 2147483647, randomLen)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(tradeNo), ethereumOrderIdBytes)
			assert.Equal(t, tradeNo, orderIdToTradeNo(tradeNoToOrderId(tradeNo)))
		})
	}
}

// Only outcomes a redelivery might change may fail the webhook; everything the
// chain or the ledger has already decided must be acknowledged so Alchemy stops
// retrying it.
func TestIsRetryablePaymentSettlementError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "settled", err: nil, want: false},
		{name: "amount mismatch", err: model.ErrPaymentAmountMismatch, want: false},
		{name: "order expired", err: model.ErrTopUpExpired, want: false},
		{name: "subscription order missing", err: model.ErrSubscriptionOrderNotFound, want: false},
		{name: "receipt contradicts event", err: fmt.Errorf("%w: reverted", service.ErrEthereumPaymentInvalid), want: false},
		{name: "receipt not indexed yet", err: fmt.Errorf("%w: no receipt", service.ErrEthereumPaymentUnconfirmed), want: true},
		{name: "database outage", err: fmt.Errorf("%w: %v", model.ErrPaymentSettlementRetryable, errors.New("db down")), want: true},
		{name: "unknown failure", err: errors.New("boom"), want: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isRetryablePaymentSettlementError(tc.err))
		})
	}
}

// The relay exists for this site's checkout only: once a project is configured,
// a handshake for any other WalletConnect project must be refused.
func TestBuildWalletConnectUpstreamURLPinsConfiguredProject(t *testing.T) {
	original := setting.EthereumWalletConnectProjectID
	t.Cleanup(func() { setting.EthereumWalletConnectProjectID = original })

	setting.EthereumWalletConnectProjectID = ""
	upstream, err := buildWalletConnectUpstreamURL("projectId=anything&auth=tok")
	require.NoError(t, err)
	assert.Equal(t, walletConnectOfficialRelayURL+"?projectId=anything&auth=tok", upstream)

	setting.EthereumWalletConnectProjectID = "site-project"
	_, err = buildWalletConnectUpstreamURL("projectId=anything&auth=tok")
	require.ErrorIs(t, err, errWalletConnectProjectMismatch)

	upstream, err = buildWalletConnectUpstreamURL("projectId=site-project&auth=tok")
	require.NoError(t, err)
	assert.Equal(t, walletConnectOfficialRelayURL+"?projectId=site-project&auth=tok", upstream)
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
