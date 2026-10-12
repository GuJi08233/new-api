package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/mines"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameMines = "mines"

// placeMines 开局时布雷，雷的位置来自密码学安全的随机数。测试里替换成固定的布局。
var placeMines = mines.Place

func init() {
	registerGame(&gameDefinition{
		Key:  gameMines,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			rule := ctx.Setting.Mines
			info, err := wagerInfo(ctx, gameMines, rule.MaxBet)
			if err != nil {
				return nil, err
			}
			info["rtp_percent"] = rule.RtpPercent
			info["grid"] = mines.Cells
			info["min_mines"] = mines.MinMines
			info["max_mines"] = mines.MaxMines
			info["max_multiplier"] = mines.MaxMultiplier
			return info, nil
		},
		Resumable: gameRoundResumable(gameMines),
		Routes: func(group *gin.RouterGroup) {
			group.GET("/round", middleware.GamePollRateLimit("mines-round"), GetMinesRound)
			group.POST("/start", middleware.GameSpinRateLimit("mines-start"), StartMines)
			group.POST("/pick", middleware.GameActionRateLimit("mines-pick"), PickMines)
			group.POST("/cashout", middleware.GameActionRateLimit("mines-cashout"), CashoutMines)
		},
	})
}

type minesStartRequest struct {
	Bet   model.GameCoins `json:"bet"`
	Mines int             `json:"mines"`
}

type minesPickRequest struct {
	wagerRoundRequest
	Index int `json:"index"`
}

// minesDetail 是一局挖宝结束时的明细，存在对局的 Detail 里供事后核查。
type minesDetail struct {
	Mines    int  `json:"mines"`
	Revealed int  `json:"revealed"`
	Lost     bool `json:"lost"`
}

// minesRoundView 是一局挖宝给玩家看的样子：雷数与按顺序翻开的安全格；进行中时带上现在兑现的派彩 cashout(还没翻开安全格时为 0，
// 不能兑现)与再翻开一个安全格之后兑现的派彩 next_cashout。雷的位置结束之后才给出(mine_cells)，踩到的那一格是 exploded。
func minesRoundView(session *model.GameSession, round *mines.Round) gin.H {
	view := gameRoundView(session)
	view["mines"] = round.Mines
	view["revealed"] = round.Revealed
	if session.Status == model.GameSessionStatusFinished {
		view["mine_cells"] = round.Layout
		if round.Lost() {
			view["exploded"] = round.Exploded
		}
		return view
	}
	cashout := model.GameCoins(0)
	if len(round.Revealed) > 0 {
		cashout = model.GameCoins(round.Cashout(int(session.Bet)))
	}
	view["cashout"] = cashout
	view["next_cashout"] = model.GameCoins(round.NextCashout(int(session.Bet)))
	return view
}

// GetMinesRound 返回玩家进行中的一局挖宝，刷新页面、游戏关闭之后都能回来接着翻；带 session_id 时返回那一局(可以已经结束)。
// 没有时 round 为 null。
func GetMinesRound(c *gin.Context) {
	var round mines.Round
	session, ok := findWagerRound(c, gameMines, &round)
	if !ok {
		return
	}
	if session == nil {
		common.ApiSuccess(c, gin.H{"round": nil})
		return
	}
	common.ApiSuccess(c, gin.H{"round": minesRoundView(session, &round)})
}

// StartMines 开一局挖宝：扣下注，服务端布好雷存在对局里。同一时间只能有一局，还有没结束的一局时要先把它玩完。
func StartMines(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Mines
	if !requireGameEnabled(c, gameMines) {
		return
	}
	var req minesStartRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, rule.MaxBet) {
		return
	}
	if req.Mines < mines.MinMines || req.Mines > mines.MaxMines {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	layout, err := placeMines(req.Mines)
	if err != nil {
		respondGameError(c, gameMines, err)
		return
	}
	// 返还率记在对局里：管理员中途改了配置，已经开局的这一局也按开局时的倍数兑现。
	round := mines.NewRound(rule.RtpPercent, layout)
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session, balance, err := model.OpenGameRound(c.GetInt("id"), gameMines, model.GameRound{
		Bet:   req.Bet,
		State: string(state),
		Ip:    gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameMines, err)
		return
	}
	common.ApiSuccess(c, gin.H{"round": minesRoundView(session, &round), "balance": balance})
}

// PickMines 翻开一格：踩雷就输掉下注、揭示全部雷；是安全格就记下，翻开所有安全格或派彩到了封顶时自动兑现。
func PickMines(c *gin.Context) {
	var req minesPickRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var round mines.Round
	session := loadWagerRound(c, gameMines, req.wagerRoundRequest, &round)
	if session == nil {
		return
	}
	mine, err := round.Pick(req.Index)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	switch {
	case mine:
		settleMines(c, session, &round, 0)
		return
	case round.AutoCashout(int(session.Bet)):
		settleMines(c, session, &round, model.GameCoins(round.Cashout(int(session.Bet))))
		return
	}
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
		respondGameError(c, gameMines, err)
		return
	}
	common.ApiSuccess(c, gin.H{"round": minesRoundView(session, &round)})
}

// CashoutMines 兑现：按翻开的安全格数派彩。至少翻开一格才能兑现。
func CashoutMines(c *gin.Context) {
	var req wagerRoundRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var round mines.Round
	session := loadWagerRound(c, gameMines, req, &round)
	if session == nil {
		return
	}
	if len(round.Revealed) == 0 {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	settleMines(c, session, &round, model.GameCoins(round.Cashout(int(session.Bet))))
}

// settleMines 结束这一局挖宝、派彩 win，回应结束的一局与结算后的余额、今天的输赢与局数。
func settleMines(c *gin.Context, session *model.GameSession, round *mines.Round, win model.GameCoins) {
	state, err := common.Marshal(round)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	detail, err := common.Marshal(minesDetail{Mines: round.Mines, Revealed: len(round.Revealed), Lost: round.Lost()})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SettleGameRound(session, model.GameRoundSettlement{Win: win, State: string(state), Detail: string(detail), Day: gameCenterDay()})
	if err != nil {
		respondGameError(c, gameMines, err)
		return
	}
	auditGameWin(c, gameMines, "挖宝", result.Session)
	common.ApiSuccess(c, gin.H{
		"round":       minesRoundView(result.Session, round),
		"balance":     result.Balance,
		"net_today":   result.Net,
		"plays_today": result.Plays,
	})
}
