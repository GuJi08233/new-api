package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// checkTradeEquity 确认 equity 是加锁之后的这个账户的估值：估值不可用时返回 ErrTradeMarkUnavailable，估值之后账户的资金又变了时
// 返回 ErrTradeValuationStale。
func checkTradeEquity(account *TradeAccount, equity TradeEquity) error {
	if !equity.Known {
		return ErrTradeMarkUnavailable
	}
	seen := equity.Account
	if seen.AssetVersion != account.AssetVersion || seen.Cash != account.Cash || seen.Frozen != account.Frozen || seen.QuotaPrincipal != account.QuotaPrincipal || seen.CoinPrincipal != account.CoinPrincipal {
		return ErrTradeValuationStale
	}
	return nil
}

// settleTradeCoinPrincipal 在转入转出之前按账户总资产把亏损记到两种本金上。总资产先付给排在前面的本金：先扣游戏币(coin_first)时
// 平台额度那部分本金排在前面，亏损先由游戏币本金承担；先扣平台额度(quota_first)时反过来。游戏币本金只会因此变少，变少的部分
// 从本金里一起去掉：之后再转入的钱不会替之前亏掉的游戏币买单，否则亏掉的币可以先转入额度补上、再转回成游戏币。没有游戏币本金的
// 账户不受影响，只有额度的用户的盈利计算与以前一样。
func settleTradeCoinPrincipal(account *TradeAccount, equity TradeEquity) error {
	coins := account.CoinPrincipal
	if coins <= 0 {
		return nil
	}
	if err := checkTradeEquity(account, equity); err != nil {
		return err
	}
	value := GameCoins(max(equity.Value, 0))
	claim := min(coins, value)
	if account.CoinFirst() {
		claim = min(coins, max(value-(GameCoins(account.QuotaPrincipal)-coins), 0))
	}
	if claim >= coins {
		return nil
	}
	account.QuotaPrincipal -= int(coins - claim)
	account.CoinPrincipal = claim
	return nil
}

// TradeCoinReturnable 返回按总资产 equity 与账户的抵扣顺序结算之后，游戏币本金还剩多少：这是现在最多能转回游戏币的数量，
// 实际转回还受可转出的资金限制。只用于展示，不改动账户。
func TradeCoinReturnable(account TradeAccount, equity int) GameCoins {
	if err := settleTradeCoinPrincipal(&account, TradeEquity{Value: equity, Known: true, Account: account}); err != nil {
		return 0
	}
	return account.CoinPrincipal
}

// SetTradeLossOrder 设置用户的亏损抵扣顺序(TradeLossCoinFirst 或 TradeLossQuotaFirst)，账户还不存在时先建出来。下一次转账与穿仓补亏空
// 都按新的顺序进行；已经结算过的游戏币本金不会因为改了顺序而回来。
func SetTradeLossOrder(userId int, order string) (*TradeAccount, error) {
	if !ValidTradeLossOrder(order) {
		return nil, ErrTradeAmountInvalid
	}
	var account *TradeAccount
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		account.LossOrder = order
		return saveTradeAccountTx(tx, account)
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

// TransferQuotaToTrade 把主钱包的 quota 额度转进模拟盘。扣额度与入账在同一个事务里，余额不够时什么都不改。equity 是转账之前的
// 账户总资产，先按抵扣顺序结算游戏币本金(见 settleTradeCoinPrincipal)。
func TransferQuotaToTrade(userId int, quota int, equity TradeEquity) (*TradeAccount, error) {
	if quota < 1 || quota >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	var account *TradeAccount
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		result := tx.Model(&User{}).Where("id = ? AND quota >= ?", userId, quota).Update("quota", gorm.Expr("quota - ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTradeQuotaInsufficient
		}
		account.Cash += quota
		account.QuotaPrincipal += quota
		account.TotalIn += quota
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerQuotaIn, Amount: quota})
	})
	if err != nil {
		return nil, err
	}
	syncCreditUserQuotaCache(userId, -quota, "trade transfer in")
	return account, nil
}

// TransferQuotaFromTrade 把模拟盘的可用资金转出成主钱包额度，返回转出后的账户与这次转出里算作盈利的部分。持仓要先卖出、
// 挂单要先撤销才能转出；有全仓合约仓位时只能转出它们占用之外的资金，浮动盈利不算(market 提供标记价格)。超过转入额度
// (QuotaPrincipal)的部分算盈利，当天累计不能超过 dailyProfitCap(额度单位，0 表示不限制)。equity 的含义见 TransferQuotaToTrade。
func TransferQuotaFromTrade(userId int, quota int, day string, dailyProfitCap int, market TradeFuturesMarket, equity TradeEquity) (*TradeAccount, int, error) {
	if quota < 1 || quota >= common.MaxQuota {
		return nil, 0, ErrTradeAmountInvalid
	}
	var account *TradeAccount
	profit := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		if profit, err = withdrawTradeCashTx(tx, account, quota, day, dailyProfitCap, market); err != nil {
			return err
		}
		if err = creditTopUpQuota(tx, userId, quota, nil); err != nil {
			return err
		}
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerQuotaOut, Amount: -quota})
	})
	if err != nil {
		return nil, 0, err
	}
	syncCreditUserQuotaCache(userId, quota, "trade transfer out")
	return account, profit, nil
}

// TransferGameCoinsToTrade 将 coins 个整币转入模拟盘，1 币 = 1 美元。两边余额、两边流水同时提交或回滚。
// 转入的币可以再转出成额度，所以和兑换主额度共用每人每天的兑换上限，按游戏中心记账日 day(GameCenterDay)计数，超过时返回
// ErrGameCoinConvertDailyLimit。equity 的含义见 TransferQuotaToTrade，转入游戏币时总要用到它。
func TransferGameCoinsToTrade(userId int, coins int, day string, equity TradeEquity) (*TradeAccount, error) {
	amount, err := WholeGameCoins(coins)
	if err != nil || coins < 1 {
		return nil, ErrGameCoinAmountInvalid
	}
	if coins > GameCoinConvertDailyLimit {
		return nil, ErrGameCoinConvertDailyLimit
	}
	var account *TradeAccount
	err = DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if err = checkTradeEquity(account, equity); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		// 没有游戏币本金时的亏损都是平台额度的。转入之前把已经亏掉的额度本金去掉，新转入的币才不会替它买单(先扣游戏币时额度本金
		// 排在前面)；这部分亏损之后再赚回来时算作盈利。结算之后游戏币本金不超过总资产，本金仍不少于游戏币本金。
		account.QuotaPrincipal = min(account.QuotaPrincipal, max(equity.Value, 0))
		// 先锁游戏币账户再占用兑换额度，与兑换主额度的加锁顺序一致。额度要在写这笔流水之前占用：当天第一次占用时按已有流水
		// 补计，先写流水就会把这一笔算两次。
		if _, err = lockGameCoinAccountTx(tx, userId); err != nil {
			return err
		}
		if err = consumeGameCoinConvertDayTx(tx, userId, day, amount); err != nil {
			return err
		}
		account.Cash += int(amount)
		account.QuotaPrincipal += int(amount)
		account.CoinPrincipal += amount
		account.TotalIn += int(amount)
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		entry := TradeLedger{Type: TradeLedgerCoinIn, Amount: int(amount)}
		if err = tradeLedgerTx(tx, account, &entry); err != nil {
			return err
		}
		_, err = applyGameCoinDeltaTx(tx, userId, -amount, GameCoinLog{Type: GameCoinLogTypeTradeOut, Quota: int(amount), RefId: strconv.Itoa(entry.Id)})
		return err
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

// TransferGameCoinsFromTrade 将模拟盘资金转回游戏币，amount 可以有小数，返回的盈利以额度单位计。
// 与额度转出共用可提现资金、本金及当日盈利上限，不能通过切换目标钱包重复支取盈利。只有按抵扣顺序结算之后还剩的游戏币本金
// (CoinPrincipal)能转回游戏币，超过时返回 ErrTradeCoinPrincipal：额度不能借道模拟盘换成游戏币。equity 的含义见 TransferQuotaToTrade。
func TransferGameCoinsFromTrade(userId int, amount GameCoins, day string, dailyProfitCap int, market TradeFuturesMarket, equity TradeEquity) (*TradeAccount, int, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return nil, 0, ErrGameCoinAmountInvalid
	}
	var account *TradeAccount
	profit := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		if amount > account.CoinPrincipal {
			return ErrTradeCoinPrincipal
		}
		account.CoinPrincipal -= amount
		if profit, err = withdrawTradeCashTx(tx, account, int(amount), day, dailyProfitCap, market); err != nil {
			return err
		}
		entry := TradeLedger{Type: TradeLedgerCoinOut, Amount: -int(amount)}
		if err = tradeLedgerTx(tx, account, &entry); err != nil {
			return err
		}
		_, err = applyGameCoinDeltaTx(tx, userId, amount, GameCoinLog{Type: GameCoinLogTypeTradeIn, Quota: int(amount), RefId: strconv.Itoa(entry.Id)})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return account, profit, nil
}

// withdrawTradeCashTx 对所有目标钱包使用同一提现规则；调用前必须锁住模拟盘账户。
func withdrawTradeCashTx(tx *gorm.DB, account *TradeAccount, quota int, day string, dailyProfitCap int, market TradeFuturesMarket) (int, error) {
	state, err := tradeCrossStateTx(tx, account.UserId, market)
	if err != nil {
		return 0, err
	}
	spotReserve, err := tradeSpotTransferReserveTx(tx, account, market)
	if err != nil {
		return 0, err
	}
	if quota > max(0, state.Withdrawable(account.Cash)-spotReserve) {
		return 0, ErrTradeWithdrawExceeded
	}
	principal := min(quota, account.QuotaPrincipal)
	profit := quota - principal
	if profit > 0 && dailyProfitCap > 0 {
		if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeProfitOutDay{UserId: account.UserId, Day: day}).Error; err != nil {
			return 0, err
		}
		result := tx.Model(&TradeProfitOutDay{}).
			Where("user_id = ? AND day = ? AND used <= ?", account.UserId, day, dailyProfitCap-profit).
			Update("used", gorm.Expr("used + ?", profit))
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected != 1 {
			return 0, ErrTradeProfitOutLimit
		}
	}
	account.Cash -= quota
	account.QuotaPrincipal -= principal
	// 游戏币本金是本金的一部分：本金转出成额度后，剩下的本金不够时能转回游戏币的也随之减少，否则先把本金转成额度、再转入额度，
	// 就能把额度换成游戏币。
	account.CoinPrincipal = min(account.CoinPrincipal, GameCoins(account.QuotaPrincipal))
	account.TotalOut += quota
	if err = saveTradeAccountTx(tx, account); err != nil {
		return 0, err
	}
	return profit, nil
}
