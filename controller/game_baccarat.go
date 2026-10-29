package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/baccarat"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameBaccarat = "baccarat"

// playBaccarat 发一局百家乐，牌来自密码学安全的随机数。测试里替换成固定的牌局。
var playBaccarat = baccarat.Play

func init() {
	registerGame(&gameDefinition{
		Key:  gameBaccarat,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameBaccarat, ctx.Setting.Baccarat.MaxBet)
			if err != nil {
				return nil, err
			}
			// 押注区、赔率与各处的理论返还率随游戏信息下发，浏览器据此画桌面、公示规则。
			info["spots"] = baccarat.Spots
			info["payouts"] = baccarat.Payouts
			info["payback"] = baccarat.Payback
			info["decks"] = baccarat.Decks
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/deal", middleware.GameSpinRateLimit("baccarat-deal"), DealBaccarat)
		},
	})
}

type baccaratBetRequest struct {
	Spot   string          `json:"spot"`
	Amount model.GameCoins `json:"amount"`
}

type baccaratDealRequest struct {
	Bets []baccaratBetRequest `json:"bets"`
}

// baccaratBetView 是一注的结算：押的位置、金额与派彩(含本金，没中为 0，和局时押闲、押庄退回本金)。
type baccaratBetView struct {
	Spot   string          `json:"spot"`
	Amount model.GameCoins `json:"amount"`
	Win    model.GameCoins `json:"win"`
}

// baccaratDetail 是百家乐一局的明细：闲、庄的点数与赢家，存在对局的 Detail 里供事后核查(整局的牌在对局状态里)。
type baccaratDetail struct {
	Player int    `json:"player"`
	Banker int    `json:"banker"`
	Winner string `json:"winner"`
}

// DealBaccarat 发一局百家乐：一次可以押好几处，各处金额之和是这一局的下注，同一处只能押一次。服务端发牌并按每一处的赔率
// 结算，扣注与派彩在同一个事务里完成，浏览器只负责按顺序翻开发出的牌。
func DealBaccarat(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Baccarat
	if !requireGameEnabled(c, gameBaccarat) {
		return
	}
	var req baccaratDealRequest
	if err := decodeGameRequest(c, &req); err != nil || len(req.Bets) < 1 || len(req.Bets) > len(baccarat.Spots) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 每一注先检查位置与金额再累加：每注都不超过单次下注上限，合计同样不能超过上限。
	stake := model.GameCoins(0)
	seen := make(map[string]bool, len(req.Bets))
	for _, bet := range req.Bets {
		if !baccarat.ValidSpot(bet.Spot) || seen[bet.Spot] {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		seen[bet.Spot] = true
		if !validWagerBet(c, bet.Amount, rule.MaxBet) {
			return
		}
		stake += bet.Amount
	}
	if !validWagerBet(c, stake, rule.MaxBet) {
		return
	}
	var round baccarat.Result
	views := make([]baccaratBetView, len(req.Bets))
	result, err := model.SettleGameWager(c.GetInt("id"), gameBaccarat, model.GameWager{
		Bet: stake,
		Play: func() (model.GameCoins, string, error) {
			var err error
			if round, err = playBaccarat(); err != nil {
				return 0, "", err
			}
			win := model.GameCoins(0)
			for i, bet := range req.Bets {
				views[i] = baccaratBetView{Spot: bet.Spot, Amount: bet.Amount, Win: model.GameCoins(round.Win(bet.Spot, int(bet.Amount)))}
				win += views[i].Win
			}
			detail, err := common.Marshal(baccaratDetail{Player: round.Player.Total, Banker: round.Banker.Total, Winner: round.Winner})
			return win, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameBaccarat, err)
		return
	}
	auditGameWin(c, gameBaccarat, "百家乐", result.Session)
	view := wagerResultView(result)
	view["round"] = round
	view["bets"] = views
	common.ApiSuccess(c, view)
}
