package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	gameCoinWithdrawBatchSize = 20
	// 已广播却仍查不到回执的交易，隔这么久重发一次，防止它被节点丢出内存池后再无人知道。
	gameCoinRebroadcastSeconds = 60
	// nonce 已被推进却查不到这笔交易的回执时，这种状态要连续保持这么久才判定它被别的交易顶掉。
	// 回执偶尔落后于 nonce 计数(负载均衡后面的节点进度不一)，过早退款会让一笔已经上链的提现变成重复发放；
	// 期间只要查到一次回执，就从头计算。
	gameCoinNonceReplacedSeconds = 10 * 60
	// 对账连续性中断这么久后重新观察，避免 worker 或 RPC 长停后立即退款。
	// 定时任务跨批次、跨节点传递连续性；没有共享观察状态的单次调用按每笔的检查间隔保守判断。
	gameCoinNonceWatchGapSeconds = 3 * 60
	// 节点给不出小费建议时使用 1 gwei。
	gameCoinDefaultTipWei = 1_000_000_000
	// 最简单的转账也要 21000 gas，预估值比它还小说明节点的回答有问题。
	gameCoinMinGas = 21_000
	// erc20TransferTopic 是 keccak256("Transfer(address,address,uint256)")，ERC-20 转账事件的 topics[0]。
	erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

// 轮流处理的两类提现单各自记着上一轮取到的最后一笔的 id，0 表示从头开始：已广播待对账的，和换下来的热钱包签出、
// 节点还没收下的。系统任务的租约保证同一时间只有一个节点在跑提现，各节点各记各的，轮到谁都能把所有单子轮一遍。
var (
	gameCoinSentCursor         atomic.Int64
	gameCoinParkedSignedCursor atomic.Int64
	// 单次入口没有持久 watch，计时写入失败后也必须让本进程下一次调用重新观察。
	gameCoinNonceStateFailedAt atomic.Int64
)

// GameCoinWithdrawRunSummary 是一轮提现处理的结果，写进系统任务的运行记录。Skipped 不为空表示这一轮
// 因为配置或节点的问题没能处理，任务记录会标为失败。
type GameCoinWithdrawRunSummary struct {
	Signed     int                 `json:"signed"`
	Replaced   int                 `json:"replaced"`
	Sent       int                 `json:"sent"`
	Confirmed  int                 `json:"confirmed"`
	Failed     int                 `json:"failed"`
	Skipped    string              `json:"skipped,omitempty"`
	NonceWatch *GameCoinNonceWatch `json:"nonce_watch,omitempty"`
}

// GameCoinNonceWatch 随系统任务的 payload/result 交接，记录当前 RPC 上成功对账的连续观察周期。
// 某一笔轮到下一批可能超过三分钟，不能把正常积压当成整个 worker 停机；RPC 地址只保存摘要。
type GameCoinNonceWatch struct {
	ChainId    int64  `json:"chain_id"`
	RpcURLHash string `json:"rpc_url_hash"`
	StartedAt  int64  `json:"started_at"`
	CheckedAt  int64  `json:"checked_at"`
}

func (watch *GameCoinNonceWatch) observe(setting *operation_setting.GameSetting, now int64) int64 {
	rpcURLHash := hex.EncodeToString(common.Sha256Raw([]byte(setting.RpcUrl)))
	if watch.ChainId != setting.ChainId || watch.RpcURLHash != rpcURLHash || watch.StartedAt <= 0 ||
		watch.CheckedAt < watch.StartedAt || now < watch.CheckedAt || now-watch.CheckedAt > gameCoinNonceWatchGapSeconds {
		watch.ChainId = setting.ChainId
		watch.RpcURLHash = rpcURLHash
		watch.StartedAt = now
	}
	watch.CheckedAt = now
	return watch.StartedAt
}

// gameCoinFees 是这一轮签名用的 EIP-1559 费用：最新区块的基础费、小费，以及费用上限(三倍基础费加小费)。
type gameCoinFees struct {
	baseFee *big.Int
	tip     *big.Int
	maxFee  *big.Int
}

// GameCoinWithdrawalsNeedWork 决定提现 worker 这一轮是否需要调度：只在它能推进某笔提现时才产生任务记录。
// 留在别的链上的交易要等管理员切回那条链；没有可用的热钱包时签不出新提现，只跟进已经签出的交易。
func GameCoinWithdrawalsNeedWork() bool {
	setting := operation_setting.GetGameSetting()
	if !setting.ChainConfigured() {
		return false
	}
	_, walletErr := loadGameHotWallet()
	return model.HasGameCoinWithdrawalWork(setting.ChainId, walletErr == nil)
}

// RunGameCoinWithdrawOnce 推进一轮游戏币提现：先对账已广播的交易，再跟进已签名但节点还没收下的
// 交易，最后按创建顺序签发新提现。同一个热钱包的交易必须按 nonce 顺序串行发出；系统任务的
// 分类型租约保证多个主节点同时只有一个在跑。没有可用的热钱包或拿不到费用时签不了交易(新提现和加价重签都不行)，
// 已经签出的交易照常跟进。
func RunGameCoinWithdrawOnce(ctx context.Context) GameCoinWithdrawRunSummary {
	// 单次调用没有跨轮次的连续性证明，仍按每笔最多三分钟的检查间隔保守处理。
	return RunGameCoinWithdrawOnceWithNonceWatch(ctx, nil)
}

// RunGameCoinWithdrawOnceWithNonceWatch 供定时任务使用，watch 必须从上一轮结果交接(首轮传空值的地址)。
// 只有成功的链上观察才推进它，调用方将返回结果持久化后，另一个节点也能接着观察。
func RunGameCoinWithdrawOnceWithNonceWatch(ctx context.Context, watch *GameCoinNonceWatch) GameCoinWithdrawRunSummary {
	summary := GameCoinWithdrawRunSummary{NonceWatch: watch}
	setting := operation_setting.GetGameSetting()
	if !setting.ChainConfigured() {
		summary.Skipped = "chain is not configured"
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

	// 多节点部署时，别的节点上刚换掉或清除的热钱包私钥要等下一次同步配置才会到这里；签名前从数据库现读一次。
	if key, err := model.ReadOptionValue(operation_setting.GameHotWalletPrivateKeyOption); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: read hot wallet key failed: %v", err))
	} else {
		operation_setting.SetGameHotWalletPrivateKey(key)
	}
	wallet, _ := loadGameHotWallet()
	fees, err := suggestGameCoinFees(ctx, setting.RpcUrl)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: fee suggestion failed: %v", err))
	}

	reconcileSentGameCoinWithdrawals(ctx, setting, wallet, fees, watch, &summary)
	if !rebroadcastSignedGameCoinWithdrawals(ctx, setting, wallet, fees, &summary) || wallet == nil || fees == nil {
		return summary
	}
	sendPendingGameCoinWithdrawals(ctx, setting, wallet, fees, &summary)
	return summary
}

// reconcileSentGameCoinWithdrawals 按回执给当前链上已广播的提现下结论。只有链上明确失败、转账没有发生或
// nonce 被别的交易占用时才退款；查不到回执、回执不完整、节点报错都只是"还没有结论"。管理员切换了网络时，
// 旧网络上的交易留待切回后再对账。一轮只对账一批，按 listGameCoinWithdrawalsInTurn 轮流取。
func reconcileSentGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, watch *GameCoinNonceWatch, summary *GameCoinWithdrawRunSummary) {
	withdrawals, err := listGameCoinWithdrawalsInTurn(model.GameCoinWithdrawalStatusSent,
		model.GameCoinWithdrawalScope{ChainId: setting.ChainId}, &gameCoinSentCursor)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list sent failed: %v", err))
		return
	}
	var latestBlock *big.Int
	for _, withdrawal := range withdrawals {
		if ctx.Err() != nil {
			return
		}
		receipt, minedTxHash, err := lookupGameCoinWithdrawalReceipt(ctx, setting.RpcUrl, withdrawal)
		if err != nil {
			if withdrawal.NonceUsedAt != 0 {
				if !setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch) {
					return
				}
			}
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if watch != nil {
			watch.observe(setting, common.GetTimestamp())
		}
		if receipt == nil {
			if !reconcileUnminedGameCoinWithdrawal(ctx, setting, wallet, fees, withdrawal, watch, summary) {
				return
			}
			continue
		}

		// 查到了回执，节点知道这笔交易，之前"nonce 已推进却查不到回执"的计时作废；回执不完整也一样。
		if withdrawal.NonceUsedAt != 0 {
			if !setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch) {
				return
			}
		}
		// 有的节点对还没打包的交易也给出回执，只是区块号与状态为空，这同样还没有结论。
		receiptBlock, blockErr := parseEthereumQuantity(receipt.BlockNumber)
		status, statusErr := parseEthereumQuantity(receipt.Status)
		if blockErr != nil || statusErr != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, fmt.Errorf("incomplete receipt for %s", minedTxHash))
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
		if status.Cmp(big.NewInt(1)) != 0 {
			failGameCoinWithdrawal(ctx, withdrawal, "链上转账执行失败，游戏币已退回", fmt.Sprintf("transaction %s reverted", minedTxHash), summary)
			continue
		}
		// 回执里没有 logs 字段(节点或代理把它截掉了)时看不出代币有没有转出，这同样还没有结论，不能当成没有转账。
		if receipt.Logs == nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, fmt.Errorf("receipt for %s has no logs", minedTxHash))
			continue
		}
		// 交易成功不等于代币到账：代币地址上没有合约，或者合约的 transfer 返回 false 而不回滚时，交易照样成功，
		// 只是没有转账事件。交易已经上链，不会再执行第二次，这时退款不会重复发放。
		if !receiptHasErc20Transfer(receipt.Logs, withdrawal.TokenAddress, withdrawal.FromAddress, withdrawal.ToAddress) {
			failGameCoinWithdrawal(ctx, withdrawal, "代币合约没有完成转账，游戏币已退回",
				fmt.Sprintf("transaction %s succeeded without a Transfer event", minedTxHash), summary)
			continue
		}
		confirmed, err := model.ConfirmGameCoinWithdrawal(withdrawal, minedTxHash)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("game coin withdraw: confirm %s failed: %v", withdrawal.TradeNo, err))
			continue
		}
		if confirmed {
			summary.Confirmed++
		}
	}
}

// listGameCoinWithdrawalsInTurn 从 cursor 记下的位置接着取一批提现单，取到末尾就从头开始，并把下一轮的起点记回 cursor。
// 按 id 顺序每次只取开头一批的话，排在前面、迟迟没有结论的单子(例如换下来的旧热钱包没有 gas 重发)一多，
// 后面的单子就永远轮不到。只能用于各单互不影响的处理：同一个热钱包的新交易要按 nonce 顺序发，不能这样轮。
func listGameCoinWithdrawalsInTurn(status string, scope model.GameCoinWithdrawalScope, cursor *atomic.Int64) ([]*model.GameCoinWithdrawal, error) {
	scope.AfterId = int(cursor.Load())
	withdrawals, err := model.ListGameCoinWithdrawalsByStatus(status, scope, gameCoinWithdrawBatchSize)
	if err == nil && len(withdrawals) == 0 && scope.AfterId != 0 {
		scope.AfterId = 0
		withdrawals, err = model.ListGameCoinWithdrawalsByStatus(status, scope, gameCoinWithdrawBatchSize)
	}
	if err != nil {
		return nil, err
	}
	// 这一批没取满，说明已经取到末尾。
	next := int64(0)
	if len(withdrawals) == gameCoinWithdrawBatchSize {
		next = int64(withdrawals[len(withdrawals)-1].Id)
	}
	cursor.Store(next)
	return withdrawals, nil
}

// lookupGameCoinWithdrawalReceipt 依次查这笔提现签出过的每个交易哈希的回执，返回查到的第一个和它的哈希，都查不到时
// 回执为 nil。同一个 nonce 只会有一笔上链，查到哪一笔就以哪一笔为准；任何一次查询出错都算没有结论。
func lookupGameCoinWithdrawalReceipt(ctx context.Context, rpcURL string, withdrawal *model.GameCoinWithdrawal) (*ethereumRpcReceipt, string, error) {
	hashes, err := withdrawal.TxHashes()
	if err != nil {
		return nil, "", err
	}
	for _, hash := range hashes {
		var receipt *ethereumRpcReceipt
		if err := callEthereumRpc(ctx, rpcURL, "eth_getTransactionReceipt", []any{hash}, &receipt); err != nil {
			return nil, "", err
		}
		if receipt != nil {
			return receipt, hash, nil
		}
	}
	return nil, "", nil
}

// receiptHasErc20Transfer 判断回执里有没有这个代币合约发出的、从 from 转给 to 的 Transfer 事件。标准写法把 from、to
// 放在 topics[1]、topics[2]；早期有的代币三个参数都不加 indexed，from、to、数量依次放在 data 里。
// 数量不比对：交易是自己签的，数量写死在里面，而管理员可能在签名之后改过代币精度。
func receiptHasErc20Transfer(logs []ethereumRpcLog, token string, from string, to string) bool {
	fromWord := "000000000000000000000000" + strings.TrimPrefix(strings.ToLower(from), "0x")
	toWord := "000000000000000000000000" + strings.TrimPrefix(strings.ToLower(to), "0x")
	for _, entry := range logs {
		if entry.Removed || len(entry.Topics) == 0 || !strings.EqualFold(entry.Address, token) ||
			!strings.EqualFold(entry.Topics[0], erc20TransferTopic) {
			continue
		}
		if len(entry.Topics) >= 3 &&
			strings.EqualFold(entry.Topics[1], "0x"+fromWord) && strings.EqualFold(entry.Topics[2], "0x"+toWord) {
			return true
		}
		data := strings.ToLower(strings.TrimPrefix(entry.Data, "0x"))
		if len(entry.Topics) == 1 && len(data) >= 3*64 && data[:64] == fromWord && data[64:128] == toWord {
			return true
		}
	}
	return false
}

// reconcileUnminedGameCoinWithdrawal 处理每个哈希都查不到回执的已广播交易。nonce 还空着时：费用上限已经付不起现在的
// 基础费就加价重签，否则定期重发。nonce 已被推进，说明这个位置被别的交易用掉了：这种状态要连续保持
// gameCoinNonceReplacedSeconds、期间 worker 对账持续成功(这笔自身查询失败时也重新观察)，最后还要节点对这笔提现的
// 每个哈希都一无所知，才退款。退款晚一些没关系，把一笔已经上链的转账退掉就是重复发放。
// 返回 false 表示本轮必须停止对账，防止计时写入失败后又被其他单的成功观察掩盖。
func reconcileUnminedGameCoinWithdrawal(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, withdrawal *model.GameCoinWithdrawal, watch *GameCoinNonceWatch, summary *GameCoinWithdrawRunSummary) bool {
	accountNonce, err := callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_getTransactionCount", []any{withdrawal.FromAddress, "latest"})
	if err != nil {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
		if withdrawal.NonceUsedAt != 0 {
			return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch)
		}
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	now := common.GetTimestamp()
	observationInterrupted := now-withdrawal.NonceCheckedAt > gameCoinNonceWatchGapSeconds
	if watch != nil {
		observationInterrupted = withdrawal.NonceUsedAt < watch.observe(setting, now)
	} else if withdrawal.NonceUsedAt < gameCoinNonceStateFailedAt.Load() {
		observationInterrupted = true
	}
	if accountNonce.Cmp(big.NewInt(withdrawal.Nonce)) > 0 {
		switch {
		case withdrawal.NonceUsedAt == 0 || observationInterrupted:
			return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, now, now, watch)
		case now-withdrawal.NonceUsedAt < gameCoinNonceReplacedSeconds:
			return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, withdrawal.NonceUsedAt, now, watch)
		}
		// 回执查询可能落在进度落后的节点上；按哈希查交易本身再确认一次，节点知道其中任何一笔(还在内存池或已经上链)
		// 都说明它没有被顶掉。
		hashes, err := withdrawal.TxHashes()
		if err != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch)
		}
		for _, hash := range hashes {
			var tx *struct {
				Hash string `json:"hash"`
			}
			if err := callEthereumRpc(ctx, setting.RpcUrl, "eth_getTransactionByHash", []any{hash}, &tx); err != nil {
				recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
				return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch)
			}
			if tx != nil {
				return setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch)
			}
		}
		failGameCoinWithdrawal(ctx, withdrawal, "交易未能上链，游戏币已退回",
			fmt.Sprintf("nonce %d of %s was used by another transaction", withdrawal.Nonce, withdrawal.FromAddress), summary)
		return true
	}
	if withdrawal.NonceUsedAt != 0 {
		if !setGameCoinWithdrawalNonceUsed(ctx, withdrawal, 0, 0, watch) {
			return false
		}
	}
	if replaceUnderpricedGameCoinTx(ctx, setting, wallet, fees, withdrawal, summary) {
		return true
	}
	if now-withdrawal.BroadcastAt < gameCoinRebroadcastSeconds {
		return true
	}
	_, err = broadcastGameCoinTx(ctx, setting.RpcUrl, withdrawal.RawTx)
	recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
	return true
}

// replaceUnderpricedGameCoinTx 在基础费涨过一笔还没上链的交易的费用上限、它再也上不了链时，用同一个 nonce、同样的 gas
// 加价重签并广播：不然这笔一直卡着，这个热钱包之后的提现全都排在它后面。新交易的小费和上限都比旧的高 12.5%
// (节点只接受至少加价 10% 的替换交易)，上限也不低于现在的三倍基础费加小费。同一个 nonce 只会有一笔上链，旧交易之后
// 即便被打包也只是它上链，不会多发。只有签出它的热钱包能重签，没有费用记录的交易不重签。返回 true 表示已经换成新交易。
func replaceUnderpricedGameCoinTx(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, withdrawal *model.GameCoinWithdrawal, summary *GameCoinWithdrawRunSummary) bool {
	if wallet == nil || fees == nil || withdrawal.GasLimit <= 0 || !strings.EqualFold(wallet.address, withdrawal.FromAddress) {
		return false
	}
	maxFee, maxFeeOk := new(big.Int).SetString(withdrawal.MaxFeePerGas, 10)
	tip, tipOk := new(big.Int).SetString(withdrawal.PriorityFeePerGas, 10)
	if !maxFeeOk || !tipOk || maxFee.Cmp(new(big.Int).Add(fees.baseFee, tip)) >= 0 {
		return false
	}
	newTip := bumpGameCoinFee(tip)
	if newTip.Cmp(fees.tip) < 0 {
		newTip = fees.tip
	}
	newMaxFee := bumpGameCoinFee(maxFee)
	if floor := new(big.Int).Add(new(big.Int).Mul(fees.baseFee, big.NewInt(3)), newTip); newMaxFee.Cmp(floor) < 0 {
		newMaxFee = floor
	}
	token, err := ethereumAddressBytes(withdrawal.TokenAddress)
	if err != nil {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
		return false
	}
	recipient, err := ethereumAddressBytes(withdrawal.ToAddress)
	if err != nil {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
		return false
	}
	rawTx, txHash, err := signGameTransferTx(&gameTransferTx{
		chainId:              big.NewInt(withdrawal.ChainId),
		nonce:                uint64(withdrawal.Nonce),
		maxPriorityFeePerGas: newTip,
		maxFeePerGas:         newMaxFee,
		gas:                  uint64(withdrawal.GasLimit),
		to:                   token,
		data:                 erc20TransferData(recipient, GameTokenRawAmount(withdrawal.Amount, withdrawal.TokenDecimals)),
	}, wallet.key)
	if err != nil {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
		return false
	}
	replaced, err := model.ReplaceGameCoinWithdrawalTx(withdrawal, model.GameCoinSignedTx{
		FromAddress:       withdrawal.FromAddress,
		Nonce:             withdrawal.Nonce,
		TxHash:            txHash,
		RawTx:             rawTx,
		GasLimit:          withdrawal.GasLimit,
		MaxFeePerGas:      newMaxFee.String(),
		PriorityFeePerGas: newTip.String(),
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: persist replacement of %s failed: %v", withdrawal.TradeNo, err))
		return false
	}
	if !replaced {
		return false
	}
	summary.Replaced++
	logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: %s re-signed nonce %d with max fee %s wei, base fee is %s wei",
		withdrawal.TradeNo, withdrawal.Nonce, newMaxFee, fees.baseFee))
	// 与首次签发一样先落库再广播：广播没有结论时留在已签名，下一轮重发。
	if outcome, err := broadcastGameCoinTx(ctx, setting.RpcUrl, rawTx); outcome == rawTxRetry {
		recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
		return true
	}
	if marked, err := model.MarkGameCoinWithdrawalSent(withdrawal.Id); err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: mark %s sent failed: %v", withdrawal.TradeNo, err))
	} else if marked {
		summary.Sent++
	}
	return true
}

// bumpGameCoinFee 把费用加价 12.5% 并向上取整。
func bumpGameCoinFee(fee *big.Int) *big.Int {
	bumped := new(big.Int).Mul(fee, big.NewInt(9))
	bumped.Add(bumped, big.NewInt(7))
	return bumped.Div(bumped, big.NewInt(8))
}

// rebroadcastSignedGameCoinWithdrawals 跟进当前链上已签名但节点尚未收下的交易。返回 false 表示当前热钱包
// 的队首仍卡着一笔，这一轮不能再签新交易：nonce 必须连续，插在后面的交易只会在内存池里等它。
// 换过热钱包之后，旧钱包卡住的交易不影响新钱包，只尽力重发，一轮一批轮流取；两类分开查，旧钱包的再多也挤不掉新钱包的。
func rebroadcastSignedGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, summary *GameCoinWithdrawRunSummary) bool {
	walletAddress := ""
	if wallet != nil {
		walletAddress = wallet.address
	}
	others, err := listGameCoinWithdrawalsInTurn(model.GameCoinWithdrawalStatusSigned,
		model.GameCoinWithdrawalScope{ChainId: setting.ChainId, ExceptFromAddress: walletAddress}, &gameCoinParkedSignedCursor)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list signed failed: %v", err))
		return false
	}
	for _, withdrawal := range others {
		if ctx.Err() != nil {
			return false
		}
		advanceSignedGameCoinWithdrawal(ctx, setting, wallet, fees, withdrawal, summary)
	}
	if walletAddress == "" {
		return true
	}
	current, err := model.ListGameCoinWithdrawalsByStatus(model.GameCoinWithdrawalStatusSigned,
		model.GameCoinWithdrawalScope{ChainId: setting.ChainId, FromAddress: walletAddress}, gameCoinWithdrawBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: list signed failed: %v", err))
		return false
	}
	for _, withdrawal := range current {
		if ctx.Err() != nil || !advanceSignedGameCoinWithdrawal(ctx, setting, wallet, fees, withdrawal, summary) {
			return false
		}
	}
	return true
}

// advanceSignedGameCoinWithdrawal 让一笔已签名的交易往前走：查到回执(签出过的任何一笔)、或者节点收下了，就转为已广播，
// 交给对账下结论。先查回执是因为它可能早已上链，只是节点收下它的那次广播没有得到答复，之后重发报的错又认不出来。
// 节点不收时，可能是基础费已经涨过这笔交易的费用上限，付不起就加价重签。返回 false 表示它还卡着，下一轮重试。
func advanceSignedGameCoinWithdrawal(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, withdrawal *model.GameCoinWithdrawal, summary *GameCoinWithdrawRunSummary) bool {
	receipt, _, err := lookupGameCoinWithdrawalReceipt(ctx, setting.RpcUrl, withdrawal)
	if err != nil || receipt == nil {
		outcome, err := broadcastGameCoinTx(ctx, setting.RpcUrl, withdrawal.RawTx)
		if outcome == rawTxRetry {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, true, err)
			replaceUnderpricedGameCoinTx(ctx, setting, wallet, fees, withdrawal, summary)
			return false
		}
	}
	// nonce 已被使用时同样交给对账：是这笔交易自己上链了就会有回执，被顶掉则宽限期后退款。
	marked, err := model.MarkGameCoinWithdrawalSent(withdrawal.Id)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: mark %s sent failed: %v", withdrawal.TradeNo, err))
		return false
	}
	if marked {
		summary.Sent++
	}
	return true
}

// sendPendingGameCoinWithdrawals 按创建顺序给新提现分配 nonce、签名、落库，然后广播。
// 签好的交易先落库再广播，进程在两步之间退出时下一轮重发的是同一笔交易，不会再签第二笔。
func sendPendingGameCoinWithdrawals(ctx context.Context, setting *operation_setting.GameSetting, wallet *gameHotWallet, fees *gameCoinFees, summary *GameCoinWithdrawRunSummary) {
	withdrawals, err := model.ListGameCoinWithdrawalsByStatus(model.GameCoinWithdrawalStatusPending,
		model.GameCoinWithdrawalScope{}, gameCoinWithdrawBatchSize)
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
	// 代币地址上没有合约时，转账交易照样执行成功却什么也没转；先确认它是合约，免得白花 gas。
	var code string
	if err := callEthereumRpc(ctx, setting.RpcUrl, "eth_getCode", []any{tokenAddress, "latest"}, &code); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: eth_getCode failed: %v", err))
		return
	}
	if strings.TrimPrefix(code, "0x") == "" {
		summary.Skipped = fmt.Sprintf("token address %s has no contract code on chain %d", tokenAddress, setting.ChainId)
		return
	}

	// nonce 从已上链的计数起算，不用 pending：pending 会把这个热钱包在别处发出、还在内存池里的交易也算进去，
	// 那笔交易一旦被丢弃，排在它后面的提现就永远等不到空缺被补上。自己签出还没上链的交易以数据库为准。
	accountNonce, err := callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_getTransactionCount", []any{wallet.address, "latest"})
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: fetch nonce failed: %v", err))
		return
	}
	nonce := accountNonce.Uint64()
	if maxNonce, found, err := model.MaxGameCoinWithdrawalNonce(wallet.address, setting.ChainId); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("game coin withdraw: read max nonce failed: %v", err))
		return
	} else if found && uint64(maxNonce)+1 > nonce {
		nonce = uint64(maxNonce) + 1
	}

	for _, withdrawal := range withdrawals {
		if ctx.Err() != nil {
			return
		}
		// 提现只按申请时的代币发放：管理员之后换了网络、合约或精度，照现在的配置发出去就是另一种代币或另一个数量。
		// 这笔提现还没有任何链上动作，退回游戏币不会重复发放，玩家可以按新配置重新申请。
		if withdrawal.ChainId != setting.ChainId || !strings.EqualFold(withdrawal.TokenAddress, tokenAddress) ||
			withdrawal.TokenDecimals != setting.TokenDecimals {
			failGameCoinWithdrawal(ctx, withdrawal, "提现的网络或代币已变更，游戏币已退回",
				fmt.Sprintf("requested chain %d token %s decimals %d, configured chain %d token %s decimals %d",
					withdrawal.ChainId, withdrawal.TokenAddress, withdrawal.TokenDecimals, setting.ChainId, tokenAddress, setting.TokenDecimals), summary)
			continue
		}
		recipient, err := ethereumAddressBytes(withdrawal.ToAddress)
		if err != nil {
			failGameCoinWithdrawal(ctx, withdrawal, "收款地址无效，游戏币已退回", err.Error(), summary)
			continue
		}
		data := erc20TransferData(recipient, GameTokenRawAmount(withdrawal.Amount, withdrawal.TokenDecimals))
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
		if gas.Cmp(big.NewInt(gameCoinMinGas)) < 0 || !gas.IsUint64() {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, fmt.Errorf("eth_estimateGas returned an implausible %s", gas))
			return
		}
		tx := &gameTransferTx{
			chainId:              big.NewInt(setting.ChainId),
			nonce:                nonce,
			maxPriorityFeePerGas: fees.tip,
			maxFeePerGas:         fees.maxFee,
			gas:                  gas.Uint64() + gas.Uint64()/4,
			to:                   token,
			data:                 data,
		}
		rawTx, txHash, err := signGameTransferTx(tx, wallet.key)
		if err != nil {
			recordGameCoinWithdrawalAttempt(ctx, withdrawal, false, err)
			return
		}
		marked, err := model.MarkGameCoinWithdrawalSigned(withdrawal.Id, model.GameCoinSignedTx{
			FromAddress:       wallet.address,
			Nonce:             int64(nonce),
			TxHash:            txHash,
			RawTx:             rawTx,
			GasLimit:          int64(tx.gas),
			MaxFeePerGas:      fees.maxFee.String(),
			PriorityFeePerGas: fees.tip.String(),
		})
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

// suggestGameCoinFees 给出 EIP-1559 的小费与费用上限。上限取基础费的三倍加小费，之后连涨几个区块也不至于卡住；
// 实际只按打包时的基础费扣。涨得更多、签出的交易付不起时，由 replaceUnderpricedGameCoinTx 加价重签。
func suggestGameCoinFees(ctx context.Context, rpcURL string) (*gameCoinFees, error) {
	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := callEthereumRpc(ctx, rpcURL, "eth_getBlockByNumber", []any{"latest", false}, &block); err != nil {
		return nil, err
	}
	if block.BaseFeePerGas == "" {
		return nil, errors.New("latest block has no baseFeePerGas; the chain must support EIP-1559")
	}
	baseFee, err := parseEthereumQuantity(block.BaseFeePerGas)
	if err != nil {
		return nil, err
	}
	tip, err := callEthereumRpcQuantity(ctx, rpcURL, "eth_maxPriorityFeePerGas", nil)
	if err != nil || tip.Sign() <= 0 {
		tip = big.NewInt(gameCoinDefaultTipWei)
	}
	maxFee := new(big.Int).Mul(baseFee, big.NewInt(3))
	return &gameCoinFees{baseFee: baseFee, tip: tip, maxFee: maxFee.Add(maxFee, tip)}, nil
}

type rawTxOutcome int

const (
	rawTxAccepted  rawTxOutcome = iota // 节点收下了，或这笔交易早已在它的内存池里
	rawTxNonceUsed                     // 这个 nonce 已经上链，是不是这笔交易要看回执
	rawTxRetry                         // 没有结论，下一轮重发同一笔
)

// broadcastGameCoinTx 广播一笔已签名的交易，并按节点的报错归类。各家节点的措辞不同：Geth 说 "already known"、
// "nonce too low"，Nethermind 说 "AlreadyKnown"、"OldNonce"，OpenEthereum 说 "already imported"、
// "nonce is too low"，Besu 说 "Known transaction"。认不出来的一律当作没有结论。
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
		case strings.Contains(message, "already known"), strings.Contains(message, "known transaction"),
			strings.Contains(message, "alreadyknown"), strings.Contains(message, "already imported"):
			return rawTxAccepted, nil
		case strings.Contains(message, "nonce too low"), strings.Contains(message, "nonce is too low"),
			strings.Contains(message, "oldnonce"):
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

// failGameCoinWithdrawal 判定提现失败并退回游戏币。reason 是给用户看的固定中文句子，写进提现单与退回流水，前端按原文翻译
// (web/classic 的 getGameCoinWithdrawFailReasonMap)，改动或新增时两边一起改；detail 是给管理员排查的技术细节。
func failGameCoinWithdrawal(ctx context.Context, withdrawal *model.GameCoinWithdrawal, reason string, detail string, summary *GameCoinWithdrawRunSummary) {
	failed, err := model.FailGameCoinWithdrawal(withdrawal, reason, detail)
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

func setGameCoinWithdrawalNonceUsed(ctx context.Context, withdrawal *model.GameCoinWithdrawal, usedAt int64, checkedAt int64, watch *GameCoinNonceWatch) bool {
	if err := model.SetGameCoinWithdrawalNonceUsed(withdrawal.Id, usedAt, checkedAt); err != nil {
		logger.LogError(ctx, fmt.Sprintf("game coin withdraw: record nonce state for %s failed: %v", withdrawal.TradeNo, err))
		gameCoinNonceStateFailedAt.Store(common.GetTimestamp())
		if watch != nil {
			// 不能把没清零的旧计时连同其他单的成功观察交给下一节点。
			*watch = GameCoinNonceWatch{}
		}
		return false
	}
	return true
}
