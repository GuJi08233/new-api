package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/horserace"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameHorserace = "horserace"

// runHorserace 跑一场赛马，名次来自密码学安全的随机数。测试里替换成固定的名次。
var runHorserace = horserace.Race

func init() {
	registerGame(&gameDefinition{
		Key:  gameHorserace,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameHorserace, ctx.Setting.Horserace.MaxBet)
			if err != nil {
				return nil, err
			}
			// 每匹马的实力、两种玩法的赔率、中奖概率与理论返还率随游戏信息下发，浏览器据此公示规则。
			horses := make([]gin.H, horserace.Horses)
			for horse := range horses {
				horses[horse] = gin.H{
					"strength":      horserace.Strength[horse],
					"win_odds":      horserace.Odds[horserace.BetWin][horse],
					"place_odds":    horserace.Odds[horserace.BetPlace][horse],
					"win_chance":    horserace.WinChance(horse),
					"place_chance":  horserace.PlaceChance(horse),
					"win_payback":   horserace.Payback(horserace.BetWin, horse),
					"place_payback": horserace.Payback(horserace.BetPlace, horse),
				}
			}
			info["horses"] = horses
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/race", middleware.GameSpinRateLimit("horserace-race"), RunHorserace)
		},
	})
}

type horseraceBetRequest struct {
	Kind   string          `json:"kind"`
	Horse  int             `json:"horse"`
	Amount model.GameCoins `json:"amount"`
}

type horseraceRaceRequest struct {
	Bets []horseraceBetRequest `json:"bets"`
}

// horseraceBetView 是一注的结算：押的玩法、马、金额与派彩(含本金，没中为 0)。
type horseraceBetView struct {
	Kind   string          `json:"kind"`
	Horse  int             `json:"horse"`
	Amount model.GameCoins `json:"amount"`
	Win    model.GameCoins `json:"win"`
}

// horseraceDetail 是一场赛马的明细，存在对局的 Detail 里供事后核查。
type horseraceDetail struct {
	Order []int              `json:"order"`
	Bets  []horseraceBetView `json:"bets"`
}

// RunHorserace 跑一场赛马：一次可以押好几注(同一匹马的同一种玩法只能押一次)，各注金额之和是这一场的下注。服务端抽出名次
// 并按每一注的赔率结算，扣注与派彩在同一个事务里完成，浏览器只负责把比赛演出来。
func RunHorserace(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Horserace
	if !requireGameEnabled(c, gameHorserace) {
		return
	}
	var req horseraceRaceRequest
	if err := decodeGameRequest(c, &req); err != nil || len(req.Bets) < 1 || len(req.Bets) > 2*horserace.Horses {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 每一注先检查玩法、马与金额再累加：每注都不超过单次下注上限，合计同样不能超过上限。
	stake := model.GameCoins(0)
	seen := make(map[horseraceBetRequest]bool, len(req.Bets))
	for _, bet := range req.Bets {
		key := horseraceBetRequest{Kind: bet.Kind, Horse: bet.Horse}
		if !horserace.ValidBet(bet.Kind, bet.Horse) || seen[key] {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		seen[key] = true
		if !validWagerBet(c, bet.Amount, rule.MaxBet) {
			return
		}
		stake += bet.Amount
	}
	if !validWagerBet(c, stake, rule.MaxBet) {
		return
	}
	var order []int
	views := make([]horseraceBetView, len(req.Bets))
	result, err := model.SettleGameWager(c.GetInt("id"), gameHorserace, model.GameWager{
		Bet: stake,
		Play: func() (model.GameCoins, string, error) {
			var err error
			if order, err = runHorserace(); err != nil {
				return 0, "", err
			}
			win := model.GameCoins(0)
			for i, bet := range req.Bets {
				multiplier := horserace.Multiplier(bet.Kind, bet.Horse, order)
				views[i] = horseraceBetView{Kind: bet.Kind, Horse: bet.Horse, Amount: bet.Amount, Win: bet.Amount * model.GameCoins(multiplier) / 100}
				win += views[i].Win
			}
			detail, err := common.Marshal(horseraceDetail{Order: order, Bets: views})
			return win, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameHorserace, err)
		return
	}
	auditGameWin(c, gameHorserace, "赛马", result.Session)
	view := wagerResultView(result)
	view["order"] = order
	view["bets"] = views
	common.ApiSuccess(c, view)
}
