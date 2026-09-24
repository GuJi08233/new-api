package model

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 游戏对局与奖励。开局时服务端给这一局发一个随机种子；结束时浏览器上传整局操作，服务端用种子重放出
// 成绩，按成绩发游戏币。每人每个游戏同一时间只能进行一局，同一局只结算一次。每人每天、每个 IP 每天
// 从每个游戏获得的游戏币分别由 GameRewardDay 和 GameRewardIpDay 计数，都不会超过该游戏的每日上限。

const (
	GameSessionStatusPlaying  = "playing"
	GameSessionStatusFinished = "finished"
)

// 一局实际发放的游戏币少于成绩应得的数量时，对局上记下原因，重复提交时原样告诉玩家。
const (
	GameRewardLimitedBySpeed = "speed" // 操作快过人类极限，判定为脚本，不发奖励
	GameRewardLimitedByTime  = "time"  // 游戏时长不够成绩应得的数量
	GameRewardLimitedByDaily = "daily" // 账号今天从这个游戏获得的奖励已达上限
	GameRewardLimitedByIp    = "ip"    // 同一 IP 今天从这个游戏获得的奖励已达上限
)

var (
	ErrGameSessionNotFound = errors.New("game session not found")
	// ErrGameSessionClosed 表示对局已不在进行中：已经结算过，或者被新开的一局作废删除了。
	ErrGameSessionClosed = errors.New("game session is no longer playing")
	// ErrGameSessionSuperseded 表示同一用户在这局之后又开了一局，这局不能再结算。
	ErrGameSessionSuperseded = errors.New("a newer game session has been started")
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
	// LimitedBy 是 Reward 少于成绩应得数量的原因(GameRewardLimitedBy*)，没有少发时为空。
	LimitedBy string `json:"limited_by" gorm:"type:varchar(16)"`
	// Ip 是结算请求的来源 IP(IPv6 已归并到前缀)，这局的奖励计入它的每日上限。
	Ip string `json:"ip" gorm:"type:varchar(64)"`
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

// GameRewardIpDay 记录每个 IP 每天从每个游戏获得的游戏币，同一 IP 下所有账号合计。站点的用户各自使用
// 独立的出口 IP，同一个 IP 就视为同一个人：它与账号共用每日上限，多开账号刷不到更多奖励。
type GameRewardIpDay struct {
	Ip    string `json:"ip" gorm:"type:varchar(64);primaryKey"`
	Game  string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day   string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins int    `json:"coins" gorm:"type:bigint;not null;default:0"`
}

// GameSettlement 是一局的结算结果：各游戏按自己的规则算出成绩和应得的游戏币，交给 FinishGameSession 按每日上限发放。
type GameSettlement struct {
	Score  int
	Detail string
	// Reward 是按成绩与游戏规则应得的游戏币，还没有扣减每日上限；LimitedBy 说明游戏规则为什么给得比分数应得的少。
	Reward    int
	LimitedBy string
	// DailyLimit 是每人每天、每个 IP 每天最多从这个游戏获得的游戏币。
	DailyLimit int
	Day        string
	Ip         string
}

// CreateGameSession 开一局。每人每个游戏同一时间只能进行一局：之前没结算的对局在这里作废删除，
// 脚本没法同时开几局来压缩所需的游戏时长，中途离开的对局也不会一直留在库里。
func CreateGameSession(userId int, game string, seed uint32) (*GameSession, error) {
	session := &GameSession{
		UserId:    userId,
		Game:      game,
		Seed:      int64(seed),
		Status:    GameSessionStatusPlaying,
		CreatedAt: common.GetTimestamp(),
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusPlaying).Delete(&GameSession{}).Error; err != nil {
			return err
		}
		return tx.Create(session).Error
	})
	if err != nil {
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

// FinishGameSession 结算一局：把对局从进行中切换为已结束，同一局只有一次能成功，并按账号和 IP 今天剩余的额度发放奖励。
// 返回结算后的对局(Reward 为实际发放的数量)、账号今天从这个游戏累计获得的数量和结算后的余额。
// 对局已不在进行中时返回 ErrGameSessionClosed，调用方重新读取对局就能知道它是已经结算还是被作废了。
func FinishGameSession(session *GameSession, settlement GameSettlement) (settled *GameSession, earned int, balance int, err error) {
	finished := *session
	finished.Status = GameSessionStatusFinished
	finished.Score = settlement.Score
	finished.Detail = settlement.Detail
	finished.LimitedBy = settlement.LimitedBy
	finished.Ip = settlement.Ip
	finished.FinishedAt = common.GetTimestamp()
	userKey := map[string]interface{}{"user_id": session.UserId, "game": session.Game, "day": settlement.Day}
	ipKey := map[string]interface{}{"ip": settlement.Ip, "game": session.Game, "day": settlement.Day}
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 先锁住计数再切换对局状态：同一用户、同一 IP 的几局同时结算时依次进行，都不会越过上限。
		userEarned, err := lockGameRewardCounter(tx, &GameRewardDay{UserId: session.UserId, Game: session.Game, Day: settlement.Day}, userKey)
		if err != nil {
			return err
		}
		// 管理员调低上限后，已领取的数量可能已经超过新上限，这时不再发放。
		granted := max(min(settlement.Reward, settlement.DailyLimit-userEarned), 0)
		if granted < settlement.Reward {
			finished.LimitedBy = GameRewardLimitedByDaily
		}
		if granted > 0 {
			ipEarned, err := lockGameRewardCounter(tx, &GameRewardIpDay{Ip: settlement.Ip, Game: session.Game, Day: settlement.Day}, ipKey)
			if err != nil {
				return err
			}
			if remaining := max(settlement.DailyLimit-ipEarned, 0); remaining < granted {
				granted = remaining
				finished.LimitedBy = GameRewardLimitedByIp
			}
		}
		finished.Reward = granted

		result := tx.Model(&GameSession{}).
			Where("id = ? AND status = ?", session.Id, GameSessionStatusPlaying).
			Updates(map[string]interface{}{
				"status":      finished.Status,
				"score":       finished.Score,
				"reward":      finished.Reward,
				"limited_by":  finished.LimitedBy,
				"ip":          finished.Ip,
				"detail":      finished.Detail,
				"finished_at": finished.FinishedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGameSessionClosed
		}
		// 开局时会作废之前的对局，但两次开局同时到达时可能都留了下来：只有最后开的那一局能结算。
		var newer int64
		if err := tx.Model(&GameSession{}).Where("user_id = ? AND game = ? AND id > ?", session.UserId, session.Game, session.Id).Count(&newer).Error; err != nil {
			return err
		}
		if newer > 0 {
			return ErrGameSessionSuperseded
		}

		earned = userEarned
		if granted == 0 {
			balance, err = readGameCoinBalanceTx(tx, session.UserId)
			return err
		}
		// 行锁之外再用条件更新兜底，两个计数在任何情况下都不会越过上限。
		if err := addGameRewardCounter(tx, &GameRewardDay{}, userKey, granted, settlement.DailyLimit); err != nil {
			return err
		}
		if err := addGameRewardCounter(tx, &GameRewardIpDay{}, ipKey, granted, settlement.DailyLimit); err != nil {
			return err
		}
		earned += granted
		balance, err = creditGameCoinsTx(tx, session.UserId, granted, map[string]interface{}{
			"total_earned": gorm.Expr("total_earned + ?", granted),
		})
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    session.UserId,
			Type:      GameCoinLogTypeGameReward,
			Amount:    granted,
			Balance:   balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    session.Game,
			CreatedAt: finished.FinishedAt,
		}).Error
	})
	if err != nil {
		return nil, 0, 0, err
	}
	return &finished, earned, balance, nil
}

// lockGameRewardCounter 锁住一个奖励计数行并返回已经发放的数量，行不存在时先建出来。row 是要建的行，key 是它的主键列。
func lockGameRewardCounter(tx *gorm.DB, row interface{}, key map[string]interface{}) (int, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return 0, err
	}
	var coins int
	err := lockForUpdate(tx).Model(row).Where(key).Select("coins").Scan(&coins).Error
	return coins, err
}

// addGameRewardCounter 给奖励计数累加 coins，条件更新保证累计数量不会越过 limit。
func addGameRewardCounter(tx *gorm.DB, table interface{}, key map[string]interface{}, coins int, limit int) error {
	result := tx.Model(table).Where(key).Where("coins <= ?", limit-coins).Update("coins", gorm.Expr("coins + ?", coins))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("game reward counter changed during settlement")
	}
	return nil
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
