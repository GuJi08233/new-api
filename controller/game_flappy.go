package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/pkg/flappy"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameFlappy = "flappy"

const (
	// flappyClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	flappyClockSlackSeconds = 2
	// flappyFinishMaxBytes 限制结算请求体。一帧最多拍一次、各次间隔之和不超过 flappy.MaxTicks，拍翅膀记录最多约 430 KB。
	flappyFinishMaxBytes = 1 << 20
)

func init() {
	registerGame(&gameDefinition{
		Key:  gameFlappy,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			return skillInfo(ctx, gameFlappy, ctx.Setting.Flappy)
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/start", middleware.GamePlayRateLimit("flappy-start"), StartFlappy)
			group.POST("/finish", middleware.GamePlayRateLimit("flappy-finish"), FinishFlappy)
		},
	})
}

type flappyFinishRequest struct {
	SessionId int   `json:"session_id"`
	Ticks     int   `json:"ticks"`
	Flaps     []int `json:"flaps"`
}

// flappyDetail 是像素鸟对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
// PeakFlaps 是任意 1 秒内拍翅膀的最多次数，判定为脚本时是证据。
type flappyDetail struct {
	Ticks     int  `json:"ticks"`
	Flaps     int  `json:"flaps"`
	Over      bool `json:"over"`
	PeakFlaps int  `json:"peak_flaps"`
}

// StartFlappy 开一局像素鸟：记下对局，把随机种子交给浏览器，整局管道的缺口位置只由种子决定。
// 要求人机验证时每局都要带一个新的 Turnstile 令牌。
func StartFlappy(c *gin.Context) {
	if !requireGameEnabled(c, gameFlappy) {
		return
	}
	session := startSkillSession(c, gameFlappy, nil)
	if session == nil {
		return
	}
	common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
}

// FinishFlappy 结算一局像素鸟。浏览器上传推进过的帧数和每次拍翅膀的帧号，服务端从同一个种子重放，成绩与奖励都以
// 重放结果为准。浏览器按真实时间推进帧，所以游戏内时长不能超过开局以来实际经过的时间；拍翅膀快过人类极限的对局判定为
// 脚本，不发奖励。同一局只结算一次，重复提交时返回第一次结算的结果。
func FinishFlappy(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Flappy
	if !requireGameEnabled(c, gameFlappy) {
		return
	}
	var req flappyFinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, flappyFinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session := loadSkillSession(c, gameFlappy, rule, req.SessionId)
	if session == nil {
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(req.Ticks) > (elapsed+flappyClockSlackSeconds)*flappy.TicksPerSecond {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	flaps, err := flappy.DecodeFlaps(req.Flaps)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	game, err := flappy.Replay(uint32(session.Seed), req.Ticks, flaps)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	cheat := ""
	if game.PeakFlaps > flappy.HumanFlapLimit {
		cheat = fmt.Sprintf("像素鸟对局 #%d 在 1 秒内拍了 %d 次翅膀，超过人类操作极限 %d 次，判定为脚本，未发奖励，重放成绩 %d 不计入",
			session.Id, game.PeakFlaps, flappy.HumanFlapLimit, game.Score)
	}
	result := settleSkillSession(c, "像素鸟", rule, session, skillOutcome{
		Score:   game.Score,
		Seconds: int(min(int64(game.Ticks/flappy.TicksPerSecond), elapsed)),
		Detail: flappyDetail{
			Ticks:     game.Ticks,
			Flaps:     game.Flaps,
			Over:      game.Over,
			PeakFlaps: game.PeakFlaps,
		},
		Cheat: cheat,
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}
