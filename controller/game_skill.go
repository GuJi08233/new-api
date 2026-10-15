package controller

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// 免费游戏(技巧游戏与人机对战)的公共流程：开局时发种子或出题，结束时各游戏重放整局或按保存的状态判定成绩，这里按领奖规则
// 算出应得的游戏币、按每日领奖次数与合计上限发放，并统一回应。

// skillSessionTTL 是一局免费游戏从开局到结算的最长时间(秒)：一局最长一小时，另留一小时给暂停。
const skillSessionTTL = 2 * 60 * 60

// skillOutcome 是一局免费游戏的成绩，由各游戏重放或判定后填好。
type skillOutcome struct {
	Score int
	// Seconds 是这局玩了多久(秒)，累加进玩家今天在这个游戏的游戏时长，每个游戏币所需时长按累计的时长计算。
	// 它必须以服务端核实过的游戏过程为依据，不能只是开局以来过了多久，不然开一局挂着不动就能攒够时长：
	// 重放的游戏取游戏内时长(不超过开局以来的真实时间)；一步一个请求的游戏按操作累计(GameSession.PlayedSeconds)；
	// 只在最后交一次的游戏按交上来的步数封顶；放弃的数独不计。
	Seconds int
	// Detail 是成绩明细，序列化后存进对局的 Detail，不能超过 255 字节。
	Detail any
	// State 是结束后要保存的完整状态，为空时保留对局原有的状态。
	State string
	// Cheat 不为空时判定为脚本：不发奖励、记一条风控告警，内容是告警原因。
	Cheat string
}

// newGameSeed 返回密码学安全的 32 位随机种子。
func newGameSeed() (uint32, error) {
	var seed [4]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(seed[:]), nil
}

// skillRuleView 是领奖规则在接口里的样子。
func skillRuleView(rule operation_setting.SkillRule) gin.H {
	return gin.H{
		"daily_rewards":        rule.DailyRewards,
		"tiers":                rule.Tiers,
		"min_seconds_per_coin": rule.MinSecondsPerCoin,
	}
}

// skillInfo 返回免费游戏页的通用信息：领奖规则、技巧与人机奖励的合计上限、今天的进度与最高成绩。game 是计数用的游戏标识。
func skillInfo(ctx *gameContext, game string, rule operation_setting.SkillRule) (gin.H, error) {
	progress, err := model.GetGameRewardProgress(ctx.UserId, ctx.Ip, game, ctx.Day)
	if err != nil {
		return nil, err
	}
	best, err := model.GetGameBestScore(ctx.UserId, game)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"rule":             skillRuleView(rule),
		"free_daily_limit": ctx.Setting.FreeDailyLimit,
		"progress":         progress,
		"best_score":       best,
	}, nil
}

// startSkillSession 开一局免费游戏：要求人机验证时先校验令牌，再用新的随机种子准备好服务端保存的对局状态(prepare 为空时
// 不需要状态)，然后作废旧局、记下新的一局。返回 nil 时已写好错误响应。
func startSkillSession(c *gin.Context, game string, prepare func(seed uint32) (string, error)) *model.GameSession {
	userId := c.GetInt("id")
	// 人机对战的存档键带 _ai 后缀，验证策略仍按服务端选择的游戏本身匹配。
	if !passGamePlayTurnstile(c, operation_setting.GetGameSetting(), strings.TrimSuffix(game, "_ai"), userId) {
		return nil
	}
	seed, err := newGameSeed()
	var state string
	if err == nil && prepare != nil {
		state, err = prepare(seed)
	}
	var session *model.GameSession
	if err == nil {
		session, err = model.CreateGameSession(userId, game, seed, state)
	}
	if err != nil {
		common.SysError(fmt.Sprintf("failed to start %s for user %d: %v", game, userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameStartFailed)
		return nil
	}
	return session
}

// loadSkillSession 读出玩家要继续或结算的一局免费游戏。不存在或已过期时回应错误；已经结算过时回应第一次结算的结果，
// 重复提交(例如结算成功但响应在网络上丢了)能拿到同样的答复。返回 nil 时已写好响应。
func loadSkillSession(c *gin.Context, game string, rule operation_setting.SkillRule, sessionId int) *model.GameSession {
	session, err := model.GetUserGameSession(c.GetInt("id"), game, sessionId)
	if errors.Is(err, model.ErrGameSessionNotFound) {
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return nil
	}
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	if session.Status != model.GameSessionStatusPlaying {
		respondSettledSkill(c, session, rule)
		return nil
	}
	if common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiErrorI18n(c, i18n.MsgGameSessionExpired)
		return nil
	}
	return session
}

// respondGameFinishRetry 回应一次没有提交的结算(数据库暂时出错)：对局还在进行中，原样重交就能结算。用 503 而不是
// 普通的失败回应，浏览器据此保留整局记录、让玩家重新提交，而不是当成这局被拒绝、开下一局时把它作废。
func respondGameFinishRetry(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"message": common.TranslateMessage(c, i18n.MsgGameFinishFailed),
	})
}

// settleSkillSession 按领奖规则算出一局应得的游戏币并结算，返回结算结果；返回 nil 时已写好错误响应。name 是写进审计日志的游戏名。
// 奖励按单局成绩取达到的最高一档，再受两条防脚本的规则约束：每个游戏币要玩家今天在这个游戏里累计玩够 MinSecondsPerCoin 秒；
// 判定为脚本的对局不发奖励。之后按账号与 IP 今天剩余的领奖次数、技巧与人机奖励的合计上限发放。
func settleSkillSession(c *gin.Context, name string, rule operation_setting.SkillRule, session *model.GameSession, outcome skillOutcome) gin.H {
	setting := operation_setting.GetGameSetting()
	userId := session.UserId
	detail, err := common.Marshal(outcome.Detail)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	// 最高成绩在结算之前读：结算一旦提交就不能再因为别的查询失败而报错，浏览器会把失败当成这局被拒绝。
	best, err := model.GetGameBestScore(userId, session.Game)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to read best %s score for user %d: %v", session.Game, userId, err))
		respondGameFinishRetry(c)
		return nil
	}
	reward, err := model.GameCoinsFromDecimal(decimal.NewFromFloat(rule.Reward(outcome.Score)))
	if err != nil {
		common.SysError(fmt.Sprintf("failed to price %s reward for user %d: %v", session.Game, userId, err))
		respondGameFinishRetry(c)
		return nil
	}
	freeDailyLimit, err := model.WholeGameCoins(setting.FreeDailyLimit)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to price the free reward limit: %v", err))
		respondGameFinishRetry(c)
		return nil
	}
	ip := c.ClientIP()
	result, err := model.FinishGameSession(session, model.GameSettlement{
		Score:          outcome.Score,
		Detail:         string(detail),
		State:          outcome.State,
		Reward:         reward,
		Seconds:        outcome.Seconds,
		Cheat:          outcome.Cheat != "",
		SecondsPerCoin: rule.MinSecondsPerCoin,
		DailyRewards:   rule.DailyRewards,
		FreeDailyLimit: freeDailyLimit,
		Day:            gameCenterDay(),
		Ip:             gameRewardIpKey(ip),
	})
	switch {
	case errors.Is(err, model.ErrGameSessionClosed):
		// 同一局的另一次提交刚结算完、另一步刚保存了新局面，或者这局已被新开的一局作废：重新读取对局再回应。
		current, readErr := model.GetUserGameSession(userId, session.Game, session.Id)
		if readErr == nil && current.Status == model.GameSessionStatusFinished {
			view, viewErr := skillSettledView(c, current, rule)
			if viewErr == nil {
				return view
			}
		}
		if readErr == nil && current.Status == model.GameSessionStatusPlaying {
			common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
			return nil
		}
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return nil
	case errors.Is(err, model.ErrGameSessionSuperseded):
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return nil
	case err != nil:
		common.SysError(fmt.Sprintf("failed to settle %s session %d for user %d: %v", session.Game, session.Id, userId, err))
		respondGameFinishRetry(c)
		return nil
	}

	if outcome.Cheat != "" {
		service.RecordGameCheatAlert(session.Game, userId, ip, outcome.Cheat)
	}
	settled := result.Session
	// 发奖记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号在领游戏奖励。奖励可能来自今天更早达标、
	// 等时长补够的一局，所以同时记下今天累计的时长。
	if settled.Reward > 0 {
		model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
			fmt.Sprintf("游戏奖励：%s今天累计玩了 %d 秒，本局成绩 %d，获得 %s 个游戏币", name, result.Seconds, settled.Score, settled.Reward.Decimal()),
			map[string]interface{}{"game": session.Game, "session_id": settled.Id, "score": settled.Score, "seconds_today": result.Seconds, "game_coins": settled.Reward})
	}
	return skillResultView(settled, rule, result.GameRewardProgress, result.Balance, max(best, settled.Score), setting.FreeDailyLimit)
}

// respondSettledSkill 回应一局已经结算过的免费游戏：成绩与奖励取自第一次结算时的记录，今日进度、余额与最高成绩取当前值。
func respondSettledSkill(c *gin.Context, session *model.GameSession, rule operation_setting.SkillRule) {
	view, err := skillSettledView(c, session, rule)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func skillSettledView(c *gin.Context, session *model.GameSession, rule operation_setting.SkillRule) (gin.H, error) {
	progress, err := model.GetGameRewardProgress(session.UserId, gameRewardIpKey(c.ClientIP()), session.Game, gameCenterDay())
	if err != nil {
		return nil, err
	}
	account, err := model.GetGameCoinAccount(session.UserId)
	if err != nil {
		return nil, err
	}
	best, err := model.GetGameBestScore(session.UserId, session.Game)
	if err != nil {
		return nil, err
	}
	return skillResultView(session, rule, progress, account.Balance, best, operation_setting.GetGameSetting().FreeDailyLimit), nil
}

// skillResultView 是一局免费游戏的结算结果，首次结算与重复提交返回同样的内容。detail 是各游戏自己的成绩明细。
func skillResultView(session *model.GameSession, rule operation_setting.SkillRule, progress model.GameRewardProgress, balance model.GameCoins, best int, freeDailyLimit int) gin.H {
	view := gin.H{
		"session_id":       session.Id,
		"score":            session.Score,
		"reward":           session.Reward,
		"limited_by":       session.LimitedBy,
		"progress":         progress,
		"balance":          balance,
		"best_score":       best,
		"rule":             skillRuleView(rule),
		"free_daily_limit": freeDailyLimit,
	}
	if session.Detail != "" {
		view["detail"] = json.RawMessage(session.Detail)
	}
	return view
}
