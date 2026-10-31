package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// GameCoinLogTypeGameShop 是在游戏商店里花游戏币买道具。花掉的币直接销毁，不进奖池也不给任何人；这类流水和兑换一样
// 不随游戏记录清理，玩家随时能查到自己买过什么。
const GameCoinLogTypeGameShop = "game_shop"

// BuyGameItem 花 cost 个游戏币买道具：update 把道具放进玩家在 game 里的存档，扣币、记流水与改写存档在同一个事务里，
// 余额不够或 update 返回错误时什么都不改。remark 写进流水(例如 dungeon:elixir×3)。返回扣币之后的余额与新的存档内容。
// 加锁顺序与结算一样先存档后游戏币账户，两者同时发生时不会互相等待。
func BuyGameItem(userId int, game string, cost GameCoins, remark string, update func(data string) (string, error)) (GameCoins, string, error) {
	if cost < 1 || cost >= common.MaxQuota {
		return 0, "", ErrGameCoinAmountInvalid
	}
	var balance GameCoins
	var data string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if data, err = updateGameSaveTx(tx, userId, game, update); err != nil {
			return err
		}
		if _, err = lockGameCoinAccountTx(tx, userId); err != nil {
			return err
		}
		if balance, err = debitGameCoinsTx(tx, userId, cost, nil); err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameShop,
			Amount:    -cost,
			Balance:   balance,
			Remark:    remark,
			CreatedAt: common.GetTimestamp(),
		}).Error
	})
	if err != nil {
		return 0, "", err
	}
	return balance, data, nil
}

// UpdateGameSessionAndSave 保存进行中对局的新状态，并在同一个事务里改写玩家的存档(例如在对局里用掉背包里的道具)。
// 和 UpdateGameSessionState 一样核对 session.Version，版本已变时返回 ErrGameRoundChanged；update 返回错误时什么都不改。
// 加锁顺序与结算一样先对局后存档。
func UpdateGameSessionAndSave(session *GameSession, state string, detail string, game string, update func(data string) (string, error)) error {
	now := common.GetTimestamp()
	played := session.PlayedSeconds(now)
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&GameSession{}).
			Where("id = ? AND status = ? AND version = ?", session.Id, GameSessionStatusPlaying, session.Version).
			Updates(map[string]interface{}{
				"state":        state,
				"detail":       detail,
				"version":      session.Version + 1,
				"play_seconds": played,
				"played_at":    now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGameRoundChanged
		}
		_, err := updateGameSaveTx(tx, session.UserId, game, update)
		return err
	})
	if err != nil {
		return err
	}
	session.State, session.Detail, session.Version = state, detail, session.Version+1
	session.PlaySeconds, session.PlayedAt = played, now
	return nil
}
