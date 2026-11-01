package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GameSave 是玩家在长期游戏里的局外存档(例如深渊远征的魂晶、职业、天赋与战绩)，内容由各游戏自己定义。
// 存档不随游戏记录清理：它是玩家长期积累的进度。
type GameSave struct {
	UserId    int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Game      string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Data      string `json:"-" gorm:"type:text"`
	Version   int    `json:"version" gorm:"not null;default:0"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

// GameSaveUpdate 在结算一局时一起改写存档：Update 拿到当前存档内容(没有存档时为空字符串)，返回新的内容。
type GameSaveUpdate struct {
	Game   string
	Update func(data string) (string, error)
}

// GetGameSave 返回玩家在某个游戏里的存档内容，还没有存档时返回空字符串。
func GetGameSave(userId int, game string) (string, error) {
	var saves []GameSave
	if err := DB.Where("user_id = ? AND game = ?", userId, game).Limit(1).Find(&saves).Error; err != nil {
		return "", err
	}
	if len(saves) == 0 {
		return "", nil
	}
	return saves[0].Data, nil
}

// UpdateGameSave 锁住存档、交给 update 改写并保存，返回新的内容。update 返回错误时什么都不改。
func UpdateGameSave(userId int, game string, update func(data string) (string, error)) (string, error) {
	var data string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		data, err = updateGameSaveTx(tx, userId, game, update)
		return err
	})
	return data, err
}

// updateGameSaveTx 在事务里改写存档：没有存档时先建一行空存档，再锁住这一行读出、改写，同一玩家同时的几次改写依次进行。
func updateGameSaveTx(tx *gorm.DB, userId int, game string, update func(data string) (string, error)) (string, error) {
	save, err := lockGameSaveTx(tx, userId, game)
	if err != nil {
		return "", err
	}
	data, err := update(save.Data)
	if err != nil {
		return "", err
	}
	return data, writeGameSaveTx(tx, save, data)
}

// lockGameSaveTx 锁住一位玩家的存档并读出来，没有存档时先建一行空存档。
func lockGameSaveTx(tx *gorm.DB, userId int, game string) (*GameSave, error) {
	now := common.GetTimestamp()
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GameSave{UserId: userId, Game: game, UpdatedAt: now}).Error; err != nil {
		return nil, err
	}
	var save GameSave
	if err := lockForUpdate(tx).Where("user_id = ? AND game = ?", userId, game).First(&save).Error; err != nil {
		return nil, err
	}
	return &save, nil
}

// writeGameSaveTx 把锁住的存档改成 data。
func writeGameSaveTx(tx *gorm.DB, save *GameSave, data string) error {
	return tx.Model(&GameSave{}).Where("user_id = ? AND game = ?", save.UserId, save.Game).
		Updates(map[string]interface{}{"data": data, "version": save.Version + 1, "updated_at": common.GetTimestamp()}).Error
}

// UpdateTwoGameSaves 在一个事务里一起改写两位玩家(first 与 second 不同)的同一个游戏存档：update 拿到两份当前内容，返回
// 两份新内容。按玩家编号从小到大加锁，两位玩家互相操作对方时不会互相等待。update 返回错误时什么都不改。
func UpdateTwoGameSaves(first int, second int, game string, update func(firstData string, secondData string) (string, string, error)) error {
	if first == second {
		return errors.New("game save: the two players must differ")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		low, high := min(first, second), max(first, second)
		lowSave, err := lockGameSaveTx(tx, low, game)
		if err != nil {
			return err
		}
		highSave, err := lockGameSaveTx(tx, high, game)
		if err != nil {
			return err
		}
		firstSave, secondSave := lowSave, highSave
		if first != low {
			firstSave, secondSave = highSave, lowSave
		}
		firstData, secondData, err := update(firstSave.Data, secondSave.Data)
		if err != nil {
			return err
		}
		if err := writeGameSaveTx(tx, firstSave, firstData); err != nil {
			return err
		}
		return writeGameSaveTx(tx, secondSave, secondData)
	})
}

// ListGameSaves 返回这些玩家在某个游戏里的存档，没有存档的玩家不在结果里。
func ListGameSaves(game string, userIds []int) ([]GameSave, error) {
	var saves []GameSave
	if len(userIds) == 0 {
		return saves, nil
	}
	err := DB.Where("game = ? AND user_id IN ?", game, userIds).Find(&saves).Error
	return saves, err
}

// ListRecentGameSaves 返回最近改过某个游戏存档的几位玩家的存档(不含 exclude)，按改动时间从近到远。
func ListRecentGameSaves(game string, exclude int, limit int) ([]GameSave, error) {
	var saves []GameSave
	err := DB.Where("game = ? AND user_id <> ?", game, exclude).Order("updated_at desc").Limit(limit).Find(&saves).Error
	return saves, err
}
