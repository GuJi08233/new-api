package model

import (
	"errors"
	"math"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 划转方向以游戏币钱包为准；模拟盘划转不算游戏发奖，也不改动原有累计收益字段。
const (
	GameCoinLogTypeTradeIn  = "trade_in"
	GameCoinLogTypeTradeOut = "trade_out"
)

var (
	ErrGameCoinAmountInvalid = errors.New("game coin amount is out of range")
	ErrGameCoinInsufficient  = errors.New("game coin balance is insufficient")
	ErrGameCoinUserNotFound  = errors.New("game coin user not found")
)

// GameCoinAccount 沿用独立游戏币钱包的表结构，余额是整数游戏币，1 币对应 1 美元。
// 游戏相关累计字段只为已有数据兼容保留，钱包初始化不会发币。
type GameCoinAccount struct {
	UserId         int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Balance        int   `json:"balance" gorm:"type:bigint;not null;default:0"`
	TotalEarned    int   `json:"total_earned" gorm:"type:bigint;not null;default:0"`
	TotalConverted int   `json:"total_converted" gorm:"type:bigint;not null;default:0"`
	TotalWithdrawn int   `json:"total_withdrawn" gorm:"type:bigint;not null;default:0"`
	TotalWagerNet  int   `json:"total_wager_net" gorm:"type:bigint;not null;default:0"`
	TotalWagered   int   `json:"total_wagered" gorm:"type:bigint;not null;default:0"`
	CreatedAt      int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64 `json:"updated_at" gorm:"bigint"`
}

// GameCoinLog 保留原钱包流水结构，每次余额变动与流水必须在同一个事务内提交。
type GameCoinLog struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"index"`
	Type      string `json:"type" gorm:"type:varchar(32)"`
	Amount    int    `json:"amount" gorm:"type:bigint"`
	Balance   int    `json:"balance" gorm:"type:bigint"`
	Quota     int    `json:"quota" gorm:"type:bigint"`
	RefId     string `json:"ref_id" gorm:"type:varchar(64);index"`
	Remark    string `json:"remark" gorm:"type:varchar(255)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func GetGameCoinAccount(userId int) (GameCoinAccount, error) {
	if userId <= 0 {
		return GameCoinAccount{}, ErrGameCoinUserNotFound
	}
	var accounts []GameCoinAccount
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&accounts).Error; err != nil {
		return GameCoinAccount{}, err
	}
	if len(accounts) == 0 {
		return GameCoinAccount{UserId: userId}, nil
	}
	return accounts[0], nil
}

// GameCoinQuota 使用模拟盘相同的汇率把整币换算为额度；先校验乘法边界，绝不截断或夹紧用户资金。
func GameCoinQuota(coins int) (int, error) {
	if coins <= 0 || coins >= common.MaxQuota || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, ErrGameCoinAmountInvalid
	}
	perUsd, err := TradeQuotaPerUsd()
	if err != nil {
		return 0, err
	}
	if perUsd <= 0 || coins > (common.MaxQuota-1)/perUsd {
		return 0, ErrGameCoinAmountInvalid
	}
	return coins * perUsd, nil
}

// lockGameCoinAccountTx 在余额第一次变动时建立空钱包，并使用跨数据库的账户锁。
// 涉及模拟盘的划转必须先锁模拟盘账户，再锁游戏币账户，所有路径遵循同一顺序。
func lockGameCoinAccountTx(tx *gorm.DB, userId int) (*GameCoinAccount, error) {
	if userId <= 0 {
		return nil, ErrGameCoinUserNotFound
	}
	var user User
	if err := tx.Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGameCoinUserNotFound
		}
		return nil, err
	}
	now := common.GetTimestamp()
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GameCoinAccount{UserId: userId, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		return nil, err
	}
	var account GameCoinAccount
	if err := lockForUpdate(tx).Where("user_id = ?", userId).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// applyGameCoinDeltaTx 只供已有资金来源的业务事务调用。调用方须在同一事务扣除来源余额、校验提款限制，
// 并向外返回本函数的任何错误以回滚整笔划转；此处不提供独立发币、兑换额度或提现入口。
func applyGameCoinDeltaTx(tx *gorm.DB, userId int, delta int, entry GameCoinLog) (int, error) {
	if delta == 0 || delta <= -common.MaxQuota || delta >= common.MaxQuota || entry.Quota < 0 || entry.Quota >= common.MaxQuota ||
		entry.Type == "" || utf8.RuneCountInString(entry.Type) > 32 || utf8.RuneCountInString(entry.RefId) > 64 || utf8.RuneCountInString(entry.Remark) > 255 {
		return 0, ErrGameCoinAmountInvalid
	}
	account, err := lockGameCoinAccountTx(tx, userId)
	if err != nil {
		return 0, err
	}
	if account.Balance < 0 || account.Balance >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	if delta < 0 && account.Balance < -delta {
		return 0, ErrGameCoinInsufficient
	}
	if delta > 0 && account.Balance > common.MaxQuota-1-delta {
		return 0, ErrGameCoinAmountInvalid
	}
	balance := account.Balance + delta
	now := common.GetTimestamp()
	result := tx.Model(&GameCoinAccount{}).Where("user_id = ? AND balance = ?", userId, account.Balance).
		Updates(map[string]interface{}{"balance": balance, "updated_at": now})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, ErrGameCoinAmountInvalid
	}
	entry.Id, entry.UserId, entry.Amount, entry.Balance, entry.CreatedAt = 0, userId, delta, balance, now
	if err = tx.Create(&entry).Error; err != nil {
		return 0, err
	}
	return balance, nil
}
