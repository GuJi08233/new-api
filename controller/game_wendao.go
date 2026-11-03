package controller

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 问道是游戏中心里的一款长线修仙游戏，由三个板块组成，共用一份存档(pkg/wendao.Save)：
//   - 历练：主角的修仙路，主线剧情与卡牌战斗，见 game_wendao_hero.go；
//   - 宗门：经营、大比、论剑与仙盟，见 game_sect*.go；
//   - 九幽秘境：即时斗法的肉鸽，见 game_rift.go。
//
// 游戏标识 wendao 的开关管整个问道，它的领奖规则是九幽秘境的(成绩是打通的重数)；宗门大比另有开关与领奖规则(wendao_tournament)。
// 世界设定与发游戏币的规则见 pkg/wendao/world.md。

const wendaoGame = "wendao"

// rollWendaoDaily 把存档里宗门的每日功课换到今天(还没有宗门时什么都不做)。改写问道存档、可能记功课的地方都先调用它。
func rollWendaoDaily(save *wendao.Save) {
	now := time.Now()
	save.RollSaveDaily(model.GameCenterDay(now), model.GameCenterDay(now.AddDate(0, 0, -1)))
}

// wendaoPartGate 是问道里一个发游戏币的玩法(九幽秘境、宗门大比)自己的开关与开局人机验证，页面按它显示开局按钮。
func wendaoPartGate(ctx *gameContext, info gin.H, game string) {
	info["enabled"] = ctx.Setting.GameEnabled(game)
	info["play_turnstile"] = gin.H{
		"required": ctx.Setting.PlayTurnstileRequired(game),
		"site_key": common.TurnstileSiteKey,
	}
}

func init() {
	registerGame(&gameDefinition{Key: wendaoGame, Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			abyss, err := riftInfo(ctx)
			if errors.Is(err, wendao.ErrNewerSave) {
				return nil, errors.New(i18n.T(ctx.Context, i18n.MsgGameWendaoNewerSave))
			}
			if err != nil {
				return nil, err
			}
			sect, err := sectInfo(ctx)
			if err != nil {
				return nil, err
			}
			return gin.H{"abyss": abyss, "sect": sect, "journey": journeyInfo(), "world": duelWorldInfo()}, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/agenda", getWendaoAgenda)
			registerHeroRoutes(group.Group("/hero"))
			registerRiftRoutes(group.Group("/rift"))
			registerDuelRoutes(group.Group("/duel"))
			registerTravelRoutes(group.Group("/travel"))
			registerSectRoutes(group.Group("/sect"))
		},
	})
}

// wendaoRewardTodos 是两个发游戏币的玩法(九幽秘境、宗门大比)今天的奖励还没领满时的待办。玩法关着、不发奖励时不提。
var wendaoRewardTodos = []struct {
	game string
	todo wendao.Todo
}{
	{riftGame, wendao.Todo{Key: "abyss_reward", Part: wendao.PartAbyss}},
	{sectTournamentGame, wendao.Todo{Key: "tournament_reward", Part: wendao.PartSect, Tab: "tournament"}},
}

// getWendaoAgenda 是问道页顶上的当前目标与待办：按现在的存档结算出来给页面看，只读不写，下一次操作时再记进存档。
func getWendaoAgenda(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	ctx := newGameContext(c)
	data, err := model.GetGameSave(ctx.UserId, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		respondSectError(c, err)
		return
	}
	now := common.GetTimestamp()
	rollWendaoDaily(save)
	if save.Sect != nil {
		save.Sect.Tick(now)
	}
	save.TickHero(now)
	agenda := save.Agenda(now, ctx.Day)
	for _, reward := range wendaoRewardTodos {
		rule, _ := ctx.Setting.SkillRuleOf(reward.game)
		if !ctx.Setting.GameEnabled(reward.game) || rule.DailyRewards <= 0 || rule.MaxReward() <= 0 ||
			(reward.todo.Part == wendao.PartSect && save.Sect == nil) {
			continue
		}
		progress, err := model.GetGameRewardProgress(ctx.UserId, ctx.Ip, reward.game, ctx.Day)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		todo := reward.todo
		todo.N = rule.DailyRewards - max(progress.Rewards, progress.IpRewards)
		if todo.N > 0 {
			agenda.Todos = append(agenda.Todos, todo)
		}
	}
	common.ApiSuccess(c, agenda)
}
