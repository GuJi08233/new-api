package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GameCenterEnabled 在游戏中心关闭时拒绝用户侧的钱包接口(兑换、提现与记录)。游戏接口不挂它，由各游戏自己判断开放与否：
// 关闭后不能开新局，押着游戏币的一局还要能玩完。管理端接口也不挂它：关闭期间管理员仍要能配置、查看提现与调整余额。
func GameCenterEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !operation_setting.GetGameSetting().Enabled {
			common.ApiErrorI18n(c, i18n.MsgGameCenterDisabled)
			c.Abort()
			return
		}
		c.Next()
	}
}

// GameRequestBodyLimit 给整组游戏接口的请求体设一个总上限，各接口解析时再按自己的请求收紧。
func GameRequestBodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// GamePlayRateLimit 按用户限制开局与结算的频率。一局最快几秒就能结束，关键操作的限流对游戏太紧；
// 但每次开局都要写一条对局记录、每次结算都要重放整局，仍要挡住脚本连续请求。
func GamePlayRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(20, 60, "GP:"+scope)
}

// GameSpinRateLimit 按用户限制老虎机旋转、Plinko 投球这类下注的频率。一次只要两三秒，比开局和结算频繁得多；
// 每次下注仍要在一个事务里扣注、派彩、写流水，脚本连续请求时要挡住。
func GameSpinRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(60, 60, "GP:"+scope)
}

// GameActionRateLimit 按用户限制对局中每一步操作的频率，例如扫雷翻开一格、挖宝翻开一块、棋类落一子。
// 一局要点很多下，但每一步只改一行对局状态。
func GameActionRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(300, 60, "GP:"+scope)
}

// GameAiRateLimit 按用户限制人机对战的落子频率。每一步都要在请求里让服务端 AI 搜索一次，比普通的一步贵得多；
// 人下棋每秒最多一两步，每分钟 60 步足够。
func GameAiRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(60, 60, "GP:"+scope)
}

// GamePollRateLimit 按用户限制对战房间与奖池的轮询频率。房间用长轮询，正常每分钟只有几次；奖池每两秒刷新一次。
func GamePollRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(120, 60, "GP:"+scope)
}
