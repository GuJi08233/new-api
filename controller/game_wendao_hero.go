package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 历练是问道的主线板块：主角存在问道存档里(wendao.Save.Hero)，所有操作都锁住存档改写，战斗的每一步也一样。历练不发
// 游戏币。斗法在浏览器里运行，提交后服务端重放，再锁住存档结算修为、法宝、丹符与资源。

// journeyInfo 是问道页里历练板块的规则：章节节点(带上敌人强度)、问心阶的每一级、突破要的机缘、参悟价格与各种上限。
func journeyInfo() gin.H {
	nodes := make([]gin.H, 0, len(wendao.JourneyNodes))
	for i := range wendao.JourneyNodes {
		n := &wendao.JourneyNodes[i]
		view := gin.H{"id": n.Id, "chapter": n.Chapter, "kind": n.Kind, "requires": n.Requires, "enemies": n.Enemies,
			"exp": n.Exp, "first": n.First, "repeat": n.Repeat, "relic": n.Relic, "potions": n.Potions, "key": n.Key,
			"cooldown": n.Cooldown, "side": n.Side, "options": n.Options}
		if len(n.Enemies) > 0 {
			hp, dmg := wendao.EnemyScale(n)
			view["scale"] = []int{hp, dmg}
		}
		nodes = append(nodes, view)
	}
	return gin.H{
		"nodes":                nodes,
		"chapters":             wendao.Chapters,
		"break_keys":           wendao.BreakKeys,
		"upgrade_cost":         wendao.UpgradeCost,
		"deck_min":             wendao.DeckMin,
		"deck_max":             wendao.DeckMax,
		"max_presets":          wendao.MaxPresets,
		"max_preset_name":      wendao.MaxPresetName,
		"max_cards":            wendao.MaxCards,
		"salvage_daily":        wendao.SalvageDaily,
		"daily_repeat_rewards": wendao.DailyRepeatRewards,
		"classes":              dungeon.Classes,
		"potion_recipes":       wendao.PotionRecipes,
		"max_potion_stack":     wendao.MaxPotionStack,
		"companion_max":        wendao.CompanionMax,
		"steps":                wendao.Steps(),
		"steps_unlock":         wendao.StepsUnlock,
		"cave":                 wendao.CaveRulesOf(),
		"refine":               wendao.RefineRulesOf(),
		"elixir":               wendao.ElixirRulesOf(),
		"beast":                wendao.BeastRulesOf(),
		"weekly":               wendao.WeeklyRulesOf(),
	}
}

// registerHeroRoutes 在 /api/game/wendao/hero 下注册历练板块的接口。
func registerHeroRoutes(group *gin.RouterGroup) {
	group.GET("", getHero)
	group.POST("/action", middleware.GameActionRateLimit("wendao-hero-action"), actHero)
	group.POST("/clear", middleware.GameActionRateLimit("wendao-hero-clear"), clearHeroFight)
}

// heroView 只给主角与储物处的概况，斗法的种子和开局只在开战的回应里发出。
func heroView(save *wendao.Save, now int64, day string) gin.H {
	view := gin.H{"hero": nil, "res": nil, "pills": nil, "rare": nil}
	if save.Sect != nil {
		view["res"], view["pills"], view["rare"] = save.Sect.Res, save.Sect.Pills, save.Sect.Rare
	}
	if save.Hero != nil && save.Sect != nil {
		view["hero"] = save.Hero.View(save.Sect, now, day)
	}
	return view
}

// getHero 读出主角，只读不写：闭关修为按现在算给页面看，下一次操作时再记进存档。
func getHero(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	now := common.GetTimestamp()
	save.TickHero(now)
	common.ApiSuccess(c, heroView(save, now, gameCenterDay()))
}

// actHero 执行一次历练操作：锁住存档，先结算闭关修为，再执行操作。创建主角时先创建散修储物处。
func actHero(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req wendao.HeroAction
	if decodeGameRequest(c, &req) != nil || req.Type == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	requestId := req.RequestId
	req.RequestId = ""
	sum, err := wendao.RequestSum(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var seed uint64
	if req.Type == "fight" || req.Type == "climb" || req.Type == "trial" {
		var err error
		if seed, err = newSectSeed(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	now := common.GetTimestamp()
	day := gameCenterDay()
	var evs []wendao.Ev
	var view gin.H
	_, err = model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(requestId, sum, now); err != nil {
				return err
			}
			if req.Type == "create" {
				if err := ensureStorehouse(save, now); err != nil {
					return err
				}
			}
			rollWendaoDaily(save)
			fled := req.Type == "flee" && save.Hero != nil && save.Hero.Battle != nil
			var err error
			if evs, err = save.HeroApply(req, now, day, seed); err != nil {
				return err
			}
			view = heroView(save, now, day)
			if req.Type == "fight" || req.Type == "climb" || req.Type == "trial" {
				save.Hero.Battle.RequestId = requestId
				active := save.Hero.Battle.Fight
				view["fight"] = gin.H{"seed": active.Seed, "setup": active.Setup, "issued": active.Issued, "token": save.Hero.Battle.Token}
			}
			if fled {
				view["ended"] = gin.H{"won": false, "fled": true}
			}
			return nil
		})
	})
	if errors.Is(err, wendao.ErrReplayed) {
		// 这一步上次已经做完了，只是页面没收到回应：交回现在的样子，不再做一遍。
		save, err := loadWendaoSave(c.GetInt("id"))
		if err != nil {
			respondSectError(c, err)
			return
		}
		save.TickHero(now)
		view = heroView(save, now, day)
		if save.Hero != nil && save.Hero.Battle != nil && save.Hero.Battle.RequestId == requestId &&
			(req.Type == "fight" || req.Type == "climb" || req.Type == "trial") {
			active := save.Hero.Battle.Fight
			view["fight"] = gin.H{"seed": active.Seed, "setup": active.Setup, "issued": active.Issued, "token": save.Hero.Battle.Token}
		}
		view["events"], view["replayed"] = []wendao.Ev{}, true
		common.ApiSuccess(c, view)
		return
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	if evs == nil {
		evs = []wendao.Ev{}
	}
	view["events"] = evs
	common.ApiSuccess(c, view)
}

// clearHeroFight 核对操作记录，只结算发给这个页面的那一场。重放在数据库事务之外执行，锁内再次核对场次，
// 防止另一个页面已经退走、结算或重开；请求收据与奖励在同一次存档更新里落下，断网重交只结算一次。
func clearHeroFight(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Inputs    string  `json:"inputs"`
		Seed      *uint32 `json:"seed"`
		Issued    int64   `json:"issued"`
		Token     string  `json:"battle_token"`
		RequestId string  `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Seed == nil || req.Issued <= 0 || req.Token == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	requestId := req.RequestId
	req.RequestId = ""
	sum, err := wendao.RequestSum(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userId, now, day := c.GetInt("id"), common.GetTimestamp(), gameCenterDay()
	save, err := loadWendaoSave(userId)
	if err != nil {
		respondSectError(c, err)
		return
	}
	// 在这份只读副本上先认收据：上次已经结算的请求即便新开了一场，也不拿旧记录去重放新的一场。
	if err := save.Admit(requestId, sum, now); err != nil {
		if !errors.Is(err, wendao.ErrReplayed) {
			respondSectError(c, err)
			return
		}
		save.TickHero(now)
		view := heroView(save, now, day)
		view["events"], view["replayed"] = []wendao.Ev{}, true
		common.ApiSuccess(c, view)
		return
	}
	if save.Hero == nil || save.Hero.Battle == nil || save.Hero.Battle.Fight == nil {
		respondSectError(c, wendao.ErrUnavailable)
		return
	}
	active := save.Hero.Battle.Fight
	token := save.Hero.Battle.Token
	if active.Seed != *req.Seed || active.Issued != req.Issued || token != req.Token {
		respondSectError(c, wendao.ErrBattleMoved)
		return
	}
	inputs, err := doufa.DecodeInputs(req.Inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	fight, err := doufa.Replay(active.Seed, active.Setup, inputs)
	if err != nil || !fight.Over || (now-active.Issued+riftGraceSeconds)*5*doufa.FPS < int64(fight.Tick)*4 {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	var evs []wendao.Ev
	_, err = model.UpdateGameSave(userId, wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(sv *wendao.Save) error {
			if err := sv.Admit(requestId, sum, now); err != nil {
				return err
			}
			if sv.Hero == nil || sv.Sect == nil || sv.Hero.Battle == nil || sv.Hero.Battle.Fight == nil {
				return wendao.ErrUnavailable
			}
			current := sv.Hero.Battle.Fight
			if current.Seed != active.Seed || current.Issued != active.Issued || sv.Hero.Battle.Token != token {
				return wendao.ErrBattleMoved
			}
			rollWendaoDaily(sv)
			var err error
			evs, err = sv.FinishHeroFight(fight, now)
			return err
		})
	})
	replayed := errors.Is(err, wendao.ErrReplayed)
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	save, err = loadWendaoSave(userId)
	if err != nil {
		respondSectError(c, err)
		return
	}
	view := heroView(save, now, day)
	if replayed {
		view["events"], view["replayed"] = []wendao.Ev{}, true
	} else {
		view["events"] = evs
		view["ended"] = gin.H{"won": fight.Won, "kills": fight.Kills, "ticks": fight.Tick,
			"hp": fight.P.HP, "max_hp": fight.P.MaxHP, "damage_taken": fight.Taken, "damage_dealt": fight.Dealt}
	}
	common.ApiSuccess(c, view)
}
