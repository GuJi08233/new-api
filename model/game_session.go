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
//
// 下注游戏(例如老虎机)没有开局和结算两步：每次下注由服务端当场决定结果，扣注与派彩在同一个事务里完成，
// 记为一局已结束的对局。它的两个计数记的是净赢(派彩减去下注)，输掉的下注会让计数减少，可以是负数；
// 设了每日上限时，净赢同样不会超过它。

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
	// ErrGameWagerDailyLimit 表示账号今天在这个下注游戏里的净赢已到上限，今天不能再下注。
	ErrGameWagerDailyLimit = errors.New("daily net win limit of this game reached")
	// ErrGameWagerIpLimit 表示同一 IP 下的账号今天在这个下注游戏里的净赢合计已到上限。
	ErrGameWagerIpLimit = errors.New("daily net win limit of this game reached for this ip")
)

// GameSession 是一局游戏。种子只发给开这一局的玩家，结算时服务端用它重放。
// idx_game_session_best 让"最高成绩"只读索引：老虎机每转一次记一局，同一用户的对局会很多。
type GameSession struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"index:idx_game_session_user_game,priority:1;index:idx_game_session_best,priority:1"`
	Game   string `json:"game" gorm:"type:varchar(32);index:idx_game_session_user_game,priority:2;index:idx_game_session_best,priority:2"`
	Seed   int64  `json:"-" gorm:"bigint"`
	Status string `json:"status" gorm:"type:varchar(16);index:idx_game_session_best,priority:3"`
	// Score 是成绩：俄罗斯方块是分数，下注游戏是实际派彩。
	Score  int `json:"score" gorm:"type:bigint;index:idx_game_session_best,priority:4"`
	Reward int `json:"reward" gorm:"type:bigint"` // 实际发放的游戏币，已按当日上限扣减
	// LimitedBy 是 Reward 少于成绩应得数量的原因(GameRewardLimitedBy*)，没有少发时为空。
	LimitedBy string `json:"limited_by" gorm:"type:varchar(16)"`
	// Ip 是结算请求的来源 IP(IPv6 已归并到前缀)，这局的奖励计入它的每日上限。
	Ip string `json:"ip" gorm:"type:varchar(64)"`
	// Detail 是各游戏自己的成绩明细(JSON)，供事后核查。
	Detail     string `json:"detail" gorm:"type:varchar(255)"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	FinishedAt int64  `json:"finished_at" gorm:"bigint"`
}

// GameRewardDay 记录每个用户每天从每个游戏获得的游戏币(下注游戏为净赢，可以为负)。结算时锁住这一行再发放，
// 同一用户的几局同时结算也不会越过上限。
type GameRewardDay struct {
	UserId int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Game   string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day    string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins  int    `json:"coins" gorm:"type:bigint;not null;default:0"`
}

// GameRewardIpDay 记录每个 IP 每天从每个游戏获得的游戏币(下注游戏为净赢)，同一 IP 下所有账号合计。站点的用户各自使用
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

// GameWager 是下注游戏的一局：由 Play 按游戏规则当场决定结果，SettleGameWager 扣注、派彩，并按每日净赢上限截断。
type GameWager struct {
	Bet int
	// Play 决定这一局的结果：参数是玩家这一局之前在下注游戏里的累计下注，返回按游戏规则应得的派彩(含退回的下注，
	// 还没有按每日上限截断)与明细。它在结算事务里、锁住玩家的计数与账户之后才调用，同一玩家的几局依次进行，
	// 并发的另一局改不了这里看到的累计下注。
	Play func(wageredBefore int) (win int, detail string, err error)
	// DailyLimit 是每人每天、每个 IP 每天最多从这个游戏净赢的游戏币，0 表示不限制。
	DailyLimit int
	Day        string
	Ip         string
}

// GameWagerResult 是一局下注游戏的结算结果。Session 的 Score 与 Reward 都是实际派彩；
// Earned、IpEarned 是结算后账号、同一 IP 今天在这个游戏的净赢，Wagered 是结算后玩家的累计下注。
type GameWagerResult struct {
	Session  *GameSession
	Earned   int
	IpEarned int
	Balance  int
	Wagered  int
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

// SettleGameWager 结算下注游戏的一局：决定结果、扣注、派彩、记对局与流水在同一个事务里，结果由 wager.Play
// 按玩家这一局之前的累计下注决定。设了每日上限时，账号或 IP 今天的净赢已到上限就拒绝下注；派彩会让净赢越过上限时
// 只派到上限为止，对局上记下原因。余额不够下注时返回 ErrGameCoinInsufficient，什么都不会改变。
func SettleGameWager(userId int, game string, wager GameWager) (*GameWagerResult, error) {
	if wager.Bet < 1 || wager.Bet >= common.MaxQuota || wager.DailyLimit < 0 || wager.DailyLimit >= common.MaxQuota {
		return nil, ErrGameCoinAmountInvalid
	}
	// 不限制每日净赢时按记账边界 2^53 计算：计数照常累加，今日输赢照常显示，也不会越界。
	limit := wager.DailyLimit
	if limit == 0 {
		limit = common.MaxQuota - 1
	}
	now := common.GetTimestamp()
	session := &GameSession{
		UserId:     userId,
		Game:       game,
		Status:     GameSessionStatusFinished,
		Ip:         wager.Ip,
		CreatedAt:  now,
		FinishedAt: now,
	}
	result := &GameWagerResult{Session: session}
	userKey := map[string]interface{}{"user_id": userId, "game": game, "day": wager.Day}
	ipKey := map[string]interface{}{"ip": wager.Ip, "game": game, "day": wager.Day}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 与 FinishGameSession 一样先锁住两个计数：同一用户、同一 IP 的几次下注依次结算，净赢不会越过上限。
		userNet, err := lockGameRewardCounter(tx, &GameRewardDay{UserId: userId, Game: game, Day: wager.Day}, userKey)
		if err != nil {
			return err
		}
		ipNet, err := lockGameRewardCounter(tx, &GameRewardIpDay{Ip: wager.Ip, Game: game, Day: wager.Day}, ipKey)
		if err != nil {
			return err
		}
		userRoom, ipRoom := limit-userNet, limit-ipNet
		if userRoom <= 0 {
			return ErrGameWagerDailyLimit
		}
		if ipRoom <= 0 {
			return ErrGameWagerIpLimit
		}
		var wagered []int
		if err := lockForUpdate(tx).Model(&GameCoinAccount{}).Where("user_id = ?", userId).Pluck("total_wagered", &wagered).Error; err != nil {
			return err
		}
		wageredBefore := 0
		if len(wagered) > 0 {
			wageredBefore = wagered[0]
		}
		win, detail, err := wager.Play(wageredBefore)
		if err != nil {
			return err
		}
		if win < 0 || win >= common.MaxQuota {
			return ErrGameCoinAmountInvalid
		}
		session.Detail = detail

		net := win - wager.Bet
		if room := min(userRoom, ipRoom); net > room {
			net = room
			session.LimitedBy = GameRewardLimitedByDaily
			if ipRoom < userRoom {
				session.LimitedBy = GameRewardLimitedByIp
			}
		}
		session.Reward = wager.Bet + net
		session.Score = session.Reward

		balance, err := debitGameCoinsTx(tx, userId, wager.Bet, map[string]interface{}{
			"total_wager_net": gorm.Expr("total_wager_net + ?", net),
			"total_wagered":   gorm.Expr("total_wagered + ?", wager.Bet),
		})
		if err != nil {
			return err
		}
		if net != 0 {
			if err := addGameRewardCounter(tx, &GameRewardDay{}, userKey, net, limit); err != nil {
				return err
			}
			if err := addGameRewardCounter(tx, &GameRewardIpDay{}, ipKey, net, limit); err != nil {
				return err
			}
		}
		if session.Reward > 0 {
			if balance, err = creditGameCoinsTx(tx, userId, session.Reward, nil); err != nil {
				return err
			}
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		result.Earned, result.IpEarned, result.Balance, result.Wagered = userNet+net, ipNet+net, balance, wageredBefore+wager.Bet
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    net,
			Balance:   balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    game,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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

// addGameRewardCounter 给奖励计数累加 coins(下注游戏输掉时为负)，条件更新保证累计数量不会越过 limit。
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

// GetGameRewardEarned 返回用户某天从某个游戏获得的游戏币，下注游戏是净赢，可以为负。
func GetGameRewardEarned(userId int, game string, day string) (int, error) {
	var coins int
	err := DB.Model(&GameRewardDay{}).Where("user_id = ? AND game = ? AND day = ?", userId, game, day).Select("coins").Scan(&coins).Error
	return coins, err
}

// GetGameRewardIpEarned 返回某个 IP 下所有账号某天从某个游戏获得的游戏币合计，下注游戏是净赢。
func GetGameRewardIpEarned(ip string, game string, day string) (int, error) {
	var coins int
	err := DB.Model(&GameRewardIpDay{}).Where("ip = ? AND game = ? AND day = ?", ip, game, day).Select("coins").Scan(&coins).Error
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

// SumGameRewardsByGame 汇总某天各游戏发出的游戏币，供管理端概览。下注游戏是所有玩家的净赢合计，为负表示玩家合计输掉的数量。
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
