package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/keno"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameKeno = "keno"

// drawKeno 开出一局基诺的号码，来自密码学安全的随机数。测试里替换成固定的号码。
var drawKeno = keno.Draw

func init() {
	registerGame(&gameDefinition{
		Key:  gameKeno,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameKeno, ctx.Setting.Keno.MaxBet)
			if err != nil {
				return nil, err
			}
			// 赔付表、每行的理论返还率与各中奖个数的概率随游戏信息下发，浏览器据此公示规则。
			payback := make([]float64, keno.MaxPicks+1)
			chances := make([][]float64, keno.MaxPicks+1)
			for picks := 1; picks <= keno.MaxPicks; picks++ {
				payback[picks] = keno.Payback(picks)
				chances[picks] = make([]float64, picks+1)
				for hits := 0; hits <= picks; hits++ {
					chances[picks][hits] = keno.HitChance(picks, hits)
				}
			}
			info["numbers"] = keno.Numbers
			info["draws"] = keno.Draws
			info["max_picks"] = keno.MaxPicks
			info["paytable"] = keno.Paytable
			info["payback"] = payback
			info["chances"] = chances
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/play", middleware.GameSpinRateLimit("keno-play"), PlayKeno)
		},
	})
}

type kenoPlayRequest struct {
	Bet   model.GameCoins `json:"bet"`
	Picks []int           `json:"picks"`
}

// kenoDetail 是基诺一局的明细，存在对局的 Detail 里供事后核查。
type kenoDetail struct {
	Picks []int `json:"picks"`
	Drawn []int `json:"drawn"`
	Hits  int   `json:"hits"`
}

// PlayKeno 开一局基诺：玩家选好号码与下注，服务端开出 keno.Draws 个号码，按选中的个数查赔付表，派彩 = 下注 × 倍数。
// 扣注与派彩在同一个事务里完成，浏览器只负责一个个翻开开出的号码。
func PlayKeno(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Keno
	if !requireGameEnabled(c, gameKeno) {
		return
	}
	var req kenoPlayRequest
	if err := decodeGameRequest(c, &req); err != nil || !keno.ValidPicks(req.Picks) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, rule.MaxBet) {
		return
	}
	var drawn []int
	var hits, multiplier int
	result, err := model.SettleGameWager(c.GetInt("id"), gameKeno, model.GameWager{
		Bet: req.Bet,
		Play: func() (model.GameCoins, string, error) {
			var err error
			if drawn, err = drawKeno(); err != nil {
				return 0, "", err
			}
			hits = keno.Hits(req.Picks, drawn)
			multiplier = keno.Multiplier(len(req.Picks), hits)
			detail, err := common.Marshal(kenoDetail{Picks: req.Picks, Drawn: drawn, Hits: hits})
			return req.Bet * model.GameCoins(multiplier) / 100, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameKeno, err)
		return
	}
	auditGameWin(c, gameKeno, "基诺", result.Session)
	view := wagerResultView(result)
	view["drawn"] = drawn
	view["hits"] = hits
	view["multiplier"] = multiplier
	common.ApiSuccess(c, view)
}
