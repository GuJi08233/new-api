package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/plinko"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gamePlinko = "plinko"

// dropPlinko 让小球落下一次，每一排往哪边偏来自密码学安全的随机数。测试里替换成固定的路径。
var dropPlinko = plinko.Drop

func init() {
	registerGame(&gameDefinition{
		Key:  gamePlinko,
		Kind: operation_setting.GameKindWager,
		Info: plinkoInfo,
		Routes: func(group *gin.RouterGroup) {
			group.POST("/drop", middleware.GameSpinRateLimit("plinko-drop"), DropPlinko)
		},
	})
}

// plinkoInfo 返回 Plinko 页的信息：每一档扣 cost(就是挡位 stake)个游戏币、落进第 k 格派彩 payouts[k] 个(挡位乘以这一格的倍率，
// 可以有小数)，落进第 k 格的概率是 weights[k] 除以 weights 之和；open 为假的一档没有开放(挡位高于管理员设的最高一档)。浏览器据此
// 画出钉板、公示概率，照着服务端给出的路径播放小球落下的动画。
func plinkoInfo(ctx *gameContext) (gin.H, error) {
	rule := ctx.Setting.Plinko
	info, err := wagerInfo(ctx, gamePlinko, rule.MaxBet)
	if err != nil {
		return nil, err
	}
	paid, cost := plinko.Payback()
	tiers := make([]gin.H, 0, len(plinko.Stakes))
	for _, stake := range plinko.Stakes {
		units, err := model.WholeGameCoins(stake)
		if err != nil {
			return nil, err
		}
		var payouts [plinko.Slots]model.GameCoins
		for slot := range payouts {
			payouts[slot] = model.GameCoins(plinko.Payout(int(units), slot))
		}
		tiers = append(tiers, gin.H{
			"stake":   stake,
			"cost":    stake,
			"payouts": payouts,
			"payback": float64(paid) / float64(cost),
			"open":    stake <= rule.MaxBet,
		})
	}
	info["rows"] = plinko.Rows
	info["weights"] = plinko.Weights
	info["tiers"] = tiers
	return info, nil
}

type plinkoDropRequest struct {
	Stake int `json:"stake"`
}

// plinkoDetail 是一颗球的明细，存在对局的 Detail 里供事后核查。
type plinkoDetail struct {
	Stake int              `json:"stake"`
	Path  [plinko.Rows]int `json:"path"`
	Slot  int              `json:"slot"`
}

// DropPlinko 投一颗球：服务端定下小球在每一排往哪边偏，落进哪一格就按这一档的派彩表结算，扣币与派彩在同一个事务里完成，
// 浏览器只照着返回的路径播放动画。
func DropPlinko(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Plinko
	if !requireGameEnabled(c, gamePlinko) {
		return
	}
	var req plinkoDropRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	bet, ok := validWagerStep(c, req.Stake, operation_setting.GameBetSteps(gamePlinko), rule.MaxBet)
	if !ok {
		return
	}
	var path [plinko.Rows]int
	var slot int
	result, err := model.SettleGameWager(c.GetInt("id"), gamePlinko, model.GameWager{
		Bet: bet,
		Play: func() (model.GameCoins, string, error) {
			var err error
			if path, err = dropPlinko(); err != nil {
				return 0, "", err
			}
			slot = plinko.SlotOf(path)
			detail, err := common.Marshal(plinkoDetail{Stake: req.Stake, Path: path, Slot: slot})
			return model.GameCoins(plinko.Payout(int(bet), slot)), string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gamePlinko, err)
		return
	}
	auditGameWin(c, gamePlinko, "Plinko", result.Session)
	view := wagerResultView(result)
	view["stake"] = req.Stake
	view["path"] = path
	view["slot"] = slot
	common.ApiSuccess(c, view)
}
