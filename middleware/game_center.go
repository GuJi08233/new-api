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
