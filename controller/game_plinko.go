package controller

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/plinko"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gamePlinko = "plinko"

// plinkoAuditMultiplier 是记审计日志的派彩倍数，与页面上按大奖展示的倍数一致。
const plinkoAuditMultiplier = 10

// dropPlinko 让小球落下一次，每一排往哪边偏来自密码学安全的随机数。测试里替换成固定的路径。
var dropPlinko = plinko.Drop

type plinkoDropRequest struct {
	Stake int `json:"stake"`
}

// plinkoDetail 是一颗球的明细，存在对局的 Detail 里供事后核查。
type plinkoDetail struct {
	Stake  int              `json:"stake"`
	Cost   int              `json:"cost"`
	Path   [plinko.Rows]int `json:"path"`
	Slot   int              `json:"slot"`
	Payout int              `json:"payout"`
}

// plinkoCounter 是 Plinko 某一档当天局数与输赢计数用的键：三档各自限制每日次数，分开计数。
func plinkoCounter(stake int) string {
	return gamePlinko + "_" + strconv.Itoa(stake)
}

// DropPlinko 投一颗球：服务端定下小球在每一排往哪边偏，落进哪一格就按这一档的派彩表结算，扣币与派彩在同一个事务里完成，
// 浏览器只照着返回的路径播放动画。三档分别限制每人每天投几颗，0 表示这一档不开放。理论返还率约 80%，
// 脚本连续投只会输得更快，不需要人机验证。
func DropPlinko(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Plinko
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req plinkoDropRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	tier, ok := plinko.TierOf(req.Stake)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgGamePlinkoStakeInvalid)
		return
	}
	limit := rule.DailyDrops(tier.Stake)
	if limit == 0 {
		common.ApiErrorI18n(c, i18n.MsgGamePlinkoTierClosed)
		return
	}
	userId := c.GetInt("id")
	var path [plinko.Rows]int
	var slot int
	settled, err := model.SettleGameDailyPlay(userId, gamePlinko, model.GameDailyPlay{
		Cost:      tier.Cost,
		PlayLimit: limit,
		Counter:   plinkoCounter(tier.Stake),
		Play: func() (int, string, error) {
			var err error
			if path, err = dropPlinko(); err != nil {
				return 0, "", err
			}
			slot = plinko.SlotOf(path)
			payout := tier.Payouts[slot]
			detail, err := common.Marshal(plinkoDetail{Stake: tier.Stake, Cost: tier.Cost, Path: path, Slot: slot, Payout: payout})
			return payout, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	switch {
	case errors.Is(err, model.ErrGameDailyPlayLimit):
		common.ApiErrorI18n(c, i18n.MsgGameDailyPlayLimit)
		return
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		return
	case err != nil:
		common.SysError(fmt.Sprintf("failed to settle plinko drop for user %d: %v", userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameDropFailed)
		return
	}
	payout := settled.Session.Reward
	// 大奖记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号从 Plinko 拿走了游戏币。
	if payout >= plinkoAuditMultiplier*tier.Cost {
		model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
			fmt.Sprintf("游戏大奖：Plinko %d 币档扣 %d，落进第 %d 格，派彩 %d 个游戏币", tier.Stake, tier.Cost, slot+1, payout),
			map[string]interface{}{"game": gamePlinko, "session_id": settled.Session.Id, "stake": tier.Stake, "cost": tier.Cost, "slot": slot, "game_coins": payout})
	}
	common.ApiSuccess(c, gin.H{
		"stake":        tier.Stake,
		"path":         path,
		"slot":         slot,
		"payout":       payout,
		"net":          payout - tier.Cost,
		"balance":      settled.Balance,
		"plays_today":  settled.Plays,
		"earned_today": settled.Earned,
		"daily_drops":  limit,
	})
}
