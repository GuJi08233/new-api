package controller

import (
	"errors"
	"fmt"
	"strconv"

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

type minesStartRequest struct {
	Bet   int `json:"bet"`
	Mines int `json:"mines"`
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
	cashout := 0
	if len(round.Revealed) > 0 {
		cashout = round.Cashout(session.Bet)
	}
	view["cashout"] = cashout
	view["next_cashout"] = round.NextCashout(session.Bet)
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
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req minesStartRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, 1, rule.MaxBet) {
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
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
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
	case round.AutoCashout(session.Bet):
		settleMines(c, session, &round, round.Cashout(session.Bet))
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
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
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
	settleMines(c, session, &round, round.Cashout(session.Bet))
}

// settleMines 结束这一局挖宝、派彩 win，回应结束的一局与结算后的余额、今天的输赢与局数。
func settleMines(c *gin.Context, session *model.GameSession, round *mines.Round, win int) {
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
	result, err := model.SettleGameRound(session, win, string(state), string(detail), gameCenterDay())
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
