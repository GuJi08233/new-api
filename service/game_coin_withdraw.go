package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	gameCoinWithdrawBatchSize = 20
	// 已广播却仍查不到回执的交易，隔这么久重发一次，防止它被节点丢出内存池后再无人知道。
	gameCoinRebroadcastSeconds = 60
	// nonce 已被推进却查不到这笔交易的回执时，再等这么久才判定它被别的交易顶掉。
	// 回执偶尔落后于 nonce 计数，过早退款会让一笔稍后上链的提现变成重复发放。
	gameCoinNonceReplacedSeconds = 10 * 60
	// 节点给不出小费建议时使用 1 gwei。
	gameCoinDefaultTipWei = 1_000_000_000
)

// GameCoinWithdrawRunSummary 是一轮提现处理的结果，写进系统任务的运行记录。
type GameCoinWithdrawRunSummary struct {
	Signed    int    `json:"signed"`
	Sent      int    `json:"sent"`
	Confirmed int    `json:"confirmed"`
	Failed    int    `json:"failed"`
	Skipped   string `json:"skipped,omitempty"`
}

// RunGameCoinWithdrawOnce 推进一轮游戏币提现：先对账已广播的交易，再重发已签名但节点还没收下的
// 交易，最后按创建顺序签发新提现。同一个热钱包的交易必须按 nonce 顺序串行发出；系统任务的
// 分类型租约保证多个主节点同时只有一个在跑。
func RunGameCoinWithdrawOnce(ctx context.Context) GameCoinWithdrawRunSummary {
	summary := GameCoinWithdrawRunSummary{}
	setting := operation_setting.GetGameSetting()
	if !setting.ChainConfigured() {
		summary.Skipped = "chain is not configured"
		return summary
	}
	wallet, err := loadGameHotWallet()
	if err != nil {
		summary.Skipped = err.Error()
		return summary
	}
	chainId, err := ethereumRpcChainId(ctx, setting.RpcUrl)
	if err != nil {
		summary.Skipped = "rpc unavailable"
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: eth_chainId failed: %v", err))
		return summary
	}
	if chainId != setting.ChainId {
		summary.Skipped = fmt.Sprintf("rpc serves chain %d, configured %d", chainId, setting.ChainId)
		return summary
	}

	reconcileSentGameCoinWithdrawals(ctx, setting, &summary)
	if rebroadcastSignedGameCoinWithdrawals(ctx, setting, wallet, &summary) {
		sendPendingGameCoinWithdrawals(ctx, setting, wallet, &summary)
	}
	return summary
}

// reconcileSentGameCoinWithdrawals 按回执给已广播的提现下结论。只有链上明确失败或 nonce 被别的
// 交易占用时才退款；查不到回执、节点报错都只是"还没有结论"。
func reconcileSentGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, summary *GameCoinWithdrawRunSummary) {
	withdrawals, err := model.ListGameCoinWithdrawalsByStatus(model.GameCoinWithdrawalStatusSent, gameCoinWithdrawBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list sent failed: %v", err))
		return
	}
	var latestBlock *big.Int
	for _, withdrawal := range withdrawals {
		if ctx.Err() != nil {
			return
		}
		// 管理员切换了网络时，旧网络上的交易留待切回后再对账。
		if withdrawal.ChainId != setting.ChainId {
			continue
		}
		var receipt *ethereumRpcReceipt
		if err := callEthereumRpc(ctx, setting.RpcUrl, "eth_getTransactionReceipt", []any{withdrawal.TxHash}, &receipt); err != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			continue
		}
		if receipt == nil {
			reconcileUnminedGameCoinWithdrawal(ctx, setting, withdrawal, summary)
			continue
		}

		receiptBlock, blockErr := parseEthereumQuantity(receipt.BlockNumber)
		status, statusErr := parseEthereumQuantity(receipt.Status)
		if blockErr != nil || statusErr != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, fmt.Errorf("malformed receipt for %s", withdrawal.TxHash))
			continue
		}
		if latestBlock == nil {
			latestBlock, err = callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_blockNumber", nil)
			if err != nil {
				recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
				return
			}
		}
		// 成功与失败都要等到确认数：还可能被重组掉的区块里的结论都不算数。
		depth := new(big.Int).Sub(latestBlock, receiptBlock)
		if depth.Add(depth, big.NewInt(1)).Cmp(big.NewInt(int64(setting.Confirmations))) < 0 {
			continue
		}
		if status.Cmp(big.NewInt(1)) == 0 {
			confirmed, err := model.ConfirmGameCoinWithdrawal(withdrawal.Id)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("game coin withdraw: confirm %s failed: %v", withdrawal.TradeNo, err))
				continue
			}
			if confirmed {
				summary.Confirmed++
			}
			continue
		}
		failGameCoinWithdrawal(ctx, withdrawal, "链上转账执行失败，游戏币已退回", fmt.Sprintf("transaction %s reverted", withdrawal.TxHash), summary)
	}
}

// reconcileUnminedGameCoinWithdrawal 处理查不到回执的已广播交易：nonce 还空着就定期重发，
// nonce 已被推进则说明这个位置被别的交易用掉了，宽限期过后退款。
func reconcileUnminedGameCoinWithdrawal(ctx context.Context, setting *operation_setting.GameSetting, withdrawal *model.GameCoinWithdrawal, summary *GameCoinWithdrawRunSummary) {
	accountNonce, err := callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_getTransactionCount", []any{withdrawal.FromAddress, "latest"})
	if err != nil {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
		return
	}
	elapsed := common.GetTimestamp() - withdrawal.BroadcastAt
	if accountNonce.Cmp(big.NewInt(withdrawal.Nonce)) > 0 {
		if elapsed >= gameCoinNonceReplacedSeconds {
			failGameCoinWithdrawal(ctx, withdrawal, "交易未能上链，游戏币已退回",
				fmt.Sprintf("nonce %d of %s was used by another transaction", withdrawal.Nonce, withdrawal.FromAddress), summary)
		}
		return
	}
	if elapsed < gameCoinRebroadcastSeconds {
		return
	}
	_, err = broadcastGameCoinTx(ctx, setting.RpcUrl, withdrawal.RawTx)
	recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
}

// rebroadcastSignedGameCoinWithdrawals 重发已签名但节点尚未收下的交易。返回 false 表示当前热钱包
// 的队首仍卡着一笔，这一轮不能再签新交易：nonce 必须连续，插在后面的交易只会在内存池里等它。
func rebroadcastSignedGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, summary *GameCoinWithdrawRunSummary) bool {
	withdrawals, err := model.ListGameCoinWithdrawalsByStatus(model.GameCoinWithdrawalStatusSigned, gameCoinWithdrawBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list signed failed: %v", err))
		return false
	}
	for _, withdrawal := range withdrawals {
		if ctx.Err() != nil {
			return false
		}
		if withdrawal.ChainId != setting.ChainId {
			continue
		}
		outcome, err := broadcastGameCoinTx(ctx, setting.RpcUrl, withdrawal.RawTx)
		if outcome == rawTxRetry {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
			// 换过热钱包之后，旧钱包的交易卡住不影响新钱包的 nonce 顺序。
			if withdrawal.FromAddress == wallet.address {
				return false
			}
			continue
		}
		// nonce 已被使用时同样交给对账：是这笔交易自己上链了就会有回执，被顶掉则宽限期后退款。
		if marked, err := model.MarkGameCoinWithdrawalSent(withdrawal.Id); err != nil {
			logger.LogError(ctx, fmt.Sprintf("game coin withdraw: mark %s sent failed: %v", withdrawal.TradeNo, err))
			return false
		} else if marked {
			summary.Sent++
		}
	}
	return true
}

// sendPendingGameCoinWithdrawals 按创建顺序给新提现分配 nonce、签名、落库，然后广播。
// 签好的交易先落库再广播，进程在两步之间退出时下一轮重发的是同一笔交易，不会再签第二笔。
func sendPendingGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, summary *GameCoinWithdrawRunSummary) {
	withdrawals, err := model.ListGameCoinWithdrawalsByStatus(model.GameCoinWithdrawalStatusPending, gameCoinWithdrawBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list pending failed: %v", err))
		return
	}
	if len(withdrawals) == 0 {
		return
	}
	tokenAddress, err := NormalizeEthereumAddress(setting.TokenAddress)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: token address invalid: %v", err))
		return
	}
	token, _ := ethereumAddressBytes(tokenAddress)

	pendingNonce, err := callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_getTransactionCount", []any{wallet.address, "pending"})
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: fetch nonce failed: %v", err))
		return
	}
	nonce := pendingNonce.Uint64()
	// 节点的 pending 计数可能还没看到刚发出的交易，已签出的最大 nonce 以数据库为准。
	if maxNonce, found, err := model.MaxGameCoinWithdrawalNonce(wallet.address, setting.ChainId); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: read max nonce failed: %v", err))
		return
	} else if found && uint64(maxNonce)+1 > nonce {
		nonce = uint64(maxNonce) + 1
	}
	tip, maxFee, err := suggestGameCoinFees(ctx, setting.RpcUrl)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: fee suggestion failed: %v", err))
		return
	}

	for _, withdrawal := range withdrawals {
		if ctx.Err() != nil {
			return
		}
		recipient, err := ethereumAddressBytes(withdrawal.ToAddress)
		if err != nil {
			failGameCoinWithdrawal(ctx, withdrawal, "收款地址无效，游戏币已退回", err.Error(), summary)
			continue
		}
		data := erc20TransferData(recipient, GameTokenRawAmount(withdrawal.Amount, setting.TokenDecimals))
		gas, err := callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_estimateGas", []any{map[string]any{
			"from": wallet.address,
			"to":   tokenAddress,
			"data": "0x" + hex.EncodeToString(data),
		}})
		if err != nil {
			// 预估时就回滚，说明这笔转账现在必然失败(热钱包代币不足、合约暂停等)，直接退回，
			// 不必花 gas 把一笔注定回滚的交易送上链。
			if isEthereumExecutionReverted(err) {
				failGameCoinWithdrawal(ctx, withdrawal, "热钱包代币不足或合约拒绝转账，游戏币已退回", err.Error(), summary)
				continue
			}
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			return
		}
		tx := &gameTransferTx{
			chainId:              big.NewInt(setting.ChainId),
			nonce:                nonce,
			maxPriorityFeePerGas: tip,
			maxFeePerGas:         maxFee,
			gas:                  gas.Uint64() + gas.Uint64()/4,
			to:                   token,
			data:                 data,
		}
		rawTx, txHash, err := signGameTransferTx(tx, wallet.key)
		if err != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			return
		}
		marked, err := model.MarkGameCoinWithdrawalSigned(withdrawal.Id, setting.ChainId, tokenAddress, wallet.address, int64(nonce), txHash, rawTx)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("game coin withdraw: persist signed %s failed: %v", withdrawal.TradeNo, err))
			return
		}
		if !marked {
			continue
		}
		summary.Signed++
		nonce++

		outcome, err := broadcastGameCoinTx(ctx, setting.RpcUrl, rawTx)
		if outcome == rawTxRetry {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
			return
		}
		if marked, err := model.MarkGameCoinWithdrawalSent(withdrawal.Id); err != nil {
			logger.LogError(ctx, fmt.Sprintf("game coin withdraw: mark %s sent failed: %v", withdrawal.TradeNo, err))
			return
		} else if marked {
			summary.Sent++
		}
	}
}

// suggestGameCoinFees 给出 EIP-1559 的小费与费用上限。上限取基础费的三倍加小费，之后连涨
// 几个区块也不至于卡住；实际只按打包时的基础费扣。
func suggestGameCoinFees(ctx context.Context, rpcURL string) (tip *big.Int, maxFee *big.Int, err error) {
	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := callEthereumRpc(ctx, rpcURL, "eth_getBlockByNumber", []any{"latest", false}, &block); err != nil {
		return nil, nil, err
	}
	if block.BaseFeePerGas == "" {
		return nil, nil, errors.New("latest block has no baseFeePerGas; the chain must support EIP-1559")
	}
	baseFee, err := parseEthereumQuantity(block.BaseFeePerGas)
	if err != nil {
		return nil, nil, err
	}
	tip, err = callEthereumRpcQuantity(ctx, rpcURL, "eth_maxPriorityFeePerGas", nil)
	if err != nil || tip.Sign() <= 0 {
		tip = big.NewInt(gameCoinDefaultTipWei)
	}
	maxFee = new(big.Int).Mul(baseFee, big.NewInt(3))
	return tip, maxFee.Add(maxFee, tip), nil
}

type rawTxOutcome int

const (
	rawTxAccepted  rawTxOutcome = iota // 节点收下了，或这笔交易早已在它的内存池里
	rawTxNonceUsed                     // 这个 nonce 已经上链，是不是这笔交易要看回执
	rawTxRetry                         // 没有结论，下一轮重发同一笔
)

func broadcastGameCoinTx(ctx context.Context, rpcURL string, rawTx string) (rawTxOutcome, error) {
	var txHash string
	err := callEthereumRpc(ctx, rpcURL, "eth_sendRawTransaction", []any{rawTx}, &txHash)
	if err == nil {
		return rawTxAccepted, nil
	}
	var rpcErr *ethereumRpcError
	if errors.As(err, &rpcErr) {
		message := strings.ToLower(rpcErr.Message)
		switch {
		case strings.Contains(message, "already known"), strings.Contains(message, "known transaction"), strings.Contains(message, "alreadyknown"):
			return rawTxAccepted, nil
		case strings.Contains(message, "nonce too low"), strings.Contains(message, "oldnonce"):
			return rawTxNonceUsed, err
		}
	}
	return rawTxRetry, err
}

func isEthereumExecutionReverted(err error) bool {
	var rpcErr *ethereumRpcError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == 3 || strings.Contains(strings.ToLower(rpcErr.Message), "execution reverted")
}

func failGameCoinWithdrawal(ctx context.Context, withdrawal *model.GameCoinWithdrawal, reason string, detail string, summary *GameCoinWithdrawRunSummary) {
	failed, err := model.FailGameCoinWithdrawal(withdrawal.Id, reason, detail)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: refund %s failed: %v", withdrawal.TradeNo, err))
		return
	}
	if !failed {
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: %s refunded: %s", withdrawal.TradeNo, detail))
	summary.Failed++
}

func recordGameCoinWithdrawalAttempt(ctx context.Context, withdrawal *model.GameCoinWithdrawal, broadcast bool, attemptErr error) {
	errText := ""
	if attemptErr != nil {
		errText = attemptErr.Error()
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: %s: %s", withdrawal.TradeNo, errText))
	}
	if err := model.RecordGameCoinWithdrawalAttempt(withdrawal.Id, broadcast, errText); err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: record attempt for %s failed: %v", withdrawal.TradeNo, err))
	}
}
