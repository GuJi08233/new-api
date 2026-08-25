package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	// 追加分组订阅授予的分组只对持有者本人可见/可选
	if attachedGroups, err := model.GetUserAttachedGroups(userId); err == nil {
		for _, attachedGroup := range attachedGroups {
			if _, ok := userUsableGroups[attachedGroup]; !ok {
				userUsableGroups[attachedGroup] = "订阅附加分组"
			}
		}
	}
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]interface{}{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	// auto 为系统内建虚拟分组，始终可选：候选为该用户有权使用的全部分组，
	// 不产生额外权限，因此无需管理员手动配置；配置了描述时优先使用
	autoDesc := "自动选择最优分组"
	if desc, ok := userUsableGroups["auto"]; ok && desc != "" {
		autoDesc = desc
	}
	usableGroups["auto"] = map[string]interface{}{
		"ratio": "自动",
		"desc":  autoDesc,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
