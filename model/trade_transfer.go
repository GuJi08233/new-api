package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TransferQuotaToTrade 把主钱包的 quota 额度转进模拟盘。扣额度与入账在同一个事务里，余额不够时什么都不改。
func TransferQuotaToTrade(userId int, quota int) (*TradeAccount, error) {
	if quota < 1 || quota >= common.MaxQuota {
		return nil, ErrTradeAmountInvalid
	}
	var account *TradeAccount
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
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
// (QuotaPrincipal)的部分算盈利，当天累计不能超过 dailyProfitCap(额度单位，0 表示不限制)。
func TransferQuotaFromTrade(userId int, quota int, day string, dailyProfitCap int, market TradeFuturesMarket) (*TradeAccount, int, error) {
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

// TransferGameCoinsToTrade 将整数游戏币转入模拟盘，1 币 = 1 美元。两边余额、两边流水同时提交或回滚。
// 转入的币可以再转出成额度，所以和兑换主额度共用每人每天的兑换上限，按游戏中心记账日 day(GameCenterDay)计数，超过时返回
// ErrGameCoinConvertDailyLimit。
func TransferGameCoinsToTrade(userId int, coins int, day string) (*TradeAccount, error) {
	quota, err := GameCoinQuota(coins)
	if err != nil {
		return nil, err
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
		// 先锁游戏币账户再占用兑换额度，与兑换主额度的加锁顺序一致。额度要在写这笔流水之前占用：当天第一次占用时按已有流水
		// 补计，先写流水就会把这一笔算两次。
		if _, err = lockGameCoinAccountTx(tx, userId); err != nil {
			return err
		}
		if err = consumeGameCoinConvertDayTx(tx, userId, day, coins); err != nil {
			return err
		}
		account.Cash += quota
		account.QuotaPrincipal += quota
		account.CoinPrincipal += coins
		account.TotalIn += quota
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		entry := TradeLedger{Type: TradeLedgerCoinIn, Amount: quota}
		if err = tradeLedgerTx(tx, account, &entry); err != nil {
			return err
		}
		_, err = applyGameCoinDeltaTx(tx, userId, -coins, GameCoinLog{Type: GameCoinLogTypeTradeOut, Quota: quota, RefId: strconv.Itoa(entry.Id)})
		return err
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

// TransferGameCoinsFromTrade 将模拟盘资金转回整数游戏币，返回的盈利以额度单位计。
// 与额度转出共用可提现资金、本金及当日盈利上限，不能通过切换目标钱包重复支取盈利。只有从游戏币转入、还没转回的币
// (CoinPrincipal)能转回游戏币，超过时返回 ErrTradeCoinPrincipal：额度不能借道模拟盘换成游戏币。
func TransferGameCoinsFromTrade(userId int, coins int, day string, dailyProfitCap int, market TradeFuturesMarket) (*TradeAccount, int, error) {
	quota, err := GameCoinQuota(coins)
	if err != nil {
		return nil, 0, err
	}
	var account *TradeAccount
	profit := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if coins > account.CoinPrincipal {
			return ErrTradeCoinPrincipal
		}
		account.CoinPrincipal -= coins
		if profit, err = withdrawTradeCashTx(tx, account, quota, day, dailyProfitCap, market); err != nil {
			return err
		}
		entry := TradeLedger{Type: TradeLedgerCoinOut, Amount: -quota}
		if err = tradeLedgerTx(tx, account, &entry); err != nil {
			return err
		}
		_, err = applyGameCoinDeltaTx(tx, userId, coins, GameCoinLog{Type: GameCoinLogTypeTradeIn, Quota: quota, RefId: strconv.Itoa(entry.Id)})
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
	// 游戏币本金是本金的一部分：本金转出成额度后，剩下的本金不够折合的币数也不能再转回游戏币，否则先把本金转成额度、
	// 再转入额度，就能把额度换成游戏币。
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	if perUsd <= 0 {
		return 0, ErrTradeAmountInvalid
	}
	account.CoinPrincipal = min(account.CoinPrincipal, account.QuotaPrincipal/perUsd)
	account.TotalOut += quota
	if err = saveTradeAccountTx(tx, account); err != nil {
		return 0, err
	}
	return profit, nil
}
