package controller

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 问道宗门的仙盟：几位掌门结成同盟，捐资源升级仙盟(给成员产出加成)，每周合力讨伐一个 Boss。仙盟与成员存在数据库里，花费与
// 奖励在同一个事务里记进掌门的宗门存档(见 model/sect_alliance.go)。仙盟只发宗门资源与丹药，不发游戏币。

const (
	// sectAllianceList 是没有入盟时列出几个仙盟。
	sectAllianceList = 20
	// sectAllianceNoticeRunes 是仙盟公告最多几个字。
	sectAllianceNoticeRunes = 100
)

// sectAllianceBoss 是周 Boss 的规则(血量、轮换机制、全盟协作阶段与个人周目标)，来自 pkg/wendao。
var sectAllianceBoss = model.SectAllianceBoss{
	Hp:   wendao.AllianceBossHp,
	Kind: wendao.AllianceBossKindForWeek,
	Kill: func(level int) (int, int, int) {
		reward := wendao.AllianceKillReward(level)
		return reward.Stone, reward.Rep, wendao.AllianceKillPills
	},
	StageMask: wendao.AllianceStageReachedMask,
	StageReward: func(level int, stageBit int) (int, int, int) {
		reward, pills := wendao.AllianceStageReward(level, stageBit)
		return reward.Stone, reward.Rep, pills
	},
	PersonalMask: wendao.AlliancePersonalReachedMask,
	PersonalReward: func(level int, goalBit int) (int, int, int) {
		reward, pills := wendao.AlliancePersonalReward(level, goalBit)
		return reward.Stone, reward.Rep, pills
	},
}

func readSectAllianceCarry(data string, week string) (model.SectAllianceCarry, error) {
	save, err := wendao.ParseSave(data)
	if err != nil {
		return model.SectAllianceCarry{}, err
	}
	if save.Sect == nil || !save.Sect.IsFounded() {
		return model.SectAllianceCarry{}, wendao.ErrUnavailable
	}
	attackDay, attacks, stageMask, personalMask := save.Sect.AllianceBossState(week)
	return model.SectAllianceCarry{
		AttackDay: attackDay, Attacks: attacks, StageMask: stageMask, PersonalMask: personalMask,
	}, nil
}

func syncSectAllianceBossSave(data string, m *model.SectAllianceMember) (string, error) {
	return wendao.UpdateSave(data, func(save *wendao.Save) error {
		if save.Sect == nil {
			return nil
		}
		save.Sect.SyncAllianceBoss(m.Week, m.AttackDay, m.WeekAttacks, m.WeekStageMask, m.WeekPersonalMask)
		return nil
	})
}

// sectAllianceWeek 是周 Boss 所在的周，按服务器本地时间的 ISO 周计算(与游戏中心按本地 0 点切日一致)。
func sectAllianceWeek() string {
	year, week := time.Now().ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// respondAllianceError 把仙盟与宗门规则的错误写成响应。
func respondAllianceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrSectAllianceJoined):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceJoined)
	case errors.Is(err, model.ErrSectAllianceNotMember):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceNotMember)
	case errors.Is(err, model.ErrSectAllianceNotLeader):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceNotLeader)
	case errors.Is(err, model.ErrSectAllianceFull):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceFull)
	case errors.Is(err, model.ErrSectAllianceName):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceName)
	case errors.Is(err, model.ErrSectAllianceFunds):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceFunds)
	case errors.Is(err, model.ErrSectAllianceAttacked):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceAttacked)
	case errors.Is(err, model.ErrSectAllianceNothing):
		common.ApiErrorI18n(c, i18n.MsgGameAllianceNothing)
	default:
		respondSectError(c, err)
	}
}

// updateSectSave 是改写宗门存档的一步：还没有宗门时报 ErrUnavailable(仙盟的操作都要先有宗门)。
func updateSectSave(data string, edit func(s *wendao.Sect) error) (string, error) {
	return wendao.UpdateSave(data, func(save *wendao.Save) error {
		// 仙盟只收开了宗的掌门，散修与宗门弟子的储物处不算。
		if save.Sect == nil || !save.Sect.IsFounded() {
			return wendao.ErrUnavailable
		}
		return edit(save.Sect)
	})
}

// sectAllianceView 是仙盟页：自己所在的仙盟(成员、周 Boss、升级要的盟资)，没有入盟时列出现有的仙盟。
func sectAllianceView(userId int) (gin.H, error) {
	alliance, member, err := model.GetSectAllianceOf(userId)
	if err != nil {
		return nil, err
	}
	rules := gin.H{
		"create_cost":     wendao.AllianceCreateCost,
		"min_hall":        wendao.AllianceMinHall,
		"max_level":       wendao.AllianceMaxLevel,
		"bonus_per_level": wendao.AllianceBonusPerLevel,
		"name_max":        wendao.AllianceNameMaxRunes,
		"notice_max":      sectAllianceNoticeRunes,
	}
	view := gin.H{"rules": rules, "self": userId, "day": gameCenterDay(), "week": sectAllianceWeek()}
	if alliance == nil {
		list, err := model.ListSectAlliances(sectAllianceList)
		if err != nil {
			return nil, err
		}
		ids := make([]int, 0, len(list))
		for _, row := range list {
			ids = append(ids, row.LeaderId)
		}
		names, err := model.GetGameDisplayNames(ids)
		if err != nil {
			return nil, err
		}
		caps := make([]int, 0, len(list))
		for _, row := range list {
			caps = append(caps, wendao.AllianceCap(row.Level))
		}
		view["list"], view["names"], view["caps"] = list, names, caps
		return view, nil
	}
	members, err := model.ListSectAllianceMembers(alliance.Id)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(members))
	for _, row := range members {
		ids = append(ids, row.UserId)
	}
	names, err := model.GetGameDisplayNames(ids)
	if err != nil {
		return nil, err
	}
	// 页面上按本周显示：还没换周的仙盟，周 Boss 视同已经换过(满血、等级按上周是否击杀、轮换新机制)，成员的本周伤害与目标视同清零。
	week := sectAllianceWeek()
	level, hp, maxHp, kind, stageMask := alliance.BossLevel, alliance.BossHp, alliance.BossMaxHp, alliance.BossKind, alliance.BossStageMask
	if alliance.Week != week {
		if alliance.Week != "" && alliance.BossHp <= 0 {
			level++
		}
		hp = wendao.AllianceBossHp(level, alliance.Members)
		maxHp = hp
		kind = wendao.AllianceBossKindForWeek(week)
		stageMask = 0
	} else {
		if kind == "" {
			kind = wendao.AllianceBossKindForWeek(week)
		}
		if stageMask == 0 && maxHp > 0 && hp < maxHp {
			stageMask = wendao.AllianceStageReachedMask(hp, maxHp)
		}
	}
	attackers, otherAttackers := 0, 0
	for i := range members {
		if members[i].Week != week {
			members[i].WeekDamage, members[i].WeekAttacks, members[i].WeekStageMask, members[i].WeekPersonalMask = 0, 0, 0, 0
		}
		if members[i].WeekDamage > 0 {
			attackers++
			if members[i].UserId != userId {
				otherAttackers++
			}
		}
	}
	myAttacks, myStageMask, myPersonalMask := 0, 0, 0
	if member.Week == week {
		myAttacks, myStageMask, myPersonalMask = member.WeekAttacks, member.WeekStageMask, member.WeekPersonalMask
	}
	stages := make([]gin.H, 0, len(wendao.AllianceStageGoals))
	for _, g := range wendao.AllianceStageGoals {
		cost, pills := wendao.AllianceStageReward(level, g.Bit)
		stages = append(stages, gin.H{
			"stage": g.Stage, "damage_pct": g.DamagePct,
			"stone": cost.Stone, "rep": cost.Rep, "pills": pills,
			"reached": stageMask&g.Bit != 0, "earned": myStageMask&g.Bit != 0,
		})
	}
	personal := make([]gin.H, 0, len(wendao.AlliancePersonalGoals))
	for _, g := range wendao.AlliancePersonalGoals {
		cost, pills := wendao.AlliancePersonalReward(level, g.Bit)
		personal = append(personal, gin.H{
			"id": g.Id, "attacks": g.Attacks,
			"stone": cost.Stone, "rep": cost.Rep, "pills": pills,
			"done": myAttacks >= g.Attacks, "earned": myPersonalMask&g.Bit != 0,
		})
	}
	boss := gin.H{
		"level": level, "hp": hp, "max_hp": maxHp, "kills": alliance.BossKills,
		"kind": kind, "stage_mask": stageMask,
		"attackers": attackers, "other_attackers": otherAttackers,
		"my_attacks": myAttacks, "stages": stages, "personal": personal,
	}
	view["alliance"] = alliance
	view["member"] = member
	view["members"] = members
	view["names"] = names
	view["boss"] = boss
	view["cap"] = wendao.AllianceCap(alliance.Level)
	view["upgrade_cost"] = wendao.AllianceUpgradeFunds(alliance.Level)
	return view, nil
}

func getSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	view, err := sectAllianceView(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// respondSectAlliance 在一次仙盟操作之后回应最新的仙盟页，以及(改了宗门时)最新的宗门。
func respondSectAlliance(c *gin.Context, extra gin.H) {
	userId := c.GetInt("id")
	view, err := sectAllianceView(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for key, value := range extra {
		view[key] = value
	}
	data, err := model.GetGameSave(userId, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if s, err := wendao.ParseSect(data); err == nil && s != nil {
		view["sect"] = s.View(common.GetTimestamp())
	}
	common.ApiSuccess(c, view)
}

// cleanAllianceText 去掉首尾空白，不接受控制字符与超过 maxRunes 个字的文字。
func cleanAllianceText(text string, maxRunes int) (string, bool) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxRunes || !utf8.ValidString(text) {
		return "", false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return text, true
}

// createSectAlliance 建一个仙盟：主殿要到 AllianceMinHall 级，花费从宗门里扣。
func createSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	name, ok := cleanAllianceText(req.Name, wendao.AllianceNameMaxRunes)
	if !ok || name == "" {
		common.ApiErrorI18n(c, i18n.MsgGameAllianceName)
		return
	}
	userId := c.GetInt("id")
	if _, member, err := model.GetSectAllianceOf(userId); err != nil || member != nil {
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgGameAllianceJoined)
		return
	}
	now := common.GetTimestamp()
	week := sectAllianceWeek()
	_, err := model.CreateSectAlliance(userId, name, week, sectAllianceBoss, func(data string) (string, model.SectAllianceCarry, error) {
		var carry model.SectAllianceCarry
		updated, err := updateSectSave(data, func(s *wendao.Sect) error {
			s.Tick(now)
			if err := s.FoundAlliance(); err != nil {
				return err
			}
			attackDay, attacks, stageMask, personalMask := s.AllianceBossState(week)
			carry = model.SectAllianceCarry{
				AttackDay: attackDay, Attacks: attacks, StageMask: stageMask, PersonalMask: personalMask,
			}
			return nil
		})
		return updated, carry, err
	})
	if err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

func joinSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Id int `json:"id"`
	}
	if decodeGameRequest(c, &req) != nil || req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	week := sectAllianceWeek()
	if err := model.JoinSectAlliance(userId, req.Id, week, sectAllianceBoss, wendao.AllianceCap, func(data string) (model.SectAllianceCarry, error) {
		return readSectAllianceCarry(data, week)
	}); err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

// leaveSectAlliance 退出仙盟(user_id 为空或是自己)，或者由盟主把一名成员请出仙盟。
func leaveSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		UserId int `json:"user_id"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	target := req.UserId
	if target == 0 {
		target = userId
	}
	if err := model.LeaveSectAlliance(userId, target, syncSectAllianceBossSave); err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

// donateSectAlliance 把宗门的灵石、灵草与玄铁捐给仙盟，折成盟资与自己的贡献。
func donateSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		wendao.Cost
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || wendao.DonationFunds(req.Cost) <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum(req.Cost)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	// 收据与捐献在同一个事务里：网络断了重发同一次捐献时不会再捐一份。
	_, err = model.DonateSectAlliance(c.GetInt("id"), func(data string) (string, int64, error) {
		var funds int64
		updated, err := wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(req.RequestId, sum, now); err != nil {
				return err
			}
			if save.Sect == nil {
				return wendao.ErrUnavailable
			}
			save.Sect.Tick(now)
			var err error
			funds, err = save.Sect.Donate(req.Cost)
			return err
		})
		return updated, funds, err
	})
	if errors.Is(err, wendao.ErrReplayed) {
		respondSectAlliance(c, gin.H{"replayed": true})
		return
	}
	if err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

func upgradeSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	if _, err := model.UpgradeSectAlliance(c.GetInt("id"), wendao.AllianceUpgradeFunds); err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

func setSectAllianceNotice(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Notice string `json:"notice"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	notice, ok := cleanAllianceText(req.Notice, sectAllianceNoticeRunes)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.SetSectAllianceNotice(c.GetInt("id"), notice); err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, nil)
}

// attackSectAlliance 派弟子讨伐本周的 Boss(每人每天一次)，伤害按出战弟子的生命总和叠加本周首领机制算，奖励先记在仙盟里待领。
func attackSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Team      []int  `json:"team"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || len(req.Team) == 0 || len(req.Team) > wendao.TeamSize {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum(struct {
		Action string `json:"action"`
		Team   []int  `json:"team"`
	}{Action: "alliance_attack", Team: req.Team})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	day := gameCenterDay()
	result, err := model.AttackSectAllianceBoss(c.GetInt("id"), day, sectAllianceWeek(), sectAllianceBoss,
		func(data string, ctx model.SectAllianceAttackCtx) (model.SectAllianceAttackPlan, error) {
			var calc wendao.AllianceAttackCalc
			updated, err := wendao.UpdateSave(data, func(save *wendao.Save) error {
				if err := save.Admit(req.RequestId, sum, now); err != nil {
					return err
				}
				if ctx.AlreadyAttacked {
					return model.ErrSectAllianceAttacked
				}
				if save.Sect == nil || !save.Sect.IsFounded() {
					return wendao.ErrUnavailable
				}
				if save.Sect.AllianceBoss != nil && save.Sect.AllianceBoss.AttackDay == day {
					return model.ErrSectAllianceAttacked
				}
				save.Sect.Tick(now)
				var err error
				calc, err = save.Sect.AllianceAttackWithBoss(req.Team, now, wendao.AllianceBossContext{
					Kind:           ctx.BossKind,
					Hp:             ctx.BossHp,
					MaxHp:          ctx.BossMaxHp,
					PriorAttackers: ctx.PriorAttackers,
				})
				return err
			})
			return model.SectAllianceAttackPlan{
				SaveData: updated, BaseDamage: calc.BaseDamage, BonusPct: calc.BonusPct, Damage: calc.Damage,
			}, err
		},
		func(damage int64) (int, int) {
			reward := wendao.AllianceAttackReward(damage)
			return reward.Stone, reward.Rep
		},
		syncSectAllianceBossSave)
	if errors.Is(err, wendao.ErrReplayed) {
		respondSectAlliance(c, gin.H{"replayed": true})
		return
	}
	if err != nil {
		respondAllianceError(c, err)
		return
	}
	respondSectAlliance(c, gin.H{"attack": result})
}

// claimSectAlliance 把讨伐、全盟协作阶段、个人周目标与击杀攒下的奖励领进宗门。
func claimSectAlliance(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		RequestId string `json:"request_id,omitempty"`
	}
	if c.Request.ContentLength > 0 && decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum("alliance_claim")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	var evs []wendao.Ev
	err = model.ClaimSectAllianceRewards(c.GetInt("id"), func(data string, m *model.SectAllianceMember) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(req.RequestId, sum, now); err != nil {
				return err
			}
			if m.PendingStone == 0 && m.PendingRep == 0 && m.PendingPills == 0 {
				return model.ErrSectAllianceNothing
			}
			if save.Sect == nil || !save.Sect.IsFounded() {
				return wendao.ErrUnavailable
			}
			save.Sect.Tick(now)
			evs = save.Sect.ClaimAlliance(wendao.Cost{Stone: m.PendingStone, Rep: m.PendingRep}, m.PendingPills)
			save.Sect.SyncAllianceBoss(m.Week, m.AttackDay, m.WeekAttacks, m.WeekStageMask, m.WeekPersonalMask)
			return nil
		})
	})
	if errors.Is(err, wendao.ErrReplayed) {
		respondSectAlliance(c, gin.H{"replayed": true, "events": []wendao.Ev{}})
		return
	}
	if err != nil {
		respondAllianceError(c, err)
		return
	}
	if evs == nil {
		evs = []wendao.Ev{}
	}
	respondSectAlliance(c, gin.H{"events": evs})
}
