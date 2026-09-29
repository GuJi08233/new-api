package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// gameDefinition 描述游戏中心里的一个游戏。每个游戏在自己的文件里用 registerGame 登记，游戏中心列表、游戏页的规则接口
// 和路由都从这里生成，加一个游戏不用改公共代码。
type gameDefinition struct {
	// Key 是游戏标识：路由 /api/game/<key>、配置字段名、对局与计数里的 game 都是它。
	Key  string
	Kind string // operation_setting.GameKind*
	// Info 返回游戏页需要的规则与今天的进度，由 GET /api/game/<key> 返回。
	Info func(ctx *gameContext) (gin.H, error)
	// Summarize 给游戏中心列表里这一项补上游戏自己的进度，可以为空。
	Summarize func(ctx *gameContext, summary gin.H, counts gameDayCounts)
	// Resumable 表示玩家在这个游戏里还有押着游戏币、没结束的一局：游戏关闭后也要让玩家回来把它玩完。可以为空。
	Resumable func(ctx *gameContext) bool
	// Routes 在 /api/game/<key> 下注册这个游戏的接口。开局类接口要用 requireGameEnabled 检查游戏是否开放，
	// 继续已有对局的接口不检查。
	Routes func(group *gin.RouterGroup)
}

// gameDayCounts 是游戏中心列表一次读出的今日计数，键是游戏标识。
type gameDayCounts struct {
	user map[string]model.GameDayCount
	ip   map[string]model.GameDayCount
}

var gameDefinitions []*gameDefinition

func registerGame(def *gameDefinition) {
	gameDefinitions = append(gameDefinitions, def)
}

func findGameDefinition(key string) *gameDefinition {
	for _, def := range gameDefinitions {
		if def.Key == key {
			return def
		}
	}
	return nil
}

// gameContext 是一次游戏请求的公共信息。
type gameContext struct {
	*gin.Context
	UserId  int
	Day     string
	Ip      string // 计数用的 IP 键，IPv6 已归并到前缀
	Setting *operation_setting.GameSetting
}

func newGameContext(c *gin.Context) *gameContext {
	return &gameContext{
		Context: c,
		UserId:  c.GetInt("id"),
		Day:     gameCenterDay(),
		Ip:      gameRewardIpKey(c.ClientIP()),
		Setting: operation_setting.GetGameSetting(),
	}
}

// RegisterGameRoutes 注册所有游戏的接口：每个游戏的规则接口 GET /api/game/<key> 与它自己登记的接口，以及对战房间的公共接口。
// group 已经挂好登录与 IP 封禁的中间件，但不挂游戏中心开关：总开关关闭时所有游戏都算关闭，开局类接口由 requireGameEnabled
// 拒绝，押着游戏币的一局照样能下完、兑现，不然后台按超时结算时玩家只能干看着输掉押注。
func RegisterGameRoutes(group *gin.RouterGroup) {
	for _, def := range gameDefinitions {
		sub := group.Group("/" + def.Key)
		sub.GET("", gameInfoHandler(def))
		if def.Routes != nil {
			def.Routes(sub)
		}
	}
	registerGameRoomRoutes(group.Group("/rooms"))
}

// gameInfoHandler 返回游戏页的规则与进度，附上所有游戏页都要用的余额、游戏币符号和人机验证配置。游戏关闭时只有还押着游戏币、
// 没结束一局的玩家能打开页面。
func gameInfoHandler(def *gameDefinition) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := newGameContext(c)
		enabled := ctx.Setting.GameEnabled(def.Key)
		if !enabled && (def.Resumable == nil || !def.Resumable(ctx)) {
			common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
			return
		}
		info, err := def.Info(ctx)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		account, err := model.GetGameCoinAccount(ctx.UserId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		info["key"] = def.Key
		info["kind"] = def.Kind
		info["enabled"] = enabled
		info["balance"] = account.Balance
		info["symbol"] = ctx.Setting.TokenSymbol
		info["play_turnstile"] = gin.H{
			"required": ctx.Setting.PlayTurnstileRequired(),
			"site_key": common.TurnstileSiteKey,
		}
		common.ApiSuccess(c, info)
	}
}

// GameRequestBodyMaxBytes 是整组游戏接口请求体的总上限，挂在路由组上兜底，取各接口上限里最大的一个(俄罗斯方块一小时对局的
// 录像)。各接口解析时再按自己的请求收紧，普通请求用 decodeGameRequest。
const GameRequestBodyMaxBytes = tetrisFinishMaxBytes

// gameRequestMaxBytes 是开局、下注、落子这类普通请求体的上限，它们只有几个字段。
const gameRequestMaxBytes = 16 << 10

// decodeGameRequest 解析游戏接口的普通 JSON 请求体，最多读 gameRequestMaxBytes 字节。json.Decoder 会先把整个值读进内存，
// 不设上限时一个登录用户发一个几百兆的请求就能耗尽内存。
func decodeGameRequest(c *gin.Context, v any) error {
	return common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, gameRequestMaxBytes), v)
}

// requireGameEnabled 在游戏或整个游戏中心关闭时拒绝开局类请求，写好错误响应并返回 false。
func requireGameEnabled(c *gin.Context, game string) bool {
	if operation_setting.GetGameSetting().GameEnabled(game) {
		return true
	}
	common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
	return false
}

// gameSummaries 返回游戏中心列表里每个开放游戏(以及玩家还有一局没结束的关闭游戏)的今日进度，以及读出的今日计数。
// 所有游戏的计数一次读出，列表不会随游戏变多而多查几十次库。
func gameSummaries(ctx *gameContext) ([]gin.H, gameDayCounts, error) {
	var counts gameDayCounts
	var err error
	if counts.user, err = model.GetUserGameDayCounts(ctx.UserId, ctx.Day); err != nil {
		return nil, counts, err
	}
	if counts.ip, err = model.GetIpGameDayCounts(ctx.Ip, ctx.Day); err != nil {
		return nil, counts, err
	}
	summaries := make([]gin.H, 0, len(gameDefinitions))
	for _, def := range gameDefinitions {
		// 关闭的游戏只给还押着游戏币、没结束一局的玩家列出来，让他回去把那一局玩完。
		enabled := ctx.Setting.GameEnabled(def.Key)
		if !enabled && (def.Resumable == nil || !def.Resumable(ctx)) {
			continue
		}
		user := counts.user[def.Key]
		summary := gin.H{
			"key":         def.Key,
			"kind":        def.Kind,
			"enabled":     enabled,
			"plays_today": user.Plays,
			"net_today":   user.Coins,
		}
		if rule, ok := ctx.Setting.SkillRuleOf(def.Key); ok {
			summary["daily_rewards"] = rule.DailyRewards
			summary["rewards_today"] = max(user.FreePlays, counts.ip[def.Key].FreePlays)
			summary["max_reward"] = rule.MaxReward()
		}
		if def.Summarize != nil {
			def.Summarize(ctx, summary, counts)
		}
		summaries = append(summaries, summary)
	}
	return summaries, counts, nil
}
