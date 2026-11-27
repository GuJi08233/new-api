package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TradeBonusModeFirst   = "first"   // 新手首次破冰体验金
	TradeBonusModeMatch   = "match"   // 任务解锁后的转入 1:1 加赠体验金
	TradeBonusModeRanking = "ranking" // 上期排行榜达标体验金

	TradeBonusPeriodOnce = "once"

	TradeLedgerBonusIn      = "bonus_in"
	TradeLedgerBonusReclaim = "bonus_reclaim"
	TradeLedgerBonusZero    = "bonus_zero"
)

var (
	ErrTradeBonusDisabled            = errors.New("trade bonus is disabled")
	ErrTradeBonusPoolExhausted       = errors.New("trade bonus daily pool is exhausted")
	ErrTradeBonusAlreadyClaimed      = errors.New("trade bonus already claimed for this period")
	ErrTradeBonusIpLimit             = errors.New("trade bonus ip limit reached")
	ErrTradeBonusAccountNotEmpty     = errors.New("trade account must be empty to claim first bonus")
	ErrTradeBonusInactiveUser        = errors.New("recent API usage is required to claim first bonus")
	ErrTradeBonusTaskIncomplete      = errors.New("game wager task not completed for bonus match")
	ErrTradeBonusActiveExists        = errors.New("active trade bonus must be cleared before this action")
	ErrTradeBonusClosePositionsFirst = errors.New("close all positions, orders, and loans before reclaiming trade bonus")
	ErrTradeBonusProfitCap           = errors.New("first trade bonus profit withdrawal limit exceeded")
	ErrTradeBonusNotRanked           = errors.New("user did not qualify for ranking bonus in previous period")
	ErrTradeQuotaOutExceedsPrincipal = errors.New("game coin principal can only be withdrawn back to game coins")
)

// TradeBonusPoolDay 记录全站每天已经发放的模拟盘体验金总额(额度单位)，先到先得。
type TradeBonusPoolDay struct {
	Day  string `json:"day" gorm:"type:varchar(10);primaryKey"`
	Used int    `json:"used" gorm:"type:bigint;not null;default:0"`
}

// TradeBonusClaim 记录每个用户的体验金领取记录，通过 (user_id, kind, period) 唯一约束防止并发重领。
type TradeBonusClaim struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex:idx_trade_bonus_claim_unique,priority:1"`
	Kind      string `json:"kind" gorm:"type:varchar(16);uniqueIndex:idx_trade_bonus_claim_unique,priority:2"`
	Period    string `json:"period" gorm:"type:varchar(16);uniqueIndex:idx_trade_bonus_claim_unique,priority:3"`
	Amount    int    `json:"amount" gorm:"type:bigint;not null;default:0"`
	Ip        string `json:"ip" gorm:"type:varchar(64);index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

// TradeBonusIpClaim 记录同 IP 在周期内领取体验金的次数。
type TradeBonusIpClaim struct {
	Ip     string `json:"ip" gorm:"type:varchar(64);primaryKey"`
	Kind   string `json:"kind" gorm:"type:varchar(16);primaryKey"`
	Period string `json:"period" gorm:"type:varchar(16);primaryKey"`
	Used   int    `json:"used" gorm:"type:bigint;not null;default:0"`
}

// TradeBonusPeriodKey 返回指定周期模式对应的周期标识(daily 为 YYYY-MM-DD，weekly 为 YYYY-Www)。
func TradeBonusPeriodKey(now time.Time, period string) string {
	if period == operation_setting.TradeBonusPeriodDaily {
		return now.Format("2006-01-02")
	}
	year, week := now.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// TradeBonusPeriodStart 返回当前周期的起始时间戳(本地时间 00:00:00)。
func TradeBonusPeriodStart(now time.Time, period string) int64 {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if period == operation_setting.TradeBonusPeriodDaily {
		return startOfDay.Unix()
	}
	offset := (int(now.Weekday()) + 6) % 7 // Monday = 0
	return startOfDay.AddDate(0, 0, -offset).Unix()
}

// GetTradeBonusPoolDayUsed 返回当日全站已发放的体验金总额(额度单位)。
func GetTradeBonusPoolDayUsed(day string) (int, error) {
	var used int
	err := DB.Model(&TradeBonusPoolDay{}).Where("day = ?", day).Select("used").Scan(&used).Error
	return used, err
}

// HasTradeBonusClaimed 判断用户在指定 kind + period 下是否已领取过体验金。
func HasTradeBonusClaimed(userId int, kind string, period string) (bool, error) {
	var count int64
	err := DB.Model(&TradeBonusClaim{}).Where("user_id = ? AND kind = ? AND period = ?", userId, kind, period).Count(&count).Error
	return count > 0, err
}

// CheckUserRecentApiActivity 校验用户是否满足近期 API 调用活跃证明：
// 当 activeDays > 0 时要求近 activeDays 天存在至少一条消费日志；当 minUsedUsd > 0 时要求账号累计已消费额(或近期消费额)不低于阈值。
func CheckUserRecentApiActivity(userId int, activeDays int, minUsedUsd float64) (bool, error) {
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return false, err
	}
	minQuota := 0
	if minUsedUsd > 0 {
		minQuota, err = common.QuotaFromDecimalStrict(decimal.NewFromFloat(minUsedUsd).Mul(decimal.NewFromInt(int64(perUsd))).Ceil())
		if err != nil {
			return false, err
		}
	}
	if activeDays <= 0 && minQuota <= 0 {
		return true, nil
	}
	if activeDays > 0 {
		since := time.Now().AddDate(0, 0, -activeDays).Unix()
		logDB := LOG_DB
		if logDB == nil {
			logDB = DB
		}
		var recentCount int64
		if err := logDB.Model(&Log{}).
			Where("user_id = ? AND type = ? AND created_at >= ?", userId, LogTypeConsume, since).
			Limit(1).Count(&recentCount).Error; err != nil {
			return false, err
		}
		if recentCount == 0 {
			return false, nil
		}
	}
	if minQuota > 0 {
		var usedQuota int
		if err := DB.Model(&User{}).Where("id = ?", userId).Select("COALESCE(used_quota, 0)").Scan(&usedQuota).Error; err != nil {
			return false, err
		}
		if usedQuota < minQuota {
			return false, nil
		}
	}
	return true, nil
}

// hasTradeOpenExposureTx 检查用户是否存在任何未平仓位、未撤委托、现货借款/借币或未结预测。
func hasTradeOpenExposureTx(tx *gorm.DB, userId int) (bool, error) {
	if tx.Migrator().HasTable(&TradePosition{}) {
		var count int64
		if err := tx.Model(&TradePosition{}).Where("user_id = ? AND qty > 0", userId).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeOrder{}) {
		var count int64
		if err := tx.Model(&TradeOrder{}).Where("user_id = ? AND status = ?", userId, TradeOrderStatusOpen).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeFuturesPosition{}) {
		var count int64
		if err := tx.Model(&TradeFuturesPosition{}).Where("user_id = ? AND qty > 0", userId).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeFuturesOrder{}) {
		var count int64
		if err := tx.Model(&TradeFuturesOrder{}).Where("user_id = ? AND status = ?", userId, TradeOrderStatusOpen).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeSpotMargin{}) {
		var count int64
		if err := tx.Model(&TradeSpotMargin{}).Where("user_id = ? AND (principal > 0 OR interest > 0)", userId).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeSpotAssetLoan{}) {
		var count int64
		if err := tx.Model(&TradeSpotAssetLoan{}).Where("user_id = ? AND (principal > 0 OR interest > 0)", userId).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradePredictionPosition{}) {
		var count int64
		if err := tx.Model(&TradePredictionPosition{}).Where("user_id = ? AND status = ?", userId, TradePredictionActive).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradePredictionSeriesPosition{}) {
		var count int64
		if err := tx.Model(&TradePredictionSeriesPosition{}).Where("user_id = ? AND status = ?", userId, TradePredictionActive).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeShortOrder{}) {
		var count int64
		if err := tx.Model(&TradeShortOrder{}).Where("user_id = ? AND status = ?", userId, TradeShortStatusActive).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	if tx.Migrator().HasTable(&TradeSprintEntry{}) {
		var count int64
		if err := tx.Model(&TradeSprintEntry{}).Where("user_id = ? AND status <> ?", userId, TradeSprintEntrySettled).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func isTradeAccountEmptyTx(tx *gorm.DB, account *TradeAccount) (bool, error) {
	if account.Cash != 0 || account.Frozen != 0 || account.BonusPrincipal != 0 {
		return false, nil
	}
	hasExposure, err := hasTradeOpenExposureTx(tx, account.UserId)
	return !hasExposure, err
}

func consumeTradeBonusPoolTx(tx *gorm.DB, day string, amount int, poolCap int) error {
	if poolCap <= 0 {
		return nil
	}
	if amount > poolCap {
		return ErrTradeBonusPoolExhausted
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeBonusPoolDay{Day: day}).Error; err != nil {
		return err
	}
	result := tx.Model(&TradeBonusPoolDay{}).
		Where("day = ? AND used <= ?", day, poolCap-amount).
		Update("used", gorm.Expr("used + ?", amount))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTradeBonusPoolExhausted
	}
	return nil
}

func consumeTradeBonusIpTx(tx *gorm.DB, ip string, kind string, period string, limit int) error {
	if ip == "" || limit <= 0 {
		return nil
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TradeBonusIpClaim{Ip: ip, Kind: kind, Period: period}).Error; err != nil {
		return err
	}
	result := tx.Model(&TradeBonusIpClaim{}).
		Where("ip = ? AND kind = ? AND period = ? AND used < ?", ip, kind, period, limit).
		Update("used", gorm.Expr("used + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTradeBonusIpLimit
	}
	return nil
}

// ClaimTradeFirstBonus 领取新手首次模拟盘体验金(默认 100 USDT)。需要账号空仓、全生命周期未领过、同 IP 限 1 次且全局体验金池未耗尽。
// 近期 API 活跃与 Turnstile 校验由 controller 层在调用前完成。
func ClaimTradeFirstBonus(userId int, ip string, day string) (*TradeAccount, int, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.BonusEnabled || setting.BonusFirstAmountUsd <= 0 {
		return nil, 0, ErrTradeBonusDisabled
	}
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return nil, 0, err
	}
	bonusQuota, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusFirstAmountUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil || bonusQuota < 1 {
		return nil, 0, ErrTradeAmountInvalid
	}
	poolCap := 0
	if setting.BonusPoolDailyUsd > 0 {
		poolCap, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusPoolDailyUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
		if err != nil {
			return nil, 0, ErrTradeAmountInvalid
		}
	}
	var account *TradeAccount
	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		if account, txErr = lockTradeAccountTx(tx, userId); txErr != nil {
			return txErr
		}
		empty, txErr := isTradeAccountEmptyTx(tx, account)
		if txErr != nil {
			return txErr
		}
		if !empty {
			return ErrTradeBonusAccountNotEmpty
		}
		claim := TradeBonusClaim{
			UserId:    userId,
			Kind:      TradeBonusModeFirst,
			Period:    TradeBonusPeriodOnce,
			Amount:    bonusQuota,
			Ip:        ip,
			CreatedAt: now,
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrTradeBonusAlreadyClaimed
		}
		if txErr = consumeTradeBonusIpTx(tx, ip, TradeBonusModeFirst, TradeBonusPeriodOnce, 1); txErr != nil {
			return txErr
		}
		if txErr = consumeTradeBonusPoolTx(tx, day, bonusQuota, poolCap); txErr != nil {
			return txErr
		}
		account.Cash += bonusQuota
		account.QuotaPrincipal = 0
		account.CoinPrincipal = 0
		account.BonusPrincipal = bonusQuota
		account.BonusMode = TradeBonusModeFirst
		if txErr = saveTradeAccountTx(tx, account); txErr != nil {
			return txErr
		}
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusIn, Amount: bonusQuota})
	})
	if err != nil {
		return nil, 0, err
	}
	return account, bonusQuota, nil
}

// ForfeitTradeBonus 主动放弃当前体验金：必须先平仓、撤单并结清借款。
// 对于新手纯体验金账户，会一并清空体验金余额(防止通过放弃体验金绕过新手盈利上限)；有盈利请先通过转出盈利兑现。
// 对于 1:1 加赠账户，只回收体验金本金部分，自有资金保留。
func ForfeitTradeBonus(userId int) (*TradeAccount, error) {
	var account *TradeAccount
	err := DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		if account, txErr = lockTradeAccountTx(tx, userId); txErr != nil {
			return txErr
		}
		if account.BonusPrincipal <= 0 {
			return nil
		}
		hasExposure, txErr := hasTradeOpenExposureTx(tx, userId)
		if txErr != nil {
			return txErr
		}
		if hasExposure {
			return ErrTradeBonusClosePositionsFirst
		}
		reclaim := min(account.BonusPrincipal, max(account.Cash, 0))
		if account.BonusMode == TradeBonusModeFirst && account.QuotaPrincipal == 0 {
			reclaim = max(account.Cash, 0)
		}
		account.Cash -= reclaim
		account.BonusPrincipal = 0
		account.BonusMode = ""
		if txErr = saveTradeAccountTx(tx, account); txErr != nil {
			return txErr
		}
		if reclaim > 0 {
			return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusReclaim, Amount: -reclaim})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

// applyTradeMatchBonusTx 在用户向模拟盘转入自有资金时，按 1:1 配赠体验金(需完成游戏中心有效下注任务、每周期限 1 次、先到先得)。
// 调用前必须已锁住模拟盘账户并完成了自有资金入账前的本金结算。
func applyTradeMatchBonusTx(tx *gorm.DB, account *TradeAccount, depositQuota int, ip string, day string, now time.Time) (int, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.BonusEnabled || !setting.BonusMatchEnabled || setting.BonusMatchMaxUsd <= 0 {
		return 0, ErrTradeBonusDisabled
	}
	if account.BonusPrincipal > 0 {
		return 0, ErrTradeBonusActiveExists
	}
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	matchCap, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusMatchMaxUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil || matchCap < 1 {
		return 0, ErrTradeAmountInvalid
	}
	matchQuota := min(depositQuota, matchCap)
	if matchQuota < 1 {
		return 0, ErrTradeAmountInvalid
	}
	poolCap := 0
	if setting.BonusPoolDailyUsd > 0 {
		poolCap, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusPoolDailyUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
		if err != nil {
			return 0, ErrTradeAmountInvalid
		}
	}
	period := TradeBonusPeriodKey(now, setting.BonusMatchPeriod)
	if setting.BonusTaskWagerCoins > 0 {
		requiredWager, err := WholeGameCoins(setting.BonusTaskWagerCoins)
		if err != nil {
			return 0, err
		}
		since := TradeBonusPeriodStart(now, setting.BonusMatchPeriod)
		wagered, err := getGameWagerVolumeSinceTx(tx, account.UserId, since)
		if err != nil {
			return 0, err
		}
		if wagered < requiredWager {
			return 0, ErrTradeBonusTaskIncomplete
		}
	}
	claim := TradeBonusClaim{
		UserId:    account.UserId,
		Kind:      TradeBonusModeMatch,
		Period:    period,
		Amount:    matchQuota,
		Ip:        ip,
		CreatedAt: now.Unix(),
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected != 1 {
		return 0, ErrTradeBonusAlreadyClaimed
	}
	if err = consumeTradeBonusPoolTx(tx, day, matchQuota, poolCap); err != nil {
		return 0, err
	}
	account.Cash += matchQuota
	account.BonusPrincipal = matchQuota
	account.BonusMode = TradeBonusModeMatch
	return matchQuota, nil
}

// creditGameCoinWagerFromTradeTx 在模拟盘真实成交后，按配置比例消减用户在游戏中心的待解锁流水(locked_wager)。
// 纯体验金账户交易不计入游戏币解锁流水，防止用白送的体验金刷掉游戏币流水锁。调用前必须已锁住 trade_accounts，符合全局锁序。
func creditGameCoinWagerFromTradeTx(tx *gorm.DB, account *TradeAccount, notionalQuota int, bps int) error {
	if notionalQuota <= 0 || bps <= 0 {
		return nil
	}
	if account.BonusPrincipal > 0 && account.QuotaPrincipal == 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&GameCoinAccount{}) {
		return nil
	}
	creditQuota, err := common.QuotaFromDecimalStrict(
		decimal.NewFromInt(int64(notionalQuota)).
			Mul(decimal.NewFromInt(int64(bps))).
			Div(decimal.NewFromInt(10000)).
			Floor(),
	)
	if err != nil || creditQuota <= 0 {
		return nil
	}
	return consumeGameCoinLockedWagerTx(tx, account.UserId, GameCoins(creditQuota))
}

// ClaimTradeRankingBonus 领取上一已结束周期排行榜达标的模拟盘体验金。
// 通过 (user_id, "ranking", periodKey) 唯一约束防重领，并受全站每日体验金池与不可叠加活跃体验金约束。
func ClaimTradeRankingBonus(userId int, periodKey string, amountUsd int, ip string, day string) (*TradeAccount, int, error) {
	setting := operation_setting.GetTradeSetting()
	if !setting.Enabled || !setting.RankingBonusEnabled || amountUsd <= 0 || periodKey == "" {
		return nil, 0, ErrTradeBonusDisabled
	}
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return nil, 0, err
	}
	bonusQuota, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(amountUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
	if err != nil || bonusQuota < 1 {
		return nil, 0, ErrTradeAmountInvalid
	}
	poolCap := 0
	if setting.BonusPoolDailyUsd > 0 {
		poolCap, err = common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(setting.BonusPoolDailyUsd)).Mul(decimal.NewFromInt(int64(perUsd))))
		if err != nil {
			return nil, 0, ErrTradeAmountInvalid
		}
	}
	var account *TradeAccount
	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		if account, txErr = lockTradeAccountTx(tx, userId); txErr != nil {
			return txErr
		}
		if account.BonusPrincipal > 0 {
			return ErrTradeBonusActiveExists
		}
		claim := TradeBonusClaim{
			UserId:    userId,
			Kind:      TradeBonusModeRanking,
			Period:    periodKey,
			Amount:    bonusQuota,
			Ip:        ip,
			CreatedAt: now,
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrTradeBonusAlreadyClaimed
		}
		if txErr = consumeTradeBonusPoolTx(tx, day, bonusQuota, poolCap); txErr != nil {
			return txErr
		}
		account.Cash += bonusQuota
		account.BonusPrincipal = bonusQuota
		account.BonusMode = TradeBonusModeRanking
		if txErr = saveTradeAccountTx(tx, account); txErr != nil {
			return txErr
		}
		return tradeLedgerTx(tx, account, &TradeLedger{Type: TradeLedgerBonusIn, Amount: bonusQuota})
	})
	if err != nil {
		return nil, 0, err
	}
	return account, bonusQuota, nil
}

