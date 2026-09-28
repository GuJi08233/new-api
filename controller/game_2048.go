package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/pkg/g2048"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const game2048 = "2048"

const (
	// g2048ClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	g2048ClockSlackSeconds = 2
	// g2048FinishMaxBytes 限制结算请求体：移动记录最多 g2048.MaxMoves 个字符，其余字段不到 100 字节。
	g2048FinishMaxBytes = 256 << 10
	// g2048SecondsPerMove 是每一步有效移动最多算作多少秒游戏时长：浏览器只在最后交一次，开局之后挂着不动不算在玩。
	g2048SecondsPerMove = 5
)

func init() {
	registerGame(&gameDefinition{
		Key:  game2048,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			return skillInfo(ctx, game2048, ctx.Setting.Game2048)
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/start", middleware.GamePlayRateLimit("2048-start"), Start2048)
			group.POST("/finish", middleware.GamePlayRateLimit("2048-finish"), Finish2048)
		},
	})
}

type g2048FinishRequest struct {
	SessionId int    `json:"session_id"`
	Moves     string `json:"moves"`
}

// g2048Detail 是 2048 对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type g2048Detail struct {
	MaxTile int  `json:"max_tile"`
	Moves   int  `json:"moves"` // 让盘面发生变化的移动数
	Over    bool `json:"over"`
}

// Start2048 开一局 2048：记下对局，把随机种子交给浏览器，整局生成的方块只由种子和玩家的移动决定。
// 要求人机验证时每局都要带一个新的 Turnstile 令牌。
func Start2048(c *gin.Context) {
	if !requireGameEnabled(c, game2048) {
		return
	}
	session := startSkillSession(c, game2048, nil)
	if session == nil {
		return
	}
	common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
}

// Finish2048 结算一局 2048。浏览器上传整局移动，服务端从同一个种子重放，成绩与奖励都以重放结果为准。2048 没有游戏内时钟，
// 游戏时长按开局以来的真实时间计算，有效移动数也不能快过人手(每秒 g2048.MaxMovesPerSecond 次)。同一局只结算一次，
// 重复提交时返回第一次结算的结果。
func Finish2048(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Game2048
	if !requireGameEnabled(c, game2048) {
		return
	}
	var req g2048FinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, g2048FinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session := loadSkillSession(c, game2048, rule, req.SessionId)
	if session == nil {
		return
	}
	game, err := g2048.Replay(uint32(session.Seed), req.Moves)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(game.Moves) > (elapsed+g2048ClockSlackSeconds)*g2048.MaxMovesPerSecond {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	result := settleSkillSession(c, "2048", rule, session, skillOutcome{
		Score:   game.Score,
		Seconds: int(min(elapsed, int64(game.Moves)*g2048SecondsPerMove)),
		Detail:  g2048Detail{MaxTile: game.MaxTile(), Moves: game.Moves, Over: game.Over},
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}
