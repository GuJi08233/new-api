package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/roulette"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameRoulette = "roulette"

// rouletteSpinMaxBytes 限制旋转请求体：roulette.MaxBets 注的注单不到 3 KB，更大的请求在解析前就拒绝。
const rouletteSpinMaxBytes = 16 << 10

// spinRoulette 转一次轮盘，开出的号码来自密码学安全的随机数。测试里替换成固定的号码。
var spinRoulette = roulette.Spin

func init() {
	registerGame(&gameDefinition{
		Key:  gameRoulette,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameRoulette, ctx.Setting.Roulette.MaxBet)
			if err != nil {
				return nil, err
			}
			// 赔率、红色号码与返还率随游戏信息下发，浏览器据此画出桌面、公示规则，把轮盘转到服务端开出的号码。
			info["payouts"] = roulette.Payouts
			info["red_numbers"] = roulette.RedNumbers
			info["max_bets"] = roulette.MaxBets
			info["payback"] = roulette.Payback
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/spin", middleware.GameSpinRateLimit("roulette-spin"), SpinRoulette)
		},
	})
}

// rouletteBetRequest 是注单上的一注，金额可以有两位小数。
type rouletteBetRequest struct {
	Type   string          `json:"type"`
	Value  int             `json:"value"`
	Amount model.GameCoins `json:"amount"`
}

type rouletteSpinRequest struct {
	Bets []rouletteBetRequest `json:"bets"`
}

// rouletteBetView 是一注的结算：这一注本身与它的派彩(含本金，没中为 0)。
type rouletteBetView struct {
	Type   string          `json:"type"`
	Value  int             `json:"value"`
	Amount model.GameCoins `json:"amount"`
	Win    model.GameCoins `json:"win"`
}

// rouletteDetail 是轮盘一局的明细，存在对局的 Detail 里供事后核查。Detail 最长 255 字节，放不下 50 注的注单，
// 只记开出的号码与注数，整张注单在对局的 State 里；合计下注与派彩在对局的 bet、reward 上。
type rouletteDetail struct {
	Number int `json:"number"`
	Bets   int `json:"bets"`
}

// SpinRoulette 转一次轮盘：一次可以下多注，各注金额之和是这一局的下注；服务端开出号码，每一注按赔率结算，派彩是所有中了的
// 注的派彩之和。扣注与派彩在同一个事务里完成，浏览器只负责把轮盘转到返回的号码。
func SpinRoulette(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Roulette
	if !requireGameEnabled(c, gameRoulette) {
		return
	}
	var req rouletteSpinRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, rouletteSpinMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(req.Bets) < 1 || len(req.Bets) > roulette.MaxBets {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 每一注先检查注型、号码与金额再累加：金额都不超过单次下注上限，50 注加起来不会溢出，合计同样不能超过上限。
	// 注单按额度单位换成规则里的注，派彩按额度单位算。
	stake := model.GameCoins(0)
	bets := make([]roulette.Bet, len(req.Bets))
	for i, bet := range req.Bets {
		bets[i] = roulette.Bet{Type: bet.Type, Value: bet.Value, Amount: int(bet.Amount)}
		if !bets[i].Valid() {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		if !validWagerBet(c, bet.Amount, rule.MaxBet) {
			return
		}
		stake += bet.Amount
	}
	if !validWagerBet(c, stake, rule.MaxBet) {
		return
	}
	// 整张注单存进对局的状态，明细里只放得下开出的号码与注数。
	slip, err := common.Marshal(bets)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var number int
	views := make([]rouletteBetView, len(bets))
	result, err := model.SettleGameWager(c.GetInt("id"), gameRoulette, model.GameWager{
		Bet:   stake,
		State: string(slip),
		Play: func() (model.GameCoins, string, error) {
			var err error
			if number, err = spinRoulette(); err != nil {
				return 0, "", err
			}
			win := model.GameCoins(0)
			for i, bet := range bets {
				views[i] = rouletteBetView{Type: bet.Type, Value: bet.Value, Amount: model.GameCoins(bet.Amount), Win: model.GameCoins(bet.Win(number))}
				win += views[i].Win
			}
			detail, err := common.Marshal(rouletteDetail{Number: number, Bets: len(req.Bets)})
			return win, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameRoulette, err)
		return
	}
	auditGameWin(c, gameRoulette, "轮盘", result.Session)
	view := wagerResultView(result)
	view["number"] = number
	view["color"] = roulette.Color(number)
	view["bets"] = views
	common.ApiSuccess(c, view)
}
