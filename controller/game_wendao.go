package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 问道是游戏中心里的一款长线修仙游戏，由三个板块组成，共用一份存档(pkg/wendao.Save)：
//   - 历练：主角的修仙路，主线剧情与卡牌战斗，见 game_wendao_hero.go；
//   - 宗门：经营、大比、论剑与仙盟，见 game_sect*.go；
//   - 九幽秘境：卡牌肉鸽，见 game_dungeon.go。
//
// 游戏标识 wendao 的开关管整个问道，它的领奖规则是秘境无尽模式的；宗门大比另有开关与领奖规则(wendao_tournament)。
// 世界设定与发游戏币的规则见 pkg/wendao/world.md。

const wendaoGame = "wendao"

// rollWendaoDaily 把存档里宗门的每日功课换到今天(还没有宗门时什么都不做)。改写问道存档、可能记功课的地方都先调用它。
func rollWendaoDaily(save *wendao.Save) {
	now := time.Now()
	save.RollSaveDaily(model.GameCenterDay(now), model.GameCenterDay(now.AddDate(0, 0, -1)))
}

// wendaoPartGate 是问道里一个发游戏币的玩法(秘境无尽、宗门大比)自己的开关与开局人机验证，页面按它显示开局按钮。
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
			abyss, err := abyssInfo(ctx)
			if err != nil {
				return nil, err
			}
			sect, err := sectInfo(ctx)
			if err != nil {
				return nil, err
			}
			return gin.H{"abyss": abyss, "sect": sect, "journey": journeyInfo()}, nil
		},
		Routes: func(group *gin.RouterGroup) {
			registerHeroRoutes(group.Group("/hero"))
			registerAbyssRoutes(group.Group("/abyss"))
			registerSectRoutes(group.Group("/sect"))
		},
	})
}
