package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The credited quota is amount * QuotaPerUnit on a 32-bit column, so an amount
// that cannot be credited must be refused at the request boundary instead of
// being stored, priced and only then saturated at settlement.
func TestRequestEthereumPayRejectsAmountBeyondQuotaRange(t *testing.T) {
	originalEnabled, originalContract := setting.EthereumEnabled, setting.EthereumContractAddress
	t.Cleanup(func() {
		setting.EthereumEnabled, setting.EthereumContractAddress = originalEnabled, originalContract
	})
	setting.EthereumEnabled = true
	setting.EthereumContractAddress = "0x00000000000000000000000000000000000000c0"

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/pay", RequestEthereumPay)

	body := strings.NewReader(fmt.Sprintf(`{"amount": %d, "token_address": "0x0000000000000000000000000000000000000000"}`, int64(9223372036854775807)))
	request := httptest.NewRequest(http.MethodPost, "/pay", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "充值数量超过上限")
}

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
		{name: "plan sold out", err: model.ErrSubscriptionPlanSoldOut, want: false},
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

// Whether the webhook should be retried and whether a human must look at the
// money are independent: a final refusal still leaves the payer out of pocket
// unless the chain itself says nothing was paid.
func TestIsStrandedChainPaymentError(t *testing.T) {
	assert.False(t, isStrandedChainPaymentError(nil))
	assert.False(t, isStrandedChainPaymentError(fmt.Errorf("%w: db", model.ErrPaymentSettlementRetryable)))
	assert.False(t, isStrandedChainPaymentError(fmt.Errorf("%w: reverted", service.ErrEthereumPaymentInvalid)))
	assert.True(t, isStrandedChainPaymentError(model.ErrPaymentAmountMismatch))
	assert.True(t, isStrandedChainPaymentError(model.ErrTopUpStatusInvalid))
	assert.True(t, isStrandedChainPaymentError(model.ErrSubscriptionPlanSoldOut))
}

// The relay exists for this site's checkout only: once a project is configured,
// every projectId value must be that project, and the query is re-encoded so
// the upstream cannot see a value the check did not.
func TestBuildWalletConnectUpstreamURLPinsConfiguredProject(t *testing.T) {
	original := setting.EthereumWalletConnectProjectID
	t.Cleanup(func() { setting.EthereumWalletConnectProjectID = original })

	setting.EthereumWalletConnectProjectID = ""
	upstream, err := buildWalletConnectUpstreamURL("projectId=anything&auth=tok")
	require.NoError(t, err)
	assert.Equal(t, walletConnectOfficialRelayURL+"?auth=tok&projectId=anything", upstream)

	setting.EthereumWalletConnectProjectID = "site-project"
	_, err = buildWalletConnectUpstreamURL("projectId=anything&auth=tok")
	require.ErrorIs(t, err, errWalletConnectProjectMismatch)

	_, err = buildWalletConnectUpstreamURL("projectId=site-project&projectId=attacker&auth=tok")
	require.ErrorIs(t, err, errWalletConnectProjectMismatch)

	upstream, err = buildWalletConnectUpstreamURL("projectId=site-project&auth=tok")
	require.NoError(t, err)
	assert.Equal(t, walletConnectOfficialRelayURL+"?auth=tok&projectId=site-project", upstream)
}

// The relay is authenticated by the session cookie, so only this site's pages
// may open it; behind a reverse proxy "this site" is also the forwarded host
// and the configured server address, not just whatever Host the proxy sends.
func TestIsWalletConnectOriginAllowed(t *testing.T) {
	original := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = original })
	system_setting.ServerAddress = "https://api.example.com"

	newRequest := func(host, forwardedHost, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/walletconnect/relay", nil)
		r.Host = host
		if forwardedHost != "" {
			r.Header.Set("X-Forwarded-Host", forwardedHost)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}

	assert.True(t, isWalletConnectOriginAllowed(newRequest("api.example.com", "", "https://api.example.com")))
	assert.True(t, isWalletConnectOriginAllowed(newRequest("127.0.0.1:3000", "api.example.com", "https://api.example.com")))
	assert.True(t, isWalletConnectOriginAllowed(newRequest("127.0.0.1:3000", "", "https://api.example.com")))
	assert.True(t, isWalletConnectOriginAllowed(newRequest("127.0.0.1:3000", "", "")))
	assert.False(t, isWalletConnectOriginAllowed(newRequest("api.example.com", "", "https://evil.example.net")))
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
