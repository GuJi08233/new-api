package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// CreateGameSessionWithSave 锁住玩家的 saveKey 存档，确认 game 没有未过期的对局，再按当前存档准备并创建一局。
// 与 UpdateGameSaveWithoutSession 共用存档锁，两个入口不能同时开始互斥的活动。prepare 只计算状态，不写库；它返回
// 错误时不创建对局。这里不改存档的内容与版本，也不删除过期对局，避免与结算时的对局、存档加锁顺序相反。
func CreateGameSessionWithSave(userId int, game string, seed uint32, activeSince int64, saveKey string, prepare func(data string) (state string, err error)) (*GameSession, error) {
	var session *GameSession
	err := DB.Transaction(func(tx *gorm.DB) error {
		save, err := lockGameSaveTx(tx, userId, saveKey)
		if err != nil {
			return err
		}
		if err := requireNoPlayingGameSessionTx(tx, userId, game, activeSince); err != nil {
			return err
		}
		state, err := prepare(save.Data)
		if err != nil {
			return err
		}
		now := common.GetTimestamp()
		session = &GameSession{UserId: userId, Game: game, Seed: int64(seed), Status: GameSessionStatusPlaying,
			State: state, PlayedAt: now, CreatedAt: now}
		return tx.Create(session).Error
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

// UpdateGameSaveWithoutSession 锁住存档并计算改动，只有 sessionGame 没有未过期对局时才写回。activeSince 是仍算有效的
// 开局时间下界，含这个时刻。update 只计算新存档，不产生外部副作用；先调用它，使请求收据能优先认出已经完成的重试。
func UpdateGameSaveWithoutSession(userId int, saveKey, sessionGame string, activeSince int64, update func(data string) (string, error)) (string, error) {
	var data string
	err := DB.Transaction(func(tx *gorm.DB) error {
		save, err := lockGameSaveTx(tx, userId, saveKey)
		if err != nil {
			return err
		}
		data, err = update(save.Data)
		if err != nil {
			return err
		}
		if err := requireNoPlayingGameSessionTx(tx, userId, sessionGame, activeSince); err != nil {
			return err
		}
		return writeGameSaveTx(tx, save, data)
	})
	return data, err
}

// requireNoPlayingGameSessionTx 在存档锁内检查互斥对局，不再锁对局行：结算会先改对局再锁存档，加对局锁会造成反序等待。
func requireNoPlayingGameSessionTx(tx *gorm.DB, userId int, game string, activeSince int64) error {
	var count int64
	if err := tx.Model(&GameSession{}).
		Where("user_id = ? AND game = ? AND status = ? AND created_at >= ?", userId, game, GameSessionStatusPlaying, activeSince).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrGameRoundOpen
	}
	return nil
}
