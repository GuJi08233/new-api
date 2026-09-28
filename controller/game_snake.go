package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/pkg/snake"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameSnake = "snake"

const (
	// snakeClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	snakeClockSlackSeconds = 2
	// snakeFinishMaxBytes 限制结算请求体。每一步最多转一次向、各次间隔之和不超过 snake.MaxSteps，转向记录最多约 200 KB。
	snakeFinishMaxBytes = 1 << 20
)

func init() {
	registerGame(&gameDefinition{
		Key:  gameSnake,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			return skillInfo(ctx, gameSnake, ctx.Setting.Snake)
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/start", middleware.GamePlayRateLimit("snake-start"), StartSnake)
			group.POST("/finish", middleware.GamePlayRateLimit("snake-finish"), FinishSnake)
		},
	})
}

type snakeFinishRequest struct {
	SessionId int   `json:"session_id"`
	Steps     int   `json:"steps"`
	Inputs    []int `json:"inputs"`
}

// snakeDetail 是贪吃蛇对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type snakeDetail struct {
	Length int  `json:"length"`
	Steps  int  `json:"steps"`
	Over   bool `json:"over"`
}

// StartSnake 开一局贪吃蛇：记下对局，把随机种子交给浏览器，整局食物出现的位置只由种子和蛇的走法决定。
// 要求人机验证时每局都要带一个新的 Turnstile 令牌。
func StartSnake(c *gin.Context) {
	if !requireGameEnabled(c, gameSnake) {
		return
	}
	session := startSkillSession(c, gameSnake, nil)
	if session == nil {
		return
	}
	common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
}

// FinishSnake 结算一局贪吃蛇。浏览器上传推进过的步数和每次生效的转向，服务端从同一个种子重放，成绩、游戏内时长与奖励
// 都以重放结果为准。浏览器按每一步的步长推进，所以游戏内时长(所有步长之和)不能超过开局以来实际经过的时间。
// 同一局只结算一次，重复提交时返回第一次结算的结果。
func FinishSnake(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Snake
	if !requireGameEnabled(c, gameSnake) {
		return
	}
	var req snakeFinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, snakeFinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session := loadSkillSession(c, gameSnake, rule, req.SessionId)
	if session == nil {
		return
	}
	inputs, err := snake.DecodeInputs(req.Inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	game, err := snake.Replay(uint32(session.Seed), req.Steps, inputs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(game.ElapsedMs) > (elapsed+snakeClockSlackSeconds)*1000 {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	result := settleSkillSession(c, "贪吃蛇", rule, session, skillOutcome{
		Score:   game.Score,
		Seconds: int(min(int64(game.ElapsedMs/1000), elapsed)),
		Detail:  snakeDetail{Length: game.Length(), Steps: game.Steps, Over: game.Over},
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}
