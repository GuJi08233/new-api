package controller

import (
	"math/big"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/hilo"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameHilo = "hilo"

// drawHiloCard 翻开一张牌，点数与花色来自密码学安全的随机数。测试里替换成固定的牌序。
var drawHiloCard = hilo.Draw

func init() {
	registerGame(&gameDefinition{
		Key:  gameHilo,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			rule := ctx.Setting.Hilo
			info, err := wagerInfo(ctx, gameHilo, rule.MaxBet)
			if err != nil {
				return nil, err
			}
			info["rtp_percent"] = rule.RtpPercent
			info["max_skips"] = hilo.MaxSkips
			info["max_multiplier"] = hilo.MaxMultiplier
			return info, nil
		},
		Resumable: gameRoundResumable(gameHilo),
		Routes: func(group *gin.RouterGroup) {
			group.GET("/round", middleware.GamePollRateLimit("hilo-round"), GetHiloRound)
			group.POST("/start", middleware.GameSpinRateLimit("hilo-start"), StartHilo)
			group.POST("/guess", middleware.GameActionRateLimit("hilo-guess"), GuessHilo)
			group.POST("/cashout", middleware.GameActionRateLimit("hilo-cashout"), CashoutHilo)
		},
	})
}

type hiloStartRequest struct {
	Bet model.GameCoins `json:"bet"`
}

type hiloGuessRequest struct {
	wagerRoundRequest
	Guess string `json:"guess"`
}

// hiloDetail 是一局高低牌结束时的明细，存在对局的 Detail 里供事后核查。兑现的倍数就是派彩除以下注，精确的累计倍数在对局状态里。
type hiloDetail struct {
	Wins  int  `json:"wins"`
	Skips int  `json:"skips"`
	Lost  bool `json:"lost"`
}

// hiloRoundView 是一局高低牌给玩家看的样子：当前牌 card、之前的牌与在每张牌上的选择 history(guess 是 higher/lower/same/skip，
// correct 表示猜对)、累计倍数 multiplier(截断到两位小数)、猜对次数 wins 与剩余换牌次数 skips_left。进行中时还有现在兑现的派彩
// cashout(还没猜对过时为 0，不能兑现)，以及当前牌上两种猜法各自命中的点数种数 hits、概率 chance、猜对后的累计倍数与可兑现派彩。
// 下一张牌在猜的那一刻才翻开，没有需要隐藏的信息。
func hiloRoundView(session *model.GameSession, round *hilo.Round) (gin.H, error) {
	multiplier, err := round.Value()
	if err != nil {
		return nil, err
	}
	history := make([]gin.H, 0, len(round.Moves))
	for i, move := range round.Moves {
		history = append(history, gin.H{
			"card":    round.Cards[i],
			"guess":   move,
			"correct": move != hilo.Skip && hilo.Correct(round.Cards[i].Rank, move, round.Cards[i+1].Rank),
		})
	}
	view := gameRoundView(session)
	view["card"] = round.Current()
	view["history"] = history
	view["multiplier"] = hilo.FormatMultiplier(multiplier)
	view["wins"] = round.Wins
	view["skips_left"] = hilo.MaxSkips - round.Skips
	if session.Status == model.GameSessionStatusFinished {
		return view, nil
	}
	cashout := model.GameCoins(0)
	if round.Wins > 0 {
		cashout = model.GameCoins(hilo.Payout(int(session.Bet), multiplier))
	}
	rank := round.Current().Rank
	options := make([]gin.H, 0, 2)
	for _, guess := range hilo.Guesses(rank) {
		hits := hilo.Hits(rank, guess)
		next := new(big.Rat).Mul(multiplier, hilo.StepMultiplier(round.RtpPercent, hits))
		options = append(options, gin.H{
			"guess":      guess,
			"hits":       hits,
			"chance":     float64(hits) / hilo.Ranks,
			"multiplier": hilo.FormatMultiplier(next),
			"payout":     model.GameCoins(hilo.Payout(int(session.Bet), next)),
		})
	}
	view["cashout"] = cashout
	view["options"] = options
	return view, nil
}

// respondHiloRound 回应 extra 加上这一局的样子。
func respondHiloRound(c *gin.Context, session *model.GameSession, round *hilo.Round, extra gin.H) {
	view, err := hiloRoundView(session, round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	extra["round"] = view
	common.ApiSuccess(c, extra)
}

// GetHiloRound 返回玩家进行中的一局高低牌，刷新页面、游戏关闭之后都能回来接着猜；带 session_id 时返回那一局(可以已经结束)。
// 没有时 round 为 null。
func GetHiloRound(c *gin.Context) {
	var round hilo.Round
	session, ok := findWagerRound(c, gameHilo, &round)
	if !ok {
		return
	}
	if session == nil {
		common.ApiSuccess(c, gin.H{"round": nil})
		return
	}
	respondHiloRound(c, session, &round, gin.H{})
}

// StartHilo 开一局高低牌：扣下注，翻开第一张牌。同一时间只能有一局，还有没结束的一局时要先把它玩完。
func StartHilo(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Hilo
	if !requireGameEnabled(c, gameHilo) {
		return
	}
	var req hiloStartRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, rule.MaxBet) {
		return
	}
	first, err := drawHiloCard()
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	// 返还率记在对局里：管理员中途改了配置，已经开局的这一局也按开局时的倍数计算。
	round := hilo.NewRound(rule.RtpPercent, first)
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session, balance, err := model.OpenGameRound(c.GetInt("id"), gameHilo, model.GameRound{
		Bet:   req.Bet,
		State: string(state),
		Ip:    gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	respondHiloRound(c, session, &round, gin.H{"balance": balance})
}

// GuessHilo 猜下一张(higher/lower/same)或换一张牌(skip)：服务端这时才翻开下一张。猜错输掉下注；猜对累计倍数，
// 派彩到了封顶时自动兑现。
func GuessHilo(c *gin.Context) {
	var req hiloGuessRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var round hilo.Round
	session := loadWagerRound(c, gameHilo, req.wagerRoundRequest, &round)
	if session == nil {
		return
	}
	// 先检查这一步合不合规则再翻牌，不合规则的请求不翻牌。
	if !round.Allowed(req.Guess) {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	next, err := drawHiloCard()
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	alive, err := round.Play(req.Guess, next)
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	if !alive {
		settleHilo(c, session, &round, 0)
		return
	}
	multiplier, err := round.Value()
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	if payout := model.GameCoins(hilo.Payout(int(session.Bet), multiplier)); payout >= session.Bet*hilo.MaxMultiplier {
		settleHilo(c, session, &round, payout)
		return
	}
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	respondHiloRound(c, session, &round, gin.H{})
}

// CashoutHilo 兑现：派彩 = floor(下注 × 累计倍数)。至少猜对一次才能兑现。
func CashoutHilo(c *gin.Context) {
	var req wagerRoundRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var round hilo.Round
	session := loadWagerRound(c, gameHilo, req, &round)
	if session == nil {
		return
	}
	if round.Wins == 0 {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	multiplier, err := round.Value()
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	settleHilo(c, session, &round, model.GameCoins(hilo.Payout(int(session.Bet), multiplier)))
}

// settleHilo 结束这一局高低牌、派彩 win，回应结束的一局与结算后的余额、今天的输赢与局数。
func settleHilo(c *gin.Context, session *model.GameSession, round *hilo.Round, win model.GameCoins) {
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	detail, err := common.Marshal(hiloDetail{Wins: round.Wins, Skips: round.Skips, Lost: round.Lost})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SettleGameRound(session, model.GameRoundSettlement{Win: win, State: string(state), Detail: string(detail), Day: gameCenterDay()})
	if err != nil {
		respondGameError(c, gameHilo, err)
		return
	}
	auditGameWin(c, gameHilo, "高低牌", result.Session)
	respondHiloRound(c, result.Session, round, gin.H{
		"balance":     result.Balance,
		"net_today":   result.Net,
		"plays_today": result.Plays,
	})
}
