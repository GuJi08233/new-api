package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 问道宗门的仙盟：几位掌门结成的同盟，一起捐资源升级仙盟、每周合力讨伐一个 Boss。规则(花费、上限、血量、奖励)在 pkg/sect，
// 这里只管把仙盟、成员与掌门的宗门存档在同一个事务里改对。加锁顺序固定为宗门存档、仙盟、成员，同时发生的操作不会互相等待。
// 仙盟只发宗门资源与丹药，不发游戏币。

// sectGameKey 是问道宗门的存档标识(与 controller 里的游戏标识一致)：仙盟的花费与奖励记在掌门的宗门存档里。
const sectGameKey = "sect"

const (
	SectAllianceRoleLeader = "leader"
	SectAllianceRoleMember = "member"
)

var (
	// ErrSectAllianceJoined 表示已经在一个仙盟里了。
	ErrSectAllianceJoined = errors.New("already in a sect alliance")
	// ErrSectAllianceNotMember 表示不在仙盟里，或者要操作的成员不在同一个仙盟。
	ErrSectAllianceNotMember = errors.New("not a member of this sect alliance")
	// ErrSectAllianceNotLeader 表示只有盟主能这样做。
	ErrSectAllianceNotLeader = errors.New("only the alliance leader can do this")
	// ErrSectAllianceFull 表示仙盟满员了。
	ErrSectAllianceFull = errors.New("sect alliance is full")
	// ErrSectAllianceName 表示盟名已经有人用了。
	ErrSectAllianceName = errors.New("sect alliance name is taken")
	// ErrSectAllianceFunds 表示盟资不够或者已经满级。
	ErrSectAllianceFunds = errors.New("not enough alliance funds")
	// ErrSectAllianceAttacked 表示今天已经讨伐过，或者本周的 Boss 已经被击杀。
	ErrSectAllianceAttacked = errors.New("already attacked the boss today")
	// ErrSectAllianceNothing 表示没有待领的奖励。
	ErrSectAllianceNothing = errors.New("no alliance rewards to claim")
)

// SectAlliance 是一个仙盟。Week 是周 Boss 所在的周(例如 2026-W44)，换周时按上周是否击杀决定新 Boss 的等级并回满血。
type SectAlliance struct {
	Id        int    `json:"id"`
	Name      string `json:"name" gorm:"type:varchar(64);uniqueIndex"`
	LeaderId  int    `json:"leader_id" gorm:"index"`
	Level     int    `json:"level" gorm:"index;not null;default:1"`
	Funds     int64  `json:"funds" gorm:"type:bigint;not null;default:0"`
	Members   int    `json:"members" gorm:"not null;default:0"`
	Notice    string `json:"notice" gorm:"type:varchar(255)"`
	Week      string `json:"week" gorm:"type:varchar(10)"`
	BossLevel int    `json:"boss_level" gorm:"not null;default:1"`
	BossHp    int64  `json:"boss_hp" gorm:"type:bigint;not null;default:0"`
	BossMaxHp int64  `json:"boss_max_hp" gorm:"type:bigint;not null;default:0"`
	BossKills int    `json:"boss_kills" gorm:"not null;default:0"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

// SectAllianceMember 是仙盟的一名成员(一位掌门只能在一个仙盟里)。WeekDamage 是本周对 Boss 造成的伤害，AttackDay 是最近一次讨伐的
// 日期；Pending* 是讨伐与击杀攒下、还没领进宗门的奖励。
type SectAllianceMember struct {
	UserId       int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	AllianceId   int    `json:"alliance_id" gorm:"index"`
	Role         string `json:"role" gorm:"type:varchar(16)"`
	Contribution int64  `json:"contribution" gorm:"type:bigint;not null;default:0"`
	Week         string `json:"week" gorm:"type:varchar(10)"`
	WeekDamage   int64  `json:"week_damage" gorm:"type:bigint;not null;default:0"`
	AttackDay    string `json:"attack_day" gorm:"type:varchar(10)"`
	PendingStone int    `json:"pending_stone" gorm:"not null;default:0"`
	PendingRep   int    `json:"pending_rep" gorm:"not null;default:0"`
	PendingPills int    `json:"pending_pills" gorm:"not null;default:0"`
	JoinedAt     int64  `json:"joined_at" gorm:"bigint"`
}

// SectAllianceBoss 是周 Boss 的规则：hp 按 Boss 等级与仙盟人数给出血量，kill 是击杀时每名出过力的成员另得的灵石、声望与丹药。
type SectAllianceBoss struct {
	Hp   func(level int, members int) int64
	Kill func(level int) (stone int, rep int, pills int)
}

// rollWeek 在换周时重置周 Boss：上周击杀了就升一级，血量按现在的人数回满。
func (a *SectAlliance) rollWeek(week string, boss SectAllianceBoss) {
	if a.Week == week {
		return
	}
	if a.Week != "" && a.BossHp <= 0 {
		a.BossLevel++
	}
	a.Week = week
	a.BossMaxHp = boss.Hp(a.BossLevel, a.Members)
	a.BossHp = a.BossMaxHp
}

func (m *SectAllianceMember) rollWeek(week string) {
	if m.Week != week {
		m.Week, m.WeekDamage = week, 0
	}
}

// saveAlliance 写回仙盟的可变字段。
func saveAlliance(tx *gorm.DB, a *SectAlliance) error {
	a.UpdatedAt = common.GetTimestamp()
	return tx.Model(&SectAlliance{}).Where("id = ?", a.Id).Updates(map[string]interface{}{
		"leader_id": a.LeaderId, "level": a.Level, "funds": a.Funds, "members": a.Members, "notice": a.Notice,
		"week": a.Week, "boss_level": a.BossLevel, "boss_hp": a.BossHp, "boss_max_hp": a.BossMaxHp,
		"boss_kills": a.BossKills, "updated_at": a.UpdatedAt,
	}).Error
}

func saveAllianceMember(tx *gorm.DB, m *SectAllianceMember) error {
	return tx.Model(&SectAllianceMember{}).Where("user_id = ?", m.UserId).Updates(map[string]interface{}{
		"role": m.Role, "contribution": m.Contribution, "week": m.Week, "week_damage": m.WeekDamage,
		"attack_day": m.AttackDay, "pending_stone": m.PendingStone, "pending_rep": m.PendingRep,
		"pending_pills": m.PendingPills,
	}).Error
}

func lockAlliance(tx *gorm.DB, id int) (*SectAlliance, error) {
	var a SectAlliance
	if err := lockForUpdate(tx).Where("id = ?", id).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSectAllianceNotMember
		}
		return nil, err
	}
	return &a, nil
}

func lockAllianceMember(tx *gorm.DB, userId int) (*SectAllianceMember, error) {
	var m SectAllianceMember
	if err := lockForUpdate(tx).Where("user_id = ?", userId).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSectAllianceNotMember
		}
		return nil, err
	}
	return &m, nil
}

// allianceOf 读出玩家所在的仙盟 id(不加锁)，不在仙盟里时返回 ErrSectAllianceNotMember。
func allianceOf(tx *gorm.DB, userId int) (int, error) {
	var ids []int
	if err := tx.Model(&SectAllianceMember{}).Where("user_id = ?", userId).Limit(1).Pluck("alliance_id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, ErrSectAllianceNotMember
	}
	return ids[0], nil
}

// GetSectAllianceOf 返回玩家所在的仙盟与自己的成员行，不在仙盟里时都为 nil。
func GetSectAllianceOf(userId int) (*SectAlliance, *SectAllianceMember, error) {
	var members []SectAllianceMember
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&members).Error; err != nil {
		return nil, nil, err
	}
	if len(members) == 0 {
		return nil, nil, nil
	}
	var alliances []SectAlliance
	if err := DB.Where("id = ?", members[0].AllianceId).Limit(1).Find(&alliances).Error; err != nil {
		return nil, nil, err
	}
	if len(alliances) == 0 {
		return nil, nil, nil
	}
	return &alliances[0], &members[0], nil
}

// GetSectAllianceLevel 返回玩家所在仙盟的等级，不在仙盟里为 0。
func GetSectAllianceLevel(userId int) (int, error) {
	alliance, _, err := GetSectAllianceOf(userId)
	if err != nil || alliance == nil {
		return 0, err
	}
	return alliance.Level, nil
}

// ListSectAlliances 返回等级最高、人数最多的 limit 个仙盟。
func ListSectAlliances(limit int) ([]SectAlliance, error) {
	var rows []SectAlliance
	err := DB.Order("level desc").Order("members desc").Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListSectAllianceMembers 返回仙盟的成员，贡献高的在前。
func ListSectAllianceMembers(allianceId int) ([]SectAllianceMember, error) {
	var rows []SectAllianceMember
	err := DB.Where("alliance_id = ?", allianceId).Order("contribution desc").Order("joined_at asc").Find(&rows).Error
	return rows, err
}

// CreateSectAlliance 建一个仙盟，建盟的掌门当盟主。pay 从宗门存档里扣掉建盟的花费(与建盟在同一个事务里)。
func CreateSectAlliance(userId int, name string, week string, boss SectAllianceBoss, pay func(data string) (string, error)) (*SectAlliance, error) {
	now := common.GetTimestamp()
	alliance := &SectAlliance{Name: name, LeaderId: userId, Level: 1, Members: 1, BossLevel: 1, CreatedAt: now, UpdatedAt: now}
	alliance.rollWeek(week, boss)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := updateGameSaveTx(tx, userId, sectGameKey, pay); err != nil {
			return err
		}
		var taken int64
		if err := tx.Model(&SectAlliance{}).Where("name = ?", name).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrSectAllianceName
		}
		if err := tx.Create(alliance).Error; err != nil {
			return err
		}
		member := &SectAllianceMember{UserId: userId, AllianceId: alliance.Id, Role: SectAllianceRoleLeader, Week: week, JoinedAt: now}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(member)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return ErrSectAllianceJoined
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return alliance, nil
}

// JoinSectAlliance 加入一个仙盟(不用审批)，满员时返回 ErrSectAllianceFull。
func JoinSectAlliance(userId int, allianceId int, week string, boss SectAllianceBoss, capOf func(level int) int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		alliance, err := lockAlliance(tx, allianceId)
		if err != nil {
			return err
		}
		alliance.rollWeek(week, boss)
		if alliance.Members >= capOf(alliance.Level) {
			return ErrSectAllianceFull
		}
		member := &SectAllianceMember{UserId: userId, AllianceId: allianceId, Role: SectAllianceRoleMember, Week: week,
			JoinedAt: common.GetTimestamp()}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(member)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			return ErrSectAllianceJoined
		}
		alliance.Members++
		return saveAlliance(tx, alliance)
	})
}

// LeaveSectAlliance 让 target 离开仙盟：userId 等于 target 时是自己退盟，否则是盟主把 target 请出仙盟。盟主自己退盟时盟主之位交给
// 贡献最高的成员；最后一人离开时仙盟解散。没领的奖励随成员行一起作废。
func LeaveSectAlliance(userId int, target int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, target)
		if err != nil {
			return err
		}
		alliance, err := lockAlliance(tx, allianceId)
		if err != nil {
			return err
		}
		if userId != target && alliance.LeaderId != userId {
			return ErrSectAllianceNotLeader
		}
		member, err := lockAllianceMember(tx, target)
		if err != nil || member.AllianceId != allianceId {
			return ErrSectAllianceNotMember
		}
		if err := tx.Where("user_id = ?", target).Delete(&SectAllianceMember{}).Error; err != nil {
			return err
		}
		alliance.Members--
		if alliance.Members <= 0 {
			return tx.Where("id = ?", allianceId).Delete(&SectAlliance{}).Error
		}
		if alliance.LeaderId == target {
			var next SectAllianceMember
			if err := tx.Where("alliance_id = ?", allianceId).Order("contribution desc").Order("joined_at asc").First(&next).Error; err != nil {
				return err
			}
			alliance.LeaderId = next.UserId
			if err := tx.Model(&SectAllianceMember{}).Where("user_id = ?", next.UserId).Update("role", SectAllianceRoleLeader).Error; err != nil {
				return err
			}
		}
		return saveAlliance(tx, alliance)
	})
}

// DonateSectAlliance 把宗门的资源捐给仙盟：pay 从宗门存档里扣掉资源并返回折成的盟资，盟资与成员的贡献在同一个事务里增加。
func DonateSectAlliance(userId int, pay func(data string) (string, int64, error)) (*SectAlliance, error) {
	var alliance *SectAlliance
	err := DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, userId)
		if err != nil {
			return err
		}
		var funds int64
		if _, err := updateGameSaveTx(tx, userId, sectGameKey, func(data string) (string, error) {
			updated, amount, err := pay(data)
			funds = amount
			return updated, err
		}); err != nil {
			return err
		}
		if alliance, err = lockAlliance(tx, allianceId); err != nil {
			return err
		}
		member, err := lockAllianceMember(tx, userId)
		if err != nil || member.AllianceId != allianceId {
			return ErrSectAllianceNotMember
		}
		if funds <= 0 || funds >= common.MaxQuota-alliance.Funds || funds >= common.MaxQuota-member.Contribution {
			return ErrSectAllianceFunds
		}
		alliance.Funds += funds
		member.Contribution += funds
		if err := saveAllianceMember(tx, member); err != nil {
			return err
		}
		return saveAlliance(tx, alliance)
	})
	return alliance, err
}

// UpgradeSectAlliance 由盟主花盟资把仙盟升一级。costOf 返回 level 级升一级要的盟资，0 表示已经满级。
func UpgradeSectAlliance(userId int, costOf func(level int) int64) (*SectAlliance, error) {
	var alliance *SectAlliance
	err := DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, userId)
		if err != nil {
			return err
		}
		if alliance, err = lockAlliance(tx, allianceId); err != nil {
			return err
		}
		if alliance.LeaderId != userId {
			return ErrSectAllianceNotLeader
		}
		cost := costOf(alliance.Level)
		if cost <= 0 || alliance.Funds < cost {
			return ErrSectAllianceFunds
		}
		alliance.Funds -= cost
		alliance.Level++
		return saveAlliance(tx, alliance)
	})
	return alliance, err
}

// SetSectAllianceNotice 由盟主改仙盟公告。
func SetSectAllianceNotice(userId int, notice string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, userId)
		if err != nil {
			return err
		}
		alliance, err := lockAlliance(tx, allianceId)
		if err != nil {
			return err
		}
		if alliance.LeaderId != userId {
			return ErrSectAllianceNotLeader
		}
		alliance.Notice = notice
		return saveAlliance(tx, alliance)
	})
}

// SectAllianceAttack 是一次讨伐的结果：Damage 是实际打掉的血量(不超过剩余血量)，Killed 表示这一下击杀了 Boss。
type SectAllianceAttack struct {
	Damage int64 `json:"damage"`
	Killed bool  `json:"killed"`
}

// AttackSectAllianceBoss 讨伐本周的 Boss，每人每天一次。attack 按宗门存档算出这次的伤害(会推进存档里的随机数)；reward 是按伤害记给
// 自己的待领奖励。击杀时本周出过力的每名成员另记一份击杀奖励。
func AttackSectAllianceBoss(userId int, day string, week string, boss SectAllianceBoss,
	attack func(data string) (string, int64, error), reward func(damage int64) (stone int, rep int)) (*SectAllianceAttack, error) {
	result := &SectAllianceAttack{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, userId)
		if err != nil {
			return err
		}
		var damage int64
		if _, err := updateGameSaveTx(tx, userId, sectGameKey, func(data string) (string, error) {
			updated, amount, err := attack(data)
			damage = amount
			return updated, err
		}); err != nil {
			return err
		}
		alliance, err := lockAlliance(tx, allianceId)
		if err != nil {
			return err
		}
		alliance.rollWeek(week, boss)
		member, err := lockAllianceMember(tx, userId)
		if err != nil || member.AllianceId != allianceId {
			return ErrSectAllianceNotMember
		}
		member.rollWeek(week)
		if member.AttackDay == day || alliance.BossHp <= 0 {
			return ErrSectAllianceAttacked
		}
		result.Damage = min(max(damage, 1), alliance.BossHp)
		alliance.BossHp -= result.Damage
		member.AttackDay = day
		member.WeekDamage += result.Damage
		stone, rep := reward(result.Damage)
		member.PendingStone += stone
		member.PendingRep += rep
		if err := saveAllianceMember(tx, member); err != nil {
			return err
		}
		if alliance.BossHp <= 0 {
			result.Killed = true
			alliance.BossKills++
			stone, rep, pills := boss.Kill(alliance.BossLevel)
			if err := tx.Model(&SectAllianceMember{}).Where("alliance_id = ? AND week = ? AND week_damage > 0", allianceId, week).
				Updates(map[string]interface{}{
					"pending_stone": gorm.Expr("pending_stone + ?", stone),
					"pending_rep":   gorm.Expr("pending_rep + ?", rep),
					"pending_pills": gorm.Expr("pending_pills + ?", pills),
				}).Error; err != nil {
				return err
			}
		}
		return saveAlliance(tx, alliance)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ClaimSectAllianceRewards 把攒下的讨伐与击杀奖励领进宗门：claim 把灵石、声望与破境丹记进宗门存档，成员的待领奖励在同一个事务里清零。
func ClaimSectAllianceRewards(userId int, claim func(data string, stone int, rep int, pills int) (string, error)) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := allianceOf(tx, userId); err != nil {
			return err
		}
		var member *SectAllianceMember
		if _, err := updateGameSaveTx(tx, userId, sectGameKey, func(data string) (string, error) {
			var err error
			// 存档锁住之后再锁成员行，按锁住时的数额领取。
			if member, err = lockAllianceMember(tx, userId); err != nil {
				return "", err
			}
			if member.PendingStone == 0 && member.PendingRep == 0 && member.PendingPills == 0 {
				return "", ErrSectAllianceNothing
			}
			return claim(data, member.PendingStone, member.PendingRep, member.PendingPills)
		}); err != nil {
			return err
		}
		member.PendingStone, member.PendingRep, member.PendingPills = 0, 0, 0
		return saveAllianceMember(tx, member)
	})
}
