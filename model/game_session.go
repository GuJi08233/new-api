package model

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 游戏对局与记账。游戏按结算方式分三类：
//
// 免费游戏不花游戏币、只会赚。技巧游戏开局时由服务端发种子或出题，结束时服务端重放整局或按保存的对局状态判定成绩，
// 成绩达标才发奖(FinishGameSession)；大转盘的免费旋转当场决定结果(SettleGameFreePlay)。每人每个游戏每天只能领几次奖，
// 同一 IP 下所有账号合计也只有这么多次；所有免费奖励每人每天(同一 IP 合计)还可以设一个总上限。
//
// 下注游戏花游戏币下注、会亏：理论返还率低于 100%，玩家长期是输的，所以不限次数、不设每日上限。当场决定结果的下注在一个
// 事务里扣注、派彩(SettleGameWager)；要分几步完成的(例如 21 点、挖宝)开局时扣注，把只有服务端知道的状态存在对局里，
// 结束时派彩(OpenGameRound、SettleGameRound)。
//
// 对战与奖池是玩家之间的输赢，见 game_room.go 与 game_jackpot.go。
//
// 每人每天、每个 IP 每天在每个游戏的计数分别记在 GameRewardDay 与 GameRewardIpDay；所有免费奖励的合计记在游戏标识为
// GameFreeCounter 的一行里。

const (
	GameSessionStatusPlaying  = "playing"
	GameSessionStatusFinished = "finished"
)

// GameFreeCounter 是所有免费奖励合计的计数行用的游戏标识，不会与真实的游戏标识重名。
const GameFreeCounter = "_free"

// 一局实际发放的游戏币少于成绩应得的数量时，对局上记下原因，重复提交时原样告诉玩家。
const (
	GameRewardLimitedBySpeed = "speed" // 操作快过人类极限，判定为脚本，不发奖励
	GameRewardLimitedByTime  = "time"  // 游戏时长不够成绩应得的数量
	GameRewardLimitedByDaily = "daily" // 账号今天在这个游戏的领奖次数已用完
	GameRewardLimitedByIp    = "ip"    // 同一 IP 今天在这个游戏的领奖次数已用完
	GameRewardLimitedByTotal = "total" // 今天所有免费奖励的合计已到上限
)

var (
	ErrGameSessionNotFound = errors.New("game session not found")
	// ErrGameSessionClosed 表示对局已不在进行中：已经结算过，或者被新开的一局作废删除了。
	ErrGameSessionClosed = errors.New("game session is no longer playing")
	// ErrGameSessionSuperseded 表示同一用户在这局之后又开了一局，这局不能再结算。
	ErrGameSessionSuperseded = errors.New("a newer game session has been started")
	// ErrGameFreePlayUsed 与 ErrGameIpFreePlayUsed 表示账号、同一 IP 下的账号今天在这个游戏里的免费次数已经用完。
	ErrGameFreePlayUsed   = errors.New("free plays of this game are used up today")
	ErrGameIpFreePlayUsed = errors.New("free plays of this game are used up today for this ip")
	// ErrGameFreeDailyLimit 表示账号或同一 IP 今天从所有免费奖励获得的游戏币已到合计上限。
	ErrGameFreeDailyLimit = errors.New("daily limit of free rewards reached")
	// ErrGameRoundOpen 表示玩家在这个游戏里还有一局没有结束，要先把它玩完。
	ErrGameRoundOpen = errors.New("an unfinished round of this game exists")
	// ErrGameRoundChanged 表示对局在读出之后被另一个请求改过或结束了，这次操作没有生效。
	ErrGameRoundChanged = errors.New("game round changed by another request")
)

// GameSession 是一局游戏。种子与状态只有服务端知道，接口只返回玩家能看到的部分。
// idx_game_session_best 让"最高成绩"与"进行中的对局"只读索引：下注游戏每玩一次记一局，同一用户的对局会很多。
type GameSession struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"index:idx_game_session_user_game,priority:1;index:idx_game_session_best,priority:1"`
	Game   string `json:"game" gorm:"type:varchar(32);index:idx_game_session_user_game,priority:2;index:idx_game_session_best,priority:2;index:idx_game_session_open,priority:2"`
	Seed   int64  `json:"-" gorm:"bigint"`
	Status string `json:"status" gorm:"type:varchar(16);index:idx_game_session_best,priority:3;index:idx_game_session_open,priority:1"`
	// Bet 是下注游戏这一局下注的游戏币(含中途加注)，免费游戏为 0。
	Bet int `json:"bet" gorm:"type:bigint;not null;default:0"`
	// Score 是成绩：技巧游戏是分数或赢下的难度，下注游戏是派彩。
	Score  int `json:"score" gorm:"type:bigint;index:idx_game_session_best,priority:4"`
	Reward int `json:"reward" gorm:"type:bigint"` // 实际发放的游戏币：免费游戏已按次数与上限扣减，下注游戏是派彩
	// LimitedBy 是 Reward 少于成绩应得数量的原因(GameRewardLimitedBy*)，没有少发时为空。
	LimitedBy string `json:"limited_by" gorm:"type:varchar(16)"`
	// Ip 是结算请求的来源 IP(IPv6 已归并到前缀)，这局的奖励计入它的领奖次数。
	Ip string `json:"ip" gorm:"type:varchar(64)"`
	// Detail 是各游戏自己的成绩明细(JSON)，供事后核查。
	Detail string `json:"detail" gorm:"type:varchar(255)"`
	// State 是对局在服务端保存的状态(JSON)，例如扫雷的雷区、21 点的牌堆，只有服务端知道。
	State string `json:"-" gorm:"type:text"`
	// Version 在每次改动 State 时加一：并发的两次操作只有一次能成功，另一次读到新状态后再来。
	Version    int   `json:"-" gorm:"not null;default:0"`
	CreatedAt  int64 `json:"created_at" gorm:"bigint;index;index:idx_game_session_open,priority:3"`
	FinishedAt int64 `json:"finished_at" gorm:"bigint"`
}

// GameRewardDay 是每个用户每天在每个游戏的计数：Coins 是免费游戏发放的游戏币，或下注、对战的净输赢(可以为负)；Plays 是结算了
// 几局；FreePlays 是免费游戏领了几次奖，大转盘是用了几次免费次数。领奖前先锁住这一行，同一用户的几局同时结算也不会越过次数。
type GameRewardDay struct {
	UserId    int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Game      string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day       string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins     int    `json:"coins" gorm:"type:bigint;not null;default:0"`
	Plays     int    `json:"plays" gorm:"not null;default:0"`
	FreePlays int    `json:"free_plays" gorm:"not null;default:0"`
}

// GameRewardIpDay 是每个 IP 每天在每个免费游戏领奖的计数，同一 IP 下所有账号合计。站点的用户各自使用独立的出口 IP，
// 同一个 IP 就视为同一个人：它与账号共用领奖次数和合计上限，多开账号领不到更多奖励。
type GameRewardIpDay struct {
	Ip        string `json:"ip" gorm:"type:varchar(64);primaryKey"`
	Game      string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day       string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins     int    `json:"coins" gorm:"type:bigint;not null;default:0"`
	Plays     int    `json:"plays" gorm:"not null;default:0"`
	FreePlays int    `json:"free_plays" gorm:"not null;default:0"`
}

// GameDayCount 是一行计数里的数字，含义见 GameRewardDay。
type GameDayCount struct {
	Coins     int
	Plays     int
	FreePlays int
}

// gameCounterDelta 是记到一行计数上的变化。MaxFreePlays、MaxCoins 大于 0 时，条件更新保证累加之后领奖次数、游戏币
// 不越过它们：行锁之外再兜一次底。
type gameCounterDelta struct {
	Coins        int
	Plays        int
	FreePlays    int
	MaxFreePlays int
	MaxCoins     int
}

// GameSettlement 是技巧游戏一局的结算：各游戏按自己的规则算出成绩和应得的游戏币，交给 FinishGameSession 按领奖次数与
// 合计上限发放。
type GameSettlement struct {
	Score  int
	Detail string
	// State 不为空时替换对局里保存的状态，例如结束后记下整张雷区，供事后核查。
	State string
	// Reward 是按成绩与游戏规则应得的游戏币，还没有按领奖次数与合计上限扣减；LimitedBy 说明游戏规则为什么给得比成绩应得的少。
	Reward    int
	LimitedBy string
	// DailyRewards 是每人每天、每个 IP 每天最多领几次这个游戏的奖励，0 表示不发奖励。
	DailyRewards int
	// FreeDailyLimit 是所有免费奖励每人每天、每个 IP 每天合计最多发放的游戏币，0 表示不限制。
	FreeDailyLimit int
	Day            string
	Ip             string
}

// GameRewardProgress 是账号某天在一个免费游戏里的进度：Plays 是结算了几局，Rewards、IpRewards 是账号、同一 IP 下所有账号
// 领了几次奖，Earned 是账号从这个游戏获得的游戏币；FreeEarned、IpFreeEarned 是账号、同一 IP 从所有免费奖励获得的合计。
type GameRewardProgress struct {
	Plays        int `json:"plays_today"`
	Rewards      int `json:"rewards_today"`
	IpRewards    int `json:"ip_rewards_today"`
	Earned       int `json:"earned_today"`
	FreeEarned   int `json:"free_earned_today"`
	IpFreeEarned int `json:"ip_free_earned_today"`
}

// GameSettlementResult 是免费游戏一局的结算结果：Session 的 Reward 是实际发放的数量，进度是结算之后的。
type GameSettlementResult struct {
	Session *GameSession
	Balance int
	GameRewardProgress
}

// GameFreePlay 是当场决定结果的一次免费奖励，例如大转盘的免费旋转。
type GameFreePlay struct {
	// FreeLimit 是每人每天、每个 IP 每天的免费次数。
	FreeLimit int
	// FreeDailyLimit 是所有免费奖励每天的合计上限，0 表示不限制。
	FreeDailyLimit int
	// Play 决定这一次的奖品与明细，在锁住计数、确认还有免费次数之后才调用。
	Play func() (win int, detail string, err error)
	Day  string
	Ip   string
}

// GameWager 是当场决定结果的一次下注：由 Play 按游戏规则决定派彩(含退回的下注)与明细，SettleGameWager 扣注、派彩。
// State 是存进对局的完整下注内容(例如轮盘的整张注单)，放不进 255 字节的明细时用它，可以为空。
type GameWager struct {
	Bet   int
	Play  func() (win int, detail string, err error)
	State string
	Day   string
	Ip    string
}

// GameWagerResult 是一局下注游戏的结算结果。Session 的 Score 与 Reward 都是派彩；Net、Plays 是结算后账号今天在这个游戏的
// 净输赢与局数。
type GameWagerResult struct {
	Session *GameSession
	Balance int
	Net     int
	Plays   int
}

// GameRound 是分几步完成的下注游戏开局时的下注与状态。
type GameRound struct {
	Bet    int
	State  string
	Detail string
	Ip     string
}

// CreateGameSession 开一局免费游戏。每人每个游戏同一时间只能进行一局：之前没结算的对局在这里作废删除，脚本没法同时
// 开几局来压缩所需的游戏时长，中途离开的对局也不会一直留在库里。state 是服务端保存的对局状态，不需要时为空。
// 下注游戏的对局押着游戏币，不能这样作废，见 OpenGameRound。
func CreateGameSession(userId int, game string, seed uint32, state string) (*GameSession, error) {
	session := &GameSession{
		UserId:    userId,
		Game:      game,
		Seed:      int64(seed),
		Status:    GameSessionStatusPlaying,
		State:     state,
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

// GetPlayingGameSession 返回用户在某个游戏里进行中的一局，没有时返回 nil。
func GetPlayingGameSession(userId int, game string) (*GameSession, error) {
	var sessions []GameSession
	err := DB.Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusPlaying).
		Order("id desc").Limit(1).Find(&sessions).Error
	if err != nil || len(sessions) == 0 {
		return nil, err
	}
	return &sessions[0], nil
}

// ListUserFinishedGameSessions 返回玩家在某个游戏里最近结束的几局，从新到旧。
func ListUserFinishedGameSessions(userId int, game string, limit int) ([]*GameSession, error) {
	var sessions []*GameSession
	err := DB.Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusFinished).
		Order("id desc").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// ListStalePlayingGameSessions 返回某个游戏里开局早于 before、还没结束的对局，从旧到新，最多 limit 局。系统任务用它结算
// 玩家离开之后自己结束的对局(例如火箭)。
func ListStalePlayingGameSessions(game string, before int64, limit int) ([]*GameSession, error) {
	var sessions []*GameSession
	err := DB.Where("status = ? AND game = ? AND created_at < ?", GameSessionStatusPlaying, game, before).
		Order("id asc").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// UpdateGameSessionState 保存进行中对局的新状态。只有 session.Version 仍是库里的版本时才会成功，否则返回
// ErrGameRoundChanged；成功后 session 的状态与版本随之更新。
func UpdateGameSessionState(session *GameSession, state string, detail string) error {
	result := DB.Model(&GameSession{}).
		Where("id = ? AND status = ? AND version = ?", session.Id, GameSessionStatusPlaying, session.Version).
		Updates(map[string]interface{}{"state": state, "detail": detail, "version": session.Version + 1})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGameRoundChanged
	}
	session.State, session.Detail, session.Version = state, detail, session.Version+1
	return nil
}

// FinishGameSession 结算一局免费游戏：把对局从进行中切换为已结束，同一局只有一次能成功；成绩达标时按账号和 IP 今天剩余的
// 领奖次数、所有免费奖励的合计上限发放奖励。对局已不在进行中时返回 ErrGameSessionClosed，调用方重新读取对局就能知道
// 它是已经结算还是被作废了。
func FinishGameSession(session *GameSession, settlement GameSettlement) (*GameSettlementResult, error) {
	finished := *session
	finished.Status = GameSessionStatusFinished
	finished.Score = settlement.Score
	finished.Detail = settlement.Detail
	finished.LimitedBy = settlement.LimitedBy
	finished.Ip = settlement.Ip
	finished.FinishedAt = common.GetTimestamp()
	if settlement.State != "" {
		finished.State = settlement.State
	}
	result := &GameSettlementResult{Session: &finished}
	counters := newGameFreeCounters(session.UserId, settlement.Ip, session.Game, settlement.Day)
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 先锁住计数再切换对局状态：同一用户、同一 IP 的几局同时结算时依次进行，都不会越过次数与上限。
		counts, err := counters.lock(tx)
		if err != nil {
			return err
		}
		granted, limitedBy := counts.grant(max(settlement.Reward, 0), settlement.DailyRewards, settlement.FreeDailyLimit)
		if limitedBy != "" {
			finished.LimitedBy = limitedBy
		}
		finished.Reward = granted

		update := tx.Model(&GameSession{}).
			Where("id = ? AND status = ?", session.Id, GameSessionStatusPlaying).
			Updates(map[string]interface{}{
				"status":      finished.Status,
				"score":       finished.Score,
				"reward":      finished.Reward,
				"limited_by":  finished.LimitedBy,
				"ip":          finished.Ip,
				"detail":      finished.Detail,
				"state":       finished.State,
				"finished_at": finished.FinishedAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
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

		if err := counters.record(tx, granted, settlement.DailyRewards, settlement.FreeDailyLimit); err != nil {
			return err
		}
		result.GameRewardProgress = counts.progress(granted)
		if granted == 0 {
			result.Balance, err = readGameCoinBalanceTx(tx, session.UserId)
			return err
		}
		result.Balance, err = creditGameCoinsTx(tx, session.UserId, granted, map[string]interface{}{
			"total_earned": gorm.Expr("total_earned + ?", granted),
		})
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    session.UserId,
			Type:      GameCoinLogTypeGameReward,
			Amount:    granted,
			Balance:   result.Balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    session.Game,
			CreatedAt: finished.FinishedAt,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SettleGameFreePlay 结算当场决定结果的一次免费奖励(大转盘的免费旋转)：账号或同一 IP 今天的免费次数已经用完、所有免费奖励
// 已到合计上限时拒绝，什么都不会改变；否则决定结果、发奖、记对局与流水在同一个事务里。奖品超出合计上限剩余的部分不发放，
// 对局上记下原因；没中奖时余额没有变动，不记流水。
func SettleGameFreePlay(userId int, game string, play GameFreePlay) (*GameSettlementResult, error) {
	if play.FreeLimit < 0 || play.FreeDailyLimit < 0 {
		return nil, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	session := &GameSession{
		UserId:     userId,
		Game:       game,
		Status:     GameSessionStatusFinished,
		Ip:         play.Ip,
		CreatedAt:  now,
		FinishedAt: now,
	}
	result := &GameSettlementResult{Session: session}
	counters := newGameFreeCounters(userId, play.Ip, game, play.Day)
	err := DB.Transaction(func(tx *gorm.DB) error {
		counts, err := counters.lock(tx)
		if err != nil {
			return err
		}
		switch {
		case counts.user.FreePlays >= play.FreeLimit:
			return ErrGameFreePlayUsed
		case counts.ip.FreePlays >= play.FreeLimit:
			return ErrGameIpFreePlayUsed
		case play.FreeDailyLimit > 0 && counts.freeRoom(play.FreeDailyLimit) <= 0:
			return ErrGameFreeDailyLimit
		}
		win, detail, err := play.Play()
		if err != nil {
			return err
		}
		if win < 0 || win >= common.MaxQuota {
			return ErrGameCoinAmountInvalid
		}
		session.Score, session.Detail = win, detail
		if play.FreeDailyLimit > 0 {
			if room := counts.freeRoom(play.FreeDailyLimit); win > room {
				win, session.LimitedBy = room, GameRewardLimitedByTotal
			}
		}
		session.Reward = win

		// 免费次数用掉一次，无论中没中奖。
		if err := bumpGameCounter(tx, &GameRewardDay{}, counters.userKey, gameCounterDelta{Coins: win, Plays: 1, FreePlays: 1, MaxFreePlays: play.FreeLimit}); err != nil {
			return err
		}
		if err := bumpGameCounter(tx, &GameRewardIpDay{}, counters.ipKey, gameCounterDelta{Coins: win, Plays: 1, FreePlays: 1, MaxFreePlays: play.FreeLimit}); err != nil {
			return err
		}
		if err := counters.recordFree(tx, win, play.FreeDailyLimit); err != nil {
			return err
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		result.GameRewardProgress = counts.progress(win)
		result.Rewards, result.IpRewards = counts.user.FreePlays+1, counts.ip.FreePlays+1
		if win == 0 {
			result.Balance, err = readGameCoinBalanceTx(tx, userId)
			return err
		}
		result.Balance, err = creditGameCoinsTx(tx, userId, win, map[string]interface{}{
			"total_earned": gorm.Expr("total_earned + ?", win),
		})
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameReward,
			Amount:    win,
			Balance:   result.Balance,
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

// SettleGameWager 结算当场决定结果的一次下注：决定结果、扣注、派彩、记对局与流水在同一个事务里。余额不够下注时返回
// ErrGameCoinInsufficient，什么都不会改变。下注游戏不设每日上限，计数只用来显示今天的输赢。
func SettleGameWager(userId int, game string, wager GameWager) (*GameWagerResult, error) {
	if wager.Bet < 1 || wager.Bet >= common.MaxQuota {
		return nil, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	session := &GameSession{
		UserId:     userId,
		Game:       game,
		Status:     GameSessionStatusFinished,
		Bet:        wager.Bet,
		Ip:         wager.Ip,
		State:      wager.State,
		CreatedAt:  now,
		FinishedAt: now,
	}
	result := &GameWagerResult{Session: session}
	err := DB.Transaction(func(tx *gorm.DB) error {
		win, detail, err := wager.Play()
		if err != nil {
			return err
		}
		if win < 0 || win >= common.MaxQuota {
			return ErrGameCoinAmountInvalid
		}
		session.Score, session.Reward, session.Detail = win, win, detail
		net := win - wager.Bet
		result.Balance, err = debitGameCoinsTx(tx, userId, wager.Bet, map[string]interface{}{
			"total_wager_net": gorm.Expr("total_wager_net + ?", net),
			"total_wagered":   gorm.Expr("total_wagered + ?", wager.Bet),
		})
		if err != nil {
			return err
		}
		if win > 0 {
			if result.Balance, err = creditGameCoinsTx(tx, userId, win, nil); err != nil {
				return err
			}
		}
		if result.Net, result.Plays, err = recordGameDayResult(tx, userId, game, wager.Day, net); err != nil {
			return err
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    net,
			Balance:   result.Balance,
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

// OpenGameRound 开一局分几步完成的下注游戏：扣下注、记一局进行中的对局与它的状态，流水记下扣掉的下注。每人每个游戏
// 同一时间只能有一局进行中，还有没结束的一局时返回 ErrGameRoundOpen，玩家要先回去把它玩完。
func OpenGameRound(userId int, game string, round GameRound) (*GameSession, int, error) {
	if round.Bet < 1 || round.Bet >= common.MaxQuota {
		return nil, 0, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	session := &GameSession{
		UserId:    userId,
		Game:      game,
		Status:    GameSessionStatusPlaying,
		Bet:       round.Bet,
		Ip:        round.Ip,
		Detail:    round.Detail,
		State:     round.State,
		CreatedAt: now,
	}
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 先锁住账户再查有没有进行中的一局：同一玩家同时开两局时依次进行，第二局能看到第一局。
		var balances []int
		if err := lockForUpdate(tx).Model(&GameCoinAccount{}).Where("user_id = ?", userId).Pluck("balance", &balances).Error; err != nil {
			return err
		}
		var open int64
		if err := tx.Model(&GameSession{}).Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusPlaying).Count(&open).Error; err != nil {
			return err
		}
		if open > 0 {
			return ErrGameRoundOpen
		}
		var err error
		balance, err = debitGameCoinsTx(tx, userId, round.Bet, map[string]interface{}{
			"total_wagered": gorm.Expr("total_wagered + ?", round.Bet),
		})
		if err != nil {
			return err
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    -round.Bet,
			Balance:   balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    game,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, 0, err
	}
	return session, balance, nil
}

// RaiseGameRound 在进行中的一局里追加下注(例如 21 点的加倍、分牌)，同时保存新状态。session.Version 已不是库里的版本时
// 返回 ErrGameRoundChanged，余额不够时返回 ErrGameCoinInsufficient，两种情况都什么都不会改变。
func RaiseGameRound(session *GameSession, extra int, state string, detail string) (int, error) {
	if extra < 1 || extra >= common.MaxQuota || session.Bet+extra >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&GameSession{}).
			Where("id = ? AND status = ? AND version = ?", session.Id, GameSessionStatusPlaying, session.Version).
			Updates(map[string]interface{}{
				"bet":     gorm.Expr("bet + ?", extra),
				"state":   state,
				"detail":  detail,
				"version": session.Version + 1,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrGameRoundChanged
		}
		var err error
		balance, err = debitGameCoinsTx(tx, session.UserId, extra, map[string]interface{}{
			"total_wagered": gorm.Expr("total_wagered + ?", extra),
		})
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    session.UserId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    -extra,
			Balance:   balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    session.Game,
			CreatedAt: common.GetTimestamp(),
		}).Error
	})
	if err != nil {
		return 0, err
	}
	session.Bet, session.State, session.Detail, session.Version = session.Bet+extra, state, detail, session.Version+1
	return balance, nil
}

// GameRoundSettlement 是一局分几步完成的下注游戏的结算。Raise 是让这一局结束的最后一步追加的下注(例如 21 点加倍之后
// 这一局直接结束)，和派彩在同一个事务里扣下；Win 是派彩(含退回的下注)。
type GameRoundSettlement struct {
	Raise  int
	Win    int
	State  string
	Detail string
	Day    string
}

// SettleGameRound 结束一局分几步完成的下注游戏：先扣下最后一步追加的下注，再派彩。只有 session.Version 仍是库里的版本时
// 才会成功，否则返回 ErrGameRoundChanged：同一局只结算一次。余额不够追加时返回 ErrGameCoinInsufficient，什么都不会改变。
// 流水记下追加的下注与派彩，没有的不记。
func SettleGameRound(session *GameSession, settlement GameRoundSettlement) (*GameWagerResult, error) {
	win, raise := settlement.Win, settlement.Raise
	if win < 0 || win >= common.MaxQuota || raise < 0 || raise >= common.MaxQuota || session.Bet+raise >= common.MaxQuota {
		return nil, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	settled := *session
	settled.Status = GameSessionStatusFinished
	settled.Bet = session.Bet + raise
	settled.Score, settled.Reward = win, win
	settled.State, settled.Detail = settlement.State, settlement.Detail
	settled.FinishedAt = now
	settled.Version = session.Version + 1
	result := &GameWagerResult{Session: &settled}
	err := DB.Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&GameSession{}).
			Where("id = ? AND status = ? AND version = ?", session.Id, GameSessionStatusPlaying, session.Version).
			Updates(map[string]interface{}{
				"status":      settled.Status,
				"bet":         settled.Bet,
				"score":       settled.Score,
				"reward":      settled.Reward,
				"state":       settled.State,
				"detail":      settled.Detail,
				"version":     settled.Version,
				"finished_at": settled.FinishedAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrGameRoundChanged
		}
		if raise > 0 {
			balance, err := debitGameCoinsTx(tx, session.UserId, raise, map[string]interface{}{
				"total_wagered": gorm.Expr("total_wagered + ?", raise),
			})
			if err != nil {
				return err
			}
			if err := tx.Create(&GameCoinLog{
				UserId:    session.UserId,
				Type:      GameCoinLogTypeGameWager,
				Amount:    -raise,
				Balance:   balance,
				RefId:     strconv.Itoa(session.Id),
				Remark:    session.Game,
				CreatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		net := win - settled.Bet
		var err error
		if win > 0 {
			result.Balance, err = creditGameCoinsTx(tx, session.UserId, win, map[string]interface{}{
				"total_wager_net": gorm.Expr("total_wager_net + ?", net),
			})
		} else {
			err = tx.Model(&GameCoinAccount{}).Where("user_id = ?", session.UserId).Updates(map[string]interface{}{
				"total_wager_net": gorm.Expr("total_wager_net + ?", net),
				"updated_at":      now,
			}).Error
			if err == nil {
				result.Balance, err = readGameCoinBalanceTx(tx, session.UserId)
			}
		}
		if err != nil {
			return err
		}
		if result.Net, result.Plays, err = recordGameDayResult(tx, session.UserId, session.Game, settlement.Day, net); err != nil {
			return err
		}
		if win == 0 {
			return nil
		}
		return tx.Create(&GameCoinLog{
			UserId:    session.UserId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    win,
			Balance:   result.Balance,
			RefId:     strconv.Itoa(session.Id),
			Remark:    session.Game,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// gameFreeCounters 是结算一次免费奖励要用到的四行计数：账号与 IP 在这个游戏的计数，以及账号与 IP 所有免费奖励的合计。
// 总是按这个顺序加锁，同时结算的几局不会互相等待成环。
type gameFreeCounters struct {
	userKey, ipKey    map[string]interface{}
	userFree, ipFree  map[string]interface{}
	userRow, userFRow *GameRewardDay
	ipRow, ipFRow     *GameRewardIpDay
}

// gameFreeCounts 是锁住的四行计数里的数字。
type gameFreeCounts struct {
	user, ip, userFree, ipFree GameDayCount
}

func newGameFreeCounters(userId int, ip string, game string, day string) *gameFreeCounters {
	return &gameFreeCounters{
		userKey:  map[string]interface{}{"user_id": userId, "game": game, "day": day},
		ipKey:    map[string]interface{}{"ip": ip, "game": game, "day": day},
		userFree: map[string]interface{}{"user_id": userId, "game": GameFreeCounter, "day": day},
		ipFree:   map[string]interface{}{"ip": ip, "game": GameFreeCounter, "day": day},
		userRow:  &GameRewardDay{UserId: userId, Game: game, Day: day},
		userFRow: &GameRewardDay{UserId: userId, Game: GameFreeCounter, Day: day},
		ipRow:    &GameRewardIpDay{Ip: ip, Game: game, Day: day},
		ipFRow:   &GameRewardIpDay{Ip: ip, Game: GameFreeCounter, Day: day},
	}
}

func (c *gameFreeCounters) lock(tx *gorm.DB) (*gameFreeCounts, error) {
	counts := &gameFreeCounts{}
	var err error
	if counts.user, err = lockGameRewardCounter(tx, c.userRow, c.userKey); err != nil {
		return nil, err
	}
	if counts.ip, err = lockGameRewardCounter(tx, c.ipRow, c.ipKey); err != nil {
		return nil, err
	}
	if counts.userFree, err = lockGameRewardCounter(tx, c.userFRow, c.userFree); err != nil {
		return nil, err
	}
	if counts.ipFree, err = lockGameRewardCounter(tx, c.ipFRow, c.ipFree); err != nil {
		return nil, err
	}
	return counts, nil
}

// record 给一局技巧游戏记账：账号的局数加一；发了奖时，账号与 IP 的领奖次数各加一，四行都累加发放的游戏币。
func (c *gameFreeCounters) record(tx *gorm.DB, granted int, dailyRewards int, freeDailyLimit int) error {
	if granted == 0 {
		return bumpGameCounter(tx, &GameRewardDay{}, c.userKey, gameCounterDelta{Plays: 1})
	}
	if err := bumpGameCounter(tx, &GameRewardDay{}, c.userKey, gameCounterDelta{Coins: granted, Plays: 1, FreePlays: 1, MaxFreePlays: dailyRewards}); err != nil {
		return err
	}
	if err := bumpGameCounter(tx, &GameRewardIpDay{}, c.ipKey, gameCounterDelta{Coins: granted, Plays: 1, FreePlays: 1, MaxFreePlays: dailyRewards}); err != nil {
		return err
	}
	return c.recordFree(tx, granted, freeDailyLimit)
}

// recordFree 把发放的免费奖励累加到账号与 IP 的合计上，设了合计上限时条件更新保证不越过它。
func (c *gameFreeCounters) recordFree(tx *gorm.DB, granted int, freeDailyLimit int) error {
	if granted == 0 {
		return nil
	}
	if err := bumpGameCounter(tx, &GameRewardDay{}, c.userFree, gameCounterDelta{Coins: granted, FreePlays: 1, MaxCoins: freeDailyLimit}); err != nil {
		return err
	}
	return bumpGameCounter(tx, &GameRewardIpDay{}, c.ipFree, gameCounterDelta{Coins: granted, FreePlays: 1, MaxCoins: freeDailyLimit})
}

// grant 按今天的计数决定一局实际发放多少：账号或同一 IP 在这个游戏的领奖次数用完就不发；设了合计上限时最多发到上限为止。
// 返回发放的数量和少发的原因(没有少发时为空)。
func (n *gameFreeCounts) grant(reward int, dailyRewards int, freeDailyLimit int) (int, string) {
	switch {
	case reward == 0:
		return 0, ""
	case n.user.FreePlays >= dailyRewards:
		return 0, GameRewardLimitedByDaily
	case n.ip.FreePlays >= dailyRewards:
		return 0, GameRewardLimitedByIp
	case freeDailyLimit > 0:
		if room := n.freeRoom(freeDailyLimit); room < reward {
			return max(room, 0), GameRewardLimitedByTotal
		}
	}
	return reward, ""
}

// freeRoom 返回账号与同一 IP 今天离所有免费奖励的合计上限还差多少，取两者中较少的。
func (n *gameFreeCounts) freeRoom(freeDailyLimit int) int {
	return min(freeDailyLimit-n.userFree.Coins, freeDailyLimit-n.ipFree.Coins)
}

// progress 返回发放 granted 之后的进度。
func (n *gameFreeCounts) progress(granted int) GameRewardProgress {
	progress := GameRewardProgress{
		Plays:        n.user.Plays + 1,
		Rewards:      n.user.FreePlays,
		IpRewards:    n.ip.FreePlays,
		Earned:       n.user.Coins + granted,
		FreeEarned:   n.userFree.Coins + granted,
		IpFreeEarned: n.ipFree.Coins + granted,
	}
	if granted > 0 {
		progress.Rewards++
		progress.IpRewards++
	}
	return progress
}

// lockGameRewardCounter 锁住一个计数行并返回其中的数字，行不存在时先建出来。row 是要建的行，key 是它的主键列。
func lockGameRewardCounter(tx *gorm.DB, row interface{}, key map[string]interface{}) (GameDayCount, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return GameDayCount{}, err
	}
	var count GameDayCount
	err := lockForUpdate(tx).Model(row).Where(key).Select("coins, plays, free_plays").Scan(&count).Error
	return count, err
}

// bumpGameCounter 给一行已存在的计数累加 delta，条件更新保证不越过 delta 里的上限。
func bumpGameCounter(tx *gorm.DB, table interface{}, key map[string]interface{}, delta gameCounterDelta) error {
	query := tx.Model(table).Where(key)
	updates := map[string]interface{}{}
	if delta.Coins != 0 {
		updates["coins"] = gorm.Expr("coins + ?", delta.Coins)
		if delta.MaxCoins > 0 {
			query = query.Where("coins <= ?", delta.MaxCoins-delta.Coins)
		}
	}
	if delta.Plays != 0 {
		updates["plays"] = gorm.Expr("plays + ?", delta.Plays)
	}
	if delta.FreePlays != 0 {
		updates["free_plays"] = gorm.Expr("free_plays + ?", delta.FreePlays)
		if delta.MaxFreePlays > 0 {
			query = query.Where("free_plays <= ?", delta.MaxFreePlays-delta.FreePlays)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("game counter changed during settlement")
	}
	return nil
}

// recordGameDayResult 给账号今天在这个游戏的计数记上一局下注或对战：局数加一、净输赢累加 net，返回累加之后的净输赢与局数。
func recordGameDayResult(tx *gorm.DB, userId int, game string, day string, net int) (int, int, error) {
	row := &GameRewardDay{UserId: userId, Game: game, Day: day}
	key := map[string]interface{}{"user_id": userId, "game": game, "day": day}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return 0, 0, err
	}
	if err := bumpGameCounter(tx, &GameRewardDay{}, key, gameCounterDelta{Coins: net, Plays: 1}); err != nil {
		return 0, 0, err
	}
	var count GameDayCount
	err := tx.Model(&GameRewardDay{}).Where(key).Select("coins, plays, free_plays").Scan(&count).Error
	return count.Coins, count.Plays, err
}

// GetGameRewardProgress 返回账号某天在一个免费游戏里的进度，ip 是账号当前的 IP。
func GetGameRewardProgress(userId int, ip string, game string, day string) (GameRewardProgress, error) {
	var user, byIp, userFree, ipFree GameDayCount
	reads := []struct {
		table interface{}
		key   map[string]interface{}
		into  *GameDayCount
	}{
		{&GameRewardDay{}, map[string]interface{}{"user_id": userId, "game": game, "day": day}, &user},
		{&GameRewardIpDay{}, map[string]interface{}{"ip": ip, "game": game, "day": day}, &byIp},
		{&GameRewardDay{}, map[string]interface{}{"user_id": userId, "game": GameFreeCounter, "day": day}, &userFree},
		{&GameRewardIpDay{}, map[string]interface{}{"ip": ip, "game": GameFreeCounter, "day": day}, &ipFree},
	}
	for _, read := range reads {
		if err := DB.Model(read.table).Where(read.key).Select("coins, plays, free_plays").Scan(read.into).Error; err != nil {
			return GameRewardProgress{}, err
		}
	}
	return GameRewardProgress{
		Plays:        user.Plays,
		Rewards:      user.FreePlays,
		IpRewards:    byIp.FreePlays,
		Earned:       user.Coins,
		FreeEarned:   userFree.Coins,
		IpFreeEarned: ipFree.Coins,
	}, nil
}

// GetUserGameDayCounts 返回账号某天在所有游戏的计数，键是游戏标识。
func GetUserGameDayCounts(userId int, day string) (map[string]GameDayCount, error) {
	var rows []GameRewardDay
	if err := DB.Where("user_id = ? AND day = ?", userId, day).Find(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]GameDayCount, len(rows))
	for _, row := range rows {
		counts[row.Game] = GameDayCount{Coins: row.Coins, Plays: row.Plays, FreePlays: row.FreePlays}
	}
	return counts, nil
}

// GetIpGameDayCounts 返回某个 IP 下所有账号某天在所有免费游戏的领奖计数，键是游戏标识。
func GetIpGameDayCounts(ip string, day string) (map[string]GameDayCount, error) {
	var rows []GameRewardIpDay
	if err := DB.Where("ip = ? AND day = ?", ip, day).Find(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]GameDayCount, len(rows))
	for _, row := range rows {
		counts[row.Game] = GameDayCount{Coins: row.Coins, Plays: row.Plays, FreePlays: row.FreePlays}
	}
	return counts, nil
}

// GetUserGameBestScores 返回用户在每个游戏里结算过的最高成绩，键是游戏标识。
func GetUserGameBestScores(userId int) (map[string]int, error) {
	var rows []struct {
		Game string
		Best int
	}
	err := DB.Model(&GameSession{}).Select("game, MAX(score) AS best").
		Where("user_id = ? AND status = ?", userId, GameSessionStatusFinished).Group("game").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	best := make(map[string]int, len(rows))
	for _, row := range rows {
		best[row.Game] = row.Best
	}
	return best, nil
}

// GetGameDayStats 返回账号某天在一个游戏的净输赢(免费游戏是获得的游戏币)与局数。
func GetGameDayStats(userId int, game string, day string) (coins int, plays int, err error) {
	var count GameDayCount
	err = DB.Model(&GameRewardDay{}).Where("user_id = ? AND game = ? AND day = ?", userId, game, day).
		Select("coins, plays, free_plays").Scan(&count).Error
	return count.Coins, count.Plays, err
}

// GetGameBestScore 返回用户在某个游戏里结算过的最高成绩，下注游戏是单局最高派彩；还没有结算过时为 0。
func GetGameBestScore(userId int, game string) (int, error) {
	var best int
	err := DB.Model(&GameSession{}).
		Where("user_id = ? AND game = ? AND status = ?", userId, game, GameSessionStatusFinished).
		Select("COALESCE(MAX(score), 0)").
		Scan(&best).Error
	return best, err
}

// SumGameRewardsByGame 汇总某天各游戏的净发放，供管理端概览：免费游戏是发出的游戏币，下注、对战与奖池是所有玩家的净输赢
// 合计，为负表示玩家合计输掉的数量；GameFreeCounter 一行是所有免费奖励的合计。
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
