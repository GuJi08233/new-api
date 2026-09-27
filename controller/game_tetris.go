package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/pkg/tetris"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameTetris = "tetris"

const (
	// tetrisClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	tetrisClockSlackSeconds = 2
	// tetrisFinishMaxBytes 限制结算请求体。一小时的操作记录在 tetris.Replay 允许的操作数以内，最多约 1.7 MB。
	tetrisFinishMaxBytes = 4 << 20
)

func init() {
	registerGame(&gameDefinition{
		Key:  gameTetris,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			return skillInfo(ctx, gameTetris, ctx.Setting.Tetris)
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/start", middleware.GamePlayRateLimit("tetris-start"), StartTetris)
			group.POST("/finish", middleware.GamePlayRateLimit("tetris-finish"), FinishTetris)
		},
	})
}

type tetrisFinishRequest struct {
	SessionId int   `json:"session_id"`
	Ticks     int   `json:"ticks"`
	Inputs    []int `json:"inputs"`
}

// tetrisDetail 是俄罗斯方块对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type tetrisDetail struct {
	Lines      int  `json:"lines"`
	Level      int  `json:"level"`
	Pieces     int  `json:"pieces"`
	Ticks      int  `json:"ticks"`
	Inputs     int  `json:"inputs"`
	Over       bool `json:"over"`
	PeakPieces int  `json:"peak_pieces"`
}

// StartTetris 开一局俄罗斯方块：记下对局，把随机种子交给浏览器，整局的方块序列只由种子决定。
// 要求人机验证时每局都要带一个新的 Turnstile 令牌，不经浏览器直接调接口的脚本开不了局。
func StartTetris(c *gin.Context) {
	if !requireGameEnabled(c, gameTetris) {
		return
	}
	session := startSkillSession(c, gameTetris, nil)
	if session == nil {
		return
	}
	common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
}

// FinishTetris 结算一局俄罗斯方块。浏览器上传整局操作，服务端从同一个种子重放，成绩与奖励都以重放结果为准。
// 浏览器按真实时间推进帧，所以游戏内时长不能超过开局以来实际经过的时间。同一局只结算一次，重复提交
// (例如结算成功但响应在网络上丢了)时返回第一次结算的结果。
func FinishTetris(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Tetris
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req tetrisFinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, tetrisFinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session := loadSkillSession(c, gameTetris, rule, req.SessionId)
	if session == nil {
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(req.Ticks) > (elapsed+tetrisClockSlackSeconds)*tetris.TicksPerSecond {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	inputs, err := tetris.DecodeInputs(req.Inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	game, err := tetris.Replay(uint32(session.Seed), req.Ticks, inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	// 操作快过人类极限的对局判定为脚本，不发奖励。
	cheat := ""
	if game.PeakPieces > tetris.HumanPieceLimit {
		cheat = fmt.Sprintf("俄罗斯方块对局 #%d 在 %d 秒内锁定了 %d 块，超过人类操作极限 %d 块，判定为脚本，未发奖励",
			session.Id, tetris.SpeedWindowTicks/tetris.TicksPerSecond, game.PeakPieces, tetris.HumanPieceLimit)
	}
	result := settleSkillSession(c, "俄罗斯方块", rule, session, skillOutcome{
		Score:   game.Score,
		Seconds: game.Ticks / tetris.TicksPerSecond,
		Detail: tetrisDetail{
			Lines:      game.Lines,
			Level:      game.Level,
			Pieces:     game.Pieces,
			Ticks:      game.Ticks,
			Inputs:     len(inputs),
			Over:       game.Over,
			PeakPieces: game.PeakPieces,
		},
		Cheat: cheat,
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}
