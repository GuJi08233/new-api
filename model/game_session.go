package model

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 游戏对局与奖励。开局时服务端给这一局发一个随机种子；结束时浏览器上传整局操作，服务端用种子重放出
// 成绩，按成绩发游戏币。同一局只能结算一次；每人每天从每个游戏获得的游戏币由 GameRewardDay 计数，
// 不会超过该游戏的上限。

const (
	GameSessionStatusPlaying  = "playing"
	GameSessionStatusFinished = "finished"
)

var (
	ErrGameSessionNotFound = errors.New("game session not found")
	ErrGameSessionFinished = errors.New("game session is already finished")
)

// GameSession 是一局游戏。种子只发给开这一局的玩家，结算时服务端用它重放。
type GameSession struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"index:idx_game_session_user_game,priority:1"`
	Game   string `json:"game" gorm:"type:varchar(32);index:idx_game_session_user_game,priority:2"`
	Seed   int64  `json:"-" gorm:"bigint"`
	Status string `json:"status" gorm:"type:varchar(16)"`
	Score  int    `json:"score" gorm:"type:bigint"`
	Reward int    `json:"reward" gorm:"type:bigint"` // 实际发放的游戏币，已按当日上限扣减
	// Detail 是各游戏自己的成绩明细(JSON)，供事后核查。
	Detail     string `json:"detail" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	FinishedAt int64  `json:"finished_at" gorm:"bigint"`
}

// GameRewardDay 记录每个用户每天从每个游戏获得的游戏币。结算时锁住这一行再发放，
// 同一用户的几局同时结算也不会越过上限。
type GameRewardDay struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Game   string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day    string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins  int    `json:"coins" gorm:"type:bigint;not null;default:0"`
}

func CreateGameSession(userId int, game string, seed uint32) (*GameSession, error) {
	session := &GameSession{
		UserId:    userId,
		Game:      game,
		Seed:      int64(seed),
		Status:    GameSessionStatusPlaying,
		CreatedAt: common.GetTimestamp(),
	}
	if err := DB.Create(session).Error; err != nil {
		return nil, err
	}
	return session, nil
}

// GetUserGameSession 返回用户在某个游戏里的一局；别人的对局、其他游戏的对局都当作不存在。
func GetUserGameSession(userId int, game string, id int) (*GameSession, error) {
	var sessions []GameSession
	if err := DB.Where("id = ? AND user_id = ? AND game = ?", id, userId, game).Limit(1).Find(&sessions).Error; err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, ErrGameSessionNotFound
	}
	return &sessions[0], nil
}

// FinishGameSession 结算一局：先把对局从进行中切换为已结束，同一局只有一次能成功；再按当日剩余额度发奖励。
// reward 是这局成绩对应的游戏币，dailyLimit 是每人每天从这个游戏最多获得的数量。
// 返回实际发放的数量、今天从这个游戏累计获得的数量和结算后的余额。
func FinishGameSession(session *GameSession, score int, detail string, reward int, dailyLimit int, day string) (granted int, earned int, balance int, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		result := tx.Model(&GameSession{}).
			Where("id = ? AND status = ?", session.Id, GameSessionStatusPlaying).
			Updates(map[string]interface{}{
				"status":      GameSessionStatusFinished,
				"score":       score,
				"detail":      detail,
				"finished_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGameSessionFinished
		}

		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GameRewardDay{UserId: session.UserId, Game: session.Game, Day: day}).Error; err != nil {
			return err
		}
		var counter GameRewardDay
		if err := lockForUpdate(tx).Where("user_id = ? AND game = ? AND day = ?", session.UserId, session.Game, day).First(&counter).Error; err != nil {
			return err
		}
		earned = counter.Coins
		// 管理员调低上限后，已领取的数量可能已经超过新上限，这时不再发放。
		granted = max(min(reward, dailyLimit-earned), 0)
		if granted == 0 {
			var err error
			balance, err = readGameCoinBalanceTx(tx, session.UserId)
			return err
		}
		// 行锁之外再用条件更新兜底，累计数量在任何情况下都不会越过上限。
		result = tx.Model(&GameRewardDay{}).
			Where("user_id = ? AND game = ? AND day = ? AND coins <= ?", session.UserId, session.Game, day, dailyLimit-granted).
			Update("coins", gorm.Expr("coins + ?", granted))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("game reward counter changed during settlement")
		}
		earned += granted
		var err error
		balance, err = creditGameCoinsTx(tx, session.UserId, granted, map[string]interface{}{
			"total_earned": gorm.Expr("total_earned + ?", granted),
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&GameCoinLog{
			UserId:    session.UserId,
			Type:      GameCoinLogTypeGameReward,
			Amount:    granted,
			Balance:   balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    session.Game,
			CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&GameSession{}).Where("id = ?", session.Id).Update("reward", granted).Error
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return granted, earned, balance, nil
}

// GetGameRewardEarned 返回用户某天从某个游戏获得的游戏币。
func GetGameRewardEarned(userId int, game string, day string) (int, error) {
	var coins int
	err := DB.Model(&GameRewardDay{}).Where("user_id = ? AND game = ? AND day = ?", userId, game, day).Select("coins").Scan(&coins).Error
	return coins, err
}

// GetGameBestScore 返回用户在某个游戏里结算过的最高分，还没有结算过时为 0。
func GetGameBestScore(userId int, game string) (int, error) {
	var best int
	err := DB.Model(&GameSession{}).
		Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusFinished).
		Select("COALESCE(MAX(score), 0)").
		Scan(&best).Error
	return best, err
}

// SumGameRewardsByGame 汇总某天各游戏发出的游戏币，供管理端概览。
func SumGameRewardsByGame(day string) (map[string]int, error) {
	var rows []struct {
		Game  string
		Total int
	}
	if err := DB.Model(&GameRewardDay{}).Select("game, SUM(coins) AS total").Where("day = ?", day).Group("game").Scan(&rows).Error; err != nil {
		return nil, err
	}
	totals := make(map[string]int, len(rows))
	for _, row := range rows {
		totals[row.Game] = row.Total
	}
	return totals, nil
}
