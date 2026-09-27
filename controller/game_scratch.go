package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/scratch"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameScratch = "scratch"

// buyScratch 抽一张刮刮乐，奖品与票面来自密码学安全的随机数。测试里替换成固定的票。
var buyScratch = scratch.Buy

func init() {
	registerGame(&gameDefinition{
		Key:  gameScratch,
		Kind: operation_setting.GameKindWager,
		Info: scratchInfo,
		Routes: func(group *gin.RouterGroup) {
			group.POST("/buy", middleware.GameSpinRateLimit("scratch-buy"), BuyScratch)
		},
	})
}

// scratchInfo 返回刮刮乐页的信息：三档票价(open 为假的一档高于管理员设的最高一档，买不了)、三档共用的奖品表(奖品是票价的
// multiple 倍，每张票中这一档的概率是 chance)与理论返还率。浏览器据此公示奖品表，刮开服务端给出的票面。
func scratchInfo(ctx *gameContext) (gin.H, error) {
	rule := ctx.Setting.Scratch
	info, err := wagerInfo(ctx, gameScratch, rule.MaxBet)
	if err != nil {
		return nil, err
	}
	steps := operation_setting.GameBetSteps(gameScratch)
	prices := make([]gin.H, 0, len(steps))
	for _, price := range steps {
		prices = append(prices, gin.H{"price": price, "open": price <= rule.MaxBet})
	}
	prizes := make([]gin.H, 0, len(scratch.Prizes))
	for _, prize := range scratch.Prizes {
		prizes = append(prizes, gin.H{"multiple": prize.Multiple, "chance": float64(prize.Weight) / scratch.TotalWeight})
	}
	paid, total := scratch.Payback()
	info["prices"] = prices
	info["prizes"] = prizes
	info["payback"] = float64(paid) / float64(total)
	return info, nil
}

type scratchBuyRequest struct {
	Price int `json:"price"`
}

// scratchDetail 是一张刮刮乐的明细，存在对局的 Detail 里供事后核查。Cells 是 9 格金额各是票价的几倍。
type scratchDetail struct {
	Price    int                    `json:"price"`
	Multiple int                    `json:"multiple"`
	Cells    [scratch.CellCount]int `json:"cells"`
}

// BuyScratch 买一张刮刮乐：按票价扣币，服务端抽出奖品并生成相符的票面，刮出 3 格相同的金额就派彩这个金额。
// 扣币与派彩在同一个事务里完成，结果在购买时就已确定，浏览器只负责刮开的交互。
func BuyScratch(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Scratch
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req scratchBuyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerStep(c, req.Price, operation_setting.GameBetSteps(gameScratch), rule.MaxBet) {
		return
	}
	var ticket scratch.Ticket
	result, err := model.SettleGameWager(c.GetInt("id"), gameScratch, model.GameWager{
		Bet: req.Price,
		Play: func() (int, string, error) {
			var err error
			if ticket, err = buyScratch(); err != nil {
				return 0, "", err
			}
			detail, err := common.Marshal(scratchDetail{Price: req.Price, Multiple: ticket.Multiple, Cells: ticket.Cells})
			return req.Price * ticket.Multiple, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	if err != nil {
		respondGameError(c, gameScratch, err)
		return
	}
	auditGameWin(c, gameScratch, "刮刮乐", result.Session)
	var cells [scratch.CellCount]int
	for cell, multiple := range ticket.Cells {
		cells[cell] = req.Price * multiple
	}
	view := wagerResultView(result)
	view["price"] = req.Price
	view["cells"] = cells
	view["prize"] = result.Session.Reward
	common.ApiSuccess(c, view)
}
