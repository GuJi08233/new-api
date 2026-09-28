package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/pkg/breakout"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const gameBreakout = "breakout"

func init() {
	registerGame(&gameDefinition{Key: gameBreakout, Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) { return skillInfo(ctx, gameBreakout, ctx.Setting.Breakout) },
		Routes: func(group *gin.RouterGroup) {
			group.POST("/start", middleware.GamePlayRateLimit("breakout-start"), StartBreakout)
			group.POST("/finish", middleware.GamePlayRateLimit("breakout-finish"), FinishBreakout)
		},
	})
}

func StartBreakout(c *gin.Context) {
	if !requireGameEnabled(c, gameBreakout) {
		return
	}
	session := startSkillSession(c, gameBreakout, nil)
	if session != nil {
		common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
	}
}

func FinishBreakout(c *gin.Context) {
	if !requireGameEnabled(c, gameBreakout) {
		return
	}
	var req struct {
		SessionId int   `json:"session_id"`
		Inputs    []int `json:"inputs"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10), &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	rule := operation_setting.GetGameSetting().Breakout
	session := loadSkillSession(c, gameBreakout, rule, req.SessionId)
	if session == nil {
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(len(req.Inputs)) > (elapsed+2)*breakout.FPS {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	game, err := breakout.Replay(uint32(session.Seed), req.Inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	result := settleSkillSession(c, "打砖块", rule, session, skillOutcome{Score: game.Score, Seconds: game.Ticks / breakout.FPS, Detail: gin.H{"bricks": game.Broken, "ticks": game.Ticks, "lives": game.Lives, "won": game.Won}})
	if result != nil {
		common.ApiSuccess(c, result)
	}
}
