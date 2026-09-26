package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wheel"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameWheel = "wheel"

// wheelAuditMultiplier 是记审计日志的派彩倍数，与页面上按大奖展示的倍数一致。
const wheelAuditMultiplier = 10

// spinWheel 转一次大转盘，按权重选中的格子来自密码学安全的随机数。测试里替换成固定的格子。
var spinWheel = wheel.Spin

type wheelSpinRequest struct {
	Free bool `json:"free"`
}

// wheelDetail 是大转盘一次旋转的明细，存在对局的 Detail 里供事后核查。Cost 是每次花费：免费旋转不扣币，奖品同样按它计算。
type wheelDetail struct {
	Free       bool `json:"free"`
	Cost       int  `json:"cost"`
	Segment    int  `json:"segment"`
	Multiplier int  `json:"multiplier"`
}

// SpinWheel 转一次大转盘：服务端按权重选出一格，派彩 = 每次花费 × 这一格的倍数，扣币与派彩在同一个事务里完成，
// 浏览器只负责把转盘转到返回的格子。请求写明这一次是不是用免费次数，服务端不会把玩家以为免费的旋转改成扣币。
// 免费次数白送游戏币，同一 IP 下所有账号合计，每次都和俄罗斯方块开局一样要过人机验证；付费旋转的理论返还率 80%，
// 脚本连续旋转只会输得更快，不需要验证。每人每天的次数含免费次数。
func SpinWheel(c *gin.Context) {
	setting := operation_setting.GetGameSetting()
	rule := setting.Wheel
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req wheelSpinRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	if req.Free && !passGamePlayTurnstile(c, setting, userId) {
		return
	}
	cost := rule.SpinCost
	var segment int
	settled, err := model.SettleGameDailyPlay(userId, gameWheel, model.GameDailyPlay{
		Free:      req.Free,
		Cost:      cost,
		FreeLimit: rule.FreeSpins,
		PlayLimit: rule.DailySpins,
		Play: func() (int, string, error) {
			var err error
			if segment, err = spinWheel(); err != nil {
				return 0, "", err
			}
			multiplier := wheel.Segments[segment].Multiplier
			detail, err := common.Marshal(wheelDetail{Free: req.Free, Cost: cost, Segment: segment, Multiplier: multiplier})
			return cost * multiplier, string(detail), err
		},
		Day: gameCenterDay(),
		Ip:  gameRewardIpKey(c.ClientIP()),
	})
	switch {
	case errors.Is(err, model.ErrGameDailyPlayLimit):
		common.ApiErrorI18n(c, i18n.MsgGameDailyPlayLimit)
		return
	case errors.Is(err, model.ErrGameFreePlayUsed):
		common.ApiErrorI18n(c, i18n.MsgGameFreePlayUsed)
		return
	case errors.Is(err, model.ErrGameIpFreePlayUsed):
		common.ApiErrorI18n(c, i18n.MsgGameIpFreePlayUsed)
		return
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		return
	case err != nil:
		common.SysError(fmt.Sprintf("failed to settle wheel spin for user %d: %v", userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameSpinFailed)
		return
	}
	multiplier := wheel.Segments[segment].Multiplier
	// 大奖记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号从转盘拿走了游戏币。
	if multiplier >= wheelAuditMultiplier {
		spent := fmt.Sprintf("花费 %d", cost)
		if req.Free {
			spent = "免费旋转"
		}
		model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
			fmt.Sprintf("游戏大奖：大转盘%s，%d 倍，派彩 %d 个游戏币", spent, multiplier, settled.Session.Reward),
			map[string]interface{}{"game": gameWheel, "session_id": settled.Session.Id, "free": req.Free, "cost": cost, "multiplier": multiplier, "game_coins": settled.Session.Reward})
	}
	common.ApiSuccess(c, gin.H{
		"segment":            segment,
		"multiplier":         multiplier,
		"free":               req.Free,
		"win":                settled.Session.Reward,
		"balance":            settled.Balance,
		"plays_today":        settled.Plays,
		"free_used_today":    settled.FreePlays,
		"ip_free_used_today": settled.IpFreePlays,
		"earned_today":       settled.Earned,
		"free_spins":         rule.FreeSpins,
		"daily_spins":        rule.DailySpins,
		"spin_cost":          rule.SpinCost,
	})
}
