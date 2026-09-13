package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	receiptTestContract = "0x00000000000000000000000000000000000000c0"
	receiptTestOrderId  = "0x4554482d312d3100000000000000000000000000000000000000000000000000"
	receiptTestTxHash   = "0x1111111111111111111111111111111111111111111111111111111111111111"
)

func receiptTestData(amountHex string) string {
	tokenWord := strings.Repeat("0", 24) + "1111111111111111111111111111111111111111"
	amountWord := strings.Repeat("0", 64-len(amountHex)) + amountHex
	return "0x" + tokenWord + amountWord
}

type receiptTestNode struct {
	chainId     string
	latestBlock string
	receipt     map[string]any // nil answers "not mined yet"
}

func (n receiptTestNode) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string `json:"method"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &call))
		var result any
		switch call.Method {
		case "eth_chainId":
			result = n.chainId
		case "eth_blockNumber":
			result = n.latestBlock
		case "eth_getTransactionReceipt":
			if n.receipt != nil {
				result = n.receipt
			}
		default:
			t.Fatalf("unexpected rpc method %s", call.Method)
		}
		body, err := common.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func paidReceipt(status string, amountHex string) map[string]any {
	return map[string]any{
		"status":      status,
		"blockNumber": "0x64",
		"logs": []map[string]any{{
			"address": receiptTestContract,
			"topics":  []string{EthereumPaymentReceivedTopic, receiptTestOrderId},
			"data":    receiptTestData(amountHex),
		}},
	}
}

func TestParsePaymentReceivedData(t *testing.T) {
	token, amount, err := ParsePaymentReceivedData(receiptTestData("a"))
	require.NoError(t, err)
	require.Equal(t, "0x1111111111111111111111111111111111111111", token)
	require.Equal(t, "10", amount)
}

// The receipt is the only evidence that money moved on the configured chain, so
// a payment settles only when the node's log matches the webhook's event, and
// anything the node cannot yet answer stays retryable rather than rejected.
func TestVerifyEthereumPaymentReceipt(t *testing.T) {
	proof := EthereumPaymentProof{
		TxHash:   receiptTestTxHash,
		Contract: receiptTestContract,
		OrderId:  receiptTestOrderId,
		Token:    "0x1111111111111111111111111111111111111111",
		Amount:   "10",
	}
	testCases := []struct {
		name          string
		node          receiptTestNode
		confirmations int64
		wantErr       error
	}{
		{name: "matching log settles", node: receiptTestNode{chainId: "0x1", latestBlock: "0x64", receipt: paidReceipt("0x1", "a")}},
		{name: "enough confirmations", node: receiptTestNode{chainId: "0x1", latestBlock: "0x66", receipt: paidReceipt("0x1", "a")}, confirmations: 3},
		{name: "too few confirmations is retryable", node: receiptTestNode{chainId: "0x1", latestBlock: "0x65", receipt: paidReceipt("0x1", "a")}, confirmations: 3, wantErr: ErrEthereumPaymentUnconfirmed},
		{name: "not mined yet is retryable", node: receiptTestNode{chainId: "0x1", latestBlock: "0x64"}, wantErr: ErrEthereumPaymentUnconfirmed},
		{name: "node on another chain is retryable", node: receiptTestNode{chainId: "0xaa36a7", latestBlock: "0x64", receipt: paidReceipt("0x1", "a")}, wantErr: ErrEthereumPaymentUnconfirmed},
		{name: "reverted transaction is final", node: receiptTestNode{chainId: "0x1", latestBlock: "0x64", receipt: paidReceipt("0x0", "a")}, wantErr: ErrEthereumPaymentInvalid},
		{name: "amount differs from event is final", node: receiptTestNode{chainId: "0x1", latestBlock: "0x64", receipt: paidReceipt("0x1", "9")}, wantErr: ErrEthereumPaymentInvalid},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyEthereumPaymentReceipt(context.Background(), tc.node.serve(t), 1, tc.confirmations, proof)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}
