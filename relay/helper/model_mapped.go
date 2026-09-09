package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	originModelName := info.OriginModelName
	mappingModelName := originModelName
	if isResponsesCompact && strings.HasSuffix(originModelName, ratio_setting.CompactModelSuffix) {
		mappingModelName = strings.TrimSuffix(originModelName, ratio_setting.CompactModelSuffix)
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := common.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}

		// 支持链式模型重定向，最终使用链尾的模型
		currentModel := mappingModelName
		visitedModels := map[string]bool{
			currentModel: true,
		}
		for {
			if mappedModel, exists := modelMap[currentModel]; exists && mappedModel != "" {
				// 模型重定向循环检测，避免无限循环
				if visitedModels[mappedModel] {
					if mappedModel == currentModel {
						if currentModel == info.OriginModelName {
							// 恒等映射等于没有重定向。这里不能提前 return：下面还要清掉
							// 上一个渠道留在上下文里的重定向记录。
							info.IsModelMapped = false
							break
						} else {
							info.IsModelMapped = true
							break
						}
					}
					return errors.New("model_mapping_contains_cycle")
				}
				visitedModels[mappedModel] = true
				currentModel = mappedModel
				info.IsModelMapped = true
			} else {
				break
			}
		}
		if info.IsModelMapped {
			info.UpstreamModelName = currentModel
		}
	}

	if isResponsesCompact {
		finalUpstreamModelName := mappingModelName
		if info.IsModelMapped && info.UpstreamModelName != "" {
			finalUpstreamModelName = info.UpstreamModelName
		}
		info.UpstreamModelName = finalUpstreamModelName
		info.OriginModelName = ratio_setting.WithCompactModelSuffix(finalUpstreamModelName)
	}
	// 记录生效的重定向，供响应体模型名回写与错误文案脱敏使用。重试换到不做重定向的
	// 渠道时要清掉上一个渠道的记录。
	if info.IsModelMapped {
		// 目标名取客户端请求里的模型名，而不是 info.OriginModelName：Responses Compact
		// 分支上面刚把后者改写成带 CompactModelSuffix 的名字。CompactModelSuffix 本身
		// 也是网关内部约定（distributor 在写入 original_model 之前就补上了它），客户端
		// 从未发过带后缀的名字，所以这里要去掉。
		requestModelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
		if isResponsesCompact {
			requestModelName = strings.TrimSuffix(requestModelName, ratio_setting.CompactModelSuffix)
		}
		service.SetModelRewrite(c, info, info.UpstreamModelName, requestModelName)
	} else {
		service.ClearModelRewrite(c)
	}

	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
