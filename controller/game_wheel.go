package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wheel"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"time"

	"github.com/gin-gonic/gin"
)

const gameWheel = "wheel"

// spinWheel 转一次大转盘，按权重选中的格子来自密码学安全的随机数。测试里替换成固定的格子。
var spinWheel = wheel.Spin

func init() {
	registerGame(&gameDefinition{
		Key:  gameWheel,
		Kind: operation_setting.GameKindWager,
		Info: wheelInfo,
		Summarize: func(ctx *gameContext, summary gin.H, counts gameDayCounts) {
			summary["free_spins"] = ctx.Setting.Wheel.FreeSpins
			summary["free_used_today"] = max(counts.user[gameWheel].FreePlays, counts.ip[gameWheel].FreePlays)
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/spin", middleware.GameSpinRateLimit("wheel-spin"), SpinWheel)
		},
	})
}

// wheelInfo 返回大转盘页的信息。net_today 是账号今天的净得：免费旋转的奖品加上付费旋转的净输赢，可以为负；
// free_used_today、ip_free_used_today 是账号、同一 IP 今天用掉的免费次数。格子与返还率随游戏信息下发，浏览器据此画出转盘、
// 公示概率，把转盘转到服务端选中的格子。
func wheelInfo(ctx *gameContext) (gin.H, error) {
	rule := ctx.Setting.Wheel
	info, err := wagerInfo(ctx, gameWheel, rule.SpinCost)
	if err != nil {
		return nil, err
	}
	progress, err := model.GetGameRewardProgress(ctx.UserId, ctx.Ip, gameWheel, ctx.Day)
	if err != nil {
		return nil, err
	}
	paid, total := wheel.Payback()
	info["free_spins"] = rule.FreeSpins
	info["spin_cost"] = rule.SpinCost
	info["free_used_today"] = progress.Rewards
	info["ip_free_used_today"] = progress.IpRewards
	info["segments"] = wheel.Segments
	info["payback"] = float64(paid) / float64(total)
	start, err := time.ParseInLocation("2006-01-02", ctx.Day, time.Local)
	if err != nil {
		return nil, err
	}
	info["day"] = ctx.Day
	info["server_time"] = common.GetTimestamp()
	info["day_ends_at"] = start.AddDate(0, 0, 1).Unix()
	return info, nil
}

type wheelSpinRequest struct {
	Free bool   `json:"free"`
	Day  string `json:"day"`
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
// 免费旋转的奖品全额发放，不受技巧游戏与人机对战的奖励总上限影响，也不占用该上限；同一 IP 下所有账号合计次数，按游戏配置要求验证；
// 付费旋转按下注游戏结算，理论返还率 80%，不限次数。
func SpinWheel(c *gin.Context) {
	setting := operation_setting.GetGameSetting()
	rule := setting.Wheel
	if !requireGameEnabled(c, gameWheel) {
		return
	}
	var req wheelSpinRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	if req.Day != "" && req.Day != gameCenterDay() {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	if req.Free && !passGamePlayTurnstile(c, setting, gameWheel, userId) {
		return
	}
	cost, err := model.WholeGameCoins(rule.SpinCost)
	if err != nil {
		respondGameError(c, gameWheel, err)
		return
	}
	var segment int
	play := func() (model.GameCoins, string, error) {
		var err error
		if segment, err = spinWheel(); err != nil {
			return 0, "", err
		}
		multiplier := wheel.Segments[segment].Multiplier
		detail, err := common.Marshal(wheelDetail{Free: req.Free, Cost: rule.SpinCost, Segment: segment, Multiplier: multiplier})
		return cost * model.GameCoins(multiplier), string(detail), err
	}
	day, ip := gameCenterDay(), gameRewardIpKey(c.ClientIP())
	var view gin.H
	if req.Free {
		result, err := model.SettleGameFreePlay(userId, gameWheel, model.GameFreePlay{
			FreeLimit: rule.FreeSpins,
			Play:      play,
			Day:       day,
			Ip:        ip,
		})
		if err != nil {
			respondGameError(c, gameWheel, err)
			return
		}
		view = gin.H{
			"win":                result.Session.Reward,
			"limited_by":         result.Session.LimitedBy,
			"balance":            result.Balance,
			"net_today":          result.Earned,
			"plays_today":        result.Plays,
			"free_used_today":    result.Rewards,
			"ip_free_used_today": result.IpRewards,
		}
		auditGameFreeWin(c, gameWheel, "大转盘免费旋转", result.Session)
	} else {
		result, err := model.SettleGameWager(userId, gameWheel, model.GameWager{Bet: cost, Play: play, Day: day, Ip: ip})
		if err != nil {
			respondGameError(c, gameWheel, err)
			return
		}
		view = wagerResultView(result)
		auditGameWin(c, gameWheel, "大转盘", result.Session)
	}
	// 统计与派彩取同一次结算的结果，提交后的查询失败不能把已成功的旋转变成失败响应。
	view["free"] = req.Free
	view["day"] = day
	view["segment"] = segment
	view["multiplier"] = wheel.Segments[segment].Multiplier
	common.ApiSuccess(c, view)
}
