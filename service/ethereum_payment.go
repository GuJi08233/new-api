package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// EthereumPaymentReceivedTopic is keccak256("PaymentReceived(bytes32,address,address,uint256)"),
// the topic[0] of the NewApiPayment contract's payment event.
const EthereumPaymentReceivedTopic = "0x1c517e85acdede9b6dbdaab4925d20d3551f2961e9a860e72658e1769f150322"

var (
	// ErrEthereumPaymentUnconfirmed means the chain could not (yet) confirm the
	// payment: the node has no receipt, too few blocks follow it, or the RPC call
	// failed. Redelivering the webhook later may succeed.
	ErrEthereumPaymentUnconfirmed = errors.New("ethereum payment not confirmed on chain")
	// ErrEthereumPaymentInvalid means the chain contradicts the webhook: the
	// transaction reverted or carries no matching PaymentReceived log. Retrying
	// cannot change that.
	ErrEthereumPaymentInvalid = errors.New("ethereum payment receipt does not match the event")
)

// EthereumPaymentProof is the PaymentReceived event as reported by the webhook,
// to be checked against the transaction receipt.
type EthereumPaymentProof struct {
	TxHash   string
	Contract string
	OrderId  string // topics[1], 0x-prefixed bytes32
	Token    string // 0x-prefixed lowercase address
	Amount   string // decimal string in the token's smallest unit
}

// ParsePaymentReceivedData decodes the non-indexed PaymentReceived fields
// (token address, amount) from a log's ABI-encoded data word pair.
func ParsePaymentReceivedData(data string) (token string, amount string, err error) {
	clean := strings.TrimPrefix(strings.TrimSpace(data), "0x")
	if len(clean) < 128 {
		return "", "", fmt.Errorf("invalid event data length: %d", len(clean))
	}
	tokenBytes, err := hex.DecodeString(clean[:64])
	if err != nil || len(tokenBytes) != 32 {
		return "", "", fmt.Errorf("invalid token word")
	}
	amountBytes, err := hex.DecodeString(clean[64:128])
	if err != nil || len(amountBytes) != 32 {
		return "", "", fmt.Errorf("invalid amount word")
	}
	token = "0x" + strings.ToLower(hex.EncodeToString(tokenBytes[12:]))
	amountInt := new(big.Int).SetBytes(amountBytes)
	if amountInt.Sign() <= 0 {
		return "", "", fmt.Errorf("invalid paid amount")
	}
	return token, amountInt.String(), nil
}

type ethereumRpcLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	Removed bool     `json:"removed"`
}

type ethereumRpcReceipt struct {
	Status      string           `json:"status"`
	BlockNumber string           `json:"blockNumber"`
	Logs        []ethereumRpcLog `json:"logs"`
}

// VerifyEthereumPaymentReceipt re-reads a payment from a JSON-RPC node the
// operator trusts and requires the PaymentReceived log to really be there.
//
// The webhook payload only routes the settlement; it cannot prove the money
// moved. Its signature proves Alchemy sent it, not that the block is canonical,
// and the same contract address can exist on several chains because CREATE
// derives it from the deployer and nonce alone. Asking the configured chain for
// the receipt closes both gaps, and the confirmation depth keeps a payment from
// settling inside a block that may still be reorganised away.
func VerifyEthereumPaymentReceipt(ctx context.Context, rpcURL string, chainId int64, confirmations int64, proof EthereumPaymentProof) error {
	txHash := strings.ToLower(strings.TrimSpace(proof.TxHash))
	if !strings.HasPrefix(txHash, "0x") || len(txHash) != 66 {
		return fmt.Errorf("%w: malformed transaction hash %q", ErrEthereumPaymentInvalid, proof.TxHash)
	}

	var chainIdHex string
	if err := callEthereumRpc(ctx, rpcURL, "eth_chainId", nil, &chainIdHex); err != nil {
		return fmt.Errorf("%w: eth_chainId: %v", ErrEthereumPaymentUnconfirmed, err)
	}
	rpcChainId, ok := new(big.Int).SetString(strings.TrimPrefix(chainIdHex, "0x"), 16)
	if !ok || rpcChainId.Int64() != chainId {
		// A node on the wrong chain is an operator mistake, not a bad payment:
		// keep the webhook retryable so the payment settles once it is fixed.
		return fmt.Errorf("%w: rpc node reports chainId %s, configured %d", ErrEthereumPaymentUnconfirmed, chainIdHex, chainId)
	}

	var receipt *ethereumRpcReceipt
	if err := callEthereumRpc(ctx, rpcURL, "eth_getTransactionReceipt", []any{txHash}, &receipt); err != nil {
		return fmt.Errorf("%w: eth_getTransactionReceipt: %v", ErrEthereumPaymentUnconfirmed, err)
	}
	if receipt == nil {
		return fmt.Errorf("%w: no receipt for %s", ErrEthereumPaymentUnconfirmed, txHash)
	}
	if !strings.EqualFold(receipt.Status, "0x1") {
		return fmt.Errorf("%w: transaction %s reverted (status %s)", ErrEthereumPaymentInvalid, txHash, receipt.Status)
	}

	if confirmations > 0 {
		receiptBlock, ok := new(big.Int).SetString(strings.TrimPrefix(receipt.BlockNumber, "0x"), 16)
		if !ok {
			return fmt.Errorf("%w: malformed receipt blockNumber %q", ErrEthereumPaymentUnconfirmed, receipt.BlockNumber)
		}
		var latestHex string
		if err := callEthereumRpc(ctx, rpcURL, "eth_blockNumber", nil, &latestHex); err != nil {
			return fmt.Errorf("%w: eth_blockNumber: %v", ErrEthereumPaymentUnconfirmed, err)
		}
		latest, ok := new(big.Int).SetString(strings.TrimPrefix(latestHex, "0x"), 16)
		if !ok {
			return fmt.Errorf("%w: malformed blockNumber %q", ErrEthereumPaymentUnconfirmed, latestHex)
		}
		depth := new(big.Int).Sub(latest, receiptBlock)
		depth.Add(depth, big.NewInt(1))
		if depth.Cmp(big.NewInt(confirmations)) < 0 {
			return fmt.Errorf("%w: %s has %s of %d confirmations", ErrEthereumPaymentUnconfirmed, txHash, depth.String(), confirmations)
		}
	}

	for _, entry := range receipt.Logs {
		if entry.Removed || len(entry.Topics) < 2 {
			continue
		}
		if !strings.EqualFold(entry.Address, proof.Contract) ||
			!strings.EqualFold(entry.Topics[0], EthereumPaymentReceivedTopic) ||
			!strings.EqualFold(entry.Topics[1], proof.OrderId) {
			continue
		}
		token, amount, err := ParsePaymentReceivedData(entry.Data)
		if err != nil {
			continue
		}
		if strings.EqualFold(token, proof.Token) && amount == proof.Amount {
			return nil
		}
	}
	return fmt.Errorf("%w: no PaymentReceived log for order %s in %s", ErrEthereumPaymentInvalid, proof.OrderId, txHash)
}

func callEthereumRpc(ctx context.Context, rpcURL string, method string, params []any, result any) error {
	if params == nil {
		params = []any{}
	}
	body, err := common.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rpc status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		// Leave result at its zero value: a nil receipt is how the node says
		// "not mined yet", which callers must distinguish from a decode error.
		return nil
	}
	return common.Unmarshal(envelope.Result, result)
}
