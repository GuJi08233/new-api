package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 历练是问道的主线板块：主角存在问道存档里(wendao.Save.Hero)，所有操作都锁住存档改写，战斗的每一步也一样。历练不发
// 游戏币，奖励是修为、功法、法宝、丹符与宗门的资源。

// journeyInfo 是问道页里历练板块的规则：章节节点(带上敌人强度)、问心阶的每一级、突破要的机缘、参悟价格与各种上限。
func journeyInfo() gin.H {
	nodes := make([]gin.H, 0, len(wendao.JourneyNodes))
	for i := range wendao.JourneyNodes {
		n := &wendao.JourneyNodes[i]
		view := gin.H{"id": n.Id, "chapter": n.Chapter, "kind": n.Kind, "requires": n.Requires, "enemies": n.Enemies,
			"exp": n.Exp, "first": n.First, "repeat": n.Repeat, "relic": n.Relic, "potions": n.Potions, "key": n.Key,
			"cooldown": n.Cooldown}
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
		"max_cards":            wendao.MaxCards,
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
	}
}

// registerHeroRoutes 在 /api/game/wendao/hero 下注册历练板块的接口。
func registerHeroRoutes(group *gin.RouterGroup) {
	group.GET("", getHero)
	group.POST("/action", middleware.GameActionRateLimit("wendao-hero-action"), actHero)
}

// heroView 是历练页要的东西：主角(还没有创建为空)、宗门的资源、丹药与灵药。ended 是这一步刚打完的那一场，页面要播放最后
// 几下与胜负。
func heroView(save *wendao.Save, now int64, day string, ended *dungeon.Run) gin.H {
	view := gin.H{"hero": nil, "res": nil, "pills": nil, "rare": nil}
	if save.Sect != nil {
		view["res"], view["pills"], view["rare"] = save.Sect.Res, save.Sect.Pills, save.Sect.Rare
	}
	if save.Hero != nil && save.Sect != nil {
		view["hero"] = save.Hero.View(save.Sect, now, day)
	}
	if ended != nil && (save.Hero == nil || save.Hero.Battle == nil || save.Hero.Battle.Run != ended) {
		view["ended"] = ended.View()
	}
	return view
}

// getHero 读出主角，只读不写：闭关修为按现在算给页面看，下一次操作时再记进存档。
func getHero(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	data, err := model.GetGameSave(c.GetInt("id"), wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	save.TickHero(now)
	common.ApiSuccess(c, heroView(save, now, gameCenterDay(), nil))
}

// actHero 执行一次历练操作：锁住存档，先结算闭关修为，再执行操作。创建主角时还没有宗门就先开宗立派。
func actHero(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req wendao.HeroAction
	if decodeGameRequest(c, &req) != nil || req.Type == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var seed uint64
	if req.Type == "fight" || req.Type == "climb" {
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
	_, err := model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if req.Type == "create" {
				if err := foundSect(save, now); err != nil {
					return err
				}
			}
			rollWendaoDaily(save)
			var running *dungeon.Run
			if save.Hero != nil && save.Hero.Battle != nil {
				running = save.Hero.Battle.Run
			}
			var err error
			if evs, err = save.HeroApply(req, now, day, seed); err != nil {
				return err
			}
			view = heroView(save, now, day, running)
			return nil
		})
	})
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
