package controller

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/tetris"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameTetris = "tetris"

const (
	// tetrisSessionTTL 是一局从开局到结算的最长时间(秒)：游戏内最多一小时，另留一小时给暂停。
	tetrisSessionTTL = 2 * 60 * 60
	// tetrisClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	tetrisClockSlackSeconds = 2
	// tetrisFinishMaxBytes 限制结算请求体。一小时的操作记录在 tetris.Replay 允许的操作数以内，最多约 1.7 MB。
	tetrisFinishMaxBytes = 4 << 20
)

type tetrisFinishRequest struct {
	SessionId int   `json:"session_id"`
	Ticks     int   `json:"ticks"`
	Inputs    []int `json:"inputs"`
}

// StartTetris 开一局俄罗斯方块：记下对局，把随机种子交给浏览器，整局的方块序列只由种子决定。
func StartTetris(c *gin.Context) {
	if !operation_setting.GetGameSetting().Tetris.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	userId := c.GetInt("id")
	var seed [4]byte
	if _, err := rand.Read(seed[:]); err != nil {
		common.SysError(fmt.Sprintf("failed to generate tetris seed for user %d: %v", userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameStartFailed)
		return
	}
	session, err := model.CreateGameSession(userId, gameTetris, binary.BigEndian.Uint32(seed[:]))
	if err != nil {
		common.SysError(fmt.Sprintf("failed to start tetris for user %d: %v", userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameStartFailed)
		return
	}
	common.ApiSuccess(c, gin.H{"session_id": session.Id, "seed": uint32(session.Seed)})
}

// FinishTetris 结算一局俄罗斯方块。浏览器上传整局操作，服务端从同一个种子重放，成绩与奖励都以重放结果为准。
// 浏览器按真实时间推进帧，所以游戏内时长不能超过开局以来实际经过的时间；同一局只能结算一次。
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
	userId := c.GetInt("id")
	session, err := model.GetUserGameSession(userId, gameTetris, req.SessionId)
	if errors.Is(err, model.ErrGameSessionNotFound) {
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session.Status != model.GameSessionStatusPlaying {
		common.ApiErrorI18n(c, i18n.MsgGameSessionFinished)
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if elapsed > tetrisSessionTTL {
		common.ApiErrorI18n(c, i18n.MsgGameSessionExpired)
		return
	}
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
	// 最高分在结算之前读：结算一旦提交就不能再因为别的查询失败而报错，否则浏览器重试只会得到「已结算」。
	best, err := model.GetGameBestScore(userId, gameTetris)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	detail, err := common.Marshal(gin.H{
		"lines":  game.Lines,
		"level":  game.Level,
		"pieces": game.Pieces,
		"ticks":  game.Ticks,
		"inputs": len(inputs),
		"over":   game.Over,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 保存配置时已要求分数为正；这里再挡一次，手工改库留下的 0 只会让这局没有奖励，不会除零。
	reward := 0
	if rule.ScorePerCoin > 0 {
		reward = game.Score / rule.ScorePerCoin
	}
	granted, earned, balance, err := model.FinishGameSession(session, game.Score, string(detail), reward, rule.DailyRewardLimit, gameCenterDay())
	if errors.Is(err, model.ErrGameSessionFinished) {
		common.ApiErrorI18n(c, i18n.MsgGameSessionFinished)
		return
	}
	if err != nil {
		common.SysError(fmt.Sprintf("failed to settle tetris session %d for user %d: %v", session.Id, userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameFinishFailed)
		return
	}
	common.ApiSuccess(c, gin.H{
		"score":              game.Score,
		"lines":              game.Lines,
		"level":              game.Level,
		"pieces":             game.Pieces,
		"ticks":              game.Ticks,
		"reward":             granted,
		"earned_today":       earned,
		"daily_reward_limit": rule.DailyRewardLimit,
		"balance":            balance,
		"best_score":         max(best, game.Score),
	})
}
