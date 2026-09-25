package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// GameCenterEnabled 在游戏中心关闭时拒绝用户侧的游戏接口。管理端接口不挂它：
// 关闭期间管理员仍要能配置、查看提现与调整余额。
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

// GamePlayRateLimit 按用户限制开局与结算的频率。一局最快几秒就能结束，关键操作的限流对游戏太紧；
// 但每次开局都要写一条对局记录、每次结算都要重放整局，仍要挡住脚本连续请求。
func GamePlayRateLimit(scope string) gin.HandlerFunc {
	return userRateLimitFactory(20, 60, "GP:"+scope)
}

// GameSpinRateLimit 按用户限制老虎机的旋转频率。转一次只要两三秒，比开局和结算频繁得多；
// 每次旋转仍要在一个事务里扣注、派彩、写流水，脚本连续请求时要挡住。
func GameSpinRateLimit() gin.HandlerFunc {
	return userRateLimitFactory(60, 60, "GP:slots-spin")
}
