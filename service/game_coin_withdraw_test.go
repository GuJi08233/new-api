package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gameCoinTestRecipient = "0x8ba1f109551bD432803012645Ac136ddd64DBA72"

// withdrawTestNode 是一个只实现提现 worker 所需方法的 JSON-RPC 节点。
type withdrawTestNode struct {
	mu             sync.Mutex
	pendingNonce   uint64
	latestNonce    uint64
	latestBlock    uint64
	estimateRevert bool
	sendDown       bool // 模拟广播时传输失败：节点可能收到也可能没收到
	receipts       map[string]map[string]any
	broadcasts     []string
}

func (n *withdrawTestNode) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &call))
		n.mu.Lock()
		defer n.mu.Unlock()
		envelope := map[string]any{"jsonrpc": "2.0", "id": 1}
		switch call.Method {
		case "eth_chainId":
			envelope["result"] = "0xaa36a7"
		case "eth_getTransactionCount":
			if call.Params[1] == "pending" {
				envelope["result"] = fmt.Sprintf("0x%x", n.pendingNonce)
			} else {
				envelope["result"] = fmt.Sprintf("0x%x", n.latestNonce)
			}
		case "eth_getBlockByNumber":
			envelope["result"] = map[string]any{"baseFeePerGas": "0x3b9aca00"}
		case "eth_maxPriorityFeePerGas":
			envelope["result"] = "0x3b9aca00"
		case "eth_estimateGas":
			if n.estimateRevert {
				envelope["error"] = map[string]any{"code": 3, "message": "execution reverted: ERC20: transfer amount exceeds balance"}
			} else {
				envelope["result"] = "0xc350"
			}
		case "eth_sendRawTransaction":
			n.broadcasts = append(n.broadcasts, call.Params[0].(string))
			if n.sendDown {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			envelope["result"] = "0x00"
		case "eth_getTransactionReceipt":
			if receipt, ok := n.receipts[call.Params[0].(string)]; ok {
				envelope["result"] = receipt
			} else {
				envelope["result"] = nil
			}
		case "eth_blockNumber":
			envelope["result"] = fmt.Sprintf("0x%x", n.latestBlock)
		default:
			t.Fatalf("unexpected rpc method %s", call.Method)
		}
		body, err := common.Marshal(envelope)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func useGameCoinTestChain(t *testing.T, node *withdrawTestNode) {
	t.Helper()
	previousKey := operation_setting.GetGameHotWalletPrivateKey()
	operation_setting.SetGameHotWalletPrivateKey(gameCoinTestKey)
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{
		ChainId:       11155111,
		RpcUrl:        node.serve(t),
		TokenAddress:  "0x64bf89340562AB4fE829a481E6797B012664425B",
		TokenSymbol:   "✦",
		TokenDecimals: 0,
		Confirmations: 1,
	})
	t.Cleanup(func() {
		operation_setting.SetGameHotWalletPrivateKey(previousKey)
		operation_setting.SetGameSettingForTest(previousSetting)
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM game_coin_accounts")
		model.DB.Exec("DELETE FROM game_coin_logs")
		model.DB.Exec("DELETE FROM game_coin_withdrawals")
		model.DB.Exec("DELETE FROM game_coin_withdraw_days")
	})
}

func createTestGameCoinWithdrawal(t *testing.T, userId int, amount int) *model.GameCoinWithdrawal {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: userId, Username: fmt.Sprintf("withdraw-%d", userId), AffCode: fmt.Sprintf("withdraw-aff-%d", userId)}).Error)
	_, err := model.AdjustGameCoins(userId, 10, "seed")
	require.NoError(t, err)
	withdrawal, _, err := model.CreateGameCoinWithdrawal(userId, gameCoinTestRecipient, amount, "2026-09-24", 100)
	require.NoError(t, err)
	return withdrawal
}

func reloadGameCoinWithdrawal(t *testing.T, id int) *model.GameCoinWithdrawal {
	t.Helper()
	var withdrawal model.GameCoinWithdrawal
	require.NoError(t, model.DB.First(&withdrawal, id).Error)
	return &withdrawal
}

func gameCoinBalance(t *testing.T, userId int) int {
	t.Helper()
	account, err := model.GetGameCoinAccount(userId)
	require.NoError(t, err)
	return account.Balance
}

// 广播时传输失败不能退款，也不能再签一笔新交易：签好的交易已经落库，下一轮原样重发，
// 节点即便第一次其实收下了，链上也只会有这一笔。
func TestGameCoinWithdrawalRebroadcastsTheSameSignedTransaction(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 3, latestNonce: 3, latestBlock: 100, sendDown: true, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2101, 3)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Signed)
	assert.Zero(t, summary.Sent)
	signed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSigned, signed.Status)
	assert.Equal(t, int64(3), signed.Nonce)
	assert.Equal(t, 7, gameCoinBalance(t, 2101), "广播失败时游戏币保持扣除状态")

	node.mu.Lock()
	node.sendDown = false
	node.mu.Unlock()
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Signed, "重发的是已落库的交易，不重新签名")
	assert.Equal(t, 1, summary.Sent)
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)
	assert.Equal(t, signed.TxHash, sent.TxHash)
	require.Len(t, node.broadcasts, 2)
	assert.Equal(t, node.broadcasts[0], node.broadcasts[1])

	node.mu.Lock()
	node.receipts[sent.TxHash] = map[string]any{"status": "0x1", "blockNumber": "0x64"}
	node.mu.Unlock()
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Equal(t, model.GameCoinWithdrawalStatusConfirmed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	account, err := model.GetGameCoinAccount(2101)
	require.NoError(t, err)
	assert.Equal(t, 7, account.Balance)
	assert.Equal(t, 3, account.TotalWithdrawn)
}

// 链上回滚的交易在达到确认数后退款并释放当日额度；没达到确认数之前不下结论。
func TestGameCoinWithdrawalRefundsRevertedTransferAfterConfirmations(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2102, 4)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	operation_setting.GetGameSetting().Confirmations = 3
	node.mu.Lock()
	node.receipts[sent.TxHash] = map[string]any{"status": "0x0", "blockNumber": "0x63"}
	node.mu.Unlock()
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed, "只有 2 个确认，回滚结论还可能被重组推翻")
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)

	node.mu.Lock()
	node.latestBlock = 101
	node.mu.Unlock()
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 10, gameCoinBalance(t, 2102))
	used, err := model.GetGameCoinWithdrawDayUsed("2026-09-24")
	require.NoError(t, err)
	assert.Zero(t, used)
}

// nonce 已被推进却查不到回执时，宽限期内按兵不动，过了宽限期才判定被顶掉并退款。
func TestGameCoinWithdrawalWaitsBeforeRefundingReplacedNonce(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 5, latestNonce: 5, latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2103, 2)
	RunGameCoinWithdrawOnce(context.Background())
	require.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)

	node.mu.Lock()
	node.latestNonce = 6
	node.mu.Unlock()
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed)
	assert.Equal(t, 8, gameCoinBalance(t, 2103))

	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).
		Update("broadcast_at", common.GetTimestamp()-gameCoinNonceReplacedSeconds).Error)
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 10, gameCoinBalance(t, 2103))
}

// 预估 gas 时就回滚的转账不上链，直接退回，也不占用 nonce。
func TestGameCoinWithdrawalRefundsWhenTransferWouldRevert(t *testing.T) {
	node := &withdrawTestNode{estimateRevert: true, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2104, 6)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Zero(t, summary.Signed)
	failed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, failed.Status)
	assert.Empty(t, failed.TxHash)
	assert.Empty(t, node.broadcasts)
	assert.Equal(t, 10, gameCoinBalance(t, 2104))
}
