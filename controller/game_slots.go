package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/slots"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameSlots = "slots"

// spinSlots 转一次老虎机，停点来自密码学安全的随机数。测试里替换成固定的停点。
var spinSlots = slots.Spin

func init() {
	registerGame(&gameDefinition{
		Key:  gameSlots,
		Kind: operation_setting.GameKindWager,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := wagerInfo(ctx, gameSlots, ctx.Setting.Slots.MaxBet)
			if err != nil {
				return nil, err
			}
			// 转轮、赔付表与返还率随游戏信息下发，浏览器据此画出转轮、公示规则，把转轮转到服务端给出的停点。
			paid, combos := slots.Payback()
			info["reels"] = slots.Strips
			info["paytable"] = slots.Paytable
			info["payback"] = float64(paid) / float64(combos)
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/spin", middleware.GameSpinRateLimit("slots-spin"), SpinSlots)
		},
	})
}

type slotsSpinRequest struct {
	Bet int `json:"bet"`
}

// slotsDetail 是老虎机一局的明细，存在对局的 Detail 里供事后核查。
type slotsDetail struct {
	Stops      [3]int `json:"stops"`
	Multiplier int    `json:"multiplier"`
}

// SpinSlots 转一次老虎机：服务端决定三个转轮的停点并按赔付表算出派彩，扣注与派彩在同一个事务里完成，
// 浏览器只负责把转轮转到返回的停点。
func SpinSlots(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Slots
	if !requireGameEnabled(c, gameSlots) {
		return
	}
	var req slotsSpinRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Bet, 1, rule.MaxBet) {
		return
	}
	var spin slots.Result
	result, err := model.SettleGameWager(c.GetInt("id"), gameSlots, model.GameWager{
		Bet: req.Bet,
		Play: func() (int, string, error) {
			var err error
			if spin, err = spinSlots(); err != nil {
				return 0, "", err
			}
			detail, err := common.Marshal(slotsDetail{Stops: spin.Stops, Multiplier: spin.Multiplier})
			return req.Bet * spin.Multiplier, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameSlots, err)
		return
	}
	auditGameWin(c, gameSlots, "老虎机", result.Session)
	view := wagerResultView(result)
	view["stops"] = spin.Stops
	view["line"] = spin.Line
	view["multiplier"] = spin.Multiplier
	view["matched"] = spin.Matched
	common.ApiSuccess(c, view)
}
