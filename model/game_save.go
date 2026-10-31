package model

import (
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
	now := common.GetTimestamp()
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GameSave{UserId: userId, Game: game, UpdatedAt: now}).Error; err != nil {
		return "", err
	}
	var save GameSave
	if err := lockForUpdate(tx).Where("user_id = ? AND game = ?", userId, game).First(&save).Error; err != nil {
		return "", err
	}
	data, err := update(save.Data)
	if err != nil {
		return "", err
	}
	err = tx.Model(&GameSave{}).Where("user_id = ? AND game = ?", userId, game).
		Updates(map[string]interface{}{"data": data, "version": save.Version + 1, "updated_at": now}).Error
	return data, err
}
