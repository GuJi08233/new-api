package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/crash"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameCrash = "crash"

const (
	// crashRecentLimit 是游戏页列出玩家最近几局的爆点。
	crashRecentLimit = 10
	// crashPollTimeout 是 GET /round?wait=1 最多等多久，远低于常见反向代理的空闲超时。
	crashPollTimeout = 25 * time.Second
	// crashSettleGrace 是到了爆点(或自动兑现的时刻)之后，长轮询再等多久才结算：玩家在爆炸前一刻发出的兑现可能还在路上
	// 或正在读库，先让它按收到的时刻结算，不被长轮询抢先判成爆炸。
	crashSettleGrace = 250 * time.Millisecond
)

// crashFlightSeconds 是一局火箭最长能飞多久：倍数涨到上限 MaxPoint 的时刻，那时任何一局都已经爆了或兑现了。
var crashFlightSeconds = crash.TimeOf(crash.MaxPoint)/1000 + 1

// newCrashPoint 开局时决定爆点，来自密码学安全的随机数。测试里替换成固定的爆点。
var newCrashPoint = crash.NewPoint

// crashNowMs 返回服务端现在的毫秒时间，火箭的倍数按开局以来的毫秒数计算。测试里替换成可控的时钟。
var crashNowMs = func() int64 { return time.Now().UnixMilli() }

// crashSleep 让长轮询等待 d，请求被取消(玩家离开或前端放弃等待)时提前返回 false。测试里替换成拨快时钟。
var crashSleep = func(c *gin.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func init() {
	registerGame(&gameDefinition{
		Key:       gameCrash,
		Kind:      operation_setting.GameKindWager,
		Info:      crashInfo,
		Resumable: gameRoundResumable(gameCrash),
		Routes: func(group *gin.RouterGroup) {
			group.GET("/round", middleware.GamePollRateLimit("crash-round"), GetCrashRound)
			group.POST("/start", middleware.GameSpinRateLimit("crash-start"), StartCrash)
			group.POST("/cashout", middleware.GameActionRateLimit("crash-cashout"), CashoutCrash)
		},
	})
}

// crashState 是一局火箭在服务端保存的状态。爆点 Point 只有服务端知道，结束之前不出现在任何响应里；倍数都是百分之一倍。
type crashState struct {
	Point int `json:"point"`
	// Auto 是自动兑现的倍数，0 表示不自动兑现。
	Auto int `json:"auto"`
	// StartedAtMs 是开局的服务端毫秒时间，倍数从这一刻起按时间表上升。
	StartedAtMs int64 `json:"started_at_ms"`
	// Cashout 是结束时兑现的倍数，爆炸时为 0。
	Cashout int `json:"cashout"`
}

type crashStartRequest struct {
	Bet         int `json:"bet"`
	AutoCashout int `json:"auto_cashout"`
}

// crashDetail 是一局火箭结束时的明细，存在对局的 Detail 里；游戏页从这里读出玩家最近几局的爆点。
type crashDetail struct {
	Crash   int `json:"crash"`
	Auto    int `json:"auto,omitempty"`
	Cashout int `json:"cashout"`
}

// crashInfo 返回火箭页的信息：返还率、倍数的增长速率(每毫秒，倍数 = floor(100 × e^(growth × 毫秒)) 百分之一倍)、爆点上限与
// 自动兑现的下限(百分之一倍)，以及玩家最近结束的几局的爆点(从新到旧)。
func crashInfo(ctx *gameContext) (gin.H, error) {
	rule := ctx.Setting.Crash
	info, err := wagerInfo(ctx, gameCrash, rule.MaxBet)
	if err != nil {
		return nil, err
	}
	sessions, err := model.ListUserFinishedGameSessions(ctx.UserId, gameCrash, crashRecentLimit)
	if err != nil {
		return nil, err
	}
	recent := make([]int, 0, len(sessions))
	for _, session := range sessions {
		var detail crashDetail
		if common.UnmarshalJsonStr(session.Detail, &detail) == nil && detail.Crash > 0 {
			recent = append(recent, detail.Crash)
		}
	}
	info["rtp_percent"] = rule.RtpPercent
	info["growth"] = crash.Growth
	info["max_multiplier"] = crash.MaxPoint
	info["min_auto_cashout"] = crash.MinAutoCashout
	info["recent"] = recent
	return info, nil
}

// crashRoundView 是一局火箭给玩家看的样子：开局的服务端毫秒时间 started_at_ms、现在的服务端毫秒时间 server_time_ms(前端据此校准
// 时钟，按同一张时间表播放上升动画)与自动兑现 auto_cashout。结束之后才给出爆点 crash_point 与兑现的倍数 cashout_multiplier
// (爆炸时为 0)。
func crashRoundView(session *model.GameSession, state *crashState) gin.H {
	view := gameRoundView(session)
	view["started_at_ms"] = state.StartedAtMs
	view["server_time_ms"] = crashNowMs()
	view["auto_cashout"] = state.Auto
	if session.Status == model.GameSessionStatusFinished {
		view["crash_point"] = state.Point
		view["cashout_multiplier"] = state.Cashout
	}
	return view
}

// settleCrash 按服务端毫秒时间 now 判定这一局：已经爆炸或到了自动兑现的时刻就结算；cashout 表示玩家在这一刻请求兑现，
// 一定会结算。返回的结果为 nil 表示火箭还在飞。系统任务结算时 c 为 nil。
func settleCrash(c *gin.Context, session *model.GameSession, state *crashState, now int64, cashout bool) (*model.GameWagerResult, error) {
	over, multiplier := crash.Settle(state.Point, state.Auto, now-state.StartedAtMs, cashout)
	if !over {
		return nil, nil
	}
	state.Cashout = multiplier
	data, err := common.Marshal(state)
	if err != nil {
		return nil, err
	}
	detail, err := common.Marshal(crashDetail{Crash: state.Point, Auto: state.Auto, Cashout: multiplier})
	if err != nil {
		return nil, err
	}
	result, err := model.SettleGameRound(session, model.GameRoundSettlement{
		Win:    crash.Payout(session.Bet, multiplier),
		State:  string(data),
		Detail: string(detail),
		Day:    gameCenterDay(),
	})
	if err != nil {
		return nil, err
	}
	auditGameWin(c, gameCrash, "火箭", result.Session)
	return result, nil
}

// respondSettledCrash 回应结束的一局与结算后的余额、今天的输赢与局数。
func respondSettledCrash(c *gin.Context, result *model.GameWagerResult, state *crashState) {
	common.ApiSuccess(c, gin.H{
		"round":       crashRoundView(result.Session, state),
		"balance":     result.Balance,
		"net_today":   result.Net,
		"plays_today": result.Plays,
	})
}

// GetCrashRound 返回玩家进行中的一局火箭；带 session_id 时返回那一局(可以已经结束)，没有时 round 为 null。
// 火箭已经过了爆点或自动兑现的时刻时在这里结算，响应带上结算后的余额。带 wait=1 时是长轮询：火箭还在飞就等到它自己结束的时刻
// (最多 crashPollTimeout)再判定，前端在爆炸或自动兑现的那一刻拿到结果，不用反复查询；等待不会提前透露爆点，
// 到时候没结束(只会是等满了时限)照常返回还在飞的这一局。
func GetCrashRound(c *gin.Context) {
	var state crashState
	session, ok := findWagerRound(c, gameCrash, &state)
	if !ok {
		return
	}
	if session == nil {
		common.ApiSuccess(c, gin.H{"round": nil})
		return
	}
	if session.Status == model.GameSessionStatusPlaying && c.Query("wait") != "" {
		endsAt := state.StartedAtMs + crash.EndsAt(state.Point, state.Auto)
		if wait := min(endsAt+crashSettleGrace.Milliseconds()-crashNowMs(), crashPollTimeout.Milliseconds()); wait > 0 {
			if !crashSleep(c, time.Duration(wait)*time.Millisecond) {
				return
			}
			// 等待期间玩家可能已经兑现。
			if session = reloadCrashRound(c, session.Id, &state); session == nil {
				return
			}
		}
	}
	if session.Status == model.GameSessionStatusPlaying {
		result, err := settleCrash(c, session, &state, crashNowMs(), false)
		switch {
		case errors.Is(err, model.ErrGameRoundChanged):
			// 同一局刚被另一个请求(兑现或另一次查看)结算，读出它的结果。
			if session = reloadCrashRound(c, session.Id, &state); session == nil {
				return
			}
		case err != nil:
			respondGameError(c, gameCrash, err)
			return
		case result != nil:
			respondSettledCrash(c, result, &state)
			return
		}
	}
	common.ApiSuccess(c, gin.H{"round": crashRoundView(session, &state)})
}

// reloadCrashRound 按编号重新读出玩家的这一局火箭，另一个请求可能刚结算了它。返回 nil 时已写好响应。
func reloadCrashRound(c *gin.Context, id int, state *crashState) *model.GameSession {
	*state = crashState{}
	session, ok := loadWagerRoundById(c, gameCrash, id, state)
	if ok && session == nil {
		common.ApiErrorI18n(c, i18n.MsgGameRoundNotFound)
	}
	return session
}

// StartCrash 开一局火箭：扣下注，服务端决定爆点并记下开局时刻，倍数从这一刻起按时间表上升。响应不含爆点。
// 上一局已经爆炸或到了自动兑现的时刻、只是还没人来结算时，先把它结算掉；还在飞时要先把它玩完。
func StartCrash(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Crash
	if !requireGameEnabled(c, gameCrash) {
		return
	}
	var req crashStartRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, 1, rule.MaxBet) {
		return
	}
	if req.AutoCashout != 0 && (req.AutoCashout < crash.MinAutoCashout || req.AutoCashout > crash.MaxPoint) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var previous crashState
	session, ok := loadGameRound(c, gameCrash, &previous)
	if !ok {
		return
	}
	if session != nil {
		if _, err := settleCrash(c, session, &previous, crashNowMs(), false); err != nil && !errors.Is(err, model.ErrGameRoundChanged) {
			respondGameError(c, gameCrash, err)
			return
		}
	}
	point, err := newCrashPoint(rule.RtpPercent)
	if err != nil {
		respondGameError(c, gameCrash, err)
		return
	}
	state := crashState{Point: point, Auto: req.AutoCashout, StartedAtMs: crashNowMs()}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session, balance, err := model.OpenGameRound(c.GetInt("id"), gameCrash, model.GameRound{
		Bet:   req.Bet,
		State: string(data),
		Ip:    gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameCrash, err)
		return
	}
	common.ApiSuccess(c, gin.H{"round": crashRoundView(session, &state), "balance": balance})
}

// CashoutCrash 兑现：按服务端收到请求这一刻的倍数派彩 floor(下注 × 倍数 / 100)；这一刻火箭已经爆了就输掉下注，
// 已经到了自动兑现的时刻就按自动兑现的倍数结算。
func CashoutCrash(c *gin.Context) {
	// 按收到请求的时刻判定，读库花的时间不算在玩家头上。
	now := crashNowMs()
	var req wagerRoundRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var state crashState
	session := loadWagerRound(c, gameCrash, req, &state)
	if session == nil {
		return
	}
	result, err := settleCrash(c, session, &state, now, true)
	if err != nil {
		respondGameError(c, gameCrash, err)
		return
	}
	respondSettledCrash(c, result, &state)
}

// sweepCrashRounds 结算玩家离开之后自己结束的火箭：开局已经超过最长飞行时间、还没人来结算的一局，按爆点或自动兑现结算，
// 自动兑现的派彩不必等玩家回来。返回结算了几局与出错的局数。
func sweepCrashRounds(ctx context.Context, now int64) (settled int, failed int, err error) {
	sessions, err := model.ListStalePlayingGameSessions(gameCrash, now-crashFlightSeconds, gameCenterSweepBatch)
	if err != nil {
		return 0, 0, err
	}
	for _, session := range sessions {
		if ctx.Err() != nil {
			break
		}
		var state crashState
		if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
			common.SysError(fmt.Sprintf("game center sweep: failed to decode crash round %d: %v", session.Id, err))
			failed++
			continue
		}
		result, err := settleCrash(nil, session, &state, crashNowMs(), false)
		switch {
		case errors.Is(err, model.ErrGameRoundChanged):
			// 玩家刚好回来结算了这一局。
		case err != nil:
			common.SysError(fmt.Sprintf("game center sweep: failed to settle crash round %d: %v", session.Id, err))
			failed++
		case result != nil:
			settled++
		}
	}
	return settled, failed, nil
}
