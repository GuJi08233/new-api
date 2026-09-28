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
// 成绩达标、并且账号当天在这个游戏里累计玩够时长才发奖(FinishGameSession)。技巧游戏与人机对战每人每个游戏每天只能领几次奖，
// 同一 IP 下所有账号合计也只有这么多次；这些奖励每人每天(同一 IP 合计)还可以设一个总上限。
// 大转盘的免费旋转当场决定结果并全额发奖(SettleGameFreePlay)，只限制账号与 IP 的免费次数，不受上述总上限影响，也不占用它。
//
// 下注游戏花游戏币下注、会亏：理论返还率低于 100%，玩家长期是输的，所以不限次数、不设每日上限。当场决定结果的下注在一个
// 事务里扣注、派彩(SettleGameWager)；要分几步完成的(例如 21 点、挖宝)开局时扣注，把只有服务端知道的状态存在对局里，
// 结束时派彩(OpenGameRound、SettleGameRound)。
//
// 对战与奖池是玩家之间的输赢，见 game_room.go 与 game_jackpot.go。
//
// 每人每天、每个 IP 每天在每个游戏的计数分别记在 GameRewardDay 与 GameRewardIpDay；技巧游戏与人机对战奖励的合计记在游戏标识为
// GameFreeCounter 的一行里，大转盘奖励不计入该合计。

const (
	GameSessionStatusPlaying  = "playing"
	GameSessionStatusFinished = "finished"
)

// GameFreeCounter 是技巧游戏与人机对战奖励合计的计数行用的游戏标识，不会与真实的游戏标识重名。
const GameFreeCounter = "_free"

// 一局实际发放的游戏币少于成绩应得的数量时，对局上记下原因，重复提交时原样告诉玩家。
const (
	GameRewardLimitedBySpeed = "speed" // 操作快过人类极限，判定为脚本，不发奖励
	GameRewardLimitedByTime  = "time"  // 账号今天在这个游戏累计的游戏时长不够成绩应得的数量
	GameRewardLimitedByDaily = "daily" // 账号今天在这个游戏的领奖次数已用完
	GameRewardLimitedByIp    = "ip"    // 同一 IP 今天在这个游戏的领奖次数已用完
	GameRewardLimitedByTotal = "total" // 今天技巧游戏与人机对战奖励的合计已到上限
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
	Reward int `json:"reward" gorm:"type:bigint"` // 实际发放的游戏币：技巧与人机奖励受次数与上限限制，大转盘及下注游戏是派彩
	// LimitedBy 是 Reward 少于成绩应得数量的原因(GameRewardLimitedBy*)，没有少发时为空。
	LimitedBy string `json:"limited_by" gorm:"type:varchar(16)"`
	// Ip 是结算请求的来源 IP(IPv6 已归并到前缀)，这局的奖励计入它的领奖次数。
	Ip string `json:"ip" gorm:"type:varchar(64)"`
	// Detail 是各游戏自己的成绩明细(JSON)，供事后核查。
	Detail string `json:"detail" gorm:"type:varchar(255)"`
	// State 是对局在服务端保存的状态(JSON)，例如扫雷的雷区、21 点的牌堆，只有服务端知道。
	State string `json:"-" gorm:"type:text"`
	// Version 在每次改动 State 时加一：并发的两次操作只有一次能成功，另一次读到新状态后再来。
	Version int `json:"-" gorm:"not null;default:0"`
	// PlaySeconds 与 PlayedAt 按操作累计一局的游戏时长(见 PlayedSeconds)：每保存一步，把离上一步(第一步离开局)的间隔
	// 计入 PlaySeconds，PlayedAt 记为这一步的时间。
	PlaySeconds int   `json:"-" gorm:"not null;default:0"`
	PlayedAt    int64 `json:"-" gorm:"bigint"`
	CreatedAt   int64 `json:"created_at" gorm:"bigint;index;index:idx_game_session_open,priority:3"`
	FinishedAt  int64 `json:"finished_at" gorm:"bigint"`
}

// GameActionGapSeconds 是一局里两步操作之间最多算作游戏时长的秒数：停下来想一想算在玩，开了局走开挂机不算。
const GameActionGapSeconds = 60

// PlayedSeconds 返回这一局到 now 为止按操作累计的游戏时长：之前每两步之间的间隔各按不超过 GameActionGapSeconds 计，
// 再加上最后一步(一步都还没走时是开局)到 now 的间隔，同样不超过 GameActionGapSeconds。
func (s *GameSession) PlayedSeconds(now int64) int {
	last := s.PlayedAt
	if last == 0 {
		last = s.CreatedAt
	}
	return s.PlaySeconds + int(min(max(now-last, 0), GameActionGapSeconds))
}

// GameRewardDay 是每个用户每天在每个游戏的计数：Coins 是免费游戏发放的游戏币，或下注、对战的净输赢(可以为负)；Plays 是结算了
// 几局；FreePlays 是免费游戏领了几次奖，大转盘是用了几次免费次数。领奖前先锁住这一行，同一用户的几局同时结算也不会越过次数。
// Seconds 与 PendingReward 只用于技巧游戏与人机对战：Seconds 是今天结算过的对局累计的游戏时长(秒)，PendingReward 是上次领奖
// 之后达标对局里最高的应得奖励，累计时长还不够时先记在这里，之后的对局把时长补够再发。
type GameRewardDay struct {
	UserId        int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Game          string `json:"game" gorm:"type:varchar(32);primaryKey"`
	Day           string `json:"day" gorm:"type:varchar(10);primaryKey;index"`
	Coins         int    `json:"coins" gorm:"type:bigint;not null;default:0"`
	Plays         int    `json:"plays" gorm:"not null;default:0"`
	FreePlays     int    `json:"free_plays" gorm:"not null;default:0"`
	Seconds       int    `json:"seconds" gorm:"not null;default:0"`
	PendingReward int    `json:"pending_reward" gorm:"not null;default:0"`
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

// GameDayCount 是一行计数里的数字，含义见 GameRewardDay。GameRewardIpDay 没有 Seconds 与 PendingReward，读出来总是 0。
type GameDayCount struct {
	Coins         int
	Plays         int
	FreePlays     int
	Seconds       int
	PendingReward int
}

// 读计数时选的列：账号的计数行(GameRewardDay)比 IP 的多出游戏时长与待发的奖励。
const (
	gameDayColumns     = "coins, plays, free_plays"
	gameUserDayColumns = gameDayColumns + ", seconds, pending_reward"
)

// gameCounterDelta 是记到一行计数上的变化。MaxFreePlays、MaxCoins 大于 0 时，条件更新保证累加之后领奖次数、游戏币
// 不越过它们：行锁之外再兜一次底。Seconds 累加到游戏时长；PendingReward 不为空时把待发的奖励改成它，这两项只能记在账号的
// 计数行上。
type gameCounterDelta struct {
	Coins         int
	Plays         int
	FreePlays     int
	MaxFreePlays  int
	MaxCoins      int
	Seconds       int
	PendingReward *int
}

// GameSettlement 是技巧游戏一局的结算：各游戏按自己的规则算出成绩和应得的游戏币，交给 FinishGameSession 按累计时长、
// 领奖次数与合计上限发放。
type GameSettlement struct {
	Score  int
	Detail string
	// State 不为空时替换对局里保存的状态，例如结束后记下整张雷区，供事后核查。
	State string
	// Reward 是按成绩与游戏规则应得的游戏币，还没有按累计时长、领奖次数与合计上限扣减。
	Reward int
	// Seconds 是这局玩了多久(秒)，累加进账号今天在这个游戏的游戏时长。
	Seconds int
	// Cheat 表示这局判定为脚本：不发奖励，游戏时长与成绩都不算数。
	Cheat bool
	// SecondsPerCoin 是每个游戏币需要账号今天在这个游戏里累计玩够的秒数，0 表示不限制。
	SecondsPerCoin int
	// DailyRewards 是每人每天、每个 IP 每天最多领几次这个游戏的奖励，0 表示不发奖励。
	DailyRewards int
	// FreeDailyLimit 是技巧游戏与人机对战奖励每人每天、每个 IP 每天合计最多发放的游戏币，0 表示不限制。
	FreeDailyLimit int
	Day            string
	Ip             string
}

// GameRewardProgress 是账号某天在一个免费游戏里的进度：Plays 是结算了几局，Rewards、IpRewards 是账号、同一 IP 下所有账号
// 领了几次奖，Earned 是账号从这个游戏获得的游戏币；FreeEarned、IpFreeEarned 是账号、同一 IP 从技巧游戏与人机对战获得的奖励合计。
// Seconds 是账号今天在这个游戏累计的游戏时长(秒)，PendingReward 是已经达标、等累计时长够了再发的游戏币。
type GameRewardProgress struct {
	Plays         int `json:"plays_today"`
	Rewards       int `json:"rewards_today"`
	IpRewards     int `json:"ip_rewards_today"`
	Earned        int `json:"earned_today"`
	FreeEarned    int `json:"free_earned_today"`
	IpFreeEarned  int `json:"ip_free_earned_today"`
	Seconds       int `json:"seconds_today"`
	PendingReward int `json:"pending_reward"`
}

// GameSettlementResult 是免费游戏一局的结算结果：Session 的 Reward 是实际发放的数量，进度是结算之后的。
type GameSettlementResult struct {
	Session *GameSession
	Balance int
	GameRewardProgress
}

// GameFreePlay 是大转盘的一次免费旋转，奖品全额发放，不占用技巧游戏与人机对战的奖励合计上限。
type GameFreePlay struct {
	// FreeLimit 是每人每天、每个 IP 每天的免费次数。
	FreeLimit int
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
	now := common.GetTimestamp()
	session := &GameSession{
		UserId:    userId,
		Game:      game,
		Seed:      int64(seed),
		Status:    GameSessionStatusPlaying,
		State:     state,
		PlayedAt:  now,
		CreatedAt: now,
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

// UpdateGameSessionState 保存进行中对局的新状态，这一步同时计入按操作累计的游戏时长。只有 session.Version 仍是库里的版本时
// 才会成功，否则返回 ErrGameRoundChanged；成功后 session 的状态、版本与游戏时长随之更新。
func UpdateGameSessionState(session *GameSession, state string, detail string) error {
	now := common.GetTimestamp()
	played := session.PlayedSeconds(now)
	result := DB.Model(&GameSession{}).
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
	session.State, session.Detail, session.Version = state, detail, session.Version+1
	session.PlaySeconds, session.PlayedAt = played, now
	return nil
}

// FinishGameSession 结算一局免费游戏：把对局从进行中切换为已结束，同一局只有一次能成功；成绩达标时按账号今天在这个游戏
// 累计的游戏时长、账号和 IP 今天剩余的领奖次数、技巧游戏与人机对战奖励的合计上限发放奖励。结算和保存每一步一样核对
// session.Version：同一局的另一步已经保存了新局面时，依据旧局面算出的结算作废，玩家不能同时发几步、挑赢了的那一步结算。
// 对局已不在进行中或版本已变时返回 ErrGameSessionClosed，调用方重新读取对局就能知道它是已经结算、被作废了还是局面变了。
func FinishGameSession(session *GameSession, settlement GameSettlement) (*GameSettlementResult, error) {
	finished := *session
	finished.Status = GameSessionStatusFinished
	finished.Version = session.Version + 1
	finished.Score = settlement.Score
	finished.Detail = settlement.Detail
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
		// 判定为脚本的对局不发奖励，游戏时长与成绩都不算数。其余对局的时长累加进账号今天在这个游戏的游戏时长，奖励取上次领奖
		// 之后达标对局里最高的应得奖励(包括这一局)；累计时长连一个游戏币都还不够时记下来，等之后的对局把时长补够再发。
		// 领奖次数或合计上限用完时今天不会再发，不用记。
		played, pending := 0, counts.user.PendingReward
		granted, limitedBy := 0, GameRewardLimitedBySpeed
		if !settlement.Cheat {
			played = max(settlement.Seconds, 0)
			reward := max(pending, settlement.Reward)
			granted, limitedBy = counts.grant(reward, counts.user.Seconds+played, settlement)
			pending = 0
			if granted == 0 && limitedBy == GameRewardLimitedByTime {
				pending = reward
			}
		}
		finished.Reward, finished.LimitedBy = granted, limitedBy

		update := tx.Model(&GameSession{}).
			Where("id = ? AND status = ? AND version = ?", session.Id, GameSessionStatusPlaying, session.Version).
			Updates(map[string]interface{}{
				"version":     session.Version + 1,
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

		if err := counters.record(tx, granted, played, pending, settlement); err != nil {
			return err
		}
		result.GameRewardProgress = counts.progress(granted, counts.user.Seconds+played, pending)
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

// SettleGameFreePlay 结算大转盘的一次免费旋转：账号或同一 IP 今天的免费次数已经用完时拒绝，什么都不会改变；否则决定结果、
// 全额发奖、记对局与流水在同一个事务里。大转盘不读写技巧游戏与人机对战的奖励合计计数；没中奖时余额没有变动，不记流水。
func SettleGameFreePlay(userId int, game string, play GameFreePlay) (*GameSettlementResult, error) {
	if play.FreeLimit < 0 {
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
		user, err := lockGameRewardCounter(tx, counters.userRow, counters.userKey, gameDayColumns)
		if err != nil {
			return err
		}
		ip, err := lockGameRewardCounter(tx, counters.ipRow, counters.ipKey, gameDayColumns)
		if err != nil {
			return err
		}
		switch {
		case user.FreePlays >= play.FreeLimit:
			return ErrGameFreePlayUsed
		case ip.FreePlays >= play.FreeLimit:
			return ErrGameIpFreePlayUsed
		}
		win, detail, err := play.Play()
		if err != nil {
			return err
		}
		if win < 0 || win >= common.MaxQuota {
			return ErrGameCoinAmountInvalid
		}
		session.Score, session.Detail = win, detail
		session.Reward = win

		// 免费次数用掉一次，无论中没中奖。
		if err := bumpGameCounter(tx, &GameRewardDay{}, counters.userKey, gameCounterDelta{Coins: win, Plays: 1, FreePlays: 1, MaxFreePlays: play.FreeLimit}); err != nil {
			return err
		}
		if err := bumpGameCounter(tx, &GameRewardIpDay{}, counters.ipKey, gameCounterDelta{Coins: win, Plays: 1, FreePlays: 1, MaxFreePlays: play.FreeLimit}); err != nil {
			return err
		}
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		result.GameRewardProgress = GameRewardProgress{
			Plays:     user.Plays + 1,
			Rewards:   user.FreePlays + 1,
			IpRewards: ip.FreePlays + 1,
			Earned:    user.Coins + win,
		}
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

// gameFreeCounters 是免费奖励的计数键：账号与 IP 在这个游戏的计数，以及账号与 IP 的技巧游戏、人机对战奖励合计。
// 技巧游戏与人机对战按这个顺序锁四行，大转盘只锁前两行，同时结算的几局不会互相等待成环。
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
	if counts.user, err = lockGameRewardCounter(tx, c.userRow, c.userKey, gameUserDayColumns); err != nil {
		return nil, err
	}
	if counts.ip, err = lockGameRewardCounter(tx, c.ipRow, c.ipKey, gameDayColumns); err != nil {
		return nil, err
	}
	if counts.userFree, err = lockGameRewardCounter(tx, c.userFRow, c.userFree, gameDayColumns); err != nil {
		return nil, err
	}
	if counts.ipFree, err = lockGameRewardCounter(tx, c.ipFRow, c.ipFree, gameDayColumns); err != nil {
		return nil, err
	}
	return counts, nil
}

// record 给一局技巧游戏记账：账号的局数加一、游戏时长加上 played 秒、待发的奖励改成 pending；发了奖时，账号与 IP 的领奖次数
// 各加一，四行都累加发放的游戏币。
func (c *gameFreeCounters) record(tx *gorm.DB, granted int, played int, pending int, settlement GameSettlement) error {
	user := gameCounterDelta{Plays: 1, Seconds: played, PendingReward: &pending}
	if granted == 0 {
		return bumpGameCounter(tx, &GameRewardDay{}, c.userKey, user)
	}
	user.Coins, user.FreePlays, user.MaxFreePlays = granted, 1, settlement.DailyRewards
	if err := bumpGameCounter(tx, &GameRewardDay{}, c.userKey, user); err != nil {
		return err
	}
	if err := bumpGameCounter(tx, &GameRewardIpDay{}, c.ipKey, gameCounterDelta{Coins: granted, Plays: 1, FreePlays: 1, MaxFreePlays: settlement.DailyRewards}); err != nil {
		return err
	}
	return c.recordFree(tx, granted, settlement.FreeDailyLimit)
}

// recordFree 把发放的技巧游戏与人机对战奖励累加到账号与 IP 的合计上，设了合计上限时条件更新保证不越过它。
func (c *gameFreeCounters) recordFree(tx *gorm.DB, granted int, freeDailyLimit int) error {
	if granted == 0 {
		return nil
	}
	if err := bumpGameCounter(tx, &GameRewardDay{}, c.userFree, gameCounterDelta{Coins: granted, FreePlays: 1, MaxCoins: freeDailyLimit}); err != nil {
		return err
	}
	return bumpGameCounter(tx, &GameRewardIpDay{}, c.ipFree, gameCounterDelta{Coins: granted, FreePlays: 1, MaxCoins: freeDailyLimit})
}

// grant 按今天的计数决定这次结算实际发放多少。reward 是上次领奖之后达标对局里最高的应得奖励，played 是账号今天在这个游戏
// 累计的游戏时长，都已算上这一局。账号或同一 IP 在这个游戏的领奖次数用完就不发；设了合计上限时最多发到上限为止；每个游戏币
// 还要今天累计玩够 SecondsPerCoin 秒(已经领到的也算在内)，时长只够几个就先发几个，一个都不够时不发。返回发放的数量和
// 少发的原因(没有少发时为空)。
func (n *gameFreeCounts) grant(reward int, played int, settlement GameSettlement) (int, string) {
	switch {
	case reward == 0:
		return 0, ""
	case n.user.FreePlays >= settlement.DailyRewards:
		return 0, GameRewardLimitedByDaily
	case n.ip.FreePlays >= settlement.DailyRewards:
		return 0, GameRewardLimitedByIp
	}
	coins, limitedBy := reward, ""
	if settlement.FreeDailyLimit > 0 {
		if room := n.freeRoom(settlement.FreeDailyLimit); room < coins {
			coins, limitedBy = max(room, 0), GameRewardLimitedByTotal
		}
	}
	if coins > 0 && settlement.SecondsPerCoin > 0 {
		if byTime := played/settlement.SecondsPerCoin - n.user.Coins; byTime < coins {
			coins, limitedBy = max(byTime, 0), GameRewardLimitedByTime
		}
	}
	return coins, limitedBy
}

// freeRoom 返回账号与同一 IP 今天离技巧游戏与人机对战奖励的合计上限还差多少，取两者中较少的。
func (n *gameFreeCounts) freeRoom(freeDailyLimit int) int {
	return min(freeDailyLimit-n.userFree.Coins, freeDailyLimit-n.ipFree.Coins)
}

// progress 返回这次结算之后的进度：发放了 granted，账号今天在这个游戏的游戏时长是 seconds、待发的奖励是 pending。
func (n *gameFreeCounts) progress(granted int, seconds int, pending int) GameRewardProgress {
	progress := GameRewardProgress{
		Plays:         n.user.Plays + 1,
		Rewards:       n.user.FreePlays,
		IpRewards:     n.ip.FreePlays,
		Earned:        n.user.Coins + granted,
		FreeEarned:    n.userFree.Coins + granted,
		IpFreeEarned:  n.ipFree.Coins + granted,
		Seconds:       seconds,
		PendingReward: pending,
	}
	if granted > 0 {
		progress.Rewards++
		progress.IpRewards++
	}
	return progress
}

// lockGameRewardCounter 锁住一个计数行并返回其中的数字，行不存在时先建出来。row 是要建的行，key 是它的主键列，columns 是
// 要读的列(gameDayColumns 或 gameUserDayColumns)。
func lockGameRewardCounter(tx *gorm.DB, row interface{}, key map[string]interface{}, columns string) (GameDayCount, error) {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return GameDayCount{}, err
	}
	var count GameDayCount
	err := lockForUpdate(tx).Model(row).Where(key).Select(columns).Scan(&count).Error
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
	if delta.Seconds != 0 {
		updates["seconds"] = gorm.Expr("seconds + ?", delta.Seconds)
	}
	if delta.PendingReward != nil {
		updates["pending_reward"] = *delta.PendingReward
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
	err := tx.Model(&GameRewardDay{}).Where(key).Select(gameDayColumns).Scan(&count).Error
	return count.Coins, count.Plays, err
}

// GetGameRewardProgress 返回账号某天在一个免费游戏里的进度，ip 是账号当前的 IP。
func GetGameRewardProgress(userId int, ip string, game string, day string) (GameRewardProgress, error) {
	var user, byIp, userFree, ipFree GameDayCount
	reads := []struct {
		table   interface{}
		key     map[string]interface{}
		columns string
		into    *GameDayCount
	}{
		{&GameRewardDay{}, map[string]interface{}{"user_id": userId, "game": game, "day": day}, gameUserDayColumns, &user},
		{&GameRewardIpDay{}, map[string]interface{}{"ip": ip, "game": game, "day": day}, gameDayColumns, &byIp},
		{&GameRewardDay{}, map[string]interface{}{"user_id": userId, "game": GameFreeCounter, "day": day}, gameDayColumns, &userFree},
		{&GameRewardIpDay{}, map[string]interface{}{"ip": ip, "game": GameFreeCounter, "day": day}, gameDayColumns, &ipFree},
	}
	for _, read := range reads {
		if err := DB.Model(read.table).Where(read.key).Select(read.columns).Scan(read.into).Error; err != nil {
			return GameRewardProgress{}, err
		}
	}
	return GameRewardProgress{
		Plays:         user.Plays,
		Rewards:       user.FreePlays,
		IpRewards:     byIp.FreePlays,
		Earned:        user.Coins,
		FreeEarned:    userFree.Coins,
		IpFreeEarned:  ipFree.Coins,
		Seconds:       user.Seconds,
		PendingReward: user.PendingReward,
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
		Select(gameDayColumns).Scan(&count).Error
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
// 合计，为负表示玩家合计输掉的数量；GameFreeCounter 一行是技巧游戏与人机对战奖励的合计。
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
