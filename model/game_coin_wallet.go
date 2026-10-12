package model

import (
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 划转方向以游戏币钱包为准；模拟盘划转不算游戏发奖，也不改动原有累计收益字段。GameCoinLogTypeTradeCover 是模拟盘穿仓的亏空
// 按用户选的抵扣顺序从游戏币里扣掉的部分。
const (
	GameCoinLogTypeTradeIn    = "trade_in"
	GameCoinLogTypeTradeOut   = "trade_out"
	GameCoinLogTypeTradeCover = "trade_cover"
)

// applyGameCoinDeltaTx 只供已有资金来源的业务事务调用。调用方须在同一事务扣除来源余额、校验提款限制，
// 并向外返回本函数的任何错误以回滚整笔划转；此处不提供独立发币、兑换额度或提现入口。
func applyGameCoinDeltaTx(tx *gorm.DB, userId int, delta GameCoins, entry GameCoinLog) (GameCoins, error) {
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
