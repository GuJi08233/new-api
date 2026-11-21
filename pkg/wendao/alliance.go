package wendao

import (
	"math"
	"strconv"
	"strings"
)

// 仙盟：几位掌门结成的同盟。盟里的数据(等级、盟资、周 Boss)存在数据库的仙盟表里，这里只放规则：建盟花费、人数上限、升级要的
// 盟资、给成员的产出加成、周 Boss 的轮换机制/血量与讨伐伤害、全盟协作目标、个人周目标以及讨伐与击杀的奖励。
// 仙盟只发宗门资源与丹药，不发游戏币。

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

// 三套按 ISO 周轮换的仙盟周首领机制，换周时快照在仙盟表上，整周不变：
//   - iron_shell（玄甲妖王）：满编 3 名弟子同阵破甲，讨伐伤害 +20%；少于 3 人按常值结算。
//   - blood_fury（狂血魔尊）：妖王气血低于 50% 时露出破绽，讨伐伤害 +25%。
//   - spirit_tide（聚灵古蛟）：同盟本周每多 1 名其他道友先出手获得 +10% 共鸣（最多 +30%），且出战弟子涵盖至少 2 种五行另 +10%。
const (
	AllianceBossIronShell  = "iron_shell"
	AllianceBossBloodFury  = "blood_fury"
	AllianceBossSpiritTide = "spirit_tide"

	allianceIronShellBonus   = 20
	allianceBloodFuryBonus   = 25
	allianceSpiritPeerBonus  = 10
	allianceSpiritPeerCap    = 3
	allianceSpiritElemBonus  = 10
	allianceStageKillBit     = 1 << 2
)

// AllianceBossKinds 是按周轮换的三套周首领机制顺序。
var AllianceBossKinds = []string{AllianceBossIronShell, AllianceBossBloodFury, AllianceBossSpiritTide}

// AllianceBossRecord 记在掌门存档里，跨仙盟保留本周已取得的周首领资格与最近讨伐日期，防止退盟换盟重复讨伐或重复领奖。
type AllianceBossRecord struct {
	Week         string `json:"week,omitempty"`
	AttackDay    string `json:"attack_day,omitempty"`
	Attacks      int    `json:"attacks,omitempty"`
	StageMask    int    `json:"stage_mask,omitempty"`
	PersonalMask int    `json:"personal_mask,omitempty"`
}

// AllianceBossState 返回掌门在 week 这一周的跨盟延续资格：AttackDay 始终保留（同日换盟不能再打），周次、全盟阶段与个人目标
// 位图只在同周内延续，跨周归零。
func (s *Sect) AllianceBossState(week string) (attackDay string, attacks int, stageMask int, personalMask int) {
	if s == nil || s.AllianceBoss == nil {
		return "", 0, 0, 0
	}
	rec := s.AllianceBoss
	if rec.Week == week {
		return rec.AttackDay, rec.Attacks, rec.StageMask, rec.PersonalMask
	}
	return rec.AttackDay, 0, 0, 0
}

// SyncAllianceBoss 把仙盟成员行上的周期资格合并进掌门存档。同一周内只增不减，跨周切换为新周记录。
func (s *Sect) SyncAllianceBoss(week string, attackDay string, attacks int, stageMask int, personalMask int) {
	if s == nil || (week == "" && attackDay == "") {
		return
	}
	if s.AllianceBoss == nil {
		s.AllianceBoss = &AllianceBossRecord{}
	}
	rec := s.AllianceBoss
	if attackDay != "" && attackDay > rec.AttackDay {
		rec.AttackDay = attackDay
	}
	if week == "" {
		return
	}
	if rec.Week != week {
		rec.Week = week
		rec.Attacks = max(attacks, 0)
		rec.StageMask = stageMask
		rec.PersonalMask = personalMask
		return
	}
	if attacks > rec.Attacks {
		rec.Attacks = attacks
	}
	rec.StageMask |= stageMask
	rec.PersonalMask |= personalMask
}

// AllianceBossKindForWeek 按 ISO 周字符串(如 "2026-W44")确定本周轮换的首领机制。
func AllianceBossKindForWeek(week string) string {
	yearStr, weekStr, ok := strings.Cut(week, "-W")
	if !ok {
		return AllianceBossIronShell
	}
	year, errY := strconv.Atoi(yearStr)
	num, errW := strconv.Atoi(weekStr)
	if errY != nil || errW != nil || num <= 0 {
		return AllianceBossIronShell
	}
	idx := (year*53 + num) % len(AllianceBossKinds)
	if idx < 0 {
		idx += len(AllianceBossKinds)
	}
	return AllianceBossKinds[idx]
}

// AllianceStageGoal 是全盟协作阶段目标：削血达到 DamagePct%（100% 为击杀）时，本周出过力的成员均记一份阶段奖励。
type AllianceStageGoal struct {
	Stage     int `json:"stage"`
	Bit       int `json:"bit"`
	DamagePct int `json:"damage_pct"`
}

// AllianceStageGoals 是全盟周首领的三档协作目标：削血 30%、削血 60% 与 100% 击杀。
var AllianceStageGoals = []AllianceStageGoal{
	{Stage: 1, Bit: 1 << 0, DamagePct: 30},
	{Stage: 2, Bit: 1 << 1, DamagePct: 60},
	{Stage: 3, Bit: allianceStageKillBit, DamagePct: 100},
}

// AlliancePersonalGoal 是个人周目标：只要本周累计讨伐达到 Attacks 次即可获得，保证低活跃仙盟也有稳定的个人目标。
type AlliancePersonalGoal struct {
	Id      int `json:"id"`
	Bit     int `json:"bit"`
	Attacks int `json:"attacks"`
}

// AlliancePersonalGoals 是每周三档个人讨伐目标：1 次、3 次、5 次。
var AlliancePersonalGoals = []AlliancePersonalGoal{
	{Id: 1, Bit: 1 << 0, Attacks: 1},
	{Id: 2, Bit: 1 << 1, Attacks: 3},
	{Id: 3, Bit: 1 << 2, Attacks: 5},
}

// AllianceStageReachedMask 返回当前血量下已达成的全盟协作阶段位图。
func AllianceStageReachedMask(hp int64, maxHp int64) int {
	if maxHp <= 0 {
		return 0
	}
	clamped := max(hp, int64(0))
	dealt := max(maxHp-clamped, int64(0))
	mask := 0
	for _, g := range AllianceStageGoals {
		if g.DamagePct >= 100 {
			if clamped <= 0 {
				mask |= g.Bit
			}
			continue
		}
		if dealt*100 >= int64(g.DamagePct)*maxHp {
			mask |= g.Bit
		}
	}
	return mask
}

// AllianceStageReward 返回 level 级周首领第 stageBit 档协作阶段的奖励（灵石、声望与破境丹）。
func AllianceStageReward(level int, stageBit int) (Cost, int) {
	lv := max(level, 1)
	switch stageBit {
	case 1 << 0:
		return Cost{Stone: 120 * lv, Rep: 12 * lv}, 0
	case 1 << 1:
		return Cost{Stone: 200 * lv, Rep: 20 * lv}, 0
	case allianceStageKillBit:
		return AllianceKillReward(lv), AllianceKillPills
	default:
		return Cost{}, 0
	}
}

// AlliancePersonalReachedMask 返回本周讨伐 attacks 次已达成的个人周目标位图。
func AlliancePersonalReachedMask(attacks int) int {
	mask := 0
	for _, g := range AlliancePersonalGoals {
		if attacks >= g.Attacks {
			mask |= g.Bit
		}
	}
	return mask
}

// AlliancePersonalReward 返回 level 级周首领第 goalBit 档个人周目标的奖励（灵石、声望与破境丹）。
func AlliancePersonalReward(level int, goalBit int) (Cost, int) {
	lv := max(level, 1)
	switch goalBit {
	case 1 << 0:
		return Cost{Stone: 100 * lv, Rep: 10 * lv}, 0
	case 1 << 1:
		return Cost{Stone: 200 * lv, Rep: 20 * lv}, 0
	case 1 << 2:
		return Cost{Stone: 350 * lv, Rep: 35 * lv}, 1
	default:
		return Cost{}, 0
	}
}

// AllianceBossContext 是本次讨伐时周首领的当前局面，用来计算周机制加成。
type AllianceBossContext struct {
	Kind           string
	Hp             int64
	MaxHp          int64
	PriorAttackers int
}

// AllianceAttackCalc 是单次讨伐的伤害拆分：BaseDamage 为弟子基础伤害，BonusPct 为周首领机制加成百分数，Damage 为最终伤害。
type AllianceAttackCalc struct {
	BaseDamage int64
	BonusPct   int
	Damage     int64
}

// AllianceBossBonusPct 按本周首领机制与当前阵容/局面算出增伤百分数（只增不减，不惩罚小盟）。
func AllianceBossBonusPct(kind string, teamCount int, distinctElements int, hp int64, maxHp int64, priorAttackers int) int {
	switch kind {
	case AllianceBossIronShell:
		if teamCount >= TeamSize {
			return allianceIronShellBonus
		}
	case AllianceBossBloodFury:
		if maxHp > 0 && hp*2 < maxHp {
			return allianceBloodFuryBonus
		}
	case AllianceBossSpiritTide:
		bonus := min(max(priorAttackers, 0), allianceSpiritPeerCap) * allianceSpiritPeerBonus
		if distinctElements >= 2 {
			bonus += allianceSpiritElemBonus
		}
		return bonus
	}
	return 0
}

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

// AllianceAttack 是宗门派 team(一到 TeamSize 名在宗门里、没受伤的弟子)讨伐周 Boss 造成的基础伤害：出战弟子的生命总和上下浮动一成。
func (s *Sect) AllianceAttack(team []int, now int64) (int64, error) {
	calc, err := s.AllianceAttackWithBoss(team, now, AllianceBossContext{})
	if err != nil {
		return 0, err
	}
	return calc.Damage, nil
}

// AllianceAttackWithBoss 在基础伤害之上叠加上本周首领机制加成，返回基础伤害、加成比例与最终伤害。
func (s *Sect) AllianceAttackWithBoss(team []int, now int64, ctx AllianceBossContext) (AllianceAttackCalc, error) {
	fighters, err := s.teamFighters(team, now)
	if err != nil {
		return AllianceAttackCalc{}, err
	}
	total := 0
	elemSeen := map[int]bool{}
	for i, f := range fighters {
		total += f.MaxHp
		if d := s.disciple(team[i]); d != nil {
			elemSeen[d.Element] = true
		}
	}
	base := int64(math.Round(float64(total) * float64(s.Rng.Range(90, 110)) / 100))
	bonusPct := AllianceBossBonusPct(ctx.Kind, len(fighters), len(elemSeen), ctx.Hp, ctx.MaxHp, ctx.PriorAttackers)
	damage := max(int64(1), base*int64(100+bonusPct)/100)
	return AllianceAttackCalc{BaseDamage: base, BonusPct: bonusPct, Damage: damage}, nil
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
