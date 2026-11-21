package model

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TradeTransferInOptions 是转入模拟盘时的可选参数(是否同时领取任务解锁的 1:1 体验金加赠)。
type TradeTransferInOptions struct {
	ClaimMatch bool
	Ip         string
	Day        string
}

// checkTradeEquity 确认 equity 是加锁之后的这个账户的估值：估值不可用时返回 ErrTradeMarkUnavailable，估值之后账户的资金又变了时
// 返回 ErrTradeValuationStale。
func checkTradeEquity(account *TradeAccount, equity TradeEquity) error {
	if !equity.Known {
		return ErrTradeMarkUnavailable
	}
	seen := equity.Account
	if seen.AssetVersion != account.AssetVersion || seen.Cash != account.Cash || seen.Frozen != account.Frozen ||
		seen.QuotaPrincipal != account.QuotaPrincipal || seen.CoinPrincipal != account.CoinPrincipal || seen.BonusPrincipal != account.BonusPrincipal {
		return ErrTradeValuationStale
	}
	return nil
}

// settleTradeCoinPrincipal 在转入转出之前按扣除体验金后的自有总资产把亏损记到两种本金上。
// 亏损优先扣自有资金(体验金作抗爆仓垫子)，再按抵扣顺序决定先扣游戏币本金还是平台额度本金。
func settleTradeCoinPrincipal(account *TradeAccount, equity TradeEquity) error {
	coins := account.CoinPrincipal
	if coins <= 0 {
		return nil
	}
	if err := checkTradeEquity(account, equity); err != nil {
		return err
	}
	ownValue := GameCoins(max(equity.Value-account.BonusPrincipal, 0))
	claim := min(coins, ownValue)
	if account.CoinFirst() {
		claim = min(coins, max(ownValue-(GameCoins(account.QuotaPrincipal)-coins), 0))
	}
	if claim >= coins {
		return nil
	}
	account.QuotaPrincipal -= int(coins - claim)
	account.CoinPrincipal = claim
	return nil
}

// TradeCoinReturnable 返回按总资产 equity 与账户的抵扣顺序结算之后，游戏币本金还剩多少：这是现在最多能转回游戏币的数量，
// 实际转回还受可转出的资金(扣除未回收体验金后)限制。只用于展示，不改动账户。
func TradeCoinReturnable(account TradeAccount, equity int) GameCoins {
	if err := settleTradeCoinPrincipal(&account, TradeEquity{Value: equity, Known: true, Account: account}); err != nil {
		return 0
	}
	return account.CoinPrincipal
}

// TradeQuotaWithdrawable 返回当前最多可转出到平台额度的数量(额度单位)，用于前端展示：
// - 持有体验金时必须先扣除体验金本金；
// - 游戏币本金只能退回游戏币，不能转成平台额度；
// - 超过全部自有本金的部分算作盈利，受对应盈利上限约束。
func TradeQuotaWithdrawable(account TradeAccount, crossWithdrawable int, equity int, profitUsed int, profitCap int, firstProfitCap int) int {
	_ = settleTradeCoinPrincipal(&account, TradeEquity{Value: equity, Known: true, Account: account})
	available := max(0, crossWithdrawable-account.BonusPrincipal)
	if account.BonusPrincipal > 0 && account.BonusMode == TradeBonusModeFirst && account.QuotaPrincipal == 0 {
		limit := available
		if firstProfitCap > 0 {
			limit = min(limit, firstProfitCap)
		}
		if profitCap > 0 {
			limit = min(limit, max(profitCap-profitUsed, 0))
		}
		return max(0, limit)
	}
	quotaOnlyPrincipal := max(0, account.QuotaPrincipal-int(account.CoinPrincipal))
	realizedProfit := max(0, available-account.QuotaPrincipal)
	if profitCap > 0 {
		realizedProfit = min(realizedProfit, max(profitCap-profitUsed, 0))
	}
	return max(0, min(available, quotaOnlyPrincipal+realizedProfit))
}

// TradeCoinWithdrawable 返回当前最多可转回游戏币的数量(已扣除体验金本金与全仓占用)。
func TradeCoinWithdrawable(account TradeAccount, crossWithdrawable int, equity int) GameCoins {
	returnable := TradeCoinReturnable(account, equity)
	available := GameCoins(max(0, crossWithdrawable-account.BonusPrincipal))
	return min(returnable, available)
}

// SetTradeLossOrder 设置用户的亏损抵扣顺序(TradeLossCoinFirst 或 TradeLossQuotaFirst)，账户还不存在时先建出来。
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

// TransferQuotaToTrade 把主钱包的 quota 额度转进模拟盘。
func TransferQuotaToTrade(userId int, quota int, equity TradeEquity) (*TradeAccount, error) {
	account, _, err := TransferQuotaToTradeWithOptions(userId, quota, equity, TradeTransferInOptions{})
	return account, err
}

// TransferQuotaToTradeWithOptions 把主钱包的 quota 额度转进模拟盘，支持同时触发任务解锁的 1:1 体验金加赠。
// 持有未结清体验金时不允许混入新的自有资金，须先提盈回收或放弃体验金。
func TransferQuotaToTradeWithOptions(userId int, quota int, equity TradeEquity, opts TradeTransferInOptions) (*TradeAccount, int, error) {
	if quota < 1 || quota >= common.MaxQuota {
		return nil, 0, ErrTradeAmountInvalid
	}
	var account *TradeAccount
	bonusGranted := 0
	now := time.Now()
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if account.BonusPrincipal > 0 {
			return ErrTradeBonusActiveExists
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
		if opts.ClaimMatch {
			day := opts.Day
			if day == "" {
				day = now.Format("2006-01-02")
			}
			bonusGranted, err = applyTradeMatchBonusTx(tx, account, quota, opts.Ip, day, now)
			if err != nil {
				return err
			}
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerQuotaIn, Amount: quota}); err != nil {
			return err
		}
		if bonusGranted > 0 {
			return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusIn, Amount: bonusGranted})
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	syncCreditUserQuotaCache(userId, -quota, "trade transfer in")
	return account, bonusGranted, nil
}

// TransferQuotaFromTrade 把模拟盘的可用资金转出成主钱包额度，返回转出后的账户与这次转出里算作盈利的部分。
// 游戏币本金(CoinPrincipal)不能转出成主额度，必须先退回游戏币钱包；仅平台额度本金与超出总本金的已实现盈利可转出成主额度。
// 若账户持有体验金，必须先平仓、撤单并结清借款，本次转出同时立即回收剩余体验金。
func TransferQuotaFromTrade(userId int, quota int, day string, dailyProfitCap int, market TradeFuturesMarket, equity TradeEquity) (*TradeAccount, int, error) {
	if quota < 1 || quota >= common.MaxQuota {
		return nil, 0, ErrTradeAmountInvalid
	}
	setting := operation_setting.GetTradeSetting()
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return nil, 0, err
	}
	firstProfitCap := 0
	if setting.BonusFirstProfitCapUsd > 0 {
		firstProfitCap, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusFirstProfitCapUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
		if err != nil {
			return nil, 0, ErrTradeAmountInvalid
		}
	}
	var account *TradeAccount
	profit := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		reclaimed := 0
		isFirstBonus := account.BonusPrincipal > 0 && account.BonusMode == TradeBonusModeFirst && account.QuotaPrincipal == 0
		if account.BonusPrincipal > 0 {
			hasExposure, err := hasTradeOpenExposureTx(tx, userId)
			if err != nil {
				return err
			}
			if hasExposure {
				return ErrTradeBonusClosePositionsFirst
			}
			netCash := max(0, account.Cash-account.BonusPrincipal)
			if quota > netCash {
				return ErrTradeWithdrawExceeded
			}
			if isFirstBonus && firstProfitCap > 0 && quota > firstProfitCap {
				return ErrTradeBonusProfitCap
			}
			reclaimed = min(account.BonusPrincipal, max(account.Cash, 0))
			if isFirstBonus {
				// 新手首次体验金一旦提盈，未提走的剩余体验金与超额浮盈一并清零回收，防止分多次绕过首次上限。
				reclaimed = max(account.Cash-quota, 0)
			}
			account.Cash -= reclaimed
			account.BonusPrincipal = 0
			account.BonusMode = ""
		}
		quotaOnlyPrincipal := max(0, account.QuotaPrincipal-int(account.CoinPrincipal))
		if profit, err = withdrawTradeQuotaTx(tx, account, quota, quotaOnlyPrincipal, day, dailyProfitCap, market); err != nil {
			return err
		}
		if err = creditTopUpQuota(tx, userId, quota, nil); err != nil {
			return err
		}
		if reclaimed > 0 {
			if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusReclaim, Amount: -reclaimed}); err != nil {
				return err
			}
		}
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerQuotaOut, Amount: -quota})
	})
	if err != nil {
		return nil, 0, err
	}
	syncCreditUserQuotaCache(userId, quota, "trade transfer out")
	return account, profit, nil
}

// TransferGameCoinsToTrade 将游戏币转入模拟盘(支持小数，不再占用游戏中心每日兑换额度)。
func TransferGameCoinsToTrade(userId int, amount GameCoins, equity TradeEquity) (*TradeAccount, error) {
	account, _, err := TransferGameCoinsToTradeWithOptions(userId, amount, equity, TradeTransferInOptions{})
	return account, err
}

// TransferGameCoinsToTradeWithOptions 将游戏币转入模拟盘(支持小数)，并可选同时领取任务解锁的 1:1 体验金加赠。
func TransferGameCoinsToTradeWithOptions(userId int, amount GameCoins, equity TradeEquity, opts TradeTransferInOptions) (*TradeAccount, int, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return nil, 0, ErrGameCoinAmountInvalid
	}
	var account *TradeAccount
	bonusGranted := 0
	now := time.Now()
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if account, err = lockTradeAccountTx(tx, userId); err != nil {
			return err
		}
		if account.BonusPrincipal > 0 {
			return ErrTradeBonusActiveExists
		}
		if err = checkTradeEquity(account, equity); err != nil {
			return err
		}
		if err = settleTradeCoinPrincipal(account, equity); err != nil {
			return err
		}
		// 没有游戏币本金时的亏损都是平台额度的。转入之前把已经亏掉的额度本金去掉，新转入的币才不会替它买单。
		account.QuotaPrincipal = min(account.QuotaPrincipal, max(equity.Value, 0))
		if _, err = lockGameCoinAccountTx(tx, userId); err != nil {
			return err
		}
		account.Cash += int(amount)
		account.QuotaPrincipal += int(amount)
		account.CoinPrincipal += amount
		account.TotalIn += int(amount)
		if opts.ClaimMatch {
			day := opts.Day
			if day == "" {
				day = now.Format("2006-01-02")
			}
			bonusGranted, err = applyTradeMatchBonusTx(tx, account, int(amount), opts.Ip, day, now)
			if err != nil {
				return err
			}
		}
		if err = saveTradeAccountTx(tx, account); err != nil {
			return err
		}
		entry := TradeLedger{Type: TradeLedgerCoinIn, Amount: int(amount)}
		if err = tradeLedgerTx(tx, account, &entry); err != nil {
			return err
		}
		if bonusGranted > 0 {
			if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusIn, Amount: bonusGranted}); err != nil {
				return err
			}
		}
		_, err = applyGameCoinDeltaTx(tx, userId, -amount, GameCoinLog{Type: GameCoinLogTypeTradeOut, Quota: int(amount), RefId: strconv.Itoa(entry.Id)})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return account, bonusGranted, nil
}

// TransferGameCoinsFromTrade 将模拟盘里的游戏币本金退回游戏币钱包(支持小数)。
// 只有按抵扣顺序结算之后还剩的游戏币本金(CoinPrincipal)能转回游戏币，盈利与平台额度本金都不能转回游戏币。
// 持有体验金时必须先平仓、撤单并结清借款，本次转出同时回收剩余体验金。
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
		reclaimed := 0
		if account.BonusPrincipal > 0 {
			hasExposure, err := hasTradeOpenExposureTx(tx, userId)
			if err != nil {
				return err
			}
			if hasExposure {
				return ErrTradeBonusClosePositionsFirst
			}
			netCash := max(0, account.Cash-account.BonusPrincipal)
			if int(amount) > netCash {
				return ErrTradeWithdrawExceeded
			}
			reclaimed = min(account.BonusPrincipal, max(account.Cash, 0))
			account.Cash -= reclaimed
			account.BonusPrincipal = 0
			account.BonusMode = ""
		}
		account.CoinPrincipal -= amount
		if profit, err = withdrawTradeCashTx(tx, account, int(amount), day, dailyProfitCap, market); err != nil {
			return err
		}
		if reclaimed > 0 {
			if err = tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusReclaim, Amount: -reclaimed}); err != nil {
				return err
			}
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

// withdrawTradeQuotaTx 处理转出到平台额度：不允许动用游戏币本金，只能转出平台额度本金与超出总本金的已实现盈利。
func withdrawTradeQuotaTx(tx *gorm.DB, account *TradeAccount, quota int, quotaOnlyPrincipal int, day string, dailyProfitCap int, market TradeFuturesMarket) (int, error) {
	state, err := tradeCrossStateTx(tx, account.UserId, market)
	if err != nil {
		return 0, err
	}
	spotReserve, err := tradeSpotTransferReserveTx(tx, account, market)
	if err != nil {
		return 0, err
	}
	withdrawable := max(0, state.Withdrawable(account.Cash)-spotReserve)
	if quota > withdrawable {
		return 0, ErrTradeWithdrawExceeded
	}
	realizedProfit := max(0, withdrawable-account.QuotaPrincipal)
	maxQuotaOut := quotaOnlyPrincipal + realizedProfit
	if quota > maxQuotaOut {
		return 0, ErrTradeQuotaOutExceedsPrincipal
	}
	principal := min(quota, quotaOnlyPrincipal)
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
	account.CoinPrincipal = min(account.CoinPrincipal, GameCoins(account.QuotaPrincipal))
	account.TotalOut += quota
	if err = saveTradeAccountTx(tx, account); err != nil {
		return 0, err
	}
	return profit, nil
}

// withdrawTradeCashTx 处理退回游戏币本金时的可用资金检查与账户更新；调用前必须锁住模拟盘账户。
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
	account.CoinPrincipal = min(account.CoinPrincipal, GameCoins(account.QuotaPrincipal))
	account.TotalOut += quota
	if err = saveTradeAccountTx(tx, account); err != nil {
		return 0, err
	}
	return profit, nil
}

