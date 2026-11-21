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

// sectGameKey 是问道的存档标识(与 pkg/wendao.SaveKey 一致)：仙盟的花费与奖励记在掌门存档里的宗门上。
const sectGameKey = "wendao"

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

// SectAlliance 是一个仙盟。Week 是周 Boss 所在的周(例如 2026-W44)，换周时按上周是否击杀决定新 Boss 的等级、回满血、重置
// 阶段位图并快照本周轮换的首领机制(BossKind)。
type SectAlliance struct {
	Id            int    `json:"id"`
	Name          string `json:"name" gorm:"type:varchar(64);uniqueIndex"`
	LeaderId      int    `json:"leader_id" gorm:"index"`
	Level         int    `json:"level" gorm:"index;not null;default:1"`
	Funds         int64  `json:"funds" gorm:"type:bigint;not null;default:0"`
	Members       int    `json:"members" gorm:"not null;default:0"`
	Notice        string `json:"notice" gorm:"type:varchar(255)"`
	Week          string `json:"week" gorm:"type:varchar(10)"`
	BossKind      string `json:"boss_kind" gorm:"type:varchar(32)"`
	BossLevel     int    `json:"boss_level" gorm:"not null;default:1"`
	BossHp        int64  `json:"boss_hp" gorm:"type:bigint;not null;default:0"`
	BossMaxHp     int64  `json:"boss_max_hp" gorm:"type:bigint;not null;default:0"`
	BossStageMask int    `json:"boss_stage_mask" gorm:"not null;default:0"`
	BossKills     int    `json:"boss_kills" gorm:"not null;default:0"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

// SectAllianceMember 是仙盟的一名成员(一位掌门只能在一个仙盟里)。WeekDamage 是本周对 Boss 造成的伤害，WeekAttacks 是本周
// 讨伐次数，WeekStageMask/WeekPersonalMask 记本周已记入待领的全盟阶段与个人周目标位图，AttackDay 是最近一次讨伐的日期；
// Pending* 是讨伐、阶段、个人目标与击杀攒下、还没领进宗门的奖励。
type SectAllianceMember struct {
	UserId           int    `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	AllianceId       int    `json:"alliance_id" gorm:"index"`
	Role             string `json:"role" gorm:"type:varchar(16)"`
	Contribution     int64  `json:"contribution" gorm:"type:bigint;not null;default:0"`
	Week             string `json:"week" gorm:"type:varchar(10)"`
	WeekDamage       int64  `json:"week_damage" gorm:"type:bigint;not null;default:0"`
	WeekAttacks      int    `json:"week_attacks" gorm:"not null;default:0"`
	WeekStageMask    int    `json:"week_stage_mask" gorm:"not null;default:0"`
	WeekPersonalMask int    `json:"week_personal_mask" gorm:"not null;default:0"`
	AttackDay        string `json:"attack_day" gorm:"type:varchar(10)"`
	PendingStone     int    `json:"pending_stone" gorm:"not null;default:0"`
	PendingRep       int    `json:"pending_rep" gorm:"not null;default:0"`
	PendingPills     int    `json:"pending_pills" gorm:"not null;default:0"`
	JoinedAt         int64  `json:"joined_at" gorm:"bigint"`
}

// SectAllianceBoss 是周 Boss 的规则：hp 按 Boss 等级与仙盟人数给出血量，kill 是击杀奖励，Kind/Stage*/Personal* 提供周机制
// 快照、全盟阶段目标与个人周目标规则。未提供 Stage*/Personal* 时退回仅按击杀发奖的最小行为。
type SectAllianceBoss struct {
	Hp             func(level int, members int) int64
	Kill           func(level int) (stone int, rep int, pills int)
	Kind           func(week string) string
	StageMask      func(hp int64, maxHp int64) int
	StageReward    func(level int, stageBit int) (stone int, rep int, pills int)
	PersonalMask   func(attacks int) int
	PersonalReward func(level int, goalBit int) (stone int, rep int, pills int)
}

var allianceGoalBits = [...]int{1 << 0, 1 << 1, 1 << 2}

func (b SectAllianceBoss) kindFor(week string) string {
	if b.Kind != nil {
		return b.Kind(week)
	}
	return ""
}

func (b SectAllianceBoss) stageMaskFor(hp int64, maxHp int64) int {
	if b.StageMask != nil {
		return b.StageMask(hp, maxHp)
	}
	if hp <= 0 {
		return 1 << 2
	}
	return 0
}

func (b SectAllianceBoss) stageRewardFor(level int, bit int) (int, int, int) {
	if b.StageReward != nil {
		return b.StageReward(level, bit)
	}
	if bit == 1<<2 && b.Kill != nil {
		return b.Kill(level)
	}
	return 0, 0, 0
}

func (b SectAllianceBoss) personalMaskFor(attacks int) int {
	if b.PersonalMask != nil {
		return b.PersonalMask(attacks)
	}
	return 0
}

func (b SectAllianceBoss) personalRewardFor(level int, bit int) (int, int, int) {
	if b.PersonalReward != nil {
		return b.PersonalReward(level, bit)
	}
	return 0, 0, 0
}

// SectAllianceCarry 是掌门存档里带进新仙盟的同周资格，防止退盟换盟重复讨伐或重复领取阶段、击杀与个人目标奖励。
type SectAllianceCarry struct {
	AttackDay    string
	Attacks      int
	StageMask    int
	PersonalMask int
}

// rollWeek 在换周时重置周 Boss：上周击杀了就升一级，血量按现在的人数回满，重置阶段位图并快照本周机制。
func (a *SectAlliance) rollWeek(week string, boss SectAllianceBoss) {
	if a.Week == week {
		if a.BossKind == "" {
			a.BossKind = boss.kindFor(week)
		}
		return
	}
	if a.Week != "" && a.BossHp <= 0 {
		a.BossLevel++
	}
	a.Week = week
	a.BossKind = boss.kindFor(week)
	a.BossMaxHp = boss.Hp(a.BossLevel, a.Members)
	a.BossHp = a.BossMaxHp
	a.BossStageMask = 0
}

func (m *SectAllianceMember) rollWeek(week string) {
	if m.Week != week {
		m.Week, m.WeekDamage, m.WeekAttacks, m.WeekStageMask, m.WeekPersonalMask = week, 0, 0, 0, 0
	}
}

// saveAlliance 写回仙盟的可变字段。
func saveAlliance(tx *gorm.DB, a *SectAlliance) error {
	a.UpdatedAt = common.GetTimestamp()
	return tx.Model(&SectAlliance{}).Where("id = ?", a.Id).Updates(map[string]interface{}{
		"leader_id": a.LeaderId, "level": a.Level, "funds": a.Funds, "members": a.Members, "notice": a.Notice,
		"week": a.Week, "boss_kind": a.BossKind, "boss_level": a.BossLevel, "boss_hp": a.BossHp,
		"boss_max_hp": a.BossMaxHp, "boss_stage_mask": a.BossStageMask, "boss_kills": a.BossKills,
		"updated_at": a.UpdatedAt,
	}).Error
}

func saveAllianceMember(tx *gorm.DB, m *SectAllianceMember) error {
	return tx.Model(&SectAllianceMember{}).Where("user_id = ?", m.UserId).Updates(map[string]interface{}{
		"role": m.Role, "contribution": m.Contribution, "week": m.Week, "week_damage": m.WeekDamage,
		"week_attacks": m.WeekAttacks, "week_stage_mask": m.WeekStageMask, "week_personal_mask": m.WeekPersonalMask,
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

// CreateSectAlliance 建一个仙盟，建盟的掌门当盟主。pay 从宗门存档里扣掉建盟的花费并返回掌门的本周周首领延续资格(与建盟在同一个事务里)。
func CreateSectAlliance(userId int, name string, week string, boss SectAllianceBoss, pay func(data string) (string, SectAllianceCarry, error)) (*SectAlliance, error) {
	now := common.GetTimestamp()
	alliance := &SectAlliance{Name: name, LeaderId: userId, Level: 1, Members: 1, BossLevel: 1, CreatedAt: now, UpdatedAt: now}
	alliance.rollWeek(week, boss)
	err := DB.Transaction(func(tx *gorm.DB) error {
		var carry SectAllianceCarry
		if _, err := updateGameSaveTx(tx, userId, sectGameKey, func(data string) (string, error) {
			updated, c, err := pay(data)
			carry = c
			return updated, err
		}); err != nil {
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
		member := &SectAllianceMember{
			UserId: userId, AllianceId: alliance.Id, Role: SectAllianceRoleLeader, Week: week,
			AttackDay: carry.AttackDay, WeekAttacks: carry.Attacks, WeekStageMask: carry.StageMask,
			WeekPersonalMask: carry.PersonalMask, JoinedAt: now,
		}
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

// JoinSectAlliance 加入一个仙盟(不用审批)，满员时返回 ErrSectAllianceFull。readCarry 在锁住掌门存档后校验开宗状态并读出本周
// 延续资格(保持 存档 → 仙盟 → 成员 的加锁顺序)，防止换盟重复讨伐或重复领奖。
func JoinSectAlliance(userId int, allianceId int, week string, boss SectAllianceBoss, capOf func(level int) int,
	readCarry func(data string) (SectAllianceCarry, error)) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var carry SectAllianceCarry
		if readCarry != nil {
			save, err := lockGameSaveTx(tx, userId, sectGameKey)
			if err != nil {
				return err
			}
			carry, err = readCarry(save.Data)
			if err != nil {
				return err
			}
		}
		alliance, err := lockAlliance(tx, allianceId)
		if err != nil {
			return err
		}
		alliance.rollWeek(week, boss)
		if alliance.Members >= capOf(alliance.Level) {
			return ErrSectAllianceFull
		}
		member := &SectAllianceMember{
			UserId: userId, AllianceId: allianceId, Role: SectAllianceRoleMember, Week: week,
			AttackDay: carry.AttackDay, WeekAttacks: carry.Attacks, WeekStageMask: carry.StageMask,
			WeekPersonalMask: carry.PersonalMask, JoinedAt: common.GetTimestamp(),
		}
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
// 贡献最高的成员；最后一人离开时仙盟解散。没领的奖励随成员行一起作废；syncSave 把本周已用资格写回 target 的存档。
func LeaveSectAlliance(userId int, target int, syncSave func(data string, m *SectAllianceMember) (string, error)) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, target)
		if err != nil {
			return err
		}
		var save *GameSave
		if syncSave != nil {
			if save, err = lockGameSaveTx(tx, target, sectGameKey); err != nil {
				return err
			}
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
			if err := tx.Where("id = ?", allianceId).Delete(&SectAlliance{}).Error; err != nil {
				return err
			}
		} else {
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
			if err := saveAlliance(tx, alliance); err != nil {
				return err
			}
		}
		if syncSave != nil && save != nil {
			updated, err := syncSave(save.Data, member)
			if err != nil {
				return err
			}
			if updated != save.Data {
				return writeGameSaveTx(tx, save, updated)
			}
		}
		return nil
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

// SectAllianceAttackCtx 是锁住仙盟后交给规则层计算本次伤害的局面，含周机制、当前血量与本周已先出手的同盟人数。
type SectAllianceAttackCtx struct {
	BossKind        string
	BossLevel       int
	BossHp          int64
	BossMaxHp       int64
	PriorAttackers  int
	AlreadyAttacked bool
}

// SectAllianceAttackPlan 是规则层算出的本次讨伐计划。
type SectAllianceAttackPlan struct {
	SaveData   string
	BaseDamage int64
	BonusPct   int
	Damage     int64
}

// SectAllianceAttack 是一次讨伐的结果：Damage 是实际打掉的血量(不超过剩余血量)，BonusPct 是周机制增伤百分数，
// Killed 表示这一下击杀了 Boss，StagesGained / PersonalGained 记本次新达成的全盟阶段与个人周目标。
type SectAllianceAttack struct {
	Damage         int64 `json:"damage"`
	BaseDamage     int64 `json:"base_damage"`
	BonusPct       int   `json:"bonus_pct"`
	Killed         bool  `json:"killed"`
	StagesGained   []int `json:"stages_gained,omitempty"`
	PersonalGained []int `json:"personal_gained,omitempty"`
	RewardStone    int   `json:"reward_stone"`
	RewardRep      int   `json:"reward_rep"`
	RewardPills    int   `json:"reward_pills,omitempty"`
}

// AttackSectAllianceBoss 讨伐本周的 Boss，每人每天一次。加锁顺序固定为 掌门存档 → 仙盟 → 成员(按编号升序)：
// prepare 先校验幂等收据再按周机制计算伤害，结算全盟阶段(30%/60%/击杀)与个人周目标(1/3/5 次)，并由 finish 把最新周期资格写回存档。
func AttackSectAllianceBoss(userId int, day string, week string, boss SectAllianceBoss,
	prepare func(data string, ctx SectAllianceAttackCtx) (SectAllianceAttackPlan, error),
	reward func(damage int64) (stone int, rep int),
	finish func(data string, m *SectAllianceMember) (string, error)) (*SectAllianceAttack, error) {
	result := &SectAllianceAttack{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		allianceId, err := allianceOf(tx, userId)
		if err != nil {
			return err
		}
		save, err := lockGameSaveTx(tx, userId, sectGameKey)
		if err != nil {
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
		var prior int64
		if err := tx.Model(&SectAllianceMember{}).
			Where("alliance_id = ? AND week = ? AND week_damage > 0 AND user_id <> ?", allianceId, week, userId).
			Count(&prior).Error; err != nil {
			return err
		}
		already := member.AttackDay == day || alliance.BossHp <= 0
		plan, err := prepare(save.Data, SectAllianceAttackCtx{
			BossKind:        alliance.BossKind,
			BossLevel:       alliance.BossLevel,
			BossHp:          alliance.BossHp,
			BossMaxHp:       alliance.BossMaxHp,
			PriorAttackers:  int(prior),
			AlreadyAttacked: already,
		})
		if err != nil {
			return err
		}
		if already {
			return ErrSectAllianceAttacked
		}
		result.BaseDamage = plan.BaseDamage
		result.BonusPct = plan.BonusPct
		result.Damage = min(max(plan.Damage, 1), alliance.BossHp)
		alliance.BossHp -= result.Damage
		member.AttackDay = day
		member.WeekDamage += result.Damage
		member.WeekAttacks++
		stone, rep := reward(result.Damage)
		member.PendingStone += stone
		member.PendingRep += rep
		result.RewardStone += stone
		result.RewardRep += rep

		// 个人周目标：只要本周讨伐次数达标即可入待领，不依赖全盟是否击杀。
		personalMask := boss.personalMaskFor(member.WeekAttacks)
		newPersonal := personalMask &^ member.WeekPersonalMask
		for idx, bit := range allianceGoalBits {
			if newPersonal&bit == 0 {
				continue
			}
			ps, pr, pp := boss.personalRewardFor(alliance.BossLevel, bit)
			member.PendingStone += ps
			member.PendingRep += pr
			member.PendingPills += pp
			result.RewardStone += ps
			result.RewardRep += pr
			result.RewardPills += pp
			member.WeekPersonalMask |= bit
			result.PersonalGained = append(result.PersonalGained, idx+1)
		}

		// 全盟协作阶段(30%/60%/100% 击杀)：出过力的成员每阶段每周最多领一次，跨盟通过 WeekStageMask 防重。
		reachedStages := boss.stageMaskFor(alliance.BossHp, alliance.BossMaxHp)
		newlyCrossed := reachedStages &^ alliance.BossStageMask
		alliance.BossStageMask |= reachedStages
		missingForSelf := reachedStages &^ member.WeekStageMask
		for idx, bit := range allianceGoalBits {
			if missingForSelf&bit == 0 {
				continue
			}
			ss, sr, sp := boss.stageRewardFor(alliance.BossLevel, bit)
			member.PendingStone += ss
			member.PendingRep += sr
			member.PendingPills += sp
			result.RewardStone += ss
			result.RewardRep += sr
			result.RewardPills += sp
			member.WeekStageMask |= bit
			result.StagesGained = append(result.StagesGained, idx+1)
		}
		if newlyCrossed != 0 {
			var others []SectAllianceMember
			if err := lockForUpdate(tx).
				Where("alliance_id = ? AND week = ? AND week_damage > 0 AND user_id <> ?", allianceId, week, userId).
				Order("user_id asc").Find(&others).Error; err != nil {
				return err
			}
			for i := range others {
				addMask := newlyCrossed &^ others[i].WeekStageMask
				if addMask == 0 {
					continue
				}
				for _, bit := range allianceGoalBits {
					if addMask&bit == 0 {
						continue
					}
					ss, sr, sp := boss.stageRewardFor(alliance.BossLevel, bit)
					others[i].PendingStone += ss
					others[i].PendingRep += sr
					others[i].PendingPills += sp
					others[i].WeekStageMask |= bit
				}
				if err := saveAllianceMember(tx, &others[i]); err != nil {
					return err
				}
			}
		}
		if alliance.BossHp <= 0 {
			result.Killed = true
			alliance.BossKills++
		}
		finalData := plan.SaveData
		if finish != nil {
			if finalData, err = finish(finalData, member); err != nil {
				return err
			}
		}
		if err := writeGameSaveTx(tx, save, finalData); err != nil {
			return err
		}
		if err := saveAllianceMember(tx, member); err != nil {
			return err
		}
		return saveAlliance(tx, alliance)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ClaimSectAllianceRewards 把攒下的讨伐、阶段、个人目标与击杀奖励领进宗门：claim 在校验幂等收据与待领数额后写进宗门存档，
// 成员的待领奖励在同一个事务里清零。
func ClaimSectAllianceRewards(userId int, claim func(data string, m *SectAllianceMember) (string, error)) error {
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
			return claim(data, member)
		}); err != nil {
			return err
		}
		member.PendingStone, member.PendingRep, member.PendingPills = 0, 0, 0
		return saveAllianceMember(tx, member)
	})
}
