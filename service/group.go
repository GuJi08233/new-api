package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

// SplitTokenGroups 把令牌分组字段按逗号拆分为去空格、去重的有序列表。
func SplitTokenGroups(tokenGroup string) []string {
	groups := make([]string, 0)
	seen := make(map[string]bool)
	for _, group := range strings.Split(tokenGroup, ",") {
		group = strings.TrimSpace(group)
		if group == "" || seen[group] {
			continue
		}
		seen[group] = true
		groups = append(groups, group)
	}
	return groups
}

// IsMultiCandidateGroup 判断令牌分组是否为多候选模式（auto 或逗号分隔的多分组）。
// 该模式下渠道选择按候选序列依次尝试，实际选中的分组通过 ContextKeyAutoGroup 传导给计费。
func IsMultiCandidateGroup(tokenGroup string) bool {
	return tokenGroup == "auto" || strings.Contains(tokenGroup, ",")
}

// ResolveCandidateGroups 返回多候选模式下的分组序列；单分组模式返回 nil。
// "auto"：全局优先级序列与用户可用分组（含订阅附加分组）的交集。
// 逗号分隔的多分组：TokenAuth 已过滤为当前有效子集，这里按令牌内顺序拆分。
func ResolveCandidateGroups(ctx *gin.Context, tokenGroup string) []string {
	if tokenGroup == "auto" {
		userId := ctx.GetInt("id")
		userGroup := common.GetContextKeyString(ctx, constant.ContextKeyUserGroup)
		return GetUserAutoGroup(userId, userGroup)
	}
	if strings.Contains(tokenGroup, ",") {
		return SplitTokenGroups(tokenGroup)
	}
	return nil
}

// FilterUsableTokenGroups 把令牌分组（可能为逗号分隔的多分组）过滤为该用户当前
// 有权使用的有效子集，保持令牌内的顺序；多分组中混入 auto 时按纯 auto 处理。
// 返回过滤后的分组串和最后一条拒绝原因（结果为空时用于报错）。
func FilterUsableTokenGroups(userId int, userGroup string, tokenGroup string) (string, string) {
	groups := SplitTokenGroups(tokenGroup)
	for _, group := range groups {
		if group == "auto" {
			groups = []string{"auto"}
			break
		}
	}
	usableGroups := GetUserUsableGroups(userGroup)
	validGroups := make([]string, 0, len(groups))
	rejectReason := ""
	for _, group := range groups {
		// auto 为系统内建虚拟分组，始终可用（不产生额外权限，
		// 实际候选在渠道选择时按用户可用分组过滤）
		if group == "auto" {
			validGroups = append(validGroups, group)
			continue
		}
		if _, ok := usableGroups[group]; !ok {
			// 常规可用分组未命中时，再检查追加分组订阅授予的分组（惰性查询，
			// 只有依赖订阅附加分组的请求才产生这次查库）；订阅过期后该分组
			// 在这里被自动跳过，多分组令牌无感回退到剩余分组
			if !model.UserHasAttachedGroup(userId, group) {
				rejectReason = fmt.Sprintf("无权访问 %s 分组", group)
				continue
			}
		}
		if !ratio_setting.ContainsGroupRatio(group) {
			rejectReason = fmt.Sprintf("分组 %s 已被弃用", group)
			continue
		}
		validGroups = append(validGroups, group)
	}
	return strings.Join(validGroups, ","), rejectReason
}

// GetUserAutoGroup 获取自动分组候选序列（按全局优先级顺序），包含订阅追加分组
// 授予的分组；订阅过期后对应分组自动从序列中消失。
func GetUserAutoGroup(userId int, userGroup string) []string {
	usableGroups := GetUserUsableGroups(userGroup)
	if userId > 0 {
		if attachedGroups, err := model.GetUserAttachedGroups(userId); err == nil {
			for _, attachedGroup := range attachedGroups {
				if _, ok := usableGroups[attachedGroup]; !ok {
					usableGroups[attachedGroup] = "订阅附加分组"
				}
			}
		}
	}
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := usableGroups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
