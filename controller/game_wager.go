package controller

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 下注游戏的公共流程。赔付表或赔率公式在各游戏的 pkg 包里，理论返还率低于 100%，玩家长期是输的，所以不限次数、不设每日
// 上限，也不要求人机验证：脚本连续下注只会输得更快。下注可以有两位小数，派彩按倍数精确到额度单位，零头舍去。

// gameAuditMinNet 是一局净赢多少个游戏币时记审计日志。按倍数记会被常见的高倍玩法刷满(骰子 10 倍约一成能中、轮盘单号
// 每中一次就是 36 倍)，风控关心的是有多少游戏币被赢走，所以按净赢的数量记。
const gameAuditMinNet = 100

// gameBetDecimals 是下注、对战押注与投入奖池最多几位小数，gameMinBetText 是最少下多少。
const (
	gameBetDecimals = 2
	gameMinBetText  = "0.01"
)

// gameMinBet 是最少的一注，即 0.01 个游戏币。
func gameMinBet() model.GameCoins {
	coins, err := model.ParseGameCoins(gameMinBetText)
	if err != nil {
		return 1
	}
	return max(coins, 1)
}

// wagerInfo 返回下注游戏页的通用信息：单次下注上下限、今天的输赢与局数、单局最高派彩。
func wagerInfo(ctx *gameContext, game string, maxBet int) (gin.H, error) {
	net, plays, err := model.GetGameDayStats(ctx.UserId, game, ctx.Day)
	if err != nil {
		return nil, err
	}
	best, err := model.GetGameBestScore(ctx.UserId, game)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"max_bet":      maxBet,
		"min_bet":      gameMinBet(),
		"bet_decimals": gameBetDecimals,
		"net_today":    net,
		"plays_today":  plays,
		"best_win":     model.GameCoins(best),
	}, nil
}

// validGameStake 表示 amount 在 [0.01 个币, maxCoins 个币] 之内、最多两位小数。下注、对战押注与投入奖池共用。
func validGameStake(amount model.GameCoins, maxCoins int) bool {
	limit, err := model.WholeGameCoins(maxCoins)
	return err == nil && amount >= gameMinBet() && amount <= limit && amount.HasDecimals(gameBetDecimals)
}

// validWagerBet 检查单次下注在 [0.01, maxBet] 个游戏币之内、最多两位小数，不在时写好错误响应并返回 false。
func validWagerBet(c *gin.Context, bet model.GameCoins, maxBet int) bool {
	if validGameStake(bet, maxBet) {
		return true
	}
	common.ApiErrorI18n(c, i18n.MsgGameBetInvalid, map[string]any{"Min": gameMinBetText, "Max": maxBet})
	return false
}

// validWagerStep 检查分档投入的游戏选的是开放的一档(不超过 maxBet 的那几档，都是整币)，是时返回这一档的游戏币；不是时写好错误
// 响应，ok 为 false。
func validWagerStep(c *gin.Context, bet int, steps []int, maxBet int) (model.GameCoins, bool) {
	open := make([]int, 0, len(steps))
	for _, step := range steps {
		if step <= maxBet {
			open = append(open, step)
		}
	}
	if slices.Contains(open, bet) {
		if coins, err := model.WholeGameCoins(bet); err == nil {
			return coins, true
		}
	}
	common.ApiErrorI18n(c, i18n.MsgGameBetStepInvalid, map[string]any{"Steps": fmt.Sprint(open)})
	return 0, false
}

// respondGameError 把结算时的常见错误换成给玩家看的提示；其他错误记日志后提示操作失败。
func respondGameError(c *gin.Context, game string, err error) {
	switch {
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
	case errors.Is(err, model.ErrGameRoundOpen):
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
	case errors.Is(err, model.ErrGameRoundChanged):
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
	case errors.Is(err, model.ErrGameFreePlayUsed):
		common.ApiErrorI18n(c, i18n.MsgGameFreePlayUsed)
	case errors.Is(err, model.ErrGameIpFreePlayUsed):
		common.ApiErrorI18n(c, i18n.MsgGameIpFreePlayUsed)
	default:
		common.SysError(fmt.Sprintf("failed to settle %s for user %d: %v", game, c.GetInt("id"), err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
	}
}

// auditGameWin 在一局下注净赢达到 gameAuditMinNet 时记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号
// 从下注游戏赢走了游戏币。
func auditGameWin(c *gin.Context, game string, name string, session *model.GameSession) {
	threshold, err := model.WholeGameCoins(gameAuditMinNet)
	if err != nil || session.Reward-session.Bet < threshold {
		return
	}
	model.RecordAuditLog(model.ClientLogSource(c), session.UserId, "", model.LogTypeSystem,
		fmt.Sprintf("游戏大奖：%s下注 %s，派彩 %s 个游戏币", name, session.Bet.Decimal(), session.Reward.Decimal()),
		map[string]interface{}{"game": game, "session_id": session.Id, "bet": session.Bet, "game_coins": session.Reward})
}

// wagerResultView 是一局下注游戏的结算结果里各游戏共用的部分。
func wagerResultView(result *model.GameWagerResult) gin.H {
	return gin.H{
		"bet":         result.Session.Bet,
		"win":         result.Session.Reward,
		"net":         result.Session.Reward - result.Session.Bet,
		"balance":     result.Balance,
		"net_today":   result.Net,
		"plays_today": result.Plays,
	}
}

// loadGameRound 读出玩家在这个游戏里进行中的一局并把状态解析进 state；没有进行中的一局时返回 nil 且不写响应，
// 出错时写好错误响应并返回 nil、false。
func loadGameRound(c *gin.Context, game string, state any) (*model.GameSession, bool) {
	session, err := model.GetPlayingGameSession(c.GetInt("id"), game)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if session == nil {
		return nil, true
	}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil {
		common.SysError(fmt.Sprintf("failed to decode %s round %d: %v", game, session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return nil, false
	}
	return session, true
}

// wagerRoundRequest 是分几步完成的下注游戏(21 点、挖宝、高低牌、火箭)继续一局的请求都要带的部分：玩家看到的对局编号与版本。
type wagerRoundRequest struct {
	SessionId int `json:"session_id"`
	Version   int `json:"version"`
}

// loadWagerRound 读出玩家在 game 里进行中的一局，并核对请求带的对局编号与版本就是它现在的样子，状态解析进 state。
// 没有进行中的一局时回应 MsgGameRoundNotFound；编号或版本对不上(另一个请求已经改过或结束了这一局)时回应 MsgGameRoundChanged，
// 前端重新读取对局再操作。继续已有的一局不检查游戏是否开放：游戏关闭时押着游戏币的对局也要能玩完。返回 nil 时已写好响应。
// 21 点、挖宝、高低牌、火箭共用。
func loadWagerRound(c *gin.Context, game string, req wagerRoundRequest, state any) *model.GameSession {
	session, ok := loadGameRound(c, game, state)
	if !ok {
		return nil
	}
	if session == nil {
		common.ApiErrorI18n(c, i18n.MsgGameRoundNotFound)
		return nil
	}
	if session.Id != req.SessionId || session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return nil
	}
	return session
}

// findWagerRound 读出 GET /api/game/<key>/round 要看的一局：查询参数带 session_id 时是玩家自己的这一局，可以已经结束(前端据此
// 查看被另一个请求结束、或响应丢失的一局的结果)；不带时是进行中的一局。没有时返回 nil 且不写响应，出错时写好错误响应并返回
// nil、false。21 点、挖宝、高低牌、火箭共用。
func findWagerRound(c *gin.Context, game string, state any) (*model.GameSession, bool) {
	id, err := strconv.Atoi(c.Query("session_id"))
	if err != nil || id <= 0 {
		return loadGameRound(c, game, state)
	}
	return loadWagerRoundById(c, game, id, state)
}

// loadWagerRoundById 读出玩家自己在 game 里编号为 id 的一局(可以已经结束)，状态解析进 state。没有时返回 nil 且不写响应，
// 出错时写好错误响应并返回 nil、false。
func loadWagerRoundById(c *gin.Context, game string, id int, state any) (*model.GameSession, bool) {
	session, err := model.GetUserGameSession(c.GetInt("id"), game, id)
	if errors.Is(err, model.ErrGameSessionNotFound) {
		return nil, true
	}
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil {
		common.SysError(fmt.Sprintf("failed to decode %s round %d: %v", game, session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return nil, false
	}
	return session, true
}

// gameRoundResumable 表示玩家在这个游戏里还有进行中的一局，游戏关闭后也要能回来把它玩完。
func gameRoundResumable(game string) func(ctx *gameContext) bool {
	return func(ctx *gameContext) bool {
		session, err := model.GetPlayingGameSession(ctx.UserId, game)
		return err == nil && session != nil
	}
}

// auditGameFreeWin 给免费奖励(大转盘的免费旋转)每一次中奖记一条带来源 IP 的审计日志，和技巧游戏的奖励一样：没有下注，
// 开出多少就白得多少，最高 50 个游戏币也不受奖励合计上限约束，风控要能在多账号视图里看到同一 IP 下哪些账号在领。
// 每人每天只有一两次免费旋转，日志不会多。
func auditGameFreeWin(c *gin.Context, game string, name string, session *model.GameSession) {
	if session.Reward <= 0 {
		return
	}
	model.RecordAuditLog(model.ClientLogSource(c), session.UserId, "", model.LogTypeSystem,
		fmt.Sprintf("游戏奖励：%s，获得 %s 个游戏币", name, session.Reward.Decimal()),
		map[string]interface{}{"game": game, "session_id": session.Id, "free": true, "game_coins": session.Reward})
}

// gameRoundView 是分几步完成的一局给玩家看的公共部分，各游戏再补上自己能公开的状态。结束的一局带上派彩。
func gameRoundView(session *model.GameSession) gin.H {
	view := gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"status":     session.Status,
		"bet":        session.Bet,
		"created_at": session.CreatedAt,
	}
	if session.Status == model.GameSessionStatusFinished {
		view["win"] = session.Reward
		view["net"] = session.Reward - session.Bet
	}
	return view
}
