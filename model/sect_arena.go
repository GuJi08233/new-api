package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 问道宗门的论剑：每个宗门设一套守擂阵容(只记弟子编号，被挑战时按守擂宗门当时的实力上场，由电脑代打)，别的掌门挑战它，
// 按 Elo 积分排名。论剑只发宗门资源，不发游戏币；积分只用来排名与匹配对手。

// SectArenaStartRating 是第一次设守擂阵容时的积分。
const SectArenaStartRating = 1000

// sectArenaK 是 Elo 的 K 值：一场最多变动这么多分。
const sectArenaK = 32

// ErrSectArenaBattleRecorded 表示这一局论剑的结果已经记过了(重复结算)。
var ErrSectArenaBattleRecorded = errors.New("sect arena battle already recorded")

// SectArena 是一个宗门在论剑榜上的一行。Lineup 是守擂弟子的编号(JSON 数组)，Power 是最近一次算出的守擂总战力，只用于展示。
type SectArena struct {
	UserId       int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Rating       int    `json:"rating" gorm:"index;not null;default:1000"`
	Power        int    `json:"power" gorm:"not null;default:0"`
	Lineup       string `json:"lineup" gorm:"type:varchar(64)"`
	Wins         int    `json:"wins" gorm:"not null;default:0"`
	Losses       int    `json:"losses" gorm:"not null;default:0"`
	DefendWins   int    `json:"defend_wins" gorm:"not null;default:0"`
	DefendLosses int    `json:"defend_losses" gorm:"not null;default:0"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
}

// SectArenaBattle 是一场论剑的记录。SessionId 唯一：同一局重复结算只记一次积分。
type SectArenaBattle struct {
	Id            int   `json:"id"`
	SessionId     int   `json:"session_id" gorm:"uniqueIndex"`
	Attacker      int   `json:"attacker" gorm:"index"`
	Defender      int   `json:"defender" gorm:"index"`
	Win           bool  `json:"win"`
	Wins          int   `json:"wins"`
	AttackerDelta int   `json:"attacker_delta"`
	DefenderDelta int   `json:"defender_delta"`
	CreatedAt     int64 `json:"created_at" gorm:"bigint;index"`
}

// SectArenaEloDelta 是积分 rating 的一方对积分 other 的一方赢了(win)或输了之后的积分变动。
func SectArenaEloDelta(rating int, other int, win bool) int {
	expected := 1 / (1 + math.Pow(10, float64(other-rating)/400))
	score := 0.0
	if win {
		score = 1
	}
	return int(math.Round(sectArenaK * (score - expected)))
}

// GetSectArena 返回玩家在论剑榜上的一行，还没设过守擂阵容时返回 nil。
func GetSectArena(userId int) (*SectArena, error) {
	var rows []SectArena
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// SetSectArenaDefense 设置守擂阵容，第一次设置时上榜(积分 SectArenaStartRating)。
func SetSectArenaDefense(userId int, lineup string, power int) (*SectArena, error) {
	now := common.GetTimestamp()
	row := &SectArena{UserId: userId, Rating: SectArenaStartRating, Power: power, Lineup: lineup, UpdatedAt: now}
	err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"lineup": lineup, "power": power, "updated_at": now}),
	}).Create(row).Error
	if err != nil {
		return nil, err
	}
	return GetSectArena(userId)
}

// UpdateSectArenaPower 记下按守擂宗门现在的实力算出的守擂总战力(只用于展示)。
func UpdateSectArenaPower(userId int, power int) error {
	return DB.Model(&SectArena{}).Where("user_id = ? AND power <> ?", userId, power).Update("power", power).Error
}

// ListSectArenaOpponents 返回积分与 rating 最接近的最多 limit 个别的宗门：比它高的与比它低的各取一半，不够时另一边补齐。
func ListSectArenaOpponents(userId int, rating int, limit int) ([]SectArena, error) {
	var above, below []SectArena
	// 没设守擂阵容的宗门(只挑战过别人)挑战不了，不列出来。
	if err := DB.Where("user_id <> ? AND lineup <> '' AND rating >= ?", userId, rating).Order("rating asc").Order("user_id asc").
		Limit(limit).Find(&above).Error; err != nil {
		return nil, err
	}
	if err := DB.Where("user_id <> ? AND lineup <> '' AND rating < ?", userId, rating).Order("rating desc").Order("user_id asc").
		Limit(limit).Find(&below).Error; err != nil {
		return nil, err
	}
	half := limit / 2
	takeAbove := min(len(above), max(half, limit-len(below)))
	takeBelow := min(len(below), limit-takeAbove)
	result := append([]SectArena{}, above[:takeAbove]...)
	return append(result, below[:takeBelow]...), nil
}

// ListSectArenaTop 返回积分最高的 limit 个宗门，同分先上榜的在前。
func ListSectArenaTop(limit int) ([]SectArena, error) {
	var rows []SectArena
	err := DB.Order("rating desc").Order("user_id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListSectArenaBattles 返回玩家最近的论剑记录(挑战别人的与被别人挑战的)，新的在前。
func ListSectArenaBattles(userId int, limit int) ([]SectArenaBattle, error) {
	var rows []SectArenaBattle
	err := DB.Where("attacker = ? OR defender = ?", userId, userId).Order("id desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// RecordSectArenaBattle 记下一场论剑并按 Elo 改双方积分：挑战方 attacker 赢了(win)或输了，守擂方 defender 反之。同一局(sessionId)
// 只记一次，重复时返回 ErrSectArenaBattleRecorded。两行按 user_id 从小到大加锁，两场方向相反的论剑同时结算也不会互相等待。
func RecordSectArenaBattle(sessionId int, attacker int, defender int, win bool, wins int) (*SectArenaBattle, error) {
	battle := &SectArenaBattle{SessionId: sessionId, Attacker: attacker, Defender: defender, Win: win, Wins: wins,
		CreatedAt: common.GetTimestamp()}
	err := DB.Transaction(func(tx *gorm.DB) error {
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(battle)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return ErrSectArenaBattleRecorded
		}
		var rows []SectArena
		if err := lockForUpdate(tx).Where("user_id IN ?", []int{attacker, defender}).Order("user_id asc").Find(&rows).Error; err != nil {
			return err
		}
		ratings := map[int]int{}
		for _, row := range rows {
			ratings[row.UserId] = row.Rating
		}
		attackerRating, ok := ratings[attacker]
		if !ok {
			// 挑战方还没设守擂阵容：先按初始积分上榜，阵容留空(别人挑战不了它)。
			attackerRating = SectArenaStartRating
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&SectArena{UserId: attacker, Rating: attackerRating, UpdatedAt: battle.CreatedAt}).Error; err != nil {
				return err
			}
		}
		defenderRating, ok := ratings[defender]
		if !ok {
			return gorm.ErrRecordNotFound
		}
		battle.AttackerDelta = SectArenaEloDelta(attackerRating, defenderRating, win)
		battle.DefenderDelta = SectArenaEloDelta(defenderRating, attackerRating, !win)
		attackerResult, defenderResult := "losses", "defend_wins"
		if win {
			attackerResult, defenderResult = "wins", "defend_losses"
		}
		// 积分不低于 0：被打到底的宗门不会一直往下掉成负数。
		if err := tx.Model(&SectArena{}).Where("user_id = ?", attacker).Updates(map[string]interface{}{
			"rating":       max(attackerRating+battle.AttackerDelta, 0),
			attackerResult: gorm.Expr(attackerResult + " + 1"),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&SectArena{}).Where("user_id = ?", defender).Updates(map[string]interface{}{
			"rating":       max(defenderRating+battle.DefenderDelta, 0),
			defenderResult: gorm.Expr(defenderResult + " + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Model(battle).Updates(map[string]interface{}{
			"attacker_delta": battle.AttackerDelta, "defender_delta": battle.DefenderDelta,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return battle, nil
}
