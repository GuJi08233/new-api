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
	"github.com/QuantumNous/new-api/service"
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
	setting := operation_setting.GetGameSetting()
	if !setting.Tetris.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	userId := c.GetInt("id")
	if setting.PlayTurnstileRequired() {
		token := c.Query("turnstile")
		if token == "" {
			common.ApiErrorI18n(c, i18n.MsgGameTurnstileRequired)
			return
		}
		if err := service.VerifyTurnstile(token, c.ClientIP()); err != nil {
			if !errors.Is(err, service.ErrTurnstileRejected) {
				common.SysError(fmt.Sprintf("failed to verify turnstile for user %d: %v", userId, err))
			}
			common.ApiErrorI18n(c, i18n.MsgGameTurnstileFailed)
			return
		}
	}
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
		respondSettledTetris(c, session, rule)
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
	// 最高分在结算之前读：结算一旦提交就不能再因为别的查询失败而报错，浏览器会把失败当成这局被拒绝。
	best, err := model.GetGameBestScore(userId, gameTetris)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 奖励按单局分数计算，再受两条防脚本的规则约束：每个游戏币至少要玩够 MinSecondsPerCoin 秒；
	// 操作快过人类极限的对局判定为脚本，不发奖励。保存配置时已要求分数为正，这里再挡一次，手工改库留下的 0 不会除零。
	reward, limitedBy := 0, ""
	if rule.ScorePerCoin > 0 {
		reward = game.Score / rule.ScorePerCoin
	}
	if rule.MinSecondsPerCoin > 0 {
		if byTime := game.Ticks / (rule.MinSecondsPerCoin * tetris.TicksPerSecond); byTime < reward {
			reward, limitedBy = byTime, model.GameRewardLimitedByTime
		}
	}
	tooFast := game.PeakPieces > tetris.HumanPieceLimit
	if tooFast {
		reward, limitedBy = 0, model.GameRewardLimitedBySpeed
	}
	detail := tetrisDetail{
		Lines:      game.Lines,
		Level:      game.Level,
		Pieces:     game.Pieces,
		Ticks:      game.Ticks,
		Inputs:     len(inputs),
		Over:       game.Over,
		PeakPieces: game.PeakPieces,
	}
	detailJson, err := common.Marshal(detail)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ip := c.ClientIP()
	settled, earned, balance, err := model.FinishGameSession(session, model.GameSettlement{
		Score:      game.Score,
		Detail:     string(detailJson),
		Reward:     reward,
		LimitedBy:  limitedBy,
		DailyLimit: rule.DailyRewardLimit,
		Day:        gameCenterDay(),
		Ip:         gameRewardIpKey(ip),
	})
	switch {
	case errors.Is(err, model.ErrGameSessionClosed):
		// 同一局的另一次提交刚结算完，或者这局已被新开的一局作废：重新读取对局再回应。
		current, err := model.GetUserGameSession(userId, gameTetris, session.Id)
		if err == nil && current.Status == model.GameSessionStatusFinished {
			respondSettledTetris(c, current, rule)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return
	case errors.Is(err, model.ErrGameSessionSuperseded):
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return
	case err != nil:
		common.SysError(fmt.Sprintf("failed to settle tetris session %d for user %d: %v", session.Id, userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameFinishFailed)
		return
	}

	if tooFast {
		service.RecordGameCheatAlert(gameTetris, userId, ip, fmt.Sprintf(
			"俄罗斯方块对局 #%d 在 %d 秒内锁定了 %d 块，超过人类操作极限 %d 块，判定为脚本，未发奖励",
			session.Id, tetris.SpeedWindowTicks/tetris.TicksPerSecond, game.PeakPieces, tetris.HumanPieceLimit))
	}
	// 发奖记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号在领游戏奖励。
	if settled.Reward > 0 {
		model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
			fmt.Sprintf("游戏奖励：俄罗斯方块 %d 分，获得 %d 个游戏币", settled.Score, settled.Reward),
			map[string]interface{}{"game": gameTetris, "session_id": settled.Id, "score": settled.Score, "game_coins": settled.Reward})
	}
	common.ApiSuccess(c, tetrisResult(settled, detail, rule, earned, balance, max(best, settled.Score)))
}

// respondSettledTetris 回应一局已经结算过的俄罗斯方块：成绩与奖励取自第一次结算时的记录，
// 今日进度、余额与最高分取当前值。
func respondSettledTetris(c *gin.Context, session *model.GameSession, rule operation_setting.GameRule) {
	var detail tetrisDetail
	if err := common.UnmarshalJsonStr(session.Detail, &detail); err != nil {
		common.ApiError(c, err)
		return
	}
	earned, err := model.GetGameRewardEarned(session.UserId, gameTetris, gameCenterDay())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	account, err := model.GetGameCoinAccount(session.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	best, err := model.GetGameBestScore(session.UserId, gameTetris)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tetrisResult(session, detail, rule, earned, account.Balance, best))
}

// tetrisResult 是一局俄罗斯方块的结算结果，首次结算与重复提交返回同样的内容。
func tetrisResult(session *model.GameSession, detail tetrisDetail, rule operation_setting.GameRule, earned int, balance int, best int) gin.H {
	return gin.H{
		"score":                session.Score,
		"lines":                detail.Lines,
		"level":                detail.Level,
		"pieces":               detail.Pieces,
		"ticks":                detail.Ticks,
		"reward":               session.Reward,
		"limited_by":           session.LimitedBy,
		"earned_today":         earned,
		"balance":              balance,
		"best_score":           best,
		"daily_reward_limit":   rule.DailyRewardLimit,
		"score_per_coin":       rule.ScorePerCoin,
		"min_seconds_per_coin": rule.MinSecondsPerCoin,
	}
}
