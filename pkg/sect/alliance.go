package sect

import "math"

// 仙盟：几位掌门结成的同盟。盟里的数据(等级、盟资、周 Boss)存在数据库的仙盟表里，这里只放规则：建盟花费、人数上限、升级要的
// 盟资、给成员的产出加成、周 Boss 的血量与讨伐伤害、讨伐与击杀的奖励。仙盟只发宗门资源与丹药，不发游戏币。

const (
	// AllianceMinHall 是建盟要求的主殿等级。
	AllianceMinHall = 2
	// AllianceMaxLevel 是仙盟的最高等级。
	AllianceMaxLevel = 5
	// AllianceNameMaxRunes 是盟名最多几个字。
	AllianceNameMaxRunes = 12
	// AllianceBonusPerLevel 是仙盟每级给成员宗门的产出加成。
	AllianceBonusPerLevel = 0.03
	// allianceBossBaseHp 是一级周 Boss 每名成员对应的血量，每升一级乘 allianceBossGrowth。
	allianceBossBaseHp = 1200
	allianceBossGrowth = 1.6
)

// AllianceCreateCost 是建盟的花费。
var AllianceCreateCost = Cost{Stone: 1000, Rep: 50}

// allianceUpgradeFunds[i] 是仙盟从 i+1 级升到 i+2 级要花的盟资。
var allianceUpgradeFunds = [AllianceMaxLevel - 1]int64{3000, 10000, 30000, 80000}

// FoundAlliance 扣掉建盟的花费：主殿要到 AllianceMinHall 级。
func (s *Sect) FoundAlliance() error {
	if s.Buildings[BuildingHall] < AllianceMinHall {
		return ErrUnavailable
	}
	return s.pay(AllianceCreateCost)
}

// AllianceCap 是 level 级仙盟的人数上限。
func AllianceCap(level int) int { return 5 + 5*level }

// AllianceUpgradeFunds 是 level 级仙盟升一级要花的盟资，已经满级时返回 0。
func AllianceUpgradeFunds(level int) int64 {
	if level < 1 || level >= AllianceMaxLevel {
		return 0
	}
	return allianceUpgradeFunds[level-1]
}

// DonationFunds 是捐出这些资源折成的盟资(也是记给捐献者的贡献)：灵石、灵草一份算一点，玄铁难得，一份算三点。声望不能捐。
func DonationFunds(c Cost) int64 {
	if c.Stone < 0 || c.Herb < 0 || c.Ore < 0 || c.Rep != 0 {
		return -1
	}
	return int64(c.Stone) + int64(c.Herb) + 3*int64(c.Ore)
}

// Donate 从宗门里拿出要捐的资源，返回折成的盟资。资源不够或什么都没捐时宗门不变。
func (s *Sect) Donate(c Cost) (int64, error) {
	funds := DonationFunds(c)
	if funds <= 0 {
		return 0, ErrAction
	}
	if err := s.pay(c); err != nil {
		return 0, err
	}
	return funds, nil
}

// AllianceBossHp 是 level 级周 Boss 的血量，按开周时仙盟的人数算：人多 Boss 也厚。
func AllianceBossHp(level int, members int) int64 {
	return int64(math.Round(allianceBossBaseHp * math.Pow(allianceBossGrowth, float64(level-1)) * float64(max(members, 1))))
}

// AllianceAttack 是宗门派 team(一到 TeamSize 名在宗门里、没受伤的弟子)讨伐周 Boss 造成的伤害：出战弟子的生命总和上下浮动一成。
func (s *Sect) AllianceAttack(team []int, now int64) (int64, error) {
	fighters, err := s.teamFighters(team, now)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, f := range fighters {
		total += f.MaxHp
	}
	return int64(math.Round(float64(total) * float64(s.Rng.Range(90, 110)) / 100)), nil
}

// AllianceAttackReward 是一次讨伐记给成员的奖励：伤害的四分之一折成灵石，四十分之一折成声望(至少 1)。
func AllianceAttackReward(damage int64) Cost {
	return Cost{Stone: int(damage / 4), Rep: int(max(damage/40, 1))}
}

// AllianceKillReward 是 level 级周 Boss 被击杀时，本周出过力的每名成员另得的奖励(丹药另给一颗破境丹)。
func AllianceKillReward(level int) Cost {
	return Cost{Stone: 400 * level, Rep: 40 * level}
}

// AllianceKillPills 是击杀周 Boss 时每名出过力的成员另得几颗破境丹。
const AllianceKillPills = 1

// ClaimAlliance 把仙盟里待领的奖励记进宗门。
func (s *Sect) ClaimAlliance(reward Cost, pills int) []Ev {
	evs := s.gain(reward)
	if pills > 0 {
		s.Pills[PillBreak] += pills
		evs = append(evs, Ev{K: "pill", S: PillBreak, N: pills})
	}
	return evs
}
