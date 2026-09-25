package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/slots"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameSlots = "slots"

// slotsAuditMultiplier 是记审计日志的派彩倍数，与页面上按大奖展示的倍数一致。
const slotsAuditMultiplier = 10

// spinSlots 转一次老虎机，停点来自密码学安全的随机数。测试里替换成固定的停点。
var spinSlots = slots.Spin

type slotsSpinRequest struct {
	Bet int `json:"bet"`
}

// slotsDetail 是老虎机一局的明细，存在对局的 Detail 里供事后核查。
type slotsDetail struct {
	Bet        int    `json:"bet"`
	Stage      string `json:"stage"`
	Stops      [3]int `json:"stops"`
	Multiplier int    `json:"multiplier"`
}

// SpinSlots 转一次老虎机：服务端决定三个转轮的停点并按赔付表算出派彩，扣注与派彩在同一个事务里完成，
// 浏览器只负责把转轮转到返回的停点。玩家累计下注满均衡阶段下注额之前按均衡赔付表(返还率 100%)，之后按常规
// 赔付表(约 80%)，脚本连续下注只会输得更快，所以不像俄罗斯方块那样要求人机验证。管理员设了每日净赢上限时，
// 同一 IP 下的账号合计计算。
func SpinSlots(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Slots
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req slotsSpinRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Bet < 1 || req.Bet > rule.MaxBet {
		common.ApiErrorI18n(c, i18n.MsgGameBetInvalid, map[string]any{"Max": rule.MaxBet})
		return
	}
	userId := c.GetInt("id")
	var spin slots.Result
	stage := slots.StageHouse
	settled, err := model.SettleGameWager(userId, gameSlots, model.GameWager{
		Bet: req.Bet,
		// 阶段按这一局之前的累计下注在结算事务里决定：同一玩家的几次旋转依次结算，并发请求多转不出均衡阶段的局。
		Play: func(wageredBefore int) (int, string, error) {
			if wageredBefore < rule.EvenStageWager {
				stage = slots.StageEven
			}
			var err error
			if spin, err = spinSlots(stage); err != nil {
				return 0, "", err
			}
			detail, err := common.Marshal(slotsDetail{Bet: req.Bet, Stage: slots.StageKeys[stage], Stops: spin.Stops, Multiplier: spin.Multiplier})
			return req.Bet * spin.Multiplier, string(detail), err
		},
		DailyLimit: rule.DailyRewardLimit,
		Day:        gameCenterDay(),
		Ip:         gameRewardIpKey(c.ClientIP()),
	})
	switch {
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
		return
	case errors.Is(err, model.ErrGameWagerDailyLimit):
		common.ApiErrorI18n(c, i18n.MsgGameWinLimitReached)
		return
	case errors.Is(err, model.ErrGameWagerIpLimit):
		common.ApiErrorI18n(c, i18n.MsgGameIpWinLimitReached)
		return
	case err != nil:
		common.SysError(fmt.Sprintf("failed to settle slots spin for user %d: %v", userId, err))
		common.ApiErrorI18n(c, i18n.MsgGameSpinFailed)
		return
	}
	// 大奖记一条带来源 IP 的审计日志，风控的多账号视图能看到同一 IP 下哪些账号从老虎机赢走了游戏币。
	// 每转都记会被连续旋转刷满，只记派彩达到下注 slotsAuditMultiplier 倍的。
	if spin.Multiplier >= slotsAuditMultiplier {
		model.RecordAuditLog(model.ClientLogSource(c), userId, "", model.LogTypeSystem,
			fmt.Sprintf("游戏大奖：老虎机下注 %d，%d 倍，派彩 %d 个游戏币", req.Bet, spin.Multiplier, settled.Session.Reward),
			map[string]interface{}{"game": gameSlots, "session_id": settled.Session.Id, "bet": req.Bet, "multiplier": spin.Multiplier, "game_coins": settled.Session.Reward})
	}
	common.ApiSuccess(c, gin.H{
		"stops":              spin.Stops,
		"line":               spin.Line,
		"multiplier":         spin.Multiplier,
		"matched":            spin.Matched,
		"stage":              slots.StageKeys[stage],
		"bet":                req.Bet,
		"win":                req.Bet * spin.Multiplier,
		"payout":             settled.Session.Reward,
		"limited_by":         settled.Session.LimitedBy,
		"balance":            settled.Balance,
		"wagered":            settled.Wagered,
		"earned_today":       settled.Earned,
		"ip_earned_today":    settled.IpEarned,
		"daily_reward_limit": rule.DailyRewardLimit,
		"even_stage_wager":   rule.EvenStageWager,
		"max_bet":            rule.MaxBet,
	})
}
