package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/blackjack"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameBlackjack = "blackjack"

// blackjackMinBet 是 21 点最少的下注：黑杰克赔 3:2，下注是偶数才能按整数派彩。
const blackjackMinBet = 2

// blackjackPayback 是这套规则按基本策略玩的理论返还率(约 99.5%)，是公开的标准结果，只用于页面公示，见 pkg/blackjack。
const blackjackPayback = 0.995

// shuffleBlackjack 每局洗一整个牌靴，牌序来自密码学安全的随机数。测试里替换成固定的牌序。
var shuffleBlackjack = blackjack.Shuffle

func init() {
	registerGame(&gameDefinition{
		Key:  gameBlackjack,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameBlackjack, ctx.Setting.Blackjack.MaxBet)
			if err != nil {
				return nil, err
			}
			info["min_bet"] = blackjackMinBet
			info["decks"] = blackjack.Decks
			info["payouts"] = gin.H{"win": 2, "push": 1, "blackjack": 2.5}
			info["payback"] = blackjackPayback
			return info, nil
		},
		Resumable: gameRoundResumable(gameBlackjack),
		Routes: func(group *gin.RouterGroup) {
			group.GET("/round", middleware.GamePollRateLimit("blackjack-round"), GetBlackjackRound)
			group.POST("/deal", middleware.GameSpinRateLimit("blackjack-deal"), DealBlackjack)
			group.POST("/action", middleware.GameActionRateLimit("blackjack-action"), ActBlackjack)
		},
	})
}

type blackjackDealRequest struct {
	Bet int `json:"bet"`
}

type blackjackActionRequest struct {
	wagerRoundRequest
	Action string `json:"action"`
}

// blackjackDetail 是一局 21 点结束时的明细，存在对局的 Detail 里供事后核查：每一手的点数与结果、庄家的点数。
type blackjackDetail struct {
	Player   []int    `json:"player"`
	Outcomes []string `json:"outcomes"`
	Dealer   int      `json:"dealer"`
}

// blackjackCards 把牌写成给玩家看的样子：点数 rank 1(A) 到 13(K)，花色 suit 0 黑桃、1 红心、2 方块、3 梅花。
func blackjackCards(cards []int) []gin.H {
	views := make([]gin.H, 0, len(cards))
	for _, card := range cards {
		views = append(views, gin.H{"rank": blackjack.Rank(card), "suit": blackjack.Suit(card)})
	}
	return views
}

// blackjackRoundView 是一局 21 点给玩家看的样子：每一手的牌、点数(soft 表示有 A 按 11 算)、下注、是否加倍、状态
// (playing/waiting/stand/bust/blackjack)，结束后还有结果 result(win/lose/push/blackjack)与这一手的派彩 payout；轮到操作的那一手
// current(没有时为 -1)；庄家的牌，结束之前只给明牌(hidden 为真，total 是明牌的点数)；peeked 表示庄家明牌是 A 或 10 点、发牌后
// 偷看过暗牌，这一局还在进行就说明庄家不是黑杰克；actions 是当前一手可以做的操作，加倍、分牌还要余额够追加这一手的下注。
// 牌靴与暗牌不出现在响应里。
func blackjackRoundView(session *model.GameSession, game *blackjack.Game) gin.H {
	var results []blackjack.Result
	if game.Over {
		results = game.Results()
	}
	hands := make([]gin.H, 0, len(game.Hands))
	for i, hand := range game.Hands {
		total, soft := blackjack.Total(hand.Cards)
		item := gin.H{
			"cards":   blackjackCards(hand.Cards),
			"total":   total,
			"soft":    soft,
			"bet":     hand.Bet,
			"doubled": hand.Doubled,
			"status":  game.Status(i),
		}
		if game.Over {
			item["result"] = results[i].Outcome
			item["payout"] = results[i].Payout
		}
		hands = append(hands, item)
	}
	dealer, current := game.Dealer, game.Current
	if !game.Over {
		dealer = dealer[:1]
	} else {
		current = -1
	}
	dealerTotal, dealerSoft := blackjack.Total(dealer)
	view := gameRoundView(session)
	view["hands"] = hands
	view["current"] = current
	view["dealer"] = gin.H{"cards": blackjackCards(dealer), "total": dealerTotal, "soft": dealerSoft, "hidden": !game.Over}
	view["peeked"] = game.Peeked
	view["actions"] = game.Actions()
	return view
}

// GetBlackjackRound 返回玩家进行中的一局 21 点，刷新页面、游戏关闭之后都能回来接着打；带 session_id 时返回那一局(可以已经结束)。
// 没有时 round 为 null。
func GetBlackjackRound(c *gin.Context) {
	var game blackjack.Game
	session, ok := findWagerRound(c, gameBlackjack, &game)
	if !ok {
		return
	}
	if session == nil {
		common.ApiSuccess(c, gin.H{"round": nil})
		return
	}
	// 发牌即结束的黑杰克、加倍或分牌之后结束的一局要记两次账(扣注与派彩)，第二次出错时这一局已经结束、却还没派彩，在这里补上。
	if session.Status == model.GameSessionStatusPlaying && game.Over {
		settleBlackjack(c, session, &game)
		return
	}
	common.ApiSuccess(c, gin.H{"round": blackjackRoundView(session, &game)})
}

// DealBlackjack 开一局 21 点：扣下注，洗牌、发牌。有黑杰克时发完牌就结束并派彩。同一时间只能有一局，还有没结束的一局时要先把它
// 玩完。
func DealBlackjack(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Blackjack
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req blackjackDealRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, blackjackMinBet, rule.MaxBet) {
		return
	}
	if req.Bet%2 != 0 {
		common.ApiErrorI18n(c, i18n.MsgGameBetEven)
		return
	}
	shoe, err := shuffleBlackjack()
	if err != nil {
		respondGameError(c, gameBlackjack, err)
		return
	}
	game, err := blackjack.Deal(shoe, req.Bet)
	if err != nil {
		respondGameError(c, gameBlackjack, err)
		return
	}
	state, err := common.Marshal(game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session, balance, err := model.OpenGameRound(c.GetInt("id"), gameBlackjack, model.GameRound{
		Bet:   req.Bet,
		State: string(state),
		Ip:    gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameBlackjack, err)
		return
	}
	if game.Over {
		settleBlackjack(c, session, game)
		return
	}
	common.ApiSuccess(c, gin.H{"round": blackjackRoundView(session, game), "balance": balance})
}

// ActBlackjack 让当前一手要牌(hit)、停牌(stand)、加倍(double)或分牌(split)。加倍与分牌追加与这一手相同的下注，余额不够时拒绝，
// 什么都不会改变。所有手都结束后庄家补牌、这一局结算。
func ActBlackjack(c *gin.Context) {
	var req blackjackActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var game blackjack.Game
	session := loadWagerRound(c, gameBlackjack, req.wagerRoundRequest, &game)
	if session == nil {
		return
	}
	extra, err := game.Act(req.Action)
	if errors.Is(err, blackjack.ErrIllegalAction) {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	if err != nil {
		respondGameError(c, gameBlackjack, err)
		return
	}
	state, err := common.Marshal(game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := gin.H{}
	switch {
	case extra > 0:
		// 追加下注与新状态在同一个事务里保存；这一步让这一局结束时，接着再派彩。
		balance, err := model.RaiseGameRound(session, extra, string(state), "")
		if err != nil {
			respondGameError(c, gameBlackjack, err)
			return
		}
		response["balance"] = balance
	case !game.Over:
		if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
			respondGameError(c, gameBlackjack, err)
			return
		}
	}
	if game.Over {
		settleBlackjack(c, session, &game)
		return
	}
	response["round"] = blackjackRoundView(session, &game)
	common.ApiSuccess(c, response)
}

// settleBlackjack 结束这一局 21 点，按各手的结果派彩，回应结束的一局与结算后的余额、今天的输赢与局数。
func settleBlackjack(c *gin.Context, session *model.GameSession, game *blackjack.Game) {
	results := game.Results()
	win := 0
	detail := blackjackDetail{Player: make([]int, 0, len(results)), Outcomes: make([]string, 0, len(results))}
	for i, result := range results {
		total, _ := blackjack.Total(game.Hands[i].Cards)
		win += result.Payout
		detail.Player = append(detail.Player, total)
		detail.Outcomes = append(detail.Outcomes, result.Outcome)
	}
	detail.Dealer, _ = blackjack.Total(game.Dealer)
	state, err := common.Marshal(game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	detailJson, err := common.Marshal(detail)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SettleGameRound(session, win, string(state), string(detailJson), gameCenterDay())
	if err != nil {
		respondGameError(c, gameBlackjack, err)
		return
	}
	auditGameWin(c, gameBlackjack, "21 点", result.Session)
	common.ApiSuccess(c, gin.H{
		"round":       blackjackRoundView(result.Session, game),
		"balance":     result.Balance,
		"net_today":   result.Net,
		"plays_today": result.Plays,
	})
}
