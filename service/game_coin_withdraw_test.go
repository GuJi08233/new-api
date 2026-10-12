package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	noCode         bool   // 代币地址上没有合约代码
	sendDown       bool   // 模拟广播时传输失败：节点可能收到也可能没收到
	baseFee        uint64 // 最新区块的基础费(wei)，0 表示 1 gwei
	// known 是节点内存池里有、还没有回执的交易，eth_getTransactionByHash 查得到它们；有回执的交易也查得到。
	known map[string]bool
	// sendRejects 让这些已签名交易的广播报一个认不出来的错(例如旧热钱包没有 gas)，广播就一直没有结论。
	sendRejects map[string]bool
	receipts    map[string]map[string]any
	broadcasts  []string
}

func (n *withdrawTestNode) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		// 处理函数跑在服务端的 goroutine 里，这里不能用 require 结束测试。
		if !assert.NoError(t, common.DecodeJson(r.Body, &call)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
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
		case "eth_getCode":
			if n.noCode {
				envelope["result"] = "0x"
			} else {
				envelope["result"] = "0x6080604052"
			}
		case "eth_getBlockByNumber":
			baseFee := n.baseFee
			if baseFee == 0 {
				baseFee = 1_000_000_000
			}
			envelope["result"] = map[string]any{"baseFeePerGas": fmt.Sprintf("0x%x", baseFee)}
		case "eth_maxPriorityFeePerGas":
			envelope["result"] = "0x3b9aca00"
		case "eth_estimateGas":
			if n.estimateRevert {
				envelope["error"] = map[string]any{"code": 3, "message": "execution reverted: ERC20: transfer amount exceeds balance"}
			} else {
				envelope["result"] = "0xc350"
			}
		case "eth_sendRawTransaction":
			raw := call.Params[0].(string)
			n.broadcasts = append(n.broadcasts, raw)
			if n.sendDown {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			if n.sendRejects[raw] {
				envelope["error"] = map[string]any{"code": -32000, "message": "insufficient funds for gas * price + value"}
			} else {
				envelope["result"] = "0x00"
			}
		case "eth_getTransactionReceipt":
			if receipt, ok := n.receipts[call.Params[0].(string)]; ok {
				envelope["result"] = receipt
			} else {
				envelope["result"] = nil
			}
		case "eth_getTransactionByHash":
			hash := call.Params[0].(string)
			if _, mined := n.receipts[hash]; mined || n.known[hash] {
				envelope["result"] = map[string]any{"hash": hash}
			} else {
				envelope["result"] = nil
			}
		case "eth_blockNumber":
			envelope["result"] = fmt.Sprintf("0x%x", n.latestBlock)
		default:
			assert.Failf(t, "unexpected rpc method", "%s", call.Method)
			envelope["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		body, err := common.Marshal(envelope)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func (n *withdrawTestNode) update(change func(node *withdrawTestNode)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	change(n)
}

func useGameCoinTestChain(t *testing.T, node *withdrawTestNode) {
	t.Helper()
	previousKey := operation_setting.GetGameHotWalletPrivateKey()
	operation_setting.SetGameHotWalletPrivateKey(gameCoinTestKey)
	// worker 签名前会从数据库现读私钥，测试用的私钥也要落库。
	require.NoError(t, model.DB.Save(&model.Option{Key: operation_setting.GameHotWalletPrivateKeyOption, Value: gameCoinTestKey}).Error)
	previousSetting := operation_setting.GetGameSetting()
	// 轮流处理的游标是进程内状态，每个测试都从头开始。
	gameCoinSentCursor.Store(0)
	gameCoinParkedSignedCursor.Store(0)
	operation_setting.SetGameSettingForTest(&operation_setting.GameSetting{
		ChainId:       11155111,
		RpcUrl:        node.serve(t),
		TokenAddress:  "0x64bf89340562AB4fE829a481E6797B012664425B",
		TokenSymbol:   "✦",
		TokenDecimals: 0,
		Confirmations: 1,
	})
	t.Cleanup(func() {
		gameCoinSentCursor.Store(0)
		gameCoinParkedSignedCursor.Store(0)
		operation_setting.SetGameHotWalletPrivateKey(previousKey)
		operation_setting.SetGameSettingForTest(previousSetting)
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM options")
		model.DB.Exec("DELETE FROM game_coin_accounts")
		model.DB.Exec("DELETE FROM game_coin_logs")
		model.DB.Exec("DELETE FROM game_coin_withdrawals")
		model.DB.Exec("DELETE FROM game_coin_withdraw_days")
	})
}

// createTestGameCoinWithdrawal 给新用户发 10 个游戏币，再按当前配置的链与代币申请一笔提现。
func createTestGameCoinWithdrawal(t *testing.T, userId int, amount int) *model.GameCoinWithdrawal {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: userId, Username: fmt.Sprintf("withdraw-%d", userId), AffCode: fmt.Sprintf("withdraw-aff-%d", userId)}).Error)
	_, err := model.AdjustGameCoins(userId, 10, "seed")
	require.NoError(t, err)
	setting := operation_setting.GetGameSetting()
	withdrawal := &model.GameCoinWithdrawal{
		UserId:        userId,
		ToAddress:     gameCoinTestRecipient,
		Amount:        amount,
		Day:           "2026-09-24",
		ChainId:       setting.ChainId,
		TokenAddress:  setting.TokenAddress,
		TokenDecimals: setting.TokenDecimals,
	}
	_, err = model.CreateGameCoinWithdrawal(withdrawal, 100)
	require.NoError(t, err)
	return withdrawal
}

// createInFlightGameCoinWithdrawal 直接落一笔已签名或已广播的提现，模拟切换网络、换热钱包之前留下的单子。
func createInFlightGameCoinWithdrawal(t *testing.T, index int, status string, chainId int64, fromAddress string) *model.GameCoinWithdrawal {
	t.Helper()
	withdrawal := &model.GameCoinWithdrawal{
		TradeNo:     fmt.Sprintf("GCW-TEST-%d", index),
		UserId:      9000 + index,
		ToAddress:   gameCoinTestRecipient,
		Amount:      1,
		Day:         "2026-09-24",
		Status:      status,
		ChainId:     chainId,
		FromAddress: fromAddress,
		Nonce:       int64(index),
		TxHash:      fmt.Sprintf("0x%064x", index),
		RawTx:       fmt.Sprintf("0x02%062x", index),
		BroadcastAt: common.GetTimestamp(),
	}
	require.NoError(t, model.DB.Create(withdrawal).Error)
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

// transferReceipt 是一笔转账成功的回执：代币合约发出了从热钱包转给收款地址的 Transfer 事件。
func transferReceipt(withdrawal *model.GameCoinWithdrawal, block uint64) map[string]any {
	addressTopic := func(address string) string {
		return "0x000000000000000000000000" + strings.ToLower(strings.TrimPrefix(address, "0x"))
	}
	return map[string]any{
		"status":      "0x1",
		"blockNumber": fmt.Sprintf("0x%x", block),
		"logs": []any{map[string]any{
			"address": strings.ToLower(withdrawal.TokenAddress),
			"topics":  []any{erc20TransferTopic, addressTopic(withdrawal.FromAddress), addressTopic(withdrawal.ToAddress)},
			"data":    fmt.Sprintf("0x%064x", withdrawal.Amount),
			"removed": false,
		}},
	}
}

// 转账事件的 topics[0] 必须是 Transfer(address,address,uint256) 的哈希，写错了所有成功的提现都会被退款。
func TestErc20TransferTopicMatchesEventSignature(t *testing.T) {
	assert.Equal(t, erc20TransferTopic, fmt.Sprintf("0x%x", keccak256([]byte("Transfer(address,address,uint256)"))))
}

// 广播时传输失败不能退款，也不能再签一笔新交易：签好的交易已经落库，下一轮原样重发，
// 节点即便第一次其实收下了，链上也只会有这一笔。回执里有代币的 Transfer 事件才算到账。
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

	node.update(func(node *withdrawTestNode) { node.sendDown = false })
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Signed, "重发的是已落库的交易，不重新签名")
	assert.Equal(t, 1, summary.Sent)
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)
	assert.Equal(t, signed.TxHash, sent.TxHash)
	require.Len(t, node.broadcasts, 2)
	assert.Equal(t, node.broadcasts[0], node.broadcasts[1])

	node.update(func(node *withdrawTestNode) { node.receipts[sent.TxHash] = transferReceipt(sent, 100) })
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
	node.update(func(node *withdrawTestNode) {
		node.receipts[sent.TxHash] = map[string]any{"status": "0x0", "blockNumber": "0x63"}
	})
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed, "只有 2 个确认，回滚结论还可能被重组推翻")
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)

	node.update(func(node *withdrawTestNode) { node.latestBlock = 101 })
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 10, gameCoinBalance(t, 2102))
	used, err := model.GetGameCoinWithdrawDayUsed("2026-09-24")
	require.NoError(t, err)
	assert.Zero(t, used)
}

// 交易执行成功却没有代币合约发出的 Transfer 事件(代币地址上没有合约，或者 transfer 返回 false 而不回滚)，
// 说明代币没有转出去。交易已经上链不会再执行，退款不会重复发放。
func TestGameCoinWithdrawalRefundsSuccessWithoutTransferEvent(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2105, 4)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	node.update(func(node *withdrawTestNode) {
		node.receipts[sent.TxHash] = map[string]any{"status": "0x1", "blockNumber": "0x64", "logs": []any{}}
	})
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Zero(t, summary.Confirmed)
	failed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, failed.Status)
	assert.Equal(t, "代币合约没有完成转账，游戏币已退回", failed.FailReason)
	assert.Equal(t, 10, gameCoinBalance(t, 2105))
}

// 有的节点对还没打包的交易也给出回执，只是区块号与状态为空。这不是"第 0 个区块里执行失败"，不能退款。
func TestGameCoinWithdrawalWaitsOnIncompleteReceipt(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2106, 5)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	node.update(func(node *withdrawTestNode) {
		node.receipts[sent.TxHash] = map[string]any{"status": nil, "blockNumber": nil, "logs": []any{}}
	})
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed)
	assert.Zero(t, summary.Confirmed)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 5, gameCoinBalance(t, 2106))
}

// nonce 已被推进却查不到回执时，这种状态要连续保持一段时间才判定交易被顶掉并退款；计时从第一次发现起算，
// 不从广播时间起算，所以 worker 停了很久之后的第一次检查也不会立刻退款。
func TestGameCoinWithdrawalWaitsBeforeRefundingReplacedNonce(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 5, latestNonce: 5, latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2103, 2)
	RunGameCoinWithdrawOnce(context.Background())
	require.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).
		Update("broadcast_at", common.GetTimestamp()-2*gameCoinNonceReplacedSeconds).Error)

	node.update(func(node *withdrawTestNode) { node.latestNonce = 6 })
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed, "第一次发现只开始计时")
	assert.NotZero(t, reloadGameCoinWithdrawal(t, withdrawal.Id).NonceUsedAt)
	assert.Equal(t, 8, gameCoinBalance(t, 2103))

	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).
		Update("nonce_used_at", common.GetTimestamp()-gameCoinNonceReplacedSeconds).Error)
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, model.GameCoinWithdrawalStatusFailed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 10, gameCoinBalance(t, 2103))
}

// 已经上链、正在等确认数的转账，偶尔一次查不到回执(负载均衡打到落后的节点)时 nonce 看起来被推进了，
// 这不能算作被顶掉：再次查到回执就重新计时，最终照常到账，不会退款。
func TestGameCoinWithdrawalKeepsMinedTransferThroughAMissingReceipt(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 4, latestNonce: 4, latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	operation_setting.GetGameSetting().Confirmations = 3
	withdrawal := createTestGameCoinWithdrawal(t, 2107, 3)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)
	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).
		Update("broadcast_at", common.GetTimestamp()-2*gameCoinNonceReplacedSeconds).Error)
	mined := transferReceipt(sent, 100)

	steps := []struct {
		receipt       bool
		latestBlock   uint64
		wantNonceUsed bool
		wantStatus    string
	}{
		{receipt: true, latestBlock: 100, wantStatus: model.GameCoinWithdrawalStatusSent},
		{receipt: false, latestBlock: 100, wantNonceUsed: true, wantStatus: model.GameCoinWithdrawalStatusSent},
		{receipt: true, latestBlock: 101, wantStatus: model.GameCoinWithdrawalStatusSent},
		{receipt: false, latestBlock: 101, wantNonceUsed: true, wantStatus: model.GameCoinWithdrawalStatusSent},
		{receipt: true, latestBlock: 102, wantStatus: model.GameCoinWithdrawalStatusConfirmed},
	}
	for i, step := range steps {
		node.update(func(node *withdrawTestNode) {
			node.latestNonce = 5 // 这笔交易已经上链，nonce 已经推进
			node.latestBlock = step.latestBlock
			delete(node.receipts, sent.TxHash)
			if step.receipt {
				node.receipts[sent.TxHash] = mined
			}
		})
		summary := RunGameCoinWithdrawOnce(context.Background())
		assert.Zero(t, summary.Failed, "step %d", i)
		current := reloadGameCoinWithdrawal(t, withdrawal.Id)
		assert.Equal(t, step.wantStatus, current.Status, "step %d", i)
		assert.Equal(t, step.wantNonceUsed, current.NonceUsedAt != 0, "step %d", i)
	}
	assert.Equal(t, 7, gameCoinBalance(t, 2107), "到账了，没有退款")
}

// "连续保持"要有人一直在看：两次看到 nonce 已被推进隔了太久(RPC 一直报错、worker 停过、积压太多轮不到它)，
// 中间发生了什么不得而知。即便从第一次看到算起早已超过十分钟，也只能从头计时，不能退款。
func TestGameCoinWithdrawalRestartsReplacedNonceWatchAfterAGap(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 5, latestNonce: 5, latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2119, 2)
	RunGameCoinWithdrawOnce(context.Background())
	require.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)

	node.update(func(node *withdrawTestNode) { node.latestNonce = 6 })
	now := common.GetTimestamp()
	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).Updates(map[string]any{
		"nonce_used_at":    now - 2*gameCoinNonceReplacedSeconds,
		"nonce_checked_at": now - gameCoinNonceWatchGapSeconds - 1,
	}).Error)
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed)
	current := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, current.Status)
	assert.GreaterOrEqual(t, current.NonceUsedAt, now, "从头计时")
	assert.Equal(t, 8, gameCoinBalance(t, 2119))
}

// 退款前按哈希查交易本身再确认一次：回执查询可能一直落在进度落后的节点上，节点还知道这笔交易，就说明它没有被顶掉。
func TestGameCoinWithdrawalKeepsTransactionTheNodeStillKnows(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 5, latestNonce: 5, latestBlock: 100, known: map[string]bool{}, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2120, 2)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	node.update(func(node *withdrawTestNode) {
		node.latestNonce = 6
		node.known[sent.TxHash] = true
	})
	RunGameCoinWithdrawOnce(context.Background())
	require.NotZero(t, reloadGameCoinWithdrawal(t, withdrawal.Id).NonceUsedAt)
	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("id = ?", withdrawal.Id).
		Update("nonce_used_at", common.GetTimestamp()-gameCoinNonceReplacedSeconds).Error)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Failed)
	current := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, current.Status)
	assert.Zero(t, current.NonceUsedAt, "节点还知道这笔交易，计时作废")
	assert.Equal(t, 8, gameCoinBalance(t, 2120))
}

// 回执里没有 logs 字段(节点或代理截掉了)时看不出代币有没有转出，等回执完整了再下结论，不能当成没有转账退款。
func TestGameCoinWithdrawalWaitsForReceiptLogs(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2118, 2)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	for _, receipt := range []map[string]any{
		{"status": "0x1", "blockNumber": "0x64"},
		{"status": "0x1", "blockNumber": "0x64", "logs": nil},
	} {
		node.update(func(node *withdrawTestNode) { node.receipts[sent.TxHash] = receipt })
		summary := RunGameCoinWithdrawOnce(context.Background())
		assert.Zero(t, summary.Failed)
		assert.Zero(t, summary.Confirmed)
		assert.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	}

	node.update(func(node *withdrawTestNode) { node.receipts[sent.TxHash] = transferReceipt(sent, 100) })
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Equal(t, 8, gameCoinBalance(t, 2118))
}

// 早期有的代币 Transfer 事件的参数都不加 indexed，from、to、数量依次放在 data 里；这样的转账同样算到账。
func TestGameCoinWithdrawalConfirmsNonIndexedTransferEvent(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2117, 2)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	word := func(address string) string {
		return "000000000000000000000000" + strings.ToLower(strings.TrimPrefix(address, "0x"))
	}
	node.update(func(node *withdrawTestNode) {
		node.receipts[sent.TxHash] = map[string]any{
			"status":      "0x1",
			"blockNumber": "0x64",
			"logs": []any{map[string]any{
				"address": strings.ToLower(sent.TokenAddress),
				"topics":  []any{erc20TransferTopic},
				"data":    "0x" + word(sent.FromAddress) + word(sent.ToAddress) + fmt.Sprintf("%064x", sent.Amount),
				"removed": false,
			}},
		}
	})
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Zero(t, summary.Failed)
	assert.Equal(t, model.GameCoinWithdrawalStatusConfirmed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
}

// 基础费涨过签名时的费用上限，这笔交易就再也上不了链，这个热钱包之后的提现也全卡在它后面。worker 用同一个 nonce、
// 同样的 gas 加价重签：小费和上限都加价 12.5%，上限不低于现在的三倍基础费加小费。旧哈希留着，之后上链的是旧交易
// 也照常到账，不会退款。
func TestGameCoinWithdrawalReplacesUnderpricedTransaction(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 4, latestNonce: 4, latestBlock: 100, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2116, 3)
	RunGameCoinWithdrawOnce(context.Background())
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)
	assert.Equal(t, "4000000000", sent.MaxFeePerGas, "三倍基础费加小费")
	assert.Equal(t, "1000000000", sent.PriorityFeePerGas)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Replaced, "基础费没涨，不重签")

	node.update(func(node *withdrawTestNode) { node.baseFee = 10_000_000_000 })
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Replaced)
	replaced := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, replaced.Status)
	assert.Equal(t, sent.Nonce, replaced.Nonce)
	assert.Equal(t, sent.GasLimit, replaced.GasLimit)
	assert.NotEqual(t, sent.TxHash, replaced.TxHash)
	assert.Equal(t, "1125000000", replaced.PriorityFeePerGas, "小费加价 12.5%")
	assert.Equal(t, "31125000000", replaced.MaxFeePerGas, "上限不低于三倍基础费加小费")
	hashes, err := replaced.TxHashes()
	require.NoError(t, err)
	assert.Equal(t, []string{replaced.TxHash, sent.TxHash}, hashes)
	assert.Equal(t, replaced.RawTx, node.broadcasts[len(node.broadcasts)-1])

	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Replaced, "新交易付得起现在的基础费")

	// 旧交易赶在新交易之前上链：同一个 nonce 只会有一笔，照常到账，记下实际上链的那一笔。
	node.update(func(node *withdrawTestNode) {
		node.latestNonce = 5
		node.receipts[sent.TxHash] = transferReceipt(sent, 100)
	})
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Zero(t, summary.Failed)
	confirmed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusConfirmed, confirmed.Status)
	assert.Equal(t, sent.TxHash, confirmed.TxHash)
	assert.Equal(t, 7, gameCoinBalance(t, 2116))
}

// 节点不收一笔已签名的交易(例如基础费涨过了它的费用上限，报的错认不出来)，这个热钱包的队首就一直卡着；付不起时同样加价重签。
func TestGameCoinWithdrawalReplacesSignedTransactionTheNodeRejects(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 2, latestNonce: 2, latestBlock: 100, sendRejects: map[string]bool{}, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2121, 3)
	node.update(func(node *withdrawTestNode) { node.sendDown = true })
	RunGameCoinWithdrawOnce(context.Background())
	signed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSigned, signed.Status)

	node.update(func(node *withdrawTestNode) {
		node.sendDown = false
		node.sendRejects[signed.RawTx] = true
		node.baseFee = 10_000_000_000
	})
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Replaced)
	assert.Equal(t, 1, summary.Sent)
	replaced := reloadGameCoinWithdrawal(t, withdrawal.Id)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, replaced.Status)
	assert.Equal(t, signed.Nonce, replaced.Nonce)
	assert.NotEqual(t, signed.TxHash, replaced.TxHash)
	assert.Equal(t, 7, gameCoinBalance(t, 2121))
}

// JSON-RPC 应答不是带 result 就是带 error。两个都没有的残缺应答要报错，不能当成"查不到这笔交易"：
// 提现把查不到回执当作交易可能被顶掉的证据。result 为 null 才是真的查不到。
func TestCallEthereumRpcRejectsReplyWithoutResult(t *testing.T) {
	cases := []struct {
		reply   string
		wantErr bool
	}{
		{reply: `{"jsonrpc":"2.0","id":1}`, wantErr: true},
		{reply: `{"jsonrpc":"2.0","id":1,"result":null}`, wantErr: false},
	}
	for _, c := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(c.reply))
		}))
		var receipt *ethereumRpcReceipt
		err := callEthereumRpc(context.Background(), server.URL, "eth_getTransactionReceipt", []any{"0x01"}, &receipt)
		server.Close()
		assert.Equal(t, c.wantErr, err != nil, c.reply)
		assert.Nil(t, receipt, c.reply)
	}
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

// 提现只按申请时的代币发放：申请之后管理员换了代币合约或精度，按新配置发出去就是另一种代币或另一个数量。
// 这时还没有任何链上动作，退回游戏币、释放当日额度不会重复发放。
func TestGameCoinWithdrawalRefundsWhenTokenChangedAfterRequest(t *testing.T) {
	cases := []struct {
		name   string
		change func(setting *operation_setting.GameSetting)
	}{
		{name: "token address", change: func(setting *operation_setting.GameSetting) {
			setting.TokenAddress = "0x3333333333333333333333333333333333333333"
		}},
		{name: "token decimals", change: func(setting *operation_setting.GameSetting) { setting.TokenDecimals = 18 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := &withdrawTestNode{receipts: map[string]map[string]any{}}
			useGameCoinTestChain(t, node)
			withdrawal := createTestGameCoinWithdrawal(t, 2115, 4)
			c.change(operation_setting.GetGameSetting())

			summary := RunGameCoinWithdrawOnce(context.Background())
			assert.Equal(t, 1, summary.Failed)
			assert.Zero(t, summary.Signed)
			assert.Empty(t, node.broadcasts)
			failed := reloadGameCoinWithdrawal(t, withdrawal.Id)
			assert.Equal(t, model.GameCoinWithdrawalStatusFailed, failed.Status)
			assert.Equal(t, "提现的网络或代币已变更，游戏币已退回", failed.FailReason)
			assert.Equal(t, 10, gameCoinBalance(t, 2115))
			used, err := model.GetGameCoinWithdrawDayUsed("2026-09-24")
			require.NoError(t, err)
			assert.Zero(t, used)
		})
	}
}

// 代币地址上没有合约时不签名：那样的转账会执行成功却什么也没转，白花 gas。提现留在待处理，改好配置后照常发出，
// 这一轮记为跳过，系统任务里能看到原因。
func TestGameCoinWithdrawalSkipsTokenWithoutContractCode(t *testing.T) {
	node := &withdrawTestNode{noCode: true, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2108, 2)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Signed)
	assert.Contains(t, summary.Skipped, "no contract code")
	assert.Empty(t, node.broadcasts)
	assert.Equal(t, model.GameCoinWithdrawalStatusPending, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
}

// nonce 从已上链的计数起算：热钱包在别处发出、还在内存池里的交易一旦被丢弃，排在它后面的提现就永远等不到空缺被补上。
func TestGameCoinWithdrawalNonceStartsFromMinedCount(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 7, latestNonce: 5, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2109, 1)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Signed)
	assert.Equal(t, int64(5), reloadGameCoinWithdrawal(t, withdrawal.Id).Nonce)
}

// 已签名的交易先查回执：它可能早已上链，只是节点收下它的那次广播没有得到答复。查到回执就交给对账，
// 不必等重发的报错能被认出来。
func TestGameCoinWithdrawalAdvancesSignedTransactionThatAlreadyMined(t *testing.T) {
	node := &withdrawTestNode{pendingNonce: 2, latestNonce: 2, latestBlock: 100, sendDown: true, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	withdrawal := createTestGameCoinWithdrawal(t, 2110, 3)
	RunGameCoinWithdrawOnce(context.Background())
	signed := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSigned, signed.Status)

	node.update(func(node *withdrawTestNode) { node.receipts[signed.TxHash] = transferReceipt(signed, 100) })
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Sent)
	assert.Len(t, node.broadcasts, 1, "查到回执后不再重发")
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Equal(t, model.GameCoinWithdrawalStatusConfirmed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
}

// 切换网络、换热钱包之后，旧链、旧钱包的单子再多也不能占满这一轮的批次：当前链上当前钱包的提现照常推进。
func TestGameCoinWithdrawalIsNotStarvedByParkedWithdrawals(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, sendRejects: map[string]bool{}, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	const oldWallet = "0x1111111111111111111111111111111111111111"
	for i := 1; i <= gameCoinWithdrawBatchSize+1; i++ {
		createInFlightGameCoinWithdrawal(t, i, model.GameCoinWithdrawalStatusSent, 1, oldWallet)
		parked := createInFlightGameCoinWithdrawal(t, 100+i, model.GameCoinWithdrawalStatusSigned, 11155111, oldWallet)
		node.sendRejects[parked.RawTx] = true
	}
	withdrawal := createTestGameCoinWithdrawal(t, 2111, 3)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Signed)
	assert.Equal(t, 1, summary.Sent)
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)

	node.update(func(node *withdrawTestNode) { node.receipts[sent.TxHash] = transferReceipt(sent, 100) })
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	var parkedSent int64
	require.NoError(t, model.DB.Model(&model.GameCoinWithdrawal{}).Where("chain_id = ? AND status = ?", 1, model.GameCoinWithdrawalStatusSent).Count(&parkedSent).Error)
	assert.EqualValues(t, gameCoinWithdrawBatchSize+1, parkedSent, "旧链上的单子留待切回后再对账")
}

// 已广播待对账的交易、旧钱包签出待重发的交易一轮都只处理一批，下一轮接着往后取：排在前面、一直没有结论的旧钱包交易
// 占满一批，后面已经上链的提现下一轮照样确认，另一个旧钱包签出的交易下一轮照样重发。
func TestGameCoinWithdrawalTakesTurnsPastStuckTransactions(t *testing.T) {
	node := &withdrawTestNode{latestBlock: 100, sendRejects: map[string]bool{}, receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	const oldWallet = "0x1111111111111111111111111111111111111111"
	for i := 1; i <= gameCoinWithdrawBatchSize; i++ {
		// 查不到回执、nonce 也没推进，每一轮都没有结论。
		createInFlightGameCoinWithdrawal(t, i, model.GameCoinWithdrawalStatusSent, 11155111, oldWallet)
		parked := createInFlightGameCoinWithdrawal(t, 100+i, model.GameCoinWithdrawalStatusSigned, 11155111, oldWallet)
		node.sendRejects[parked.RawTx] = true
	}
	otherParked := createInFlightGameCoinWithdrawal(t, 200, model.GameCoinWithdrawalStatusSigned, 11155111, "0x2222222222222222222222222222222222222222")
	withdrawal := createTestGameCoinWithdrawal(t, 2114, 3)

	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Signed)
	assert.Equal(t, 1, summary.Sent)
	sent := reloadGameCoinWithdrawal(t, withdrawal.Id)
	require.Equal(t, model.GameCoinWithdrawalStatusSent, sent.Status)
	assert.Equal(t, model.GameCoinWithdrawalStatusSigned, reloadGameCoinWithdrawal(t, otherParked.Id).Status, "这一轮重发的一批被排在前面的旧钱包交易占满")

	node.update(func(node *withdrawTestNode) { node.receipts[sent.TxHash] = transferReceipt(sent, 100) })
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Confirmed)
	assert.Equal(t, model.GameCoinWithdrawalStatusConfirmed, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)
	assert.Equal(t, 1, summary.Sent)
	assert.Equal(t, model.GameCoinWithdrawalStatusSent, reloadGameCoinWithdrawal(t, otherParked.Id).Status)
}

// 多节点部署时，别的节点上清除或换掉的热钱包私钥要等同步配置才到本节点的内存；worker 签名前从数据库现读，
// 私钥一清除就不再签新提现。
func TestGameCoinWithdrawalReadsHotWalletKeyFromDatabase(t *testing.T) {
	node := &withdrawTestNode{receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	t.Setenv(GameHotWalletPrivateKeyEnv, "")
	withdrawal := createTestGameCoinWithdrawal(t, 2113, 2)

	require.NoError(t, model.DB.Save(&model.Option{Key: operation_setting.GameHotWalletPrivateKeyOption, Value: ""}).Error)
	summary := RunGameCoinWithdrawOnce(context.Background())
	assert.Zero(t, summary.Signed, "私钥已在别的节点上清除")
	assert.Equal(t, model.GameCoinWithdrawalStatusPending, reloadGameCoinWithdrawal(t, withdrawal.Id).Status)

	require.NoError(t, model.DB.Save(&model.Option{Key: operation_setting.GameHotWalletPrivateKeyOption, Value: gameCoinTestKey}).Error)
	summary = RunGameCoinWithdrawOnce(context.Background())
	assert.Equal(t, 1, summary.Signed)
}

// worker 只在能推进某笔提现时才调度：留在别的链上的单子不算；没有可用的热钱包时，待签名的单子也不算。
func TestGameCoinWithdrawalsNeedWork(t *testing.T) {
	node := &withdrawTestNode{receipts: map[string]map[string]any{}}
	useGameCoinTestChain(t, node)
	assert.False(t, GameCoinWithdrawalsNeedWork(), "没有提现")

	createInFlightGameCoinWithdrawal(t, 1, model.GameCoinWithdrawalStatusSent, 1, "0x1111111111111111111111111111111111111111")
	assert.False(t, GameCoinWithdrawalsNeedWork(), "只有留在旧链上的提现")

	createTestGameCoinWithdrawal(t, 2112, 1)
	assert.True(t, GameCoinWithdrawalsNeedWork(), "有待签名的提现，热钱包可用")
	operation_setting.SetGameHotWalletPrivateKey("")
	t.Setenv(GameHotWalletPrivateKeyEnv, "")
	assert.False(t, GameCoinWithdrawalsNeedWork(), "没有热钱包，签不出来")

	createInFlightGameCoinWithdrawal(t, 2, model.GameCoinWithdrawalStatusSent, 11155111, "0x1111111111111111111111111111111111111111")
	assert.True(t, GameCoinWithdrawalsNeedWork(), "当前链上有已广播的交易要对账，不需要热钱包")
}

// 各家节点对重复广播、nonce 已被占用的措辞不同，错误还可能配着非 200 的状态码返回；认得出来的才有结论，
// 其余的一律下一轮重发。
func TestBroadcastGameCoinTxRecognizesNodeErrors(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    rawTxOutcome
	}{
		{status: http.StatusOK, message: "already known", want: rawTxAccepted},
		{status: http.StatusOK, message: "Transaction with the same hash was already imported.", want: rawTxAccepted},
		{status: http.StatusOK, message: "AlreadyKnown", want: rawTxAccepted},
		{status: http.StatusOK, message: "nonce too low: next nonce 8, tx nonce 7", want: rawTxNonceUsed},
		{status: http.StatusOK, message: "Transaction nonce is too low. Try incrementing the nonce.", want: rawTxNonceUsed},
		{status: http.StatusOK, message: "OldNonce", want: rawTxNonceUsed},
		{status: http.StatusInternalServerError, message: "nonce too low", want: rawTxNonceUsed},
		{status: http.StatusOK, message: "replacement transaction underpriced", want: rawTxRetry},
		{status: http.StatusBadGateway, want: rawTxRetry},
	}
	for _, c := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			if c.message != "" {
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":%q}}`, c.message)
			}
		}))
		outcome, _ := broadcastGameCoinTx(context.Background(), server.URL, "0x02")
		server.Close()
		assert.Equal(t, c.want, outcome, "%d %s", c.status, c.message)
	}
}
