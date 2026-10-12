package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dice"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameDice = "dice"

// rollDice 掷一次骰子，点数来自密码学安全的随机数。测试里替换成固定的点数。
var rollDice = dice.Roll

func init() {
	registerGame(&gameDefinition{
		Key:  gameDice,
		Kind: operation_setting.GameKindWager,
		Info: diceInfo,
		Routes: func(group *gin.RouterGroup) {
			group.POST("/roll", middleware.GameSpinRateLimit("dice-roll"), RollDice)
		},
	})
}

// diceInfo 返回骰子页的信息：配置的返还率与每一档倍数的获胜阈值、概率。押 multiplier 倍时，方向 under 掷出小于 threshold 赢，
// 方向 over 掷出不小于 10000 − threshold 赢，两个方向赢的概率都是 chance。浏览器据此公示规则、画出获胜区间。
func diceInfo(ctx *gameContext) (gin.H, error) {
	rule := ctx.Setting.Dice
	info, err := wagerInfo(ctx, gameDice, rule.MaxBet)
	if err != nil {
		return nil, err
	}
	multipliers := make([]gin.H, 0, len(dice.Multipliers))
	for _, multiplier := range dice.Multipliers {
		threshold := dice.Threshold(rule.RtpPercent, multiplier)
		multipliers = append(multipliers, gin.H{
			"multiplier": multiplier,
			"threshold":  threshold,
			"chance":     float64(threshold) / dice.Sides,
		})
	}
	info["rtp_percent"] = rule.RtpPercent
	info["multipliers"] = multipliers
	return info, nil
}

type diceRollRequest struct {
	Bet        model.GameCoins `json:"bet"`
	Multiplier int             `json:"multiplier"`
	Direction  string          `json:"direction"`
}

// diceDetail 是骰子一局的明细，存在对局的 Detail 里供事后核查。
type diceDetail struct {
	Roll       int    `json:"roll"`
	Multiplier int    `json:"multiplier"`
	Direction  string `json:"direction"`
	Threshold  int    `json:"threshold"`
}

// RollDice 掷一次骰子：玩家选好倍数与方向，服务端按配置的返还率算出获胜阈值、掷出点数，赢了派彩 = 下注 × 倍数。
// 扣注与派彩在同一个事务里完成，浏览器只负责播放掷出的点数。
func RollDice(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Dice
	if !requireGameEnabled(c, gameDice) {
		return
	}
	var req diceRollRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, rule.MaxBet) {
		return
	}
	// 倍数直接乘进派彩，只能是公示的几档；方向只能是 under 或 over。
	if !dice.ValidMultiplier(req.Multiplier) || !dice.ValidDirection(req.Direction) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	threshold := dice.Threshold(rule.RtpPercent, req.Multiplier)
	var roll int
	var won bool
	result, err := model.SettleGameWager(c.GetInt("id"), gameDice, model.GameWager{
		Bet: req.Bet,
		Play: func() (model.GameCoins, string, error) {
			var err error
			if roll, err = rollDice(); err != nil {
				return 0, "", err
			}
			won = dice.Wins(roll, threshold, req.Direction)
			win := model.GameCoins(0)
			if won {
				win = req.Bet * model.GameCoins(req.Multiplier)
			}
			detail, err := common.Marshal(diceDetail{Roll: roll, Multiplier: req.Multiplier, Direction: req.Direction, Threshold: threshold})
			return win, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameDice, err)
		return
	}
	auditGameWin(c, gameDice, "骰子", result.Session)
	view := wagerResultView(result)
	view["roll"] = roll
	view["threshold"] = threshold
	view["multiplier"] = req.Multiplier
	view["direction"] = req.Direction
	view["won"] = won
	common.ApiSuccess(c, view)
}
