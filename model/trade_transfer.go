package model

import (
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
		return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerQuotaIn, Amount: quota})
	})
	if err != nil {
		return nil, err
	}
	syncCreditUserQuotaCache(userId, -quota, "trade transfer in")
	return account, nil
}

// TransferQuotaFromTrade 把模拟盘的可用资金转出成主钱包额度，返回转出后的账户与这次转出里算作盈利的部分。持仓要先卖出、
// 挂单要先撤销才能转出；超过转入额度(QuotaPrincipal)的部分算盈利，当天累计不能超过 dailyProfitCap(额度单位，0 表示
// 不限制)。
func TransferQuotaFromTrade(userId int, quota int, day string, dailyProfitCap int) (*TradeAccount, int, error) {
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
		if quota > account.Cash {
			return ErrTradeWithdrawExceeded
		}
		principal := min(quota, account.QuotaPrincipal)
		profit = quota - principal
		if profit > 0 && dailyProfitCap > 0 {
			if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeProfitOutDay{UserId: userId, Day: day}).Error; err != nil {
				return err
			}
			result := tx.Model(&TradeProfitOutDay{}).
				Where("user_id = ? AND day = ? AND used <= ?", userId, day, dailyProfitCap-profit).
				Update("used", gorm.Expr("used + ?", profit))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTradeProfitOutLimit
			}
		}
		account.Cash -= quota
		account.QuotaPrincipal -= principal
		account.TotalOut += quota
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = creditTopUpQuota(tx, userId, quota, nil); err != nil {
			return err
		}
		return tradeLedgerTx(tx, account, TradeLedger{Type: TradeLedgerQuotaOut, Amount: -quota})
	})
	if err != nil {
		return nil, 0, err
	}
	syncCreditUserQuotaCache(userId, quota, "trade transfer out")
	return account, profit, nil
}
