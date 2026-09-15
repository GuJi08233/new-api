package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
	if !channelError.AutoBan {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

// EnableChannelKey 恢复多密钥渠道中被自动禁用的一把密钥并通知 root；
// 渠道整体因此由自动禁用回到启用时，通知内容一并说明。返回密钥状态是否发生变化。
func EnableChannelKey(channelId int, keyIndex int, channelName string) bool {
	keyChanged, channelRecovered, err := model.EnableChannelKey(channelId, keyIndex)
	if err != nil {
		common.SysError(fmt.Sprintf("通道「%s」（#%d）密钥 #%d 恢复失败：%v", channelName, channelId, keyIndex+1, err))
		return false
	}
	if !keyChanged {
		return false
	}
	subject := fmt.Sprintf("通道「%s」（#%d）密钥 #%d 已自动恢复", channelName, channelId, keyIndex+1)
	content := fmt.Sprintf("通道「%s」（#%d）密钥 #%d 测试通过，已自动恢复启用", channelName, channelId, keyIndex+1)
	if channelRecovered {
		subject = fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content = fmt.Sprintf("通道「%s」（#%d）密钥 #%d 测试通过，通道已恢复启用", channelName, channelId, keyIndex+1)
	}
	NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	return true
}

// ShouldDisableChannel 按全局规则判定上游错误是否应触发自动禁用。
func ShouldDisableChannel(err *types.NewAPIError) bool {
	return shouldDisableChannel(err, nil)
}

// ShouldDisableChannelWithSetting 在全局规则之上叠加渠道级自动禁用状态码：
// 渠道配置了 AutoDisableStatusCodes 时，用它替换全局状态码列表判定本渠道，并视为该渠道
// 显式开启了自动禁用（不再要求全局 AutomaticDisableChannelEnabled）。
// 关键词与 channel: 类错误的规则保持不变；配置无法解析时退回全局规则。
func ShouldDisableChannelWithSetting(err *types.NewAPIError, setting dto.ChannelSettings) bool {
	ranges, parseErr := operation_setting.ParseHTTPStatusCodeRanges(setting.AutoDisableStatusCodes)
	if parseErr != nil {
		return shouldDisableChannel(err, nil)
	}
	return shouldDisableChannel(err, ranges)
}

func shouldDisableChannel(err *types.NewAPIError, channelRanges []operation_setting.StatusCodeRange) bool {
	if err == nil {
		return false
	}
	if !common.AutomaticDisableChannelEnabled && len(channelRanges) == 0 {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if len(channelRanges) > 0 {
		if operation_setting.MatchStatusCodeRanges(channelRanges, err.StatusCode) {
			return true
		}
	} else if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}
