package model

import "cmp"

// ListTradeTraders 返回这些用户里成交过的人：买入过现货或开过合约仓位(卖出与平仓之前一定先有这两种成交)。
func ListTradeTraders(userIds []int) ([]int, error) {
	var traders []int
	if len(userIds) == 0 {
		return traders, nil
	}
	err := DB.Model(&TradeLedger{}).Distinct().
		Where("user_id IN ? AND type IN ?", userIds, []string{TradeLedgerBuy, TradeLedgerFuturesOpen}).
		Pluck("user_id", &traders).Error
	return traders, err
}

// ListTradeUserNames 返回这些用户在排行榜上显示的名字：显示名，没有时用用户名。已删除的用户不在结果里。
func ListTradeUserNames(userIds []int) (map[int]string, error) {
	names := make(map[int]string, len(userIds))
	if len(userIds) == 0 {
		return names, nil
	}
	var users []User
	if err := DB.Select("id", "username", "display_name").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		names[user.Id] = cmp.Or(user.DisplayName, user.Username)
	}
	return names, nil
}
