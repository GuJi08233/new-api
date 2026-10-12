package model

import (
	"errors"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 夺宝奖池。全站同一时间只有一轮开放：玩家往奖池里投游戏币，每个额度单位就是一张奖券；第二位玩家投入后开始倒计时，
// 到点从所有奖券里随机抽出一张，持有者拿走奖池，平台按比例抽成。一直只有一位玩家的一轮放久了就原样退回。

const (
	GameJackpotStatusOpen     = "open"     // 开放投入，或已到开奖时间等待开奖
	GameJackpotStatusDrawn    = "drawn"    // 已开奖并派彩
	GameJackpotStatusRefunded = "refunded" // 只有一位玩家，投入已退回
)

// gameJackpotOpenSlot 是开放中那一轮的 Slot。结束的一轮改成自己的编号，Slot 上的唯一索引保证同一时间最多一轮开放，
// 两个请求同时建新一轮也只有一个能建成。
const gameJackpotOpenSlot = "open"

var (
	// ErrGameJackpotClosed 表示当前一轮已到开奖时间，不能再投入，等开奖之后投下一轮。
	ErrGameJackpotClosed = errors.New("jackpot round is closed for entries")
	// ErrGameJackpotLimit 表示这次投入会让玩家在这一轮的投入超过每轮上限。
	ErrGameJackpotLimit = errors.New("jackpot entry limit per round exceeded")
	// ErrGameJackpotSameIp 表示同一 IP 下的另一个账号已经投了这一轮。
	ErrGameJackpotSameIp = errors.New("another account on this ip has entered this jackpot round")
)

// GameJackpotRound 是奖池的一轮。
type GameJackpotRound struct {
	Id     int    `json:"id"`
	Slot   string `json:"-" gorm:"type:varchar(16);uniqueIndex"`
	Status string `json:"status" gorm:"type:varchar(16);index"`
	// Pot 是奖池里的游戏币，也是奖券总数；Players 是投入过的玩家数。
	Pot     GameCoins `json:"pot" gorm:"type:bigint;not null;default:0"`
	Players int       `json:"players" gorm:"not null;default:0"`
	// Deadline 是开奖时间，第二位玩家投入之前为 0。
	Deadline int64 `json:"deadline" gorm:"bigint"`
	// RakePercent 是这一轮开放时的抽成比例，之后管理员改了配置也按它开奖。
	RakePercent int `json:"rake_percent" gorm:"not null;default:0"`
	WinnerId    int `json:"winner_id"`
	// WinnerStake 是赢家在这一轮投入的游戏币，与奖池之比就是赢家中奖的概率。
	WinnerStake GameCoins `json:"winner_stake" gorm:"type:bigint;not null;default:0"`
	// Payout 是赢家拿到的游戏币，Rake 是平台抽走的，两者之和就是奖池。Ticket 是抽中的奖券，从 0 起编号：玩家按第一次投入的
	// 先后排，每位玩家的奖券连在一起，数量就是他的投入合计。
	Payout    GameCoins `json:"payout" gorm:"type:bigint;not null;default:0"`
	Rake      GameCoins `json:"rake" gorm:"type:bigint;not null;default:0"`
	Ticket    GameCoins `json:"ticket" gorm:"type:bigint;not null;default:0"`
	CreatedAt int64     `json:"created_at" gorm:"bigint;index"`
	DrawnAt   int64     `json:"drawn_at" gorm:"bigint"`
}

// GameJackpotEntry 是一次投入。一位玩家在一轮里可以投几次，奖券按投入先后连续编号。
type GameJackpotEntry struct {
	Id        int       `json:"id"`
	RoundId   int       `json:"round_id" gorm:"index"`
	UserId    int       `json:"user_id" gorm:"index"`
	Amount    GameCoins `json:"amount" gorm:"type:bigint"`
	Ip        string    `json:"-" gorm:"type:varchar(64)"`
	CreatedAt int64     `json:"created_at" gorm:"bigint"`
}

// gameJackpotRef 是奖池流水的关联编号，与对局编号区分开。
func gameJackpotRef(roundId int) string {
	return "jackpot:" + strconv.Itoa(roundId)
}

// lockOpenGameJackpotRoundTx 锁住开放中的一轮，create 为真且没有时按抽成比例 rakePercent 先建出来；没有且不建时返回 nil。
func lockOpenGameJackpotRoundTx(tx *gorm.DB, create bool, rakePercent int, now int64) (*GameJackpotRound, error) {
	if create {
		round := &GameJackpotRound{Slot: gameJackpotOpenSlot, Status: GameJackpotStatusOpen, RakePercent: rakePercent, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(round).Error; err != nil {
			return nil, err
		}
	}
	var rounds []GameJackpotRound
	if err := lockForUpdate(tx).Where("slot = ?", gameJackpotOpenSlot).Limit(1).Find(&rounds).Error; err != nil {
		return nil, err
	}
	if len(rounds) == 0 {
		return nil, nil
	}
	return &rounds[0], nil
}

// GameJackpotEntryRule 是投入时的规则：每人每轮最多投入 MaxBet，第二位玩家投入后 RoundSeconds 秒开奖，新开的一轮按
// RakePercent 抽成。
type GameJackpotEntryRule struct {
	MaxBet       GameCoins
	RoundSeconds int
	RakePercent  int
}

// EnterGameJackpot 往开放中的一轮投入 amount 个游戏币，没有开放的一轮时新开一轮。玩家在一轮里的投入合计不超过 rule.MaxBet；
// 这是第二位玩家时开始倒计时。当前一轮已到开奖时间时返回 ErrGameJackpotClosed，调用方先开奖再投。同一 IP 下的账号算同一个人：
// 别的账号已经投了这一轮时返回 ErrGameJackpotSameIp，不然一个人用几个账号就能凑够两位玩家开奖、越过每人每轮的上限，
// 在自己的账号之间倒腾游戏币(对战也不许同一 IP 的账号互相押注)。
func EnterGameJackpot(userId int, amount GameCoins, rule GameJackpotEntryRule, ip string) (*GameJackpotRound, GameCoins, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return nil, 0, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	var round *GameJackpotRound
	var balance GameCoins
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if round, err = lockOpenGameJackpotRoundTx(tx, true, rule.RakePercent, now); err != nil {
			return err
		}
		if round == nil {
			return errors.New("jackpot round was not created")
		}
		if round.Deadline != 0 && round.Deadline <= now {
			return ErrGameJackpotClosed
		}
		var sameIp []int
		if err := tx.Model(&GameJackpotEntry{}).Where("round_id = ? AND ip = ? AND user_id <> ?", round.Id, ip, userId).
			Limit(1).Pluck("id", &sameIp).Error; err != nil {
			return err
		}
		if len(sameIp) > 0 {
			return ErrGameJackpotSameIp
		}
		var entered GameCoins
		if err := tx.Model(&GameJackpotEntry{}).Where("round_id = ? AND user_id = ?", round.Id, userId).
			Select("COALESCE(SUM(amount), 0)").Scan(&entered).Error; err != nil {
			return err
		}
		if entered+amount > rule.MaxBet {
			return ErrGameJackpotLimit
		}
		balance, err = debitGameCoinsTx(tx, userId, amount, map[string]interface{}{
			"total_wagered": gorm.Expr("total_wagered + ?", amount),
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&GameJackpotEntry{RoundId: round.Id, UserId: userId, Amount: amount, Ip: ip, CreatedAt: now}).Error; err != nil {
			return err
		}
		round.Pot += amount
		if entered == 0 {
			round.Players++
		}
		if round.Players >= 2 && round.Deadline == 0 {
			round.Deadline = now + int64(rule.RoundSeconds)
		}
		if err := tx.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Updates(map[string]interface{}{
			"pot":      round.Pot,
			"players":  round.Players,
			"deadline": round.Deadline,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeGameWager,
			Amount:    -amount,
			Balance:   balance,
			RefId:     gameJackpotRef(round.Id),
			Remark:    "jackpot",
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, 0, err
	}
	return round, balance, nil
}

// gameJackpotTotals 按用户汇总一轮的投入，并按用户 ID 从小到大返回用户列表，结算时依次记账，加锁顺序一致。
func gameJackpotTotals(entries []GameJackpotEntry) (map[int]GameCoins, []int) {
	totals := make(map[int]GameCoins)
	for _, entry := range entries {
		totals[entry.UserId] += entry.Amount
	}
	users := make([]int, 0, len(totals))
	for userId := range totals {
		users = append(users, userId)
	}
	sort.Ints(users)
	return totals, users
}

// DrawGameJackpot 给已到开奖时间的一轮开奖：pick 从 [0, 奖池) 里抽一张奖券，持有者拿走奖池减去这一轮的抽成(向下取整)。
// 没有到开奖时间的一轮时什么都不做，返回 nil。
func DrawGameJackpot(pick func(tickets int) (int, error), day string) (*GameJackpotRound, error) {
	now := common.GetTimestamp()
	var drawn *GameJackpotRound
	err := DB.Transaction(func(tx *gorm.DB) error {
		round, err := lockOpenGameJackpotRoundTx(tx, false, 0, now)
		if err != nil || round == nil || round.Deadline == 0 || round.Deadline > now {
			return err
		}
		var entries []GameJackpotEntry
		if err := tx.Where("round_id = ?", round.Id).Order("id asc").Find(&entries).Error; err != nil {
			return err
		}
		picked, err := pick(int(round.Pot))
		if err != nil {
			return err
		}
		ticket := GameCoins(picked)
		if ticket < 0 || ticket >= round.Pot {
			return errors.New("jackpot ticket out of range")
		}
		// 奖券按玩家编号，和页面上按玩家画的奖池一一对应，指针停在哪一段就是谁中奖。抽中每位玩家的概率只看他的投入合计，
		// 与怎么编号无关。
		totals, users := gameJackpotTotals(entries)
		counted := make(map[int]bool, len(totals))
		rest := ticket
		for _, entry := range entries {
			if counted[entry.UserId] {
				continue
			}
			counted[entry.UserId] = true
			if rest < totals[entry.UserId] {
				round.WinnerId = entry.UserId
				break
			}
			rest -= totals[entry.UserId]
		}
		round.Status, round.Ticket, round.DrawnAt = GameJackpotStatusDrawn, ticket, now
		round.Rake = round.Pot * GameCoins(round.RakePercent) / 100
		round.Payout = round.Pot - round.Rake
		if err := tx.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Updates(map[string]interface{}{
			"slot":      "r" + strconv.Itoa(round.Id),
			"status":    round.Status,
			"winner_id": round.WinnerId,
			"payout":    round.Payout,
			"rake":      round.Rake,
			"ticket":    round.Ticket,
			"drawn_at":  round.DrawnAt,
		}).Error; err != nil {
			return err
		}
		round.WinnerStake = totals[round.WinnerId]
		if err := tx.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Update("winner_stake", round.WinnerStake).Error; err != nil {
			return err
		}
		for _, userId := range users {
			net := -totals[userId]
			if userId == round.WinnerId {
				net += round.Payout
				balance, err := creditGameCoinsTx(tx, userId, round.Payout, map[string]interface{}{
					"total_wager_net": gorm.Expr("total_wager_net + ?", net),
				})
				if err != nil {
					return err
				}
				if err := tx.Create(&GameCoinLog{
					UserId:    userId,
					Type:      GameCoinLogTypeGameWager,
					Amount:    round.Payout,
					Balance:   balance,
					RefId:     gameJackpotRef(round.Id),
					Remark:    "jackpot",
					CreatedAt: now,
				}).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&GameCoinAccount{}).Where("user_id = ?", userId).Updates(map[string]interface{}{
				"total_wager_net": gorm.Expr("total_wager_net + ?", net),
				"updated_at":      now,
			}).Error; err != nil {
				return err
			}
			if _, _, err := recordGameDayResult(tx, userId, "jackpot", day, net); err != nil {
				return err
			}
		}
		drawn = round
		return nil
	})
	if err != nil {
		return nil, err
	}
	return drawn, nil
}

// RefundStaleGameJackpot 退回一直只有一位玩家、在 staleBefore 之前开的一轮：投入原样退回，这一轮关闭。
// 没有这样的一轮时什么都不做。
func RefundStaleGameJackpot(staleBefore int64) (*GameJackpotRound, error) {
	now := common.GetTimestamp()
	var refunded *GameJackpotRound
	err := DB.Transaction(func(tx *gorm.DB) error {
		round, err := lockOpenGameJackpotRoundTx(tx, false, 0, now)
		if err != nil || round == nil || round.Players >= 2 || round.CreatedAt >= staleBefore {
			return err
		}
		var entries []GameJackpotEntry
		if err := tx.Where("round_id = ?", round.Id).Order("id asc").Find(&entries).Error; err != nil {
			return err
		}
		if err := tx.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Updates(map[string]interface{}{
			"slot":     "r" + strconv.Itoa(round.Id),
			"status":   GameJackpotStatusRefunded,
			"drawn_at": now,
		}).Error; err != nil {
			return err
		}
		totals, users := gameJackpotTotals(entries)
		for _, userId := range users {
			// 没有开奖，这些投入不算下注过。
			balance, err := creditGameCoinsTx(tx, userId, totals[userId], map[string]interface{}{
				"total_wagered": gorm.Expr("total_wagered - ?", totals[userId]),
			})
			if err != nil {
				return err
			}
			if err := tx.Create(&GameCoinLog{
				UserId:    userId,
				Type:      GameCoinLogTypeGameWager,
				Amount:    totals[userId],
				Balance:   balance,
				RefId:     gameJackpotRef(round.Id),
				Remark:    "jackpot",
				CreatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		round.Status, round.DrawnAt = GameJackpotStatusRefunded, now
		refunded = round
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refunded, nil
}

// GetOpenGameJackpotRound 返回开放中的一轮，没有时返回 nil。
func GetOpenGameJackpotRound() (*GameJackpotRound, error) {
	var rounds []GameJackpotRound
	if err := DB.Where("slot = ?", gameJackpotOpenSlot).Limit(1).Find(&rounds).Error; err != nil || len(rounds) == 0 {
		return nil, err
	}
	return &rounds[0], nil
}

// HasOpenGameJackpotEntry 判断玩家在开放中的这一轮里有没有投入。
func HasOpenGameJackpotEntry(userId int) (bool, error) {
	round, err := GetOpenGameJackpotRound()
	if err != nil || round == nil {
		return false, err
	}
	var ids []int
	err = DB.Model(&GameJackpotEntry{}).Where("round_id = ? AND user_id = ?", round.Id, userId).Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
}

// ListGameJackpotEntries 按投入先后返回一轮的所有投入。
func ListGameJackpotEntries(roundId int) ([]GameJackpotEntry, error) {
	var entries []GameJackpotEntry
	err := DB.Where("round_id = ?", roundId).Order("id asc").Find(&entries).Error
	return entries, err
}

// ListRecentGameJackpotRounds 按开奖先后倒序返回最近开奖的几轮。
func ListRecentGameJackpotRounds(limit int) ([]GameJackpotRound, error) {
	var rounds []GameJackpotRound
	err := DB.Where("status = ?", GameJackpotStatusDrawn).Order("id desc").Limit(limit).Find(&rounds).Error
	return rounds, err
}

// HasDueGameJackpot 表示开放中的一轮已到开奖时间，或者一直只有一位玩家、在 staleBefore 之前开的一轮该退回了。
func HasDueGameJackpot(now int64, staleBefore int64) bool {
	round, err := GetOpenGameJackpotRound()
	if err != nil || round == nil {
		return false
	}
	if round.Deadline != 0 {
		return round.Deadline <= now
	}
	return round.CreatedAt < staleBefore
}
